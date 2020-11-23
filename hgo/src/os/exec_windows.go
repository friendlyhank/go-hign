package os

import (
	"internal/syscall/windows"
	"syscall"
)

func init(){
	cmd := windows.UTF16PtrToString(syscall.GetCommandLine())
	println(cmd)
}