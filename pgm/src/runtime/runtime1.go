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
		i,i1 float32
		j,j1 float64
		k unsafe.Pointer
		l *uint16
		m [4]byte
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

	if unsafe.Sizeof(a) !=1{

	}
	if unsafe.Sizeof(b) != 1{

	}
	if unsafe.Sizeof(c) != 2{

	}
	if unsafe.Sizeof(d) != 2{

	}
	if unsafe.Sizeof(e) != 4{

	}
	if unsafe.Sizeof(f) != 4{

	}
	if unsafe.Sizeof(g) != 8{

	}
	if unsafe.Sizeof(h) != 8{

	}
	if unsafe.Sizeof(i) != 4{

	}
	if unsafe.Sizeof(j) != 8{

	}
	if unsafe.Sizeof(k) != sys.PtrSize{

	}
	if unsafe.Sizeof(l) != sys.PtrSize{

	}
	if unsafe.Sizeof(x1) != 1{

	}
	if unsafe.Offsetof(y1.y) != 1{

	}
	if unsafe.Sizeof(y1) != 2{

	}
}