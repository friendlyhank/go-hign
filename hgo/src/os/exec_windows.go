package os

import (
	"internal/syscall/windows"
	"syscall"
)

func init(){
	cmd := windows.UTF16PtrToString(syscall.GetCommandLine())
	Args = commandLineToArgv(cmd)
}

func appendBSBytes(b []byte,n int)[]byte{
	for ; n > 0; n-- {
		b = append(b, '\\')
	}
	return b
}

// readNextArg splits command line string hcmd into next
// argument and command line remainder.
func readNextArg(cmd string)(arg []byte,rest string){
	var b []byte
	var inquote bool
	var nslash int
	for ; len(cmd) > 0; cmd = cmd[1:] {
		c := cmd[0]
		switch c {
		case ' ', '\t':
			if !inquote {
				return appendBSBytes(b, nslash), cmd[1:]
			}
		case '"':
			b = appendBSBytes(b, nslash/2)
			if nslash%2 == 0 {
				// use "Prior to 2008" rule from
				// http://daviddeley.com/autohotkey/parameters/parameters.htm
				// section 5.2 to deal with double double quotes
				if inquote && len(cmd) > 1 && cmd[1] == '"' {
					b = append(b, c)
					cmd = cmd[1:]
				}
				inquote = !inquote
			} else {
				b = append(b, c)
			}
			nslash = 0
			continue
		case '\\':
			nslash++
			continue
		}
		b = appendBSBytes(b, nslash)
		nslash = 0
		b = append(b, c)
	}
	return appendBSBytes(b,nslash),""
}

// commandLineToArgv splits a command line into individual argument
// strings, following the Windows conventions documented
// at http://daviddeley.com/autohotkey/parameters/parameters.htm#WINARGV
//从文件路径中解析除参数信息
func commandLineToArgv(cmd string)[]string{
	var args []string
	for len(cmd) > 0{
		if cmd[0] == ' ' || cmd[0] == '\t'{
			cmd = cmd[1:]
			continue
		}
		var arg []byte
		arg,cmd =readNextArg(cmd)
		args = append(args,string(arg))
	}
	return args
}