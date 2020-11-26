package runtime

//go:nosplit
func nanotime() int64 {
	return nanotime1()
}
