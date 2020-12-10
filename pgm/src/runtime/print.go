package runtime

import (
	"runtime/internal/atomic"
	"runtime/internal/sys"
	"unsafe"
)

// The compiler knows that a print of a value of this type
// should use printhex instead of printuint (decimal).
type hex uint64

// write to goroutine-local buffer if diverting output,
// or else standard error.
func gwrite(b []byte) {
	if len(b) == 0 {
		return
	}
	recordForPanic(b)
	gp := getg()
	// Don't use the writebuf if gp.m is dying. We want anything
	// written through gwrite to appear in the terminal rather
	// than be written to in some buffer, if we're in a panicking state.
	// Note that we can't just clear writebuf in the gp.m.dying case
	// because a panic isn't allowed to have any write barriers.
	if gp == nil || gp.writebuf == nil || gp.m.dying > 0 {
		writeErr(b)
		return
	}

	n := copy(gp.writebuf[len(gp.writebuf):cap(gp.writebuf)], b)
	gp.writebuf = gp.writebuf[:len(gp.writebuf)+n]
}

var (
	// printBacklog is a circular buffer of messages written with the builtin
	// print* functions, for use in postmortem analysis of core dumps.
	printBacklog      [512]byte
	printBacklogIndex int
)

// recordForPanic maintains a circular buffer of messages written by the
// runtime leading up to a process crash, allowing the messages to be
// extracted from a core dump.
//
// The text written during a process crash (following "panic" or "fatal
// error") is not saved, since the goroutine stacks will generally be readable
// from the runtime datastructures in the core file.
func recordForPanic(b []byte) {
	printlock()

	if atomic.Load(&panicking) == 0 {
		// Not actively crashing: maintain circular buffer of print output.
		for i := 0; i < len(b); {
			n := copy(printBacklog[printBacklogIndex:], b[i:])
			i += n
			printBacklogIndex += n
			printBacklogIndex %= len(printBacklog)
		}
	}

	printunlock()
}

// The compiler emits calls to printlock and printunlock around
// the multiple calls that implement a single Go print or println
// statement. Some of the print helpers (printslice, for example)
// call print recursively. There is also the problem of a crash
// happening during the print routines and needing to acquire
// the print lock to print information about the crash.
// For both these reasons, let a thread acquire the printlock 'recursively'.

func printlock() {

}

func printunlock() {

}

func printnl() {}

func printuint(){
}

func printint(v int64) {

}

func printpointer(p unsafe.Pointer) {

}

func printstring(s string){

}

// hexdumpWords prints a word-oriented hex dump of [p, end).
//
// If mark != nil, it will be called with each printed word's address
// and should return a character mark to appear just before that
// word's value. It can return 0 to indicate no mark.
func hexdumpWords(p, end uintptr, mark func(uintptr) byte) {
	p1 := func(x uintptr) {
		var buf [2 * sys.PtrSize]byte
		for i := len(buf) - 1; i >= 0; i-- {
			if x&0xF < 10 {
				buf[i] = byte(x&0xF) + '0'
			} else {
				buf[i] = byte(x&0xF) - 10 + 'a'
			}
			x >>= 4
		}
		gwrite(buf[:])
	}

	printlock()
	var markbuf [1]byte
	markbuf[0] = ' '
	for i := uintptr(0); p+i < end; i += sys.PtrSize {
		if i%16 == 0 {
			if i != 0 {
				println()
			}
			p1(p + i)
			print(": ")
		}

		if mark != nil {
			markbuf[0] = mark(p + i)
			if markbuf[0] == 0 {
				markbuf[0] = ' '
			}
		}
		gwrite(markbuf[:])
		val := *(*uintptr)(unsafe.Pointer(p + i))
		p1(val)
		print(" ")

		// Can we symbolize val?
		fn := findfunc(val)
		if fn.valid() {
			print("<", funcname(fn), "+", val-fn.entry, "> ")
		}
	}
	println()
	printunlock()
}
