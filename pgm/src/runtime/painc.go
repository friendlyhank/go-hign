package runtime

import "runtime/internal/atomic"

//go:nosplit
func throw(s string) {
	// Everything throw does should be recursively nosplit so it
	// can be called even when it's unsafe to grow the stack.
	systemstack(func() {
		print("fatal error: ", s, "\n")
	})
	//获取当前g
	gp :=getg()
	//将状态更换为异常状态
	if gp.m.throwing == 0{
		gp.m.throwing = 1
	}
	fatalthrow()
	*(*int)(nil) = 0 // not reached
}

// panicking is non-zero when crashing the program for an unrecovered panic.
// panicking is incremented and decremented atomically.
var panicking uint32 //抛出异常

// paniclk is held while printing the panic information and stack trace,
// so that two concurrent panics don't overlap their output.
var paniclk mutex //抛出异常会锁定

// fatalthrow implements an unrecoverable runtime throw. It freezes the
// system, prints stack traces starting from its caller, and terminates the
// process.
//
//go:nosplit
func fatalthrow() {
	pc := getcallerpc()
	sp := getcallersp()
	gp := getg()
	// Switch to the system stack to avoid any stack growth, which
	// may make things worse if the runtime is in a bad state.
	systemstack(func() {
		startpanic_m()

		if dopanic_m(gp, pc, sp) {
			// crash uses a decent amount of nosplit stack and we're already
			// low on stack in throw, so crash on the system stack (unlike
			// fatalpanic).
			crash()
		}

		exit(2)
	})

	*(*int)(nil) = 0 // not reached
}

// startpanic_m prepares for an unrecoverable panic.
//
// It returns true if panic messages should be printed, or false if
// the runtime is in bad shape and should just print stacks.
//
// It must not have write barriers even though the write barrier
// explicitly ignores writes once dying > 0. Write barriers still
// assume that g.m.p != nil, and this function may not have P
// in some contexts (e.g. a panic in a signal handler for a signal
// sent to an M with no P).
//
//go:nowritebarrierrec
func startpanic_m() bool {
	_g_ := getg()
	if mheap_.cachealloc.size == 0 { // very early
		print("runtime: panic before malloc heap initialized\n")
	}
	// Disallow malloc during an unrecoverable panic. A panic
	// could happen in a signal handler, or in a throw, or inside
	// malloc itself. We want to catch if an allocation ever does
	// happen (even if we're not in one of these situations).
	_g_.m.mallocing++

	// If we're dying because of a bad lock count, set it to a
	// good lock count so we don't recursively panic below.
	if _g_.m.locks < 0{
		_g_.m.locks = 1
	}

	switch _g_.m.dying {
	case 0:
		// Setting dying >0 has the side-effect of disabling this G's writebuf.
		_g_.m.dying = 1
		atomic.Xadd(&panicking, 1)
		lock(&paniclk)
		if debug.schedtrace > 0 || debug.scheddetail > 0{
			schedtrace(true)
		}
		freezetheworld()
		return true
	case 1:
		// Something failed while panicking.
		// Just print a stack trace and exit.
		_g_.m.dying = 2
		print("panic during panic\n")
		return false
	case 2:
		// This is a genuine bug in the runtime, we couldn't even
		// print the stack trace successfully.
		_g_.m.dying = 3
		print("stack trace unavailable\n")
		exit(4)
		fallthrough
	default:
		// Can't even print! Just exit.
		exit(5)
		return false // Need to return something.
	}
}

func dopanic_m(gp *g, pc, sp uintptr) bool {
	if gp.sig != 0 {
		signame := signame(gp.sig)
		if signame != "" {
			print("[signal ", signame)
		} else {
			print("[signal ", hex(gp.sig))
		}
		print(" code=", hex(gp.sigcode0), " addr=", hex(gp.sigcode1), " pc=", hex(gp.sigpc), "]\n")
	}

	level, all, docrash := gotraceback()
	_g_ := getg()
	if level > 0 {
		if gp != gp.m.curg {
			all = true
		}
		if gp != gp.m.g0 {
			print("\n")
			goroutineheader(gp)
			traceback(pc, sp, 0, gp)
		} else if level >= 2 || _g_.m.throwing > 0 {
			print("\nruntime stack:\n")
			traceback(pc, sp, 0, gp)
		}
		if !didothers && all {
			didothers = true
			tracebackothers(gp)
		}
	}
	unlock(&paniclk)

	if atomic.Xadd(&panicking, -1) != 0 {
		// Some other m is panicking too.
		// Let it print what it needs to print.
		// Wait forever without chewing up cpu.
		// It will exit when it's done.
		lock(&deadlock)
		lock(&deadlock)
	}

	printDebugLog()

	if throwReportQuirk != nil {
		throwReportQuirk()
	}

	return docrash
}
