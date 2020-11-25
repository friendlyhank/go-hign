package cpu

// The booleans in X86 contain the correspondingly named cpuid feature bit.
// HasAVX and HasAVX2 are only set if the OS does support XMM and YMM registers
// in addition to the cpuid feature bit being set.
// The struct is padded to avoid false sharing.
var X86 struct {
	HasAVX2 bool
	HasERMS bool
}
