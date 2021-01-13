// Copyright 2010 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package json implements encoding and decoding of JSON as defined in
// RFC 7159. The mapping between JSON and Go values is described
// in the documentation for the Marshal and Unmarshal functions.
//
// See "JSON and Go" for an introduction to this package:
// https://golang.org/doc/articles/json_and_go.html
package json

import (
	"bytes"
	"encoding"
	"reflect"
	"strconv"
	"unicode/utf8"
)

// Marshal returns the JSON encoding of v.
//
// Marshal traverses the value v recursively.
// If an encountered value implements the Marshaler interface
// and is not a nil pointer, Marshal calls its MarshalJSON method
// to produce JSON. If no MarshalJSON method is present but the
// value implements encoding.TextMarshaler instead, Marshal calls
// its MarshalText method and encodes the result as a JSON string.
// The nil pointer exception is not strictly necessary
// but mimics a similar, necessary exception in the behavior of
// UnmarshalJSON.
//
// Otherwise, Marshal uses the following type-dependent default encodings:
//
// Boolean values encode as JSON booleans.
//
// Floating point, integer, and Number values encode as JSON numbers.
//
// String values encode as JSON strings coerced to valid UTF-8,
// replacing invalid bytes with the Unicode replacement rune.
// So that the JSON will be safe to embed inside HTML <script> tags,
// the string is encoded using HTMLEscape,
// which replaces "<", ">", "&", U+2028, and U+2029 are escaped
// to "\u003c","\u003e", "\u0026", "\u2028", and "\u2029".
// This replacement can be disabled when using an Encoder,
// by calling SetEscapeHTML(false).
//
// Array and slice values encode as JSON arrays, except that
// []byte encodes as a base64-encoded string, and a nil slice
// encodes as the null JSON value.
//
// Struct values encode as JSON objects.
// Each exported struct field becomes a member of the object, using the
// field name as the object key, unless the field is omitted for one of the
// reasons given below.
//
// The encoding of each struct field can be customized by the format string
// stored under the "json" key in the struct field's tag.
// The format string gives the name of the field, possibly followed by a
// comma-separated list of options. The name may be empty in order to
// specify options without overriding the default field name.
//
// The "omitempty" option specifies that the field should be omitted
// from the encoding if the field has an empty value, defined as
// false, 0, a nil pointer, a nil interface value, and any empty array,
// slice, map, or string.
//
// As a special case, if the field tag is "-", the field is always omitted.
// Note that a field with name "-" can still be generated using the tag "-,".
//
// Examples of struct field tags and their meanings:
//
//   // Field appears in JSON as key "myName".
//   Field int `json:"myName"`
//
//   // Field appears in JSON as key "myName" and
//   // the field is omitted from the object if its value is empty,
//   // as defined above.
//   Field int `json:"myName,omitempty"`
//
//   // Field appears in JSON as key "Field" (the default), but
//   // the field is skipped if empty.
//   // Note the leading comma.
//   Field int `json:",omitempty"`
//
//   // Field is ignored by this package.
//   Field int `json:"-"`
//
//   // Field appears in JSON as key "-".
//   Field int `json:"-,"`
//
// The "string" option signals that a field is stored as JSON inside a
// JSON-encoded string. It applies only to fields of string, floating point,
// integer, or boolean types. This extra level of encoding is sometimes used
// when communicating with JavaScript programs:
//
//    Int64String int64 `json:",string"`
//
// The key name will be used if it's a non-empty string consisting of
// only Unicode letters, digits, and ASCII punctuation except quotation
// marks, backslash, and comma.
//
// Anonymous struct fields are usually marshaled as if their inner exported fields
// were fields in the outer struct, subject to the usual Go visibility rules amended
// as described in the next paragraph.
// An anonymous struct field with a name given in its JSON tag is treated as
// having that name, rather than being anonymous.
// An anonymous struct field of interface type is treated the same as having
// that type as its name, rather than being anonymous.
//
// The Go visibility rules for struct fields are amended for JSON when
// deciding which field to marshal or unmarshal. If there are
// multiple fields at the same level, and that level is the least
// nested (and would therefore be the nesting level selected by the
// usual Go rules), the following extra rules apply:
//
// 1) Of those fields, if any are JSON-tagged, only tagged fields are considered,
// even if there are multiple untagged fields that would otherwise conflict.
//
// 2) If there is exactly one field (tagged or not according to the first rule), that is selected.
//
// 3) Otherwise there are multiple fields, and all are ignored; no error occurs.
//
// Handling of anonymous struct fields is new in Go 1.1.
// Prior to Go 1.1, anonymous struct fields were ignored. To force ignoring of
// an anonymous struct field in both current and earlier versions, give the field
// a JSON tag of "-".
//
// Map values encode as JSON objects. The map's key type must either be a
// string, an integer type, or implement encoding.TextMarshaler. The map keys
// are sorted and used as JSON object keys by applying the following rules,
// subject to the UTF-8 coercion described for string values above:
//   - keys of any string type are used directly
//   - encoding.TextMarshalers are marshaled
//   - integer keys are converted to strings
//
// Pointer values encode as the value pointed to.
// A nil pointer encodes as the null JSON value.
//
// Interface values encode as the value contained in the interface.
// A nil interface value encodes as the null JSON value.
//
// Channel, complex, and function values cannot be encoded in JSON.
// Attempting to encode such a value causes Marshal to return
// an UnsupportedTypeError.
//
// JSON cannot represent cyclic data structures and Marshal does not
// handle them. Passing cyclic structures to Marshal will result in
// an error.
//
func Marshal(v interface{}) ([]byte, error) {
	e := newEncodeState()

	err := e.marshal(v, encOpts{escapeHTML:true})
	if err != nil {
		return nil, err
	}
	buf :=append([]byte(nil),e.Bytes()...)
	return buf, nil
}

func (e *encodeState)error(err error){
	panic(jsonError{err})
}

func isEmptyValue(v reflect.Value)bool{
	switch v.Kind() {
	case reflect.Array,reflect.Map,reflect.Slice,reflect.String:
		return v.Len() == 0
	case reflect.Bool:
		return !v.Bool()
	case reflect.Int,reflect.Int8,reflect.Int16,reflect.Int32,reflect.Int64:
		return v.Int() == 0
	case reflect.Uint,reflect.Uint8,reflect.Uint16,reflect.Uint32,reflect.Uint64,reflect.Uintptr:
		return v.Uint() == 0
	case reflect.Float32,reflect.Float64:
		return v.Float() == 0
	case reflect.Interface,reflect.Ptr:
		return v.IsNil()
	}
	return false
}

// HTMLEscape appends to dst the JSON-encoded src with <, >, &, U+2028 and U+2029
// characters inside string literals changed to \u003c, \u003e, \u0026, \u2028, \u2029
// so that the JSON will be safe to embed inside HTML <script> tags.
// For historical reasons, web browsers don't honor standard HTML
// escaping within <script> tags, so an alternative JSON encoding must
// be used.
//将数据解析成json格式并写入buffer
func HTMLEscape(dst *bytes.Buffer, src []byte) {
	// The characters can only appear in string literals,
	// so just scan the string one byte at a time.
	start := 0
	for i, c := range src {
		if c == '<' || c == '>' || c == '&' {
			if start < i {
				dst.Write(src[start:i])
			}
			dst.WriteString(`\u00`)
			dst.WriteByte(hex[c>>4])
			dst.WriteByte(hex[c&0xF])
			start = i + 1
		}
		// Convert U+2028 and U+2029 (E2 80 A8 and E2 80 A9).
		if c == 0xE2 && i+2 < len(src) && src[i+1] == 0x80 && src[i+2]&^1 == 0xA8 {
			if start < i {
				dst.Write(src[start:i])
			}
			dst.WriteString(`\u202`)
			dst.WriteByte(hex[src[i+2]&0xF])
			start = i + 3
		}
	}
	if start < len(src) {
		dst.Write(src[start:])
	}
}

// Marshaler is the interface implemented by types that
// can marshal themselves into valid JSON.
type Marshaler interface {
	MarshalJSON()([]byte, error)
}

// An UnsupportedTypeError is returned by Marshal when attempting
// to encode an unsupported value type.
type UnsupportedTypeError struct{
	Type reflect.Type
}

func (e *UnsupportedTypeError)Error()string{
	return "json: unsupported type: "
}

type UnsupportedValueError struct{
	Value reflect.Value
	Str string
}

func (e *UnsupportedValueError)Error()string{
	return "json: unsupported value: " + e.Str
}

// A MarshalerError represents an error from calling a MarshalJSON or MarshalText method.
//接口实现的错误
type MarshalerError struct {
	Type       reflect.Type
	Err        error
	sourceFunc string
}

func (e *MarshalerError) Error() string {
	srcFunc := e.sourceFunc
	if srcFunc == ""{
		srcFunc = "MarshalJSON"
	}
	return "json: error calling " + srcFunc +
		" for type "  +
		": " + e.Err.Error()
}

var hex = "0123456789abcdef"

// An encodeState encodes JSON into a bytes.Buffer.
type encodeState struct {
	bytes.Buffer //缓冲池 accumulated output
	scratch [64]byte // 64字节数组去装载数组类型(像是用来临时存储的)
}

func newEncodeState() *encodeState {
	return &encodeState{}
}

// jsonError is an error wrapper type for internal use only.
// Panics with errors are wrapped in jsonError so that the top-level recover
// can distinguish intentional panics from this package.
type jsonError struct{ error }

func (e *encodeState) marshal(v interface{}, opts encOpts) (err error) {
	e.reflectValue(reflect.ValueOf(v), opts)
	return nil
}

// NOTE: keep in sync with stringBytes below.
func (e *encodeState)string(s string,escapeHTML bool){
	e.WriteByte('"')
	start := 0
	if start < len(s){
		e.WriteString(s[start:])
	}
	e.WriteByte('"')
}

// NOTE: keep in sync with string above.
func (e *encodeState)stringBytes(s []byte,escapeHTML bool){
	e.WriteByte('"')
	start := 0
	for i :=0;i<len(s);{
		if b := s[i]; b < utf8.RuneSelf {
			//这说明json数据是遵循html特殊字符安全的
			//另一种是不是html的安全过滤
			if htmlSafeSet[b] || (!escapeHTML && safeSet[b]){
				i++
				//安全的不需要处理的数据之前跳过
				continue
			}
			//先将特殊符号之前符合的数据先写入缓冲区
			if start <i {
				e.Write(s[start:i])
			}
			//特殊字符加上转移字符
			e.WriteByte('\\')
			switch b {
			case '\\','"':
				e.WriteByte(b)
			case '\n'://遇到换行
				e.WriteByte('n')
			case '\r'://回车
				e.WriteByte('r')
			case '\t'://水平制表
				e.WriteByte('t')
			}
			i++
			start = i
			continue
		}
	}
	if start < len(s) {
		e.Write(s[start:])
	}
	e.WriteByte('"')
}

//用反射去序列化
func (e *encodeState) reflectValue(v reflect.Value, opts encOpts) {
	valueEncoder(v)(e, v, opts)
}

type encOpts struct {
	// quoted causes primitive fields to be encoded inside JSON strings.
	//是否将指定的字段转化为string类型
	quoted bool
	// escapeHTML causes '<', '>', and '&' to be escaped in JSON strings.
	//是否解析成json格式
	escapeHTML bool
}

//编码的方法
type encoderFunc func(e *encodeState, v reflect.Value, opts encOpts)

//根据反射去获取编码的方法
func valueEncoder(v reflect.Value) encoderFunc {
	if !v.IsValid(){
		return invalidValueEncoder
	}
	return typeEncoder(v.Type())
}

//根据反射reflect.Type去获得编码的方法
func typeEncoder(t reflect.Type) encoderFunc {

	// To deal with recursive types, populate the map with an
	// indirect func before we build it. This type waits on the
	// real func (f) to be ready and then calls it. This indirect
	// func is only used for recursive types.
	var (
		f encoderFunc
	)

	// Compute the real encoder and replace the indirect func with it.
	f = newTypeEncoder(t, true)

	return f
}

var (
	marshalerType     = reflect.TypeOf((*Marshaler)(nil)).Elem()
	//nil空实现了任意的接口
	textMarshalerType = reflect.TypeOf((*encoding.TextMarshaler)(nil)).Elem()
)

// newTypeEncoder constructs an encoderFunc for a type.
// The returned encoder only checks CanAddr when allowAddr is true.
//根据反射类型kind去获取编码方法
func newTypeEncoder(t reflect.Type, allowAddr bool) encoderFunc {
	// If we have a non-pointer value whose type implements
	// Marshaler with a value receiver, then we're better off taking
	// the address of the value - otherwise we end up with an
	// allocation as we cast the value to an interface.
	//不是指针类型,转化为指针再判断是否继承Marshaler
	//注意这里必须找到对应实现接口的那个类型
	//TODO Hank 这里不是特别明白,为什么要转化为指针类型去判断
	//然后条件编码的内部还要根据是否可以寻址去决定执行继承的相关接口
	if t.Kind() != reflect.Ptr && allowAddr && reflect.PtrTo(t).Implements(marshalerType){
		return newCondAddrEncoder(addrMarshalerEncoder,newTypeEncoder(t,false))
	}
	//指针类型实现了接口 继承Marshaler
	if t.Implements(marshalerType){
		return marshalerEncoder
	}
	//不是指针类型，转化为指针再判断是否继承TextMarshaler
	//TODO Hank 这里不是特别明白,为什么要转化为指针类型去判断
	//然后条件编码的内部还要根据是否可以寻址去决定执行继承的相关接口
	if t.Kind() != reflect.Ptr && allowAddr && reflect.PtrTo(t).Implements(textMarshalerType){
		return newCondAddrEncoder(addrTextMarshalerEncoder,newTypeEncoder(t,false))
	}
	//指针类型实现了接口 继承TextMarshaler
	if t.Implements(textMarshalerType){
		return textMarshalerEncoder
	}

	switch t.Kind() {
	case reflect.Bool:
		return boolEncoder
	case reflect.Int,reflect.Int8,reflect.Int16,reflect.Int32,reflect.Int64:
		return intEncoder
	case reflect.Uint,reflect.Uint8,reflect.Uint16,reflect.Uint32,reflect.Uint64,reflect.Uintptr:
		return uintEncoder
	case reflect.Float32:
		return float32Encoder
	case reflect.Float64:
		return float64Encoder
	case reflect.String:
		return stringEncoder
	case reflect.Interface:
		return interfaceEncoder
	case reflect.Struct:
		return newStructEncoder(t)
	case reflect.Map:
		return newMapEncoder(t)
	case reflect.Slice:
		return newSliceEncoder(t)
	case reflect.Array:
		return newArrayEncoder(t)
	case reflect.Ptr://如果是指针,一般会先进来这里
		return newPtrEncoder(t)
	default:
		return unsupportedTypeEncoder
	}
}

//无效的编码输出
func invalidValueEncoder(e *encodeState,v reflect.Value,_ encOpts){
	e.WriteString("null")
}

//指针实现了marshaler接口
func marshalerEncoder(e *encodeState, v reflect.Value, opts encOpts) {
	if v.Kind() == reflect.Ptr && v.IsNil(){
		e.WriteString("null")
	}
}

//非指针实现了marshaler接口
func addrMarshalerEncoder(e *encodeState,v reflect.Value,opts encOpts){
	va :=v.Addr()
	if va.IsNil(){
		e.WriteString("null")
		return
	}
	m :=va.Interface().(Marshaler)
	b,err :=m.MarshalJSON()
	if err == nil{
		// copy JSON into buffer, checking validity.
		println(b)
	}
	if err != nil{
		e.error(&MarshalerError{v.Type(),err,"MarshalJSON"})
	}
}

//指针实现了textMarshaler接口
func textMarshalerEncoder(e *encodeState,v reflect.Value,opts encOpts){

}

//非指针实现了textMarshaler接口
func addrTextMarshalerEncoder(e *encodeState,v reflect.Value,opts encOpts){
	va :=v.Addr()
	if va.IsNil(){
		e.WriteString("null")
		return
	}
	m := va.Interface().(encoding.TextMarshaler)
	b,err :=m.MarshalText()
	if err != nil{
		e.error(&MarshalerError{v.Type(), err, "MarshalText"})
	}
	e.stringBytes(b,opts.escapeHTML)
}

func boolEncoder(e *encodeState,v reflect.Value,opts encOpts){
	if opts.quoted{
		e.WriteByte('"')
	}
	if v.Bool(){
		e.WriteString("true")
	}else{
		e.WriteString("false")
	}
	if opts.quoted{
		e.WriteByte('"')
	}
}

func intEncoder(e *encodeState, v reflect.Value, opts encOpts) {
	//这里将反射获得int的值转化为字节类型的ASCII码
	//e.scratch为64字节数组,每次作为临时数据用字节的第一位去装载数值
	b := strconv.AppendInt(e.scratch[:0],v.Int(),10)
	if opts.quoted{
		e.WriteByte('"')
	}
	e.Write(b)
	if opts.quoted{
		e.WriteByte('"')
	}
}

func uintEncoder(e *encodeState,v reflect.Value,opts encOpts){
	b :=strconv.AppendUint(e.scratch[:0],v.Uint(),10)
	if opts.quoted{
		e.WriteByte('"')
	}
	e.Write(b)
	if opts.quoted{
		e.WriteByte('"')
	}
}

var (
	float32Encoder = (floatEncoder(32)).encode
	float64Encoder = (floatEncoder(64)).encode
)

type floatEncoder int // number of bits
func (bits floatEncoder)encode(e *encodeState,v reflect.Value,opts encOpts){

}

func stringEncoder(e *encodeState, v reflect.Value, opts encOpts) {
	//如果是json.Number类型
	if v.Type() == numberType{
		numStr :=v.String()
		// In Go1.5 the empty string encodes to "0", while this is not a valid number literal
		// we keep compatibility so check validity after this.
		if numStr ==""{
			numStr = "0"
		}
		if opts.quoted{
			e.WriteByte('"')
		}
		e.WriteString(numStr)
		if opts.quoted{
			e.WriteByte('"')
		}
		return
	}
	e.string(v.String(),opts.escapeHTML)
}

func interfaceEncoder(e *encodeState,v reflect.Value,opts encOpts){
	if v.IsNil(){
		e.WriteString("null")
	}
	valueEncoder(v)(e,v,opts)
}

//结构体编码器
type structEncoder struct{
	fields structFields
}

type structFields struct {
	list []field //所有的字段
	nameIndex map[string]int //字段名索引,方便查找
}

func newStructEncoder(t reflect.Type)encoderFunc{
	se :=structEncoder{fields: cachedTypeFields(t)}
	return se.encode
}

func (se structEncoder)encode(e *encodeState,v reflect.Value,opts encOpts){
	next := byte('{')
FieldLoop:
	for i := range se.fields.list{
		f :=&se.fields.list[i]

		// Find the nested struct field by following f.index.
		fv :=v
		for _,i := range f.index{
			//如果是指针类型,去获取其对应的元素
			if fv.Kind() == reflect.Ptr{
				if fv.IsNil(){
					continue FieldLoop
				}
				fv = fv.Elem()
			}
			fv = fv.Field(i)
		}

		//如果要忽略空值
		if f.omitEmpty && isEmptyValue(fv){
			continue
		}
		e.WriteByte(next)
		next = ','
		if opts.escapeHTML{
			e.WriteString(f.nameEscHTML)
		}else{
			e.WriteString(f.nameNonEsc)
		}
		opts.quoted = f.quoted
		//继续对每个字段进行编码
		f.encoder(e,fv,opts)
	}
	if next == '{' {
		e.WriteString("{}")
	}else{
		e.WriteByte('}')
	}
}

//map类型编码
type mapEncoder struct{
	elemEnc encoderFunc
}

func newMapEncoder(t reflect.Type)encoderFunc{
	me := mapEncoder{typeEncoder(t.Elem())}
	return me.encode
}

func (me mapEncoder)encode(e *encodeState,v reflect.Value,opts encOpts){
	if v.IsNil(){
		e.WriteString("null")
		return
	}
	e.WriteByte('{')

	// Extract and sort the keys.

}

//slice类型编码
type sliceEncoder struct{
	arrayEnc encoderFunc
}

func newSliceEncoder(r reflect.Type)encoderFunc{
	enc :=sliceEncoder{}
	return enc.encode
}

func (se sliceEncoder)encode(e *encodeState,v reflect.Value,opts encOpts){}

type arrayEncoder struct{
	elemEnc encoderFunc
}

func newArrayEncoder(t reflect.Type)encoderFunc{
	enc :=arrayEncoder{}
	return enc.encode
}

func (ae arrayEncoder)encode(e *encodeState,v reflect.Value,opts encOpts){}

//指针类型编码
type ptrEncoder struct{
	elemEnc encoderFunc
}

func newPtrEncoder(t reflect.Type)encoderFunc{
	enc := ptrEncoder{typeEncoder(t.Elem())}
	return enc.encode
}

func (pe ptrEncoder) encode(e *encodeState, v reflect.Value, opts encOpts){
	if v.IsNil(){
		e.WriteString("null")
		return
	}

	//指针类型进入递归解析
	pe.elemEnc(e,v.Elem(),opts)
}

//条件编码
//用在接口的实现Marshaler和TextMarshaler
type condAddrEncoder struct{
	canAddrEnc, elseEnc encoderFunc
}

// newCondAddrEncoder returns an encoder that checks whether its value
// CanAddr and delegates to canAddrEnc if so, else to elseEnc.
func newCondAddrEncoder(canAddrEnc, elseEnc encoderFunc) encoderFunc {
	enc :=condAddrEncoder{canAddrEnc: canAddrEnc,elseEnc:elseEnc}
	return enc.encode
}

func (ce condAddrEncoder) encode(e *encodeState, v reflect.Value, opts encOpts) {
	if v.CanAddr() {
		ce.canAddrEnc(e,v,opts)
	}else{
		ce.elseEnc(e,v,opts)
	}
}

//所有类型都不知道错误方法
func unsupportedTypeEncoder(e *encodeState,v reflect.Value, _ encOpts){
	e.error(&UnsupportedTypeError{})
}

// typeFields returns a list of fields that JSON should recognize for the given type.
// The algorithm is breadth-first search over the set of structs to include - the top struct
// and then any reachable anonymous structs.
//获取结构体的字段
func typeFields(t reflect.Type)structFields{
	// Anonymous fields to explore at the current level and the next.
	current :=[]field{}
	next :=[]field{{typ:t}}

	// Types already visited at an earlier level.
	//过滤器
	visited := map[reflect.Type]bool{}

	//所有的字段
	var fields []field

	// Buffer to run HTMLEscape on field names.
	//按格式将数据写入buf
	var nameEscBuf bytes.Buffer

	for len(next) > 0{
		current, next = next, current[:0]
		for _, f := range current {
			if visited[f.typ] {
				continue
			}
			visited[f.typ] = true

			// Scan f.typ for fields to include.
			for i := 0; i < f.typ.NumField(); i++ {
				sf := f.typ.Field(i)
				isUnexported := sf.PkgPath != "" //如果包的路径不为空，说明是私有属性，忽略
				if sf.Anonymous {
					t := sf.Type
					//指针类型需要进一步解析
					if t.Kind() == reflect.Ptr {
						t = t.Elem()
					}
					if isUnexported && t.Kind() != reflect.Struct {
						// Ignore embedded fields of unexported non-struct types.
						continue
					}
					// Do not ignore embedded fields of unexported struct types
					// since they may have exported fields.
				}else if isUnexported {
					// Ignore unexported non-embedded fields.
					continue
				}
				tag := sf.Tag.Get("json")
				if tag == "-" {//如果有-表示忽略该字段的解析
					continue
				}
				name, opts := parseTag(tag)
				if !isValidTag(name){
					name = ""
				}
				//设置索引
				index :=make([]int,len(f.index)+1)
				copy(index,f.index)
				index[len(f.index)] = i

				ft :=sf.Type

				// Only strings, floats, integers, and booleans can be quoted.
				quoted := false
				//string将string,floats,integers and booleans设置成string
				if opts.Contains("string"){
					switch ft.Kind() {
					case reflect.Bool,
						reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
						reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
						reflect.Float32, reflect.Float64,
						reflect.String:
							quoted = true
					}
				}

				// Record found field and index sequence.
				if name != "" || !sf.Anonymous || ft.Kind() != reflect.Struct {
					tagged := name != ""
					if name == ""{
						name = sf.Name
					}
					field := field{
						name:name,
						tag:tagged,
						index:index,
						typ:ft,
						omitEmpty:opts.Contains("omitempty"),
						quoted:quoted,
					}
					field.nameBytes =[]byte(field.name)

					// Build nameEscHTML and nameNonEsc ahead of time.
					//重置下缓冲区
					nameEscBuf.Reset()
					nameEscBuf.WriteString(`"`)
					HTMLEscape(&nameEscBuf,field.nameBytes)
					nameEscBuf.WriteString(`":`)
					field.nameEscHTML =nameEscBuf.String()
					field.nameNonEsc = `"` + field.name + `":`

					fields = append(fields,field)

					continue
				}
			}
		}
	}
	for i :=range fields{
		f := &fields[i]
		//获取对应子字段得编码方法
		f.encoder = typeEncoder(typeByIndex(t,f.index))
	}
	nameIndex := make(map[string]int,len(fields))
	for i,field :=range fields{
		nameIndex[field.name] = i
	}
	return structFields{fields,nameIndex}
}

//检查标签是否有效
func isValidTag(s string) bool {
	if s == ""{
		return false
	}
	return true
}

//根据索引去获取结构体字段的类型
func typeByIndex(t reflect.Type,index []int)reflect.Type{
	for _,i := range index{
		if t.Kind() == reflect.Ptr{
			t  =t.Elem()
		}
		t = t.Field(i).Type
	}
	return t
}

// A field represents a single field found in a struct.
//用于结构体的字段
type field struct{
	name string
	nameBytes []byte                 // []byte(name)

	//两种json的数据格式
	nameNonEsc  string // `"` + name + `":`
	nameEscHTML string // `"` + HTMLEscape(name) + `":`

	tag bool //是否设置标签
	index []int //索引
	typ reflect.Type
	omitEmpty bool //是否要过滤空值字段
	quoted bool //是否要转化为string类型

	encoder encoderFunc //字段继续编码
}


	// cachedTypeFields is like typeFields but uses a cache to avoid repeated work.
//解析struct结构体的字段并缓存
func cachedTypeFields(t reflect.Type)structFields{
	return typeFields(t)
}