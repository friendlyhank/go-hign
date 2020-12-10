package runtime

// Central list of free objects of a given size.
//
//go:notinheap
type mcentral struct {
	lock mutex
	spanclass spanClass

	// For !go115NewMCentralImpl.
	nonempty mSpanList // list of spans with a free object, ie a nonempty free list
	empty    mSpanList // list of spans with no free objects (or cached in an mcache)

	// nmalloc is the cumulative count of objects allocated from
	// this mcentral, assuming all spans in mcaches are
	// fully-allocated. Written atomically, read under STW.
	nmalloc uint64 //已分配的数量
}

// Allocate a span to use in an mcache.
//分配一个mspan给mcache线程缓存
func(c *mcentral) cacheSpan() *mspan {
	//版本兼容,不是go115NewMCentralImpl走旧的缓存分配
	if !go115NewMCentralImpl {
		return c.oldCacheSpan()
	}
	return nil
}

func (c *mcentral)oldCacheSpan()*mspan{
	return nil
}

// Return span from an mcache.
//
// s must have a span class corresponding to this
// mcentral and it must not be empty.
//中心缓存回收mcache现成的mspan
func (c *mcentral) uncacheSpan(s *mspan) {

}