package runtime

import "unsafe"

// getg returns the pointer to the current g.
// The compiler rewrites calls to this function into instructions
// that fetch the g directly (from TLS or from the dedicated register).
func getg()*g

// systemstack runs fn on a system stack.
// If systemstack is called from the per-OS-thread (g0) stack, or
// if systemstack is called from the signal handling (gsignal) stack,
// systemstack calls fn directly and returns.
// Otherwise, systemstack is being called from the limited stack
// of an ordinary goroutine. In this case, systemstack switches
// to the per-OS-thread stack, calls fn, and switches back.
// It is common to use a func literal as the argument, in order
// to share inputs and outputs with the code around the call
// to system stack:
//
//	... set up y ...
//	systemstack(func() {
//		x = bigcall(y)
//	})
//	... use x ...
//
//runtime/asm_amd64.s  切换到g0执行栈,相当于系统栈的调用
//go:noescape
func systemstack(fn func())


// in internal/bytealg/equal_*.s
//go:noescape
func memequal(a, b unsafe.Pointer, size uintptr) bool

func memequal_varlen(a, b unsafe.Pointer) bool