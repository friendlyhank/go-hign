package runtime

// itabAdd adds the given itab to the itab hash table.
// itabLock must be held.
func itabAdd(m *itab) {

}

//在编译过程中,如果有实现接口类型会被初始化
func itabsinit() {
	for _,md := range activeModules(){
		for _,i := range md.itablinks{
			itabAdd(i)
		}
	}
}
