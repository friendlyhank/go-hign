package syscall

import (
	"internal/syscall/windows/sysdll"
	"unsafe"
)

var(
	modkernel32 = NewLazyDLL(sysdll.Add("kernel32.dll"))

	procGetCommandLineW =modkernel32.NewProc("GetCommandLineW")
	procGetSystemDirectoryW                = modkernel32.NewProc("GetSystemDirectoryW")
)

func GetCommandLine()(cmd *uint16){
	r0,_,_ :=Syscall(procGetCommandLineW.Addr(),0, 0, 0, 0)
	cmd = (*uint16)(unsafe.Pointer(r0))
	return
}

//获取系统路径
func getSystemDirectory(dir *uint16,dirLen uint32)(len uint32,err error){
	r0,_,e1 :=Syscall(procGetSystemDirectoryW.Addr(),2, uintptr(unsafe.Pointer(dir)), uintptr(dirLen), 0)
	len =uint32(r0)
	if len == 0{
		if e1 != 0{

		}else{}
	}
	return
}