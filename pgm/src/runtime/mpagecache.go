package runtime

import "unsafe"

//页缓存

const pageCachePages = 8 * unsafe.Sizeof(pageCache{}.cache)

// pageCache represents a per-p cache of pages the allocator can
// allocate from without a lock. More specifically, it represents
// a pageCachePages*pageSize chunk of memory with 0 or more free
// pages in it.
//页缓存
type pageCache struct {
	base  uintptr // base address of the chunk
	cache uint64  // 64-bit bitmap representing free pages (1 means free)
	scav  uint64  // 64-bit bitmap representing scavenged pages (1 means scavenged)
}

// empty returns true if the pageCache has any free pages, and false
// otherwise.
func (c *pageCache)empty()bool{
	return c.cache == 0
}

// alloc allocates npages from the page cache and is the main entry
// point for allocation.
//
// Returns a base address and the amount of scavenged memory in the
// allocated region in bytes.
//
// Returns a base address of zero on failure, in which case the
// amount of scavenged memory should be ignored.
//从pageCache按分页分配一块内存
func (c *pageCache)alloc(npages uintptr)(uintptr,uintptr){

}

// allocToCache acquires a pageCachePages-aligned chunk of free pages which
// may not be contiguous, and returns a pageCache structure which owns the
// chunk.
//
// s.mheapLock must be held.
//页缓存分配一块到pageCache
func (s *pageAlloc) allocToCache() pageCache {

}


