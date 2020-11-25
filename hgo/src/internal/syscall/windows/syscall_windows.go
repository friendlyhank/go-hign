package windows

import (
	"internal/unsafeheader"
	"unicode/utf16"
	"unsafe"
)

// UTF16PtrToString is like UTF16ToString, but takes *uint16
// as a parameter instead of []uint16.
func UTF16PtrToString(p *uint16)string{
	if p == nil{
		return ""
	}
	// Find NUL terminator.
	end :=unsafe.Pointer(p)
	n := 0
	for *(*uint16)(end) != 0 {
		end = unsafe.Pointer(uintptr(end) + unsafe.Sizeof(*p))
		n++
	}
	// Turn *uint16 into []uint16.
	var s []uint16
	hdr := (*unsafeheader.Slice)(unsafe.Pointer(&s))
	hdr.Data = unsafe.Pointer(p)
	hdr.Cap = n
	hdr.Len = n
	// Decode []uint16 into string.
	return string(utf16.Decode(s))
}
