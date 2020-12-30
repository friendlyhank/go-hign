package runtime

import (
	"internal/cpu"
	"runtime/internal/atomic"
	"runtime/internal/sys"
	"unsafe"
)

const (
	// go115NewMCentralImpl is a feature flag for the new mcentral implementation.
	//
	// This flag depends on go115NewMarkrootSpans because the new mcentral
	// implementation requires that markroot spans no longer rely on mgcsweepbufs.
	// The definition of this flag helps ensure that if there's a problem with
	// the new markroot spans implementation and it gets turned off, that the new
	// mcentral implementation also gets turned off so the runtime isn't broken.
	//表示大于1.15的新版本
	go115NewMCentralImpl = true && go115NewMarkrootSpans
)

// Main malloc heap.
// The heap itself is the "free" and "scav" treaps,
// but all the other global data is here too.
//
// mheap must not be heap-allocated because it contains mSpanLists,
// which must not be heap-allocated.
//
//go:notinheap
type mheap struct{
	// lock must only be acquired on the system stack, otherwise a g
	// could self-deadlock if its stack grows with the lock held.
	lock      mutex
	pages     pageAlloc //全局的页分配器page allocation data structure
	sweepgen uint32 //gc扫描状态 sweep generation, see comment in mspan; written during STW
	sweepdone uint32    //是否正在扫描 all spans are swept

	// allspans is a slice of all mspans ever created. Each mspan
	// appears exactly once.
	//
	// The memory for allspans is manually managed and can be
	// reallocated and move as the heap grows.
	//
	// In general, allspans is protected by mheap_.lock, which
	// prevents concurrent access as well as freeing the backing
	// store. Accesses during STW might not hold the lock, but
	// must ensure that allocation cannot happen around the
	// access (since that may free the backing store).
	allspans []*mspan // all spans out there

	// sweepSpans contains two mspan stacks: one of swept in-use
	// spans, and one of unswept in-use spans. These two trade
	// roles on each GC cycle. Since the sweepgen increases by 2
	// on each cycle, this means the swept spans are in
	// sweepSpans[sweepgen/2%2] and the unswept spans are in
	// sweepSpans[1-sweepgen/2%2]. Sweeping pops spans from the
	// unswept stack and pushes spans that are still in-use on the
	// swept stack. Likewise, allocating an in-use span pushes it
	// on the swept stack.
	//
	// For !go115NewMCentralImpl.
	sweepSpans [2]gcSweepBuf

	// arenas is the heap arena map. It points to the metadata for
	// the heap for every arena frame of the entire usable virtual
	// address space.
	//
	// Use arenaIndex to compute indexes into this array.
	//
	// For regions of the address space that are not backed by the
	// Go heap, the arena map contains nil.
	//
	// Modifications are protected by mheap_.lock. Reads can be
	// performed without locking; however, a given entry can
	// transition from nil to non-nil at any time when the lock
	// isn't held. (Entries never transitions back to nil.)
	//
	// In general, this is a two-level mapping consisting of an L1
	// map and possibly many L2 maps. This saves space when there
	// are a huge number of arena frames. However, on many
	// platforms (even 64-bit), arenaL1Bits is 0, making this
	// effectively a single-level map. In this case, arenas[0]
	// will never be nil.
	//二维矩阵管理的内存可以是不连续的
	//对于任意一个地址,我们可以根据area的基址计算该地址所在的页数并通过spans数组
	//获得管理该片内存的管理单元
	arenas [1 << arenaL1Bits]*[1 << arenaL2Bits]*heapArena

	// _ uint32 // ensure 64-bit alignment of central

	// central free lists for small size classes.
	// the padding makes sure that the mcentrals are
	// spaced CacheLinePadSize bytes apart, so that each mcentral.lock
	// gets its own cache line.
	// central is indexed by spanClass.
	//134个中心缓存
	central [numSpanClasses]struct {
		mcentral mcentral
		pad      [cpu.CacheLinePadSize - unsafe.Sizeof(mcentral{})%cpu.CacheLinePadSize]byte
	}

	spanalloc             fixalloc //mspan固定缓存块 allocator for span*
	cachealloc            fixalloc //mcache固定缓存分配 allocator for mcache*
	specialfinalizeralloc fixalloc // allocator for specialfinalizer*
	specialprofilealloc   fixalloc // allocator for specialprofile*
	speciallock           mutex    // lock for special record allocators.
	arenaHintAlloc        fixalloc // allocator for arenaHints
}

var mheap_ mheap

// A heapArena stores metadata for a heap arena. heapArenas are stored
// outside of the Go heap and accessed via the mheap_.arenas index.
////稀疏的内存管理，管理着所有的内存
//go:notinheap
type heapArena struct{
	// bitmap stores the pointer/scalar bitmap for the words in
	// this arena. See mbitmap.go for a description. Use the
	// heapBits type to access this.
	//用于表示arena区域那些地址保存了对象
	bitmap [heapArenaBitmapBytes]byte

	// spans maps from virtual address page ID within this arena to *mspan.
	// For allocated spans, their pages map to the span itself.
	// For free spans, only the lowest and highest pages map to the span itself.
	// Internal pages map to an arbitrary span.
	// For pages that have never been allocated, spans entries are nil.
	//
	// Modifications are protected by mheap.lock. Reads can be
	// performed without locking, but ONLY from indexes that are
	// known to contain in-use or stack spans. This means there
	// must not be a safe-point between establishing that an
	// address is live and looking it up in the spans array.
	//内存管理的单元,每个内存单元会管理几页的内存空间
	spans [pagesPerArena]*mspan

	// zeroedBase marks the first byte of the first page in this
	// arena which hasn't been used yet and is therefore already
	// zero. zeroedBase is relative to the arena base.
	// Increases monotonically until it hits heapArenaBytes.
	//
	// This field is sufficient to determine if an allocation
	// needs to be zeroed because the page allocator follows an
	// address-ordered first-fit policy.
	//
	// Read atomically and written with an atomic CAS.
	zeroedBase uintptr //指向的是基地址
}

// arenaHint is a hint for where to grow the heap arenas. See
// mheap_.arenaHints.
//
//go:notinheap
type arenaHint struct {
}

	// An mspan is a run of pages.
//
// When a mspan is in the heap free treap, state == mSpanFree
// and heapmap(s->start) == span, heapmap(s->start+s->npages-1) == span.
// If the mspan is in the heap scav treap, then in addition to the
// above scavenged == true. scavenged == false in all other cases.
//
// When a mspan is allocated, state == mSpanInUse or mSpanManual
// and heapmap(i) == span for all s->start <= i < s->start+s->npages.

// Every mspan is in one doubly-linked list, either in the mheap's
// busy list or one of the mcentral's span lists.

// An mspan representing actual memory has state mSpanInUse,
// mSpanManual, or mSpanFree. Transitions between these states are
// constrained as follows:
//
// * A span may transition from free to in-use or manual during any GC
//   phase.
//
// * During sweeping (gcphase == _GCoff), a span may transition from
//   in-use to free (as a result of sweeping) or manual to free (as a
//   result of stacks being freed).
//
// * During GC (gcphase != _GCoff), a span *must not* transition from
//   manual or in-use to free. Because concurrent GC may read a pointer
//   and then look up its span, the span state must be monotonic.
//
// Setting mspan.state to mSpanInUse or mSpanManual must be done
// atomically and only after all other span fields are valid.
// Likewise, if inspecting a span is contingent on it being
// mSpanInUse, the state should be loaded atomically and checked
// before depending on other fields. This allows the garbage collector
// to safely deal with potentially invalid pointers, since resolving
// such pointers may race with a span being allocated.
type mSpanState uint8 //mspan的状态

const (
	mSpanDead   mSpanState = iota
	mSpanInUse             // allocated for garbage collected heap
	mSpanManual            // allocated for manual management (e.g., stack allocator)
)

// mSpanStateNames are the names of the span states, indexed by
// mSpanState.
var mSpanStateNames = []string{
	"mSpanDead",
	"mSpanInUse",
	"mSpanManual",
	"mSpanFree",
}

// mSpanStateBox holds an mSpanState and provides atomic operations on
// it. This is a separate type to disallow accidental comparison or
// assignment with mSpanState.
type mSpanStateBox struct {
	s mSpanState
}

func (b *mSpanStateBox) set(s mSpanState) {
	atomic.Store8((*uint8)(&b.s), uint8(s))
}

func (b *mSpanStateBox) get() mSpanState {
	return mSpanState(atomic.Load8((*uint8)(&b.s)))
}

// mSpanList heads a linked list of spans.
//链成链表的mSpanList
//go:notinheap
type mSpanList struct{
	first *mspan
	last *mspan
}

//内存的基本管理单元
//go:notinheap
type mspan struct {
	next *mspan // next span in list, or nil if none
	prev *mspan  // previous span in list, or nil if none
	list *mSpanList // For debugging. TODO: Remove.

	startAddr uintptr //起试的地址 address of first byte of span aka s.base()
	npages    uintptr //包含页数 number of pages in span

	// freeindex is the slot index between 0 and nelems at which to begin scanning
	// for the next free object in this span.
	// Each allocation scans allocBits starting at freeindex until it encounters a 0
	// indicating a free object. freeindex is then adjusted so that subsequent scans begin
	// just past the newly discovered free object.
	//
	// If freeindex == nelem, this span has no free objects.
	//
	// allocBits is a bitmap of objects in this span.
	// If n >= freeindex and allocBits[n/8] & (1<<(n%8)) is 0
	// then object n is free;
	// otherwise, object n is allocated. Bits starting at nelem are
	// undefined and should never be referenced.
	//
	// Object n starts at address n*elemsize + (start << pageShift).
	freeindex uintptr //扫描页中空闲对象的初始索引

	// TODO: Look up nelems from sizeclass and remove this field if it
	// helps performance.
	nelems uintptr //有多少对象在mspan number of object in the span.

	// Cache of the allocBits at freeindex. allocCache is shifted
	// such that the lowest bit corresponds to the bit freeindex.
	// allocCache holds the complement of allocBits, thus allowing
	// ctz (count trailing zero) to use it directly.
	// allocCache may contain bits beyond s.nelems; the caller must ignore
	// these.
	allocCache uint64 //allocBits的补码,可以快速查找内存中未被使用的内存

	// allocBits and gcmarkBits hold pointers to a span's mark and
	// allocation bits. The pointers are 8 byte aligned.
	// There are three arenas where this data is held.
	// free: Dirty arenas that are no longer accessed
	//       and can be reused.
	// next: Holds information to be used in the next GC cycle.
	// current: Information being used during this GC cycle.
	// previous: Information being used during the last GC cycle.
	// A new GC cycle starts with the call to finishsweep_m.
	// finishsweep_m moves the previous arena to the free arena,
	// the current arena to the previous arena, and
	// the next arena to the current arena.
	// The next arena is populated as the spans request
	// memory to hold gcmarkBits for the next GC cycle as well
	// as allocBits for newly allocated spans.
	//
	// The pointer arithmetic is done "by hand" instead of using
	// arrays to avoid bounds checks along critical performance
	// paths.
	// The sweep will free the old allocBits and set allocBits to the
	// gcmarkBits. The gcmarkBits are replaced with a fresh zeroed
	// out memory.
	//分别用于内存的占用和回收情况
	allocBits  *gcBits
	gcmarkBits *gcBits

	// sweep generation:
	// if sweepgen == h->sweepgen - 2, the span needs sweeping 这个span等待扫描
	// if sweepgen == h->sweepgen - 1, the span is currently being swept 这个span正在被扫描
	// if sweepgen == h->sweepgen, the span is swept and ready to use 这个span被扫描过了,等待使用
	// if sweepgen == h->sweepgen + 1, the span was cached before sweep began and is still cached, and needs sweeping 这个span在扫描前已经缓存了对象，需要扫描
	// if sweepgen == h->sweepgen + 3, the span was swept and then cached and is still cached 这个span已经扫描而且缓存了对象
	// h->sweepgen is incremented by 2 after every GC

	sweepgen    uint32

	allocCount  uint16        //已分配的数量 number of allocated objects
	spanclass   spanClass     //sizeclasses.go size class and noscan (uint8)
	state mSpanStateBox //当前的状态 mSpanInUse etc; accessed atomically (get/set methods)
	needzero    uint8         // needs to be zeroed before allocation
	elemsize    uintptr       //元素的大小(spanclass) computed from sizeclass or from npages
}

func (s *mspan)base()uintptr{
	return s.startAddr
}

// recordspan adds a newly allocated span to h.allspans.
//
// This only happens the first time a span is allocated from
// mheap.spanalloc (it is not called when a span is reused).
//
// Write barriers are disallowed here because it can be called from
// gcWork when allocating new workbufs. However, because it's an
// indirect call from the fixalloc initializer, the compiler can't see
// this.
//记录新分配的mspan到mheap.allspans
//go:nowritebarrierrec
func recordspan(vh unsafe.Pointer, p unsafe.Pointer) {
	h :=(*mheap)(vh)
	s :=(*mspan)(p) //向系统新申请的内存mspan
	//实际上就是切片操作,需要扩容
	if len(h.allspans) >= cap(h.allspans){
		n := 64 * 1024 / sys.PtrSize
		if n < cap(h.allspans)*3/2{
			n = cap(h.allspans) *3 /2
		}
		var new []*mspan
		sp := (*slice)(unsafe.Pointer(&new))
		//向系统分配内存
		sp.array = sysAlloc(uintptr(n)*sys.PtrSize,&memstats.other_sys)
		if sp.array == nil{
			throw("runtime: cannot allocate memory")
		}
		sp.len = len(h.allspans)
		sp.cap = n
		if len(h.allspans) > 0{
			//复制切片
			copy(new,h.allspans)
		}
		oldAllspans :=h.allspans
		*(*notInHeapSlice)(unsafe.Pointer(&h.allspans)) = *(*notInHeapSlice)(unsafe.Pointer(&new))
		//将旧的切片释放
		if len(oldAllspans) != 0{
			sysFree(unsafe.Pointer(&oldAllspans[0]),uintptr(cap(oldAllspans))*unsafe.Sizeof(oldAllspans[0]),&memstats.other_sys)
		}
	}
	h.allspans = h.allspans[:len(h.allspans)+1]
	h.allspans[len(h.allspans)-1] =s
}

// Initialize the heap.
func (h *mheap)init(){
	lockInit(&h.lock, lockRankMheap)
	lockInit(&h.sweepSpans[0].spineLock,lockRankSpine)
	lockInit(&h.sweepSpans[1].spineLock, lockRankSpine)
	lockInit(&h.speciallock,lockRankMheapSpecial)

	h.spanalloc.init(unsafe.Sizeof(mspan{}),recordspan,unsafe.Pointer(h),&memstats.mspan_sys)
	h.cachealloc.init(unsafe.Sizeof(mcache{}),nil,nil,&memstats.mcache_sys)//线程缓存的初始化
	h.specialfinalizeralloc.init(unsafe.Sizeof(specialfinalizer{}),nil,nil,&memstats.other_sys)
	h.specialprofilealloc.init(unsafe.Sizeof(specialprofile{}), nil, nil, &memstats.other_sys)
	h.arenaHintAlloc.init(unsafe.Sizeof(arenaHint{}), nil, nil, &memstats.other_sys)

	//中心缓存初始化
	for i := range h.central {
		h.central[i].mcentral.init(spanClass(i))
	}

	//初始化全局的页分配器
	h.pages.init(&h.lock,&memstats.gc_sys)
}

// reclaim sweeps and reclaims at least npage pages into the heap.
// It is called before allocating npage pages to keep growth in check.
//
// reclaim implements the page-reclaimer half of the sweeper.
//
// h must NOT be locked.
//回收一部分的内存
func (h *mheap) reclaim(npage uintptr){

}

// alloc allocates a new span of npage pages from the GC'd heap.
//
// spanclass indicates the span's size class and scannability.
//
// If needzero is true, the memory for the returned span will be zeroed.
//mheap分配新的内存管理单元
func (h *mheap) alloc(npages uintptr, spanclass spanClass, needzero bool) *mspan {
	// Don't do any operations that lock the heap on the G stack.
	// It might trigger stack growth, and the stack growth code needs
	// to be able to allocate heap.
	var s *mspan
	systemstack(func() {
		// To prevent excessive heap growth, before allocating n pages
		// we need to sweep and reclaim at least n pages.
		//如果没有在扫描
		if h.sweepdone == 0{
			//回收一部分内存
			h.reclaim(npages)
		}
		s = h.allocSpan(npages,false,spanclass,&memstats.heap_inuse)
	})

	if s != nil{
		//是否需要清零操作
		if needzero && s.needzero != 0 {
			memclrNoHeapPointers(unsafe.Pointer(s.base()), s.npages<<_PageShift)
		}
		s.needzero = 0
	}
	return s
}

// allocManual allocates a manually-managed span of npage pages.
// allocManual returns nil if allocation fails.
//
// allocManual adds the bytes used to *stat, which should be a
// memstats in-use field. Unlike allocations in the GC'd heap, the
// allocation does *not* count toward heap_inuse or heap_sys.
//
// The memory backing the returned span may not be zeroed if
// span.needzero is set.
//
// allocManual must be called on the system stack because it may
// acquire the heap lock via allocSpan. See mheap for details.
//在systemstack中调用
//go:systemstack
func (h *mheap) allocManual(npages uintptr, stat *uint64) *mspan {
	return h.allocSpan(npages,true,0,stat)
}

// tryAllocMSpan attempts to allocate an mspan object from
// the P-local cache, but may fail.
//
// h need not be locked.
//
// This caller must ensure that its P won't change underneath
// it during this function. Currently to ensure that we enforce
// that the function is run on the system stack, because that's
// the only place it is used now. In the future, this requirement
// may be relaxed if its use is necessary elsewhere.
//
//go:systemstack
func (h *mheap)tryAllocMSpan()*mspan{
	pp := getg().m.p.ptr()
	// If we don't have a p or the cache is empty, we can't do
	// anything here.

}

// allocSpan allocates an mspan which owns npages worth of memory.
//
// If manual == false, allocSpan allocates a heap span of class spanclass
// and updates heap accounting. If manual == true, allocSpan allocates a
// manually-managed span (spanclass is ignored), and the caller is
// responsible for any accounting related to its use of the span. Either
// way, allocSpan will atomically add the bytes in the newly allocated
// span to *sysStat.
//
// The returned span is fully initialized.
//
// h must not be locked.
//
// allocSpan must be called on the system stack both because it acquires
// the heap lock and because it must block GC transitions.
//在systemstack中调用,分配新的内存管理单元,manual==true会按照spanclasss去分配
//go:systemstack
func (h *mheap) allocSpan(npages uintptr, manual bool, spanclass spanClass, sysStat *uint64) (s *mspan) {
	// Function-global state.
	gp := getg()
	base, scav := uintptr(0), uintptr(0)

	// If the allocation is small enough, try the page cache!
	//如果申请的内存比较小,获取申请的内存的处理器并通过页缓存去分配
	pp := gp.m.p.ptr()
	if pp != nil && npages < pageCachePages/4{
		c :=&pp.pcache

		// If the cache is empty, refill it.
		//如何页缓存是空，则向全局分配器中申请一些
		if c.empty(){
			lock(&h.lock)
			*c = h.pages.allocToCache()
			unlock(&h.lock)
		}

		// Try to allocate from the cache.
		base, scav = c.alloc(npages)
	}
}

// Initialize an empty doubly-linked list.
func (list *mSpanList) init() {
	list.first = nil
	list.last =  nil
}

//链表操作,删除某个mspan节点
func (list *mSpanList)remove(span *mspan){
	if span.list != list {
		print("runtime: failed mSpanList.remove span.npages=", span.npages,
			" span=", span, " prev=", span.prev, " span.list=", span.list, " list=", list, "\n")
		throw("mSpanList.remove")
	}
	if list.first ==span{
		list.first = span.next
	}else{
		span.prev.next = span.next
	}
	if list.last == span{
		list.last  = span.prev
	}else{
		span.next.prev = span.prev
	}
	span.next = nil
	span.prev = nil
	span.list = nil
}

func (list *mSpanList)isEmpty()bool{
	return list.first == nil
}

//链表操作,插入某个mspan节点
func (list *mSpanList)insert(span *mspan){
	if span.next != nil || span.prev != nil || span.list != nil {
		println("runtime: failed mSpanList.insert", span, span.next, span.prev, span.list)
		throw("mSpanList.insert")
	}
	span.next  = list.first
	if list.first != nil{
		// The list contains at least one span; link it in.
		// The last span in the list doesn't change.
		list.first.prev = span
	}else{
		// The list contains no spans, so this is also the last span.
		list.last = span
	}
	list.first  = span
	span.list  = list
}

func (list *mSpanList) insertBack(span *mspan) {
	if span.next != nil || span.prev != nil || span.list != nil {
		println("runtime: failed mSpanList.insertBack", span, span.next, span.prev, span.list)
		throw("mSpanList.insertBack")
	}
	span.prev = list.last
	if list.last != nil {
		// The list contains at least one span.
		list.last.next = span
	} else {
		// The list contains no spans, so this is also the first span.
		list.first = span
	}
	list.last = span
	span.list = list
}

// takeAll removes all spans from other and inserts them at the front
// of list.
func (list *mSpanList) takeAll(other *mSpanList) {
	if other.isEmpty() {
		return
	}

	// Reparent everything in other to list.
	for s := other.first; s != nil; s = s.next {
		s.list = list
	}

	// Concatenate the lists.
	if list.isEmpty() {
		*list = *other
	} else {
		// Neither list is empty. Put other before list.
		other.last.next = list.first
		list.first.prev = other.last
		list.first = other.first
	}

	other.first, other.last = nil, nil
}

// A spanClass represents the size class and noscan-ness of a span.
//
// Each size class has a noscan spanClass and a scan spanClass. The
// noscan spanClass contains only noscan objects, which do not contain
// pointers and thus do not need to be scanned by the garbage
// collector.
type spanClass uint8

const (
	numSpanClasses = _NumSizeClasses << 1
	tinySpanClass  = spanClass(tinySizeClass<<1 | 1) //微小对象spanclass
)

func makeSpanClass(sizeclass uint8, noscan bool) spanClass {
	return spanClass(sizeclass<<1) | spanClass(bool2int(noscan))
}

func (sc spanClass) sizeclass() int8{
	return int8(sc >> 1)
}

// The described object has a finalizer set for it.
//
// specialfinalizer is allocated from non-GC'd memory, so any heap
// pointers must be specially handled.
//
//go:notinheap
type specialfinalizer struct {
}

// The described object is being heap profiled.
//
//go:notinheap
type specialprofile struct {}

// gcBits is an alloc/mark bitmap. This is always used as *gcBits.
//
//go:notinheap
type gcBits uint8