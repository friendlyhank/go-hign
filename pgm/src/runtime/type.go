// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Runtime type representation.

package runtime

import "unsafe"

// tflag is documented in reflect/type.go.
//
// tflag values must be kept in sync with copies in:
//	cmd/compile/internal/gc/reflect.go
//	cmd/link/internal/ld/decodesym.go
//	reflect/type.go
//      internal/reflectlite/type.go
type tflag uint8

// Needs to be in sync with ../cmd/link/internal/ld/decodesym.go:/^func.commonsize,
// ../cmd/compile/internal/gc/reflect.go:/^func.dcommontype and
// ../reflect/type.go:/^type.rtype.
// ../internal/reflectlite/type.go:/^type.rtype.
//表示某种数据类型
type _type struct {
	size       uintptr //占用的字节大小
	ptrdata uintptr //指针数据 size of memory prefix holding all pointers
	hash       uint32
	tflag      tflag //额外的标记信息
	align      uint8 //内存对齐系数
	fieldAlign uint8 //字段内存对齐系数
	kind uint8
	// function for comparing objects of this type
	// (ptr to object A, ptr to object B) -> ==?
	equal func(unsafe.Pointer, unsafe.Pointer) bool//用于判断当前类型多个对象是否相等
	str       nameOff //名字偏移量
	ptrToThis typeOff //指针的偏移量
}

// reflectOffs holds type offsets defined at run time by the reflect package.
//
// When a type is defined at run time, its *rtype data lives on the heap.
// There are a wide range of possible addresses the heap may use, that
// may not be representable as a 32-bit offset. Moreover the GC may
// one day start moving heap memory, in which case there is no stable
// offset that can be defined.
//
// To provide stable offsets, we add pin *rtype objects in a global map
// and treat the offset as an identifier. We use negative offsets that
// do not overlap with any compile-time module offsets.
//
// Entries are created by reflect.addReflectOff.
var reflectOffs struct {
	lock mutex
}

type imethod struct {}

type interfacetype struct {}

type maptype struct {
	typ _type
	key *_type
	elem *_type
	bucket *_type // internal type representing a hash bucket
	// function for hashing keys (ptr to key, seed) -> hash
	hasher     func(unsafe.Pointer, uintptr) uintptr
	keysize uint8 //key的大小 size of key slot
	elemsize uint8 //元素的大小 size of elem slot
	bucketsize uint16 //桶的大小 size of bucket
	flags uint32
}

// Note: flag values must match those used in the TMAP case
// in ../cmd/compile/internal/gc/reflect.go:dtypesym.
//判断key值是否指针
func (mt *maptype) indirectkey() bool { // store ptr to key instead of key itself
	return mt.flags&1 != 0
}

//判断元素是否指针
func (mt *maptype) indirectelem() bool { // store ptr to elem instead of elem itself
	return mt.flags&2 != 0
}

func (mt *maptype)reflexivekey()bool{// true if k==k for all keys
	return mt.flags&8 != 0
}

//判断key值是否需要重写
func (mt *maptype) needkeyupdate() bool { // true if we need to update key on an overwrite
	return mt.flags&8 != 0
}

type arraytype struct {}

type chantype struct {}

type slicetype struct {}

type functype struct {}

type ptrtype struct {}

type structfield struct {}

type structtype struct {}