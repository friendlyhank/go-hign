// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package reflect implements run-time reflection, allowing a program to
// manipulate objects with arbitrary types. The typical use is to take a value
// with static type interface{} and extract its dynamic type information by
// calling TypeOf, which returns a Type.
//
// A call to ValueOf returns a Value representing the run-time data.
// Zero takes a Type and returns a Value representing a zero value
// for that type.
//
// See "The Laws of Reflection" for an introduction to reflection in Go:
// https://golang.org/doc/articles/laws_of_reflection.html
package reflect

import (
	"internal/unsafeheader"
	"strconv"
	"unsafe"
)

// Type is the representation of a Go type.
//
// Not all methods apply to all kinds of types. Restrictions,
// if any, are noted in the documentation for each method.
// Use the Kind method to find out the kind of type before
// calling kind-specific methods. Calling a method
// inappropriate to the kind of type causes a run-time panic.
//
// Type values are comparable, such as with the == operator,
// so they can be used as map keys.
// Two Type values are equal if they represent identical types.
type Type interface {
	// Kind returns the specific kind of this type.
	Kind()Kind //对应kind 所有的数据类型

	// Elem returns a type's element type.
	// It panics if the type's Kind is not Array, Chan, Map, Ptr, or Slice.
	//获取Array,Chan,Map,Ptr或者Slice的元素类型
	Elem() Type

	// Implements reports whether the type implements the interface type u.
	//判断是否继承某个接口
	Implements(u Type) bool

	// Field returns a struct type's i'th field.
	// It panics if the type's Kind is not Struct.
	// It panics if i is not in the range [0, NumField()).
	//根据索引获取指定结构体某个字段
	Field(i int) StructField

	// NumField returns a struct type's field count.
	// It panics if the type's Kind is not Struct.
	//获取结构体的字段个数
	NumField() int

	// Name returns the type's name within its package for a defined type.
	// For other (non-defined) types it returns the empty string.
	Name() string
}

// BUG(rsc): FieldByName and related functions consider struct field names to be equal
// if the names are equal, even if they are unexported names originating
// in different packages. The practical effect of this is that the result of
// t.FieldByName("x") is not well defined if the struct type t contains
// multiple fields named x (embedded from different packages).
// FieldByName may return one of the fields named x or may report that there are none.
// See https://golang.org/issue/4876 for more details.

/*
 * These data structures are known to the compiler (../../cmd/internal/gc/reflect.go).
 * A few are known to ../runtime/type.go to convey to debuggers.
 * They are also known to ../runtime/type.go.
 */

// A Kind represents the specific kind of type that a Type represents.
// The zero Kind is not a valid kind.
type Kind uint

//所有的类型
const(
	Invalid Kind = iota //无效的
	Bool
	Int
	Int8
	Int16
	Int32
	Int64
	Uint
	Uint8
	Uint16
	Uint32
	Uint64
	Uintptr
	Float32
	Float64
	Complex64
	Complex128
	Array
	Chan
	Func
	Interface
	Map
	Ptr
	Slice
	String
	Struct
	UnsafePointer
)

// tflag is used by an rtype to signal what extra type information is
// available in the memory directly following the rtype value.
//
// tflag values must be kept in sync with copies in:
//	cmd/compile/internal/gc/reflect.go
//	cmd/link/internal/ld/decodesym.go
//	runtime/type.go
type tflag uint8

const(
	// tflagUncommon means that there is a pointer, *uncommonType,
	// just beyond the outer type structure.
	//
	// For example, if t.Kind() == Struct and t.tflag&tflagUncommon != 0,
	// then t has uncommonType data and it can be accessed as:
	//
	//	type tUncommon struct {
	//		structType
	//		u uncommonType
	//	}
	//	u := &(*tUncommon)(unsafe.Pointer(t)).u
	//uncommon类型,带有实现的方法
	tflagUncommon tflag = 1 << 0

	// tflagExtraStar means the name in the str field has an
	// extraneous '*' prefix. This is because for most types T in
	// a program, the type *T also exists and reusing the str data
	// saves binary size.
	tflagExtraStar tflag = 1 << 1

	//是否有名字
	// tflagNamed means the type has a name.
	tflagNamed tflag = 1 << 2
)

// rtype is the common implementation of most values.
// It is embedded in other struct types.
//
// rtype must be kept in sync with ../runtime/type.go:/^type._type.
//通用的数据类型
type rtype struct {
	size uintptr //大小
	ptrdata    uintptr //指针数据 number of bytes in the type that can contain pointers
	hash       uint32  // hash of type; avoids computation in hash tables
	tflag      tflag   // extra type information flags
	align      uint8   // alignment of variable with this type
	fieldAlign uint8   // alignment of struct field with this type
	kind uint8 // enumeration for C 对应go的所有数据类型
	// function for comparing objects of this type
	// (ptr to object A, ptr to object B) -> ==?
	equal     func(unsafe.Pointer, unsafe.Pointer) bool
	gcdata    *byte   // garbage collection data
	str       nameOff //名字偏移量信息 string form
	ptrToThis typeOff //获取指针类型的偏移量 type for pointer to this type, may be zero
}

// Method on non-interface type
type method struct{
	name nameOff // name of method
	mtyp typeOff //方法类型 method type (without receiver)
}

// uncommonType is present only for defined types or types with methods
// (if T is a defined type, the uncommonTypes for T and *T have methods).
// Using a pointer to this struct reduces the overall size required
// to describe a non-defined type with no methods.
type uncommonType struct {
	pkgPath nameOff // import path; empty for built-in types like int, string
	mcount  uint16  //方法的数量 number of methods
	xcount  uint16  // number of exported methods
	moff    uint32  //方法的偏移量 offset from this uncommontype to [mcount]method
	_       uint32  // unused
}

// add returns p+x.
//
// The whySafe string is ignored, so that the function still inlines
// as efficiently as p+x, but all call sites should use the string to
// record why the addition is safe, which is to say why the addition
// does not cause x to advance to the very end of p's allocation
// and therefore point incorrectly at the next block in memory.
//偏移量计算,得到某个字段(struct,array,slice)
func add(p unsafe.Pointer,x uintptr,whySafe string)unsafe.Pointer{
	return unsafe.Pointer(uintptr(p)+x)
}

//A StructField describes a single field in a struct.
//结构体对应的字段
type StructField struct {
	// Name is the field name.
	Name string
	// PkgPath is the package path that qualifies a lower case (unexported)
	// field name. It is empty for upper case (exported) field names.
	// See https://golang.org/ref/spec#Uniqueness_of_identifiers
	PkgPath string//包的路径

	Type      Type      // field type
	Tag       StructTag //标签 field tag string
	Offset    uintptr   //相对于结构体的偏移量 offset within struct, in bytes
	Index     []int     //索引 index sequence for Type.FieldByIndex
	Anonymous bool      //是否嵌套字段(如内嵌指针需要进一步解析) is an embedded field
}

// A StructTag is the tag string in a struct field.
//
// By convention, tag strings are a concatenation of
// optionally space-separated key:"value" pairs.
// Each key is a non-empty string consisting of non-control
// characters other than space (U+0020 ' '), quote (U+0022 '"'),
// and colon (U+003A ':').  Each value is quoted using U+0022 '"'
// characters and Go string literal syntax.
type StructTag string

// Get returns the value associated with key in the tag string.
// If there is no such key in the tag, Get returns the empty string.
// If the tag does not have the conventional format, the value
// returned by Get is unspecified. To determine whether a tag is
// explicitly set to the empty string, use Lookup.
//获取结构体标签的具体某个key值
func (tag StructTag) Get(key string) string {
	v, _ := tag.Lookup(key)
	return v
}

// Lookup returns the value associated with key in the tag string.
// If the key is present in the tag the value (which may be empty)
// is returned. Otherwise the returned value will be the empty string.
// The ok return value reports whether the value was explicitly set in
// the tag string. If the tag does not have the conventional format,
// the value returned by Lookup is unspecified.
//查找结构体标签的具体某个key值
func (tag StructTag) Lookup(key string) (value string, ok bool) {
	// When modifying this code, also update the validateStructTag code
	// in cmd/vet/structtag.go.

	for tag != "" {
		// Skip leading space.
		i := 0
		for i < len(tag) && tag[i] == ' ' {
			i++
		}
		tag = tag[i:]
		if tag == "" {
			break
		}

		// Scan to colon. A space, a quote or a control character is a syntax error.
		// Strictly speaking, control chars include the range [0x7f, 0x9f], not just
		// [0x00, 0x1f], but in practice, we ignore the multi-byte control characters
		// as it is simpler to inspect the tag's bytes than the tag's runes.
		i = 0
		for i < len(tag) && tag[i] > ' ' && tag[i] != ':' && tag[i] != '"' && tag[i] != 0x7f {
			i++
		}
		if i == 0 || i+1 >= len(tag) || tag[i] != ':' || tag[i+1] != '"' {
			break
		}
		name := string(tag[:i])
		tag = tag[i+1:]

		// Scan quoted string to find value.
		i = 1
		for i < len(tag) && tag[i] != '"' {
			if tag[i] == '\\' {
				i++
			}
			i++
		}
		if i >= len(tag) {
			break
		}
		qvalue := string(tag[:i+1])
		tag = tag[i+1:]

		if key == name {
			value, err := strconv.Unquote(qvalue)
			if err != nil {
				break
			}
			return value, true
		}
	}
	return "", false
}

//数组类型 arrayType represents a fixed array type.
type arrayType struct{
	rtype
	elem  *rtype // array element type
	len uintptr
}

//chan类型
type chanType struct{
	rtype
	elem  *rtype // channel element type
}

// funcType represents a function type.
//
// A *rtype for each in and out parameter is stored in an array that
// directly follows the funcType (and possibly its uncommonType). So
// a function type with one method, one input, and one output is:
//
//	struct {
//		funcType
//		uncommonType
//		[2]*rtype    // [0] is in, [1] is out
//	}
//方法类型
type funcType struct{
	rtype
	inCount  uint16
	outCount uint16 // top bit is set if last input parameter is ...
}

// imethod represents a method on an interface type
type imethod struct {
	name nameOff //name of method 查找方法名偏移量
	typ  typeOff // .(*FuncType) underneath查找方法地址的偏移量
}

//接口类型 interfaceType represents an interface type.
type interfaceType struct{
	rtype
	pkgPath name      //对应的包名 import path
	methods []imethod //对应的方法 sorted by hash
}

//map类型 mapType represents a map type.
type mapType struct{
	rtype
	key    *rtype //key map key type
	elem   *rtype //value map element (value) type
	bucket *rtype //bucket internal bucket structure
	// function for hashing keys (ptr to key, seed) -> hash
	hasher     func(unsafe.Pointer, uintptr) uintptr
	keysize    uint8  // size of key slot
	valuesize  uint8  // size of value slot
	bucketsize uint16 // size of bucket
	flags      uint32
}

//指针类型 ptrType represents a pointer type.
type ptrType struct{
	rtype
	elem *rtype
}

// sliceType represents a slice type.
//slice类型
type sliceType struct{
	rtype
	elem *rtype // slice element type
}

//结构体类型 structType represents a struct type.
type structType struct {
	rtype
	pkgPath name
	fields  []structField // sorted by offset
}

// Field returns the i'th struct field.
//获取结构体的具体某个字段
func (t *structType) Field(i int) (f StructField) {
	if i < 0 || i >= len(t.fields){
	}
	p :=&t.fields[i]
	f.Type =toType(p.typ)
	f.Name = p.name.name()
	f.Anonymous =p.embedded()
	//如果不需要输出，说明是私有属性,那么要记录下包名
	if !p.name.isExported(){
		f.PkgPath = t.pkgPath.name()
	}
	if tag :=p.name.tag();tag != ""{
		f.Tag =StructTag(tag)
	}
	f.Offset = p.offset()

	// NOTE(rsc): This is the only allocation in the interface
	// presented by a reflect.Type. It would be nice to avoid,
	// at least in the common cases, but we need to make sure
	// that misbehaving clients of reflect cannot affect other
	// uses of reflect. One possibility is CL 5371098, but we
	// postponed that ugliness until there is a demonstrated
	// need for the performance. This is issue 2320.
	f.Index = []int{i}
	return
}

// TypeOf returns the reflection Type that represents the dynamic type of i.
// If i is a nil interface value, TypeOf returns nil.
//反射获取类型
func TypeOf(i interface{})Type{
	eface :=*(*emptyInterface)(unsafe.Pointer(&i))
	return toType(eface.typ)
}

// PtrTo returns the pointer type with element t.
// For example, if t represents type Foo, PtrTo(t) represents *Foo.
//将某个类型转化为指针类型返回
func PtrTo(t Type) Type {
	return t.(*rtype).ptrTo()
}

//将某个类型转化为指针类型返回
func (t *rtype) ptrTo()*rtype{
	//根据偏移量去获取
	if t.ptrToThis != 0{
		return t.typeOff(t.ptrToThis)
	}
	return nil
}

//判断是否继承某个接口
func (t *rtype)Implements(u Type)bool{
	if u == nil{
		panic("reflect: nil type passed to Type.Implements")
	}
	if u.Kind() != Interface{
		panic("reflect: non-interface type passed to Type.Implements")
	}
	return implements(u.(*rtype),t)
}

func implements(T,V *rtype)bool{
	if T.Kind() != Interface{
		return false
	}
	t :=(*interfaceType)(unsafe.Pointer(T))
	if len(t.methods) == 0{
		return true
	}

	// The same algorithm applies in both cases, but the
	// method tables for an interface type and a concrete type
	// are different, so the code is duplicated.
	// In both cases the algorithm is a linear scan over the two
	// lists - T's methods and V's methods - simultaneously.
	// Since method tables are stored in a unique sorted order
	// (alphabetical, with no duplicate method names), the scan
	// through V's methods must hit a match for each of T's
	// methods along the way, or else V does not implement T.
	// This lets us run the scan in overall linear time instead of
	// the quadratic time  a naive search would require.
	// See also ../runtime/iface.go.
	if V.Kind()  == Interface{

	}

	//转化为特定的带方法的类型
	v := V.uncommon()
	if v == nil{
		return false
	}
	i := 0
	vmethods := v.methods()
	for j :=0;j < int(v.mcount);j++{
		//根据偏移量获取方法
		tm :=&t.methods[i]
		tmName :=t.nameOff(tm.name)
		vm :=vmethods[j]
		vmName :=V.nameOff(vm.name)
		if vmName.name() == tmName.name() && V.typeOff(vm.mtyp) == t.typeOff(tm.typ){
			if !tmName.isExported(){
				tmPkgPath :=tmName.pkgPath()
				if tmPkgPath == ""{
					tmPkgPath = t.pkgPath.name()
				}
				vmPkgPath :=vmName.pkgPath()
				if vmPkgPath == ""{
					vmPkgPath = V.nameOff(v.pkgPath).name()
				}
				if tmPkgPath != vmPkgPath {
					continue
				}
			}
			if i++;i >= len(t.methods){
				return true
			}
		}
	}
	return false
}

type structTypeUncommon struct{
	structType
	u uncommonType
}

//structType得结构体字段 Struct field
type structField struct {
	name        name    // name is always non-empty
	typ         *rtype  // type of field
	offsetEmbed uintptr //相对于结构体的偏移量 byte offset of field<<1 | isEmbedded
}

//获得某个字段的偏移量
func (f *structField) offset() uintptr {
	return f.offsetEmbed >> 1
}

//是否嵌入式类型(所谓嵌入式表示包含指针或结构体)
func (f *structField)embedded()bool{
	return f.offsetEmbed&1 != 0
}

// name is an encoded type name with optional extra data.
//
// The first byte is a bit field containing:
//
//	1<<0 the name is exported
//	1<<1 tag data follows the name
//	1<<2 pkgPath nameOff follows the name and tag
//
// The next two bytes are the data length:
//
//	 l := uint16(data[1])<<8 | uint16(data[2])
//
// Bytes [3:3+l] are the string data.
//
// If tag data follows then bytes 3+l and 3+l+1 are the tag length,
// with the data following.
//
// If the import path follows, then 4 bytes at the end of
// the data form a nameOff. The import path is only set for concrete
// methods that are defined in a different package than their type.
//
// If a name starts with "*", then the exported bit represents
// whether the pointed to type is exported.
type name struct {
	bytes *byte
}

func (n name) data(off int, whySafe string) *byte {
	return (*byte)(add(unsafe.Pointer(n.bytes), uintptr(off), whySafe))
}

//是否需要输出字段,输出说明是公有属性,不输出说明是私有属性
func (n name)isExported()bool{
	return (*n.bytes)&(1<<0) != 0
}

func (n name) nameLen() int {
	return int(uint16(*n.data(1, "name len field"))<<8 | uint16(*n.data(2, "name len field")))
}

func (n name) tagLen() int {
	if *n.data(0, "name flag field")&(1<<1) == 0 {
		return 0
	}
	off := 3 + n.nameLen()
	return int(uint16(*n.data(off, "name taglen field"))<<8 | uint16(*n.data(off+1, "name taglen field")))
}

func (n name)name()(s string){
	if n.bytes == nil{
		return
	}
	b :=(*[4]byte)(unsafe.Pointer(n.bytes))
	hdr :=(*unsafeheader.String)(unsafe.Pointer(&s))
	hdr.Data = unsafe.Pointer(&b[3])
	hdr.Len = int(b[1])<<8 | int(b[2])
	return s
}

func (n name)tag()(s string){
	tl := n.tagLen()
	if tl == 0 {
		return ""
	}
	nl := n.nameLen()
	hdr := (*unsafeheader.String)(unsafe.Pointer(&s))
	hdr.Data = unsafe.Pointer(n.data(3+nl+2, "non-empty string"))
	hdr.Len = tl
	return s
}

func (n name) pkgPath() string {
	if n.bytes == nil || *n.data(0, "name flag field")&(1<<2) == 0 {
		return ""
	}
	off := 3 + n.nameLen()
	if tl := n.tagLen(); tl > 0 {
		off += 2 + tl
	}
	var nameOff int32
	// Note that this field may not be aligned in memory,
	// so we cannot use a direct int32 assignment here.
	copy((*[4]byte)(unsafe.Pointer(&nameOff))[:], (*[4]byte)(unsafe.Pointer(n.data(off, "name offset field")))[:])
	pkgPathName := name{(*byte)(resolveTypeOff(unsafe.Pointer(n.bytes), nameOff))}
	return pkgPathName.name()
}

const(
	kindMask        = (1 << 5) - 1
)

var kindNames = []string{
	Invalid:       "invalid",
	Bool:          "bool",
	Int:           "int",
	Int8:          "int8",
	Int16:         "int16",
	Int32:         "int32",
	Int64:         "int64",
	Uint:          "uint",
	Uint8:         "uint8",
	Uint16:        "uint16",
	Uint32:        "uint32",
	Uint64:        "uint64",
	Uintptr:       "uintptr",
	Float32:       "float32",
	Float64:       "float64",
	Complex64:     "complex64",
	Complex128:    "complex128",
	Array:         "array",
	Chan:          "chan",
	Func:          "func",
	Interface:     "interface",
	Map:           "map",
	Ptr:           "ptr",
	Slice:         "slice",
	String:        "string",
	Struct:        "struct",
	UnsafePointer: "unsafe.Pointer",
}

func (t *uncommonType)methods()[]method{
	if t.mcount == 0{
		return nil
	}
	return (*[1 << 16]method)(add(unsafe.Pointer(t), uintptr(t.moff), "t.xcount > 0"))[:t.xcount:t.xcount]
}

// resolveNameOff resolves a name offset from a base pointer.
// The (*rtype).nameOff method is a convenience wrapper for this function.
// Implemented in the runtime package.
func resolveNameOff(ptrInModule unsafe.Pointer, off int32) unsafe.Pointer

// resolveTypeOff resolves an *rtype offset from a base type.
// The (*rtype).typeOff method is a convenience wrapper for this function.
// Implemented in the runtime package.
//resolveTypeOff关联到runtime的reflect_resolveTypeOff
func resolveTypeOff(rtype unsafe.Pointer, off int32) unsafe.Pointer

//TODO HANK 搞清楚为什么判断接口实现的时候是当nameOff和typeOff偏移量去判断的
//这个应该是偏移量的一些计算
type nameOff int32 // offset to a name
type typeOff int32 //指针*rtype的偏移量 offset to an *rtype
type textOff int32 // offset from top of text section

func (t *rtype)nameOff(off nameOff)name{
	return name{(*byte)(resolveNameOff(unsafe.Pointer(t), int32(off)))}
}

func (t *rtype) typeOff(off typeOff) *rtype {
	return (*rtype)(resolveTypeOff(unsafe.Pointer(t), int32(off)))
}

//转化成带方法的特定类型uncommonType
func (t *rtype) uncommon() *uncommonType {
	if t.tflag&tflagUncommon == 0{
		return nil
	}
	switch t.Kind() {
	case Struct:
		return &(*structTypeUncommon)(unsafe.Pointer(t)).u
	case Ptr:
		type u struct{
			ptrType
			u uncommonType
		}
		return &(*u)(unsafe.Pointer(t)).u
	case Func:
		type u struct{
			funcType
			u uncommonType
		}
		return &(*u)(unsafe.Pointer(t)).u
	case Slice:
		type u struct{
			sliceType
			u uncommonType
		}
		return &(*u)(unsafe.Pointer(t)).u
	case Array:
		type u struct{
			arrayType
			u uncommonType
		}
		return &(*u)(unsafe.Pointer(t)).u
	case Chan:
		type u struct{
			chanType
			u uncommonType
		}
		return &(*u)(unsafe.Pointer(t)).u
	case Map:
		type u struct {
			mapType
			u uncommonType
		}
		return &(*u)(unsafe.Pointer(t)).u
	case Interface:
		type u struct{
			interfaceType
			u uncommonType
		}
		return &(*u)(unsafe.Pointer(t)).u
	default:
		type u struct{
			rtype
			u uncommonType
		}
		return &(*u)(unsafe.Pointer(t)).u
	}
}

func (t *rtype)String()string{
	s :=t.nameOff(t.str).name()
	return s
}

func (t *rtype)Kind()Kind{return Kind(t.kind & kindMask)}

//是否指针类型
func (t *rtype)pointers()bool{return t.ptrdata != 0}

func (t *rtype)hasName()bool{
	return t.tflag&tflagNamed != 0
}

func (t *rtype)Name()string{
	if !t.hasName(){
		return ""
	}
	s :=t.String()
	i := len(s) - 1
	for i >=0 && s[i] != '.'{
		i--
	}
	return s[i+1:]
}

func (t *rtype)Elem()Type{
	switch t.Kind() {
	case Array:
		tt :=(*arrayType)(unsafe.Pointer(t))
		return toType(tt.elem)
	case Chan:
		tt := (*chanType)(unsafe.Pointer(t))
		return toType(tt.elem)
	case Map:
		tt :=(*mapType)(unsafe.Pointer(t))
		return toType(tt.elem)
	case Ptr:
		tt := (*ptrType)(unsafe.Pointer(t))
		return toType(tt.elem)
	case Slice:
		tt := (*sliceType)(unsafe.Pointer(t))
		return toType(tt.elem)
	}
	panic("")
}

func (t *rtype) Field(i int) StructField {
	if t.Kind() != Struct {
		panic("reflect: Field of non-struct type ")
	}
	tt := (*structType)(unsafe.Pointer(t))
	return tt.Field(i)
}

func (t *rtype) NumField() int {
	if t.Kind() != Struct {
		panic("reflect: NumField of non-struct type ")
	}
	tt := (*structType)(unsafe.Pointer(t))
	return len(tt.fields)
}

// toType converts from a *rtype to a Type that can be returned
// to the client of package reflect. In gc, the only concern is that
// a nil *rtype must be replaced by a nil Type, but in gccgo this
// function takes care of ensuring that multiple *rtype for the same
// type are coalesced into a single Type.
func toType(t *rtype)Type{
	if t == nil{
		return nil
	}
	return t
}