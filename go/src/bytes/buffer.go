package bytes

// A Buffer is a variable-sized buffer of bytes with Read and Write methods.
// The zero value for Buffer is an empty buffer ready to use.
type Buffer struct{
	buf []byte //contents are the bytes buf[off : len(buf)]
	off int //偏移量,读取用&buf[off],写入的时候可以用&buf[len(buf)]  read at &buf[off], write at &buf[len(buf)]
}

// Bytes returns a slice of length b.Len() holding the unread portion of the buffer.
// The slice is valid for use only until the next buffer modification (that is,
// only until the next call to a method like Read, Write, Reset, or Truncate).
// The slice aliases the buffer content at least until the next buffer modification,
// so immediate changes to the slice will affect the result of future reads.
func (b *Buffer) Bytes()[]byte{return b.buf[b.off:]}
