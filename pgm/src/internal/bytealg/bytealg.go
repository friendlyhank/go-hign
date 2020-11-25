package bytealg

import (
	"internal/cpu"
	"unsafe"
)

// Offsets into internal/cpu records for use in assembly.
const (
	offsetX86HasAVX2 = unsafe.Offsetof(cpu.X86.HasAVX2)
)
