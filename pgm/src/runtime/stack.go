// Copyright 2013 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package runtime

import (
	"internal/cpu"
	"runtime/internal/sys"
	"unsafe"
)

/*
Stack layout parameters.
Included both by runtime (compiled via 6c) and linkers (compiled via gcc).

The per-goroutine g->stackguard is set to point StackGuard bytes
above the bottom of the stack.  Each function compares its stack
pointer against g->stackguard to check for overflow.  To cut one
instruction from the check sequence for functions with tiny frames,
the stack is allowed to protrude StackSmall bytes below the stack
guard.  Functions with large frames don't bother with the check and
always call morestack.  The sequences are (for amd64, others are
similar):

	guard = g->stackguard
	frame = function's stack frame size
	argsize = size of function arguments (call + return)

	stack frame size <= StackSmall:
		CMPQ guard, SP
		JHI 3(PC)
		MOVQ m->morearg, $(argsize << 32)
		CALL morestack(SB)

	stack frame size > StackSmall but < StackBig
		LEAQ (frame-StackSmall)(SP), R0
		CMPQ guard, R0
		JHI 3(PC)
		MOVQ m->morearg, $(argsize << 32)
		CALL morestack(SB)

	stack frame size >= StackBig:
		MOVQ m->morearg, $((argsize << 32) | frame)
		CALL morestack(SB)

The bottom StackGuard - StackSmall bytes are important: there has
to be enough room to execute functions that refuse to check for
stack overflow, either because they need to be adjacent to the
actual caller's frame (deferproc) or because they handle the imminent
stack overflow (morestack).

For example, deferproc might call malloc, which does one of the
above checks (without allocating a full frame), which might trigger
a call to morestack.  This sequence needs to fit in the bottom
section of the stack.  On amd64, morestack's frame is 40 bytes, and
deferproc's frame is 56 bytes.  That fits well within the
StackGuard - StackSmall bytes at the bottom.
The linkers explore all possible call traces involving non-splitting
functions to make sure that this limit cannot be violated.
*/

const (
	// StackSystem is a number of additional bytes to add
	// to each stack below the usual guard area for OS-specific
	// purposes like signal handling. Used on Windows, Plan 9,
	// and iOS because they do not use a separate stack.
	_StackSystem = sys.GoosWindows*512*sys.PtrSize + sys.GoosPlan9*512 + sys.GoosDarwin*sys.GoarchArm*1024 + sys.GoosDarwin*sys.GoarchArm64*1024

	// The stack guard is a pointer this many bytes above the
	// bottom of the stack.
	_StackGuard = 928*sys.StackGuardMultiplier + _StackSystem
)

// Global pool of spans that have free stacks.
// Stacks are assigned an order according to size.
//     order = log_2(size/FixedStack)
// There is a free list for each order.
var stackpool [_NumStackOrders]struct {
	item stackpoolItem
	_    [cpu.CacheLinePadSize - unsafe.Sizeof(stackpoolItem{})%cpu.CacheLinePadSize]byte
}

//go:notinheap
type stackpoolItem struct {

}

// Global pool of spans that have free stacks.
// Stacks are assigned an order according to size.
//     order = log_2(size/FixedStack)
// There is a free list for each order.

//栈信息的初始化
func stackinit(){

}

// Called from runtime·morestack when more stack is needed.
// Allocate larger stack and relocate to new stack.
// Stack growth is multiplicative, for constant amortized cost.
//
// g->atomicstatus will be Grunning or Gscanrunning upon entry.
// If the scheduler is trying to stop this g, then it will set preemptStop.
//
// This must be nowritebarrierrec because it can be called as part of
// stack growth from other nowritebarrierrec functions, but the
// compiler doesn't check this.
//
//go:nowritebarrierrec
func newstack() {

}

var maxstacksize uintptr = 1 << 20 // enough until runtime.main sets it for real

// This is exported as ABI0 via linkname so obj can call it.
//
//go:nosplit
//go:linkname morestackc
func morestackc() {
}