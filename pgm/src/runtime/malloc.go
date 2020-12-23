package runtime

import (
	"runtime/internal/atomic"
	"runtime/internal/sys"
	"unsafe"
)

const (
	debugMalloc = false

	maxTinySize   = _TinySize //微小对象
	tinySizeClass = _TinySizeClass
	maxSmallSize  = _MaxSmallSize //小对象

	pageShift = _PageShift
	pageSize  = _PageSize
	pageMask  = _PageMask
	// By construction, single page spans of the smallest object class
	// have the most objects per span.
	maxObjsPerSpan = pageSize / 8

	concurrentSweep = _ConcurrentSweep

	_PageSize = 1 << _PageShift
	_PageMask = _PageSize - 1

	// _64bit = 1 on 64-bit systems, 0 on 32-bit systems
	_64bit = 1 << (^uintptr(0) >> 63) / 2

	// Tiny allocator parameters, see "Tiny allocator" comment in malloc.go.
	_TinySize      = 16
	_TinySizeClass = int8(2)

	_FixAllocChunk = 16 << 10 // Chunk size for FixAlloc

	// Per-P, per order stack segment cache size.
	_StackCacheSize = 32 * 1024

	// Number of orders that get caching. Order 0 is FixedStack
	// and each successive order is twice as large.
	// We want to cache 2KB, 4KB, 8KB, and 16KB stacks. Larger stacks
	// will be allocated directly.
	// Since FixedStack is different on different systems, we
	// must vary NumStackOrders to keep the same maximum cached size.
	//   OS               | FixedStack | NumStackOrders
	//   -----------------+------------+---------------
	//   linux/darwin/bsd | 2KB        | 4
	//   windows/32       | 4KB        | 3
	//   windows/64       | 8KB        | 2
	//   plan9            | 4KB        | 3
	_NumStackOrders = 4 - sys.PtrSize/4*sys.GoosWindows - 1*sys.GoosPlan9

	// heapAddrBits is the number of bits in a heap address. On
	// amd64, addresses are sign-extended beyond heapAddrBits. On
	// other arches, they are zero-extended.
	//
	// On most 64-bit platforms, we limit this to 48 bits based on a
	// combination of hardware and OS limitations.
	//
	// amd64 hardware limits addresses to 48 bits, sign-extended
	// to 64 bits. Addresses where the top 16 bits are not either
	// all 0 or all 1 are "non-canonical" and invalid. Because of
	// these "negative" addresses, we offset addresses by 1<<47
	// (arenaBaseOffset) on amd64 before computing indexes into
	// the heap arenas index. In 2017, amd64 hardware added
	// support for 57 bit addresses; however, currently only Linux
	// supports this extension and the kernel will never choose an
	// address above 1<<47 unless mmap is called with a hint
	// address above 1<<47 (which we never do).
	//
	// arm64 hardware (as of ARMv8) limits user addresses to 48
	// bits, in the range [0, 1<<48).
	//
	// ppc64, mips64, and s390x support arbitrary 64 bit addresses
	// in hardware. On Linux, Go leans on stricter OS limits. Based
	// on Linux's processor.h, the user address space is limited as
	// follows on 64-bit architectures:
	//
	// Architecture  Name              Maximum Value (exclusive)
	// ---------------------------------------------------------------------
	// amd64         TASK_SIZE_MAX     0x007ffffffff000 (47 bit addresses)
	// arm64         TASK_SIZE_64      0x01000000000000 (48 bit addresses)
	// ppc64{,le}    TASK_SIZE_USER64  0x00400000000000 (46 bit addresses)
	// mips64{,le}   TASK_SIZE64       0x00010000000000 (40 bit addresses)
	// s390x         TASK_SIZE         1<<64 (64 bit addresses)
	//
	// These limits may increase over time, but are currently at
	// most 48 bits except on s390x. On all architectures, Linux
	// starts placing mmap'd regions at addresses that are
	// significantly below 48 bits, so even if it's possible to
	// exceed Go's 48 bit limit, it's extremely unlikely in
	// practice.
	//
	// On 32-bit platforms, we accept the full 32-bit address
	// space because doing so is cheap.
	// mips32 only has access to the low 2GB of virtual memory, so
	// we further limit it to 31 bits.
	//
	// On darwin/arm64, although 64-bit pointers are presumably
	// available, pointers are truncated to 33 bits. Furthermore,
	// only the top 4 GiB of the address space are actually available
	// to the application, but we allow the whole 33 bits anyway for
	// simplicity.
	// TODO(mknyszek): Consider limiting it to 32 bits and using
	// arenaBaseOffset to offset into the top 4 GiB.
	//
	// WebAssembly currently has a limit of 4GB linear memory.
	heapAddrBits = (_64bit*(1-sys.GoarchWasm)*(1-sys.GoosDarwin*sys.GoarchArm64))*48 + (1-_64bit+sys.GoarchWasm)*(32-(sys.GoarchMips+sys.GoarchMipsle)) + 33*sys.GoosDarwin*sys.GoarchArm64

	// maxAlloc is the maximum size of an allocation. On 64-bit,
	// it's theoretically possible to allocate 1<<heapAddrBits bytes. On
	// 32-bit, however, this is one less than 1<<32 because the
	// number of bytes in the address space doesn't actually fit
	// in a uintptr.
	maxAlloc = (1 << heapAddrBits) - (1-_64bit)*1

	// The number of bits in a heap address, the size of heap
	// arenas, and the L1 and L2 arena map sizes are related by
	//
	//   (1 << addr bits) = arena size * L1 entries * L2 entries
	//
	// Currently, we balance these as follows:
	//
	//       Platform  Addr bits  Arena size  L1 entries   L2 entries
	// --------------  ---------  ----------  ----------  -----------
	//       */64-bit         48        64MB           1    4M (32MB)
	// windows/64-bit         48         4MB          64    1M  (8MB)
	//       */32-bit         32         4MB           1  1024  (4KB)
	//     */mips(le)         31         4MB           1   512  (2KB)

	// heapArenaBytes is the size of a heap arena. The heap
	// consists of mappings of size heapArenaBytes, aligned to
	// heapArenaBytes. The initial heap mapping is one arena.
	//
	// This is currently 64MB on 64-bit non-Windows and 4MB on
	// 32-bit and on Windows. We use smaller arenas on Windows
	// because all committed memory is charged to the process,
	// even if it's not touched. Hence, for processes with small
	// heaps, the mapped arena space needs to be commensurate.
	// This is particularly important with the race detector,
	// since it significantly amplifies the cost of committed
	// memory.
	heapArenaBytes = 1 << logHeapArenaBytes

	// logHeapArenaBytes is log_2 of heapArenaBytes. For clarity,
	// prefer using heapArenaBytes where possible (we need the
	// constant to compute some other constants).
	logHeapArenaBytes = (6+20)*(_64bit*(1-sys.GoosWindows)*(1-sys.GoarchWasm)) + (2+20)*(_64bit*sys.GoosWindows) + (2+20)*(1-_64bit) + (2+20)*sys.GoarchWasm

	// heapArenaBitmapBytes is the size of each heap arena's bitmap.
	heapArenaBitmapBytes = heapArenaBytes / (sys.PtrSize * 8 / 2)

	pagesPerArena = heapArenaBytes / pageSize

	// arenaL1Bits is the number of bits of the arena number
	// covered by the first level arena map.
	//
	// This number should be small, since the first level arena
	// map requires PtrSize*(1<<arenaL1Bits) of space in the
	// binary's BSS. It can be zero, in which case the first level
	// index is effectively unused. There is a performance benefit
	// to this, since the generated code can be more efficient,
	// but comes at the cost of having a large L2 mapping.
	//
	// We use the L1 map on 64-bit Windows because the arena size
	// is small, but the address space is still 48 bits, and
	// there's a high cost to having a large L2.
	arenaL1Bits = 6 * (_64bit * sys.GoosWindows)

	// arenaL2Bits is the number of bits of the arena number
	// covered by the second level arena index.
	//
	// The size of each arena map allocation is proportional to
	// 1<<arenaL2Bits, so it's important that this not be too
	// large. 48 bits leads to 32MB arena index allocations, which
	// is about the practical threshold.
	arenaL2Bits = heapAddrBits - logHeapArenaBytes - arenaL1Bits

	// arenaL1Shift is the number of bits to shift an arena frame
	// number by to compute an index into the first level arena map.
	arenaL1Shift = arenaL2Bits

	// arenaBits is the total bits in a combined arena map index.
	// This is split between the index into the L1 arena map and
	// the L2 arena map.
	arenaBits = arenaL1Bits + arenaL2Bits

	// arenaBaseOffset is the pointer value that corresponds to
	// index 0 in the heap arena map.
	//
	// On amd64, the address space is 48 bits, sign extended to 64
	// bits. This offset lets us handle "negative" addresses (or
	// high addresses if viewed as unsigned).
	//
	// On aix/ppc64, this offset allows to keep the heapAddrBits to
	// 48. Otherwize, it would be 60 in order to handle mmap addresses
	// (in range 0x0a00000000000000 - 0x0afffffffffffff). But in this
	// case, the memory reserved in (s *pageAlloc).init for chunks
	// is causing important slowdowns.
	//
	// On other platforms, the user address space is contiguous
	// and starts at 0, so no offset is necessary.
	arenaBaseOffset = 0xffff800000000000*sys.GoarchAmd64 + 0x0a00000000000000*sys.GoosAix
	// A typed version of this constant that will make it into DWARF (for viewcore).
	arenaBaseOffsetUintptr = uintptr(arenaBaseOffset)

	// Max number of threads to run garbage collection.
	// 2, 3, and 4 are all plausible maximums depending
	// on the hardware details of the machine. The garbage
	// collector scales well to 32 cpus.
	_MaxGcproc = 32

	// minLegalPointer is the smallest possible legal pointer.
	// This is the smallest possible architectural page size,
	// since we assume that the first page is never mapped.
	//
	// This should agree with minZeroPage in the compiler.
	minLegalPointer uintptr = 4096
)

// physPageSize is the size in bytes of the OS's physical pages.
// Mapping and unmapping operations must be done at multiples of
// physPageSize.
//
// This must be set by the OS init code (typically in osinit) before
// mallocinit.
//虚拟内存的分页
var physPageSize uintptr

// OS memory management abstraction layer
//
// Regions of the address space managed by the runtime may be in one of four
// states at any given time:
// 1) None - Unreserved and unmapped, the default state of any region.
// 2) Reserved - Owned by the runtime, but accessing it would cause a fault.
//               Does not count against the process' memory footprint.
// 3) Prepared - Reserved, intended not to be backed by physical memory (though
//               an OS may implement this lazily). Can transition efficiently to
//               Ready. Accessing memory in such a region is undefined (may
//               fault, may give back unexpected zeroes, etc.).
// 4) Ready - may be accessed safely.
//
// This set of states is more than is strictly necessary to support all the
// currently supported platforms. One could get by with just None, Reserved, and
// Ready. However, the Prepared state gives us flexibility for performance
// purposes. For example, on POSIX-y operating systems, Reserved is usually a
// private anonymous mmap'd region with PROT_NONE set, and to transition
// to Ready would require setting PROT_READ|PROT_WRITE. However the
// underspecification of Prepared lets us use just MADV_FREE to transition from
// Ready to Prepared. Thus with the Prepared state we can set the permission
// bits just once early on, we can efficiently tell the OS that it's free to
// take pages away from us when we don't strictly need them.
//
// For each OS there is a common set of helpers defined that transition
// memory regions between these states. The helpers are as follows:
//
// sysAlloc transitions an OS-chosen region of memory from None to Ready.
// More specifically, it obtains a large chunk of zeroed memory from the
// operating system, typically on the order of a hundred kilobytes
// or a megabyte. This memory is always immediately available for use.
//
// sysFree transitions a memory region from any state to None. Therefore, it
// returns memory unconditionally. It is used if an out-of-memory error has been
// detected midway through an allocation or to carve out an aligned section of
// the address space. It is okay if sysFree is a no-op only if sysReserve always
// returns a memory region aligned to the heap allocator's alignment
// restrictions.
//
// sysReserve transitions a memory region from None to Reserved. It reserves
// address space in such a way that it would cause a fatal fault upon access
// (either via permissions or not committing the memory). Such a reservation is
// thus never backed by physical memory.
// If the pointer passed to it is non-nil, the caller wants the
// reservation there, but sysReserve can still choose another
// location if that one is unavailable.
// NOTE: sysReserve returns OS-aligned memory, but the heap allocator
// may use larger alignment, so the caller must be careful to realign the
// memory obtained by sysReserve.
//
// sysMap transitions a memory region from Reserved to Prepared. It ensures the
// memory region can be efficiently transitioned to Ready.
//
// sysUsed transitions a memory region from Prepared to Ready. It notifies the
// operating system that the memory region is needed and ensures that the region
// may be safely accessed. This is typically a no-op on systems that don't have
// an explicit commit step and hard over-commit limits, but is critical on
// Windows, for example.
//
// sysUnused transitions a memory region from Ready to Prepared. It notifies the
// operating system that the physical pages backing this memory region are no
// longer needed and can be reused for other purposes. The contents of a
// sysUnused memory region are considered forfeit and the region must not be
// accessed again until sysUsed is called.
//
// sysFault transitions a memory region from Ready or Prepared to Reserved. It
// marks a region such that it will always fault if accessed. Used only for
// debugging the runtime.
func mallocinit(){
	// Initialize the heap.
	//初始化mheap
	mheap_.init()
	//mcache0缓存在这里设置
	mcache0 =allocmcache()
}

//0字节的内存分配基础地址 base address for all 0-byte allocations
var zerobase uintptr

// Allocate an object of size bytes.
// Small objects are allocated from the per-P cache's free lists.
// Large objects (> 32 kB) are allocated straight from the heap.
//分配指定大小的对象
//如果是小对象直接在线程缓存分配
//如果是大的对象在堆(heap)上分配
func mallocgc(size uintptr, typ *_type, needzero bool) unsafe.Pointer {

	if size == 0{
		return unsafe.Pointer(&zerobase)
	}

	// Set mp.mallocing to keep from being preempted by GC.
	mp := acquirem()
	//如果当前的m正在执行分配任务，则抛出异常
	if mp.mallocing != 0{
		throw("malloc deadlock")
	}
	mp.mallocing = 1

	dataSize := size
	var c *mcache
	if mp.p != 0{
		c =mp.p.ptr().mcache
	}else{
		// We will be called without a P while bootstrapping,
		// in which case we use mcache0, which is set in mallocinit.
		// mcache0 is cleared when bootstrapping is complete,
		// by procresize.
		c = mcache0
		if c == nil{
			throw("malloc called with no P")
		}
	}
	var span *mspan
	var x unsafe.Pointer
	//是否需要gc扫描这个对象
	noscan := typ == nil || typ.ptrdata == 0
	//如果是小对象(小于32kb),在线程缓存中分配内存
	if size <= maxSmallSize{
		if noscan && size < maxTinySize{
			// Tiny allocator.
			//
			// Tiny allocator combines several tiny allocation requests
			// into a single memory block. The resulting memory block
			// is freed when all subobjects are unreachable. The subobjects
			// must be noscan (don't have pointers), this ensures that
			// the amount of potentially wasted memory is bounded.
			//
			// Size of the memory block used for combining (maxTinySize) is tunable.
			// Current setting is 16 bytes, which relates to 2x worst case memory
			// wastage (when all but one subobjects are unreachable).
			// 8 bytes would result in no wastage at all, but provides less
			// opportunities for combining.
			// 32 bytes provides more opportunities for combining,
			// but can lead to 4x worst case wastage.
			// The best case winning is 8x regardless of block size.
			//
			// Objects obtained from tiny allocator must not be freed explicitly.
			// So when an object will be freed explicitly, we ensure that
			// its size >= maxTinySize.
			//
			// SetFinalizer has a special case for objects potentially coming
			// from tiny allocator, it such case it allows to set finalizers
			// for an inner byte of a memory block.
			//
			// The main targets of tiny allocator are small strings and
			// standalone escaping variables. On a json benchmark
			// the allocator reduces number of allocations by ~12% and
			// reduces heap size by ~20%.
			off :=c.tinyoffset
			// Align tiny pointer for required (conservative) alignment.
			if size&7 == 0{
				off = alignUp(off,8)
			}else if size&3 == 0{
				off = alignUp(off,4)
			}else if size&1 == 0{
				off = alignUp(off,2)
			}
			if off+size <= maxTinySize && c.tiny != 0{
				// The object fits into existing tiny block.
				x  = unsafe.Pointer(c.tiny + off)
				c.tinyoffset  = off + size
				c.local_tinyallocs++ //对象个数增加
				mp.mallocing = 0
				releasem(mp)
				return x
			}
			//到了这里表示微小对象的分配已经满了,需要申请资源
			// Allocate a new maxTinySize block.
			//分配一块新的微小块
			span = c.alloc[tinySizeClass]
			v := nextFreeFast(span)
			if v == 0{
			}
		}
	}
}

// nextFreeFast returns the next free object if one is quickly available.
// Otherwise it returns 0.
//从mspan找到空闲的对象
func nextFreeFast(s *mspan) gclinkptr {
	theBit := sys.Ctz64(s.allocCache)// Is there a free object in the allocCache?
	//是否还有空闲
	if theBit < 64{
		result := s.freeindex + uintptr(theBit)
		if result < s.nelems{
			freeidx := result + 1
			if freeidx%64 == 0 && freeidx != s.nelems{
				return 0
			}
			s.allocCache >>= uint(theBit + 1)
			s.freeindex = freeidx
			s.allocCount++
			return gclinkptr(result*s.elemsize + s.base())
		}
	}
	return 0
}

// nextFree returns the next free object from the cached span if one is available.
// Otherwise it refills the cache with a span with an available object and
// returns that object along with a flag indicating that this was a heavy
// weight allocation. If it is a heavy weight allocation the caller must
// determine whether a new GC cycle needs to be started or if the GC is active
// whether this goroutine needs to assist the GC.
//
// Must run in a non-preemptible context since otherwise the owner of
// c could change.
//线程缓存获取下一个空闲的span
func (c *mcache) nextFree(spc spanClass) (v gclinkptr, s *mspan, shouldhelpgc bool) {
	s = c.alloc[spc]
	shouldhelpgc = false
}

type persistentAlloc struct {
	base *notInHeap//base基础地址
	off uintptr //内存的偏移量
}

var globalAlloc struct{
	mutex
	persistentAlloc
}

// persistentChunkSize is the number of bytes we allocate when we grow
// a persistentAlloc.
//设置persistentAlloc最大值为256kb
const persistentChunkSize = 256 << 10

// persistentChunks is a list of all the persistent chunks we have
// allocated. The list is maintained through the first word in the
// persistent chunk. This is updated atomically.
var persistentChunks *notInHeap

// //在系统栈上向系统申请内存 -
func persistentalloc(size,align uintptr,sysStat *uint64)unsafe.Pointer{
	var p *notInHeap
	systemstack(func() {
		p = persistentalloc1(size,align,sysStat)
	})
	return unsafe.Pointer(p)
}

// Must run on system stack because stack growth can (re)invoke it.
// See issue 9174.
//在系统栈上向系统申请内存
//go:systemstack
func persistentalloc1(size,align uintptr,sysStat *uint64)*notInHeap{
	const (
		maxBlock = 64 << 10 //最大块64kb VM reservation granularity is 64K on windows
	)

	if size == 0{
		throw("persistentalloc: size == 0")
	}
	if align != 0{

	}else{
		align = 8
	}
	if size >= maxBlock{
		//直接从系统中分配
		return (*notInHeap)(sysAlloc(size,sysStat))
	}

	mp := acquirem()
	var persistent *persistentAlloc
	if mp != nil && mp.p != 0{
		persistent = &mp.p.ptr().palloc
	}else{
		lock(&globalAlloc.mutex)
		persistent = &globalAlloc.persistentAlloc
	}
	persistent.off = alignUp(persistent.off,align)
	//如果persistent内存不足,则向系统申请内存
	if persistent.off+size > persistentChunkSize || persistent.base == nil{
		persistent.base = (*notInHeap)(sysAlloc(persistentChunkSize,&memstats.other_sys))
		if persistent.base == nil{
			if persistent == &globalAlloc.persistentAlloc{
				unlock(&globalAlloc.mutex)
			}
			throw("runtime: cannot allocate memory")
		}

		// Add the new chunk to the persistentChunks list.
		for{
			chunks := uintptr(unsafe.Pointer(persistentChunks))
			*(*uintptr)(unsafe.Pointer(persistent.base)) = chunks
			if atomic.Casuintptr((*uintptr)(unsafe.Pointer(&persistentChunks)), chunks, uintptr(unsafe.Pointer(persistent.base))) {
				break
			}
		}
		persistent.off =alignUp(sys.PtrSize,align)
	}
	//更新偏移量
	p := persistent.base.add(persistent.off)
	persistent.off += size
	releasem(mp)
	if persistent == &globalAlloc.persistentAlloc{
		unlock(&globalAlloc.mutex)
	}

	//统计相关信息
	if sysStat != &memstats.other_sys{
		mSysStatInc(sysStat, size)
		mSysStatDec(&memstats.other_sys, size)
	}
	return p
}


// notInHeap is off-heap memory allocated by a lower-level allocator
// like sysAlloc or persistentAlloc.
//
// In general, it's better to use real types marked as go:notinheap,
// but this serves as a generic type for situations where that isn't
// possible (like in the allocators).
//
// TODO: Use this as the return type of sysAlloc, persistentAlloc, etc?
//
//go:notinheap
type notInHeap struct{}

func (p *notInHeap)add(bytes uintptr)*notInHeap{
	return (*notInHeap)(unsafe.Pointer(uintptr(unsafe.Pointer(p)) + bytes))
}
