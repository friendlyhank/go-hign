package runtime

// Per-thread (in Go, per-P) cache for small objects.
// No locking needed because it is per-thread (per-P).
//
// mcaches are allocated from non-GC'd memory, so any heap pointers
// must be specially handled.
//线程的缓存
//go:notinheap
type mcache struct{
	stackcache [_NumStackOrders]stackfreelist //栈缓存 与全局栈缓存stackpool相比减少了锁竞争影响

	alloc [numSpanClasses]*mspan // spans to allocate from, indexed by spanClass

	// flushGen indicates the sweepgen during which this mcache
	// was last flushed. If flushGen != mheap_.sweepgen, the spans
	// in this mcache are stale and need to the flushed so they
	// can be swept. This is done in acquirep.
	flushGen uint32
}

// A gclinkptr is a pointer to a gclink, but it is opaque
// to the garbage collector.
type gclinkptr uintptr

type stackfreelist struct{
	list gclinkptr// linked list of free stacks
	size uintptr // total size of stacks in list
}

//初始化分配mcache
func allocmcache()*mcache{
	var c *mcache
	systemstack(func() {

	})
	return c
}

func (c *mcache)releaseAll(){
	for i := range c.alloc {

	}
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
}
