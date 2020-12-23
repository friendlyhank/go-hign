package runtime

import (
	"runtime/internal/atomic"
	"unsafe"
)

// Per-thread (in Go, per-P) cache for small objects.
// No locking needed because it is per-thread (per-P).
//
// mcaches are allocated from non-GC'd memory, so any heap pointers
// must be specially handled.
//线程的缓存
//go:notinheap
type mcache struct{
	// Allocator cache for tiny objects w/o pointers.
	// See "Tiny allocator" comment in malloc.go.

	// tiny points to the beginning of the current tiny block, or
	// nil if there is no current tiny block.
	//
	// tiny is a heap pointer. Since mcache is in non-GC'd memory,
	// we handle it by clearing it in releaseAll during mark
	// termination.
	tiny             uintptr//微小的对象
	tinyoffset       uintptr//下一个空闲内存所在的偏移量
	local_tinyallocs uintptr //记录内存分配器中分配的对象个数 number of tiny allocs not counted in other stats

	alloc [numSpanClasses]*mspan //每个缓存可以持有67(spanclasses)*2个runtime.span spans to allocate from, indexed by spanClass

	stackcache [_NumStackOrders]stackfreelist //栈缓存 与全局栈缓存stackpool相比减少了锁竞争影响

	// flushGen indicates the sweepgen during which this mcache
	// was last flushed. If flushGen != mheap_.sweepgen, the spans
	// in this mcache are stale and need to the flushed so they
	// can be swept. This is done in acquirep.
	flushGen uint32
}

// A gclink is a node in a linked list of blocks, like mlink,
// but it is opaque to the garbage collector.
// The GC does not trace the pointers during collection,
// and the compiler does not emit write barriers for assignments
// of gclinkptr values. Code should store references to gclinks
// as gclinkptr, not as *gclink.
type gclink struct{
	next gclinkptr
}

// A gclinkptr is a pointer to a gclink, but it is opaque
// to the garbage collector.
type gclinkptr uintptr

// ptr returns the *gclink form of p.
// The result should be used for accessing fields, not stored
// in other data structures.
func (p gclinkptr)ptr()*gclink{
	return (*gclink)(unsafe.Pointer(p))
}

type stackfreelist struct{
	list gclinkptr// linked list of free stacks
	size uintptr // total size of stacks in list
}

// dummy mspan that contains no free objects.
var emptymspan mspan

//初始化分配mcache线程缓存
func allocmcache()*mcache{
	var c *mcache
	systemstack(func() {
		lock(&mheap_.lock)
		//初始化向mheap_.cachealloc申请内存
		c = (*mcache)(mheap_.cachealloc.alloc())
		c.flushGen  = mheap_.sweepgen
		unlock(&mheap_.lock)
	})
	for i := range c.alloc{
		c.alloc[i] =&emptymspan
	}
	return c
}

//释放mcache线程缓存(p销毁的时候需要释放)
func freemcache(c *mcache){
	systemstack(func() {
		c.releaseAll()
		stackcache_clear(c)

		// NOTE(rsc,rlh): If gcworkbuffree comes back, we need to coordinate
		// with the stealing of gcworkbufs during garbage collection to avoid
		// a race where the workbuf is double-freed.
		// gcworkbuffree(c.gcworkbuf)
		lock(&mheap_.lock)
		mheap_.cachealloc.free(unsafe.Pointer(c))
		unlock(&mheap_.lock)
	})
}

// refill acquires a new span of span class spc for c. This span will
// have at least one free object. The current span in c must be full.
//
// Must run in a non-preemptible context since otherwise the owner of
// c could change.
//线程已经满了,向中心缓存中申请内存
func (c *mcache) refill(spc spanClass) {
	// Return the current cached span to the central lists.
	s := c.alloc[spc]

	//没满
	if uintptr(s.allocCount) != s.nelems {
		throw("refill of span with free space remaining")
	}

	if s != &emptymspan {
		// Mark this span as no longer cached.
		if s.sweepgen != mheap_.sweepgen+3 {
			throw("bad sweepgen in refill")
		}
		//版本兼容 表示大于1.15版本
		if go115NewMCentralImpl {
			mheap_.central[spc].mcentral.uncacheSpan(s)
		} else {
			atomic.Store(&s.sweepgen, mheap_.sweepgen)
		}
	}

	// Get a new cached span from the central lists.
	//先中心缓存申请内存
	s = mheap_.central[spc].mcentral.cacheSpan()
	if s == nil {
		throw("out of memory")
	}

	if uintptr(s.allocCount) == s.nelems{
		throw("span has no free space")
	}

	// Indicate that this span is cached and prevent asynchronous
	// sweeping in the next sweep phase.
	s.sweepgen = mheap_.sweepgen + 3

	c.alloc[spc] = s
}

func (c *mcache)releaseAll(){
	for i := range c.alloc {
		s := c.alloc[i]
		if s != &emptymspan{
			//不是空的emptyspan,进行回收
			mheap_.central[i].mcentral.uncacheSpan(s)
			c.alloc[i] = &emptymspan
		}
	}
	// Clear tinyalloc pool.
	//微小对象清零
	c.tiny = 0
	c.tinyoffset = 0
}

// prepareForSweep flushes c if the system has entered a new sweep phase
// since c was populated. This must happen between the sweep phase
// starting and the first allocation from c.
func (c *mcache)prepareForSweep(){
	// Alternatively, instead of making sure we do this on every P
	// between starting the world and allocating on that P, we
	// could leave allocate-black on, allow allocation to continue
	// as usual, use a ragged barrier at the beginning of sweep to
	// ensure all cached spans are swept, and then disable
	// allocate-black. However, with this approach it's difficult
	// to avoid spilling mark bits into the *next* GC cycle.
	sg := mheap_.sweepgen
	if c.flushGen == sg{
		return
	}else if c.flushGen != sg -2{
		println("bad flushGen", c.flushGen, "in prepareForSweep; sweepgen", sg)
		throw("bad flushGen")
	}
	c.releaseAll()
	stackcache_clear(c)
}
