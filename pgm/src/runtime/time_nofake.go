package runtime

import "unsafe"

//go:nosplit
func nanotime() int64 {
	return nanotime1()
}


// write must be nosplit on Windows (see write1)
//
//go:nosplit
func write(fd uintptr, p unsafe.Pointer, n int32) int32 {
	return write1(fd, p, n)
}

