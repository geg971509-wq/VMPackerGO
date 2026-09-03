package arm64

import "testing"

func TestRealSignedOffsetPairsDoNotBecomeWriteback(t *testing.T) {
	decoder := NewDecoder()
	cases := []struct {
		name string
		raw  uint32
		op   Op
	}{
		{"stp-0004", 0xA9016FFC, STP},
		{"stp-000c", 0xA90267FA, STP},
		{"stp-0010", 0xA9035FF8, STP},
		{"stp-0014", 0xA90457F6, STP},
		{"stp-0018", 0xA9054FF4, STP},
		{"stp-0034", 0xA93E27BF, STP},
		{"stp-0074", 0xA9037FFF, STP},
		{"stp-0078", 0xA901FFFF, STP},
		{"stp-007c", 0xA900FFFF, STP},
		{"stp-0088", 0xA9047FFF, STP},
		{"stp-02bc", 0xA902FFFF, STP},
		{"ldp-02e8", 0xA941EFF7, LDP},
		{"ldp-02ec", 0xA94073FA, LDP},
		{"ldp-0368", 0xA97DCFA1, LDP},
		{"ldp-0380", 0xA9454FF4, LDP},
		{"ldp-0384", 0xA94457F6, LDP},
		{"ldp-0388", 0xA9435FF8, LDP},
		{"ldp-038c", 0xA94267FA, LDP},
		{"ldp-0390", 0xA9416FFC, LDP},
		{"stp-03c0", 0xA93BA3BF, STP},
		{"stp-03cc", 0xA93CFFA8, STP},
		{"stp-04ac", 0xA93CFFBC, STP},
		{"stp-04b4", 0xA93BA3BF, STP},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			inst := decoder.Decode(tc.raw, 0)
			if got := Op(inst.Op); got != tc.op {
				t.Fatalf("raw=%08x decoded as %s, want %s", tc.raw, OpName(got), OpName(tc.op))
			}
			if inst.WB != 2 {
				t.Fatalf("raw=%08x WB=%d, want signed-offset mode 2", tc.raw, inst.WB)
			}
			if err := validateInstructionPolicy(inst); err != nil {
				t.Fatalf("raw=%08x signed-offset pair rejected: %v", tc.raw, err)
			}
		})
	}
}
