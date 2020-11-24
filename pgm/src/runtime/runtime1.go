// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package runtime

func args(c int32,v **byte){
	println(c)
	println(v)
}

//asm_amd64.s_rt0_go
func check(){

}