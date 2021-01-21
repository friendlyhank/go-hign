// Copyright 2010 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Represents JSON data structure using native Go types: booleans, floats,
// strings, arrays, and maps.
package json

import (
	"encoding"
	"reflect"
	"unicode/utf8"
)

// Unmarshal parses the JSON-encoded data and stores the result
// in the value pointed to by v. If v is nil or not a pointer,
// Unmarshal returns an InvalidUnmarshalError.
//
// Unmarshal uses the inverse of the encodings that
// Marshal uses, allocating maps, slices, and pointers as necessary,
// with the following additional rules:
//
// To unmarshal JSON into a pointer, Unmarshal first handles the case of
// the JSON being the JSON literal null. In that case, Unmarshal sets
// the pointer to nil. Otherwise, Unmarshal unmarshals the JSON into
// the value pointed at by the pointer. If the pointer is nil, Unmarshal
// allocates a new value for it to point to.
//
// To unmarshal JSON into a value implementing the Unmarshaler interface,
// Unmarshal calls that value's UnmarshalJSON method, including
// when the input is a JSON null.
// Otherwise, if the value implements encoding.TextUnmarshaler
// and the input is a JSON quoted string, Unmarshal calls that value's
// UnmarshalText method with the unquoted form of the string.
//
// To unmarshal JSON into a struct, Unmarshal matches incoming object
// keys to the keys used by Marshal (either the struct field name or its tag),
// preferring an exact match but also accepting a case-insensitive match. By
// default, object keys which don't have a corresponding struct field are
// ignored (see Decoder.DisallowUnknownFields for an alternative).
//
// To unmarshal JSON into an interface value,
// Unmarshal stores one of these in the interface value:
//
//	bool, for JSON booleans
//	float64, for JSON numbers
//	string, for JSON strings
//	[]interface{}, for JSON arrays
//	map[string]interface{}, for JSON objects
//	nil for JSON null
//
// To unmarshal a JSON array into a slice, Unmarshal resets the slice length
// to zero and then appends each element to the slice.
// As a special case, to unmarshal an empty JSON array into a slice,
// Unmarshal replaces the slice with a new empty slice.
//
// To unmarshal a JSON array into a Go array, Unmarshal decodes
// JSON array elements into corresponding Go array elements.
// If the Go array is smaller than the JSON array,
// the additional JSON array elements are discarded.
// If the JSON array is smaller than the Go array,
// the additional Go array elements are set to zero values.
//
// To unmarshal a JSON object into a map, Unmarshal first establishes a map to
// use. If the map is nil, Unmarshal allocates a new map. Otherwise Unmarshal
// reuses the existing map, keeping existing entries. Unmarshal then stores
// key-value pairs from the JSON object into the map. The map's key type must
// either be any string type, an integer, implement json.Unmarshaler, or
// implement encoding.TextUnmarshaler.
//
// If a JSON value is not appropriate for a given target type,
// or if a JSON number overflows the target type, Unmarshal
// skips that field and completes the unmarshaling as best it can.
// If no more serious errors are encountered, Unmarshal returns
// an UnmarshalTypeError describing the earliest such error. In any
// case, it's not guaranteed that all the remaining fields following
// the problematic one will be unmarshaled into the target object.
//
// The JSON null value unmarshals into an interface, map, pointer, or slice
// by setting that Go value to nil. Because null is often used in JSON to mean
// ``not present,'' unmarshaling a JSON null into any other Go type has no effect
// on the value and produces no error.
//
// When unmarshaling quoted strings, invalid UTF-8 or
// invalid UTF-16 surrogate pairs are not treated as an error.
// Instead, they are replaced by the Unicode replacement
// character U+FFFD.
//
func Unmarshal(data []byte, v interface{}) error {
	// Check for well-formedness.
	// Avoids filling out half a data structure
	// before discovering a JSON syntax error.
	var d decodeState
	err :=checkValid(data,&d.scan)
	if err != nil{
		return err
	}

	d.init(data)
	return d.unmarshal(v)
}

// Unmarshaler is the interface implemented by types
// that can unmarshal a JSON description of themselves.
// The input can be assumed to be a valid encoding of
// a JSON value. UnmarshalJSON must copy the JSON data
// if it wishes to retain the data after returning.
//
// By convention, to approximate the behavior of Unmarshal itself,
// Unmarshalers implement UnmarshalJSON([]byte("null")) as a no-op.
type Unmarshaler interface {
	UnmarshalJSON([]byte) error
}

// An InvalidUnmarshalError describes an invalid argument passed to Unmarshal.
// (The argument to Unmarshal must be a non-nil pointer.)
type InvalidUnmarshalError struct {
	Type reflect.Type
}

func (e *InvalidUnmarshalError) Error() string {
	if e.Type == nil{
		return "json: Unmarshal(nil)"
	}

	if e.Type.Kind() != reflect.Ptr{
		return "json: Unmarshal(non-pointer " + ")"
	}
	return "json: Unmarshal(nil " + ")"
}

func (d *decodeState)unmarshal(v interface{}) error{
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Ptr || rv.IsNil(){
		return &InvalidUnmarshalError{reflect.TypeOf(v)}
	}

	d.scan.reset()
	//获取下一步解析的步骤,并判断是否结束
	d.scanWhile(scanSkipSpace)
	// We decode rv not rv.Elem because the Unmarshaler interface
	// test must be applied at the top level of the value.
	err :=d.value(rv)
	if err != nil{
	}
	return d.savedError
}

// A Number represents a JSON number literal.
//json内部定义的Number类型,用于编解码中未知是string或是int类型
//用于interface类型编解码过程将数值解析成float64类型，如果数值较大会精度丢失的问题
//filename json.Number `json:"filename"`
type Number string

// String returns the literal text of the number.
func(n Number)String()string{return string(n)}

// decodeState represents the state while decoding a JSON value.
type decodeState struct{
	data []byte  //数据
	off int //读取数据的偏移量 next read offset in data
	opcode       int //最近一次读取的状态码 last read result
	scan         scanner
	savedError error //保存错误
	useNumber bool //是否使用了json.number
}

// readIndex returns the position of the last byte read.
//获取最近一次读取的索引
func (d *decodeState)readIndex()int{
	return d.off - 1
}

// phasePanicMsg is used as a panic message when we end up with something that
// shouldn't happen. It can indicate a bug in the JSON decoder, or that
// something is editing the data slice while the decoder executes.
//解析错误
const phasePanicMsg = "JSON decoder out of sync - data changing underfoot?"

func (d *decodeState)init(data []byte)*decodeState{
	d.data = data
	d.off = 0
	d.savedError = nil
	return d
}

// scanWhile processes bytes in d.data[d.off:] until it
// receives a scan code not equal to op.
//遍历所有的数据,直到结束
func (d *decodeState) scanWhile(op int) {
	s,data,i := &d.scan,d.data,d.off
	for i < len(data){
		newOp := s.step(s,data[i])
		i++
		if newOp != op{
			d.opcode = newOp
			d.off = i //更新偏移量
			return
		}
	}
}

// rescanLiteral is similar to scanWhile(scanContinue), but it specialises the
// common case where we're decoding a literal. The decoder scans the input
// twice, once for syntax errors and to check the length of the value, and the
// second to perform the decoding.
//
// Only in the second step do we use decodeState to tokenize literals, so we
// know there aren't any syntax errors. We can take advantage of that knowledge,
// and scan a literal's bytes much more quickly.
//分析出单个key或value,更新偏移量和下一步的不走
func (d *decodeState)rescanLiteral(){
	data,i :=d.data,d.off
Switch:
	switch data[i-1] {
	case '"': // string
	for ;i < len(data);i++{
		switch data[i] {
		case '"':
			i++// tokenize the closing quote too
			break Switch
		}
		}
	}
	if i < len(data){
		d.opcode =stateEndValue(&d.scan,data[i])
	}else{
		d.opcode =scanEnd
	}
	d.off = i + 1
}

//递归 解析对象的值
func (d *decodeState)value(v reflect.Value)error{
	switch d.opcode{
	default:
		panic(phasePanicMsg)//解析数组
	case scanBeginArray:
		if v.IsValid(){
			if err :=d.array(v);err != nil{
				return err
			}
		}
	case scanBeginObject://解析对象
		if v.IsValid(){
			if err := d.object(v);err != nil{
				return err
			}
		}
	case scanBeginLiteral://解析字段
		// All bytes inside literal return scanContinue op code.
		start :=d.readIndex()
		d.rescanLiteral()
		if v.IsValid(){
			//将值设置到对应类型的反射value中
			if err :=d.literalStore(d.data[start:d.readIndex()],v,false);err != nil{
				return nil
			}
		}
	}
	return nil
}

// indirect walks down v allocating pointers as needed,
// until it gets to a non-pointer.
// If it encounters an Unmarshaler, indirect stops and returns that.
// If decodingNull is true, indirect stops at the first settable pointer so it
// can be set to nil.
func indirect(v reflect.Value,decodingNull bool)(Unmarshaler,encoding.TextUnmarshaler,reflect.Value){
	v = v.Elem()
	return nil,nil,v
}

// array consumes an array from d.data[d.off-1:], decoding into v.
// The first byte of the array ('[') has been read already.
//解析数组
func (d *decodeState)array(v reflect.Value) error {
	// Check for unmarshaler.
	u,ut,pv :=indirect(v,false)
	if u != nil{
	}
	if ut != nil{

	}
	v = pv
	return nil
}

// object consumes an object from d.data[d.off-1:], decoding into v.
// The first byte ('{') of the object has been read already.
//对象的解码
func (d *decodeState)object(v reflect.Value)error{
	// Check for unmarshaler.
	//三种格式Unmarshaler,TextUnmarshaler,第三种返回的是元素
	u,ut,pv :=indirect(v,false)
	//Unmarshaler
	if u != nil{

	}
	//encoding.TextUnmarshaler
	if ut != nil{

	}

	v = pv
	t :=v.Type()

	//结构体的解析
	var fields structFields

	// Check type of target:
	//   struct or
	//   map[T1]T2 where T1 is string, an integer type,
	//             or an encoding.TextUnmarshaler
	switch v.Kind() {
	case reflect.Map:
	case reflect.Struct:
		//获取结构体的所有字段
		fields = cachedTypeFields(t)
	}

	for{
		// Read opening " of string key or closing }.
		d.scanWhile(scanSkipSpace)
		//遍历对象结束
		if d.opcode == scanEndObject{
			// closing } - can only happen on first iteration.
			break
		}
		if d.opcode != scanBeginLiteral{
			panic(phasePanicMsg)
		}

		//Read key 获取key值
		start := d.readIndex()
		//找到key的偏移量并更新步骤方法
		d.rescanLiteral()
		item := d.data[start:d.readIndex()]
		key,ok :=unquoteBytes(item)
		if !ok{
			panic(phasePanicMsg)
		}

		// Figure out field corresponding to key.
		var subv reflect.Value
		destring := false //是否要转化为string类型 whether the value is wrapped in a string to be decoded first

		if v.Kind() == reflect.Map{

		}else{
			var f *field
			if i,ok :=fields.nameIndex[string(key)];ok{
				// Found an exact name match.
				f = &fields.list[i]
			}else{
			}
			if f != nil{
				subv = v
				destring = f.quoted
				for _,i :=range f.index{
					//如果结构体字段是指针,找到它的元素
					if subv.Kind() == reflect.Ptr{
						if subv.IsNil(){

						}
						subv = subv.Elem()
					}
					subv = subv.Field(i)
				}
			}
		}

		// Read : before value.
		if d.opcode != scanObjectKey{
			panic(phasePanicMsg)
		}
		//继续下一步
		d.scanWhile(scanSkipSpace)

		//转化为string
		if destring {

		}else{
			if err :=d.value(subv);err != nil{
				return err
			}
		}

	}
	return nil
}

var numberType  = reflect.TypeOf("")

// literalStore decodes a literal stored in item into v.
//
// fromQuoted indicates whether this literal came from unwrapping a
// string from the ",string" struct tag option. this is used only to
// produce more helpful error messages.
//将值写进对应的类型里
func (d *decodeState)literalStore(item []byte, v reflect.Value, fromQuoted bool) error {
	// Check for unmarshaler.
	if len(item) == 0{
		//Empty string given
		return nil
	}
	isNull :=item[0] == 'n'
	u,ut,pv :=indirect(v,isNull)
	if u != nil{

	}
	if ut != nil{

	}

	v = pv

	switch c := item[0]; c {
	case 'n'://null
	case 't','f'://true, false
	case '"'://string
		s,ok :=unquoteBytes(item)
		if !ok{
			panic(phasePanicMsg)
		}
		switch v.Kind() {
		default:
		case reflect.String:
			v.SetString(string(s))
		}
	}
	return nil
}

//去除特殊符号,等到想要的值或字段
func unquoteBytes(s []byte) (t []byte, ok bool) {
	//首位如果不是""则直接返回
	if len(s)<2 || s[0] != '"' || s[len(s)-1] != '"'{
		return
	}
	//去除首尾
	s = s[1 : len(s)-1]
	// Check for unusual characters. If there are none,
	// then no unquoting is needed, so return a slice of the
	// original bytes.
	r := 0
	for r < len(s) {
		c := s[r]
		if c == '\\' || c == '"' || c < ' ' {
			break
		}
		if c < utf8.RuneSelf {
			r++
			continue
		}
	}
	if r == len(s){
		return s,true
	}
	return nil,false
}

