// Copyright 2014 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package runtime

// This file contains the implementation of Go's map type.
//
// A map is just a hash table. The data is arranged
// into an array of buckets. Each bucket contains up to
// 8 key/elem pairs. The low-order bits of the hash are
// used to select a bucket. Each bucket contains a few
// high-order bits of each hash to distinguish the entries
// within a single bucket.
//
// If more than 8 keys hash to a bucket, we chain on
// extra buckets.
//
// When the hashtable grows, we allocate a new array
// of buckets twice as big. Buckets are incrementally
// copied from the old bucket array to the new bucket array.
//
// Map iterators walk through the array of buckets and
// return the keys in walk order (bucket #, then overflow
// chain order, then bucket index).  To maintain iteration
// semantics, we never move keys within their bucket (if
// we did, keys might be returned 0 or 2 times).  When
// growing the table, iterators remain iterating through the
// old table and must check the new table if the bucket
// they are iterating through has been moved ("evacuated")
// to the new table.

// Picking loadFactor: too large and we have lots of overflow
// buckets, too small and we waste a lot of space. I wrote
// a simple program to check some stats for different loads:
// (64-bit, 8 byte keys and elems)
//  loadFactor    %overflow  bytes/entry     hitprobe    missprobe
//        4.00         2.13        20.77         3.00         4.00
//        4.50         4.05        17.30         3.25         4.50
//        5.00         6.85        14.77         3.50         5.00
//        5.50        10.55        12.94         3.75         5.50
//        6.00        15.27        11.67         4.00         6.00
//        6.50        20.90        10.79         4.25         6.50
//        7.00        27.14        10.15         4.50         7.00
//        7.50        34.03         9.73         4.75         7.50
//        8.00        41.10         9.40         5.00         8.00
//
// %overflow   = percentage of buckets which have an overflow bucket
// bytes/entry = overhead bytes used per key/elem pair
// hitprobe    = # of entries to check when looking up a present key
// missprobe   = # of entries to check when looking up an absent key
//
// Keep in mind this data is for maximally loaded tables, i.e. just
// before the table grows. Typical tables will be somewhat less loaded.

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

	// Maximum key or elem size to keep inline (instead of mallocing per element).
	// Must fit in a uint8.
	// Fast versions cannot handle big elems - the cutoff size for
	// fast versions in cmd/compile/internal/gc/walk.go must be at most this elem.
	maxKeySize  = 128
	maxElemSize = 128

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
	emptyRest      = 0 //元素被删除且重置 this cell is empty, and there are no more non-empty cells at higher indexes or overflows.
	emptyOne       = 1 //元素是空的 this cell is empty
	evacuatedX     = 2 //元素被迁移到前半部分 key/elem is valid.  Entry has been evacuated to first half of larger table.
	evacuatedY     = 3 //元素被迁移到后半部分 same as above, but evacuated to second half of larger table.
	evacuatedEmpty = 4 //元素已经被迁移 cell is empty, bucket is evacuated.
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
	nevacuate  uintptr //扩容之后数据迁移的计数器,方便知道下次迁移的位置

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
	overflow *[]*bmap //当前被使用的溢出桶(注意溢出桶不一定是连续的,可以看插入的时候会从内存分配溢出桶)
	oldoverflow *[]*bmap //扩容的话被使用的溢出桶会放在这里

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
//生成tophash值,取hash得高八位来作为tophash
func tophash(hash uintptr)uint8{
	top := uint8(hash >> (sys.PtrSize*8 - 8))
	if top < minTopHash {
		top += minTopHash
	}
	return top
}

//判断桶是否为空,如果桶是空的tophash表示可以状态值
func evacuated(b *bmap) bool {
	h :=b.tophash[0]
	return h > emptyOne && h <minTopHash
}

//这里字面意思理解获取溢出桶,实际理解是找到下一个链接的桶
func (b *bmap) overflow(t *maptype) *bmap {
	return *(**bmap)(add(unsafe.Pointer(b),uintptr(t.bucketsize)-sys.PtrSize))
}

//这里字面意思理解设置溢出桶,实际可以理解为链表,设置链接下一个桶
func (b *bmap) setoverflow(t *maptype, ovf *bmap) {
	*(**bmap)(add(unsafe.Pointer(b), uintptr(t.bucketsize)-sys.PtrSize)) = ovf
}

func (b *bmap)keys()unsafe.Pointer{
	return add(unsafe.Pointer(b),dataOffset)
}

// incrnoverflow increments h.noverflow.
// noverflow counts the number of overflow buckets.
// This is used to trigger same-size map growth.
// See also tooManyOverflowBuckets.
// To keep hmap small, noverflow is a uint16.
// When there are few buckets, noverflow is an exact count.
// When there are many buckets, noverflow is an approximate count.
func (h *hmap) incrnoverflow() {
	// We trigger same-size map growth if there are
	// as many overflow buckets as buckets.
	// We need to be able to count to 1<<h.B.
	if h.B < 16 {
		h.noverflow++
		return
	}
	// Increment with probability 1/(1<<(h.B-15)).
	// When we reach 1<<15 - 1, we will have approximately
	// as many overflow buckets as buckets.
	mask := uint32(1)<<(h.B-15) - 1
	// Example: if h.B == 18, then mask == 7,
	// and fastrand & 7 == 0 with probability 1/8.
	if fastrand()&mask == 0 {
		h.noverflow++
	}
}

//当溢出桶被使用时,创建新的溢出桶
func (h *hmap) newoverflow(t *maptype, b *bmap) *bmap {
	var ovf *bmap
	if h.extra != nil && h.extra.nextOverflow != nil{
		// We have preallocated overflow buckets available.
		// See makeBucketArray for more details.
		ovf = h.extra.nextOverflow
		//如果当前溢出桶是nil了,表示不是最后一个桶(不是特别清楚的去看初始化的溢出桶的最后一个桶是链接buckets的首地址)
		//溢出桶被使用之后也会被放在链表最后所以也不会是nil
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
	h.incrnoverflow()//溢出统计,扩容时也会参考这个

	//TODO HANK 这里我也不是很理解
	if t.bucket.ptrdata == 0{
		//这里可以说明overflow可以是不连续的空间
		h.createOverflow()
		*h.extra.overflow = append(*h.extra.overflow, ovf)
	}

	b.setoverflow(t,ovf) //将要用的溢出桶链接在当前桶的最尾部
	return ovf
}

func (h *hmap)createOverflow(){
	if h.extra == nil{
		h.extra = new(mapextra)
	}
	if h.extra.overflow == nil{
		h.extra.overflow = new([]*bmap)
	}
}

func makemap64(t *maptype,hint int64,h *hmap)*hmap{
	if int64(int(hint)) != hint{
		hint = 0
	}
	return makemap(t,int(hint),h)
}

// makemap_small implements Go map creation for make(map[k]v) and
// make(map[k]v, hint) when hint is known to be at most bucketCnt
// at compile time and the map needs to be allocated on the heap.
func makemap_small()*hmap{
	h := new(hmap)
	h.hash0 = fastrand()
	return h
}

// makemap implements Go map creation for make(map[k]v, hint).
// If the compiler has determined that the map or the first bucket
// can be created on the stack, h and/or bucket may be non-nil.
// If h != nil, the map can be created directly in h.
// If h.buckets != nil, bucket pointed to can be used as the first bucket.
//map初始化
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
//分配map的桶的内存空间
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
//map查询方法一
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
		//旧桶数据还没迁移
		if !evacuated(oldb){
			b = oldb //更改为去旧桶里查找
		}
	}
	top := tophash(hash)
bucketloop:
	for ; b != nil;b = b.overflow(t){
		for i := uintptr(0);i<bucketCnt;i++{
			if b.tophash[i] != top{
				if b.tophash[i] == emptyRest{
					break bucketloop
				}
				continue
			}
			k := add(unsafe.Pointer(b),dataOffset+i*uintptr(t.keysize))
			if t.indirectkey(){
				k = *((*unsafe.Pointer)(k))
			}
			if t.key.equal(key,k){
				e := add(unsafe.Pointer(b),dataOffset+bucketCnt*uintptr(t.keysize)+i*uintptr(t.elemsize))
				if t.indirectelem(){
					e = *((*unsafe.Pointer)(e))
				}
				return e
			}
		}
	}
	return unsafe.Pointer(&zeroVal[0])
}

//map查询方法二
func mapaccess2(t *maptype, h *hmap, key unsafe.Pointer) (unsafe.Pointer, bool) {
	if h == nil || h.count  == 0{
		return unsafe.Pointer(&zeroVal[0]),false
	}
	if h.flags&hashWriting != 0{

	}
	hash :=t.hasher(key,uintptr(h.hash0))
	m := bucketMask(h.B)
	//根据hash获取指定的桶
	b :=(*bmap)(unsafe.Pointer(uintptr(h.buckets)+(hash&m)*uintptr(t.bucketsize)))
	//如果旧桶不为空，说明发生了扩容，在旧桶里找
	if c :=h.oldbuckets;c != nil{
		if !h.sameSizeGrow(){
			// There used to be half as many buckets; mask down one more power of two.
			//如果不是等量扩容,旧桶是当前桶的一半大小
			m >>=1
		}
		oldb :=(*bmap)(unsafe.Pointer(uintptr(c)+(hash&m)*uintptr(t.bucketsize)))
		if !evacuated(oldb){
			b = oldb //更改为去旧桶里查找
		}
	}
	top := tophash(hash)
bucketloop:
	for ; b != nil;b =b.overflow(t){
		for i := uintptr(0);i < bucketCnt;i++{
			if b.tophash[i] != top{
				if b.tophash[i] == emptyRest{
					break bucketloop
				}
				continue
			}
			k := add(unsafe.Pointer(b),dataOffset+i*uintptr(t.keysize))
			if t.indirectkey(){
				k = *((*unsafe.Pointer)(k))
			}
			if t.key.equal(key,k){
				e := add(unsafe.Pointer(b),dataOffset+bucketCnt*uintptr(t.keysize)+i*uintptr(t.elemsize))
				if t.indirectelem(){
					e = *((*unsafe.Pointer)(e))
				}
				return e,true
			}
		}
	}
	return unsafe.Pointer(&zeroVal[0]),false
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
	//如果正在扩容,进行数据迁移
	if h.growing(){
		growWork(t, h, bucket)
	}

	b :=(*bmap)(unsafe.Pointer(uintptr(h.buckets)+bucket*uintptr(t.bucketsize)))
	top :=tophash(hash)

	var inserti *uint8//记录插入的tophash
	var insertk unsafe.Pointer//记录插入的key值地址
	var elem unsafe.Pointer//记录插入的value值地址

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

			//未找到可插入的位置,找一下有没溢出桶，如果有继续执行写入操作
			ovf := b.overflow(t)
			if ovf == nil{
				break
			}
			b = ovf
		}
	}

	// Did not find mapping for key. Allocate new cell & add entry.

	// If we hit the max load factor or we have too many overflow buckets,
	// and we're not already in the middle of growing, start growing.
	//1.哈希不是正在扩容状态的
	//2.元素的数量 > 2^B次方(桶的数量) * 6.5,6.5表示为装载因子,很容易理解装载因子最大为8(一个桶能装载的元素数量)
	//溢出桶过多 noverflow >= 1<<B,B最大为15
	if !h.growing() && (overLoadFactor(h.count+1,h.B) || tooManyOverflowBuckets(h.noverflow,h.B)){
		hashGrow(t,h)//发生扩容
		goto again
	}

	//没有插入过的情况会这里往下走
	if inserti == nil{
		//如果在正常桶和溢出桶中都未找到插入的位置，那么得到一个新的溢出桶执行插入
		// all current buckets are full, allocate a new one.
		newb := h.newoverflow(t,b)
		inserti =&newb.tophash[0]
		insertk = add(unsafe.Pointer(newb),dataOffset)
		elem = add(insertk,bucketCnt*uintptr(t.keysize))
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

func mapdelete(t *maptype, h *hmap, key unsafe.Pointer){
	if h ==  nil || h.count == 0{
		return
	}
	if h.flags&hashWriting != 0{

	}

	hash := t.hasher(key,uintptr(h.hash0))

	// Set hashWriting after calling t.hasher, since t.hasher may panic,
	// in which case we have not actually done a write (delete).
	h.flags ^= hashWriting

	bucket := hash & bucketMask(h.B)

	//如果正在扩容,进行数据迁移,否则要在旧桶里删除
	if h.growing(){
		growWork(t,h,bucket)
	}
	b :=(*bmap)(add(h.buckets,bucket*uintptr(t.bucketsize)))
	bOrig :=b
	top := tophash(hash)
search:
	for ;b != nil;b = b.overflow(t){
		for i := uintptr(0);i < bucketCnt;i++{
			if b.tophash[i] != top{
				if b.tophash[i] == emptyRest{
					break search
				}
				continue
			}
			k := add(unsafe.Pointer(b),dataOffset+i*uintptr(t.keysize))
			k2 := k
			if t.indirectkey(){
				k2 =*((*unsafe.Pointer)(k2))
			}
			if !t.key.equal(key,k2){
				continue
			}
			// Only clear key if there are pointers in it.
			if t.indirectkey(){
				*(*unsafe.Pointer)(k) = nil
			}else if t.key.ptrdata != 0{
				memclrHasPointers(k,t.key.size)
			}
			e := add(unsafe.Pointer(b),dataOffset+bucketCnt*uintptr(t.keysize)+i*uintptr(t.elemsize))
			if t.indirectelem(){
				*(*unsafe.Pointer)(e)=nil
			}else if t.elem.ptrdata != 0{
				memclrHasPointers(e,t.elem.size)
			}else{
				memclrNoHeapPointers(e,t.elem.size)
			}
			b.tophash[i] = emptyOne
		}
	}
}

//哈希扩容
func hashGrow(t *maptype,h *hmap){
	// If we've hit the load factor, get bigger.
	// Otherwise, there are too many overflow buckets,
	// so keep the same number of buckets and "grow" laterally.
	bigger := uint8(1)
	if !overLoadFactor(h.count+1,h.B){ //没有超出装载因子是等量扩容
		bigger = 0
		h.flags |= sameSizeGrow
	}
	oldbuckets := h.buckets
	newbuckets, nextOverflow := makeBucketArray(t, h.B+bigger, nil)

	//更新哈希的标志
	flags := h.flags &^ (iterator | oldIterator)
	if h.flags&iterator != 0{
		flags |= oldIterator
	}
	// commit the grow (atomic wrt gc)
	h.B += bigger
	h.flags = flags
	h.oldbuckets = oldbuckets
	h.buckets = newbuckets
	h.nevacuate = 0
	h.noverflow = 0

	if h.extra != nil && h.extra.overflow != nil{
		// Promote current overflow buckets to the old generation.
		if h.extra.oldoverflow != nil{

		}
		h.extra.oldoverflow = h.extra.overflow
		h.extra.overflow =  nil
	}
	if nextOverflow != nil{
		if h.extra == nil{
			h.extra = new(mapextra)
		}
		h.extra.nextOverflow = nextOverflow
	}
	// the actual copying of the hash table data is done incrementally
	// by growWork() and evacuate().
}

// overLoadFactor reports whether count items placed in 1<<B buckets is over loadFactor
//用于帮助计算桶的指数值 count > (1<< B *6.5)
func overLoadFactor(count int, B uint8) bool {
	return count > bucketCnt && uintptr(count) > loadFactorNum*(bucketShift(B)/loadFactorDen)
}

// tooManyOverflowBuckets reports whether noverflow buckets is too many for a map with 1<<B buckets.
// Note that most of these overflow buckets must be in sparse use;
// if use was dense, then we'd have already triggered regular map growth.
//判断溢出栈是否过大
func tooManyOverflowBuckets(noverflow uint16, B uint8) bool {
	// If the threshold is too low, we do extraneous work.
	// If the threshold is too high, maps that grow and shrink can hold on to lots of unused memory.
	// "too many" means (approximately) as many overflow buckets as regular buckets.
	// See incrnoverflow for more details.
	if B > 15{
		B = 15
	}
	// The compiler doesn't see here that B < 16; mask B to generate shorter shift code.
	//溢出桶的数量是否大于1<<B,B最大为15
	return noverflow >= uint16(1)<<(B&15)
}

//判断哈希表是否在扩容
func (h *hmap)growing()bool{
	return h.oldbuckets != nil
}

//判断是否等量扩容,如果为true,则表示buckets和oldbuckets大小相等
func (h *hmap)sameSizeGrow()bool{
	return h.flags&sameSizeGrow != 0
}

// noldbuckets calculates the number of buckets prior to the current map growth.
//获取旧桶的数量
func (h *hmap)noldbuckets()uintptr{
	oldB :=h.B
	if !h.sameSizeGrow(){
		oldB-- //扩容是两倍扩容,所以B-1就是之前旧桶B的大小
	}
	return bucketShift(oldB)
}

// oldbucketmask provides a mask that can be applied to calculate n % noldbuckets().
func (h *hmap) oldbucketmask() uintptr {
	return h.noldbuckets()-1
}

//扩容之后数据是逐渐转移的
func growWork(t *maptype,h *hmap,bucket uintptr){
	// make sure we evacuate the oldbucket corresponding
	// to the bucket we're about to use
	//对当前要使用的桶进行迁移
	evacuate(t,h,bucket&h.oldbucketmask())

	// evacuate one more oldbucket to make progress on growing
	if h.growing(){
		//从迁移的标志位置继续迁移
		evacuate(t,h,h.nevacuate)
	}
}

func bucketEvacuated(t *maptype, h *hmap, bucket uintptr) bool {
	b := (*bmap)(add(h.oldbuckets, bucket*uintptr(t.bucketsize)))
	return evacuated(b)
}

// evacDst is an evacuation destination.
type evacDst struct {
	b *bmap          // current destination bucket
	i int            // key/elem index into b
	k unsafe.Pointer // pointer to current key storage
	e unsafe.Pointer // pointer to current elem storage
}

func evacuate(t *maptype, h *hmap, oldbucket uintptr) {
	//获取指定要转移的桶
	b := (*bmap)(add(h.oldbuckets,oldbucket*uintptr(t.bucketsize)))
	newbit := h.noldbuckets()
	//如果这个桶不为空
	if !evacuated(b){
		// TODO: reuse overflow buckets instead of using new ones, if there
		// is no iterator using the old buckets.  (If !oldIterator.)

		// xy contains the x and y (low and high) evacuation destinations.
		var xy [2]evacDst
		x :=&xy[0]
		x.b = (*bmap)(add(h.buckets,oldbucket*uintptr(t.bucketsize)))
		x.k =add(unsafe.Pointer(x.b),dataOffset)
		x.e = add(x.k,bucketCnt*uintptr(t.keysize))

		//不是等量扩容才会设置两个evacDst
		if !h.sameSizeGrow(){
			// Only calculate y pointers if we're growing bigger.
			// Otherwise GC can see bad pointers.
			y :=&xy[1]
			y.b = (*bmap)(add(h.buckets,(oldbucket+newbit)*uintptr(t.bucketsize)))
			y.k = add(unsafe.Pointer(y.b),dataOffset)
			y.e = add(y.k,bucketCnt*uintptr(t.keysize))
		}

		//遍历桶
		for ; b != nil;b = b.overflow(t){
			//获取k和e的首地址
			k := add(unsafe.Pointer(b),dataOffset)
			e := add(k,bucketCnt*uintptr(t.keysize))
			//遍历桶内元素,不断去获取i,key,elem三个首地址
			for i := 0; i < bucketCnt; i, k, e = i+1, add(k, uintptr(t.keysize)), add(e, uintptr(t.elemsize)) {
				top :=b.tophash[i]
				if isEmpty(top){
					b.tophash[i] = evacuatedEmpty//标记为迁移状态
					continue
				}
				if top < minTopHash{

				}
				k2 := k
				if t.indirectkey(){
					k2 = *((*unsafe.Pointer)(k2))
				}
				var useY uint8
				if !h.sameSizeGrow(){
					// Compute hash to make our evacuation decision (whether we need
					// to send this key/elem to bucket x or bucket y).
					hash :=t.hasher(k2,uintptr(h.hash0))
					if h.flags&iterator != 0 && !t.reflexivekey() && !t.key.equal(k2,k2){
						// If key != key (NaNs), then the hash could be (and probably
						// will be) entirely different from the old hash. Moreover,
						// it isn't reproducible. Reproducibility is required in the
						// presence of iterators, as our evacuation decision must
						// match whatever decision the iterator made.
						// Fortunately, we have the freedom to send these keys either
						// way. Also, tophash is meaningless for these kinds of keys.
						// We let the low bit of tophash drive the evacuation decision.
						// We recompute a new random tophash for the next level so
						// these keys will get evenly distributed across all buckets
						useY = top & 1
						top  = tophash(hash)
					}else{
						if hash&newbit != 0{
							useY = 1
						}
					}
				}

				if evacuatedX+1 != evacuatedY || evacuatedX^1 != evacuatedY {

				}

				b.tophash[i] = evacuatedX + useY //设置状态旧桶数据插入到前部分还是后部分 evacuatedX + 1 == evacuatedY
				dst :=&xy[useY]
				//当前桶已经满了,插入到溢出桶
				if dst.i == bucketCnt{
					dst.b = h.newoverflow(t, dst.b)
					dst.i = 0
					dst.k = add(unsafe.Pointer(dst.b),dataOffset)
					dst.e = add(dst.k,bucketCnt*uintptr(t.keysize))
				}
				dst.b.tophash[dst.i&(bucketCnt-1)] = top // mask dst.i as an optimization, to avoid a bounds check
				//key值复制
				if t.indirectkey(){//指针的复制
					*(*unsafe.Pointer)(dst.k) = k2 // copy pointer
				}else{
					typedmemmove(t.key, dst.k, k) //非指针的key复制 copy elem
				}
				//value值复制
				if t.indirectelem(){//key指针复制
					*(*unsafe.Pointer)(dst.e) = *(*unsafe.Pointer)(e)
				}else{
					typedmemmove(t.key, dst.k, k) //非指针的value复制 copy elem
				}
				dst.i++
				// These updates might push these pointers past the end of the
				// key or elem arrays.  That's ok, as we have the overflow pointer
				// at the end of the bucket to protect against pointing past the
				// end of the bucket.
				//找到桶下一个元素cell的地址位置
				dst.k = add(dst.k,uintptr(t.keysize))
				dst.e = add(dst.e,uintptr(t.elemsize))
			}
		}
		//如果旧桶没有在使用,就把旧桶清除掉帮助gc
		for h.flags&oldIterator == 0&&t.bucket.ptrdata != 0{
			b := add(h.oldbuckets, oldbucket*uintptr(t.bucketsize))
			// Preserve b.tophash because the evacuation
			// state is maintained there.
			ptr := add(b, dataOffset)
			n := uintptr(t.bucketsize) - dataOffset
			memclrHasPointers(ptr, n)
		}
	}

	//迁移完成之后,更新nevacuate迁移计数器
	if oldbucket == h.nevacuate{
		advanceEvacuationMark(h,t,newbit)
	}
}

//迁移计数器的标记
func advanceEvacuationMark(h *hmap, t *maptype, newbit uintptr) {
	h.nevacuate++
	// Experiments suggest that 1024 is overkill by at least an order of magnitude.
	// Put it in there as a safeguard anyway, to ensure O(1) behavior.
	stop := h.nevacuate + 1024
	if stop > newbit {
		stop = newbit
	}
	for h.nevacuate != stop && bucketEvacuated(t, h, h.nevacuate) {
		h.nevacuate++
	}
	if h.nevacuate == newbit { // newbit == # of oldbuckets
		// Growing is all done. Free old main bucket array.
		h.oldbuckets = nil
		// Can discard old overflow buckets as well.
		// If they are still referenced by an iterator,
		// then the iterator holds a pointers to the slice.
		if h.extra != nil {
			h.extra.oldoverflow = nil
		}
		h.flags &^= sameSizeGrow
	}
}

const maxZero = 1024 // must match value in cmd/compile/internal/gc/walk.go:zeroValSize
var zeroVal [maxZero]byte