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
