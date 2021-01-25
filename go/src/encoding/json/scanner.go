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
	if isSpace(c){
		return scanSkipSpace
	}
	return stateBeginValue(s,c)
}

// stateBeginValue is the state at the beginning of the input.
//状态从起始符号开始的解析,其实符号可以为`{`、`[`、`"`、0等
//最后得到得结果分析只有三个object、array、单元素(key或value)
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
	case '0'://如果开始的字符是0,说明是小数点 beginning of 0.123
		s.step = state0
		return scanBeginLiteral
	case 't'://如果开始的字符是t(true)开头 beginning of true
		s.step = stateT
		return  scanBeginLiteral
	case 'f'://如果开始的字符是f(false)开头  beginning of false
		s.step = stateF
		return scanBeginLiteral
	case 'n'://如果开始的字符是n(null)开头 beginning of null
		return scanBeginLiteral
	}
	if '1' <= c && c <= '9'{// beginning of 1234.5
		s.step = state1
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

// state1 is the state after reading a non-zero integer during a number,
// such as after reading `1` or `100` but not `0`.
//读取到`1`、`100`之后，说明是小数点或数字
func state1(s *scanner, c byte) int {
	if '0' <= c && c <= '9'{
		s.step = state1
		return scanContinue
	}
	return state0(s,c)
}

// state0 is the state after reading `0` during a number.
//遇到`0`开头的时候的的情况
func state0(s *scanner,c byte) int{
	if c == '.'{
		s.step = stateDot
	}
	return stateEndValue(s,c)
}

// stateDot is the state after reading the integer and decimal point in a number,
// such as after reading `1.`.
//遇到`1.`开头的情况,说明可能是小数点
func stateDot(s *scanner,c byte) int{
	if '0' <= c && c <= '9'{
		s.step = stateDot0
		return scanContinue
	}
	return s.error(c, "after decimal point in numeric literal")
}

// stateDot0 is the state after reading the integer, decimal point, and subsequent
// digits of a number, such as after reading `3.14`.
//读取`3.14`之后,说明是小数点,后面继续拼接的是数字
func stateDot0(s *scanner,c byte)int{
	if '0' <= c && c <= '9'{
		return scanContinue
	}
	return stateEndValue(s,c)
}

// stateT is the state after reading `t`.
//读取`t`字符后,应该紧跟的是`r`(true)
func stateT(s *scanner,c byte)int{
	if c == 'r'{
		s.step = stateTr
		return scanContinue
	}
	return s.error(c, "in literal true (expecting 'r')")
}

// stateTr is the state after reading `tr`.
//读取`tr`字符后,应该紧跟的是`u`(true)
func stateTr(s *scanner,c byte)int{
	if c == 'u'{
		s.step = stateTru
		return scanContinue
	}
	return s.error(c, "in literal true (expecting 'u')")
}

// stateTru is the state after reading `tru`.
//读取`tru`字符后,应该紧跟的是`e`(true)
func stateTru(s *scanner, c byte) int {
	if c == 'e'{
		s.step = stateEndValue
		return scanContinue
	}
	return s.error(c, "in literal true (expecting 'e')")
}

// stateF is the state after reading `f`.
//读取`f`字符后,应该紧跟得是`a`(false)
func stateF(s *scanner,c byte)int{
	if c == 'a'{
		s.step = stateFa
		return scanContinue
	}
	return s.error(c, "in literal false (expecting 'a')")
}

// stateFa is the state after reading `fa`.
//读取`fa`字符后,应该紧跟的是`l`(false)
func stateFa(s *scanner, c byte) int {
 	if c == 'l'{
 		s.step = stateFal
		return scanContinue
	}
	return s.error(c, "in literal false (expecting 'l')")
}

// stateFal is the state after reading `fal`.
//读取`fal`字符后,应该紧跟的是`s`(false)
func stateFal(s *scanner, c byte) int {
	if c == 's'{
		s.step = stateFals
		return scanContinue
	}
	return s.error(c, "in literal false (expecting 's')")
}

// stateFals is the state after reading `fals`.
func stateFals(s *scanner, c byte) int {
	if c == 'e' {
		s.step = stateEndValue
		return scanContinue
	}
	return s.error(c, "in literal false (expecting 'e')")
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
