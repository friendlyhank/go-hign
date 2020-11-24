package bytealg

import "unsafe"

//go:linkname abigen_runtime_memequal_varlen runtime.memequal_varlen
func abigen_runtime_memequal_varlen(a, b unsafe.Pointer) bool
