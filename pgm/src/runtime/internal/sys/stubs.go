// Copyright 2014 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package sys

// Declarations for runtime services implemented in C or assembly.

const PtrSize = 4 << (^uintptr(0) >> 63)   //unsafe.Sizeof(uintptr(0)) but an ideal const uintptr数据类型的占用的字节

var DefaultGoroot string // set at link time

// AIX requires a larger stack for syscalls.
const StackGuardMultiplier = StackGuardMultiplierDefault*(1-GoosAix) + 2*GoosAix
