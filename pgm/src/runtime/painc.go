package runtime

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
	*(*int)(nil) = 0 // not reached
}

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
