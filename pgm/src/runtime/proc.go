package runtime

import(
	"runtime/internal/atomic"
	"runtime/internal/sys"
	"unsafe"
)

var buildVersion = sys.TheVersion

// set using hcmd/go/internal/modload.ModInfoProg
var modinfo string

// Goroutine scheduler
// The scheduler's job is to distribute ready-to-run goroutines over worker threads.
//
// The main concepts are:
// G - goroutine.
// M - worker thread, or machine.
// P - processor, a resource that is required to execute Go code.
//     M must have an associated P to execute Go code, however it can be
//     blocked or in a syscall w/o an associated P.
//
// Design doc at https://golang.org/s/go11sched.

// Worker thread parking/unparking.
// We need to balance between keeping enough running worker threads to utilize
// available hardware parallelism and parking excessive running worker threads
// to conserve CPU resources and power. This is not simple for two reasons:
// (1) scheduler state is intentionally distributed (in particular, per-P work
// queues), so it is not possible to compute global predicates on fast paths;
// (2) for optimal thread management we would need to know the future (don't park
// a worker thread when a new goroutine will be readied in near future).
//
// Three rejected approaches that would work badly:
// 1. Centralize all scheduler state (would inhibit scalability).
// 2. Direct goroutine handoff. That is, when we ready a new goroutine and there
//    is a spare P, unpark a thread and handoff it the thread and the goroutine.
//    This would lead to thread state thrashing, as the thread that readied the
//    goroutine can be out of work the very next moment, we will need to park it.
//    Also, it would destroy locality of computation as we want to preserve
//    dependent goroutines on the same thread; and introduce additional latency.
// 3. Unpark an additional thread whenever we ready a goroutine and there is an
//    idle P, but don't do handoff. This would lead to excessive thread parking/
//    unparking as the additional threads will instantly park without discovering
//    any work to do.
//
// The current approach:
// We unpark an additional thread when we ready a goroutine if (1) there is an
// idle P and there are no "spinning" worker threads. A worker thread is considered
// spinning if it is out of local work and did not find work in global run queue/
// netpoller; the spinning state is denoted in m.spinning and in sched.nmspinning.
// Threads unparked this way are also considered spinning; we don't do goroutine
// handoff so such threads are out of work initially. Spinning threads do some
// spinning looking for work in per-P run queues before parking. If a spinning
// thread finds work it takes itself out of the spinning state and proceeds to
// execution. If it does not find work it takes itself out of the spinning state
// and then parks.
// If there is at least one spinning thread (sched.nmspinning>1), we don't unpark
// new threads when readying goroutines. To compensate for that, if the last spinning
// thread finds work and stops spinning, it must unpark a new spinning thread.
// This approach smooths out unjustified spikes of thread unparking,
// but at the same time guarantees eventual maximal CPU parallelism utilization.
//
// The main implementation complication is that we need to be very careful during
// spinning->non-spinning thread transition. This transition can race with submission
// of a new goroutine, and either one part or another needs to unpark another worker
// thread. If they both fail to do that, we can end up with semi-persistent CPU
// underutilization. The general pattern for goroutine readying is: submit a goroutine
// to local work queue, #StoreLoad-style memory barrier, check sched.nmspinning.
// The general pattern for spinning->non-spinning transition is: decrement nmspinning,
// #StoreLoad-style memory barrier, check all per-P work queues for new work.
// Note that all this complexity does not apply to global run queue as we are not
// sloppy about thread unparking when submitting to global queue. Also see comments
// for nmspinning manipulation.

var(
	//定义m0,g0
	m0  m
	g0  g
)

//go:linkname main_main main.main
func main_main()

// mainStarted indicates that the main M has started.
var mainStarted bool

// The main goroutine
func main(){
	g :=getg()

	// Max stack size is 1 GB on 64-bit, 250 MB on 32-bit.
	// Using decimal instead of binary GB and MB because
	// they look nicer in the stack overflow failure message.
	//在64位情况下最大时1GB,32位最大的栈大小是250mb
	if sys.PtrSize == 8{
		maxstacksize =1000000000
	}else{
		maxstacksize =250000000
	}

	//Allow newproc to start new Ms.
	mainStarted = true

	if g.m != &m0{
		throw("runtime.main not on m0")
	}

	fn := main_main// make an indirect call, as the linker doesn't know the address of the main package when laying down the runtime
	fn()

	exit(0)
}

//runtime/asm_amd64.s
//go:nosplit
func badctxt() {
	throw("ctxt != 0")
}

//runtime/asm_amd64.s
//go:nosplit
//go:nowritebarrierrec
func badmorestackg0() {
}

//go:nosplit
//go:nowritebarrierrec
func badmorestackgsignal() {

}

// The bootstrap sequence is:
//
//	call osinit
//	call schedinit
//	make & queue new G
//	call runtime·mstart
//
// The new G calls runtime·main.
func schedinit(){
	lockInit(&sched.lock,lockRankSched)

	// raceinit must be the first call to race detector.
	// In particular, it must be done before mallocinit below calls racemapshadow.
	_g_ :=getg()

	//最大的线程数量限制
	sched.maxmcount =10000

	//栈、内存分配器、调取器相关初始化
	stackinit()
	mallocinit()
	mcommoninit(_g_.m,-1)

	//初始化参数和环境变量

	//垃圾回收站初始化

	//设置p的数量
	procs := ncpu
	if procresize(procs) != nil {
		throw("unknown runnable goroutine during bootstrap")
	}

	if buildVersion == ""{
		// Condition should never trigger. This code just serves
		// to ensure runtime·buildVersion is kept in the resulting binary.
		buildVersion ="unknown"
	}
	if len(modinfo) == 1{
		// Condition should never trigger. This code just serves
		// to ensure runtime·modinfo is kept in the resulting binary.
		modinfo = ""
	}
}

//初始化m,g0才可以去初始化
func mcommoninit(mp *m,id int64){
	_g_ := getg()
	// g0 stack won't make sense for user (and is not necessary unwindable).
	//g0才能初始化m
	if _g_ != _g_.m.g0{
	}

	lock(&sched.lock)

	if id >= 0{
		mp.id =id
	}else{
		mp.id = mReserveID()
	}

	mp.fastrand[0] = uint32(int64Hash(uint64(mp.id),fastrandseed))
	mp.fastrand[1] = uint32(int64Hash(uint64(cputicks()),^fastrandseed))
	if mp.fastrand[0]|mp.fastrand[1] == 0{
		mp.fastrand[1] = 1
	}

	// Add to allm so garbage collector doesn't free g->m
	// when it is just in a register or thread-local storage.
	//可以理解为allm是一个链表,每个m通过alllink链接起来
	mp.alllink = allm

	// NumCgoCall() iterates over allm w/o schedlock,
	// so we need to publish it safely.
	//保证原子操作
	atomicstorep(unsafe.Pointer(&allm),unsafe.Pointer(mp))
	unlock(&sched.lock)
}

var fastrandseed uintptr

func fastrandinit(){
	s := (*[unsafe.Sizeof(fastrandseed)]byte)(unsafe.Pointer(&fastrandseed))[:]
	getRandomData(s)
}

// mstart is the entry-point for new Ms.
//
// This must not split the stack because we may not even have stack
// bounds set up yet.
//
// May run during STW (because it doesn't have a P yet), so write
// barriers are not allowed.
//
//启动m
//go:nosplit
//go:nowritebarrierrec
func mstart(){
	_g_ := getg()
	mstart1()
	println(_g_)
	//Exit this thread. 停止这个线程
}

func mstart1(){
	_g_ := getg()
	if _g_ != _g_.m.g0{
		throw("bad runtime·mstart")
	}
	minit()

	// Record the caller for use as the top of stack in mcall and
	// for terminating the thread.
	// We're never coming back to mstart1 after we call schedule,
	// so other calls can reuse the current frame.
	schedule()
}

//进入循环调度
// One round of scheduler: find a runnable goroutine and execute it.
// Never returns.
func schedule() {
	_g_ := getg()
	if _g_.m.locks != 0{
		throw("schedule: holding locks")
	}

	var gp *g
	var inheritTime bool

	if gp == nil{
		gp,inheritTime = runqget(_g_.m.p.ptr())
		// We can see gp != nil here even if the M is spinning,
		// if checkTimers added a local goroutine via goready.
	}

	if gp == nil{
		gp,inheritTime =findrunnable()
	}

	execute(gp,inheritTime)
}

// Schedules gp to run on the current M.
// If inheritTime is true, gp inherits the remaining time in the
// current time slice. Otherwise, it starts a new time slice.
// Never returns.
//
// Write barriers are allowed because this is called immediately after
// acquiring a P in several places.
//
//go:yeswritebarrierrec
func execute(gp *g, inheritTime bool) {
	_g_ := getg()
	_g_.m.curg = gp

	gogo(&gp.sched)
}

// Finds a runnable goroutine to execute.
// Tries to steal from other P's, get g from local or global queue, poll network.
func findrunnable()(gp *g,inheritTime bool){
	_g_ := getg()

	// The conditions here and in handoffp must agree: if
	// findrunnable would return a G to run, handoffp must start
	// an M.

top:
	_p_ := _g_.m.p.ptr()
	// local runq
	if gp, inheritTime := runqget(_p_); gp != nil {
		return gp, inheritTime
	}
	goto top
}

func checkmcount(){
	// sched lock is held
	if mcount() > sched.maxmcount {
		print("runtime: program exceeds ",sched.maxmcount,"-thread limit\n")
		throw("thread exhaustion")
	}
}

// mReserveID returns the next ID to use for a new m. This new m is immediately
// considered 'running' by checkdead.
//
// sched.lock must be held.
//生成m的id
func mReserveID()int64{
	if sched.mnext +1 < sched.mnext{
		throw("runtime: thread ID overflow")
	}
	id := sched.mnext
	sched.mnext++
	//检查m的数量是否超出限制
	checkmcount()
	return id
}

// init initializes pp, which may be a freshly allocated p or a
// previously destroyed p, and transitions it to status _Pgcstop.
//初始化p
func (pp *p)init(id int32){
	pp.id = id
	pp.status =_Pgcstop
}

// destroy releases all of the resources associated with pp and
// transitions it to status _Pdead.
//
// sched.lock must be held and the world must be stopped.
//p的销毁
func (pp *p)destroy(){
	// Move all runnable goroutines to the global queue
	//将所有的可执行的goroutines移动到全局队列
	for pp.runqhead != pp.runqtail {
		// Pop from tail of local queue
		pp.runqtail--
		gp := pp.runq[pp.runqtail%uint32(len(pp.runq))].ptr()
		// Push onto head of global queue
		globrunqputhead(gp)
	}
	//将优先队列的移动到全局队列
	if pp.runnext != 0 {
		globrunqputhead(pp.runnext.ptr())
		pp.runnext = 0
	}
}


// Change number of processors. The world is stopped, sched is locked.
// gcworkbufs are not being modified by either the GC or
// the write barrier code.
// Returns list of Ps with local work, they need to be scheduled by the caller.
func procresize(nprocs int32)*p{
	old := gomaxprocs
	if old <0 || nprocs <= 0{
		throw("procresize:invalid arg")
	}

	// update statistics
	now :=nanotime()
	if sched.procresizetime != 0{
		sched.totaltime +=int64(old) * (now - sched.procresizetime)
	}
	sched.procresizetime =  now

	//Grow allp if necessary. p的数量需要增加
	for nprocs > int32(len(allp)){
		// Synchronize with retake, which could be running
		// concurrently since it doesn't run on a P.
		lock(&allpLock)
		if nprocs <= int32(cap(allp)){
			allp =allp[:nprocs]
		}else{
			nallp := make([]*p,nprocs)
			// Copy everything up to allp's cap so we
			// never lose old allocated Ps.
			copy(nallp,allp[:cap(allp)])
			allp =nallp
		}
		unlock(&allpLock)
	}
	// initialize new P's
	for i := old;i <nprocs;i++{
		pp := allp[i]
		if pp == nil{
			pp = new(p)
		}
		pp.init(i)
		atomicstorep(unsafe.Pointer(&allp[i]), unsafe.Pointer(pp))
	}

	_g_ := getg()
	if _g_.m.p != 0 && _g_.m.p.ptr().id < nprocs{
		// continue to use the current P
		//startTheWorld重新唤起的时候会调用
		_g_.m.p.ptr().status =_Prunning
	}else{
		// release the current P and acquire allp[0].
		//
		// We must do this before destroying our current P
		// because p.destroy itself has write barriers, so we
		// need to do that from a valid P.
		//这里会去绑定p
		if _g_.m.p != 0{
			_g_.m.p.ptr().m = 0
		}
		_g_.m.p = 0
		p := allp[0] //切换到p0
		p.m = 0
		//m和p进行绑定
	}

	//release resources from unused P's 释放多余的p
	for i := nprocs;i <old;i++{
		p :=allp[i]
		p.destroy()
		// can't free P itself because it can be referenced by an M in syscall
	}

	//将多余的allp释放
	// Trim allp.
	if int32(len(allp)) != nprocs {
		lock(&allpLock)
		allp = allp[:nprocs]
		unlock(&allpLock)
	}
	return nil
}

// Associate p and the current m.
//
// This function is allowed to have write barriers even if the caller
// isn't because it immediately acquires _p_.
//
//go:yeswritebarrierrec
//当前m和p进行绑定
func acquirep(_p_ *p) {
	// Do the part that isn't allowed to have write barriers.

}

// wirep is the first step of acquirep, which actually associates the
// current M to _p_. This is broken out so we can disallow write
// barriers for this part, since we don't yet have a P.
//
//go:nowritebarrierrec
//go:nosplit
func wirep(_p_ *p) {
	_g_ := getg()

	//m已经被绑定,抛出异常
	if _g_.m.p != 0{
		throw("wirep: already in go")
	}
	//p如果被绑定，抛出异常
	if _p_.m != 0 || _p_.status != _Pidle{
		id := int64(0)
		if _p_.m != 0 {
			id = _p_.m.ptr().id
		}
		print("wirep: p->m=", _p_.m, "(", id, ") p->status=", _p_.status, "\n")
		throw("wirep: invalid p state")
	}
	_g_.m.p.set(_p_)
	_p_.m.set(_g_.m)
	_p_.status = _Prunning
}

// Create a new g running fn with siz bytes of arguments.
// Put it on the queue of g's waiting to run.
// The compiler turns a go statement into a call to this.
//
// The stack layout of this call is unusual: it assumes that the
// arguments to pass to fn are on the stack sequentially immediately
// after &fn. Hence, they are logically part of newproc's argument
// frame, even though they don't appear in its signature (and can't
// because their types differ between call sites).
//
// This must be nosplit because this stack layout means there are
// untyped arguments in newproc's argument frame. Stack copies won't
// be able to adjust them and stack splits won't be able to copy them.
//
//go:nosplit
func newproc(siz int32, fn *funcval) {
	gp := getg()
	systemstack(func(){
		newg := newproc1(gp)

		_p_ :=getg().m.p.ptr()
		runqput(_p_,newg,true)
	})
}

// Create a new g in state _Grunnable, starting at fn, with narg bytes
// of arguments starting at argp. callerpc is the address of the go
// statement that created this. The caller is responsible for adding
// the new g to the scheduler.
//
// This must run on the system stack because it's the continuation of
// newproc, which cannot split the stack.
//
//创建一个g,只能在系统的栈上调用
//go:systemstack
func newproc1(callergp *g)*g{
	_g_ := getg()

	_p_ := _g_.m.p.ptr()
	newg :=gfget(_p_) //从全局或当前的p中获取一个空闲的g
	//如果获取不到空闲的g,直接新建一个g
	if newg == nil{

	}
	//memmove将参数拷贝到栈上
	return newg
}

// Get from gfree list.
// If local list is empty, grab a batch from global list.
//从全局队列或当前p队列获取一个空闲的g
func gfget(_p_ *p) *g{
retry:
	if _p_.gFree.empty() && (!sched.gFree.stack.empty() || !sched.gFree.noStack.empty()){
		lock(&sched.gFree.lock)
		// Move a batch of free Gs to the P.
		//从全局队列的剩余空闲g列表中转移，最多转移32个
		for _p_.gFree.n < 32{
			// Prefer Gs with stacks.
			gp := sched.gFree.stack.pop()
			if gp == nil{
				gp = sched.gFree.noStack.pop()
				if gp == nil{
					break
				}
			}
			sched.gFree.n--
			_p_.gFree.push(gp)
			_p_.gFree.n++
		}
		unlock(&sched.gFree.lock)
		goto retry
	}
	//从当前p中取一个空闲的g
	gp := _p_.gFree.pop()
	if gp == nil{
		return nil
	}
	_p_.gFree.n--
	return gp
}

func mcount()int32{
	return int32(sched.mnext - sched.nmfreed)
}

// runqput tries to put g on the local runnable queue.
// If next is false, runqput adds g to the tail of the runnable queue.
// If next is true, runqput puts g in the _p_.runnext slot.
// If the run queue is full, runnext puts g on the global queue.
// Executed only by the owner P.
//如果next等于false,将G放到本地的队列中
//如果next等于true,将G放到本地的优先队列中
//如果本地的队列已经满了,本地的g会移到全局的队列中
func runqput(_p_ *p,gp *g,next bool){
	if next{
		retryNext:
			oldnext := _p_.runnext
			if !_p_.runnext.cas(oldnext,guintptr(unsafe.Pointer(gp))){
				goto retryNext
			}
			if oldnext == 0{
				return
			}
		// Kick the old runnext out to the regular run queue.
		//原来优先队列的g会被放到本地队列
		gp = oldnext.ptr()
	}
retry:
	h :=atomic.LoadAcq(&_p_.runqhead)
	t :=_p_.runqtail
	//如果本地队列没慢,则放入本地队列
	if t-h < uint32(len(_p_.runq)){
		_p_.runq[t%uint32(len(_p_.runq))].set(gp)
		atomic.StoreRel(&_p_.runqtail, t+1) // store-release, makes the item available for consumption
		return
	}
	//本地队列已满,放入全局队列
	if runqputslow(_p_,gp,h,t){
		return
	}
	goto retry
}

// Put g and a batch of work from local runnable queue on global queue.
// Executed only by the owner P.
//将g放到全局队列
func runqputslow(_p_ *p,gp *g,h,t uint32)bool{
	var batch [len(_p_.runq)/2 + 1]*g

	//从P本地转移一半到全局队列
	// First, grab a batch from local queue.
	n := t - h
	n = n / 2
	if n != uint32(len(_p_.runq)/2){
		throw("runqputslow: queue is not full")
	}
	//从列表头部开始读取
	for i := uint32(0);i < n;i++{
		batch[i] =_p_.runq[(h+i)%uint32(len(_p_.runq))].ptr()
	}
	//调整p队列头部的位置
	batch[n] = gp

	// Link the goroutines.将g像链表一样链接起来
	for i :=uint32(0);i < n;i++{
		batch[i].schedlink.set(batch[i+1])
	}

	var q gQueue
	q.head.set(batch[0])
	q.tail.set(batch[n])

	// Now put the batch on global queue.插入到全局队列
	lock(&sched.lock)
	globrunqputbatch(&q,int32(n+1))
	unlock(&sched.lock)
	return true
}

// Get g from local runnable queue.
// If inheritTime is true, gp should inherit the remaining time in the
// current time slice. Otherwise, it should start a new time slice.
// Executed only by the owner P.
func runqget(_p_ *p) (gp *g, inheritTime bool) {
	// If there's a runnext, it's the next G to run.
	//从优先队列中获取
	for{
		next :=_p_.runnext
		if next == 0{
			break
		}
		if _p_.runnext.cas(next,0){
			return next.ptr(),true
		}
	}

	for{
		h :=atomic.LoadAcq(&_p_.runqhead) //load-acquire, synchronize with other consumers
		t := _p_.runqtail
		if h == t {
			return nil,false
		}
		gp := _p_.runq[h%uint32(len(_p_.runq))].ptr()
		if atomic.CasRel(&_p_.runqhead, h, h+1) { // cas-release, commits consume
			return gp, false
		}
	}
}

// Put gp at the head of the global runnable queue.
// Sched must be locked.
// May run during STW, so write barriers are not allowed.
//go:nowritebarrierrec
func globrunqputhead(gp *g) {
	sched.runq.push(gp)
	sched.runqsize++
}

// Put a batch of runnable goroutines on the global runnable queue.
// This clears *batch.
// Sched must be locked.
func globrunqputbatch(batch *gQueue,n int32){
	sched.runq.pushBackAll(*batch)
	sched.runqsize +=n
	*batch =gQueue{}
}

// A gQueue is a dequeue of Gs linked through g.schedlink. A G can only
// be on one gQueue or gList at a time.
type gQueue struct{
	head guintptr
	tail guintptr
}

// push adds gp to the head of q.
func (q *gQueue) push(gp *g) {
	q.head.set(gp)
	if q.tail == 0 {
		q.tail.set(gp)
	}
}

func (q *gQueue)pushBackAll(q2 gQueue){
	if q2.tail == 0{
		return
	}
	q2.tail.ptr().schedlink = 0
	if q.tail != 0{
		q.tail.ptr().schedlink = q2.head
	}else{
		q.head = q2.head
	}
	q.tail = q2.tail
}

// A gList is a list of Gs linked through g.schedlink. A G can only be
// on one gQueue or gList at a time.
type gList struct{
	head guintptr
}

// empty reports whether l is empty.
func (l *gList)empty()bool{
	return l.head  == 0
}

// push adds gp to the head of l.
func (l *gList)push(gp *g){
	gp.schedlink =l.head
	l.head.set(gp)
}

// pop removes and returns the head of l. If l is empty, it returns nil.
func (l *gList)pop()*g{
	gp :=l.head.ptr()
	if gp != nil{
		l.head =gp.schedlink
	}
	return gp
}

