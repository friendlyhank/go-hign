package reflect

import "unsafe"

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

// ValueOf returns a new Value initialized to the concrete value
// stored in the interface i. ValueOf(nil) returns the zero Value.
func ValueOf(i interface{})Value{
	if i == nil{
		return Value{}
	}

	return unpackEface(i)
}