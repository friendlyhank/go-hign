package runtime

// Many of the following panic entry-points turn into throws when they
// happen in various runtime contexts. These should never happen in
// the runtime, and if they do, they indicate a serious issue and
// should not be caught by user code.
//
// The panic{Index,Slice,divide,shift} functions are called by
// code generated by the compiler for out of bounds index expressions,
// out of bounds slice expressions, division by zero, and shift by negative.
// The panicdivide (again), panicoverflow, panicfloat, and panicmem
// functions are called by the signal handler when a signal occurs
// indicating the respective problem.
//
// Since panic{Index,Slice,shift} are never called directly, and
// since the runtime package should never have an out of bounds slice
// or array reference or negative shift, if we see those functions called from the
// runtime package we turn the panic into a throw. That will dump the
// entire runtime stack for easier debugging.
//
// The entry points called by the signal handler will be called from
// runtime.sigpanic, so we can't disallow calls from the runtime to
// these (they always look like they're called from the runtime).
// Hence, for these, we just check for clearly bad runtime conditions.
//
// The panic{Index,Slice} functions are implemented in assembly and tail call
// to the goPanic{Index,Slice} functions below. This is done so we can use
// a space-minimal register calling convention.

// failures in the comparisons for s[x], 0 <= x < y (y == len(s))
func goPanicIndex(x int, y int) {

}
