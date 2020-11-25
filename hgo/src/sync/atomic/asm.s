// Copyright 2011 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// +build !race

#include "textflag.h"

TEXT ·LoadPointer(SB),NOSPLIT,$0
	JMP	runtime∕internal∕atomic·Loadp(SB)

TEXT ·StoreUintptr(SB),NOSPLIT,$0
	JMP	runtime∕internal∕atomic·Storeuintptr(SB)
