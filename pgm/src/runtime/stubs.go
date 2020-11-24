package runtime

import "unsafe"

// getg returns the pointer to the current g.
// The compiler rewrites calls to this function into instructions
// that fetch the g directly (from TLS or from the dedicated register).
func getg()*g


// in internal/bytealg/equal_*.s
//go:noescape
func memequal(a, b unsafe.Pointer, size uintptr) bool

func memequal_varlen(a, b unsafe.Pointer) bool