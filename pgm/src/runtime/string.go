// Copyright 2014 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package runtime

import (
	"internal/bytealg"
	"unsafe"
)

// Variant with *byte pointer type for DWARF debugging.
type stringStructDWARF struct {}

//go:nosplit
func gostringnocopy(str *byte) string {
	ss := stringStruct{str: unsafe.Pointer(str), len: findnull(str)}
	s := *(*string)(unsafe.Pointer(&ss))
	return s
}

type stringStruct struct {
	str unsafe.Pointer
	len int
}

func hasPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}

func index(s, t string) int {
	if len(t) == 0 {
		return 0
	}
	for i := 0; i < len(s); i++ {
		if s[i] == t[0] && hasPrefix(s[i:], t) {
			return i
		}
	}
	return -1
}

func contains(s, t string) bool {
	return index(s, t) >= 0
}

//go:nosplit
func findnull(s *byte) int {
	if s == nil {
		return 0
	}

	// Avoid IndexByteString on Plan 9 because it uses SSE instructions
	// on x86 machines, and those are classified as floating point instructions,
	// which are illegal in a note handler.
	if GOOS == "plan9" {
		p := (*[maxAlloc/2 - 1]byte)(unsafe.Pointer(s))
		l := 0
		for p[l] != 0 {
			l++
		}
		return l
	}

	// pageSize is the unit we scan at a time looking for NULL.
	// It must be the minimum page size for any architecture Go
	// runs on. It's okay (just a minor performance loss) if the
	// actual system page size is larger than this value.
	const pageSize = 4096

	offset := 0
	ptr := unsafe.Pointer(s)
	// IndexByteString uses wide reads, so we need to be careful
	// with page boundaries. Call IndexByteString on
	// [ptr, endOfPage) interval.
	safeLen := int(pageSize - uintptr(ptr)%pageSize)

	for {
		t := *(*string)(unsafe.Pointer(&stringStruct{ptr, safeLen}))
		// Check one page at a time.
		if i := bytealg.IndexByteString(t, 0); i != -1 {
			return offset + i
		}
		// Move to next page
		ptr = unsafe.Pointer(uintptr(ptr) + uintptr(safeLen))
		offset += safeLen
		safeLen = pageSize
	}
}