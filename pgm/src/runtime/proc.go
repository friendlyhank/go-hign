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
	mcache0 *mcache //m0的缓存
)

//执行runtime初始化函数init()
//go:linkname runtime_inittask runtime..inittask
var runtime_inittask initTask

//执行用户逻辑代码的init()
//go:linkname main_inittask main..inittask
var main_inittask initTask

// main_init_done is a signal used by cgocallbackg that initialization
// has been completed. It is made before _cgo_notify_runtime_init_done,
// so all cgo calls can rely on it existing. When main_init is complete,
// it is closed, meaning cgocallbackg can reliably receive from it.
//main_init_done单独被cgocallbackg方法使用,保证在执行cgo调用之前main init先完成初始化
var main_init_done chan bool

//main_main和main.main链接
//go:linkname main_main main.main
func main_main()

//m0启动干活之后其他的m才能被创建启动
// mainStarted indicates that the main M has started.
var mainStarted bool

// runtimeInitTime is the nanotime() at which the runtime started.
var runtimeInitTime int64

// Value to use for signal mask for newly created M's.
//TODO HANK return
var initSigmask sigset

// The main goroutine
func main(){
	g :=getg()

	// Racectx of m0->g0 is used only as the parent of the main goroutine.
	// It must not be used for anything else.
	g.m.g0.racectx = 0

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

	if GOARCH != "wasm"{// no threads on wasm yet, so no sysmon
		systemstack(func() {
			newm(sysmon,nil,-1)//创建新的m,还会启动监控sysmon
		})
	}

	if g.m != &m0{
		throw("runtime.main not on m0")
	}

	doInit(&runtime_inittask)//执行runtime初始化函数init()
	if nanotime() == 0{
		throw("nanotime returning zero")
	}

	// Record when the world started.
	runtimeInitTime = nanotime()

	main_init_done = make(chan bool)

	doInit(&main_inittask) //执行用户逻辑代码的init()

	close(main_init_done)

	fn := main_main// make an indirect call, as the linker doesn't know the address of the main package when laying down the runtime
	fn()

	exit(0)
}

func goready(gp *g,traceskip int){
	systemstack(func() {
		ready(gp,traceskip,true)
	})
}

// funcPC returns the entry PC of the function f.
// It assumes that f is a func value. Otherwise the behavior is undefined.
// CAREFUL: In programs with plugins, funcPC can return different values
// for the same function (because there are actually multiple copies of
// the same function in the address space). To be safe, don't use the
// results of this function in any == expression. It is only safe to
// use the result as an address at which to start executing code.
//go:nosplit
func funcPC(f interface{}) uintptr {
	return *(*uintptr)(efaceOf(&f).data)
}

//runtime/asm_amd64.s
//go:nosplit
func badctxt() {
	throw("ctxt != 0")
}

var (
	allgs []*g //所有的g
	allglock mutex//操作所有g的锁
)

func allgadd(gp *g){
	if readgstatus(gp) ==_Gidle{
		throw("allgadd: bad status Gidle")
	}

	lock(&allglock)
	allgs = append(allgs,gp)
	allglen = uintptr(len(allgs))
	unlock(&allglock)
}

// All reads and writes of g's status go through readgstatus, casgstatus
// castogscanstatus, casfrom_Gscanstatus.
//go:nosplit
func readgstatus(gp *g)uint32{
	return atomic.Load(&gp.atomicstatus)
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
//调度器的初始化
func schedinit(){
	lockInit(&sched.lock,lockRankSched)
	lockInit(&sched.sysmonlock, lockRankSysmon)
	lockInit(&sched.deferlock,lockRankDefer)
	lockInit(&sched.sudoglock, lockRankSudog)
	lockInit(&deadlock, lockRankDeadlock)
	lockInit(&paniclk, lockRankPanic)
	lockInit(&allglock, lockRankAllg)
	lockInit(&allpLock, lockRankAllp)
	lockInit(&reflectOffs.lock,lockRankReflectOffs)
	lockInit(&finlock,lockRankFin)
	lockInit(&trace.bufLock, lockRankTraceBuf)
	lockInit(&trace.stringsLock, lockRankTraceStrings)
	lockInit(&trace.lock, lockRankTrace)
	lockInit(&cpuprof.lock, lockRankCpuprof)
	lockInit(&trace.stackTab.lock, lockRankTraceStackTab)


	// raceinit must be the first call to race detector.
	// In particular, it must be done before mallocinit below calls racemapshadow.
	_g_ :=getg()

	//最大的线程数量限制
	sched.maxmcount =10000

	tracebackinit()
	//校验moduledata数据
	moduledataverify()
	//栈相关的初始化
	stackinit()
	//内存分配器初始化
	mallocinit()
	//初始化m
	mcommoninit(_g_.m,-1)
	itabsinit() // uses activeModules

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

// Mark gp ready to run.
//标记g的ready状态然后去运行
func ready(gp *g, traceskip int, next bool) {

	status :=readgstatus(gp)

	// Mark runnable.
	_g_ := getg()
	mp :=acquirem()
	if status^_Gscan != _Gwaiting{//如果不是运行状态
		throw("bad g->status in ready")
	}
}

// freezeStopWait is a large value that freezetheworld sets
// sched.stopwait to in order to request that all Gs permanently stop.
const freezeStopWait = 0x7fffffff

// All reads and writes of g's status go through readgstatus, casgstatus
// castogscanstatus, casfrom_Gscanstatus.
//go:nosplit
func readgstatus(gp *g) uint32 {
	return atomic.Load(&gp.atomicstatus)
}

// freezing is set to non-zero if the runtime is trying to freeze the
// world.
var freezing uint32

// Similar to stopTheWorld but best-effort and can be called several times.
// There is no reverse operation, used during crashing.
// This function must not lock any mutexes.
//比stopTheWorld 更小力度的,常用于抛出异常,这时候抢占所有的g
func freezetheworld() {
	atomic.Store(&freezing,1)
	// stopwait and preemption requests can be lost
	// due to races with concurrently executing threads,
	// so try several times
	for i :=0; i <5;i++{
		// this should tell the scheduler to not start any new goroutines
		sched.stopwait = freezeStopWait
		atomic.Store(&sched.gcwaiting,1)
		// this should stop running goroutines
		if !preemptall(){
			break // no running goroutines
		}
		usleep(1000)
	}
	// to be sure
	usleep(1000)
	preemptall()
	usleep(1000)
}

// If asked to move to or from a Gscanstatus this will throw. Use the castogscanstatus
// and casfrom_Gscanstatus instead.
// casgstatus will loop if the g->atomicstatus is in a Gscan status until the routine that
// put it in the Gscan state is finished.
//修改g的状态
//go:nosplit
func casgstatus(gp *g, oldval, newval uint32) {
	for i := 0; !atomic.Cas(&gp.atomicstatus,oldval,newval);i++{

	}
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

	osStack := _g_.stack.lo == 0
	if osStack {

	}
	mstart1()
	//Exit this thread. 停止这个线程
}

func mstart1(){
	_g_ := getg()
	if _g_ != _g_.m.g0{
		throw("bad runtime·mstart")
	}
	minit()

	// Install signal handlers; after minit so that minit can
	// prepare the thread to be able to handle the signals.
	if _g_.m == &m0{
		mstartm0()//这里还可以创建额外的m
	}

	//有些m的启动函数会绑定runtime.sysmon()
	if fn :=_g_.m.mstartfn;fn != nil{
		fn()
	}

	// Record the caller for use as the top of stack in mcall and
	// for terminating the thread.
	// We're never coming back to mstart1 after we call schedule,
	// so other calls can reuse the current frame.
	schedule()
}

// mstartm0 implements part of mstart1 that only runs on the m0.
//
// Write barriers are allowed here because we know the GC can't be
// running yet, so they'll be no-ops.
//
//m0的启动可以根据cpu的数量创建出更多的m出来
//go:yeswritebarrierrec
func mstartm0(){
	if GOOS == "windows"{
		newextram()
	}
}

var extram uintptr
var extraMCount uint32 // Protected by lockextra
var extraMWaiters uint32

//生成m的锁,保证只搞一次
func lockextra(nilokay bool) *m {
	const locked = 1

	incr := false
	for{
		old := atomic.Loaduintptr(&extram)
		if old == locked {
			osyield()
			continue
		}
		if old == 0 && !nilokay {
			if !incr {
				// Add 1 to the number of threads
				// waiting for an M.
				// This is cleared by newextram.
				atomic.Xadd(&extraMWaiters, 1)
				incr = true
			}
			usleep(1)
			continue
		}
		if atomic.Casuintptr(&extram, old, locked) {
			return (*m)(unsafe.Pointer(old))
		}
		osyield()
		continue
	}
}

//go:nosplit
func unlockextra(mp *m) {
	atomic.Storeuintptr(&extram, uintptr(unsafe.Pointer(mp)))
}

// Create a new m. It will start off with a call to fn, or else the scheduler.
// fn needs to be static and not a heap allocated closure.
// May run with m.p==nil, so write barriers are not allowed.
//
// id is optional pre-allocated m ID. Omit by passing -1.
//创建新的m,带有启动的函数并且会绑定线程
//go:nowritebarrierrec
func newm(fn func(), _p_ *p, id int64) {
	mp :=allocm(_p_,fn,id)
	newm1(mp)
}

func newm1(mp *m){
	newosproc(mp)//绑定线程
}

// Allocate a new m unassociated with any thread.
// Can use p for allocation context if needed.
// fn is recorded as the new m's m.mstartfn.
// id is optional pre-allocated m ID. Omit by passing -1.
//
// This function is allowed to have write barriers even if the caller
// isn't because it borrows _p_.
//
//分配一个新的m并进行初始化
//go:yeswritebarrierrec
func allocm(_p_ *p, fn func(), id int64) *m {
	_g_ := getg()
	acquirem() // disable GC because it can be called from sysmon 加锁
	if _g_.m.p == 0{
		acquirep(_p_)
	}

	mp := new(m)
	mp.mstartfn = fn
	mcommoninit(mp,id)
	releasem(_g_.m) //解锁
	return nil
}

// newextram allocates m's and puts them on the extra list.
// It is called with a working local m, so that it can do things
// like call schedlock and allocate.
func newextram() {
	c := atomic.Xchg(&extraMWaiters, 0)
	if c > 0 {

	}else {
		// Make sure there is at least one extra M.保证至少有一个额外的m
		mp := lockextra(true)
		unlockextra(mp)
		if mp == nil {
			oneNewExtraM()
		}
	}
}

// oneNewExtraM allocates an m and puts it on the extra list.
func oneNewExtraM(){
	// Create extra goroutine locked to extra m.
	// The goroutine is the context in which the cgo callback will run.
	// The sched.pc will never be returned to, but setting it to
	// goexit makes clear to the traceback routines where
	// the goroutine stack ends.
	mp := allocm(nil, nil, -1)
	gp := malg(4096)
	gp.sched.g = guintptr(unsafe.Pointer(gp))
	// malg returns status as _Gidle. Change to _Gdead before
	// adding to allg where GC can see it. We use _Gdead to hide
	// this from tracebacks and stack scans since it isn't a
	// "real" goroutine until needm grabs it.
	casgstatus(gp, _Gidle, _Gdead)
	gp.m = mp
	mp.curg = gp
	// put on allg for garbage collector 新的g放入全局allg
	allgadd(gp)

	// Add m to the extra list.
	mnext := lockextra(true)
	mp.schedlink.set(mnext)
	extraMCount++
	unlockextra(mp)
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
		// Check the global runnable queue once in a while to ensure fairness.
		// Otherwise two goroutines can completely occupy the local runqueue
		// by constantly respawning each other.
		if _g_.m.p.ptr().schedtick%61 == 0 && sched.runqsize > 0{
			lock(&sched.lock)
			gp  =globrunqget(_g_.m.p.ptr(),1)
			unlock(&sched.lock)
		}
	}

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

// Finishes execution of the current goroutine.
func goexit1(){
	mcall(goexit0)
}

// goexit continuation on g0.
func goexit0(gp *g){

	casgstatus(gp,_Grunning,_Gdead)

	schedule()
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

	// Assign gp.m before entering _Grunning so running Gs have an
	// M.
	_g_.m.curg = gp
	gp.m = _g_.m
	casgstatus(gp,_Grunnable,_Grunning)

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

// init initializes pp, which may be a freshly allocated p or a
// previously destroyed p, and transitions it to status _Pgcstop.
//初始化p
func (pp *p)init(id int32){
	pp.id = id
	pp.status =_Pgcstop
	if pp.mcache == nil{
		if id == 0{
			if mcache0 == nil{
				throw("missing mcache?")
			}
			// Use the bootstrap mcache0. Only one P will get
			// mcache0: the one with ID 0.
			//p0绑定mcache0
			pp.mcache = mcache0
		}else{
			pp.mcache =allocmcache()
		}
	}
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
	//释放线程缓存
	freemcache(pp.mcache)
	pp.mcache = nil
	pp.status = _Pdead
}


// Change number of processors. The world is stopped, sched is locked.
// gcworkbufs are not being modified by either the GC or
// the write barrier code.
// Returns list of Ps with local work, they need to be scheduled by the caller.
//调整p的数量,startTheWorld,schedinit的时候会调用
//p0在这里和m0,g0绑定
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
		p.status =_Pidle
		//m和p进行绑定
		acquirep(p)
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

	var runnablePs *p
	for i := nprocs -1;i >= 0;i --{
		p := allp[i]
		//当前的p已经绑定m并且已经是使用状态
		if _g_.m.p.ptr() == p{
			continue
		}
		//如果是未使用的p,则放入空闲链表
		if runqempty(p){
			pidleput(p)
		}else{

		}
	}
	var int32p *int32 = &gomaxprocs // make compiler check that gomaxprocs is an int32
	atomic.Store((*uint32)(unsafe.Pointer(int32p)), uint32(nprocs))
	return runnablePs
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
	wirep(_p_)
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

// Allocate a new g, with a stack big enough for stacksize bytes.
//内存分配一个g
func malg(stacksize int32)*g{
	newg := new(g)
	if stacksize  > 0{
		stacksize = round2(_StackSystem + stacksize)
		systemstack(func() {
			newg.stack = stackalloc(uint32(stacksize))
		})
		newg.stackguard0 =newg.stack.lo + _StackGuard //栈大小的范围,超出会扩容
	}
	return newg
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
	argp := add(unsafe.Pointer(&fn),sys.PtrSize)
	gp := getg()
	systemstack(func(){
		//获取一个新的g
		newg := newproc1(fn,argp,siz,gp)

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
func newproc1(fn *funcval,argp unsafe.Pointer,narg int32,callergp *g)*g{
	_g_ := getg()

	if fn == nil{
		_g_.m.throwing = -1 // do not dump full stacks
		throw("go of nil func value")
	}
	acquirem()
	siz :=narg
	siz = (siz + 7) &^ 7

	_p_ := _g_.m.p.ptr()
	newg :=gfget(_p_) //从全局或当前的p中获取一个空闲的g
	//如果获取不到空闲的g,直接新建一个g
	if newg == nil{
		newg = malg(_StackMin)
		casgstatus(newg,_Gidle,_Gdead)//变更状态为没有被使用,没有执行代码,但有分配栈的状态
		allgadd(newg)
	}
	if newg.stack.hi  == 0{
		throw("newproc1: newg missing stack")
	}
	if readgstatus(newg) !=_Gdead{
		throw("newproc1: new g is not Gdead")
	}

	totalSize := 4*sys.RegSize + uintptr(siz) + sys.MinFrameSize // extra space in case of reads slightly beyond frame
	totalSize +=-totalSize & (sys.SpAlign - 1)
	//确定sp和参数的入栈位置
	sp :=newg.stack.hi - totalSize
	spArg := sp
	if narg > 0{
		//将执行的参数拷贝入栈
		memmove(unsafe.Pointer(spArg),argp,uintptr(narg))
	}
	newg.sched.sp = sp //记录栈指针
	//此处保存的goexit地址,这是循环调度的关键
	newg.sched.pc  = funcPC(goexit) + sys.PCQuantum// +PCQuantum so that previous instruction is in same function
	newg.sched.g =guintptr(unsafe.Pointer(newg))
	gostartcallfn(&newg.sched,fn)//这里会导致sched.pc和sched.sp的变化
	newg.startpc = fn.fn //保存的要执行的逻辑代码
	casgstatus(newg,_Gdead,_Grunnable) //修改状态为可运行状态
	releasem(_g_.m)

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

// Try get a batch of G's from the global runnable queue.
// Sched must be locked.
func globrunqget(_p_ *p, max int32) *g {
	if sched.runqsize == 0{
		return nil
	}

	n :=sched.runqsize/gomaxprocs + 1
	if n > sched.runqsize{
		n = sched.runqsize
	}
	if max > 0 && n > max{
		n = max
	}
	if n > int32(len(_p_.runq))/2{
		n = int32(len(_p_.runq)) / 2
	}

	sched.runqsize -= n

	gp :=sched.runq.pop()
	n--
	for ; n > 0; n-- {
		gp1 := sched.runq.pop()
		runqput(_p_,gp1,false)
	}
	return gp
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

// Always runs without a P, so write barriers are not allowed.
//负责抢占和负责网络事件
//go:nowritebarrierrec
func sysmon(){

}

// Tell all goroutines that they have been preempted and they should stop.
// This function is purely best-effort. It can fail to inform a goroutine if a
// processor just started running it.
// No locks need to be held.
// Returns true if preemption request was issued to at least one goroutine.
//抢占所有的g,stopTheWorld和freezetheworl的时候会调用
func preemptall()bool{
	res := false
	for _,_p_ := range allp{
		if _p_.status !=_Grunning{
			continue
		}
		if preemptone(_p_){
			res = true
		}
	}
	return res
}

// Tell the goroutine running on processor P to stop.
// This function is purely best-effort. It can incorrectly fail to inform the
// goroutine. It can send inform the wrong goroutine. Even if it informs the
// correct goroutine, that goroutine might ignore the request if it is
// simultaneously executing newstack.
// No lock needs to be held.
// Returns true if preemption request was issued.
// The actual preemption will happen at some point in the future
// and will be indicated by the gp->status no longer being
// Grunning
func preemptone(_p_ *p) bool {
	mp :=_p_.m.ptr()
	if mp == nil || mp == getg().m{
		return false
	}
	gp := mp.curg
	if gp == nil || gp == mp.g0{
		return false
	}

	// Every call in a go routine checks for stack overflow by
	// comparing the current stack pointer to gp->stackguard0.
	// Setting gp->stackguard0 to StackPreempt folds
	// preemption into the normal stack overflow check.
	gp.stackguard0 =stackPreempt

	// Request an async preemption of this P.
	if preemptMSupported && debug.asyncpreemptoff == 0 {
		_p_.preempt = true
		preemptM(mp)
	}
	return true
}

// Standard syscall entry used by the go syscall library and normal cgo calls.
//
// This is exported via linkname to assembly in the syscall package.
//
//go:nosplit
//go:linkname entersyscall
func entersyscall() {
	reentersyscall(getcallerpc(), getcallersp())
}

func entersyscall_sysmon() {
	lock(&sched.lock)
	if atomic.Load(&sched.sysmonwait) != 0 {
		atomic.Store(&sched.sysmonwait, 0)
		notewakeup(&sched.sysmonnote)
	}
	unlock(&sched.lock)
}

// The goroutine g is about to enter a system call.
// Record that it's not using the cpu anymore.
// This is called only from the go syscall library and cgocall,
// not from the low-level system calls used by the runtime.
//
// Entersyscall cannot split the stack: the gosave must
// make g->sched refer to the caller's stack segment, because
// entersyscall is going to return immediately after.
//
// Nothing entersyscall calls can split the stack either.
// We cannot safely move the stack during an active call to syscall,
// because we do not know which of the uintptr arguments are
// really pointers (back into the stack).
// In practice, this means that we make the fast path run through
// entersyscall doing no-split things, and the slow path has to use systemstack
// to run bigger things on the system stack.
//
// reentersyscall is the entry point used by cgo callbacks, where explicitly
// saved SP and PC are restored. This is needed when exitsyscall will be called
// from a function further up in the call stack than the parent, as g->syscallsp
// must always point to a valid stack frame. entersyscall below is the normal
// entry point for syscalls, which obtains the SP and PC from the caller.
//
// Syscall tracing:
// At the start of a syscall we emit traceGoSysCall to capture the stack trace.
// If the syscall does not block, that is it, we do not emit any other events.
// If the syscall blocks (that is, P is retaken), retaker emits traceGoSysBlock;
// when syscall returns we emit traceGoSysExit and when the goroutine starts running
// (potentially instantly, if exitsyscallfast returns true) we emit traceGoStart.
// To ensure that traceGoSysExit is emitted strictly after traceGoSysBlock,
// we remember current value of syscalltick in m (_g_.m.syscalltick = _g_.m.p.ptr().syscalltick),
// whoever emits traceGoSysBlock increments p.syscalltick afterwards;
// and we wait for the increment before emitting traceGoSysExit.
// Note that the increment is done even if tracing is not enabled,
// because tracing can be enabled in the middle of syscall. We don't want the wait to hang.
//
//go:nosplit
func reentersyscall(pc, sp uintptr) {
	_g_ := getg()

	// Disable preemption because during this function g is in Gsyscall status,
	// but can have inconsistent g->sched, do not let GC observe it.
	_g_.m.locks++

	// Entersyscall must not call any function that might split/grow the stack.
	// (See details in comment above.)
	// Catch calls that might, by replacing the stack guard with something that
	// will trip any stack check and leaving a flag to tell newstack to die.
	_g_.stackguard0 = stackPreempt
	_g_.throwsplit = true

	// Leave SP around for GC and traceback.
	save(pc, sp)
	_g_.syscallsp = sp
	_g_.syscallpc = pc
	casgstatus(_g_, _Grunning, _Gsyscall)
	if _g_.syscallsp < _g_.stack.lo || _g_.stack.hi < _g_.syscallsp {
		systemstack(func() {
			print("entersyscall inconsistent ", hex(_g_.syscallsp), " [", hex(_g_.stack.lo), ",", hex(_g_.stack.hi), "]\n")
			throw("entersyscall")
		})
	}

	if trace.enabled {
		systemstack(traceGoSysCall)
		// systemstack itself clobbers g.sched.{pc,sp} and we might
		// need them later when the G is genuinely blocked in a
		// syscall
		save(pc, sp)
	}

	if atomic.Load(&sched.sysmonwait) != 0 {
		systemstack(entersyscall_sysmon)
		save(pc, sp)
	}

	if _g_.m.p.ptr().runSafePointFn != 0 {
		// runSafePointFn may stack split if run on this stack
		systemstack(runSafePointFn)
		save(pc, sp)
	}

	_g_.m.syscalltick = _g_.m.p.ptr().syscalltick
	_g_.sysblocktraced = true
	pp := _g_.m.p.ptr()
	pp.m = 0
	_g_.m.oldp.set(pp)
	_g_.m.p = 0
	atomic.Store(&pp.status, _Psyscall)
	if sched.gcwaiting != 0 {
		systemstack(entersyscall_gcwait)
		save(pc, sp)
	}

	_g_.m.locks--
}

func entersyscall_gcwait() {
	_g_ := getg()
	_p_ := _g_.m.oldp.ptr()

	lock(&sched.lock)
	if sched.stopwait > 0 && atomic.Cas(&_p_.status, _Psyscall, _Pgcstop) {
		if trace.enabled {
			traceGoSysBlock(_p_)
			traceProcStop(_p_)
		}
		_p_.syscalltick++
		if sched.stopwait--; sched.stopwait == 0 {
			notewakeup(&sched.stopnote)
		}
	}
	unlock(&sched.lock)
}

// runSafePointFn runs the safe point function, if any, for this P.
// This should be called like
//
//     if getg().m.p.runSafePointFn != 0 {
//         runSafePointFn()
//     }
//
// runSafePointFn must be checked on any transition in to _Pidle or
// _Psyscall to avoid a race where forEachP sees that the P is running
// just before the P goes into _Pidle/_Psyscall and neither forEachP
// nor the P run the safe-point function.
func runSafePointFn() {
	p := getg().m.p.ptr()
	// Resolve the race between forEachP running the safe-point
	// function on this P's behalf and this P running the
	// safe-point function directly.
	if !atomic.Cas(&p.runSafePointFn, 1, 0) {
		return
	}
	sched.safePointFn(p)
	lock(&sched.lock)
	sched.safePointWait--
	if sched.safePointWait == 0 {
		notewakeup(&sched.safePointNote)
	}
	unlock(&sched.lock)
}


// save updates getg().sched to refer to pc and sp so that a following
// gogo will restore pc and sp.
//
// save must not have write barriers because invoking a write barrier
// can clobber getg().sched.
//
//go:nosplit
//go:nowritebarrierrec
func save(pc, sp uintptr) {
	_g_ := getg()

	_g_.sched.pc = pc
	_g_.sched.sp = sp
	_g_.sched.lr = 0
	_g_.sched.ret = 0
	_g_.sched.g = guintptr(unsafe.Pointer(_g_))
	// We need to ensure ctxt is zero, but can't have a write
	// barrier here. However, it should always already be zero.
	// Assert that.
	if _g_.sched.ctxt != nil {
		badctxt()
	}
}

// The goroutine g exited its system call.
// Arrange for it to run on a cpu again.
// This is called only from the go syscall library, not
// from the low-level system calls used by the runtime.
//
// Write barriers are not allowed because our P may have been stolen.
//
// This is exported via linkname to assembly in the syscall package.
//
//go:nosplit
//go:nowritebarrierrec
//go:linkname exitsyscall
func exitsyscall() {
	_g_ := getg()

	_g_.m.locks++ // see comment in entersyscall
	if getcallersp() > _g_.syscallsp {
		throw("exitsyscall: syscall frame is no longer valid")
	}

	_g_.waitsince = 0
	oldp := _g_.m.oldp.ptr()
	_g_.m.oldp = 0
	if exitsyscallfast(oldp) {
		if trace.enabled {
			if oldp != _g_.m.p.ptr() || _g_.m.syscalltick != _g_.m.p.ptr().syscalltick {
				systemstack(traceGoStart)
			}
		}
		// There's a cpu for us, so we can run.
		_g_.m.p.ptr().syscalltick++
		// We need to cas the status and scan before resuming...
		casgstatus(_g_, _Gsyscall, _Grunning)

		// Garbage collector isn't running (since we are),
		// so okay to clear syscallsp.
		_g_.syscallsp = 0
		_g_.m.locks--
		if _g_.preempt {
			// restore the preemption request in case we've cleared it in newstack
			_g_.stackguard0 = stackPreempt
		} else {
			// otherwise restore the real _StackGuard, we've spoiled it in entersyscall/entersyscallblock
			_g_.stackguard0 = _g_.stack.lo + _StackGuard
		}
		_g_.throwsplit = false

		if sched.disable.user && !schedEnabled(_g_) {
			// Scheduling of this goroutine is disabled.
			Gosched()
		}

		return
	}

	_g_.sysexitticks = 0
	if trace.enabled {
		// Wait till traceGoSysBlock event is emitted.
		// This ensures consistency of the trace (the goroutine is started after it is blocked).
		for oldp != nil && oldp.syscalltick == _g_.m.syscalltick {
			osyield()
		}
		// We can't trace syscall exit right now because we don't have a P.
		// Tracing code can invoke write barriers that cannot run without a P.
		// So instead we remember the syscall exit time and emit the event
		// in execute when we have a P.
		_g_.sysexitticks = cputicks()
	}

	_g_.m.locks--

	// Call the scheduler.
	mcall(exitsyscall0)

	// Scheduler returned, so we're allowed to run now.
	// Delete the syscallsp information that we left for
	// the garbage collector during the system call.
	// Must wait until now because until gosched returns
	// we don't know for sure that the garbage collector
	// is not running.
	_g_.syscallsp = 0
	_g_.m.p.ptr().syscalltick++
	_g_.throwsplit = false
}

// exitsyscall slow path on g0.
// Failed to acquire P, enqueue gp as runnable.
//
//go:nowritebarrierrec
func exitsyscall0(gp *g) {
	_g_ := getg()

	casgstatus(gp, _Gsyscall, _Grunnable)
	dropg()
	lock(&sched.lock)
	var _p_ *p
	if schedEnabled(_g_) {
		_p_ = pidleget()
	}
	if _p_ == nil {
		globrunqput(gp)
	} else if atomic.Load(&sched.sysmonwait) != 0 {
		atomic.Store(&sched.sysmonwait, 0)
		notewakeup(&sched.sysmonnote)
	}
	unlock(&sched.lock)
	if _p_ != nil {
		acquirep(_p_)
		execute(gp, false) // Never returns.
	}
	if _g_.m.lockedg != 0 {
		// Wait until another thread schedules gp and so m again.
		stoplockedm()
		execute(gp, false) // Never returns.
	}
	stopm()
	schedule() // Never returns.
}

// Stops execution of the current m until new work is available.
// Returns with acquired P.
func stopm() {
	_g_ := getg()

	if _g_.m.locks != 0 {
		throw("stopm holding locks")
	}
	if _g_.m.p != 0 {
		throw("stopm holding p")
	}
	if _g_.m.spinning {
		throw("stopm spinning")
	}

	lock(&sched.lock)
	mput(_g_.m)
	unlock(&sched.lock)
	notesleep(&_g_.m.park)
	noteclear(&_g_.m.park)
	acquirep(_g_.m.nextp.ptr())
	_g_.m.nextp = 0
}

// Put mp on midle list.
// Sched must be locked.
// May run during STW, so write barriers are not allowed.
//go:nowritebarrierrec
func mput(mp *m) {
	mp.schedlink = sched.midle
	sched.midle.set(mp)
	sched.nmidle++
	checkdead()
}

// Check for deadlock situation.
// The check is based on number of running M's, if 0 -> deadlock.
// sched.lock must be held.
func checkdead() {
	// For -buildmode=c-shared or -buildmode=c-archive it's OK if
	// there are no running goroutines. The calling program is
	// assumed to be running.
	if islibrary || isarchive {
		return
	}

	// If we are dying because of a signal caught on an already idle thread,
	// freezetheworld will cause all running threads to block.
	// And runtime will essentially enter into deadlock state,
	// except that there is a thread that will call exit soon.
	if panicking > 0 {
		return
	}

	// If we are not running under cgo, but we have an extra M then account
	// for it. (It is possible to have an extra M on Windows without cgo to
	// accommodate callbacks created by syscall.NewCallback. See issue #6751
	// for details.)
	var run0 int32
	if !iscgo && cgoHasExtraM {
		mp := lockextra(true)
		haveExtraM := extraMCount > 0
		unlockextra(mp)
		if haveExtraM {
			run0 = 1
		}
	}

	run := mcount() - sched.nmidle - sched.nmidlelocked - sched.nmsys
	if run > run0 {
		return
	}
	if run < 0 {
		print("runtime: checkdead: nmidle=", sched.nmidle, " nmidlelocked=", sched.nmidlelocked, " mcount=", mcount(), " nmsys=", sched.nmsys, "\n")
		throw("checkdead: inconsistent counts")
	}

	grunning := 0
	lock(&allglock)
	for i := 0; i < len(allgs); i++ {
		gp := allgs[i]
		if isSystemGoroutine(gp, false) {
			continue
		}
		s := readgstatus(gp)
		switch s &^ _Gscan {
		case _Gwaiting,
			_Gpreempted:
			grunning++
		case _Grunnable,
			_Grunning,
			_Gsyscall:
			unlock(&allglock)
			print("runtime: checkdead: find g ", gp.goid, " in status ", s, "\n")
			throw("checkdead: runnable g")
		}
	}
	unlock(&allglock)
	if grunning == 0 { // possible if main goroutine calls runtime·Goexit()
		unlock(&sched.lock) // unlock so that GODEBUG=scheddetail=1 doesn't hang
		throw("no goroutines (main called runtime.Goexit) - deadlock!")
	}

	// Maybe jump time forward for playground.
	if faketime != 0 {
		when, _p_ := timeSleepUntil()
		if _p_ != nil {
			faketime = when
			for pp := &sched.pidle; *pp != 0; pp = &(*pp).ptr().link {
				if (*pp).ptr() == _p_ {
					*pp = _p_.link
					break
				}
			}
			mp := mget()
			if mp == nil {
				// There should always be a free M since
				// nothing is running.
				throw("checkdead: no m for timer")
			}
			mp.nextp.set(_p_)
			notewakeup(&mp.park)
			return
		}
	}

	// There are no goroutines running, so we can look at the P's.
	for _, _p_ := range allp {
		if len(_p_.timers) > 0 {
			return
		}
	}

	getg().m.throwing = -1 // do not dump full stacks
	unlock(&sched.lock)    // unlock so that GODEBUG=scheddetail=1 doesn't hang
	throw("all goroutines are asleep - deadlock!")
}

// Stops execution of the current m that is locked to a g until the g is runnable again.
// Returns with acquired P.
func stoplockedm() {
	_g_ := getg()

	if _g_.m.lockedg == 0 || _g_.m.lockedg.ptr().lockedm.ptr() != _g_.m {
		throw("stoplockedm: inconsistent locking")
	}
	if _g_.m.p != 0 {
		// Schedule another M to run this p.
		_p_ := releasep()
		handoffp(_p_)
	}
	incidlelocked(1)
	// Wait until another thread schedules lockedg again.
	notesleep(&_g_.m.park)
	noteclear(&_g_.m.park)
	status := readgstatus(_g_.m.lockedg.ptr())
	if status&^_Gscan != _Grunnable {
		print("runtime:stoplockedm: g is not Grunnable or Gscanrunnable\n")
		dumpgstatus(_g_)
		throw("stoplockedm: not runnable")
	}
	acquirep(_g_.m.nextp.ptr())
	_g_.m.nextp = 0
}

func dumpgstatus(gp *g) {
	_g_ := getg()
	print("runtime: gp: gp=", gp, ", goid=", gp.goid, ", gp->atomicstatus=", readgstatus(gp), "\n")
	print("runtime:  g:  g=", _g_, ", goid=", _g_.goid, ",  g->atomicstatus=", readgstatus(_g_), "\n")
}

func incidlelocked(v int32) {
	lock(&sched.lock)
	sched.nmidlelocked += v
	if v > 0 {
		checkdead()
	}
	unlock(&sched.lock)
}

func mspinning() {
	// startm's caller incremented nmspinning. Set the new M's spinning.
	getg().m.spinning = true
}

// Schedules some M to run the p (creates an M if necessary).
// If p==nil, tries to get an idle P, if no idle P's does nothing.
// May run with m.p==nil, so write barriers are not allowed.
// If spinning is set, the caller has incremented nmspinning and startm will
// either decrement nmspinning or set m.spinning in the newly started M.
//go:nowritebarrierrec
func startm(_p_ *p, spinning bool) {
	lock(&sched.lock)
	if _p_ == nil {
		_p_ = pidleget()
		if _p_ == nil {
			unlock(&sched.lock)
			if spinning {
				// The caller incremented nmspinning, but there are no idle Ps,
				// so it's okay to just undo the increment and give up.
				if int32(atomic.Xadd(&sched.nmspinning, -1)) < 0 {
					throw("startm: negative nmspinning")
				}
			}
			return
		}
	}
	mp := mget()
	if mp == nil {
		// No M is available, we must drop sched.lock and call newm.
		// However, we already own a P to assign to the M.
		//
		// Once sched.lock is released, another G (e.g., in a syscall),
		// could find no idle P while checkdead finds a runnable G but
		// no running M's because this new M hasn't started yet, thus
		// throwing in an apparent deadlock.
		//
		// Avoid this situation by pre-allocating the ID for the new M,
		// thus marking it as 'running' before we drop sched.lock. This
		// new M will eventually run the scheduler to execute any
		// queued G's.
		id := mReserveID()
		unlock(&sched.lock)

		var fn func()
		if spinning {
			// The caller incremented nmspinning, so set m.spinning in the new M.
			fn = mspinning
		}
		newm(fn, _p_, id)
		return
	}
	unlock(&sched.lock)
	if mp.spinning {
		throw("startm: m is spinning")
	}
	if mp.nextp != 0 {
		throw("startm: m has p")
	}
	if spinning && !runqempty(_p_) {
		throw("startm: p has runnable gs")
	}
	// The caller incremented nmspinning, so set m.spinning in the new M.
	mp.spinning = spinning
	mp.nextp.set(_p_)
	notewakeup(&mp.park)
}

// Hands off P from syscall or locked M.
// Always runs without a P, so write barriers are not allowed.
//go:nowritebarrierrec
func handoffp(_p_ *p) {
	// handoffp must start an M in any situation where
	// findrunnable would return a G to run on _p_.

	// if it has local work, start it straight away
	if !runqempty(_p_) || sched.runqsize != 0 {
		startm(_p_, false)
		return
	}
	// if it has GC work, start it straight away
	if gcBlackenEnabled != 0 && gcMarkWorkAvailable(_p_) {
		startm(_p_, false)
		return
	}
	// no local work, check that there are no spinning/idle M's,
	// otherwise our help is not required
	if atomic.Load(&sched.nmspinning)+atomic.Load(&sched.npidle) == 0 && atomic.Cas(&sched.nmspinning, 0, 1) { // TODO: fast atomic
		startm(_p_, true)
		return
	}
	lock(&sched.lock)
	if sched.gcwaiting != 0 {
		_p_.status = _Pgcstop
		sched.stopwait--
		if sched.stopwait == 0 {
			notewakeup(&sched.stopnote)
		}
		unlock(&sched.lock)
		return
	}
	if _p_.runSafePointFn != 0 && atomic.Cas(&_p_.runSafePointFn, 1, 0) {
		sched.safePointFn(_p_)
		sched.safePointWait--
		if sched.safePointWait == 0 {
			notewakeup(&sched.safePointNote)
		}
	}
	if sched.runqsize != 0 {
		unlock(&sched.lock)
		startm(_p_, false)
		return
	}
	// If this is the last running P and nobody is polling network,
	// need to wakeup another M to poll network.
	if sched.npidle == uint32(gomaxprocs-1) && atomic.Load64(&sched.lastpoll) != 0 {
		unlock(&sched.lock)
		startm(_p_, false)
		return
	}
	if when := nobarrierWakeTime(_p_); when != 0 {
		wakeNetPoller(when)
	}
	pidleput(_p_)
	unlock(&sched.lock)
}

// wakeNetPoller wakes up the thread sleeping in the network poller,
// if there is one, and if it isn't going to wake up anyhow before
// the when argument.
func wakeNetPoller(when int64) {
	if atomic.Load64(&sched.lastpoll) == 0 {
		// In findrunnable we ensure that when polling the pollUntil
		// field is either zero or the time to which the current
		// poll is expected to run. This can have a spurious wakeup
		// but should never miss a wakeup.
		pollerPollUntil := int64(atomic.Load64(&sched.pollUntil))
		if pollerPollUntil == 0 || pollerPollUntil > when {
			netpollBreak()
		}
	}
}

// Disassociate p and the current m.
func releasep() *p {
	_g_ := getg()

	if _g_.m.p == 0 {
		throw("releasep: invalid arg")
	}
	_p_ := _g_.m.p.ptr()
	if _p_.m.ptr() != _g_.m || _p_.status != _Prunning {
		print("releasep: m=", _g_.m, " m->p=", _g_.m.p.ptr(), " p->m=", hex(_p_.m), " p->status=", _p_.status, "\n")
		throw("releasep: invalid p state")
	}
	if trace.enabled {
		traceProcStop(_g_.m.p.ptr())
	}
	_g_.m.p = 0
	_p_.m = 0
	_p_.status = _Pidle
	return _p_
}

// Put gp on the global runnable queue.
// Sched must be locked.
// May run during STW, so write barriers are not allowed.
//go:nowritebarrierrec
func globrunqput(gp *g) {
	sched.runq.pushBack(gp)
	sched.runqsize++
}

// pushBack adds gp to the tail of q.
func (q *gQueue) pushBack(gp *g) {
	gp.schedlink = 0
	if q.tail != 0 {
		q.tail.ptr().schedlink.set(gp)
	} else {
		q.head.set(gp)
	}
	q.tail.set(gp)
}


// dropg removes the association between m and the current goroutine m->curg (gp for short).
// Typically a caller sets gp's status away from Grunning and then
// immediately calls dropg to finish the job. The caller is also responsible
// for arranging that gp will be restarted using ready at an
// appropriate time. After calling dropg and arranging for gp to be
// readied later, the caller can do other work but eventually should
// call schedule to restart the scheduling of goroutines on this m.
func dropg() {
	_g_ := getg()

	setMNoWB(&_g_.m.curg.m, nil)
	setGNoWB(&_g_.m.curg, nil)
}

//go:nosplit

// Gosched yields the processor, allowing other goroutines to run. It does not
// suspend the current goroutine, so execution resumes automatically.
func Gosched() {
	checkTimeouts()
	mcall(gosched_m)
}

// Gosched continuation on g0.
func gosched_m(gp *g) {
	if trace.enabled {
		traceGoSched()
	}
	goschedImpl(gp)
}

func goschedImpl(gp *g) {
	status := readgstatus(gp)
	if status&^_Gscan != _Grunning {
		dumpgstatus(gp)
		throw("bad g status")
	}
	casgstatus(gp, _Grunning, _Grunnable)
	dropg()
	lock(&sched.lock)
	globrunqput(gp)
	unlock(&sched.lock)

	schedule()
}

// schedEnabled reports whether gp should be scheduled. It returns
// false is scheduling of gp is disabled.
func schedEnabled(gp *g) bool {
	if sched.disable.user {
		return isSystemGoroutine(gp, true)
	}
	return true
}

//go:nosplit
func exitsyscallfast(oldp *p) bool {
	_g_ := getg()

	// Freezetheworld sets stopwait but does not retake P's.
	if sched.stopwait == freezeStopWait {
		return false
	}

	// Try to re-acquire the last P.
	if oldp != nil && oldp.status == _Psyscall && atomic.Cas(&oldp.status, _Psyscall, _Pidle) {
		// There's a cpu for us, so we can run.
		wirep(oldp)
		exitsyscallfast_reacquired()
		return true
	}

	// Try to get any other idle P.
	if sched.pidle != 0 {
		var ok bool
		systemstack(func() {
			ok = exitsyscallfast_pidle()
			if ok && trace.enabled {
				if oldp != nil {
					// Wait till traceGoSysBlock event is emitted.
					// This ensures consistency of the trace (the goroutine is started after it is blocked).
					for oldp.syscalltick == _g_.m.syscalltick {
						osyield()
					}
				}
				traceGoSysExit(0)
			}
		})
		if ok {
			return true
		}
	}
	return false
}

func traceGoSysExit(ts int64) {
	if ts != 0 && ts < trace.ticksStart {
		// There is a race between the code that initializes sysexitticks
		// (in exitsyscall, which runs without a P, and therefore is not
		// stopped with the rest of the world) and the code that initializes
		// a new trace. The recorded sysexitticks must therefore be treated
		// as "best effort". If they are valid for this trace, then great,
		// use them for greater accuracy. But if they're not valid for this
		// trace, assume that the trace was started after the actual syscall
		// exit (but before we actually managed to start the goroutine,
		// aka right now), and assign a fresh time stamp to keep the log consistent.
		ts = 0
	}
	_g_ := getg().m.curg
	_g_.traceseq++
	_g_.tracelastp = _g_.m.p
	traceEvent(traceEvGoSysExit, -1, uint64(_g_.goid), _g_.traceseq, uint64(ts)/traceTickDiv)
}

func exitsyscallfast_pidle() bool {
	lock(&sched.lock)
	_p_ := pidleget()
	if _p_ != nil && atomic.Load(&sched.sysmonwait) != 0 {
		atomic.Store(&sched.sysmonwait, 0)
		notewakeup(&sched.sysmonnote)
	}
	unlock(&sched.lock)
	if _p_ != nil {
		acquirep(_p_)
		return true
	}
	return false
}

// exitsyscallfast_reacquired is the exitsyscall path on which this G
// has successfully reacquired the P it was running on before the
// syscall.
//
//go:nosplit
func exitsyscallfast_reacquired() {
	_g_ := getg()
	if _g_.m.syscalltick != _g_.m.p.ptr().syscalltick {
		if trace.enabled {
			// The p was retaken and then enter into syscall again (since _g_.m.syscalltick has changed).
			// traceGoSysBlock for this syscall was already emitted,
			// but here we effectively retake the p from the new syscall running on the same p.
			systemstack(func() {
				// Denote blocking of the new syscall.
				traceGoSysBlock(_g_.m.p.ptr())
				// Denote completion of the current syscall.
				traceGoSysExit(0)
			})
		}
		_g_.m.p.ptr().syscalltick++
	}
}

// Try to get an m from midle list.
// Sched must be locked.
// May run during STW, so write barriers are not allowed.
//go:nowritebarrierrec
func mget()*m{
	mp :=sched.midle.ptr()
	if mp != nil{
		sched.midle =mp.schedlink
		sched.nmidle--
	}
	return mp
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

// Put p to on _Pidle list.
// Sched must be locked.
// May run during STW, so write barriers are not allowed.
//go:nowritebarrierrec
func pidleput(_p_ *p){
	if !runqempty(_p_){
		throw("pidleput: P has non-empty run queue")
	}
	_p_.link = sched.pidle
	sched.pidle.set(_p_)
	atomic.Xadd(&sched.npidle,1)// TODO: fast atomic
}

// Try get a p from _Pidle list.
// Sched must be locked.
// May run during STW, so write barriers are not allowed.
//go:nowritebarrierrec
func pidleget() *p {
	_p_ := sched.pidle.ptr()
	if _p_ != nil{
		sched.pidle = _p_.link
		atomic.Xadd(&sched.npidle,-1) //TODO: fast atomic
	}
	return _p_
}

// runqempty reports whether _p_ has no Gs on its local run queue.
// It never returns true spuriously.
func runqempty(_p_ *p)bool{
	// Defend against a race where 1) _p_ has G1 in runqnext but runqhead == runqtail,
	// 2) runqput on _p_ kicks G1 to the runq, 3) runqget on _p_ empties runqnext.
	// Simply observing that runqhead == runqtail and then observing that runqnext == nil
	// does not mean the queue is empty.
	for{
		head := atomic.Load(&_p_.runqhead)
		tail := atomic.Load(&_p_.runqtail)
		runnext :=atomic.Loaduintptr((*uintptr)(unsafe.Pointer(&_p_.runnext)))
		if tail == atomic.Load(&_p_.runqtail){
			return head == tail && runnext == 0
		}
	}
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

// pop removes and returns the head of queue q. It returns nil if
// q is empty.
func (q *gQueue)pop()*g{
	gp :=q.head.ptr()
	if gp != nil{
		q.head =gp.schedlink
		if q.head == 0{
			q.tail = 0
		}
	}
	return  gp
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

var starttime int64

//schedt相关调试信息
func schedtrace(detailed bool) {
	now := nanotime()
	if starttime == 0 {
		starttime = now
	}

	lock(&sched.lock)
	print("SCHED ", (now-starttime)/1e6, "ms: gomaxprocs=", gomaxprocs, " idleprocs=", sched.npidle, " threads=", mcount(), " spinningthreads=", sched.nmspinning, " idlethreads=", sched.nmidle, " runqueue=", sched.runqsize)
	if detailed {
		print(" gcwaiting=", sched.gcwaiting, " nmidlelocked=", sched.nmidlelocked, " stopwait=", sched.stopwait, " sysmonwait=", sched.sysmonwait, "\n")
	}
	// We must be careful while reading data from P's, M's and G's.
	// Even if we hold schedlock, most data can be changed concurrently.
	// E.g. (p->m ? p->m->id : -1) can crash if p->m changes from non-nil to nil.
	for i, _p_ := range allp {
		mp := _p_.m.ptr()
		h := atomic.Load(&_p_.runqhead)
		t := atomic.Load(&_p_.runqtail)
		if detailed {
			id := int64(-1)
			if mp != nil {
				id = mp.id
			}
			print("  P", i, ": status=", _p_.status, " schedtick=", _p_.schedtick, " syscalltick=", _p_.syscalltick, " m=", id, " runqsize=", t-h, " gfreecnt=", _p_.gFree.n, " timerslen=", len(_p_.timers), "\n")
		} else {
			// In non-detailed mode format lengths of per-P run queues as:
			// [len1 len2 len3 len4]
			print(" ")
			if i == 0 {
				print("[")
			}
			print(t - h)
			if i == len(allp)-1 {
				print("]\n")
			}
		}
	}

	if !detailed {
		unlock(&sched.lock)
		return
	}

	for mp := allm; mp != nil; mp = mp.alllink {
		_p_ := mp.p.ptr()
		gp := mp.curg
		lockedg := mp.lockedg.ptr()
		id1 := int32(-1)
		if _p_ != nil {
			id1 = _p_.id
		}
		id2 := int64(-1)
		if gp != nil {
			id2 = gp.goid
		}
		id3 := int64(-1)
		if lockedg != nil {
			id3 = lockedg.goid
		}
		print("  M", mp.id, ": p=", id1, " curg=", id2, " mallocing=", mp.mallocing, " throwing=", mp.throwing, " preemptoff=", mp.preemptoff, ""+" locks=", mp.locks, " dying=", mp.dying, " spinning=", mp.spinning, " blocked=", mp.blocked, " lockedg=", id3, "\n")
	}

	lock(&allglock)
	for gi := 0; gi < len(allgs); gi++ {
		gp := allgs[gi]
		mp := gp.m
		lockedm := gp.lockedm.ptr()
		id1 := int64(-1)
		if mp != nil {
			id1 = mp.id
		}
		id2 := int64(-1)
		if lockedm != nil {
			id2 = lockedm.id
		}
		print("  G", gp.goid, ": status=", readgstatus(gp), "(", gp.waitreason.String(), ") m=", id1, " lockedm=", id2, "\n")
	}
	unlock(&allglock)
	unlock(&sched.lock)
}

// An initTask represents the set of initializations that need to be done for a package.
// Keep in sync with ../../test/initempty.go:initTask
type initTask struct{
	// TODO: pack the first 3 fields more tightly?
	state uintptr //0不需要初始化 1需要初始化 2初始化完成 0 = uninitialized, 1 = in progress, 2 = done
	ndeps uintptr
	nfns  uintptr
	// followed by ndeps instances of an *initTask, one per package depended on
	// followed by nfns pcs, one per init function to run
}

//执行初始化函数init() TODO HANK return
func doInit(t *initTask) {
	switch t.state {
	case 2: // fully initialized
		return
	case 1: // initialization in progress
		throw("recursive call during initialization - linker skew")
	default: // not initialized yet
		t.state = 1 // initialization in progress
		for i := uintptr(0); i < t.ndeps; i++ {
			p := add(unsafe.Pointer(t), (3+i)*sys.PtrSize)
			t2 := *(**initTask)(p)
			doInit(t2)
		}
		for i := uintptr(0); i < t.nfns; i++ {
			p := add(unsafe.Pointer(t), (3+t.ndeps+i)*sys.PtrSize)
			f := *(*func())(unsafe.Pointer(&p))
			f()
		}
		t.state = 2 // initialization done
	}
}


