package atomic

//go:nosplit
//go:noinline
func LoadAcq(ptr *uint32) uint32 {
	return *ptr
}

//go:noescape
func StoreRel(ptr *uint32, val uint32)
