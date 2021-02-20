package types

//类型kind(和反射有点区别)
// EType describes a kind of type.
type EType uint8

const (
	Txxx EType = iota

	TINT8
	TUINT8
	TINT16
	TUINT16
	TINT32
	TUINT32
	TINT64
	TUINT64
	TINT
	TUINT
	TUINTPTR

	TCOMPLEX64
	TCOMPLEX128

	TFLOAT32
	TFLOAT64

	TBOOL

	TPTR
	TFUNC
	TSLICE
	TARRAY
	TSTRUCT
	TCHAN
	TMAP
	TINTER
	TFORW
	TANY
	TSTRING
	TUNSAFEPTR

	// pseudo-types for literals
	TIDEAL // untyped numeric constants
	TNIL
	TBLANK

	// pseudo-types for frame layout
	TFUNCARGS
	TCHANARGS

	// SSA backend types
	TSSA   // internal types used by SSA backend (flags, memory, etc.)
	TTUPLE // a pair of types, used by SSA backend

	NTYPE
)

// A Type represents a Go type.
//用于表示go特定的类型(和反射的type不同)
type Type struct{
	// Extra contains extra etype-specific fields.
	// As an optimization, those etype-specific structs which contain exactly
	// one pointer-shaped field are stored as values rather than pointers when possible.
	//
	// TMAP: *Map
	// TFORW: *Forward
	// TFUNC: *Func
	// TSTRUCT: *Struct
	// TINTER: *Interface
	// TFUNCARGS: FuncArgs
	// TCHANARGS: ChanArgs
	// TCHAN: *Chan
	// TPTR: Ptr
	// TARRAY: *Array
	// TSLICE: Slice
	Extra interface{} //存储对应数据的结构体

	// Width is the width of this Type in bytes.
	Width int64 //类型的大小 valid if Align > 0

	Etype EType //类型kind kind of type

    flags bitset8 //设置标记,例如该类型是否会被分配到堆上、
}

const (
	typeNotInHeap  = 1 << iota //表示不会分配再堆上 type cannot be heap allocated
)

//判断类型是否分配再堆上
func (t *Type) NotInHeap() bool  { return t.flags&typeNotInHeap != 0 }

//设置不再堆上分配的标记
func (t *Type) SetNotInHeap(b bool)  { t.flags.set(typeNotInHeap, b) }

// Map contains Type fields specific to maps.
type Map struct{

}

// Array contains Type fields specific to array types.
//数组结构体
type Array struct{
	Elem  *Type // element type
	Bound int64 //number of elements; <0 if unknown yet
}

func New(et EType)*Type{
	t :=&Type{
		Etype:et,
		Width:BADWIDTH,
	}
	// TODO(josharian): lazily initialize some of these?
	switch t.Etype {
	case TMAP:
		t.Extra = new(Map)
	}
	return t
}

// NewArray returns a new fixed-length array Type.
func NewArray(elem *Type,bound int64) *Type{
	if bound < 0{

	}
	t := New(TARRAY)
	t.Extra = &Array{Elem: elem,Bound: bound}
	t.SetNotInHeap(elem.NotInHeap())
	return t
}
