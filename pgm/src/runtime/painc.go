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
}
