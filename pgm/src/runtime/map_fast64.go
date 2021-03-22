package runtime

import "unsafe"

//如果哈希的key的hash是int64或uint64类型,那么对查找方法做了优化
func mapaccess2_fast64(t *maptype, h *hmap, key uint64) (unsafe.Pointer, bool) {
	if h == nil || h.count == 0 {
		return unsafe.Pointer(&zeroVal[0]), false
	}
	if h.flags&hashWriting != 0 {

	}
	var b *bmap
	if h.B == 0{
		// One-bucket table. No need to hash.
		b = (*bmap)(h.buckets)
	}else{
		hash :=t.hasher(noescape(unsafe.Pointer(&key)),uintptr(h.hash0))
		m :=bucketMask(h.B)
		b = (*bmap)(add(h.buckets,(hash&m)*uintptr(t.bucketsize)))
		if c := h.oldbuckets;c != nil{
			if !h.sameSizeGrow(){
				// There used to be half as many buckets; mask down one more power of two.
				m >>= 1
			}
			oldb := (*bmap)(add(c, (hash&m)*uintptr(t.bucketsize)))
			if !evacuated(oldb){
				b = oldb
			}
		}
	}

	//1.优化在于因为是确定类型,使用tophash效率会相对较低,所以省去，而是直接对key的比较
	//2.确定类型不需要指针类型的判断和转化
	for ; b != nil; b = b.overflow(t) {
		for i,k := uintptr(0),b.keys();i <bucketCnt;i,k = i+1,add(k,8){
			if *(*uint64)(k) == key && !isEmpty(b.tophash[i]){
				return add(unsafe.Pointer(b),dataOffset+bucketCnt*8+i*uintptr(t.elemsize)),true
			}
		}
	}
	return unsafe.Pointer(&zeroVal[0]),false
}
