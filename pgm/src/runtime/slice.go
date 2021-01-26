// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package runtime

import (
	"runtime/internal/math"
	"unsafe"
)

//切片就是高级版的数组
type slice struct {
	array unsafe.Pointer
	len int
	cap int
}

// A notInHeapSlice is a slice backed by go:notinheap memory.
type notInHeapSlice struct{
	array *notInHeap
	len   int
	cap   int
}

// 调用make(slice,size)
func makeslice(et *_type, len, cap int) unsafe.Pointer {
	mem,overflow :=math.MulUintptr(et.size,uintptr(cap))
	//如果内存地址溢出或者是超出可分配的最大值
	if overflow || mem > maxAlloc || len < 0 || len > cap{

	}
	//分配内存空间
	return mallocgc(mem,et,true)
}