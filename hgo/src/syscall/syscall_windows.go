package syscall

import "unicode/utf16"

type Handle uintptr

// UTF16FromString returns the UTF-16 encoding of the UTF-8 string
// s, with a terminating NUL added. If s contains a NUL byte at any
// location, it returns (nil, EINVAL).
func UTF16FromString(s string) ([]uint16, error){
	for i :=0;i<len(s);i++{
		if s[i] == 0{
			return nil,EINVAL
		}
	}
	return utf16.Encode([]rune(s + "\x00")), nil
}

// UTF16PtrFromString returns pointer to the UTF-16 encoding of
// the UTF-8 string s, with a terminating NUL added. If s
// contains a NUL byte at any location, it returns (nil, EINVAL).
func UTF16PtrFromString(s string) (*uint16, error) {
	a,err :=UTF16FromString(s)
	if err != nil{
		return nil,err
	}
	return &a[0],nil
}

// Errno is the Windows error number.
//
// Errno values can be tested against error values from the os package
// using errors.Is. For example:
//
//	_, _, err := syscall.Syscall(...)
//	if errors.Is(err, os.ErrNotExist) ...
type Errno uintptr

func (e Errno)Error()string{
	return ""
}