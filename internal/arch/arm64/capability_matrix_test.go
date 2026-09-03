package arm64

import "testing"

func TestEveryDecoderOpcodeHasExplicitProductDisposition(t *testing.T) {
	for op := UNKNOWN; op <= UNSUPPORTED; op++ {
		rule, ok := instructionRules[op]
		if !ok {
			t.Errorf("ARM64 opcode %s (%d) has no explicit product policy", OpName(op), op)
			continue
		}
		switch rule.disposition {
		case dispositionVirtual, dispositionNativeThunk, dispositionReject:
		default:
			t.Errorf("ARM64 opcode %s (%d) has invalid disposition %d", OpName(op), op, rule.disposition)
		}
	}
}

func TestArchitecturalSentinelsRemainExplicitRejects(t *testing.T) {
	for _, op := range []Op{UNKNOWN, HLT, BRK, UNSUPPORTED} {
		rule, ok := instructionRules[op]
		if !ok || rule.disposition != dispositionReject {
			t.Errorf("ARM64 opcode %s must remain an explicit reject", OpName(op))
		}
	}
}
