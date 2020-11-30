// Copyright 2020 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// +build goexperiment.staticlockranking

package runtime


type lockRankStruct struct {
	// static lock ranking of the lock
	rank lockRank
	// pad field to make sure lockRankStruct is a multiple of 8 bytes, even on
	// 32-bit systems.
	pad int
}