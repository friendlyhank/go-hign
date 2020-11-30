// Copyright 2011 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// +build aix darwin netbsd openbsd plan9 solaris windows

package runtime

import (
	"runtime/internal/atomic"
	"unsafe"
)

// This implementation depends on OS-specific implementations of
//
//	func semacreate(mp *m)
//		Create a semaphore for mp, if it does not already have one.
//
//	func semasleep(ns int64) int32
//		If ns < 0, acquire m's semaphore and return 0.
//		If ns >= 0, try to acquire m's semaphore for at most ns nanoseconds.
//		Return 0 if the semaphore was acquired, -1 if interrupted or timed out.
//
//	func semawakeup(mp *m)
//		Wake up mp, which is or will soon be sleeping on its semaphore.
//
const (
	locked uintptr = 1

	active_spin     = 4
	active_spin_cnt = 30
	passive_spin    = 1
)


func lock(l *mutex){
	lockWithRank(l,getLockRank(l))
}

func lock2(l *mutex){
	gp := getg()
	if gp.m.locks < 0 {
		throw("runtime·lock: lock count")
	}
	gp.m.locks++

	// Speculative grab for lock.
	//如果不是锁定状态,则修改状态为锁定,并返回true
	//如果状态不是锁定状态，直接返回falst
	if atomic.Casuintptr(&l.key, 0, locked) {
		return
	}
	//
	semacreate(gp.m)

	//单处理器和多处理器的处理
	// On uniprocessor's, no point spinning.
	// On multiprocessors, spin for ACTIVE_SPIN attempts.
	spin := 0
	if ncpu > 1 {
		spin = active_spin
	}
Loop:
	for i := 0; ; i++ {
		//加载锁的状态值
		v := atomic.Loaduintptr(&l.key)
		//如果不是锁定状态
		if v&locked == 0 {
			// Unlocked. Try to lock.尝试去锁定
			if atomic.Casuintptr(&l.key, v, v|locked) {
				return
			}
			i = 0
		}
		if i < spin {
			procyield(active_spin_cnt) //进入循环
		} else if i < spin+passive_spin {
			osyield()
		} else {
			// Someone else has it.
			// l->waitm points to a linked list of M's waiting
			// for this lock, chained through m->nextwaitm.
			// Queue this M.
			for {
				//进入锁等待的m的链表
				gp.m.nextwaitm = muintptr(v &^ locked)
				if atomic.Casuintptr(&l.key, v, uintptr(unsafe.Pointer(gp.m))|locked) {
					break
				}
				v = atomic.Loaduintptr(&l.key)
				if v&locked == 0 {
					continue Loop
				}
			}
			if v&locked != 0 {
				// Queued. Wait.
				semasleep(-1)
				i = 0
			}
		}
	}
}

func unlock(l *mutex){
	unlockWithRank(l)
}

//go:nowritebarrier
// We might not be holding a p in this code.
func unlock2(l *mutex) {
	gp := getg()
	var mp *m
	for {
		//加载锁的状态值
		v := atomic.Loaduintptr(&l.key)
		if v == locked {
			if atomic.Casuintptr(&l.key, locked, 0) {
				break
			}
		} else {
			// Other M's are waiting for the lock.
			// Dequeue an M.
			//从锁的等待队列中取出一个mp唤醒
			mp = muintptr(v &^ locked).ptr()
			if atomic.Casuintptr(&l.key, v, uintptr(mp.nextwaitm)) {
				// Dequeued an M.  Wake it.
				semawakeup(mp)
				break
			}
		}
	}
	gp.m.locks--
	if gp.m.locks < 0 {
		throw("runtime·unlock: lock count")
	}
}
