package runtime

// Per-thread (in Go, per-P) cache for small objects.
// No locking needed because it is per-thread (per-P).
//
// mcaches are allocated from non-GC'd memory, so any heap pointers
// must be specially handled.
//线程的缓存
//go:notinheap
type mcache struct{
	stackcache [_NumStackOrders]stackfreelist
}

// A gclinkptr is a pointer to a gclink, but it is opaque
// to the garbage collector.
type gclinkptr uintptr

//初始化分配mcache
func allocmcache()*mcache{
	var c *mcache
	systemstack(func() {

	})
	return c
}
