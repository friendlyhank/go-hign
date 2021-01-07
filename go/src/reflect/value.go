package reflect

import (
	"internal/unsafeheader"
	"unsafe"
)

const ptrSize = 4 << (^uintptr(0) >> 63) // unsafe.Sizeof(uintptr(0)) but an ideal const

// Value is the reflection interface to a Go value.
//
// Not all methods apply to all kinds of values. Restrictions,
// if any, are noted in the documentation for each method.
// Use the Kind method to find out the kind of value before
// calling kind-specific methods. Calling a method
// inappropriate to the kind of type causes a run time panic.
//
// The zero Value represents no value.
// Its IsValid method returns false, its Kind method returns Invalid,
// its String method returns "<invalid Value>", and all other methods panic.
// Most functions and methods never return an invalid value.
// If one does, its documentation states the conditions explicitly.
//
// A Value can be used concurrently by multiple goroutines provided that
// the underlying Go value can be used concurrently for the equivalent
// direct operations.
//
// To compare two Values, compare the results of the Interface method.
// Using == on two Values does not compare the underlying values
// they represent.
type Value struct{
	// typ holds the type of the value represented by a Value.
	typ *rtype

	// Pointer-valued data or, if flagIndir is set, pointer to data.
	// Valid when either flagIndir is set or typ.pointers() is true.
	ptr unsafe.Pointer

	// flag holds metadata about the value.
	// The lowest bits are flag bits:
	//	- flagStickyRO: obtained via unexported not embedded field, so read-only
	//	- flagEmbedRO: obtained via unexported embedded field, so read-only
	//	- flagIndir: val holds a pointer to the data
	//	- flagAddr: v.CanAddr is true (implies flagIndir)
	//	- flagMethod: v is a method value.
	// The next five bits give the Kind of the value.
	// This repeats typ.Kind() except for method values.
	// The remaining 23+ bits give a method number for method values.
	// If flag.kind() != Func, code can assume that flagMethod is unset.
	// If ifaceIndir(typ), code can assume that flagIndir is set.
	//(t *rtype)Kind()一样
	flag

	// A method value represents a curried method invocation
	// like r.Read for some receiver r. The typ+val+flag bits describe
	// the receiver r, but the flag's Kind bits say Func (methods are
	// functions), and the top bits of the flag give the method number
	// in r's type's method table.
}

type flag uintptr

const (
	flagKindWidth        = 5 // there are 27 kinds
	flagKindMask    flag = 1<<flagKindWidth - 1
	flagStickyRO    flag = 1 << 5
	flagEmbedRO     flag = 1 << 6
	flagIndir       flag = 1 << 7
	flagAddr        flag = 1 << 8
	flagMethod      flag = 1 << 9
	flagRO          flag = flagStickyRO | flagEmbedRO
)

//Value可以通过flag获取Kind
func (f flag)kind()Kind{
	return Kind(f & flagKindMask)
}

// pointer returns the underlying pointer represented by v.
// v.Kind() must be Ptr, Map, Chan, Func, or UnsafePointer
//转化为指针,只有Ptr,Map,Chan,Func或者pointter才能转化为指针
func (v Value) pointer()unsafe.Pointer{
	if v.typ.size !=ptrSize || !v.typ.pointers(){
		panic("can't call pointer on a non-pointer Value")
	}
	if v.flag&flagIndir != 0{
		return *(*unsafe.Pointer)(v.ptr)
	}
	return v.ptr
}

//
func unpackEface(i interface{}) Value {
	e := (*emptyInterface)(unsafe.Pointer(&i))
	// NOTE: don't read e.word until we know whether it is really a pointer or not.
	t :=e.typ
	if t == nil{
		return Value{}
	}
	f :=flag(t.Kind())
	return Value{t,e.word,f}
}

// A ValueError occurs when a Value method is invoked on
// a Value that does not support it. Such cases are documented
// in the description of each method.
//反射Value的错误
type ValueError struct {
	Method string
	Kind   Kind
}

// emptyInterface is the header for an interface{} value.
type emptyInterface struct{
	typ *rtype
	word unsafe.Pointer
}

// mustBe panics if f's kind is not expected.
// Making this a method on flag instead of on Value
// (and embedding flag in Value) means that we can write
// the very clear v.mustBe(Bool) and have it compile into
// v.flag.mustBe(Bool), which will only bother to copy the
// single important word for the receiver.
func (f flag)mustBe(expected Kind) {}

// Bool returns v's underlying value.
// It panics if v's kind is not Bool.
//value转为bool类型，如果不是bool类型会报错
func(v Value)Bool()bool{
	v.mustBe(Bool)
	return *(*bool)(v.ptr)
}

// String returns the string v's underlying value, as a string.
// String is a special case because of Go's String method convention.
// Unlike the other getters, it does not panic if v's Kind is not String.
// Instead, it returns a string of the form "<T value>" where T is v's type.
// The fmt package treats Values specially. It does not call their String
// method implicitly but instead prints the concrete values they hold.
//value转化为string类型
func (v Value)String()string{
	switch k :=v.kind();k {
	case Invalid:
		return "<invalid Value>"
	case String:
		return *(*string)(v.ptr)
	}
	return ""
}

// Elem returns the value that the interface v contains
// or that the pointer v points to.
// It panics if v's Kind is not Interface or Ptr.
// It returns the zero Value if v is nil.
//这个方法用于查找下级元素的,Type.Elem也有这个方法,一个返回Value和Type,实现方法大致相同但是也不一样
//这样实现的目的是为了保持链式法保持原来的结构去查询指定的键值
func (v Value) Elem() Value {
	k :=v.kind()
	switch k {
	case Ptr:
		ptr := v.ptr
		// The returned value's address is v's value.
		if ptr == nil{
			return Value{}
		}
		tt :=(*ptrType)(unsafe.Pointer(v.typ))
		typ :=tt.elem
		fl := v.flag&flagRO | flagIndir | flagAddr
		fl |= flag(typ.Kind())
		return Value{typ,ptr,fl}
	}
	panic(&ValueError{"reflect.Value.Elem", v.kind()})
}

// Field returns the i'th field of the struct v.
// It panics if v's Kind is not Struct or i is out of range.
//获取结构体得指定字段,如果不是结构体会抛出异常
func (v Value)Field(i int)Value{
	if v.kind() != Struct{
		panic(&ValueError{"reflect.Value.Field",v.kind()})
	}
	tt :=(*structType)(unsafe.Pointer(v.typ))
	if uint(i)>=uint(len(tt.fields)){
		panic("reflect: Field index out of range")
	}
	field :=&tt.fields[i]
	typ :=field.typ

	// Inherit permission bits from v, but clear flagEmbedRO.
	fl := v.flag&(flagStickyRO|flagIndir|flagAddr) | flag(typ.Kind())

	// Either flagIndir is set and v.ptr points at struct,
	// or flagIndir is not set and v.ptr is the actual struct data.
	// In the former case, we want v.ptr + offset.
	// In the latter case, we must have field.offset = 0,
	// so v.ptr + field.offset is still the correct address.
	ptr := add(v.ptr,field.offset(),"same as non-reflect &v.field")
	return Value{typ,ptr,fl}
}

//Type returns v's type.
//和反射reflect.TypeOf一样
func (v Value)Type()Type{
	f :=v.flag
	if f == 0{
		panic(&ValueError{"reflect.Value.Type", Invalid})
	}

	//如果Value不是方法,则直接返回Value.rtype
	if f&flagMethod == 0{
		return v.typ
	}

	//如果Value是方法
	return nil
}

// Int returns v's underlying value, as an int64.
// It panics if v's Kind is not Int, Int8, Int16, Int32, or Int64.
//value转int64类型,如果v.Kind不是Int,Int8,Int16,Int32,Int64类型会抛出异常
func (v Value)Int()int64{
 	k :=v.kind()
 	p := v.ptr
	switch k {
	case Int:
		return int64(*(*int)(p))
	case Int8:
		return int64(*(*int8)(p))
	case Int16:
		return int64(*(*int16)(p))
	case Int32:
		return int64(*(*int32)(p))
	case Int64:
		return  *(*int64)(p)
	}
	panic(&ValueError{"reflect.Value.Int", v.kind()})
}

// Uint returns v's underlying value, as a uint64.
// It panics if v's Kind is not Uint, Uintptr, Uint8, Uint16, Uint32, or Uint64.
func (v Value)Uint()uint64{
	k := v.kind()
	p := v.ptr
	switch k {
	case Uint:
		return uint64(*(*uint)(p))
	case Uint8:
		return uint64(*(*uint8)(p))
	case Uint16:
		return uint64(*(*uint16)(p))
	case Uint32:
		return uint64(*(*uint32)(p))
	case Uint64:
		return *(*uint64)(p)
	case Uintptr:
		return uint64(*(*uintptr)(p))
	}
	panic(&ValueError{"reflect.Value.Uint", v.kind()})
}

// Float returns v's underlying value, as a float64.
// It panics if v's Kind is not Float32 or Float64
func (v Value)Float()float64{
	k := v.kind()
	switch k {
	case Float32:
		return float64(*(*float32)(v.ptr))
	case Float64:
		return *(*float64)(v.ptr)
	}
	panic(&ValueError{"reflect.Value.Float", v.kind()})
}

// IsNil reports whether its argument v is nil. The argument must be
// a chan, func, interface, map, pointer, or slice value; if it is
// not, IsNil panics. Note that IsNil is not always equivalent to a
// regular comparison with nil in Go. For example, if v was created
// by calling ValueOf with an uninitialized interface variable i,
// i==nil will be true but v.IsNil will panic as v will be the zero
// Value.
//chan,func,interface,map,pointer or slice,pointer类型才能判断是否空值,不然会抛出异常
func (v Value)IsNil() bool {
	k :=v.kind()
	switch k {
	case Chan,Func,Map,Ptr,UnsafePointer:
		if v.flag&flagMethod != 0{
			return false
		}
		ptr := v.ptr
		if v.flag&flagIndir != 0{
			ptr = *(*unsafe.Pointer)(ptr)
		}
		return ptr == nil
	case Interface,Slice:
		// Both interface and slice are nil if first word is 0.
		// Both are always bigger than a word; assume flagIndir.
		return *(*unsafe.Pointer)(v.ptr) == nil
	}
	panic(&ValueError{"reflect.Value.IsNil", v.kind()})
}

// Kind returns v's Kind.
// If v is the zero Value (IsValid returns false), Kind returns Invalid.
func (v Value)Kind()Kind{
	return v.kind()
}

// Len returns v's length.
// It panics if v's Kind is not Array, Chan, Map, Slice, or String.
func (v Value)Len()int{
	k := v.kind()
	switch k {
	case Array:
		tt :=(*arrayType)(unsafe.Pointer(v.typ))
		return int(tt.len)
	case Chan:
		return chanlen(v.pointer())
	case Map:
		return maplen(v.pointer())
	case Slice:
		return (*unsafeheader.Slice)(v.ptr).Len
	case String:
		// String is bigger than a word; assume flagIndir.
		return (*unsafeheader.String)(v.ptr).Len
	}
	panic(&ValueError{"reflect.Value.Len", v.kind()})
}

// ValueOf returns a new Value initialized to the concrete value
// stored in the interface i. ValueOf(nil) returns the zero Value.
func ValueOf(i interface{})Value{
	if i == nil{
		return Value{}
	}

	return unpackEface(i)
}

// implemented in ../runtime
////获取chan长度
func chanlen(ch unsafe.Pointer) int

//获取map长度
//go:noescape
func maplen(m unsafe.Pointer) int
