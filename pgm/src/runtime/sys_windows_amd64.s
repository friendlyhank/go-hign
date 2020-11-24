// Copyright 2011 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

#include "go_asm.h"
#include "go_tls.h"
#include "textflag.h"

// set tls base to DI
TEXT runtime·settls(SB),NOSPLIT,$0
	MOVQ	DI, 0x28(GS)
	RET
