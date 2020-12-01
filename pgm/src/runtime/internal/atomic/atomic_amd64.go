package atomic

//runtime/internal/atomic/asm_amd64.s

//go:nosplit
//go:noinline
func LoadAcq(ptr *uint32) uint32 {
	return *ptr
}

//go:noescape
func CasRel(ptr *uint32, old, new uint32) bool

//go:noescape
func StoreRel(ptr *uint32, val uint32)

