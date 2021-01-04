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
	"reflect"
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

	err := e.marshal(v, encOpts{})
	if err != nil {
		return nil, err
	}
	return nil, nil
}

// An encodeState encodes JSON into a bytes.Buffer.
type encodeState struct {
	bytes.Buffer // accumulated output
}

func newEncodeState() *encodeState {
	return &encodeState{}
}

func (e *encodeState) marshal(v interface{}, opts encOpts) (err error) {
	e.reflectValue(reflect.ValueOf(v), opts)
	return nil
}

//用反射去序列化
func (e *encodeState) reflectValue(v reflect.Value, opts encOpts) {
	valueEncoder(v)(e, v, opts)
}

type encOpts struct {
}

//编码的方法
type encoderFunc func(e *encodeState, v reflect.Value, opts encOpts)

//根据反射去获取编码的方法
func valueEncoder(v reflect.Value) encoderFunc {
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

// newTypeEncoder constructs an encoderFunc for a type.
// The returned encoder only checks CanAddr when allowAddr is true.
//根据反射类型kind去获取编码方法
func newTypeEncoder(t reflect.Type, allowAddr bool) encoderFunc {
	switch t.Kind() {
	case reflect.String:
		return stringEncoder
	case reflect.Struct:
		return newStructEncoder(t)
	case reflect.Ptr://如果是指针,一般会先进来这里
		return newPtrEncoder(t)
	}
	return nil
}

func stringEncoder(e *encodeState, v reflect.Value, opts encOpts) {
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

}

//指针类型
type ptrEncoder struct{
	elemEnc encoderFunc
}

func newPtrEncoder(t reflect.Type)encoderFunc{
	enc := ptrEncoder{typeEncoder(t.Elem())}
	return enc.encode
}

func (pe ptrEncoder) encode(e *encodeState, v reflect.Value, opts encOpts){
	//指针类型进入递归解析
	pe.elemEnc(e,v.Elem(),opts)
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
					}
					field.nameBytes =[]byte(field.name)

					fields = append(fields,field)
				}
			}
		}
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

// A field represents a single field found in a struct.
//用于结构体的字段
type field struct{
	name string
	nameBytes []byte                 // []byte(name)

	tag bool //是否设置标签
	index []int //索引
	typ reflect.Type
	omitEmpty bool //是否要过滤空值字段

}


	// cachedTypeFields is like typeFields but uses a cache to avoid repeated work.
//解析struct结构体的字段并缓存
func cachedTypeFields(t reflect.Type)structFields{
	return typeFields(t)
}