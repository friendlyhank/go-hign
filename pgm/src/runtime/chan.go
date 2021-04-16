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

// chanbuf(c, i) is pointer to the i'th slot in the buffer.
func chanbuf(c *hchan, i uint) unsafe.Pointer{
	return add(c.buf,uintptr(i)*uintptr(c.elemsize))
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
		return true
	}

	//如果是又缓冲槽的,将数据写入缓冲槽
	if c.qcount < c.dataqsiz{
		// Space is available in the channel buffer. Enqueue the element to send.
		//根据发送索引计算出插入的位置
		qp :=chanbuf(c,c.sendx)
		typedmemmove(c.elemtype,qp,ep)
		c.sendx++
		//如果缓冲槽满了，重置发送索引
		if c.sendx ==c.dataqsiz{
			c.sendx = 0
		}
		c.qcount++
		//解锁
		unlock(&c.lock)
		return true
	}

	//如果不需要设置阻塞，直接解锁,然后返回false表示发送失败
	if !block{
		unlock(&c.lock)
		return false
	}

	// Block on the channel. Some receiver will complete our operation for us.
	gp := getg()
	mysg := acquireSudog()

	// No stack splits between assigning elem and enqueuing mysg
	// on gp.waiting where copystack can find it.
	mysg.elem = ep
	mysg.g = gp
	mysg.c = c
	gp.waiting =mysg
	c.sendq.enqueue(mysg)//这里应该可以说明有多个不断入队列
	//使当前goroutine休眠等待激活
	gopark(chanparkcommit, unsafe.Pointer(&c.lock), waitReasonChanSend, traceEvGoBlockSend, 2)
	//保持活跃状态，直到被接收者接收
	KeepAlive(ep)

	//到这里说明被唤醒了,将资源进行释放
	// someone woke us up.
	if mysg != gp.waiting {
		throw("G waiting list is corrupted")
	}
	gp.waiting = nil
	mysg.c = nil
	releaseSudog(mysg)
	return true
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
	//直接将数据写入i <- chan
	memmove(dst,src,t.size)
}

// chanrecv receives on channel c and writes the received data to ep.
// ep may be nil, in which case received data is ignored.
// If block == false and no elements are available, returns (false, false).
// Otherwise, if c is closed, zeros *ep and returns (true, false).
// Otherwise, fills in *ep with an element and returns (true, true).
// A non-nil ep must point to the heap or the caller's stack.
//chan的接收
func chanrecv(c *hchan, ep unsafe.Pointer, block bool) (selected, received bool) {
	// raceenabled: don't need to check ep, as it is always on the stack
	// or is new memory allocated by reflect.

	if c== nil{
		if !block{
			return
		}
		gopark(nil, nil, waitReasonChanReceiveNilChan, traceEvGoStop, 2)
		throw("unreachable")
	}

	lock(&c.lock)

	if sg :=c.sendq.dequeue();sg != nil{
		// Found a waiting sender. If buffer is size 0, receive value
		// directly from sender. Otherwise, receive from head of queue
		// and add sender's value to the tail of the queue (both map to
		// the same buffer slot because the queue is full).
		recv(c,sg,ep, func() {unlock(&c.lock)},3)
		return true,true
	}

	//没有在等待的发送队列,缓冲槽中有数据，从缓存槽中读取数据
	if c.qcount > 0{
		// Receive directly from queue
		qp := chanbuf(c,c.recvx)
		if ep != nil{
			typedmemmove(c.elemtype, ep, qp)//将缓冲槽的数据拷贝到接收方目标地址
		}
		typedmemclr(c.elemtype, qp)//将已经被拷贝到发送方的缓冲槽释放
		c.recvx++
		if c.recvx == c.dataqsiz{
			c.recvx = 0
		}
		c.qcount--
		unlock(&c.lock)
		return true,true
	}

	if !block{
		unlock(&c.lock)
		return false,false
	}

	// no sender available: block on this channel.
	gp := getg()
	mysg := acquireSudog()

	// No stack splits between assigning elem and enqueuing mysg
	// on gp.waiting where copystack can find it.
	//例如i := <- ch,在发送端唤醒的时候就会把值拷贝到i中,然后唤醒i
	mysg.elem = ep
	gp.waiting =mysg
	mysg.g = gp
	mysg.c = c
	c.recvq.enqueue(mysg)//写入接收队列

	//进入休眠，等待被唤醒
	gopark(chanparkcommit, unsafe.Pointer(&c.lock), waitReasonChanReceive, traceEvGoBlockRecv, 2)

	// someone woke us up
	//走到这里说明被唤醒了
	if mysg != gp.waiting {
		throw("G waiting list is corrupted")
	}
	gp.waiting = nil
	mysg.c = nil
	releaseSudog(mysg)
	return true, false
}

// recv processes a receive operation on a full channel c.
// There are 2 parts:
// 1) The value sent by the sender sg is put into the channel
//    and the sender is woken up to go on its merry way.
// 2) The value received by the receiver (the current G) is
//    written to ep.
// For synchronous channels, both values are the same.
// For asynchronous channels, the receiver gets its data from
// the channel buffer and the sender's data is put in the
// channel buffer.
// Channel c must be full and locked. recv unlocks c with unlockf.
// sg must already be dequeued from c.
// A non-nil ep must point to the heap or the caller's stack.
func recv(c *hchan, sg *sudog, ep unsafe.Pointer, unlockf func(), skip int) {
	//如果没有缓存槽,那么直接拷贝发送队列的值
	if c.dataqsiz == 0{
		if ep != nil{
			// copy data from sender
			recvDirect(c.elemtype,sg,ep)
		}
	}else{
		// Queue is full. Take the item at the
		// head of the queue. Make the sender enqueue
		// its item at the tail of the queue. Since the
		// queue is full, those are both the same slot.
		//如果有缓存槽,说明缓存槽都饱满并且产生了等待的发送队列
		//这时候先从缓冲槽中获取
		qp :=chanbuf(c,c.recvx)
		// copy data from queue to receiver
		if ep != nil{
			typedmemmove(c.elemtype,ep,qp)//将缓冲槽的数据拷贝到接收方目标地址
		}
		// copy data from sender to queue
		typedmemmove(c.elemtype,qp,sg.elem)//将发送方的头队列复制到当前缓存槽位置
		c.recvx++//更新发送索引
		if c.recvx == c.dataqsiz{
			c.recvx = 0
		}
		c.sendx = c.recvx //在缓存槽满的情况两个值是相等的 c.sendx = (c.sendx+1) % c.dataqsiz
	}

	//将在阻塞中的发送方唤醒
	sg.elem = nil
	gp :=sg.g
	unlockf()
	goready(gp, skip+1)
}

func recvDirect(t *_type, sg *sudog, dst unsafe.Pointer) {
	// dst is on our stack or the heap, src is on another stack.
	// The channel is locked, so src will not move during this
	// operation.
	src := sg.elem
	memmove(dst,src,t.size)
}

func closechan(c *hchan) {
	if c == nil{

	}

	lock(&c.lock)
	if c.closed != 0{
		unlock(&c.lock)
	}

	c.closed =1

	var glist gList

	//释放接收者
	// release all readers
	for{
		sg :=c.recvq.dequeue()
		if sg == nil{
			break
		}
		if sg.elem != nil{
			typedmemclr(c.elemtype,sg.elem)
			sg.elem = nil
		}
		gp := sg.g
		glist.push(gp)
	}

	//释放所有的发送者
	// release all writers (they will panic)
	for{
		sg := c.sendq.dequeue()
		if sg == nil{
			break
		}
		sg.elem = nil
		gp := sg.g
		glist.push(gp)
	}
	unlock(&c.lock)

	// Ready all Gs now that we've dropped the channel lock.
	//将等待状态的g变为运行状态
	for !glist.empty(){
		gp :=glist.pop()
		gp.schedlink = 0
		goready(gp,3)
	}
}

func chanparkcommit(gp *g, chanLock unsafe.Pointer) bool {
	// Make sure we unlock after setting activeStackChans and
	// unsetting parkingOnChan. The moment we unlock chanLock
	// we risk gp getting readied by a channel operation and
	// so gp could continue running before everything before
	// the unlock is visible (even to gp itself).
	unlock((*mutex)(chanLock))
	return true
}

func (q *waitq) enqueue(sgp *sudog) {
	sgp.next = nil
	x := q.last
	if x == nil{
		sgp.prev = nil
		q.first = sgp
		q.last = sgp
		return
	}
	sgp.prev = x
	x.next = sgp
	q.last = sgp
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