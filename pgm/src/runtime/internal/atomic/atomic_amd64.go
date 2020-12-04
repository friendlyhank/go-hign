package atomic

import "unsafe"

//runtime/internal/atomic/asm_amd64.s
// Export some functions via linkname to assembly in sync/atomic.
//go:linkname Load
//go:linkname Loadp
//go:linkname Load64

//go:nosplit
//go:noinline
func Load(ptr *uint32) uint32 {
	return *ptr
}

//go:nosplit
//go:noinline
func Loadp(ptr unsafe.Pointer) unsafe.Pointer {
	return *(*unsafe.Pointer)(ptr)
}

//go:noescape
func Xadd(ptr *uint32, delta int32) uint32

//go:nosplit
//go:noinline
func LoadAcq(ptr *uint32) uint32 {
	return *ptr
}

//go:noescape
func CasRel(ptr *uint32, old, new uint32) bool

//go:noescape
func StoreRel(ptr *uint32, val uint32)

