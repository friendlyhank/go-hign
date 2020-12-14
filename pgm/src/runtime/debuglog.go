package runtime

import (
	"runtime/internal/atomic"
	"unsafe"
)

const (
	debugLogUnknown = 1 + iota
	debugLogBoolTrue
	debugLogBoolFalse
	debugLogInt
	debugLogUint
	debugLogHex
	debugLogPtr
	debugLogString
	debugLogConstString
	debugLogStringOverflow

	debugLogPC
	debugLogTraceback
)

// debugLogBytes is the size of each per-M ring buffer. This is
// allocated off-heap to avoid blowing up the M and hence the GC'd
// heap size.
const debugLogBytes = 16 << 10

// A dlogger writes to the debug log.
//
// To obtain a dlogger, call dlog(). When done with the dlogger, call
// end().
//
//go:notinheap
type dlogger struct {
	w debugLogWriter

	// allLink is the next dlogger in the allDloggers list.
	allLink *dlogger

	// owned indicates that this dlogger is owned by an M. This is
	// accessed atomically.
	owned uint32
}

// A debugLogWriter is a ring buffer of binary debug log records.
//
// A log record consists of a 2-byte framing header and a sequence of
// fields. The framing header gives the size of the record as a little
// endian 16-bit value. Each field starts with a byte indicating its
// type, followed by type-specific data. If the size in the framing
// header is 0, it's a sync record consisting of two little endian
// 64-bit values giving a new time base.
//
// Because this is a ring buffer, new records will eventually
// overwrite old records. Hence, it maintains a reader that consumes
// the log as it gets overwritten. That reader state is where an
// actual log reader would start.
//
//go:notinheap
type debugLogWriter struct {
	write uint64
	data  debugLogBuf

	// tick and nano are the time bases from the most recently
	// written sync record.
	tick, nano uint64

	// r is a reader that consumes records as they get overwritten
	// by the writer. It also acts as the initial reader state
	// when printing the log.
	r debugLogReader

	// buf is a scratch buffer for encoding. This is here to
	// reduce stack usage.
	buf [10]byte
}

type debugLogReader struct {
	data *debugLogBuf

	// begin and end are the positions in the log of the beginning
	// and end of the log data, modulo len(data).
	begin, end uint64

	// tick and nano are the current time base at begin.
	tick, nano uint64
}

//go:notinheap
type debugLogBuf [debugLogBytes]byte

// allDloggers is a list of all dloggers, linked through
// dlogger.allLink. This is accessed atomically. This is prepend only,
// so it doesn't need to protect against ABA races.
var allDloggers *dlogger

// printDebugLog prints the debug log.
func printDebugLog() {
	if !dlogEnabled {
		return
	}

	// This function should not panic or throw since it is used in
	// the fatal panic path and this may deadlock.

	printlock()

	// Get the list of all debug logs.
	allp := (*uintptr)(unsafe.Pointer(&allDloggers))
	all := (*dlogger)(unsafe.Pointer(atomic.Loaduintptr(allp)))

	// Count the logs.
	n := 0
	for l := all; l != nil; l = l.allLink {
		n++
	}
	if n == 0 {
		printunlock()
		return
	}

	// Prepare read state for all logs.
	type readState struct {
		debugLogReader
		first    bool
		lost     uint64
		nextTick uint64
	}
	state1 := sysAlloc(unsafe.Sizeof(readState{})*uintptr(n), nil)
	if state1 == nil {
		println("failed to allocate read state for", n, "logs")
		printunlock()
		return
	}
	state := (*[1 << 20]readState)(state1)[:n]
	{
		l := all
		for i := range state {
			s := &state[i]
			s.debugLogReader = l.w.r
			s.first = true
			s.lost = l.w.r.begin
			s.nextTick = s.peek()
			l = l.allLink
		}
	}

	// Print records.
	for {
		// Find the next record.
		var best struct {
			tick uint64
			i    int
		}
		best.tick = ^uint64(0)
		for i := range state {
			if state[i].nextTick < best.tick {
				best.tick = state[i].nextTick
				best.i = i
			}
		}
		if best.tick == ^uint64(0) {
			break
		}

		// Print record.
		s := &state[best.i]
		if s.first {
			print(">> begin log ", best.i)
			if s.lost != 0 {
				print("; lost first ", s.lost>>10, "KB")
			}
			print(" <<\n")
			s.first = false
		}

		end, _, nano, p := s.header()
		oldEnd := s.end
		s.end = end

		print("[")
		var tmpbuf [21]byte
		pnano := int64(nano) - runtimeInitTime
		if pnano < 0 {
			// Logged before runtimeInitTime was set.
			pnano = 0
		}
		print(string(itoaDiv(tmpbuf[:], uint64(pnano), 9)))
		print(" P ", p, "] ")

		for i := 0; s.begin < s.end; i++ {
			if i > 0 {
				print(" ")
			}
			if !s.printVal() {
				// Abort this P log.
				print("<aborting P log>")
				end = oldEnd
				break
			}
		}
		println()

		// Move on to the next record.
		s.begin = end
		s.end = oldEnd
		s.nextTick = s.peek()
	}

	printunlock()
}

func (r *debugLogReader) printVal() bool {
	typ := r.data[r.begin%uint64(len(r.data))]
	r.begin++

	switch typ {
	default:
		print("<unknown field type ", hex(typ), " pos ", r.begin-1, " end ", r.end, ">\n")
		return false

	case debugLogUnknown:
		print("<unknown kind>")

	case debugLogBoolTrue:
		print(true)

	case debugLogBoolFalse:
		print(false)

	case debugLogInt:
		print(r.varint())

	case debugLogUint:
		print(r.uvarint())

	case debugLogHex, debugLogPtr:
		print(hex(r.uvarint()))

	case debugLogString:
		sl := r.uvarint()
		if r.begin+sl > r.end {
			r.begin = r.end
			print("<string length corrupted>")
			break
		}
		for sl > 0 {
			b := r.data[r.begin%uint64(len(r.data)):]
			if uint64(len(b)) > sl {
				b = b[:sl]
			}
			r.begin += uint64(len(b))
			sl -= uint64(len(b))
			gwrite(b)
		}

	case debugLogConstString:
		len, ptr := int(r.uvarint()), uintptr(r.uvarint())
		ptr += firstmoduledata.etext
		str := stringStruct{
			str: unsafe.Pointer(ptr),
			len: len,
		}
		s := *(*string)(unsafe.Pointer(&str))
		print(s)

	case debugLogStringOverflow:
		print("..(", r.uvarint(), " more bytes)..")

	case debugLogPC:
		printDebugLogPC(uintptr(r.uvarint()), false)

	case debugLogTraceback:
		n := int(r.uvarint())
		for i := 0; i < n; i++ {
			print("\n\t")
			// gentraceback PCs are always return PCs.
			// Convert them to call PCs.
			//
			// TODO(austin): Expand inlined frames.
			printDebugLogPC(uintptr(r.uvarint()), true)
		}
	}

	return true
}

// printDebugLogPC prints a single symbolized PC. If returnPC is true,
// pc is a return PC that must first be converted to a call PC.
func printDebugLogPC(pc uintptr, returnPC bool) {
	fn := findfunc(pc)
	if returnPC && (!fn.valid() || pc > fn.entry) {
		// TODO(austin): Don't back up if the previous frame
		// was a sigpanic.
		pc--
	}

	print(hex(pc))
	if !fn.valid() {
		print(" [unknown PC]")
	} else {
		name := funcname(fn)
		file, line := funcline(fn, pc)
		print(" [", name, "+", hex(pc-fn.entry),
			" ", file, ":", line, "]")
	}
}

func (r *debugLogReader) uvarint() uint64 {
	var u uint64
	for i := uint(0); ; i += 7 {
		b := r.data[r.begin%uint64(len(r.data))]
		r.begin++
		u |= uint64(b&^0x80) << i
		if b&0x80 == 0 {
			break
		}
	}
	return u
}

func (r *debugLogReader) varint() int64 {
	u := r.uvarint()
	var v int64
	if u&1 == 0 {
		v = int64(u >> 1)
	} else {
		v = ^int64(u >> 1)
	}
	return v
}

//go:nosplit
func (l *debugLogWriter) uvarint(u uint64) {
	i := 0
	for u >= 0x80 {
		l.buf[i] = byte(u) | 0x80
		u >>= 7
		i++
	}
	l.buf[i] = byte(u)
	i++
	l.bytes(l.buf[:i])
}

const (
	// debugLogHeaderSize is the number of bytes in the framing
	// header of every dlog record.
	debugLogHeaderSize = 2

	// debugLogSyncSize is the number of bytes in a sync record.
	debugLogSyncSize = debugLogHeaderSize + 2*8
)

func (r *debugLogReader) header() (end, tick, nano uint64, p int) {
	// Read size. We've already skipped sync packets and checked
	// bounds in peek.
	size := uint64(r.readUint16LEAt(r.begin))
	end = r.begin + size
	r.begin += debugLogHeaderSize

	// Read tick, nano, and p.
	tick = r.uvarint() + r.tick
	nano = r.uvarint() + r.nano
	p = int(r.varint())

	return
}

//go:nosplit
func (r *debugLogReader) readUint16LEAt(pos uint64) uint16 {
	return uint16(r.data[pos%uint64(len(r.data))]) |
		uint16(r.data[(pos+1)%uint64(len(r.data))])<<8
}

func (r *debugLogReader) peek() (tick uint64) {
	// Consume any sync records.
	size := uint64(0)
	for size == 0 {
		if r.begin+debugLogHeaderSize > r.end {
			return ^uint64(0)
		}
		size = uint64(r.readUint16LEAt(r.begin))
		if size != 0 {
			break
		}
		if r.begin+debugLogSyncSize > r.end {
			return ^uint64(0)
		}
		// Sync packet.
		r.tick = r.readUint64LEAt(r.begin + debugLogHeaderSize)
		r.nano = r.readUint64LEAt(r.begin + debugLogHeaderSize + 8)
		r.begin += debugLogSyncSize
	}

	// Peek tick delta.
	if r.begin+size > r.end {
		return ^uint64(0)
	}
	pos := r.begin + debugLogHeaderSize
	var u uint64
	for i := uint(0); ; i += 7 {
		b := r.data[pos%uint64(len(r.data))]
		pos++
		u |= uint64(b&^0x80) << i
		if b&0x80 == 0 {
			break
		}
	}
	if pos > r.begin+size {
		return ^uint64(0)
	}
	return r.tick + u
}

//go:nosplit
func (r *debugLogReader) readUint64LEAt(pos uint64) uint64 {
	var b [8]byte
	for i := range b {
		b[i] = r.data[pos%uint64(len(r.data))]
		pos++
	}
	return uint64(b[0]) | uint64(b[1])<<8 |
		uint64(b[2])<<16 | uint64(b[3])<<24 |
		uint64(b[4])<<32 | uint64(b[5])<<40 |
		uint64(b[6])<<48 | uint64(b[7])<<56
}

//go:nosplit
func (l *debugLogWriter) bytes(x []byte) {
	l.ensure(uint64(len(x)))
	pos := l.write
	l.write += uint64(len(x))
	for len(x) > 0 {
		n := copy(l.data[pos%uint64(len(l.data)):], x)
		pos += uint64(n)
		x = x[n:]
	}
}

//go:nosplit
func (l *debugLogWriter) ensure(n uint64) {
	for l.write+n >= l.r.begin+uint64(len(l.data)) {
		// Consume record at begin.
		if l.r.skip() == ^uint64(0) {
			// Wrapped around within a record.
			//
			// TODO(austin): It would be better to just
			// eat the whole buffer at this point, but we
			// have to communicate that to the reader
			// somehow.
			throw("record wrapped around")
		}
	}
}

//go:nosplit
func (r *debugLogReader) skip() uint64 {
	// Read size at pos.
	if r.begin+debugLogHeaderSize > r.end {
		return ^uint64(0)
	}
	size := uint64(r.readUint16LEAt(r.begin))
	if size == 0 {
		// Sync packet.
		r.tick = r.readUint64LEAt(r.begin + debugLogHeaderSize)
		r.nano = r.readUint64LEAt(r.begin + debugLogHeaderSize + 8)
		size = debugLogSyncSize
	}
	if r.begin+size > r.end {
		return ^uint64(0)
	}
	r.begin += size
	return size
}
