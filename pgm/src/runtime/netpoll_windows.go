package runtime

import "runtime/internal/atomic"

const _DWORD_MAX = 0xffffffff

const _INVALID_HANDLE_VALUE = ^uintptr(0)

var (
	iocphandle uintptr = _INVALID_HANDLE_VALUE // completion port io handle

	netpollWakeSig uint32 // used to avoid duplicate calls of netpollBreak
)

func netpollBreak() {
	if atomic.Cas(&netpollWakeSig, 0, 1) {
		if stdcall4(_PostQueuedCompletionStatus, iocphandle, 0, 0, 0) == 0 {
			println("runtime: netpoll: PostQueuedCompletionStatus failed (errno=", getlasterror(), ")")
			throw("runtime: netpoll: PostQueuedCompletionStatus failed")
		}
	}
}
