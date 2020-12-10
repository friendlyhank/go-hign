package runtime

import "unsafe"

// Addresses collected in a cgo backtrace when crashing.
// Length must match arg.Max in x_cgo_callers in runtime/cgo/gcc_traceback.c.
type cgoCallers [32]uintptr

var racecgosync uint64 // represents possible synchronization in C code

// Call from Go to C.
//
// This must be nosplit because it's used for syscalls on some
// platforms. Syscalls may have untyped arguments on the stack, so
// it's not safe to grow or scan the stack.
//
//go:nosplit
func cgocall(fn, arg unsafe.Pointer) int32 {
	if !iscgo && GOOS != "solaris" && GOOS != "illumos" && GOOS != "windows" {
		throw("cgocall unavailable")
	}

	if fn == nil {
		throw("cgocall nil")
	}

	if raceenabled {
		racereleasemerge(unsafe.Pointer(&racecgosync))
	}

	mp := getg().m
	mp.ncgocall++
	mp.ncgo++

	// Reset traceback.
	mp.cgoCallers[0] = 0

	// Announce we are entering a system call
	// so that the scheduler knows to create another
	// M to run goroutines while we are in the
	// foreign code.
	//
	// The call to asmcgocall is guaranteed not to
	// grow the stack and does not allocate memory,
	// so it is safe to call while "in a system call", outside
	// the $GOMAXPROCS accounting.
	//
	// fn may call back into Go code, in which case we'll exit the
	// "system call", run the Go code (which may grow the stack),
	// and then re-enter the "system call" reusing the PC and SP
	// saved by entersyscall here.
	entersyscall()

	// Tell asynchronous preemption that we're entering external
	// code. We do this after entersyscall because this may block
	// and cause an async preemption to fail, but at this point a
	// sync preemption will succeed (though this is not a matter
	// of correctness).
	osPreemptExtEnter(mp)

	mp.incgo = true
	errno := asmcgocall(fn, arg)

	// Update accounting before exitsyscall because exitsyscall may
	// reschedule us on to a different M.
	mp.incgo = false
	mp.ncgo--

	osPreemptExtExit(mp)

	exitsyscall()

	// Note that raceacquire must be called only after exitsyscall has
	// wired this M to a P.
	if raceenabled {
		raceacquire(unsafe.Pointer(&racecgosync))
	}

	// From the garbage collector's perspective, time can move
	// backwards in the sequence above. If there's a callback into
	// Go code, GC will see this function at the call to
	// asmcgocall. When the Go call later returns to C, the
	// syscall PC/SP is rolled back and the GC sees this function
	// back at the call to entersyscall. Normally, fn and arg
	// would be live at entersyscall and dead at asmcgocall, so if
	// time moved backwards, GC would see these arguments as dead
	// and then live. Prevent these undead arguments from crashing
	// GC by forcing them to stay live across this time warp.
	KeepAlive(fn)
	KeepAlive(arg)
	KeepAlive(mp)

	return errno
}
