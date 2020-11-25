package syscall

// Windows reserves errors >= 1<<29 for application use.
const APPLICATION_ERROR = 1 << 29

// Invented values to support what package os and others expects.
const (
	E2BIG Errno = APPLICATION_ERROR + iota
	EINVAL
)
