package runtime

import (
	"runtime/internal/sys"
	"unsafe"
)

// Event types in the trace, args are given in square brackets.
const (
	traceEvNone              = 0  // unused
	traceEvBatch             = 1  // start of per-P batch of events [pid, timestamp]
	traceEvFrequency         = 2  // contains tracer timer frequency [frequency (ticks per second)]
	traceEvStack             = 3  // stack [stack id, number of PCs, array of {PC, func string ID, file string ID, line}]
	traceEvGomaxprocs        = 4  // current value of GOMAXPROCS [timestamp, GOMAXPROCS, stack id]
	traceEvProcStart         = 5  // start of P [timestamp, thread id]
	traceEvProcStop          = 6  // stop of P [timestamp]
	traceEvGCStart           = 7  // GC start [timestamp, seq, stack id]
	traceEvGCDone            = 8  // GC done [timestamp]
	traceEvGCSTWStart        = 9  // GC STW start [timestamp, kind]
	traceEvGCSTWDone         = 10 // GC STW done [timestamp]
	traceEvGCSweepStart      = 11 // GC sweep start [timestamp, stack id]
	traceEvGCSweepDone       = 12 // GC sweep done [timestamp, swept, reclaimed]
	traceEvGoCreate          = 13 // goroutine creation [timestamp, new goroutine id, new stack id, stack id]
	traceEvGoStart           = 14 // goroutine starts running [timestamp, goroutine id, seq]
	traceEvGoEnd             = 15 // goroutine ends [timestamp]
	traceEvGoStop            = 16 // goroutine stops (like in select{}) [timestamp, stack]
	traceEvGoSched           = 17 // goroutine calls Gosched [timestamp, stack]
	traceEvGoPreempt         = 18 // goroutine is preempted [timestamp, stack]
	traceEvGoSleep           = 19 // goroutine calls Sleep [timestamp, stack]
	traceEvGoBlock           = 20 // goroutine blocks [timestamp, stack]
	traceEvGoUnblock         = 21 // goroutine is unblocked [timestamp, goroutine id, seq, stack]
	traceEvGoBlockSend       = 22 // goroutine blocks on chan send [timestamp, stack]
	traceEvGoBlockRecv       = 23 // goroutine blocks on chan recv [timestamp, stack]
	traceEvGoBlockSelect     = 24 // goroutine blocks on select [timestamp, stack]
	traceEvGoBlockSync       = 25 // goroutine blocks on Mutex/RWMutex [timestamp, stack]
	traceEvGoBlockCond       = 26 // goroutine blocks on Cond [timestamp, stack]
	traceEvGoBlockNet        = 27 // goroutine blocks on network [timestamp, stack]
	traceEvGoSysCall         = 28 // syscall enter [timestamp, stack]
	traceEvGoSysExit         = 29 // syscall exit [timestamp, goroutine id, seq, real timestamp]
	traceEvGoSysBlock        = 30 // syscall blocks [timestamp]
	traceEvGoWaiting         = 31 // denotes that goroutine is blocked when tracing starts [timestamp, goroutine id]
	traceEvGoInSyscall       = 32 // denotes that goroutine is in syscall when tracing starts [timestamp, goroutine id]
	traceEvHeapAlloc         = 33 // memstats.heap_live change [timestamp, heap_alloc]
	traceEvNextGC            = 34 // memstats.next_gc change [timestamp, next_gc]
	traceEvTimerGoroutine    = 35 // not currently used; previously denoted timer goroutine [timer goroutine id]
	traceEvFutileWakeup      = 36 // denotes that the previous wakeup of this goroutine was futile [timestamp]
	traceEvString            = 37 // string dictionary entry [ID, length, string]
	traceEvGoStartLocal      = 38 // goroutine starts running on the same P as the last event [timestamp, goroutine id]
	traceEvGoUnblockLocal    = 39 // goroutine is unblocked on the same P as the last event [timestamp, goroutine id, stack]
	traceEvGoSysExitLocal    = 40 // syscall exit on the same P as the last event [timestamp, goroutine id, real timestamp]
	traceEvGoStartLabel      = 41 // goroutine starts running with label [timestamp, goroutine id, seq, label string id]
	traceEvGoBlockGC         = 42 // goroutine blocks on GC assist [timestamp, stack]
	traceEvGCMarkAssistStart = 43 // GC mark assist start [timestamp, stack]
	traceEvGCMarkAssistDone  = 44 // GC mark assist done [timestamp]
	traceEvUserTaskCreate    = 45 // trace.NewContext [timestamp, internal task id, internal parent task id, stack, name string]
	traceEvUserTaskEnd       = 46 // end of a task [timestamp, internal task id, stack]
	traceEvUserRegion        = 47 // trace.WithRegion [timestamp, internal task id, mode(0:start, 1:end), stack, name string]
	traceEvUserLog           = 48 // trace.Log [timestamp, internal task id, key string id, stack, value string]
	traceEvCount             = 49
	// Byte is used but only 6 bits are available for event type.
	// The remaining 2 bits are used to specify the number of arguments.
	// That means, the max event type value is 63.
)

const (
	// Timestamps in trace are cputicks/traceTickDiv.
	// This makes absolute values of timestamp diffs smaller,
	// and so they are encoded in less number of bytes.
	// 64 on x86 is somewhat arbitrary (one tick is ~20ns on a 3GHz machine).
	// The suggested increment frequency for PowerPC's time base register is
	// 512 MHz according to Power ISA v2.07 section 6.2, so we use 16 on ppc64
	// and ppc64le.
	// Tracing won't work reliably for architectures where cputicks is emulated
	// by nanotime, so the value doesn't matter for those architectures.
	traceTickDiv = 16 + 48*(sys.Goarch386|sys.GoarchAmd64)
	// Maximum number of PCs in a single stack trace.
	// Since events contain only stack id rather than whole stack trace,
	// we can allow quite large values here.
	traceStackSize = 128
	// Identifier of a fake P that is used when we trace without a real P.
	traceGlobProc = -1
	// Maximum number of bytes to encode uint64 in base-128.
	traceBytesPerNumber = 10
	// Shift of the number of arguments in the first event byte.
	traceArgCountShift = 6
	// Flag passed to traceGoPark to denote that the previous wakeup of this
	// goroutine was futile. For example, a goroutine was unblocked on a mutex,
	// but another goroutine got ahead and acquired the mutex before the first
	// goroutine is scheduled, so the first goroutine has to block again.
	// Such wakeups happen on buffered channels and sync.Mutex,
	// but are generally not interesting for end user.
	traceFutileWakeup byte = 128
)

// trace is global tracing context.
var trace struct {
	lock          mutex       // protects the following members
	lockOwner     *g          // to avoid deadlocks during recursive lock locks
	enabled       bool        // when set runtime traces events
	shutdown      bool        // set when we are waiting for trace reader to finish after setting enabled to false
	headerWritten bool        // whether ReadTrace has emitted trace header
	footerWritten bool        // whether ReadTrace has emitted trace footer
	shutdownSema  uint32      // used to wait for ReadTrace completion
	seqStart      uint64      // sequence number when tracing was started
	ticksStart    int64       // cputicks when tracing was started
	ticksEnd      int64       // cputicks when tracing was stopped
	timeStart     int64       // nanotime when tracing was started
	timeEnd       int64       // nanotime when tracing was stopped
	seqGC         uint64      // GC start/done sequencer
	reading       traceBufPtr // buffer currently handed off to user
	empty         traceBufPtr // stack of empty buffers
	fullHead      traceBufPtr // queue of full buffers
	fullTail      traceBufPtr
	reader        guintptr        // goroutine that called ReadTrace, or nil
	stackTab      traceStackTable // maps stack traces to unique ids

	// Dictionary for traceEvString.
	//
	// TODO: central lock to access the map is not ideal.
	//   option: pre-assign ids to all user annotation region names and tags
	//   option: per-P cache
	//   option: sync.Map like data structure
	stringsLock mutex
	strings     map[string]uint64
	stringSeq   uint64

	// markWorkerLabels maps gcMarkWorkerMode to string ID.
	markWorkerLabels [len(gcMarkWorkerModeStrings)]uint64

	bufLock mutex       // protects buf
	buf     traceBufPtr // global trace buffer, used when running without a p
}

// traceBufPtr is a *traceBuf that is not traced by the garbage
// collector and doesn't have write barriers. traceBufs are not
// allocated from the GC'd heap, so this is safe, and are often
// manipulated in contexts where write barriers are not allowed, so
// this is necessary.
//
// TODO: Since traceBuf is now go:notinheap, this isn't necessary.
type traceBufPtr uintptr

func (tp traceBufPtr) ptr() *traceBuf   { return (*traceBuf)(unsafe.Pointer(tp)) }
func (tp *traceBufPtr) set(b *traceBuf) { *tp = traceBufPtr(unsafe.Pointer(b)) }
func traceBufPtrOf(b *traceBuf) traceBufPtr {
	return traceBufPtr(unsafe.Pointer(b))
}

// traceAllocBlock is a block in traceAlloc.
//
// traceAllocBlock is allocated from non-GC'd memory, so it must not
// contain heap pointers. Writes to pointers to traceAllocBlocks do
// not need write barriers.
//
//go:notinheap
type traceAllocBlock struct {
	next traceAllocBlockPtr
	data [64<<10 - sys.PtrSize]byte
}

// TODO: Since traceAllocBlock is now go:notinheap, this isn't necessary.
type traceAllocBlockPtr uintptr

func (p traceAllocBlockPtr) ptr() *traceAllocBlock   { return (*traceAllocBlock)(unsafe.Pointer(p)) }
func (p *traceAllocBlockPtr) set(x *traceAllocBlock) { *p = traceAllocBlockPtr(unsafe.Pointer(x)) }

// traceStack is a single stack in traceStackTable.
type traceStack struct {
	link traceStackPtr
	hash uintptr
	id   uint32
	n    int
	stk  [0]uintptr // real type [n]uintptr
}


// traceStackTable maps stack traces (arrays of PC's) to unique uint32 ids.
// It is lock-free for reading.
type traceStackTable struct {
	lock mutex
	seq  uint32
	mem  traceAlloc
	tab  [1 << 13]traceStackPtr
}

type traceStackPtr uintptr

func (tp traceStackPtr) ptr() *traceStack { return (*traceStack)(unsafe.Pointer(tp)) }

// stack returns slice of PCs.
func (ts *traceStack) stack() []uintptr {
	return (*[traceStackSize]uintptr)(unsafe.Pointer(&ts.stk))[:ts.n]
}

// traceAlloc is a non-thread-safe region allocator.
// It holds a linked list of traceAllocBlock.
type traceAlloc struct {
	head traceAllocBlockPtr
	off  uintptr
}

// traceBufHeader is per-P tracing buffer.
type traceBufHeader struct {
	link      traceBufPtr             // in trace.empty/full
	lastTicks uint64                  // when we wrote the last event
	pos       int                     // next write offset in arr
	stk       [traceStackSize]uintptr // scratch buffer for traceback
}

// traceBuf is per-P tracing buffer.
//
//go:notinheap
type traceBuf struct {
	traceBufHeader
	arr [64<<10 - unsafe.Sizeof(traceBufHeader{})]byte // underlying buffer for traceBufHeader.buf
}

func traceGoSysCall() {
	traceEvent(traceEvGoSysCall, 1)
}

// traceEvent writes a single event to trace buffer, flushing the buffer if necessary.
// ev is event type.
// If skip > 0, write current stack id as the last argument (skipping skip top frames).
// If skip = 0, this event type should contain a stack, but we don't want
// to collect and remember it for this particular call.
func traceEvent(ev byte, skip int, args ...uint64) {
	mp, pid, bufp := traceAcquireBuffer()
	// Double-check trace.enabled now that we've done m.locks++ and acquired bufLock.
	// This protects from races between traceEvent and StartTrace/StopTrace.

	// The caller checked that trace.enabled == true, but trace.enabled might have been
	// turned off between the check and now. Check again. traceLockBuffer did mp.locks++,
	// StopTrace does stopTheWorld, and stopTheWorld waits for mp.locks to go back to zero,
	// so if we see trace.enabled == true now, we know it's true for the rest of the function.
	// Exitsyscall can run even during stopTheWorld. The race with StartTrace/StopTrace
	// during tracing in exitsyscall is resolved by locking trace.bufLock in traceLockBuffer.
	//
	// Note trace_userTaskCreate runs the same check.
	if !trace.enabled && !mp.startingtrace {
		traceReleaseBuffer(pid)
		return
	}

	if skip > 0 {
		if getg() == mp.curg {
			skip++ // +1 because stack is captured in traceEventLocked.
		}
	}
	traceEventLocked(0, mp, pid, bufp, ev, skip, args...)
	traceReleaseBuffer(pid)
}

func traceEventLocked(extraBytes int, mp *m, pid int32, bufp *traceBufPtr, ev byte, skip int, args ...uint64) {
	buf := bufp.ptr()
	// TODO: test on non-zero extraBytes param.
	maxSize := 2 + 5*traceBytesPerNumber + extraBytes // event type, length, sequence, timestamp, stack id and two add params
	if buf == nil || len(buf.arr)-buf.pos < maxSize {
		buf = traceFlush(traceBufPtrOf(buf), pid).ptr()
		bufp.set(buf)
	}

	ticks := uint64(cputicks()) / traceTickDiv
	tickDiff := ticks - buf.lastTicks
	buf.lastTicks = ticks
	narg := byte(len(args))
	if skip >= 0 {
		narg++
	}
	// We have only 2 bits for number of arguments.
	// If number is >= 3, then the event type is followed by event length in bytes.
	if narg > 3 {
		narg = 3
	}
	startPos := buf.pos
	buf.byte(ev | narg<<traceArgCountShift)
	var lenp *byte
	if narg == 3 {
		// Reserve the byte for length assuming that length < 128.
		buf.varint(0)
		lenp = &buf.arr[buf.pos-1]
	}
	buf.varint(tickDiff)
	for _, a := range args {
		buf.varint(a)
	}
	if skip == 0 {
		buf.varint(0)
	} else if skip > 0 {
		buf.varint(traceStackID(mp, buf.stk[:], skip))
	}
	evSize := buf.pos - startPos
	if evSize > maxSize {
		throw("invalid length of trace event")
	}
	if lenp != nil {
		// Fill in actual length.
		*lenp = byte(evSize - 2)
	}
}

func traceStackID(mp *m, buf []uintptr, skip int) uint64 {
	_g_ := getg()
	gp := mp.curg
	var nstk int
	if gp == _g_ {
		nstk = callers(skip+1, buf)
	} else if gp != nil {
		gp = mp.curg
		nstk = gcallers(gp, skip, buf)
	}
	if nstk > 0 {
		nstk-- // skip runtime.goexit
	}
	if nstk > 0 && gp.goid == 1 {
		nstk-- // skip runtime.main
	}
	id := trace.stackTab.put(buf[:nstk])
	return uint64(id)
}

// put returns a unique id for the stack trace pcs and caches it in the table,
// if it sees the trace for the first time.
func (tab *traceStackTable) put(pcs []uintptr) uint32 {
	if len(pcs) == 0 {
		return 0
	}
	hash := memhash(unsafe.Pointer(&pcs[0]), 0, uintptr(len(pcs))*unsafe.Sizeof(pcs[0]))
	// First, search the hashtable w/o the mutex.
	if id := tab.find(pcs, hash); id != 0 {
		return id
	}
	// Now, double check under the mutex.
	lock(&tab.lock)
	if id := tab.find(pcs, hash); id != 0 {
		unlock(&tab.lock)
		return id
	}
	// Create new record.
	tab.seq++
	stk := tab.newStack(len(pcs))
	stk.hash = hash
	stk.id = tab.seq
	stk.n = len(pcs)
	stkpc := stk.stack()
	for i, pc := range pcs {
		stkpc[i] = pc
	}
	part := int(hash % uintptr(len(tab.tab)))
	stk.link = tab.tab[part]
	atomicstorep(unsafe.Pointer(&tab.tab[part]), unsafe.Pointer(stk))
	unlock(&tab.lock)
	return stk.id
}

// newStack allocates a new stack of size n.
func (tab *traceStackTable) newStack(n int) *traceStack {
	return (*traceStack)(tab.mem.alloc(unsafe.Sizeof(traceStack{}) + uintptr(n)*sys.PtrSize))
}

// alloc allocates n-byte block.
func (a *traceAlloc) alloc(n uintptr) unsafe.Pointer {
	n = alignUp(n, sys.PtrSize)
	if a.head == 0 || a.off+n > uintptr(len(a.head.ptr().data)) {
		if n > uintptr(len(a.head.ptr().data)) {
			throw("trace: alloc too large")
		}
		block := (*traceAllocBlock)(sysAlloc(unsafe.Sizeof(traceAllocBlock{}), &memstats.other_sys))
		if block == nil {
			throw("trace: out of memory")
		}
		block.next.set(a.head.ptr())
		a.head.set(block)
		a.off = 0
	}
	p := &a.head.ptr().data[a.off]
	a.off += n
	return unsafe.Pointer(p)
}

// find checks if the stack trace pcs is already present in the table.
func (tab *traceStackTable) find(pcs []uintptr, hash uintptr) uint32 {
	part := int(hash % uintptr(len(tab.tab)))
Search:
	for stk := tab.tab[part].ptr(); stk != nil; stk = stk.link.ptr() {
		if stk.hash == hash && stk.n == len(pcs) {
			for i, stkpc := range stk.stack() {
				if stkpc != pcs[i] {
					continue Search
				}
			}
			return stk.id
		}
	}
	return 0
}

// varint appends v to buf in little-endian-base-128 encoding.
func (buf *traceBuf) varint(v uint64) {
	pos := buf.pos
	for ; v >= 0x80; v >>= 7 {
		buf.arr[pos] = 0x80 | byte(v)
		pos++
	}
	buf.arr[pos] = byte(v)
	pos++
	buf.pos = pos
}

// byte appends v to buf.
func (buf *traceBuf) byte(v byte) {
	buf.arr[buf.pos] = v
	buf.pos++
}

// traceFlush puts buf onto stack of full buffers and returns an empty buffer.
func traceFlush(buf traceBufPtr, pid int32) traceBufPtr {
	owner := trace.lockOwner
	dolock := owner == nil || owner != getg().m.curg
	if dolock {
		lock(&trace.lock)
	}
	if buf != 0 {
		traceFullQueue(buf)
	}
	if trace.empty != 0 {
		buf = trace.empty
		trace.empty = buf.ptr().link
	} else {
		buf = traceBufPtr(sysAlloc(unsafe.Sizeof(traceBuf{}), &memstats.other_sys))
		if buf == 0 {
			throw("trace: out of memory")
		}
	}
	bufp := buf.ptr()
	bufp.link.set(nil)
	bufp.pos = 0

	// initialize the buffer for a new batch
	ticks := uint64(cputicks()) / traceTickDiv
	bufp.lastTicks = ticks
	bufp.byte(traceEvBatch | 1<<traceArgCountShift)
	bufp.varint(uint64(pid))
	bufp.varint(ticks)

	if dolock {
		unlock(&trace.lock)
	}
	return buf
}

// traceFullQueue queues buf into queue of full buffers.
func traceFullQueue(buf traceBufPtr) {
	buf.ptr().link = 0
	if trace.fullHead == 0 {
		trace.fullHead = buf
	} else {
		trace.fullTail.ptr().link = buf
	}
	trace.fullTail = buf
}

// traceReleaseBuffer releases a buffer previously acquired with traceAcquireBuffer.
func traceReleaseBuffer(pid int32) {
	if pid == traceGlobProc {
		unlock(&trace.bufLock)
	}
	releasem(getg().m)
}

// traceAcquireBuffer returns trace buffer to use and, if necessary, locks it.
func traceAcquireBuffer() (mp *m, pid int32, bufp *traceBufPtr) {
	mp = acquirem()
	if p := mp.p.ptr(); p != nil {
		return mp, p.id, &p.tracebuf
	}
	lock(&trace.bufLock)
	return mp, traceGlobProc, &trace.buf
}

func traceGoSysBlock(pp *p) {
	// Sysmon and stopTheWorld can declare syscalls running on remote Ps as blocked,
	// to handle this we temporary employ the P.
	mp := acquirem()
	oldp := mp.p
	mp.p.set(pp)
	traceEvent(traceEvGoSysBlock, -1)
	mp.p = oldp
	releasem(mp)
}

func traceGoSched() {
	_g_ := getg()
	_g_.tracelastp = _g_.m.p
	traceEvent(traceEvGoSched, 1)
}

func traceGoStart() {
	_g_ := getg().m.curg
	_p_ := _g_.m.p
	_g_.traceseq++
	if _g_ == _p_.ptr().gcBgMarkWorker.ptr() {
		traceEvent(traceEvGoStartLabel, -1, uint64(_g_.goid), _g_.traceseq, trace.markWorkerLabels[_p_.ptr().gcMarkWorkerMode])
	} else if _g_.tracelastp == _p_ {
		traceEvent(traceEvGoStartLocal, -1, uint64(_g_.goid))
	} else {
		_g_.tracelastp = _p_
		traceEvent(traceEvGoStart, -1, uint64(_g_.goid), _g_.traceseq)
	}
}

func traceProcStop(pp *p) {
	// Sysmon and stopTheWorld can stop Ps blocked in syscalls,
	// to handle this we temporary employ the P.
	mp := acquirem()
	oldp := mp.p
	mp.p.set(pp)
	traceEvent(traceEvProcStop, -1)
	mp.p = oldp
	releasem(mp)
}

