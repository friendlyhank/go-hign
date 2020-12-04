// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package runtime

import (
	"runtime/internal/atomic"
	"runtime/internal/sys"
	"unsafe"
	)

// defined constants
const(
	// G status
	//
	// Beyond indicating the general state of a G, the G status
	// acts like a lock on the goroutine's stack (and hence its
	// ability to execute user code).
	//
	// If you add to this list, add to the list
	// of "okay during garbage collection" status
	// in mgcmark.go too.
	//
	// TODO(austin): The _Gscan bit could be much lighter-weight.
	// For example, we could choose not to run _Gscanrunnable
	// goroutines found in the run queue, rather than CAS-looping
	// until they become _Grunnable. And transitions like
	// _Gscanwaiting -> _Gscanrunnable are actually okay because
	// they don't affect stack ownership.

	// _Gidle means this goroutine was just allocated and has not
	// yet been initialized.
	//刚刚被分配并且还没有被初始化
	_Gidle = iota // 0

	// _Grunnable means this goroutine is on a run queue. It is
	// not currently executing user code. The stack is not owned.
	//没有执行代码，没有栈的所有权，存储在运行队列中
	_Grunnable // 1

	// _Grunning means this goroutine may execute user code. The
	// stack is owned by this goroutine. It is not on a run queue.
	// It is assigned an M and a P (g.m and g.m.p are valid).
	//可以执行代码，拥有栈的所有权，被赋予了内核线程 M 和处理器 P
	_Grunning // 2

	// _Gsyscall means this goroutine is executing a system call.
	// It is not executing user code. The stack is owned by this
	// goroutine. It is not on a run queue. It is assigned an M.
	//正在执行系统调用，拥有栈的所有权，没有执行用户代码，被赋予了内核线程 M 但是不在运行队列上
	_Gsyscall // 3

	// _Gwaiting means this goroutine is blocked in the runtime.
	// It is not executing user code. It is not on a run queue,
	// but should be recorded somewhere (e.g., a channel wait
	// queue) so it can be ready()d when necessary. The stack is
	// not owned *except* that a channel operation may read or
	// write parts of the stack under the appropriate channel
	// lock. Otherwise, it is not safe to access the stack after a
	// goroutine enters _Gwaiting (e.g., it may get moved).
	//由于运行时而被阻塞，没有执行用户代码并且不在运行队列上，但是可能存在于 Channel 的等待队列上
	_Gwaiting // 4

	// _Gmoribund_unused is currently unused, but hardcoded in gdb
	// scripts.
	_Gmoribund_unused // 5

	// _Gdead means this goroutine is currently unused. It may be
	// just exited, on a free list, or just being initialized. It
	// is not executing user code. It may or may not have a stack
	// allocated. The G and its stack (if any) are owned by the M
	// that is exiting the G or that obtained the G from the free
	// list.
	//没有被使用，没有执行代码，可能有分配的栈
	_Gdead // 6

	// _Genqueue_unused is currently unused.
	_Genqueue_unused // 7

	// _Gcopystack means this goroutine's stack is being moved. It
	// is not executing user code and is not on a run queue. The
	// stack is owned by the goroutine that put it in _Gcopystack.
	//栈正在被拷贝，没有执行代码，不在运行队列上
	_Gcopystack // 8

	// _Gpreempted means this goroutine stopped itself for a
	// suspendG preemption. It is like _Gwaiting, but nothing is
	// yet responsible for ready()ing it. Some suspendG must CAS
	// the status to _Gwaiting to take responsibility for
	// ready()ing this G.
	//由于抢占而被阻塞，没有执行用户代码并且不在运行队列上，等待唤醒
	_Gpreempted // 9

	// _Gscan combined with one of the above states other than
	// _Grunning indicates that GC is scanning the stack. The
	// goroutine is not executing user code and the stack is owned
	// by the goroutine that set the _Gscan bit.
	//
	// _Gscanrunning is different: it is used to briefly block
	// state transitions while GC signals the G to scan its own
	// stack. This is otherwise like _Grunning.
	//
	// atomicstatus&~Gscan gives the state the goroutine will
	// return to when the scan completes.
	//GC 正在扫描栈空间，没有执行代码，可以与其他状态同时存在
	_Gscan          = 0x1000
	_Gscanrunnable  = _Gscan + _Grunnable  // 0x1001
	_Gscanrunning   = _Gscan + _Grunning   // 0x1002
	_Gscansyscall   = _Gscan + _Gsyscall   // 0x1003
	_Gscanwaiting   = _Gscan + _Gwaiting   // 0x1004
	_Gscanpreempted = _Gscan + _Gpreempted // 0x1009
)

const(
	// P status

	// _Pidle means a P is not being used to run user code or the
	// scheduler. Typically, it's on the idle P list and available
	// to the scheduler, but it may just be transitioning between
	// other states.
	//
	// The P is owned by the idle list or by whatever is
	// transitioning its state. Its run queue is empty.
	//处理器没有运行用户代码或者调度器，被空闲队列或者改变其状态的结构持有，运行队列为空
	_Pidle = iota

	// _Prunning means a P is owned by an M and is being used to
	// run user code or the scheduler. Only the M that owns this P
	// is allowed to change the P's status from _Prunning. The M
	// may transition the P to _Pidle (if it has no more work to
	// do), _Psyscall (when entering a syscall), or _Pgcstop (to
	// halt for the GC). The M may also hand ownership of the P
	// off directly to another M (e.g., to schedule a locked G).
	//被线程 M 持有，并且正在执行用户代码或者调度器
	_Prunning

	// _Psyscall means a P is not running user code. It has
	// affinity to an M in a syscall but is not owned by it and
	// may be stolen by another M. This is similar to _Pidle but
	// uses lightweight transitions and maintains M affinity.
	//
	// Leaving _Psyscall must be done with a CAS, either to steal
	// or retake the P. Note that there's an ABA hazard: even if
	// an M successfully CASes its original P back to _Prunning
	// after a syscall, it must understand the P may have been
	// used by another M in the interim.
	//没有执行用户代码，当前线程陷入系统调用
	_Psyscall

	// _Pgcstop means a P is halted for STW and owned by the M
	// that stopped the world. The M that stopped the world
	// continues to use its P, even in _Pgcstop. Transitioning
	// from _Prunning to _Pgcstop causes an M to release its P and
	// park.
	//
	// The P retains its run queue and startTheWorld will restart
	// the scheduler on Ps with non-empty run queues.
	//被线程 M 持有，当前处理器由于垃圾回收被停止
	_Pgcstop

	// _Pdead means a P is no longer used (GOMAXPROCS shrank). We
	// reuse Ps if GOMAXPROCS increases. A dead P is mostly
	// stripped of its resources, though a few things remain
	// (e.g., trace buffers).
	//当前处理器已经不被使用
	_Pdead
)

// Mutual exclusion locks.  In the uncontended case,
// as fast as spin locks (just a few user-level instructions),
// but on the contention path they sleep in the kernel.
// A zeroed Mutex is unlocked (no need to initialize each lock).
// Initialization is helpful for static lock ranking, but not required.
type mutex struct{
	lockRankStruct
	// Futex-based impl treats it as uint32 key,
	// while sema-based impl as M* waitm.
	// Used to be a union, but unions break precise GC.
	key uintptr
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
type guintptr uintptr

//go:nosplit
func (gp guintptr) ptr() *g { return (*g)(unsafe.Pointer(gp)) }

//go:nosplit
func (gp *guintptr) set(g *g) { *gp = guintptr(unsafe.Pointer(g)) }

//go:nosplit
func (gp *guintptr) cas(old, new guintptr) bool {
	return atomic.Casuintptr((*uintptr)(unsafe.Pointer(gp)), uintptr(old), uintptr(new))
}

type puintptr uintptr

//go:nosplit
func (pp puintptr) ptr() *p { return (*p)(unsafe.Pointer(pp)) }

//go:nosplit
func (pp *puintptr) set(p *p) { *pp = puintptr(unsafe.Pointer(p)) }

// muintptr is a *m that is not tracked by the garbage collector.
//
// Because we do free Ms, there are some additional constrains on
// muintptrs:
//
// 1. Never hold an muintptr locally across a safe point.
//
// 2. Any muintptr in the heap must be owned by the M itself so it can
//    ensure it is not in use when the last true *m is released.
type muintptr uintptr

//go:nosplit
func (mp muintptr) ptr() *m { return (*m)(unsafe.Pointer(mp)) }

//go:nosplit
func (mp *muintptr) set(m *m) { *mp = muintptr(unsafe.Pointer(m)) }

// Stack describes a Go execution stack.
// The bounds of the stack are exactly [lo, hi),
// with no implicit data structures on either side.
//栈的范围是[lo,hi)
type stack struct {
	lo uintptr //低地址
	hi uintptr //高地址
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

	_panic *_painc // innermost panic - offset known to liblink 最内侧的painc结构体
	_defer *_defer // innermost defer 最内侧的延迟函数结构体
	m            *m      //当前绑定的m current m; offset known to arm liblink
	sched        gobuf //用于存储调度相关的信息
	atomicstatus uint32//go 运行的状态
	goid int64 // Goroutine的ID
	schedlink    guintptr //下一个链接的g构造成链表
}

type m struct{
	g0      *g     // goroutine with scheduling stack
	morebuf gobuf

	gsignal       *g           // signal-handling g
	tls           [6]uintptr   // thread-local storage (for x86 extern register)
	curg          *g       // current running goroutine
	p             puintptr // attached p for executing go code (nil if not executing go code)
	id int64
	throwing int32 //1有异常 0无异常
	locks int32 //统计锁的数量
	profilehz int32
	fastrand [2]uint32 //random
	alllink *m // on allm 等于allm
	schedlink     muintptr//和sched.midle形成链表，记录空闲的m
	nextwaitm     muintptr    //next m waiting for lock下一个等待锁的m

	// these are here because they are too large to be on the stack
	// of low-level NOSPLIT functions.
	//系统内核调用os.windows
	libcall libcall
	libcallpc uintptr //for cpu profiler
	libcallsp uintptr
	libcallg  guintptr //保存系统调用时的g
	syscall libcall

	mOS
}

type p struct{
	id int32
	status uint32 //one of pidle/prunning/.. p的状态
	link puintptr //与schedt.pidle形成空闲p的链表
	m muintptr //back-link to associated m (nil if idle)绑定的p

	// wbBuf is this P's GC write barrier buffer.
	//
	// TODO: Consider caching this in the running G.
	wbBuf wbBuf

	// Queue of runnable goroutines. Accessed without lock.
	runqhead uint32 //队列头部
	runqtail uint32 //队列尾部
	runq [256]guintptr //队列g
	// runnext, if non-nil, is a runnable G that was ready'd by
	// the current G and should be run next instead of what's in
	// runq if there's time remaining in the running G's time
	// slice. It will inherit the time left in the current time
	// slice. If a set of goroutines is locked in a
	// communicate-and-wait pattern, this schedules that set as a
	// unit and eliminates the (potentially large) scheduling
	// latency that otherwise arises from adding the ready'd
	// goroutines to the end of the run queue.
	runnext guintptr //优先队列g

	gFree struct{
		gList
		n int32
	}
}

type schedt struct{

	lock mutex

	// When increasing nmidle, nmidlelocked, nmsys, or nmfreed, be
	// sure to call checkdead().

	midle muintptr //idle m's waiting for work 空闲的m链表
	nmidle int32//number of idle m's waiting for work 空闲的m的数量
	mnext int64 //number of m's that have been created and next M ID 下一个m的id
	maxmcount    int32 //maximum number of m's allowed (or die) 设置m的最大数量
	nmfreed int64 //cumulative number of freed m's 累计释放的m的数量


	pidle puintptr //idle p's 空闲的p链表,下一个链接的p在p.link中取出
	npidle uint32 //空闲的p数量

	// Global runnable queue. 全局的g队列
	runq gQueue
	runqsize int32

	// Global cache of dead G's.
	gFree struct{
		lock mutex
		stack gList // Gs with stacks
		noStack gList // Gs without stacks
		n int32
	}

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
	sp   uintptr //栈指针
	pc   uintptr //程序计数器
	g    guintptr //持有runtime.gobuf的Goroutine
	ctxt unsafe.Pointer
	ret  sys.Uintreg //系统调用的返回值
	lr   uintptr
	bp   uintptr // for GOEXPERIMENT=framepointer
}

type funcval struct{

}

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

// extendRandom extends the random numbers in r[:n] to the whole slice r.
// Treats n<0 as n==0.
func extendRandom(r []byte, n int) {
	if n < 0 {
		n = 0
	}
	for n < len(r) {
		// Extend random bits using hash function & time seed
		w := n
		if w > 16 {
			w = 16
		}
		h := memhash(unsafe.Pointer(&r[n-w]), uintptr(nanotime()), uintptr(w))
		for i := 0; i < sys.PtrSize && n < len(r); i++ {
			r[n] = byte(h)
			n++
			h >>= 8
		}
	}
}

// A _defer holds an entry on the list of deferred calls.
// If you add a field here, add code to clear it in freedefer and deferProcStack
// This struct must match the code in cmd/compile/internal/gc/reflect.go:deferstruct
// and cmd/compile/internal/gc/ssa.go:(*state).call.
// Some defers will be allocated on the stack and some on the heap.
// All defers are logically part of the stack, so write barriers to
// initialize them are not required. All defers must be manually scanned,
// and for heap defers, marked.
type _defer struct{
	link *_defer
}

// A _panic holds information about an active panic.
//
// This is marked go:notinheap because _panic values must only ever
// live on the stack.
//
// The argp and link fields are stack pointers, but don't need special
// handling during stack growth: because they are pointer-typed and
// _panic values only live on the stack, regular stack pointer
// adjustment takes care of them.
type _painc struct{
	link *_painc
}

var(
	allm *m //m相当于是链表,通过m.alllink链接起来
	allp       []*p  // len(allp) == gomaxprocs; may change at safe points, otherwise immutable p的数量,一般与gomaxprocs相等
	allpLock   mutex // Protects P-less reads of allp and all writes 操作allp的锁
	ncpu int32 //cpu的核数
	gomaxprocs int32 //当前最大的p的数量
	sched schedt

	// Information about what cpu features are available.
	// Packages outside the runtime should not use these
	// as they are not an external api.
	// Set on startup in asm_{386,amd64}.s
	//在asm_{386,amd64}.s/rt0.go被设置
	processorVersionInfo uint32
	isIntel bool
	lfenceBeforeRdtsc bool
)
