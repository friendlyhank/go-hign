// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package runtime

import (
	"runtime/internal/sys"
	"unsafe"
)

//asm_amd64.s_rt0_go
func args(c int32,v **byte){
}

func environ()[]string{
	return envs
}

//asm_amd64.s_rt0_go 数据类型检查
func check(){
	var(
		a int8
		b uint8
		c int16
		d uint16
		e int32
		f uint32
		g int64
		h uint64
		//i,i1 float32
		//j,j1 float64
		i float32
		j float64
		k unsafe.Pointer
		l *uint16
		//m [4]byte
	)
	type x1t struct{
		x uint8
	}
	type y1t struct{
		x1 x1t
		y uint8
	}
	var x1 x1t
	var y1 y1t

	//不同数据类型占用的字节数
	if unsafe.Sizeof(a) !=1{
		throw("bad a")
	}
	if unsafe.Sizeof(b) != 1{
		throw("bad b")
	}
	if unsafe.Sizeof(c) != 2{
		throw("bad c")
	}
	if unsafe.Sizeof(d) != 2{
		throw("bad d")
	}
	if unsafe.Sizeof(e) != 4{
		throw("bad e")
	}
	if unsafe.Sizeof(f) != 4{
		throw("bad f")
	}
	if unsafe.Sizeof(g) != 8{
		throw("bad g")
	}
	if unsafe.Sizeof(h) != 8{
		throw("bad h")
	}
	if unsafe.Sizeof(i) != 4{
		throw("bad i")
	}
	if unsafe.Sizeof(j) != 8{
		throw("bad j")
	}
	if unsafe.Sizeof(k) != sys.PtrSize{
		throw("bad k")
	}
	if unsafe.Sizeof(l) != sys.PtrSize{
		throw("bad l")
	}
	if unsafe.Sizeof(x1) != 1{
		throw("bad unsafe.Sizeof x1")
	}
	if unsafe.Offsetof(y1.y) != 1{
		throw("bad offsetof y1.y")
	}
	if unsafe.Sizeof(y1) != 2{
		throw("bad unsafe.Sizeof y1")
	}
}

// Holds variables parsed from GODEBUG env var,
// except for "memprofilerate" since there is an
// existing int var for that value, which may
// already have an initial value.
var debug struct {
	allocfreetrace     int32
	cgocheck           int32
	clobberfree        int32
	efence             int32
	gccheckmark        int32
	gcpacertrace       int32
	gcshrinkstackoff   int32
	gcstoptheworld     int32
	gctrace            int32
	invalidptr         int32
	madvdontneed       int32 // for Linux; issue 28466
	sbrk               int32
	scavenge           int32
	scavtrace          int32
	scheddetail        int32
	schedtrace         int32
	tracebackancestors int32
	asyncpreemptoff    int32
}

// Poor mans 64-bit division.
// This is a very special function, do not use it if you are not sure what you are doing.
// int64 division is lowered into _divv() call on 386, which does not fit into nosplit functions.
// Handles overflow in a time-specific manner.
// This keeps us within no-split stack limits on 32-bit processors.
//go:nosplit
func timediv(v int64, div int32, rem *int32) int32 {
	res := int32(0)
	for bit := 30; bit >= 0; bit-- {
		if v >= int64(div)<<uint(bit) {
			v = v - (int64(div) << uint(bit))
			// Before this for loop, res was 0, thus all these
			// power of 2 increments are now just bitsets.
			res |= 1 << uint(bit)
		}
	}
	if v >= int64(div) {
		if rem != nil {
			*rem = 0
		}
		return 0x7fffffff
	}
	if rem != nil {
		*rem = int32(v)
	}
	return res
}