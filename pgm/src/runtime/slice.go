// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package runtime

import (
	"runtime/internal/math"
	"runtime/internal/sys"
	"unsafe"
)

//切片就是动态数组
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
		newcap = newcap + newcap
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

	var overflow bool
	var lenmem,newlenmem,capmem uintptr
	/*
	 *lenmem表示旧切片实际元素长度所占的内存空间大小
	 *newlenmem表示新切片实际元素长度所占的内存空间大小
	 *capmem表示扩容之后的容量大小
	 *overflow是否溢出
	 */
	// Specialize for common values of et.size.
	// For 1 we don't need any division/multiplication.
	// For sys.PtrSize, compiler will optimize division/multiplication into a shift by a constant.
	// For powers of 2, use a variable shift.
	switch{
	case et.size == 1://元素所占的字节数为1
		lenmem = uintptr(old.len)
		newlenmem = uintptr(cap)
		capmem = roundupsize(uintptr(newcap))//向上取整分配内存
		overflow = uintptr(newcap) > maxAlloc
		newcap = int(capmem)
	case et.size == sys.PtrSize://元素所占的字节数为8个字节
		lenmem = uintptr(old.len) * sys.PtrSize
		newlenmem = uintptr(cap) * sys.PtrSize
		capmem = roundupsize(uintptr(newcap) * sys.PtrSize)
		overflow = uintptr(newcap) > maxAlloc/sys.PtrSize
		newcap = int(capmem / sys.PtrSize)
	case isPowerOfTwo(et.size)://元素所占的字节数为2的倍数
		var shift uintptr
		//根据元素的字节数计算出位运算系数
		if sys.PtrSize == 8 {
			// Mask shift for better code generation.
			shift = uintptr(sys.Ctz64(uint64(et.size))) & 63
		} else {
			shift = uintptr(sys.Ctz32(uint32(et.size))) & 31
		}
		//计算内存空间用位运算
		lenmem = uintptr(old.len) << shift
		newlenmem = uintptr(cap) << shift
		capmem = roundupsize(uintptr(newcap) << shift)
		overflow = uintptr(newcap) > (maxAlloc >> shift)
		newcap = int(capmem >> shift)

	default:
		lenmem = uintptr(old.len) * et.size
		newlenmem = uintptr(cap) * et.size
		capmem, overflow = math.MulUintptr(et.size, uintptr(newcap))
		capmem = roundupsize(capmem)
		newcap = int(capmem / et.size)
	}

	// The check of overflow in addition to capmem > maxAlloc is needed
	// to prevent an overflow which can be used to trigger a segfault
	// on 32bit architectures with this example program:
	//
	// type T [1<<27 + 1]int64
	//
	// var d T
	// var s []T
	//
	// func main() {
	//   s = append(s, d, d, d, d)
	//   print(len(s), "\n")
	// }

	if overflow || capmem > maxAlloc {
	}

	var p unsafe.Pointer
	//如果元素不是指针
	if et.ptrdata == 0{
		//申请一块无类型的内存空间
		p = mallocgc(capmem, nil, false)
		// The append() that calls growslice is going to overwrite from old.len to cap (which will be the new length).
		// Only clear the part that will not be overwritten.
		//将超出切片当前长度的位置进行初始化
		memclrNoHeapPointers(add(p, newlenmem), capmem-newlenmem)
	}else{
		// Note: can't use rawmem (which avoids zeroing of memory), because then GC can scan uninitialized memory.
		p = mallocgc(capmem,et,true)
	}
	//将旧切片的值考入新的切片
	memmove(p, old.array, lenmem)

	return slice{p,old.len,newcap}
}

//是否为2的倍数
func isPowerOfTwo(x uintptr)bool{
	return x&(x-1) == 0
}