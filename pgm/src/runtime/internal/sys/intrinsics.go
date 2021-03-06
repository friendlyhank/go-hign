// Copyright 2016 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// +build !386

// TODO finish intrinsifying 386, deadcode the assembly, remove build tags, merge w/ intrinsics_common
// TODO replace all uses of CtzXX with TrailingZerosXX; they are the same.

package sys

// Using techniques from http://supertech.csail.mit.edu/papers/debruijn.pdf

const deBruijn64ctz = 0x0218a392cd3d5dbf

var deBruijnIdx64ctz = [64]byte{
	0, 1, 2, 7, 3, 13, 8, 19,
	4, 25, 14, 28, 9, 34, 20, 40,
	5, 17, 26, 38, 15, 46, 29, 48,
	10, 31, 35, 54, 21, 50, 41, 57,
	63, 6, 12, 18, 24, 27, 33, 39,
	16, 37, 45, 47, 30, 53, 49, 56,
	62, 11, 23, 32, 36, 44, 52, 55,
	61, 22, 43, 51, 60, 42, 59, 58,
}

const deBruijn32ctz = 0x04653adf
var deBruijnIdx32ctz = [32]byte{
	0, 1, 2, 6, 3, 11, 7, 16,
	4, 14, 12, 21, 8, 23, 17, 26,
	31, 5, 10, 15, 13, 20, 22, 25,
	30, 9, 19, 24, 29, 18, 28, 27,
}

// Ctz64 counts trailing (low-order) zeroes,
// and if all are zero, then 64.
//可以在64位系统中将size转化为位运算
func Ctz64(x uint64) int {
	x &= -x                       // isolate low-order bit
	y := x * deBruijn64ctz >> 58  // extract part of deBruijn sequence
	i := int(deBruijnIdx64ctz[y]) // convert to bit index
	z := int((x - 1) >> 57 & 64)  // adjustment if zero
	return i + z
}

// Ctz32 counts trailing (low-order) zeroes,
// and if all are zero, then 32.
//可以在32位系统中将size转化为位运算
func Ctz32(x uint32) int {
	x &= -x                       // isolate low-order bit
	y := x * deBruijn32ctz >> 27  // extract part of deBruijn sequence
	i := int(deBruijnIdx32ctz[y]) // convert to bit index
	z := int((x - 1) >> 26 & 32)  // adjustment if zero
	return i + z
}
