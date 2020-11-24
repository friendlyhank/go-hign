package runtime

import "unsafe"

// The compiler emits calls to printlock and printunlock around
// the multiple calls that implement a single Go print or println
// statement. Some of the print helpers (printslice, for example)
// call print recursively. There is also the problem of a crash
// happening during the print routines and needing to acquire
// the print lock to print information about the crash.
// For both these reasons, let a thread acquire the printlock 'recursively'.

func printlock() {

}

func printunlock() {

}

func printnl() {}

func printint(v int64) {

}

func printpointer(p unsafe.Pointer) {

}
