// Copyright 2014 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package runtime

import (
	"runtime/internal/math"
	"unsafe"
)

const(
	maxAlign  = 8
	hchanSize = unsafe.Sizeof(hchan{}) + uintptr(-int(unsafe.Sizeof(hchan{}))&(maxAlign-1))
)

type hchan struct {
	qcount   uint           //channel中元素的个数 total data in the queue
	dataqsiz uint //channel中循环队列的长度 // size of the circular queue
	buf  unsafe.Pointer //channel的缓冲区数据指针 points to an array of dataqsiz elements
	elemsize uint16 //元素的大小
	closed uint32 //是否关闭状态
	elemtype *_type //元素的类型 element type
	sendx    uint   //发送的索引 send index
	recvx    uint   //接收的索引 receive index

	recvq    waitq  //等待接收goroutine队列列表 list of recv waiters
	sendq    waitq  //等待发送的goroutine队列列表 list of send waiters

	// lock protects all fields in hchan, as well as several
	// fields in sudogs blocked on this channel.
	//
	// Do not change another G's status while holding this lock
	// (in particular, do not ready a G), as this can deadlock
	// with stack shrinking.
	lock mutex //所以这里要注意channel也是用到锁的
}

type waitq struct {
	first *sudog
	last  *sudog
}

//channel初始化
func makechan(t *chantype,size int)*hchan{
	elem := t.elem

	mem,overflow :=math.MulUintptr(elem.size,uintptr(size))
	if overflow || mem > maxAlloc-hchanSize || size < 0 {
	}

	var c *hchan
	switch  {
	case mem == 0:
		// Queue or element size is zero.
		c = (*hchan)(mallocgc(hchanSize, nil, true))
		// Race detector uses this location for synchronization.
		c.buf = c.raceaddr()//无缓冲区
	case elem.ptrdata == 0://如果不是指针类型
		// Elements do not contain pointers.
		// Allocate hchan and buf in one call.
		c = (*hchan)(mallocgc(hchanSize+mem,nil,true))
		c.buf = add(unsafe.Pointer(c),hchanSize)
	default:
		//如果是指针
		// Elements contain pointers.
		c = new(hchan)
		c.buf = mallocgc(mem,elem,true)
	}

	c.elemsize = uint16(elem.size)
	c.elemtype = elem
	c.dataqsiz =uint(size)
	return c
}

// entry point for c <- x from compiled code
//go:nosplit
func chansend1(c *hchan, elem unsafe.Pointer) {
	chansend(c, elem, true, getcallerpc())
}

/*
 * generic single channel send/recv
 * If block is not nil,
 * then the protocol will not
 * sleep but return if it could
 * not complete.
 *
 * sleep can wake up with g.param == nil
 * when a channel involved in the sleep has
 * been closed.  it is easiest to loop and re-run
 * the operation; we'll see that it's now closed.
 */
//ep就是elem,block表示发送时阻塞的
func chansend(c *hchan, ep unsafe.Pointer, block bool, callerpc uintptr) bool {

	//chanel发送会锁定
	lock(&c.lock)

	//如果有等待的接收队列,那么直接发送
	if sg := c.recvq.dequeue();sg != nil{
		// Found a waiting receiver. We pass the value we want to send
		// directly to the receiver, bypassing the channel buffer (if any).
		send(c, sg, ep, func() { unlock(&c.lock) }, 3)
	}
}

// send processes a send operation on an empty channel c.
// The value ep sent by the sender is copied to the receiver sg.
// The receiver is then woken up to go on its merry way.
// Channel c must be empty and locked.  send unlocks c with unlockf.
// sg must already be dequeued from c.
// ep must be non-nil and point to the heap or the caller's stack.
func send(c *hchan, sg *sudog, ep unsafe.Pointer, unlockf func(), skip int) {
	if sg.elem != nil{
		sendDirect(c.elemtype,sg,ep)
	}
	gp :=sg.g
	unlockf()
	goready(gp, skip+1)
}

// Sends and receives on unbuffered or empty-buffered channels are the
// only operations where one running goroutine writes to the stack of
// another running goroutine. The GC assumes that stack writes only
// happen when the goroutine is running and are only done by that
// goroutine. Using a write barrier is sufficient to make up for
// violating that assumption, but the write barrier has to work.
// typedmemmove will call bulkBarrierPreWrite, but the target bytes
// are not in the heap, so that will not help. We arrange to call
// memmove and typeBitsBulkBarrier instead.
func sendDirect(t *_type, sg *sudog, src unsafe.Pointer) {
	// src is on our stack, dst is a slot on another stack.

	// Once we read sg.elem out of sg, it will no longer
	// be updated if the destination's stack gets copied (shrunk).
	// So make sure that no preemption points can happen between read & use.
	dst := sg.elem

	// No need for cgo write barrier checks because dst is always
	// Go memory.
	memmove(dst,src,t.size)
}

func (q *waitq)dequeue()*sudog{
	for{
		sgp := q.first
		if sgp == nil {
			return nil
		}
		y :=sgp.next
		if y == nil{
			q.first = nil
			q.last =  nil
		}else{
			y.prev =  nil
			q.first = y
			sgp.next = nil// mark as removed (see dequeueSudog)
		}
		return sgp
	}
}

func (c *hchan) raceaddr() unsafe.Pointer {
	// Treat read-like and write-like operations on the channel to
	// happen at this address. Avoid using the address of qcount
	// or dataqsiz, because the len() and cap() builtins read
	// those addresses, and we don't want them racing with
	// operations like close().
	return unsafe.Pointer(&c.buf)
}