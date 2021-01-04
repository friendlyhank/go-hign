package bytes

import "errors"

//小缓存区大小为64字节 smallBufferSize is an initial allocation minimal capacity.
const smallBufferSize = 64

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

// ErrTooLarge is passed to panic if memory cannot be allocated to store data in a buffer.
var ErrTooLarge = errors.New("bytes.Buffer: too large")

//buffer的最大值
const maxInt = int(^uint(0) >> 1)

// Bytes returns a slice of length b.Len() holding the unread portion of the buffer.
// The slice is valid for use only until the next buffer modification (that is,
// only until the next call to a method like Read, Write, Reset, or Truncate).
// The slice aliases the buffer content at least until the next buffer modification,
// so immediate changes to the slice will affect the result of future reads.
func (b *Buffer) Bytes()[]byte{return b.buf[b.off:]}

// String returns the contents of the unread portion of the buffer
// as a string. If the Buffer is a nil pointer, it returns "<nil>".
//
// To build strings more efficiently, see the strings.Builder type.
func (b *Buffer)String()string{
	if b == nil{
		// Special case, useful in debugging.
		return "<nil>"
	}
	return string(b.buf[b.off:])
}

// empty reports whether the unread portion of the buffer is empty.
func (b *Buffer)empty()bool{return len(b.buf)<=b.off}

// Len returns the number of bytes of the unread portion of the buffer;
// b.Len() == len(b.Bytes()).
func (b *Buffer)Len() int{return len(b.buf) - b.off}

// Cap returns the capacity of the buffer's underlying byte slice, that is, the
// total space allocated for the buffer's data.
func (b *Buffer)Cap()int{return cap(b.buf)}

// Reset resets the buffer to be empty,
// but it retains the underlying storage for use by future writes.
// Reset is the same as Truncate(0).
//重置缓冲区
func(b *Buffer)Reset(){
	b.buf = b.buf[:0]
	b.off = 0
	b.lastRead = opInvalid
}

// tryGrowByReslice is a inlineable version of grow for the fast-case where the
// internal buffer only needs to be resliced.
// It returns the index where bytes should be written and whether it succeeded.
//尝试取增长缓冲区的空间,仅仅是改变数组大小,不是扩容
//返回true表示没有超出容量,直接改变数组大小,返回false表示需要扩容
func (b *Buffer) tryGrowByReslice(n int)(int,bool){
	if l := len(b.buf); n <= cap(b.buf)-l {
		b.buf = b.buf[:l+n]
		return l,true
	}
	return 0,false
}

// grow grows the buffer to guarantee space for n more bytes.
// It returns the index where bytes should be written.
// If the buffer can't grow it will panic with ErrTooLarge.
//buffer需要扩容
func (b *Buffer)grow(n int) int {
	m := b.Len()
	// If buffer is empty, reset to recover space.
	if m == 0 && b.off != 0{
		b.Reset()
	}
	// Try to grow by means of a reslice.
	if i,ok :=b.tryGrowByReslice(n);ok{
		return i
	}
	//一开始缓冲区为nil并且是小对象
	if b.buf == nil && n<= smallBufferSize{
		b.buf = make([]byte,n,smallBufferSize)
	}
	c :=cap(b.buf)
	if n <= c/2-m{
		// We can slide things down instead of allocating a new
		// slice. We only need m+n <= c to slide, but
		// we instead let capacity get twice as large so we
		// don't spend all our time copying.
		//这里看似多余如果用并发的思想去想就合理
		copy(b.buf,b.buf[b.off:])
	}else if c > maxInt-c-n{
		panic(ErrTooLarge)
	}else{
		// Not enough space anywhere, we need to allocate.
		//buffer不足,需要两倍扩容
		buf :=makeSlice(2*c + n)
		copy(buf,b.buf[b.off:])
		b.buf = buf
	}
	// Restore b.off and len(b.buf).
	b.off = 0
	b.buf = b.buf[:m+n]
	return m
}

// Write appends the contents of p to the buffer, growing the buffer as
// needed. The return value n is the length of p; err is always nil. If the
// buffer becomes too large, Write will panic with ErrTooLarge.
func (b *Buffer)Write(p []byte) (n int, err error) {
	b.lastRead = opInvalid
	m,ok :=b.tryGrowByReslice(len(p))
	if !ok{
		m = b.grow(len(p))
	}
	return copy(b.buf[m:],p),nil
}

// WriteString appends the contents of s to the buffer, growing the buffer as
// needed. The return value n is the length of s; err is always nil. If the
// buffer becomes too large, WriteString will panic with ErrTooLarge.
func (b *Buffer) WriteString(s string) (n int, err error) {
	b.lastRead = opInvalid
	//buf每次写入都需要调整一下大小
	m,ok :=b.tryGrowByReslice(len(s))
	if !ok{
		m = b.grow(len(s))
	}
	return copy(b.buf[m:],s),nil
}

// makeSlice allocates a slice of size n. If the allocation fails, it panics
// with ErrTooLarge.
func makeSlice(n int)[]byte{
	return make([]byte,n)
}

// WriteByte appends the byte c to the buffer, growing the buffer as needed.
// The returned error is always nil, but is included to match bufio.Writer's
// WriteByte. If the buffer becomes too large, WriteByte will panic with
// ErrTooLarge.
func (b *Buffer) WriteByte(c byte) error {
	b.lastRead =opInvalid
	m,ok := b.tryGrowByReslice(1)
	if !ok{
		m = b.grow(1)
	}
	b.buf[m] =c
	return nil
}
