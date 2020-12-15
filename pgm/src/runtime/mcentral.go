package runtime

import "runtime/internal/atomic"

// Central list of free objects of a given size.
//
//go:notinheap
type mcentral struct {
	lock mutex
	spanclass spanClass

	// For !go115NewMCentralImpl.版本箭头
	nonempty mSpanList //空闲得mspan链表 list of spans with a free object, ie a nonempty free list
	empty    mSpanList //不是空闲得mspan链表 list of spans with no free objects (or cached in an mcache)

	// nmalloc is the cumulative count of objects allocated from
	// this mcentral, assuming all spans in mcaches are
	// fully-allocated. Written atomically, read under STW.
	nmalloc uint64 //已分配的数量
}

// Initialize a single central free list.
//初始化中心缓存
func (c *mcentral) init(spc spanClass) {

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
 	if !go115NewMCentralImpl{
		c.oldUncacheSpan(s)
		return
	}
}

// Return span from an mcache.
//
// For !go115NewMCentralImpl.
func (c *mcentral)oldUncacheSpan(s *mspan){
	if s.allocCount  == 0{
		throw("uncaching span but s.allocCount == 0")
	}

	//更新一下状态
	sg :=mheap_.sweepgen
	stale := s.sweepgen == sg+1
	if stale{
		// Span was cached before sweep began. It's our
		// responsibility to sweep it.
		//
		// Set sweepgen to indicate it's not cached but needs
		// sweeping and can't be allocated from. sweep will
		// set s.sweepgen to indicate s is swept.
		atomic.Store(&s.sweepgen,sg-1)
	}else{
		// Indicate that s is no longer cached.
		atomic.Store(&s.sweepgen,sg)
	}

	n := int(s.nelems) - int(s.allocCount)
	if n > 0{
		// cacheSpan updated alloc assuming all objects on s
		// were going to be allocated. Adjust for any that
		// weren't. We must do this before potentially
		// sweeping the span.
		atomic.Xadd64(&c.nmalloc,-int64(n))

		lock(&c.lock)
		c.empty.remove(s)
		c.nonempty.insert(s)
	}
}