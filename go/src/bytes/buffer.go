package bytes

// A Buffer is a variable-sized buffer of bytes with Read and Write methods.
// The zero value for Buffer is an empty buffer ready to use.
type Buffer struct{
	buf []byte //contents are the bytes buf[off : len(buf)]
	off int //偏移量,读取用&buf[off],写入的时候可以用&buf[len(buf)]  read at &buf[off], write at &buf[len(buf)]
	lastRead readOp//最后一次读的状态
}

// The readOp constants describe the last action performed on
// the buffer, so that UnreadRune and UnreadByte can check for
// invalid usage. opReadRuneX constants are chosen such that
// converted to int they correspond to the rune size that was read.
type readOp int8

// Don't use iota for these, as the values need to correspond with the
// names and comments, which is easier to see when being explicit.
const (
	opInvalid   readOp = 0  //没有读取的操作 Non-read operation.
)

// Bytes returns a slice of length b.Len() holding the unread portion of the buffer.
// The slice is valid for use only until the next buffer modification (that is,
// only until the next call to a method like Read, Write, Reset, or Truncate).
// The slice aliases the buffer content at least until the next buffer modification,
// so immediate changes to the slice will affect the result of future reads.
func (b *Buffer) Bytes()[]byte{return b.buf[b.off:]}

// Reset resets the buffer to be empty,
// but it retains the underlying storage for use by future writes.
// Reset is the same as Truncate(0).
//重置缓冲区
func(b *Buffer)Reset(){
	b.buf = b.buf[:0]
	b.off = 0
	b.lastRead = opInvalid
}
