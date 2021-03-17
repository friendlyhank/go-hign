// Copyright 2014 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package runtime

import (
	"runtime/internal/math"
	"runtime/internal/sys"
	"unsafe"
)

const (
	// Maximum number of key/elem pairs a bucket can hold.
	//定义桶能存储最大的值
	bucketCntBits = 3
	bucketCnt     = 1 << bucketCntBits

	// Maximum average load of a bucket that triggers growth is 6.5.
	// Represent as loadFactorNum/loadFactorDen, to allow integer math.
	loadFactorNum = 13
	loadFactorDen = 2

	// data offset should be the size of the bmap struct, but needs to be
	// aligned correctly. For amd64p32 this means 64-bit alignment
	// even though pointers are 32 bit.
	//bmap结构体所占用的大小
	dataOffset = unsafe.Offsetof(struct {
		b bmap
		v int64
	}{}.v)

	// Possible tophash values. We reserve a few possibilities for special marks.
	// Each bucket (including its overflow buckets, if any) will have either all or none of its
	// entries in the evacuated* states (except during the evacuate() method, which only happens
	// during map writes and thus no one else can observe the map during that time).
	//tophash的状态信息
	//O和1的状态到底有啥区别
	emptyRest      = 0 // this cell is empty, and there are no more non-empty cells at higher indexes or overflows.
	emptyOne       = 1 // this cell is empty
	evacuatedX     = 2 // key/elem is valid.  Entry has been evacuated to first half of larger table.
	evacuatedY     = 3 // same as above, but evacuated to second half of larger table.
	evacuatedEmpty = 4 // cell is empty, bucket is evacuated.
	minTopHash     = 5 // minimum tophash for a normal filled cell.

	// hmap flags标记
	iterator     = 1 //buckets正在被使用 there may be an iterator using buckets
	oldIterator = 2 //oldbuckets正在被使用 there may be an iterator using oldbuckets
	hashWriting = 4 //map正在被写入 a goroutine is writing to the map
	sameSizeGrow = 8 //map在等量扩容 the current map growth is to a new map of the same size
)

// isEmpty reports whether the given tophash array entry represents an empty bucket entry.
//判断tophash是否为空
func isEmpty(x uint8)bool{
	return x <= emptyOne
}

type hmap struct{
	// Note: the format of the hmap is also encoded in cmd/compile/internal/gc/reflect.go.
	// Make sure this stays in sync with the compiler's definition.
	count int // # live cells == size of map.  Must be first (used by len() builtin)
	flags uint8 //标记是否被使用 是否正在写入 是否正在扩容
	B uint8 //桶的数量2^B log_2 of # of buckets (can hold up to loadFactor * 2^B items)
	noverflow uint16 // approximate number of overflow buckets; see incrnoverflow for details
	hash0 uint32 //哈希因子 hash seed
	buckets    unsafe.Pointer //2^B的桶 array of 2^B Buckets. may be nil if count==0.
	oldbuckets unsafe.Pointer //扩容时用于保存之前的buckets
	nevacuate  uintptr

	extra *mapextra // 额外的map字段
}

// mapextra holds fields that are not present on all maps.
type mapextra struct {
	// If both key and elem do not contain pointers and are inline, then we mark bucket
	// type as containing no pointers. This avoids scanning such maps.
	// However, bmap.overflow is a pointer. In order to keep overflow buckets
	// alive, we store pointers to all overflow buckets in hmap.extra.overflow and hmap.extra.oldoverflow.
	// overflow and oldoverflow are only used if key and elem do not contain pointers.
	// overflow contains overflow buckets for hmap.buckets.
	// oldoverflow contains overflow buckets for hmap.oldbuckets.
	// The indirection allows to store a pointer to the slice in hiter.
	overflow *[]*bmap
	oldoverflow *[]*bmap

	// nextOverflow holds a pointer to a free overflow bucket.
	nextOverflow *bmap
}

// A bucket for a Go map.
type bmap struct {
	// tophash generally contains the top byte of the hash value
	// for each key in this bucket. If tophash[0] < minTopHash,
	// tophash[0] is a bucket evacuation state instead.
	//tophash用于快速找到对应的键值
	tophash [bucketCnt]uint8
	// Followed by bucketCnt keys and then bucketCnt elems.
	// NOTE: packing all the keys together and then all the elems together makes the
	// code a bit more complicated than alternating key/elem/key/elem/... but it allows
	// us to eliminate padding which would be needed for, e.g., map[int64]int8.
	// Followed by an overflow pointer.
}

// bucketShift returns 1<<b, optimized for code generation.
//计算2^n的值
func bucketShift(b uint8) uintptr {
	// Masking the shift amount allows overflow checks to be elided.
	return uintptr(1) << (b & (sys.PtrSize*8 - 1))
}

// bucketMask returns 1<<b - 1, optimized for code generation.
func bucketMask(b uint8) uintptr {
	return bucketShift(b) - 1
}

// tophash calculates the tophash value for hash.
//生成tophash值
func tophash(hash uintptr)uint8{
	top := uint8(hash >> (sys.PtrSize*8 - 8))
	if top < minTopHash {
		top += minTopHash
	}
	return top
}

func evacuated(b *bmap) bool {
	h :=b.tophash[0]

}

//获取溢出桶
func (b *bmap) overflow(t *maptype) *bmap {
	return *(**bmap)(add(unsafe.Pointer(b),uintptr(t.bucketsize)-sys.PtrSize))
}

//设置溢出桶
func (b *bmap) setoverflow(t *maptype, ovf *bmap) {
	*(**bmap)(add(unsafe.Pointer(b), uintptr(t.bucketsize)-sys.PtrSize)) = ovf
}

//当溢出桶被使用时,创建新的溢出桶
func (h *hmap) newoverflow(t *maptype, b *bmap) *bmap {
	var ovf *bmap
	if h.extra != nil && h.extra.nextOverflow != nil{
		// We have preallocated overflow buckets available.
		// See makeBucketArray for more details.
		ovf = h.extra.nextOverflow
		//如果当前溢出桶是nil了,表示不是最后一个桶(不是特别清楚的去看初始化的溢出桶的最后一个桶是链接buckets的首地址)
		//这时候更新h.extra.nextOverflow链接到下一个桶方便下次使用
		if ovf.overflow(t) == nil{
			// We're not at the end of the preallocated overflow buckets. Bump the pointer.
			h.extra.nextOverflow =(*bmap)(add(unsafe.Pointer(ovf), uintptr(t.bucketsize)))
		}else{
			// This is the last preallocated overflow bucket.
			// Reset the overflow pointer on this bucket,
			// which was set to a non-nil sentinel value.
			//表示当前是最后一个桶了,h.extra.nextOverflow设置为nil,下次要从内存中分配
			ovf.setoverflow(t,nil)
			h.extra.nextOverflow = nil
		}
	}else{
		//没有溢出桶直接内存分配一个
		ovf =(*bmap)(newobject(t.bucket))
	}
}

// makemap implements Go map creation for make(map[k]v, hint).
// If the compiler has determined that the map or the first bucket
// can be created on the stack, h and/or bucket may be non-nil.
// If h != nil, the map can be created directly in h.
// If h.buckets != nil, bucket pointed to can be used as the first bucket.
func makemap(t *maptype,hint int,h *hmap)*hmap{
	//计算内存空间和判断是否内存溢出
	mem,overflow := math.MulUintptr(uintptr(hint),t.bucket.size)
	if overflow || mem > maxAlloc{
		hint = 0
	}

	// initialize Hmap
	if h == nil{
		h = new(hmap)
	}
	h.hash0 = fastrand()

	// Find the size parameter B which will hold the requested # of elements.
	// For hint < 0 overLoadFactor returns false since hint < bucketCnt.
	//计算出指数B,那么桶的数量表示2^B
	B := uint8(0)
	for overLoadFactor(hint,B){
		B++
	}
	h.B = B

	// allocate initial hash table
	// if B == 0, the buckets field is allocated lazily later (in mapassign)
	// If hint is large zeroing this memory could take a while.
	if h.B != 0{
		var nextOverflow *bmap
		//根据B去创建对应的桶和溢出桶
		h.buckets,nextOverflow =makeBucketArray(t,h.B,nil)
		if nextOverflow != nil{
			h.extra = new(mapextra)
			h.extra.nextOverflow = nextOverflow
		}
	}
	return h
}

// makeBucketArray initializes a backing array for map buckets.
// 1<<b is the minimum number of buckets to allocate.
// dirtyalloc should either be nil or a bucket array previously
// allocated by makeBucketArray with the same t and b parameters.
// If dirtyalloc is nil a new backing array will be alloced and
// otherwise dirtyalloc will be cleared and reused as backing array.
func makeBucketArray(t *maptype,b uint8,dirtyalloc unsafe.Pointer)(buckets unsafe.Pointer, nextOverflow *bmap){
	base :=bucketShift(b)
	nbuckets := base
	// For small b, overflow buckets are unlikely.
	// Avoid the overhead of the calculation.
	//当指数B大于等于4,增加额外的溢出桶
	if b >= 4{
		// Add on the estimated number of overflow buckets
		// required to insert the median number of elements
		// used with this value of b.
		nbuckets += bucketShift(b - 4)
		sz :=t.bucket.size * nbuckets
		up :=roundupsize(sz)
		if up != sz{
			nbuckets = up / t.bucket.size
		}
	}

	if dirtyalloc == nil{
		//生成对应数量的桶
		buckets =newarray(t.bucket,int(nbuckets))
	}else{
		// dirtyalloc was previously generated by
		// the above newarray(t.bucket, int(nbuckets))
		// but may not be empty.
		buckets = dirtyalloc
		size :=t.bucket.size *nbuckets
		print(size)
	}

	if base != nbuckets{
		// We preallocated some overflow buckets.
		// To keep the overhead of tracking these overflow buckets to a minimum,
		// we use the convention that if a preallocated overflow bucket's overflow
		// pointer is nil, then there are more available by bumping the pointer.
		// We need a safe non-nil pointer for the last overflow bucket; just use buckets.
		//得到对应的溢出桶
		nextOverflow = (*bmap)(add(buckets,base * uintptr(t.bucketsize)))
		last :=(*bmap)(add(buckets,(nbuckets-1)* uintptr(t.bucketsize)))
		last.setoverflow(t,(*bmap)(buckets))
	}
	return buckets,nextOverflow
}

// mapaccess1 returns a pointer to h[key].  Never returns nil, instead
// it will return a reference to the zero object for the elem type if
// the key is not in the map.
// NOTE: The returned pointer may keep the whole map live, so don't
// hold onto it for very long.
func mapaccess1(t *maptype, h *hmap, key unsafe.Pointer) unsafe.Pointer {
	if h == nil || h.count == 0{

	}

	//当前map正在被写入
	if h.flags&hashWriting != 0 {
		throw("concurrent map read and map write")
	}

	hash := t.hasher(key,uintptr(h.hash0))
	m := bucketMask(h.B)
	//根据hash获取指定的桶
	b :=(*bmap)(unsafe.Pointer(uintptr(h.buckets) + (hash&m)*uintptr(t.bucketsize)))
	//如果旧桶不为空，说明发生了扩容，在旧桶里找
	if c :=h.oldbuckets; c != nil{
		if !h.sameSizeGrow(){
			// There used to be half as many buckets; mask down one more power of two.
			//如果不是等量扩容,旧桶是当前桶的一半大小
			m >>= 1
		}
		oldb := (*bmap)(add(c,(hash&m)*uintptr(t.bucketsize)))
	}
}

// Like mapaccess, but allocates a slot for the key if it is not present in the map.
func mapassign(t *maptype, h *hmap, key unsafe.Pointer) unsafe.Pointer {
	if h ==  nil{
	}

	if h.flags&hashWriting != 0{

	}
	hash :=t.hasher(key,uintptr(h.hash0))

	// Set hashWriting after calling t.hasher, since t.hasher may panic,
	// in which case we have not actually done a write.
	//更新状态为正在写入
	h.flags ^= hashWriting

	if h.buckets == nil{
		h.buckets = newobject(t.bucket)//相当于 newarray(t.bucket, 1)
	}

again:
	bucket := hash & bucketMask(h.B)

	b :=(*bmap)(unsafe.Pointer(uintptr(h.buckets)+bucket*uintptr(t.bucketsize)))
	top :=tophash(hash)

	var inserti *uint8//记录插入的tophash
	var insertk unsafe.Pointer//记录插入的key值
	var elem unsafe.Pointer//记录插入的value值

bucketloop:
	for{
		for i :=uintptr(0);i < bucketCnt;i++{
			//判断tophash是否相等
			if b.tophash[i] != top {
				//如果tophash不相等并且等于空,那么可以则可以插入
				if isEmpty(b.tophash[i]) && inserti == nil{
					inserti = &b.tophash[i]
					//获取对应插入key和value的指针地址
					insertk = add(unsafe.Pointer(b),dataOffset+i*uintptr(t.keysize))
					elem = add(unsafe.Pointer(b),dataOffset+bucketCnt*uintptr(t.keysize)+i *uintptr(t.elemsize))
				}
				//如果该位置是可用状态的
				if b.tophash[i] == emptyRest{
					break bucketloop
				}
				continue
			}

			//走到这里说明tophash相等,说明之前已经设置过了
			k := add(unsafe.Pointer(b),dataOffset+i+uintptr(t.keysize))
			//如果是指针,则要转化为指针
			if t.indirectkey(){
				k = *((*unsafe.Pointer)(k))
			}
			if !t.key.equal(key,k){
				continue
			}
			// already have a mapping for key. Update it.
			//key值需要修改
			if t.needkeyupdate(){
				typedmemmove(t.key, k, key)
			}
			elem = add(unsafe.Pointer(b),dataOffset+bucketCnt*uintptr(t.keysize)+i*uintptr(t.elemsize))
			goto done

			//空间已经满了,找到溢出桶
			ovf := b.overflow(t)
			if ovf == nil{
				break
			}
			b = ovf
		}
	}

	// Did not find mapping for key. Allocate new cell & add entry.

	//没有插入过的情况会这里往下走，如果已经满了,会转化为溢出桶插入
	if inserti == nil{

	}

	// store new key/elem at insert position
	//如果key是指针
	if t.indirectkey(){
		kmem :=newobject(t.key)
		*(*unsafe.Pointer)(insertk) = kmem
		insertk = kmem
	}
	//如果插入元素是指针
	if t.indirectelem(){
		vmem :=newobject(t.elem)
		*(*unsafe.Pointer)(elem) = vmem
	}
	typedmemmove(t.key, insertk, key)
	//设置tophash值
	*inserti = top
	h.count++ //元素增加

done:
		if h.flags&hashWriting == 0{

		}
		h.flags &^= hashWriting
		//如果插入元素是指针
		if t.indirectelem(){
			elem = *((*unsafe.Pointer)(elem))
		}

		//注意这里只是返回指针地址,真正插入元素是在编译完成
		return elem
}

//判断是否等量扩容,如果为true,则表示buckets和oldbuckets大小相等
func (h *hmap)sameSizeGrow()bool{
	return h.flags&sameSizeGrow != 0
}

// overLoadFactor reports whether count items placed in 1<<B buckets is over loadFactor
//用于帮助计算桶的指数值
func overLoadFactor(count int, B uint8) bool {
	return count > bucketCnt && uintptr(count) > loadFactorNum*(bucketShift(B)/loadFactorDen)
}

const maxZero = 1024 // must match value in cmd/compile/internal/gc/walk.go:zeroValSize
var zeroVal [maxZero]byte