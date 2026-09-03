package arm64

import (
	"testing"
)

func FuzzDecoderPolicyNeverPanics(f *testing.F) {
	for _, raw := range []uint32{
		0x00000000,
		0xd503201f, // nop
		0xd65f03c0, // ret
		0x14000000, // b .
		0x94000000, // bl .
		0xffffffff,
	} {
		f.Add(raw, int32(0))
	}
	f.Fuzz(func(t *testing.T, raw uint32, offset int32) {
		// Keep the fuzzer focused on architectural decoding rather than integer
		// conversion pathologies in callers.
		off := int(offset & 0x0fffffff)
		inst := NewDecoder().Decode(raw, off)
		_ = OpName(Op(inst.Op))
		_ = validateInstructionPolicy(inst)
	})
}
