// Copyright 2016 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package runtime

import "unsafe"

// A gcSweepBuf is a set of *mspans.
//
// gcSweepBuf is safe for concurrent push operations *or* concurrent
// pop operations, but not both simultaneously.
type gcSweepBuf struct {
	// A gcSweepBuf is a two-level data structure consisting of a
	// growable spine that points to fixed-sized blocks. The spine
	// can be accessed without locks, but adding a block or
	// growing it requires taking the spine lock.
	//
	// Because each mspan covers at least 8K of heap and takes at
	// most 8 bytes in the gcSweepBuf, the growth of the spine is
	// quite limited.
	//
	// The spine and all blocks are allocated off-heap, which
	// allows this to be used in the memory manager and avoids the
	// need for write barriers on all of these. We never release
	// this memory because there could be concurrent lock-free
	// access and we're likely to reuse it anyway. (In principle,
	// we could do this during STW.)

	spineLock mutex
	spine     unsafe.Pointer // *[N]*gcSweepBlock, accessed atomically
	spineLen  uintptr        // Spine array length, accessed atomically
	spineCap  uintptr        // Spine array cap, accessed under lock

	// index is the first unused slot in the logical concatenation
	// of all blocks. It is accessed atomically.
	index uint32
}
