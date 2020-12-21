package runtime

import (
	"runtime/internal/atomic"
	"runtime/internal/sys"
	"unsafe"
)

//go:cgo_import_dynamic runtime._AddVectoredExceptionHandler AddVectoredExceptionHandler%2 "kernel32.dll"
//go:cgo_import_dynamic runtime._CloseHandle CloseHandle%1 "kernel32.dll"
//go:cgo_import_dynamic runtime._CreateEventA CreateEventA%4 "kernel32.dll"
//go:cgo_import_dynamic runtime._CreateIoCompletionPort CreateIoCompletionPort%4 "kernel32.dll"
//go:cgo_import_dynamic runtime._CreateThread CreateThread%6 "kernel32.dll"
//go:cgo_import_dynamic runtime._CreateWaitableTimerA CreateWaitableTimerA%3 "kernel32.dll"
//go:cgo_import_dynamic runtime._DuplicateHandle DuplicateHandle%7 "kernel32.dll"
//go:cgo_import_dynamic runtime._ExitProcess ExitProcess%1 "kernel32.dll"
//go:cgo_import_dynamic runtime._FreeEnvironmentStringsW FreeEnvironmentStringsW%1 "kernel32.dll"
//go:cgo_import_dynamic runtime._GetConsoleMode GetConsoleMode%2 "kernel32.dll"
//go:cgo_import_dynamic runtime._GetEnvironmentStringsW GetEnvironmentStringsW%0 "kernel32.dll"
//go:cgo_import_dynamic runtime._GetProcAddress GetProcAddress%2 "kernel32.dll"
//go:cgo_import_dynamic runtime._GetProcessAffinityMask GetProcessAffinityMask%3 "kernel32.dll"
//go:cgo_import_dynamic runtime._GetQueuedCompletionStatusEx GetQueuedCompletionStatusEx%6 "kernel32.dll"
//go:cgo_import_dynamic runtime._GetStdHandle GetStdHandle%1 "kernel32.dll"
//go:cgo_import_dynamic runtime._GetSystemDirectoryA GetSystemDirectoryA%2 "kernel32.dll"
//go:cgo_import_dynamic runtime._GetSystemInfo GetSystemInfo%1 "kernel32.dll"
//go:cgo_import_dynamic runtime._GetThreadContext GetThreadContext%2 "kernel32.dll"
//go:cgo_import_dynamic runtime._SetThreadContext SetThreadContext%2 "kernel32.dll"
//go:cgo_import_dynamic runtime._LoadLibraryW LoadLibraryW%1 "kernel32.dll"
//go:cgo_import_dynamic runtime._LoadLibraryA LoadLibraryA%1 "kernel32.dll"
//go:cgo_import_dynamic runtime._OpenProcess OpenProcess%3 "kernel32.dll"
//go:cgo_import_dynamic runtime._PostQueuedCompletionStatus PostQueuedCompletionStatus%4 "kernel32.dll"
//go:cgo_import_dynamic runtime._ProcessIdToSessionId ProcessIdToSessionId%2 "kernel32.dll"
//go:cgo_import_dynamic runtime._QueryFullProcessImageNameA QueryFullProcessImageNameA%4 "kernel32.dll"
//go:cgo_import_dynamic runtime._ResumeThread ResumeThread%1 "kernel32.dll"
//go:cgo_import_dynamic runtime._SetConsoleCtrlHandler SetConsoleCtrlHandler%2 "kernel32.dll"
//go:cgo_import_dynamic runtime._SetErrorMode SetErrorMode%1 "kernel32.dll"
//go:cgo_import_dynamic runtime._SetEvent SetEvent%1 "kernel32.dll"
//go:cgo_import_dynamic runtime._SetProcessPriorityBoost SetProcessPriorityBoost%2 "kernel32.dll"
//go:cgo_import_dynamic runtime._SetThreadPriority SetThreadPriority%2 "kernel32.dll"
//go:cgo_import_dynamic runtime._SetUnhandledExceptionFilter SetUnhandledExceptionFilter%1 "kernel32.dll"
//go:cgo_import_dynamic runtime._SetWaitableTimer SetWaitableTimer%6 "kernel32.dll"
//go:cgo_import_dynamic runtime._SuspendThread SuspendThread%1 "kernel32.dll"
//go:cgo_import_dynamic runtime._SwitchToThread SwitchToThread%0 "kernel32.dll"
//go:cgo_import_dynamic runtime._TlsAlloc TlsAlloc%0 "kernel32.dll"
//go:cgo_import_dynamic runtime._VirtualAlloc VirtualAlloc%4 "kernel32.dll"
//go:cgo_import_dynamic runtime._VirtualFree VirtualFree%3 "kernel32.dll"
//go:cgo_import_dynamic runtime._VirtualQuery VirtualQuery%3 "kernel32.dll"
//go:cgo_import_dynamic runtime._WaitForSingleObject WaitForSingleObject%2 "kernel32.dll"
//go:cgo_import_dynamic runtime._WaitForMultipleObjects WaitForMultipleObjects%4 "kernel32.dll"
//go:cgo_import_dynamic runtime._WriteConsoleW WriteConsoleW%5 "kernel32.dll"
//go:cgo_import_dynamic runtime._WriteFile WriteFile%5 "kernel32.dll"

type stdFunction unsafe.Pointer

var (
	// Following syscalls are available on every Windows PC.
	// All these variables are set by the Windows executable
	// loader before the Go program starts.
	_AddVectoredExceptionHandler,
	_CloseHandle,
	_CreateEventA,//创建时间
	_CreateIoCompletionPort,
	_CreateThread,//创建线程
	_CreateWaitableTimerA,
	_DuplicateHandle,
	_ExitProcess,
	_FreeEnvironmentStringsW,
	_GetConsoleMode,
	_GetEnvironmentStringsW,
	_GetProcAddress,
	_GetProcessAffinityMask,
	_GetQueuedCompletionStatusEx,
	_GetStdHandle,
	_GetSystemDirectoryA,
	_GetSystemInfo,//获取系统信息
	_GetSystemTimeAsFileTime,
	_GetThreadContext,
	_SetThreadContext,
	_LoadLibraryW,
	_LoadLibraryA,
	_OpenProcess,
	_PostQueuedCompletionStatus,
	_ProcessIdToSessionId,
	_QueryFullProcessImageNameA,
	_QueryPerformanceCounter,
	_QueryPerformanceFrequency,
	_ResumeThread,
	_SetConsoleCtrlHandler,
	_SetErrorMode,
	_SetEvent,
	_SetProcessPriorityBoost,
	_SetThreadPriority,
	_SetUnhandledExceptionFilter,
	_SetWaitableTimer,
	_SuspendThread,
	_SwitchToThread,
	_TlsAlloc,
	_VirtualAlloc,//系统虚拟内存的分配
	_VirtualFree,//系统虚拟内存的释放
	_VirtualQuery,
	_WaitForSingleObject,
	_WaitForMultipleObjects,
	_WriteConsoleW,
	_WriteFile,
	_ stdFunction

	// Following syscalls are only available on some Windows PCs.
	// We will load syscalls, if available, before using them.
	_AddDllDirectory,
	_AddVectoredContinueHandler,
	_LoadLibraryExA,
	_LoadLibraryExW,
	_ stdFunction

	// Use RtlGenRandom to generate cryptographically random data.
	// This approach has been recommended by Microsoft (see issue
	// 15589 for details).
	// The RtlGenRandom is not listed in advapi32.dll, instead
	// RtlGenRandom function can be found by searching for SystemFunction036.
	// Also some versions of Mingw cannot link to SystemFunction036
	// when building executable as Cgo. So load SystemFunction036
	// manually during runtime startup.
	_RtlGenRandom stdFunction//生成随机数

	// Load ntdll.dll manually during startup, otherwise Mingw
	// links wrong printf function to cgo executable (see issue
	// 12030 for details).
	_NtWaitForSingleObject     stdFunction
	_NtQueryInformationProcess stdFunction

	// These are from non-kernel32.dll, so we prefer to LoadLibraryEx them.
	_timeBeginPeriod,
	_timeEndPeriod,
	_WSAGetOverlappedResult,
	_ stdFunction
)

// Function to be called by windows CreateThread
// to start new os thread.
//runtime.sys_windows_amd64.s启动线程
func tstart_stdcall(newm *m)

type mOS struct{
	threadLock mutex // protects "thread" and prevents closing
	thread uintptr // thread handle 线程绑定处理

	waitsema   uintptr // semaphore for parking on locks
	resumesema uintptr // semaphore to indicate suspend/resume

	// preemptExtLock synchronizes preemptM with entry/exit from
	// external C code.
	//
	// This protects against races between preemptM calling
	// SuspendThread and external code on this thread calling
	// ExitProcess. If these happen concurrently, it's possible to
	// exit the suspending thread and suspend the exiting thread,
	// leading to deadlock.
	//
	// 0 indicates this M is not being preempted or in external
	// code. Entering external code CASes this from 0 to 1. If
	// this fails, a preemption is in progress, so the thread must
	// wait for the preemption. preemptM also CASes this from 0 to
	// 1. If this fails, the preemption fails (as it would if the
	// PC weren't in Go code). The value is reset to 0 when
	// returning from external code or after a preemption is
	// complete.
	//
	// TODO(austin): We may not need this if preemption were more
	// tightly synchronized on the G/P status and preemption
	// blocked transition into _Gsyscall/_Psyscall.
	preemptExtLock uint32 //线程抢占的锁
}

// Call a Windows function with stdcall conventions,
// and switch to os stack during the call.
//TODO HANK return
//runtime/sys_windows_amd64.s
func asmstdcall(fn unsafe.Pointer)

var asmstdcallAddr unsafe.Pointer //asmstdcall的调用地址,osinit中初始化

func osinit() {
	//返回方法的地址
	asmstdcallAddr = unsafe.Pointer(funcPC(asmstdcall))
	usleep2Addr = unsafe.Pointer(funcPC(usleep2))
	switchtothreadAddr = unsafe.Pointer(funcPC(switchtothread))

	setBadSignalMsg()

	//获取cpu核数
	ncpu =getproccount()
}

// useQPCTime controls whether time.now and nanotime use QueryPerformanceCounter.
// This is only set to 1 when running under Wine.
var useQPCTime uint8

var qpcStartCounter int64
var qpcMultiplier int64

//runtime/sys_windows_amd64.s
//go:nosplit
func nanotimeQPC() int64 {
	var counter int64 = 0
	stdcall1(_QueryPerformanceCounter, uintptr(unsafe.Pointer(&counter)))

	// returns number of nanoseconds 返回毫秒
	return (counter - qpcStartCounter) * qpcMultiplier
}

type sigset struct{}

//获取cpu的数量
func getproccount() int32{
	var mask,sysmask uintptr
	ret :=stdcall3(_GetProcessAffinityMask,currentProcess,uintptr(unsafe.Pointer(&mask)),uintptr(unsafe.Pointer(&sysmask)))
	if ret != 0{
		n := 0
		maskbits := int(unsafe.Sizeof(mask) * 8)
		for i :=0; i <maskbits;i++{
			if mask&(1<<uint(i)) != 0 {
				n++
			}
		}
		if n != 0{
			return int32(n)
		}
	}
	// use GetSystemInfo if GetProcessAffinityMask fails
	var info systeminfo
	stdcall1(_GetSystemInfo,uintptr(unsafe.Pointer(&info)))
	return int32(info.dwnumberofprocessors)
}

const(
	currentProcess = ^uintptr(0) // -1 = current process
	currentThread  = ^uintptr(1) // -2 = current thread
)

func getlasterror() uint32
func setlasterror(err uint32)

//go:nosplit
func getRandomData(r []byte) {
	n := 0
	if stdcall2(_RtlGenRandom, uintptr(unsafe.Pointer(&r[0])), uintptr(len(r)))&0xff != 0 {
		n = len(r)
	}
	extendRandom(r,n)
}

// exiting is set to non-zero when the process is exiting.
var exiting uint32

//go:nosplit
func exit(code int32) {
	// Disallow thread suspension for preemption. Otherwise,
	// ExitProcess and SuspendThread can race: SuspendThread
	// queues a suspension request for this thread, ExitProcess
	// kills the suspending thread, and then this thread suspends.
	stdcall1(_ExitProcess,uintptr(code))
}

//创建锁的事件
//go:nosplit
func semacreate(mp *m) {
	if mp.waitsema != 0 {
		return
	}
	mp.waitsema = stdcall4(_CreateEventA, 0, 0, 0, 0)
	if mp.waitsema == 0 {
		systemstack(func() {
			print("runtime: createevent failed; errno=", getlasterror(), "\n")
			throw("runtime.semacreate")
		})
	}
	mp.resumesema = stdcall4(_CreateEventA, 0, 0, 0, 0)
	if mp.resumesema == 0 {
		systemstack(func() {
			print("runtime: createevent failed; errno=", getlasterror(), "\n")
			throw("runtime.semacreate")
		})
		stdcall1(_CloseHandle, mp.waitsema)
		mp.waitsema = 0
	}
}

//进入等待，等待时间为纳秒
//go:nosplit
func semasleep(ns int64) int32 {
	const (
		_WAIT_ABANDONED = 0x00000080
		_WAIT_OBJECT_0  = 0x00000000
		_WAIT_TIMEOUT   = 0x00000102
		_WAIT_FAILED    = 0xFFFFFFFF
	)

	var result uintptr
	if ns < 0 {
		result = stdcall2(_WaitForSingleObject, getg().m.waitsema, uintptr(_INFINITE))
	} else {
		start := nanotime()
		elapsed := int64(0)
		for {
			ms := int64(timediv(ns-elapsed, 1000000, nil))
			if ms == 0 {
				ms = 1
			}
			result = stdcall4(_WaitForMultipleObjects, 2,
				uintptr(unsafe.Pointer(&[2]uintptr{getg().m.waitsema, getg().m.resumesema})),
				0, uintptr(ms))
			if result != _WAIT_OBJECT_0+1 {
				// Not a suspend/resume event
				break
			}
			elapsed = nanotime() - start
			if elapsed >= ns {
				return -1
			}
		}
	}
	switch result {
	case _WAIT_OBJECT_0: // Signaled
		return 0

	case _WAIT_TIMEOUT:
		return -1

	case _WAIT_ABANDONED:
		systemstack(func() {
			throw("runtime.semasleep wait_abandoned")
		})

	case _WAIT_FAILED:
		systemstack(func() {
			print("runtime: waitforsingleobject wait_failed; errno=", getlasterror(), "\n")
			throw("runtime.semasleep wait_failed")
		})

	default:
		systemstack(func() {
			print("runtime: waitforsingleobject unexpected; result=", result, "\n")
			throw("runtime.semasleep unexpected")
		})
	}

	return -1 // unreachable
}

//go:nosplit
func semawakeup(mp *m) {
	if stdcall1(_SetEvent, mp.waitsema) == 0 {
		systemstack(func() {
			print("runtime: setevent failed; errno=", getlasterror(), "\n")
			throw("runtime.semawakeup")
		})
	}
}

// Calling stdcall on os stack.
// May run during STW, so write barriers are not allowed.
//在线程栈上做系统调用
//go:nowritebarrier
//go:nosplit
func stdcall(fn stdFunction)uintptr{
	gp :=getg()
	mp :=gp.m
	mp.libcall.fn = uintptr(unsafe.Pointer(fn))
	resetLibcall := false
	if mp.profilehz != 0 && mp.libcallsp == 0 {
		// leave pc/sp for cpu profiler
		mp.libcallg.set(gp)
		mp.libcallpc = getcallerpc()
		// sp must be the last, because once async cpu profiler finds
		// all three values to be non-zero, it will use them
		mp.libcallsp = getcallersp()
		resetLibcall  = true // See comment in sys_darwin.go:libcCall
	}
	asmcgocall(asmstdcallAddr,unsafe.Pointer(&mp.libcall))
	if resetLibcall{
		mp.libcallsp  = 0
	}
	return mp.libcall.r1
}

//go:nosplit
func stdcall0(fn stdFunction) uintptr {
	mp := getg().m
	mp.libcall.n = 0
	mp.libcall.args = uintptr(noescape(unsafe.Pointer(&fn))) // it's unused but must be non-nil, otherwise crashes
	return stdcall(fn)
}

//go:nosplit
func stdcall1(fn stdFunction, a0 uintptr) uintptr {
	mp := getg().m
	mp.libcall.n = 1
	mp.libcall.args = uintptr(noescape(unsafe.Pointer(&a0)))
	return stdcall(fn)
}

//go:nosplit
func stdcall2(fn stdFunction, a0, a1 uintptr) uintptr {
	mp := getg().m
	mp.libcall.n = 2
	mp.libcall.args = uintptr(noescape(unsafe.Pointer(&a0)))
	return stdcall(fn)
}

//go:nosplit
func stdcall3(fn stdFunction,a0,a1,a2 uintptr)uintptr{
	mp := getg().m
	mp.libcall.n =3
	mp.libcall.args = uintptr(noescape(unsafe.Pointer(&a0)))
	return stdcall(fn)
}

//go:nosplit
func stdcall4(fn stdFunction, a0, a1, a2, a3 uintptr) uintptr {
	mp := getg().m
	mp.libcall.n = 4
	mp.libcall.args = uintptr(noescape(unsafe.Pointer(&a0)))
	return stdcall(fn)
}

//go:nosplit
func stdcall5(fn stdFunction, a0, a1, a2, a3, a4 uintptr) uintptr {
	mp := getg().m
	mp.libcall.n = 5
	mp.libcall.args = uintptr(noescape(unsafe.Pointer(&a0)))
	return stdcall(fn)
}

//go:nosplit
func stdcall6(fn stdFunction, a0, a1, a2, a3, a4, a5 uintptr) uintptr {
	mp := getg().m
	mp.libcall.n = 6
	mp.libcall.args = uintptr(noescape(unsafe.Pointer(&a0)))
	return stdcall(fn)
}

//go:nosplit
func stdcall7(fn stdFunction, a0, a1, a2, a3, a4, a5, a6 uintptr) uintptr {
	mp := getg().m
	mp.libcall.n = 7
	mp.libcall.args = uintptr(noescape(unsafe.Pointer(&a0)))
	return stdcall(fn)
}

// in sys_windows_386.s and sys_windows_amd64.s
func onosstack(fn unsafe.Pointer, arg uint32)
func usleep2(usec uint32)
func switchtothread()

var usleep2Addr unsafe.Pointer
var switchtothreadAddr unsafe.Pointer

//go:nosplit
func osyield() {
	onosstack(switchtothreadAddr, 0)
}

//go:nosplit
func usleep(us uint32) {
	// Have 1us units; want 100ns units.
	onosstack(usleep2Addr, 10*us)
}

// May run with m.p==nil, so write barriers are not allowed. This
// function is called by newosproc0, so it is also required to
// operate without stack guards.
//m包装线程
//go:nowritebarrierrec
//go:nosplit
func newosproc(mp *m){
	// We pass 0 for the stack size to use the default for this binary.
	thandle :=stdcall6(_CreateThread,0,0,
		funcPC(tstart_stdcall),uintptr(unsafe.Pointer(mp)),
		0,0)
	if thandle == 0{
		print("runtime: failed to create new OS thread (have ", mcount(), " already; errno=", getlasterror(), ")\n")
		throw("runtime.newosproc")
	}
	// Close thandle to avoid leaking the thread object if it exits.
	stdcall1(_CloseHandle, thandle)
}

// Called to initialize a new m (including the bootstrap m).
// Called on the parent thread (main thread in case of bootstrap), can allocate memory.
//这就是空实现
func mpreinit(mp *m) {
}

// Called to initialize a new m (including the bootstrap m).
// Called on the new thread, cannot allocate memory.
func minit(){
	var thandle uintptr
	stdcall7(_DuplicateHandle, currentProcess, currentThread, currentProcess, uintptr(unsafe.Pointer(&thandle)), 0, 0, _DUPLICATE_SAME_ACCESS)

	mp := getg().m
	lock(&mp.threadLock)
	mp.thread = thandle
	unlock(&mp.threadLock)
}

func gFromTLS(mp *m) *g {
	switch GOARCH {
	case "arm":
		tls := &mp.tls[0]
		return **((***g)(unsafe.Pointer(tls)))
	case "386", "amd64":
		tls := &mp.tls[0]
		return *((**g)(unsafe.Pointer(tls)))
	}
	throw("unsupported architecture")
	return nil
}

const preemptMSupported = GOARCH != "arm"

// suspendLock protects simultaneous SuspendThread operations from
// suspending each other.
var suspendLock mutex

//抢占线程
func preemptM(mp *m) {
	if GOARCH == "arm"{
		// TODO: Implement call injection
		return
	}

	if mp == getg().m {
		throw("self-preempt")
	}

	// Synchronize with external code that may try to ExitProcess.
	if !atomic.Cas(&mp.preemptExtLock, 0, 1) {
		// External code is running. Fail the preemption
		// attempt.
		atomic.Xadd(&mp.preemptGen, 1)
		return
	}

	// Acquire our own handle to mp's thread.
	lock(&mp.threadLock)
	if mp.thread == 0 {
		// The M hasn't been minit'd yet (or was just unminit'd).
		unlock(&mp.threadLock)
		atomic.Store(&mp.preemptExtLock, 0)
		atomic.Xadd(&mp.preemptGen, 1)
		return
	}
	var thread uintptr
	stdcall7(_DuplicateHandle, currentProcess, mp.thread, currentProcess, uintptr(unsafe.Pointer(&thread)), 0, 0, _DUPLICATE_SAME_ACCESS)
	unlock(&mp.threadLock)

	// Prepare thread context buffer. This must be aligned to 16 bytes.
	var c *context
	var cbuf [unsafe.Sizeof(*c) + 15]byte
	c = (*context)(unsafe.Pointer((uintptr(unsafe.Pointer(&cbuf[15]))) &^ 15))
	c.contextflags = _CONTEXT_CONTROL

	// Serialize thread suspension. SuspendThread is asynchronous,
	// so it's otherwise possible for two threads to suspend each
	// other and deadlock. We must hold this lock until after
	// GetThreadContext, since that blocks until the thread is
	// actually suspended.
	lock(&suspendLock)

	// Suspend the thread.
	if int32(stdcall1(_SuspendThread, thread)) == -1 {
		unlock(&suspendLock)
		stdcall1(_CloseHandle, thread)
		atomic.Store(&mp.preemptExtLock, 0)
		// The thread no longer exists. This shouldn't be
		// possible, but just acknowledge the request.
		atomic.Xadd(&mp.preemptGen, 1)
		return
	}

	// We have to be very careful between this point and once
	// we've shown mp is at an async safe-point. This is like a
	// signal handler in the sense that mp could have been doing
	// anything when we stopped it, including holding arbitrary
	// locks.

	// We have to get the thread context before inspecting the M
	// because SuspendThread only requests a suspend.
	// GetThreadContext actually blocks until it's suspended.
	stdcall2(_GetThreadContext, thread, uintptr(unsafe.Pointer(c)))

	unlock(&suspendLock)

	// Does it want a preemption and is it safe to preempt?
	gp := gFromTLS(mp)
	if wantAsyncPreempt(gp) {
		if ok, newpc := isAsyncSafePoint(gp, c.ip(), c.sp(), c.lr()); ok {
			// Inject call to asyncPreempt
			targetPC := funcPC(asyncPreempt)
			switch GOARCH {
			default:
				throw("unsupported architecture")
			case "386", "amd64":
				// Make it look like the thread called targetPC.
				sp := c.sp()
				sp -= sys.PtrSize
				*(*uintptr)(unsafe.Pointer(sp)) = newpc
				c.set_sp(sp)
				c.set_ip(targetPC)
			}

			stdcall2(_SetThreadContext, thread, uintptr(unsafe.Pointer(c)))
		}
	}

	atomic.Store(&mp.preemptExtLock, 0)

	// Acknowledge the preemption.
	atomic.Xadd(&mp.preemptGen, 1)

	stdcall1(_ResumeThread, thread)
	stdcall1(_CloseHandle, thread)
}

var (
	utf16ConsoleBack     [1000]uint16
	utf16ConsoleBackLock mutex
)

// writeConsole writes bufLen bytes from buf to the console File.
// It returns the number of bytes written.
func writeConsole(handle uintptr, buf unsafe.Pointer, bufLen int32) int {
	const surr2 = (surrogateMin + surrogateMax + 1) / 2

	// Do not use defer for unlock. May cause issues when printing a panic.
	lock(&utf16ConsoleBackLock)

	b := (*[1 << 30]byte)(buf)[:bufLen]
	s := *(*string)(unsafe.Pointer(&b))

	utf16tmp := utf16ConsoleBack[:]

	total := len(s)
	w := 0
	for _, r := range s {
		if w >= len(utf16tmp)-2 {
			writeConsoleUTF16(handle, utf16tmp[:w])
			w = 0
		}
		if r < 0x10000 {
			utf16tmp[w] = uint16(r)
			w++
		} else {
			r -= 0x10000
			utf16tmp[w] = surrogateMin + uint16(r>>10)&0x3ff
			utf16tmp[w+1] = surr2 + uint16(r)&0x3ff
			w += 2
		}
	}
	writeConsoleUTF16(handle, utf16tmp[:w])
	unlock(&utf16ConsoleBackLock)
	return total
}

// writeConsoleUTF16 is the dedicated windows calls that correctly prints
// to the console regardless of the current code page. Input is utf-16 code points.
// The handle must be a console handle.
func writeConsoleUTF16(handle uintptr, b []uint16) {
	l := uint32(len(b))
	if l == 0 {
		return
	}
	var written uint32
	stdcall5(_WriteConsoleW,
		handle,
		uintptr(unsafe.Pointer(&b[0])),
		uintptr(l),
		uintptr(unsafe.Pointer(&written)),
		0,
	)
	return
}

// write1 must be nosplit because it's used as a last resort in
// functions like badmorestackg0. In such cases, we'll always take the
// ASCII path.
//
//go:nosplit
func write1(fd uintptr, buf unsafe.Pointer, n int32) int32 {
	const (
		_STD_OUTPUT_HANDLE = ^uintptr(10) // -11
		_STD_ERROR_HANDLE  = ^uintptr(11) // -12
	)
	var handle uintptr
	switch fd {
	case 1:
		handle = stdcall1(_GetStdHandle, _STD_OUTPUT_HANDLE)
	case 2:
		handle = stdcall1(_GetStdHandle, _STD_ERROR_HANDLE)
	default:
		// assume fd is real windows handle.
		handle = fd
	}
	isASCII := true
	b := (*[1 << 30]byte)(buf)[:n]
	for _, x := range b {
		if x >= 0x80 {
			isASCII = false
			break
		}
	}

	if !isASCII {
		var m uint32
		isConsole := stdcall2(_GetConsoleMode, handle, uintptr(unsafe.Pointer(&m))) != 0
		// If this is a console output, various non-unicode code pages can be in use.
		// Use the dedicated WriteConsole call to ensure unicode is printed correctly.
		if isConsole {
			return int32(writeConsole(handle, buf, n))
		}
	}
	var written uint32
	stdcall5(_WriteFile, handle, uintptr(buf), uintptr(n), uintptr(unsafe.Pointer(&written)), 0)
	return int32(written)
}

// osPreemptExtEnter is called before entering external code that may
// call ExitProcess.
//
// This must be nosplit because it may be called from a syscall with
// untyped stack slots, so the stack must not be grown or scanned.
//
//go:nosplit
func osPreemptExtEnter(mp *m) {
	for !atomic.Cas(&mp.preemptExtLock, 0, 1) {
		// An asynchronous preemption is in progress. It's not
		// safe to enter external code because it may call
		// ExitProcess and deadlock with SuspendThread.
		// Ideally we would do the preemption ourselves, but
		// can't since there may be untyped syscall arguments
		// on the stack. Instead, just wait and encourage the
		// SuspendThread APC to run. The preemption should be
		// done shortly.
		osyield()
	}
	// Asynchronous preemption is now blocked.
}

// osPreemptExtExit is called after returning from external code that
// may call ExitProcess.
//
// See osPreemptExtEnter for why this is nosplit.
//
//go:nosplit
func osPreemptExtExit(mp *m) {
	atomic.Store(&mp.preemptExtLock, 0)
}
