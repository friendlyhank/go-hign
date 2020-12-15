package runtime

//全局的页分配器
type pageAlloc struct {
	// sysStat is the runtime memstat to update when new system
	// memory is committed by the pageAlloc for allocation metadata.
	//统计相关
	sysStat *uint64
}

func (s *pageAlloc)init(mheapLock *mutex, sysStat *uint64){

}