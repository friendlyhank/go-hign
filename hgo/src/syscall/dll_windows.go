package syscall

import (
	"internal/syscall/windows/sysdll"
	"sync/atomic"
	"unsafe"
)

// DLLError describes reasons for DLL load failures.
type DLLError struct {
	Err error
	ObjName string
	Msg string
}

func(e *DLLError)Error()string{return  e.Msg}

// Implemented in ../runtime/syscall_windows.go.
func Syscall(trap, nargs, a1, a2, a3 uintptr) (r1, r2 uintptr, err Errno)
func loadsystemlibrary(filename *uint16, absoluteFilepath *uint16) (handle uintptr, err Errno)

// A DLL implements access to a single DLL.
type DLL struct {
	Name string
	Handle Handle
}

// We use this for computing the absolute path for system DLLs on systems
// where SEARCH_SYSTEM32 is not available.
var systemDirectoryPrefix string

func init(){
}

// LoadDLL loads the named DLL file into memory.
//
// If name is not an absolute path and is not a known system DLL used by
// Go, Windows will search for the named DLL in many locations, causing
// potential DLL preloading attacks.
//
// Use LazyDLL in golang.org/x/sys/windows for a secure way to
// load system DLLs.
//安全的方式使用LazyDLL,这个包在golang.org/x/sys/windows
func LoadDLL(name string)(*DLL,error){
	//将ddl string类型的转化为utf16
	namep, err := UTF16PtrFromString(name)
	if err != nil{
		return nil,err
	}
	var h uintptr
	var e Errno
	if sysdll.IsSystemDLL[name]{
		absoluteFilepathp,err :=UTF16PtrFromString(systemDirectoryPrefix + name)
		if err != nil{
			return nil,err
		}
		h, e = loadsystemlibrary(namep, absoluteFilepathp)
	}
	if e != 0{
		return nil,&DLLError{
			Err: e,
			ObjName: name,
			Msg:     "Failed to load " + name + ": " + e.Error(),
		}
	}
	d := &DLL{
		Name:name,
		Handle:Handle(h),
	}
	return d,nil
}

func (d *DLL)FindProc(name string)(proc *Proc,err error){
	namep,err :=BytePtrFromString(name)
	if err != nil{
		return nil,err
	}
	p := &Proc{
	}
	println(namep)
	return p,nil
}

// A Proc implements access to a procedure inside a DLL.
type Proc struct{
	Dll *DLL
	Name string
	addr uintptr
}

// Addr returns the address of the procedure represented by p.
// The return value can be passed to Syscall to run the procedure.
func (p *Proc)Addr()uintptr{
	return p.addr
}

// A LazyDLL implements access to a single DLL.
// It will delay the load of the DLL until the first
// call to its Handle method or to one of its
// LazyProc's Addr method.
//
// LazyDLL is subject to the same DLL preloading attacks as documented
// on LoadDLL.
//
// Use LazyDLL in golang.org/x/sys/windows for a secure way to
// load system DLLs.
type LazyDLL struct{
	Name string
	dll *DLL
}

// Load loads DLL file d.Name into memory. It returns an error if fails.
// Load will not try to load DLL, if it is already loaded into memory.
//取内存中load DLL,如果已经存在,则不需要重复去loaded
func (d *LazyDLL)Load()error{
	// Non-racy version of:
	// if d.dll == nil {
	if atomic.LoadPointer((*unsafe.Pointer)(unsafe.Pointer(&d.dll))) == nil {
		if d.dll == nil{
			dll,e := LoadDLL(d.Name)
			if e != nil{
				return e
			}
			// Non-racy version of:
			// d.dll = dll
			atomic.StorePointer((*unsafe.Pointer)(unsafe.Pointer(&d.dll)), unsafe.Pointer(dll))
		}
	}
	return nil
}

func (d *LazyDLL)NewProc(name string)*LazyProc{
	return &LazyProc{l:d,Name: name}
}

// NewLazyDLL creates new LazyDLL associated with DLL file.
func NewLazyDLL(name string)*LazyDLL{
	return &LazyDLL{Name: name}
}

// A LazyProc implements access to a procedure inside a LazyDLL.
// It delays the lookup until the Addr, Call, or Find method is called.
type LazyProc struct{
	Name string
	l *LazyDLL
	proc *Proc
}

// Find searches DLL for procedure named p.Name. It returns
// an error if search fails. Find will not search procedure,
// if it is already found and loaded into memory.
func (p *LazyProc)Find() error{
	// Non-racy version of:
	// if p.proc == nil {
	if atomic.LoadPointer((*unsafe.Pointer)(unsafe.Pointer(&p.proc))) == nil {
		if p.proc == nil{
			e := p.l.Load()
			if e != nil{
				return e
			}
			proc,e := p.l.dll.FindProc(p.Name)
			if e != nil{
				return e
			}
			println(proc)
		}
	}
	return nil
}

// mustFind is like Find but panics if search fails.
func (p *LazyProc)mustFind(){
	e := p.Find()
	if e != nil{
		//painc(e)
	}
}

// Addr returns the address of the procedure represented by p.
// The return value can be passed to Syscall to run the procedure.
func (p *LazyProc)Addr()uintptr{
	p.mustFind()
	return p.proc.Addr()
}

