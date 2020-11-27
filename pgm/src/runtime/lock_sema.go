package runtime

func lock(l *mutex){
	lockWithRank(l,getLockRank(l))
}

func lock2(l *mutex){
}

func unlock(l *mutex){
	unlockWithRank(l)
}

//go:nowritebarrier
// We might not be holding a p in this code.
func unlock2(l *mutex) {

}
