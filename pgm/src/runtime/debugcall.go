package runtime

// debugCallCheck checks whether it is safe to inject a debugger
// function call with return PC pc. If not, it returns a string
// explaining why.
//
//go:nosplit
func debugCallCheck(pc uintptr) string {
	return ""
}

// debugCallWrap starts a new goroutine to run a debug call and blocks
// the calling goroutine. On the goroutine, it prepares to recover
// panics from the debug call, and then calls the call dispatching
// function at PC dispatch.
//
// This must be deeply nosplit because there are untyped values on the
// stack from debugCallV1.
//
//go:nosplit
func debugCallWrap(dispatch uintptr) {

}
