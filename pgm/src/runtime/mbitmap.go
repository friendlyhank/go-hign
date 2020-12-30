package runtime

// nextFreeIndex returns the index of the next free object in s at
// or after s.freeindex.
// There are hardware instructions that can be used to make this
// faster if profiling warrants it.
//mspan下一个空闲的索引
func (s *mspan)nextFreeIndex()uintptr{
	sfreeindex := s.freeindex
	snelems := s.nelems
	if sfreeindex == snelems{
		return sfreeindex
	}
	if sfreeindex > snelems {
		throw("s.freeindex > s.nelems")
	}
}
