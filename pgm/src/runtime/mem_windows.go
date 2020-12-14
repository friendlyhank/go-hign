package runtime

import "unsafe"

const (
	_MEM_COMMIT   = 0x1000
	_MEM_RESERVE  = 0x2000
	_MEM_DECOMMIT = 0x4000
	_MEM_RELEASE  = 0x8000

	_PAGE_READWRITE = 0x0004
	_PAGE_NOACCESS  = 0x0001

	_ERROR_NOT_ENOUGH_MEMORY = 8
	_ERROR_COMMITMENT_LIMIT  = 1455
)

// Don't split the stack as this function may be invoked without a valid G,
// which prevents us from allocating more stack.
//直接向系统申请内存
//go:nosplit
func sysAlloc(n uintptr, sysStat *uint64) unsafe.Pointer {
	mSysStatInc(sysStat, n)
	return unsafe.Pointer(stdcall4(_VirtualAlloc, 0, n, _MEM_COMMIT|_MEM_RESERVE, _PAGE_READWRITE))
}
