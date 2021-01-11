// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.
package strconv

const fastSmalls = true // enable fast path for small integers

// AppendInt appends the string form of the integer i,
// as generated by FormatInt, to dst and returns the extended buffer.
func AppendInt(dst []byte,i int64,base int)[]byte{
	if fastSmalls && 0 <= i && i < nSmalls && base == 10{
		return append(dst, small(int(i))...)
	}
	//超过100的数
	return dst
}

// AppendUint appends the string form of the unsigned integer i,
// as generated by FormatUint, to dst and returns the extended buffer.
func AppendUint(dst []byte,i uint64,base int)[]byte{
	if fastSmalls && i < nSmalls && base == 10{
		return append(dst, small(int(i))...)
	}
	//超过100的数
	return dst
}

//小于100的数值转化为string ASCII码
func small(i int)string{
	if i < 10{
		return digits
	}
	return smallsString[i*2 : i*2+2]
}

const nSmalls = 100

const digits = "0123456789abcdefghijklmnopqrstuvwxyz"

const smallsString = "00010203040506070809" +
	"10111213141516171819" +
	"20212223242526272829" +
	"30313233343536373839" +
	"40414243444546474849" +
	"50515253545556575859" +
	"60616263646566676869" +
	"70717273747576777879" +
	"80818283848586878889" +
	"90919293949596979899"

// formatBits computes the string representation of u in the given base.
// If neg is set, u is treated as negative int64 value. If append_ is
// set, the string is appended to dst and the resulting byte slice is
// returned as the first result value; otherwise the string is returned
// as the second result value.
//
func formatBits(dst []byte, u uint64, base int, neg, append_ bool) (d []byte, s string) {
	if base < 2 || base > len(digits){
		panic("strconv: illegal AppendInt/FormatInt base")
	}
	// 2 <= base && base <= len(digits)

}