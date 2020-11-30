package atomic

//runtime/internal/atomic/asm_amd64.s


//go:noescape
func Casuintptr(ptr *uintptr, old, new uintptr) bool

//go:noescape
func Storeuintptr(ptr *uintptr, new uintptr)

//go:noescape
func Loaduintptr(ptr *uintptr) uintptr
