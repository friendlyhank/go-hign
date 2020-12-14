package runtime


// Following are not implemented.

//好像没什么用
func initsig(preinit bool) {
}

func signame(sig uint32) string {
	return ""
}

//go:nosplit
func crash() {
	// TODO: This routine should do whatever is needed
	// to make the Windows program abort/crash as it
	// would if Go was not intercepting signals.
	// On Unix the routine would remove the custom signal
	// handler and then raise a signal (like SIGABRT).
	// Something like that should happen here.
	// It's okay to leave this empty for now: if crash returns
	// the ordinary exit-after-panic happens.
}
