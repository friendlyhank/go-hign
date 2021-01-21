// Copyright 2010 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package json

// JSON value parser state machine.
// Just about at the limit of what is reasonable to write by hand.
// Some parts are a bit tedious, but overall it nicely factors out the
// otherwise common code from the multiple scanning functions
// in this package (Compact, Indent, checkValid, etc).
//
// This file starts with two simple examples using the scanner
// before diving into the scanner itself.

// checkValid verifies that data is valid JSON-encoded data.
// scan is passed in for use by checkValid to avoid an allocation.
//检查数据是否有效
func checkValid(data []byte, scan *scanner) error {
	scan.reset()
	for _,c := range data{
		if scan.step(scan,c) == scanError{
			return scan.err
		}
	}
	return nil
}

// A SyntaxError is a description of a JSON syntax error.
type SyntaxError struct{
	msg string // description of error
	Offset int64 // error occurred after reading Offset bytes
}

func (e *SyntaxError)Error()string{return e.msg}

// A scanner is a JSON scanning state machine.
// Callers call scan.reset and then pass bytes in one at a time
// by calling scan.step(&scan, c) for each byte.
// The return value, referred to as an opcode, tells the
// caller about significant parsing events like beginning
// and ending literals, objects, and arrays, so that the
// caller can follow along if it wishes.
// The return value scanEnd indicates that a single top-level
// JSON value has been completed, *before* the byte that
// just got passed in.  (The indication must be delayed in order
// to recognize the end of numbers: is 123 a whole value or
// the beginning of 12345e+6?).
type scanner struct{
	// The step is a func to be called to execute the next transition.
	// Also tried using an integer constant and a single func
	// with a switch, but using the func directly was 10% faster
	// on a 64-bit Mac Mini, and it's nicer to read.
	step func(*scanner,byte) int

	// Stack of what we're in the middle of - array values, object keys, object values.
	//解析的状态
	parseState []int

	// Error that happened, if any.
	err error
}

const (
	scanContinue = iota //开始扫描的状态 uninteresting byte
	scanBeginLiteral        // end implied by next result != scanContinue
	scanBeginObject //说明要解析的是个对象 begin object
	scanObjectKey  //解析对象的key just finished object key (string)
	scanObjectValue //解析对象的value just finished non-last object value
	scanEndObject   // end object (implies scanObjectValue if possible)
	scanSkipSpace //扫描最后结束 space byte; can skip; known to be last "continue" result
	scanBeginArray          //解析数组 begin array
	scanArrayValue          //解析数组的值 just finished array value
	scanEndArray            //最后数组解析完成 end array (implies scanArrayValue if possible)

	// Stop.
	scanEnd   // top-level value ended *before* this byte; known to be first "stop" result
	scanError //解析发生错误 hit an error, scanner.err.
)

// These values are stored in the parseState stack.
// They give the current state of a composite value
// being scanned. If the parser is inside a nested value
// the parseState describes the nested state, outermost at entry 0.
const(
	parseObjectKey   = iota //说明要解析对象的key  parsing object key (before colon)
	parseObjectValue        //解析对象的value parsing object value (after colon)
	parseArrayValue         //解析数组的value parsing array value
)

// This limits the max nesting depth to prevent stack overflow.
// This is permitted by https://tools.ietf.org/html/rfc7159#section-9
//最大解析深度，不然会栈溢出
const maxNestingDepth = 10000

// reset prepares the scanner for use.
// It must be called before calling s.step.
func (s *scanner)reset(){
	s.step = stateBeginValue
	s.parseState = s.parseState[0:0]
	s.err  = nil
}

// pushParseState pushes a new parse state p onto the parse stack.
// an error state is returned if maxNestingDepth was exceeded, otherwise successState is returned.
func (s *scanner) pushParseState(c byte, newParseState int, successState int) int {
	s.parseState = append(s.parseState,newParseState)
	if len(s.parseState) <= maxNestingDepth{
		return newParseState
	}
	return s.error(c, "exceeded max depth")
}

func isSpace(c byte) bool {
	return c <= ' ' && (c == ' ' || c == '\t' || c == '\r' || c == '\n')
}

// stateBeginValueOrEmpty is the state after reading `[`.
func stateBeginValueOrEmpty(s *scanner, c byte) int {
	return 0
}

// stateBeginValue is the state at the beginning of the input.
//一开始输入的处理
func stateBeginValue(s *scanner,c byte)int{
	if isSpace(c){
		return scanSkipSpace
	}
	switch c {
	case '{':
		s.step =stateBeginStringOrEmpty
		//说明要解析的是对象,下一步要解析的是key值
		return s.pushParseState(c,parseObjectKey,scanBeginObject)
	case '[':
		s.step = stateBeginValueOrEmpty
		//说明解析的是数组，下一步要解析的是数组的value
		return s.pushParseState(c,parseArrayValue,scanBeginArray)
	case '"':
		s.step = stateInString
		//说明要解析的是字段key或value
		return scanBeginLiteral
	}
	return s.error(c,"looking for beginning of value")
}

// stateBeginStringOrEmpty is the state after reading `{`.
//解析的步骤在`{`用到
func stateBeginStringOrEmpty(s *scanner,c byte)int{
	if isSpace(c){
		return scanSkipSpace
	}
	return stateBeginString(s,c)
}

// stateBeginString is the state after reading `{"key": value,`.
//解析的步骤在`{"key": value,`之后用到
func stateBeginString(s *scanner, c byte) int {
	if c == '"'{
		s.step =stateInString
		return scanBeginLiteral
	}
	return s.error(c, "looking for beginning of object key string")
}

// stateEndValue is the state after completing a value,
// such as after reading `{}` or `true` or `["x"`.
//遇到`:`或`,`的时候转换成去寻找key或value
func stateEndValue(s *scanner, c byte) int {
	n :=len(s.parseState)
	if n  == 0{

	}
	ps :=s.parseState[n-1]
	switch ps {
	case parseObjectKey://当前解析的是key,下一步解析的是value
		if c == ':'{
			//解析状态改为解析值
			s.parseState[n-1] = parseObjectValue
			s.step = stateBeginValue //步骤重新回到第一步
			return scanObjectKey
		}
		return s.error(c, "after object key")
	case parseObjectValue://当前解析的是value,下一步解析的是key
		if c == ','{
			s.parseState[n-1] =parseObjectKey
			s.step = stateBeginString
			return scanObjectValue
		}
	}
	return s.error(c,"")
}

// stateInString is the state after reading `"`.
//遇到'"'符号时候解析去解析字段或值
func stateInString(s *scanner,c byte)int{
	if c == '"' {
		s.step = stateEndValue
		return scanContinue
	}
	return scanContinue
}

func stateError(s *scanner,c byte)int{
	return scanError
}

// error records an error and switches to the error state.
//scan错误
func (s *scanner)error(c byte,context string) int{
	s.step = stateError
	s.err = &SyntaxError{}
	return scanError
}
