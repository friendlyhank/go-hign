package runtime

import "runtime/internal/atomic"

// Central list of free objects of a given size.
//
//go:notinheap
type mcentral struct {
	lock mutex
	spanclass spanClass

	//旧版本,小于1.15
	nonempty mSpanList //空闲得mspan链表 list of spans with a free object, ie a nonempty free list
	empty    mSpanList //不是空闲得mspan链表 list of spans with no free objects (or cached in an mcache)

	// partial and full contain two mspan sets: one of swept in-use
	// spans, and one of unswept in-use spans. These two trade
	// roles on each GC cycle. The unswept set is drained either by
	// allocation or by the background sweeper in every GC cycle,
	// so only two roles are necessary.
	//
	// sweepgen is increased by 2 on each GC cycle, so the swept
	// spans are in partial[sweepgen/2%2] and the unswept spans are in
	// partial[1-sweepgen/2%2]. Sweeping pops spans from the
	// unswept set and pushes spans that are still in-use on the
	// swept set. Likewise, allocating an in-use span pushes it
	// on the swept set.
	//
	// Some parts of the sweeper can sweep arbitrary spans, and hence
	// can't remove them from the unswept set, but will add the span
	// to the appropriate swept list. As a result, the parts of the
	// sweeper and mcentral that do consume from the unswept list may
	// encounter swept spans, and these should be ignored.
	//新版本,大于1.15的结构体
	partial [2]spanSet //空闲得mspan链表 list of spans with a free object
	full    [2]spanSet //不是空闲得mspan链表 list of spans with no free objects

	// nmalloc is the cumulative count of objects allocated from
	// this mcentral, assuming all spans in mcaches are
	// fully-allocated. Written atomically, read under STW.
	nmalloc uint64 //已分配的数量
}

// Initialize a single central free list.
//初始化中心缓存
func (c *mcentral) init(spc spanClass) {
	c.spanclass = spc
	if go115NewMCentralImpl{
		lockInit(&c.partial[0].spineLock, lockRankSpanSetSpine)
		lockInit(&c.partial[1].spineLock, lockRankSpanSetSpine)
		lockInit(&c.full[0].spineLock, lockRankSpanSetSpine)
		lockInit(&c.full[1].spineLock, lockRankSpanSetSpine)
	}else{
		c.nonempty.init()
		c.empty.init()
		lockInit(&c.lock,lockRankMcentral)
	}
}

// partialUnswept returns the spanSet which holds partially-filled
// unswept spans for this sweepgen.
func (c *mcentral) partialUnswept(sweepgen uint32) *spanSet{
	return &c.partial[1-sweepgen/2%2]
}

// partialSwept returns the spanSet which holds partially-filled
// swept spans for this sweepgen.
func (c *mcentral)partialSwept(sweepgen uint32)*spanSet{
	return &c.full[1-sweepgen/2%2]
}

// fullUnswept returns the spanSet which holds unswept spans without any
// free slots for this sweepgen.
func (c *mcentral) fullUnswept(sweepgen uint32) *spanSet {
	return &c.full[1-sweepgen/2%2]
}

// fullSwept returns the spanSet which holds swept spans without any
// free slots for this sweepgen.
func (c *mcentral) fullSwept(sweepgen uint32) *spanSet {
	return &c.full[sweepgen/2%2]
}

// Allocate a span to use in an mcache.
//中心缓存分配一个mspan给mcache线程缓存
func(c *mcentral) cacheSpan() *mspan {
	//版本兼容,不是go115NewMCentralImpl走旧的缓存分配
	if !go115NewMCentralImpl {
		return c.oldCacheSpan()
	}
	// Deduct credit for this span allocation and sweep if necessary.
	spanBytes := uintptr(class_to_allocnpages[c.spanclass.sizeclass()]) * _PageSize

	sg :=mheap_.sweepgen

	// If we sweep spanBudget spans without finding any free
	// space, just allocate a fresh span. This limits the amount
	// of time we can spend trying to find free space and
	// amortizes the cost of small object sweeping over the
	// benefit of having a full free span to allocate from. By
	// setting this to 100, we limit the space overhead to 1%.
	//
	// TODO(austin,mknyszek): This still has bad worst-case
	// throughput. For example, this could find just one free slot
	// on the 100th swept span. That limits allocation latency, but
	// still has very poor throughput. We could instead keep a
	// running free-to-used budget and switch to fresh span
	// allocation if the budget runs low.
	spanBudget := 100

	var s *mspan

	//从有空闲的对象中查找可以使用的内存管理单元
	// Try partial swept spans first.
	if s = c.partialSwept(sg).pop();s != nil{
		goto havespan
	}

	// Now try partial unswept spans.
	for ;spanBudget >= 0;spanBudget--{
		s = c.partialUnswept(sg).pop()
		if s == nil{
			break
		}
		//等待扫描
		if atomic.Load(&s.sweepgen) == sg - 2 && atomic.Cas(&s.sweepgen,sg-2,sg-1){
			goto havespan
		}
	}
	// Now try full unswept spans, sweeping them and putting them into the
	// right list if we fail to get a span.
	//从没有空闲的对象中查找可以使用的内存管理单元
	for ;spanBudget >= 0;spanBudget--{
		s = c.fullUnswept(sg).pop()
		if s == nil{
			break
		}
		if atomic.Load(&s.sweepgen) == sg - 2 && atomic.Cas(&s.sweepgen,sg -2,sg - 1){

		}
	}

	// We failed to get a span from the mcentral so get one from mheap.
	//从堆中申请新的内存管理单元
	s = c.grow()

	// At this point s is a span that should have free slots.
havespan:
	n := int(s.nelems) - int(s.allocCount)
	if n == 0 ||s.freeindex == s.nelems || uintptr(s.allocCount) == s.nelems{
		throw("span has no free objects")
	}
	// Assume all objects from this span will be allocated in the
	// mcache. If it gets uncached, we'll adjust this.
	atomic.Xadd64(&c.nmalloc,int64(n))
	usedBytes := uintptr(s.allocCount) * s.elemsize
	atomic.Xadd64(&memstats.heap_live, int64(spanBytes)-int64(usedBytes))

	// Adjust the allocCache so that s.freeindex corresponds to the low bit in
	// s.allocCache.
	s.allocCache >>= s.freeindex % 64

	return s
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

//中心缓存内存不足,从mheap中申请空间
func (c *mcentral) grow() *mspan {
	//计算跨度类需要待分配的页数
	npages := uintptr(class_to_allocnpages[c.spanclass.sizeclass()])
	size := uintptr(class_to_size[c.spanclass.sizeclass()])

	s := mheap_.alloc(npages,c.spanclass,true)
	if s == nil{
		return nil
	}
}