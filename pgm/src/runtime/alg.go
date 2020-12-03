package runtime

import "unsafe"

// in asm_*.s
func memhash(p unsafe.Pointer, h, s uintptr) uintptr
func memhash32(p unsafe.Pointer, h uintptr) uintptr
func memhash64(p unsafe.Pointer, h uintptr) uintptr
func strhash(p unsafe.Pointer, h uintptr) uintptr

func memequal0(p, q unsafe.Pointer) bool {
	return true
}
func memequal8(p, q unsafe.Pointer) bool {
	return *(*int8)(p) == *(*int8)(q)
}
func memequal16(p, q unsafe.Pointer) bool {
	return *(*int16)(p) == *(*int16)(q)
}
func memequal32(p, q unsafe.Pointer) bool {
	return *(*int32)(p) == *(*int32)(q)
}
func memequal64(p, q unsafe.Pointer) bool {
	return *(*int64)(p) == *(*int64)(q)
}
func memequal128(p, q unsafe.Pointer) bool {
	return *(*[2]int64)(p) == *(*[2]int64)(q)
}
func f32equal(p, q unsafe.Pointer) bool {
	return *(*float32)(p) == *(*float32)(q)
}
func f64equal(p, q unsafe.Pointer) bool {
	return *(*float64)(p) == *(*float64)(q)
}
func c64equal(p, q unsafe.Pointer) bool {
	return *(*complex64)(p) == *(*complex64)(q)
}
func c128equal(p, q unsafe.Pointer) bool {
	return *(*complex128)(p) == *(*complex128)(q)
}
func strequal(p, q unsafe.Pointer) bool {
	return *(*string)(p) == *(*string)(q)
}

// Testing adapters for hash quality tests (see hash_test.go)
func stringHash(s string, seed uintptr) uintptr {
	return strhash(noescape(unsafe.Pointer(&s)), seed)
}

func int32Hash(i uint32, seed uintptr) uintptr {
	return memhash32(noescape(unsafe.Pointer(&i)), seed)
}

func int64Hash(i uint64, seed uintptr) uintptr {
	return memhash64(noescape(unsafe.Pointer(&i)), seed)
}
