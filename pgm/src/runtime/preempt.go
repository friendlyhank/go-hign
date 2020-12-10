package runtime

import "unsafe"

// Keep in sync with cmd/compile/internal/gc/plive.go:go115ReduceLiveness.
const go115ReduceLiveness = true

const go115RestartSeq = go115ReduceLiveness && true // enable restartable sequences

// wantAsyncPreempt returns whether an asynchronous preemption is
// queued for gp.
func wantAsyncPreempt(gp *g) bool {
	// Check both the G and the P.
	return (gp.preempt || gp.m.p != 0 && gp.m.p.ptr().preempt) && readgstatus(gp)&^_Gscan == _Grunning
}

// asyncPreemptStack is the bytes of stack space required to inject an
// asyncPreempt call.
var asyncPreemptStack = ^uintptr(0)

// canPreemptM reports whether mp is in a state that is safe to preempt.
//
// It is nosplit because it has nosplit callers.
//
//go:nosplit
func canPreemptM(mp *m) bool {
	return mp.locks == 0 && mp.mallocing == 0 && mp.preemptoff == "" && mp.p.ptr().status == _Prunning
}

//go:generate go run mkpreempt.go

// asyncPreempt saves all user registers and calls asyncPreempt2.
//
// When stack scanning encounters an asyncPreempt frame, it scans that
// frame and its parent frame conservatively.
//
// asyncPreempt is implemented in assembly.
func asyncPreempt()

// isAsyncSafePoint reports whether gp at instruction PC is an
// asynchronous safe point. This indicates that:
//
// 1. It's safe to suspend gp and conservatively scan its stack and
// registers. There are no potentially hidden pointer values and it's
// not in the middle of an atomic sequence like a write barrier.
//
// 2. gp has enough stack space to inject the asyncPreempt call.
//
// 3. It's generally safe to interact with the runtime, even if we're
// in a signal handler stopped here. For example, there are no runtime
// locks held, so acquiring a runtime lock won't self-deadlock.
//
// In some cases the PC is safe for asynchronous preemption but it
// also needs to adjust the resumption PC. The new PC is returned in
// the second result.
func isAsyncSafePoint(gp *g, pc, sp, lr uintptr) (bool, uintptr) {
	mp := gp.m

	// Only user Gs can have safe-points. We check this first
	// because it's extremely common that we'll catch mp in the
	// scheduler processing this G preemption.
	if mp.curg != gp {
		return false, 0
	}

	// Check M state.
	if mp.p == 0 || !canPreemptM(mp) {
		return false, 0
	}

	// Check stack space.
	if sp < gp.stack.lo || sp-gp.stack.lo < asyncPreemptStack {
		return false, 0
	}

	// Check if PC is an unsafe-point.
	f := findfunc(pc)
	if !f.valid() {
		// Not Go code.
		return false, 0
	}
	if (GOARCH == "mips" || GOARCH == "mipsle" || GOARCH == "mips64" || GOARCH == "mips64le") && lr == pc+8 && funcspdelta(f, pc, nil) == 0 {
		// We probably stopped at a half-executed CALL instruction,
		// where the LR is updated but the PC has not. If we preempt
		// here we'll see a seemingly self-recursive call, which is in
		// fact not.
		// This is normally ok, as we use the return address saved on
		// stack for unwinding, not the LR value. But if this is a
		// call to morestack, we haven't created the frame, and we'll
		// use the LR for unwinding, which will be bad.
		return false, 0
	}
	var up int32
	var startpc uintptr
	if !go115ReduceLiveness {
		smi := pcdatavalue(f, _PCDATA_RegMapIndex, pc, nil)
		if smi == -2 {
			// Unsafe-point marked by compiler. This includes
			// atomic sequences (e.g., write barrier) and nosplit
			// functions (except at calls).
			return false, 0
		}
	} else {
		up, startpc = pcdatavalue2(f, _PCDATA_UnsafePoint, pc)
		if up != _PCDATA_UnsafePointSafe {
			// Unsafe-point marked by compiler. This includes
			// atomic sequences (e.g., write barrier) and nosplit
			// functions (except at calls).
			return false, 0
		}
	}
	if fd := funcdata(f, _FUNCDATA_LocalsPointerMaps); fd == nil || fd == unsafe.Pointer(&no_pointers_stackmap) {
		// This is assembly code. Don't assume it's
		// well-formed. We identify assembly code by
		// checking that it has either no stack map, or
		// no_pointers_stackmap, which is the stack map
		// for ones marked as NO_LOCAL_POINTERS.
		//
		// TODO: Are there cases that are safe but don't have a
		// locals pointer map, like empty frame functions?
		return false, 0
	}
	name := funcname(f)
	if inldata := funcdata(f, _FUNCDATA_InlTree); inldata != nil {
		inltree := (*[1 << 20]inlinedCall)(inldata)
		ix := pcdatavalue(f, _PCDATA_InlTreeIndex, pc, nil)
		if ix >= 0 {
			name = funcnameFromNameoff(f, inltree[ix].func_)
		}
	}
	if hasPrefix(name, "runtime.") ||
		hasPrefix(name, "runtime/internal/") ||
		hasPrefix(name, "reflect.") {
		// For now we never async preempt the runtime or
		// anything closely tied to the runtime. Known issues
		// include: various points in the scheduler ("don't
		// preempt between here and here"), much of the defer
		// implementation (untyped info on stack), bulk write
		// barriers (write barrier check),
		// reflect.{makeFuncStub,methodValueCall}.
		//
		// TODO(austin): We should improve this, or opt things
		// in incrementally.
		return false, 0
	}
	if go115RestartSeq {
		switch up {
		case _PCDATA_Restart1, _PCDATA_Restart2:
			// Restartable instruction sequence. Back off PC to
			// the start PC.
			if startpc == 0 || startpc > pc || pc-startpc > 20 {
				throw("bad restart PC")
			}
			return true, startpc
		case _PCDATA_RestartAtEntry:
			// Restart from the function entry at resumption.
			return true, f.entry
		}
	} else {
		switch up {
		case _PCDATA_Restart1, _PCDATA_Restart2, _PCDATA_RestartAtEntry:
			// go115RestartSeq is not enabled. Treat it as unsafe point.
			return false, 0
		}
	}
	return true, pc
}

var no_pointers_stackmap uint64 // defined in assembly, for NO_LOCAL_POINTERS macro
