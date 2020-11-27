package runtime

import "unsafe"

//原子操作方法,保证写入不被破坏
// atomicstorep performs *ptr = new atomically and invokes a write barrier.
//
//go:nosplit
func atomicstorep(ptr unsafe.Pointer,new unsafe.Pointer){

}
