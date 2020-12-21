// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package runtime

import "unsafe"

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