// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package runtime

import (
	"runtime/internal/sys"
	"unsafe"
	)

// Stack describes a Go execution stack.
// The bounds of the stack are exactly [lo, hi),
// with no implicit data structures on either side.
type stack struct {
	lo uintptr
	hi uintptr
}

type g struct{
	// Stack parameters.
	// stack describes the actual stack memory: [stack.lo, stack.hi).
	// stackguard0 is the stack pointer compared in the Go stack growth prologue.
	// It is stack.lo+StackGuard normally, but can be StackPreempt to trigger a preemption.
	// stackguard1 is the stack pointer compared in the C stack growth prologue.
	// It is stack.lo+StackGuard on g0 and gsignal stacks.
	// It is ~0 on other goroutine stacks, to trigger a call to morestackc (and crash).
	stack       stack   // offset known to runtime/cgo
	stackguard0 uintptr // offset known to liblink
	stackguard1 uintptr // offset known to liblink

	m            *m      //当前绑定的m current m; offset known to arm liblink
	sched        gobuf
}

type m struct{
	g0      *g     // goroutine with scheduling stack
	morebuf gobuf

	gsignal       *g           // signal-handling g
	tls           [6]uintptr   // thread-local storage (for x86 extern register)
	curg          *g       // current running goroutine
	p             puintptr // attached p for executing go code (nil if not executing go code)
	throwing int32 //1有异常 0无异常
	profilehz int32

	// these are here because they are too large to be on the stack
	// of low-level NOSPLIT functions.
	//系统内核调用os.windows
	libcall libcall
	libcallpc uintptr //for cpu profiler
	libcallsp uintptr
	libcallg  guintptr //保存系统调用时的g
	syscall libcall
}

type p struct{
	// wbBuf is this P's GC write barrier buffer.
	//
	// TODO: Consider caching this in the running G.
	wbBuf wbBuf
}

type schedt struct{
	maxmcount    int32 //maximum number of m's allowed (or die) 设置m的最大数量

	procresizetime int64 // nanotime() of last change to gomaxprocs 最后一次调整p数量的时间(毫秒)
	totaltime int64 // ∫gomaxprocs dt up to procresizetime 最后一次和最新调整p数量的总时间
}

type gobuf struct {
	// The offsets of sp, pc, and g are known to (hard-coded in) libmach.
	//
	// ctxt is unusual with respect to GC: it may be a
	// heap-allocated funcval, so GC needs to track it, but it
	// needs to be set and cleared from assembly, where it's
	// difficult to have write barriers. However, ctxt is really a
	// saved, live register, and we only ever exchange it between
	// the real register and the gobuf. Hence, we treat it as a
	// root during stack scanning, which means assembly that saves
	// and restores it doesn't need write barriers. It's still
	// typed as a pointer so that any other writes from Go get
	// write barriers.
	sp   uintptr
	pc   uintptr
	g    guintptr
	ctxt unsafe.Pointer
	ret  sys.Uintreg
	lr   uintptr
	bp   uintptr // for GOEXPERIMENT=framepointer
}

type funcval struct{

}

type iface struct{}

type eface struct{}

// The guintptr, muintptr, and puintptr are all used to bypass write barriers.
// It is particularly important to avoid write barriers when the current P has
// been released, because the GC thinks the world is stopped, and an
// unexpected write barrier would not be synchronized with the GC,
// which can lead to a half-executed write barrier that has marked the object
// but not queued it. If the GC skips the object and completes before the
// queuing can occur, it will incorrectly free the object.
//
// We tried using special assignment functions invoked only when not
// holding a running P, but then some updates to a particular memory
// word went through write barriers and some did not. This breaks the
// write barrier shadow checking mode, and it is also scary: better to have
// a word that is completely ignored by the GC than to have one for which
// only a few updates are ignored.
//
// Gs and Ps are always reachable via true pointers in the
// allgs and allp lists or (during allocation before they reach those lists)
// from stack variables.
//
// Ms are always reachable via true pointers either from allm or
// freem. Unlike Gs and Ps we do free Ms, so it's important that
// nothing ever hold an muintptr across a safe point.

// A guintptr holds a goroutine pointer, but typed as a uintptr
// to bypass write barriers. It is used in the Gobuf goroutine state
// and in scheduling lists that are manipulated without a P.
//
// The Gobuf.g goroutine pointer is almost always updated by assembly code.
// In one of the few places it is updated by Go code - func save - it must be
// treated as a uintptr to avoid a write barrier being emitted at a bad time.
// Instead of figuring out how to emit the write barriers missing in the
// assembly manipulation, we change the type of the field to uintptr,
// so that it does not require write barriers at all.
//
// Goroutine structs are published in the allg list and never freed.
// That will keep the goroutine structs from being collected.
// There is never a time that Gobuf.g's contain the only references
// to a goroutine: the publishing of the goroutine in allg comes first.
// Goroutine pointers are also kept in non-GC-visible places like TLS,
// so I can't see them ever moving. If we did want to start moving data
// in the GC, we'd need to allocate the goroutine structs from an
// alternate arena. Using guintptr doesn't make that problem any worse.
//g链表
type guintptr uintptr

//go:nosplit
func (gp guintptr) ptr() *g { return (*g)(unsafe.Pointer(gp)) }

//go:nosplit
func (gp *guintptr) set(g *g) { *gp = guintptr(unsafe.Pointer(g)) }

type puintptr uintptr

type sudog struct {}

//系统调用
type libcall struct{
	fn uintptr //系统调用方法
	n uintptr  //参数的个数
	args uintptr //参数
	r1 uintptr //return values 返回值
	r2 uintptr
	err uintptr
}

// describes how to handle callback
type wincallbackcontext struct {
	gobody unsafe.Pointer // go function to call
	argsize uintptr // callback arguments size (in bytes)
}

type itab struct {}

var(
	allm *m
	allp       []*p  // len(allp) == gomaxprocs; may change at safe points, otherwise immutable p的数量,一般与gomaxprocs相等
	ncpu int32 //cpu的核数
	gomaxprocs int32 //当前最大的p的数量

	// Information about what cpu features are available.
	// Packages outside the runtime should not use these
	// as they are not an external api.
	// Set on startup in asm_{386,amd64}.s
	//在asm_{386,amd64}.s/rt0.go被设置
	processorVersionInfo uint32
	isIntel bool
	lfenceBeforeRdtsc bool
)