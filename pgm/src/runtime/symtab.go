package runtime

import (
	"runtime/internal/sys"
	"unsafe"
)

// PCDATA and FUNCDATA table indexes.
//
// See funcdata.h and ../cmd/internal/objabi/funcdata.go.
const (
	_PCDATA_RegMapIndex   = 0 // if !go115ReduceLiveness
	_PCDATA_UnsafePoint   = 0 // if go115ReduceLiveness
	_PCDATA_StackMapIndex = 1
	_PCDATA_InlTreeIndex  = 2

	_FUNCDATA_ArgsPointerMaps    = 0
	_FUNCDATA_LocalsPointerMaps  = 1
	_FUNCDATA_RegPointerMaps     = 2 // if !go115ReduceLiveness
	_FUNCDATA_StackObjects       = 3
	_FUNCDATA_InlTree            = 4
	_FUNCDATA_OpenCodedDeferInfo = 5

	_ArgsSizeUnknown = -0x80000000
)

const (
	// PCDATA_UnsafePoint values.
	_PCDATA_UnsafePointSafe   = -1 // Safe for async preemption
	_PCDATA_UnsafePointUnsafe = -2 // Unsafe for async preemption

	// _PCDATA_Restart1(2) apply on a sequence of instructions, within
	// which if an async preemption happens, we should back off the PC
	// to the start of the sequence when resume.
	// We need two so we can distinguish the start/end of the sequence
	// in case that two sequences are next to each other.
	_PCDATA_Restart1 = -3
	_PCDATA_Restart2 = -4

	// Like _PCDATA_RestartAtEntry, but back to function entry if async
	// preempted.
	_PCDATA_RestartAtEntry = -5
)


// moduledata records information about the layout of the executable
// image. It is written by the linker. Any changes here must be
// matched changes to the code in hcmd/internal/ld/symtab.go:symtab.
// moduledata is stored in statically allocated non-pointer memory;
// none of the pointers here are visible to the garbage collector.
type moduledata struct {
	pclntable    []byte
	ftab         []functab
	filetab      []uint32
	findfunctab  uintptr
	minpc, maxpc uintptr

	text, etext           uintptr
	noptrdata, enoptrdata uintptr
	data, edata           uintptr
	bss, ebss             uintptr
	noptrbss, enoptrbss   uintptr
	end, gcdata, gcbss    uintptr
	types, etypes         uintptr

	textsectmap []textsect
	typelinks   []int32 // offsets from types
	itablinks   []*itab

	ptab []ptabEntry

	pluginpath string
	pkghashes  []modulehash

	modulename   string
	modulehashes []modulehash

	hasmain uint8 // 1 if module contains the main function, 0 otherwise

	gcdatamask, gcbssmask bitvector

	typemap map[typeOff]*_type // offset to *_rtype in previous module

	bad bool // module failed to load and should be ignored

	next *moduledata
}

// A modulehash is used to compare the ABI of a new module or a
// package in a new module with the loaded program.
//
// For each shared library a module links against, the linker creates an entry in the
// moduledata.modulehashes slice containing the name of the module, the abi hash seen
// at link time and a pointer to the runtime abi hash. These are checked in
// moduledataverify1 below.
//
// For each loaded plugin, the pkghashes slice has a modulehash of the
// newly loaded package that can be used to check the plugin's version of
// a package against any previously loaded version of the package.
// This is done in plugin.lastmoduleinit.
type modulehash struct {
	modulename   string
	linktimehash string
	runtimehash  *string
}

// Information from the compiler about the layout of stack frames.
// Note: this type must agree with reflect.bitVector.
type bitvector struct {
	n        int32 // # of bits
	bytedata *uint8
}

type functab struct{
	entry   uintptr
	funcoff uintptr
}

// Mapping information for secondary text sections
type textsect struct {}

var firstmoduledata moduledata  // linker symbol
var lastmoduledatap *moduledata // linker symbol
var modulesSlice *[]*moduledata // see activeModules

const minfunc = 16                 // minimum function size
const pcbucketsize = 256 * minfunc // size of bucket in the pc->func lookup table

// findfunctab is an array of these structures.
// Each bucket represents 4096 bytes of the text segment.
// Each subbucket represents 256 bytes of the text segment.
// To find a function given a pc, locate the bucket and subbucket for
// that pc. Add together the idx and subbucket value to obtain a
// function index. Then scan the functab array starting at that
// index to find the target function.
// This table uses 20 bytes for every 4096 bytes of code, or ~0.5% overhead.
type findfuncbucket struct {
	idx        uint32
	subbuckets [16]byte
}

type pcvalueCache struct {
	entries [2][8]pcvalueCacheEnt
}

type pcvalueCacheEnt struct {
	// targetpc and off together are the key of this cache entry.
	targetpc uintptr
	off      int32
	// val is the value of this cached pcvalue entry.
	val int32
}

func cfuncname(f funcInfo) *byte {
	if !f.valid() || f.nameoff == 0 {
		return nil
	}
	return &f.datap.pclntable[f.nameoff]
}

func funcname(f funcInfo) string {
	return gostringnocopy(cfuncname(f))
}

func funcspdelta(f funcInfo, targetpc uintptr, cache *pcvalueCache) int32 {
	x, _ := pcvalue(f, f.pcsp, targetpc, cache, true)
	if x&(sys.PtrSize-1) != 0 {
		print("invalid spdelta ", funcname(f), " ", hex(f.entry), " ", hex(targetpc), " ", hex(f.pcsp), " ", x, "\n")
	}
	return x
}

// pcvalueCacheKey returns the outermost index in a pcvalueCache to use for targetpc.
// It must be very cheap to calculate.
// For now, align to sys.PtrSize and reduce mod the number of entries.
// In practice, this appears to be fairly randomly and evenly distributed.
func pcvalueCacheKey(targetpc uintptr) uintptr {
	return (targetpc / sys.PtrSize) % uintptr(len(pcvalueCache{}.entries))
}

// Like pcdatavalue, but also return the start PC of this PCData value.
// It doesn't take a cache.
func pcdatavalue2(f funcInfo, table int32, targetpc uintptr) (int32, uintptr) {
	if table < 0 || table >= f.npcdata {
		return -1, 0
	}
	return pcvalue(f, pcdatastart(f, table), targetpc, nil, true)
}

// readvarint reads a varint from p.
func readvarint(p []byte) (read uint32, val uint32) {
	var v, shift, n uint32
	for {
		b := p[n]
		n++
		v |= uint32(b&0x7F) << (shift & 31)
		if b&0x80 == 0 {
			break
		}
		shift += 7
	}
	return n, v
}

func funcdata(f funcInfo, i uint8) unsafe.Pointer {
	if i < 0 || i >= f.nfuncdata {
		return nil
	}
	p := add(unsafe.Pointer(&f.nfuncdata), unsafe.Sizeof(f.nfuncdata)+uintptr(f.npcdata)*4)
	if sys.PtrSize == 8 && uintptr(p)&4 != 0 {
		if uintptr(unsafe.Pointer(f._func))&4 != 0 {
			println("runtime: misaligned func", f._func)
		}
		p = add(p, 4)
	}
	return *(*unsafe.Pointer)(add(p, uintptr(i)*sys.PtrSize))
}

// step advances to the next pc, value pair in the encoded table.
func step(p []byte, pc *uintptr, val *int32, first bool) (newp []byte, ok bool) {
	// For both uvdelta and pcdelta, the common case (~70%)
	// is that they are a single byte. If so, avoid calling readvarint.
	uvdelta := uint32(p[0])
	if uvdelta == 0 && !first {
		return nil, false
	}
	n := uint32(1)
	if uvdelta&0x80 != 0 {
		n, uvdelta = readvarint(p)
	}
	*val += int32(-(uvdelta & 1) ^ (uvdelta >> 1))
	p = p[n:]

	pcdelta := uint32(p[0])
	n = 1
	if pcdelta&0x80 != 0 {
		n, pcdelta = readvarint(p)
	}
	p = p[n:]
	*pc += uintptr(pcdelta * sys.PCQuantum)
	return p, true
}

func pcdatavalue(f funcInfo, table int32, targetpc uintptr, cache *pcvalueCache) int32 {
	if table < 0 || table >= f.npcdata {
		return -1
	}
	r, _ := pcvalue(f, pcdatastart(f, table), targetpc, cache, true)
	return r
}

func pcdatastart(f funcInfo, table int32) int32 {
	return *(*int32)(add(unsafe.Pointer(&f.nfuncdata), unsafe.Sizeof(f.nfuncdata)+uintptr(table)*4))
}

// Returns the PCData value, and the PC where this value starts.
// TODO: the start PC is returned only when cache is nil.
func pcvalue(f funcInfo, off int32, targetpc uintptr, cache *pcvalueCache, strict bool) (int32, uintptr) {
	if off == 0 {
		return -1, 0
	}

	// Check the cache. This speeds up walks of deep stacks, which
	// tend to have the same recursive functions over and over.
	//
	// This cache is small enough that full associativity is
	// cheaper than doing the hashing for a less associative
	// cache.
	if cache != nil {
		x := pcvalueCacheKey(targetpc)
		for i := range cache.entries[x] {
			// We check off first because we're more
			// likely to have multiple entries with
			// different offsets for the same targetpc
			// than the other way around, so we'll usually
			// fail in the first clause.
			ent := &cache.entries[x][i]
			if ent.off == off && ent.targetpc == targetpc {
				return ent.val, 0
			}
		}
	}

	if !f.valid() {
		if strict && panicking == 0 {
			print("runtime: no module data for ", hex(f.entry), "\n")
			throw("no module data")
		}
		return -1, 0
	}
	datap := f.datap
	p := datap.pclntable[off:]
	pc := f.entry
	prevpc := pc
	val := int32(-1)
	for {
		var ok bool
		p, ok = step(p, &pc, &val, pc == f.entry)
		if !ok {
			break
		}
		if targetpc < pc {
			// Replace a random entry in the cache. Random
			// replacement prevents a performance cliff if
			// a recursive stack's cycle is slightly
			// larger than the cache.
			// Put the new element at the beginning,
			// since it is the most likely to be newly used.
			if cache != nil {
				x := pcvalueCacheKey(targetpc)
				e := &cache.entries[x]
				ci := fastrand() % uint32(len(cache.entries[x]))
				e[ci] = e[0]
				e[0] = pcvalueCacheEnt{
					targetpc: targetpc,
					off:      off,
					val:      val,
				}
			}

			return val, prevpc
		}
		prevpc = pc
	}

	// If there was a table, it should have covered all program counters.
	// If not, something is wrong.
	if panicking != 0 || !strict {
		return -1, 0
	}

	print("runtime: invalid pc-encoded table f=", funcname(f), " pc=", hex(pc), " targetpc=", hex(targetpc), " tab=", p, "\n")

	p = datap.pclntable[off:]
	pc = f.entry
	val = -1
	for {
		var ok bool
		p, ok = step(p, &pc, &val, pc == f.entry)
		if !ok {
			break
		}
		print("\tvalue=", val, " until pc=", hex(pc), "\n")
	}

	throw("invalid runtime symbol table")
	return -1, 0
}

// NOTE: Func does not expose the actual unexported fields, because we return *Func
// values to users, and we want to keep them from being able to overwrite the data
// with (say) *f = Func{}.
// All code operating on a *Func must call raw() to get the *_func
// or funcInfo() to get the funcInfo instead.

// A Func represents a Go function in the running binary.
type Func struct {
	opaque struct{} // unexported field to disallow conversions
}

func (f *Func) raw() *_func {
	return (*_func)(unsafe.Pointer(f))
}

// A FuncID identifies particular functions that need to be treated
// specially by the runtime.
// Note that in some situations involving plugins, there may be multiple
// copies of a particular special runtime function.
// Note: this list must match the list in cmd/internal/objabi/funcid.go.
type funcID uint8

const (
	funcID_normal funcID = iota // not a special function
	funcID_runtime_main
	funcID_goexit
	funcID_jmpdefer
	funcID_mcall
	funcID_morestack
	funcID_mstart
	funcID_rt0_go
	funcID_asmcgocall
	funcID_sigpanic
	funcID_runfinq
	funcID_gcBgMarkWorker
	funcID_systemstack_switch
	funcID_systemstack
	funcID_cgocallback_gofunc
	funcID_gogo
	funcID_externalthreadhandler
	funcID_debugCallV1
	funcID_gopanic
	funcID_panicwrap
	funcID_handleAsyncEvent
	funcID_asyncPreempt
	funcID_wrapper // any autogenerated code (hash/eq algorithms, method wrappers, etc.)
)

func findmoduledatap(pc uintptr) *moduledata {
	for datap := &firstmoduledata; datap != nil; datap = datap.next {
		if datap.minpc <= pc && pc < datap.maxpc {
			return datap
		}
	}
	return nil
}

type funcInfo struct {
	*_func
	datap *moduledata
}

func (f funcInfo) valid() bool {
	return f._func != nil
}

func (f funcInfo) _Func() *Func {
	return (*Func)(unsafe.Pointer(f._func))
}

func funcnameFromNameoff(f funcInfo, nameoff int32) string {
	return gostringnocopy(cfuncnameFromNameoff(f, nameoff))
}

func cfuncnameFromNameoff(f funcInfo, nameoff int32) *byte {
	if !f.valid() {
		return nil
	}
	return &f.datap.pclntable[nameoff]
}

func findfunc(pc uintptr) funcInfo {
	datap := findmoduledatap(pc)
	if datap == nil {
		return funcInfo{}
	}
	const nsub = uintptr(len(findfuncbucket{}.subbuckets))

	x := pc - datap.minpc
	b := x / pcbucketsize
	i := x % pcbucketsize / (pcbucketsize / nsub)

	ffb := (*findfuncbucket)(add(unsafe.Pointer(datap.findfunctab), b*unsafe.Sizeof(findfuncbucket{})))
	idx := ffb.idx + uint32(ffb.subbuckets[i])

	// If the idx is beyond the end of the ftab, set it to the end of the table and search backward.
	// This situation can occur if multiple text sections are generated to handle large text sections
	// and the linker has inserted jump tables between them.

	if idx >= uint32(len(datap.ftab)) {
		idx = uint32(len(datap.ftab) - 1)
	}
	if pc < datap.ftab[idx].entry {
		// With multiple text sections, the idx might reference a function address that
		// is higher than the pc being searched, so search backward until the matching address is found.

		for datap.ftab[idx].entry > pc && idx > 0 {
			idx--
		}
		if idx == 0 {
			throw("findfunc: bad findfunctab entry idx")
		}
	} else {
		// linear search to find func with pc >= entry.
		for datap.ftab[idx+1].entry <= pc {
			idx++
		}
	}
	funcoff := datap.ftab[idx].funcoff
	if funcoff == ^uintptr(0) {
		// With multiple text sections, there may be functions inserted by the external
		// linker that are not known by Go. This means there may be holes in the PC
		// range covered by the func table. The invalid funcoff value indicates a hole.
		// See also cmd/link/internal/ld/pcln.go:pclntab
		return funcInfo{}
	}
	return funcInfo{(*_func)(unsafe.Pointer(&datap.pclntable[funcoff])), datap}
}

// inlinedCall is the encoding of entries in the FUNCDATA_InlTree table.
type inlinedCall struct {
	parent   int16  // index of parent in the inltree, or < 0
	funcID   funcID // type of the called function
	_        byte
	file     int32 // fileno index into filetab
	line     int32 // line number of the call site
	func_    int32 // offset into pclntab for name of called function
	parentPc int32 // position of an instruction whose source position is the call site (offset from entry)
}