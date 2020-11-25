package runtime

import (
	"internal/cpu"
	"unsafe"
)

// Offsets into internal/cpu records for use in assembly.
const (
	offsetX86HasAVX2 = unsafe.Offsetof(cpu.X86.HasAVX2)
	offsetX86HasERMS = unsafe.Offsetof(cpu.X86.HasERMS)
)
