package atomic

//runtime/asm_amd64.s
//go:noescape
func Casuintptr(ptr *uintptr, old, new uintptr) bool
