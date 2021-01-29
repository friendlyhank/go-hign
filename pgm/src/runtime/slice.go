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

// growslice handles slice growth during append.
// It is passed the slice element type, the old slice, and the desired new minimum capacity,
// and it returns a new slice with at least that capacity, with the old data
// copied into it.
// The new slice's length is set to the old slice's length,
// NOT to the new requested capacity.
// This is for codegen convenience. The old slice's length is used immediately
// to calculate where to write new values during an append.
// TODO: When the old backend is gone, reconsider this decision.
// The SSA backend might prefer the new length or to return only ptr/cap and save stack space.
func growslice(et *_type,old slice,cap int)slice{

	if cap < old.cap{

	}

	if et.size == 0{
		// append should not create a slice with nil pointer but non-zero len.
		// We assume that append doesn't need to preserve old.array in this case.
		return slice{unsafe.Pointer(&zerobase),old.len,cap}
	}

	newcap := old.cap
	doublecap :=newcap + newcap
	if cap > doublecap{
		newcap := newcap + newcap
	}else{
		// Check 0 < newcap to detect overflow
		// and prevent an infinite loop.
		for 0 <newcap && newcap < cap{
			newcap += newcap / 4
		}

		// Set newcap to the requested cap when
		// the newcap calculation overflowed.
		if newcap <= 0{
			newcap = cap
		}
	}
}