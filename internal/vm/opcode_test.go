package vm

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"testing"
)

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, errors.New("reader failed") }

func deterministicInput(seed byte) []byte {
	b := make([]byte, 8192)
	for i := range b {
		b[i] = byte(i*31) ^ seed
	}
	return b
}

type opcodeGolden struct {
	semantic           Opcode
	wire               byte
	name               string
	size               int
	macro              string
	branchTargetOffset int
}

var opcodeGoldenTable = [131]opcodeGolden{
	{0, 0xC3, "NOP", 1, "OP_NOP", -1},
	{1, 0x5A, "MOV_IMM64", 10, "OP_MOV_IMM", -1},
	{2, 0x49, "MOV_IMM32", 6, "OP_MOV_IMM32", -1},
	{3, 0x2F, "MOV_REG", 3, "OP_MOV_REG", -1},
	{4, 0x91, "LOAD8", 5, "OP_LOAD8", -1},
	{5, 0xA4, "LOAD32", 5, "OP_LOAD32", -1},
	{6, 0xB7, "LOAD64", 5, "OP_LOAD64", -1},
	{7, 0xD2, "STORE8", 5, "OP_STORE8", -1},
	{8, 0x19, "STORE32", 5, "OP_STORE32", -1},
	{9, 0x2A, "STORE64", 5, "OP_STORE64", -1},
	{10, 0xE7, "LOAD16", 5, "OP_LOAD16", -1},
	{11, 0xE8, "STORE16", 5, "OP_STORE16", -1},
	{12, 0x37, "ADD", 4, "OP_ADD", -1},
	{13, 0x6E, "SUB", 4, "OP_SUB", -1},
	{14, 0x83, "MUL", 4, "OP_MUL", -1},
	{15, 0x1B, "XOR", 4, "OP_XOR", -1},
	{16, 0x4D, "AND", 4, "OP_AND", -1},
	{17, 0x72, "OR", 4, "OP_OR", -1},
	{18, 0xAE, "SHL", 4, "OP_SHL", -1},
	{19, 0xF1, "SHR", 4, "OP_SHR", -1},
	{20, 0xDA, "ASR", 4, "OP_ASR", -1},
	{21, 0xF2, "UMULH", 4, "OP_UMULH", -1},
	{22, 0x08, "NOT", 3, "OP_NOT", -1},
	{23, 0x3D, "ROR", 4, "OP_ROR", -1},
	{24, 0xE5, "ADD_IMM", 7, "OP_ADD_IMM", -1},
	{25, 0x78, "SUB_IMM", 7, "OP_SUB_IMM", -1},
	{26, 0x3C, "XOR_IMM", 7, "OP_XOR_IMM", -1},
	{27, 0xD9, "AND_IMM", 7, "OP_AND_IMM", -1},
	{28, 0x6B, "OR_IMM", 7, "OP_OR_IMM", -1},
	{29, 0xB3, "MUL_IMM", 7, "OP_MUL_IMM", -1},
	{30, 0x7A, "SHL_IMM", 7, "OP_SHL_IMM", -1},
	{31, 0x8C, "SHR_IMM", 7, "OP_SHR_IMM", -1},
	{32, 0x9D, "ASR_IMM", 7, "OP_ASR_IMM", -1},
	{33, 0x9F, "CMP", 3, "OP_CMP", -1},
	{34, 0xA1, "CMP_IMM", 6, "OP_CMP_IMM", -1},
	{35, 0x44, "JMP", 5, "OP_JMP", 1},
	{36, 0x58, "JE", 5, "OP_JE", 1},
	{37, 0xBB, "JNE", 5, "OP_JNE", 1},
	{38, 0x15, "JL", 5, "OP_JL", 1},
	{39, 0x29, "JGE", 5, "OP_JGE", 1},
	{40, 0x36, "JGT", 5, "OP_JGT", 1},
	{41, 0x47, "JLE", 5, "OP_JLE", 1},
	{42, 0x52, "JB", 5, "OP_JB", 1},
	{43, 0x64, "JAE", 5, "OP_JAE", 1},
	{44, 0x53, "JBE", 5, "OP_JBE", 1},
	{45, 0x65, "JA", 5, "OP_JA", 1},
	{46, 0x63, "PUSH", 2, "OP_PUSH", -1},
	{47, 0x27, "POP", 2, "OP_POP", -1},
	{48, 0xAB, "CALL_NATIVE", 9, "OP_CALL_NAT", -1},
	{49, 0xBC, "CALL_REG", 2, "OP_CALL_REG", -1},
	{50, 0xCD, "BR_REG", 2, "OP_BR_REG", -1},
	{51, 0xEE, "RET", 2, "OP_RET", -1},
	{52, 0x00, "HALT", 1, "OP_HALT", -1},
	{53, 0xC1, "VLD16", 4, "OP_VLD16", -1},
	{54, 0xC2, "VST16", 4, "OP_VST16", -1},
	{55, 0x16, "TBZ", 7, "OP_TBZ", 3},
	{56, 0x17, "TBNZ", 7, "OP_TBNZ", 3},
	{57, 0x18, "CCMP_REG", 6, "OP_CCMP_REG", -1},
	{58, 0x1A, "CCMP_IMM", 6, "OP_CCMP_IMM", -1},
	{59, 0x1C, "CCMN_REG", 6, "OP_CCMN_REG", -1},
	{60, 0x1D, "CCMN_IMM", 6, "OP_CCMN_IMM", -1},
	{61, 0x1E, "SVC", 3, "OP_SVC", -1},
	{62, 0x1F, "UDIV", 4, "OP_UDIV", -1},
	{63, 0x21, "SDIV", 4, "OP_SDIV", -1},
	{64, 0x20, "MRS", 4, "OP_MRS", -1},
	{65, 0x22, "SMULH", 4, "OP_SMULH", -1},
	{66, 0x23, "CLZ", 3, "OP_CLZ", -1},
	{67, 0x24, "CLS", 3, "OP_CLS", -1},
	{68, 0x25, "RBIT", 3, "OP_RBIT", -1},
	{69, 0x26, "REV", 3, "OP_REV", -1},
	{70, 0x28, "REV16", 3, "OP_REV16", -1},
	{71, 0x2B, "REV32", 3, "OP_REV32", -1},
	{72, 0x2C, "ADC", 4, "OP_ADC", -1},
	{73, 0x2D, "SBC", 4, "OP_SBC", -1},
	{74, 0x01, "S_VLOAD", 2, "OP_S_VLOAD", -1},
	{75, 0x02, "S_VSTORE", 2, "OP_S_VSTORE", -1},
	{76, 0x03, "S_PUSH32", 5, "OP_S_PUSH_IMM32", -1},
	{77, 0x04, "S_PUSH64", 9, "OP_S_PUSH_IMM64", -1},
	{78, 0x05, "S_DUP", 1, "OP_S_DUP", -1},
	{79, 0x06, "S_SWAP", 1, "OP_S_SWAP", -1},
	{80, 0x07, "S_DROP", 1, "OP_S_DROP", -1},
	{81, 0x09, "S_ADD", 1, "OP_S_ADD", -1},
	{82, 0x0A, "S_SUB", 1, "OP_S_SUB", -1},
	{83, 0x0B, "S_MUL", 1, "OP_S_MUL", -1},
	{84, 0x0C, "S_XOR", 1, "OP_S_XOR", -1},
	{85, 0x0D, "S_AND", 1, "OP_S_AND", -1},
	{86, 0x0E, "S_OR", 1, "OP_S_OR", -1},
	{87, 0x0F, "S_SHL", 1, "OP_S_SHL", -1},
	{88, 0x10, "S_SHR", 1, "OP_S_SHR", -1},
	{89, 0x11, "S_ASR", 1, "OP_S_ASR", -1},
	{90, 0x12, "S_ROR", 1, "OP_S_ROR", -1},
	{91, 0x13, "S_UMULH", 1, "OP_S_UMULH", -1},
	{92, 0x14, "S_SMULH", 1, "OP_S_SMULH", -1},
	{93, 0x7B, "S_UDIV", 1, "OP_S_UDIV", -1},
	{94, 0x7C, "S_SDIV", 1, "OP_S_SDIV", -1},
	{95, 0x7D, "S_ADC", 1, "OP_S_ADC", -1},
	{96, 0x7E, "S_SBC", 1, "OP_S_SBC", -1},
	{97, 0x7F, "S_NOT", 1, "OP_S_NOT", -1},
	{98, 0x80, "S_CLZ", 1, "OP_S_CLZ", -1},
	{99, 0x81, "S_CLS", 1, "OP_S_CLS", -1},
	{100, 0x82, "S_RBIT", 1, "OP_S_RBIT", -1},
	{101, 0x84, "S_REV", 1, "OP_S_REV", -1},
	{102, 0x85, "S_REV16", 1, "OP_S_REV16", -1},
	{103, 0x86, "S_REV32", 1, "OP_S_REV32", -1},
	{104, 0x87, "S_TRUNC32", 1, "OP_S_TRUNC32", -1},
	{105, 0x88, "S_SEXT32", 1, "OP_S_SEXT32", -1},
	{106, 0x89, "S_CMP", 1, "OP_S_CMP", -1},
	{107, 0x8A, "S_LD8", 1, "OP_S_LD8", -1},
	{108, 0x8B, "S_LD16", 1, "OP_S_LD16", -1},
	{109, 0x92, "S_LD32", 1, "OP_S_LD32", -1},
	{110, 0x93, "S_LD64", 1, "OP_S_LD64", -1},
	{111, 0x94, "S_ST8", 1, "OP_S_ST8", -1},
	{112, 0x95, "S_ST16", 1, "OP_S_ST16", -1},
	{113, 0x96, "S_ST32", 1, "OP_S_ST32", -1},
	{114, 0x97, "S_ST64", 1, "OP_S_ST64", -1},
	{115, 0x2E, "JCOND", 6, "OP_JCOND", 2},
	{116, 0x30, "S_ADD_FLAGS", 2, "OP_S_ADD_FLAGS", -1},
	{117, 0x31, "S_SUB_FLAGS", 2, "OP_S_SUB_FLAGS", -1},
	{118, 0x32, "S_AND_FLAGS", 2, "OP_S_AND_FLAGS", -1},
	{119, 0x33, "S_ADC_FLAGS", 2, "OP_S_ADC_FLAGS", -1},
	{120, 0x34, "S_SBC_FLAGS", 2, "OP_S_SBC_FLAGS", -1},
	{121, 0x35, "CBZ", 6, "OP_CBZ", 2},
	{122, 0x38, "CBNZ", 6, "OP_CBNZ", 2},
	{123, 0x39, "MOV_IMAGE", 10, "OP_MOV_IMAGE", -1},
	{124, 0x3A, "S_PUSH_IMAGE", 9, "OP_S_PUSH_IMAGE", -1},
	{125, 0x3B, "CALL_IMAGE", 9, "OP_CALL_IMAGE", -1},
	{126, 0x3E, "PAUTH", 2, "OP_PAUTH", -1},
	{127, 0x40, "BARRIER", 3, "OP_BARRIER", -1},
	{128, 0x41, "ATOMIC", 7, "OP_ATOMIC", -1},
	{129, 0x42, "EXCLUSIVE", 5, "OP_EXCLUSIVE", -1},
	{130, 0x43, "FPSIMD", 5, "OP_FPSIMD", -1},
}

func TestOpcodeMetadataMatchesLegacyGoldenTable(t *testing.T) {
	if len(opcodeGoldenTable) != 131 {
		t.Fatalf("golden table has %d entries, want 131", len(opcodeGoldenTable))
	}
	if OpcodeCount != Opcode(len(opcodeGoldenTable)) {
		t.Fatalf("OpcodeCount = %d, want %d golden entries", OpcodeCount, len(opcodeGoldenTable))
	}

	identity := IdentityOpcodeMap()
	header, err := identity.CHeader()
	if err != nil {
		t.Fatal(err)
	}
	var macroLines []string
	for _, line := range strings.Split(header, "\n") {
		if strings.HasPrefix(line, "#define OP_") {
			macroLines = append(macroLines, line)
		}
	}
	if len(macroLines) != len(opcodeGoldenTable) {
		t.Fatalf("generated header has %d opcode macros, want %d", len(macroLines), len(opcodeGoldenTable))
	}

	semantics := map[Opcode]bool{}
	wires := map[byte]bool{}
	names := map[string]bool{}
	macros := map[string]bool{}
	for i, want := range opcodeGoldenTable {
		if want.semantic != Opcode(i) {
			t.Errorf("golden entry %d has semantic %d", i, want.semantic)
		}
		if semantics[want.semantic] {
			t.Errorf("duplicate golden semantic %d", want.semantic)
		}
		if wires[want.wire] {
			t.Errorf("duplicate golden wire 0x%02X", want.wire)
		}
		if names[want.name] {
			t.Errorf("duplicate golden name %q", want.name)
		}
		if macros[want.macro] {
			t.Errorf("duplicate golden macro %q", want.macro)
		}
		semantics[want.semantic] = true
		wires[want.wire] = true
		names[want.name] = true
		macros[want.macro] = true

		def, ok := OpcodeDefinitionFor(want.semantic)
		if !ok {
			t.Errorf("definition %d missing", want.semantic)
			continue
		}
		if def.Opcode != want.semantic || def.IdentityWire != want.wire || def.Name != want.name || def.Size != want.size || def.CMacro != want.macro || def.BranchTargetOffset != want.branchTargetOffset {
			t.Errorf("definition %d = %+v, want %+v", want.semantic, def, want)
		}
		wire, err := identity.Wire(want.semantic)
		if err != nil {
			t.Errorf("identity wire %d: %v", want.semantic, err)
		} else if wire != want.wire {
			t.Errorf("identity wire %d = 0x%02X, want 0x%02X", want.semantic, wire, want.wire)
		}
		if got := OpcodeName(want.semantic); got != want.name {
			t.Errorf("OpcodeName(%d) = %q, want %q", want.semantic, got, want.name)
		}
		if got := InstructionSize(want.semantic); got != want.size {
			t.Errorf("InstructionSize(%d) = %d, want %d", want.semantic, got, want.size)
		}
		if got := BranchTargetOffset(want.semantic); got != want.branchTargetOffset {
			t.Errorf("BranchTargetOffset(%d) = %d, want %d", want.semantic, got, want.branchTargetOffset)
		}
		if got, wantLine := macroLines[i], fmt.Sprintf("#define %s 0x%02X", want.macro, want.wire); got != wantLine {
			t.Errorf("CHeader line %d = %q, want %q", i, got, wantLine)
		}
	}
}

func TestOpcodeDefinitions(t *testing.T) {
	if OpcodeCount != 131 || len(opcodeDefinitions) != 131 {
		t.Fatalf("got %d constants and %d definitions, want 131", OpcodeCount, len(opcodeDefinitions))
	}

	semantics := map[Opcode]bool{}
	names := map[string]bool{}
	macros := map[string]bool{}
	wires := map[byte]bool{}
	for op := Opcode(0); op < OpcodeCount; op++ {
		def, ok := OpcodeDefinitionFor(op)
		if !ok {
			t.Fatalf("definition %d missing", op)
		}
		if def.Opcode != op {
			t.Errorf("definition %d has semantic %d", op, def.Opcode)
		}
		if semantics[def.Opcode] || names[def.Name] || macros[def.CMacro] || wires[def.IdentityWire] {
			t.Errorf("definition %d is not unique: %+v", op, def)
		}
		semantics[def.Opcode] = true
		names[def.Name] = true
		macros[def.CMacro] = true
		wires[def.IdentityWire] = true
		if def.Size < 1 || def.Size > 10 {
			t.Errorf("%s has invalid size %d", def.Name, def.Size)
		}
		if def.BranchTargetOffset != -1 && def.BranchTargetOffset+4 > def.Size {
			t.Errorf("%s has invalid branch target offset %d", def.Name, def.BranchTargetOffset)
		}
		if InstructionSize(op) != def.Size || OpcodeName(op) != def.Name || BranchTargetOffset(op) != def.BranchTargetOffset {
			t.Errorf("metadata accessors disagree for %s", def.Name)
		}
	}
	if _, ok := OpcodeDefinitionFor(OpcodeCount); ok {
		t.Fatal("out-of-range definition accepted")
	}
}

func TestIdentityOpcodeMap(t *testing.T) {
	m := IdentityOpcodeMap()
	if err := m.Validate(); err != nil {
		t.Fatal(err)
	}
	wantDigest := sha256.Sum256(m.semanticToWire[:])
	gotDigest, err := m.Digest()
	if err != nil || gotDigest != wantDigest {
		t.Fatalf("identity digest = %x, %v; want %x", gotDigest, err, wantDigest)
	}
	for _, def := range opcodeDefinitions {
		wire, err := m.Wire(def.Opcode)
		if err != nil {
			t.Fatal(err)
		}
		if wire != def.IdentityWire {
			t.Errorf("%s wire = 0x%02X, want 0x%02X", def.Name, wire, def.IdentityWire)
		}
		op, err := m.Decode(def.IdentityWire)
		if err != nil || op != def.Opcode {
			t.Errorf("wire 0x%02X decoded as %d, %v", def.IdentityWire, op, err)
		}
	}
}

func TestRandomOpcodeMapRoundTrip(t *testing.T) {
	m, err := NewOpcodeMap(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Validate(); err != nil {
		t.Fatal(err)
	}

	seen := map[byte]bool{}
	for op := Opcode(0); op < OpcodeCount; op++ {
		wire, err := m.Wire(op)
		if err != nil {
			t.Fatal(err)
		}
		if seen[wire] {
			t.Fatalf("wire 0x%02X assigned twice", wire)
		}
		seen[wire] = true
		got, err := m.Decode(wire)
		if err != nil || got != op {
			t.Fatalf("roundtrip %d through 0x%02X = %d, %v", op, wire, got, err)
		}
	}
}

func TestNewOpcodeMapDeterminismAndFailures(t *testing.T) {
	inputA := deterministicInput(0x11)
	inputB := deterministicInput(0xA7)
	first, err := NewOpcodeMap(bytes.NewReader(inputA))
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewOpcodeMap(bytes.NewReader(inputA))
	if err != nil {
		t.Fatal(err)
	}
	other, err := NewOpcodeMap(bytes.NewReader(inputB))
	if err != nil {
		t.Fatal(err)
	}
	firstDigest, err := first.Digest()
	if err != nil {
		t.Fatal(err)
	}
	secondDigest, _ := second.Digest()
	otherDigest, _ := other.Digest()
	if firstDigest != secondDigest {
		t.Fatal("same reader input produced different maps")
	}
	if firstDigest == otherDigest {
		t.Fatal("different reader input produced the same map")
	}

	if _, err := NewOpcodeMap(nil); err == nil {
		t.Fatal("nil reader accepted")
	}
	if _, err := NewOpcodeMap(bytes.NewReader([]byte{0})); err == nil {
		t.Fatal("short reader accepted")
	}
	if _, err := NewOpcodeMap(failingReader{}); err == nil {
		t.Fatal("erroring reader accepted")
	}

	var zero OpcodeMap
	checks := []func() error{
		zero.Validate,
		func() error { _, err := zero.Wire(OpNop); return err },
		func() error { _, err := zero.Decode(0); return err },
		func() error { _, err := zero.Digest(); return err },
		func() error { _, err := zero.CHeader(); return err },
		func() error { _, _, err := DisasmOne([]byte{0}, 0, zero); return err },
	}
	for i, check := range checks {
		if err := check(); err == nil {
			t.Errorf("zero map check %d succeeded", i)
		}
	}
	if _, err := first.Wire(OpcodeCount); err == nil {
		t.Fatal("out-of-range semantic opcode accepted")
	}
}

func TestOpcodeMapCHeader(t *testing.T) {
	header, err := IdentityOpcodeMap().CHeader()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(header, "#ifndef VMPACKER_VM_OPCODE_MAP_H") || !strings.Contains(header, "#endif /* VMPACKER_VM_OPCODE_MAP_H */") {
		t.Fatal("header guard missing")
	}

	defines := map[string]bool{}
	for _, line := range strings.Split(header, "\n") {
		if !strings.HasPrefix(line, "#define OP_") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 3 || defines[fields[1]] {
			t.Fatalf("invalid or duplicate define %q", line)
		}
		defines[fields[1]] = true
	}
	if len(defines) != 131 {
		t.Fatalf("got %d OP_* defines, want 131", len(defines))
	}
	for _, def := range opcodeDefinitions {
		if !defines[def.CMacro] {
			t.Errorf("missing define %s", def.CMacro)
		}
	}
}

func TestDisassemblyUsesOpcodeMap(t *testing.T) {
	identity := IdentityOpcodeMap()
	text, size, err := DisasmOne([]byte{0}, 0, identity)
	if err != nil || text != "0000: HALT" || size != 1 {
		t.Fatalf("identity HALT = %q, %d, %v", text, size, err)
	}
	if _, _, err := DisasmOne([]byte{0xFF}, 0, identity); err == nil {
		t.Fatal("unassigned wire accepted")
	}

	swapped := identity
	swapped.semanticToWire[OpHalt], swapped.semanticToWire[OpNop] = swapped.semanticToWire[OpNop], swapped.semanticToWire[OpHalt]
	swapped.wireToSemantic[0], swapped.wireToSemantic[0xC3] = OpNop, OpHalt
	if err := swapped.Validate(); err != nil {
		t.Fatal(err)
	}
	text, size, err = DisasmOne([]byte{0}, 0, swapped)
	if err != nil || text != "0000: NOP" || size != 1 {
		t.Fatalf("wire zero under swapped map = %q, %d, %v", text, size, err)
	}
	text, size, err = DisasmOne([]byte{0xC3}, 0, swapped)
	if err != nil || text != "0000: HALT" || size != 1 {
		t.Fatalf("relocated HALT = %q, %d, %v", text, size, err)
	}
}

func TestWire255Supported(t *testing.T) {
	m := IdentityOpcodeMap()
	old := m.semanticToWire[OpNop]
	m.semanticToWire[OpNop] = 255
	m.wireToSemantic[255] = OpNop
	m.valid[255] = true
	m.valid[old] = false
	if err := m.Validate(); err != nil {
		t.Fatal(err)
	}
	wire, err := m.Wire(OpNop)
	if err != nil || wire != 255 {
		t.Fatalf("NOP wire = %d, %v", wire, err)
	}
	op, err := m.Decode(255)
	if err != nil || op != OpNop {
		t.Fatalf("wire 255 decoded as %d, %v", op, err)
	}
	text, size, err := DisasmOne([]byte{255}, 0, m)
	if err != nil || text != "0000: NOP" || size != 1 {
		t.Fatalf("wire 255 disassembly = %q, %d, %v", text, size, err)
	}
	if _, err := m.Decode(old); err == nil {
		t.Fatal("replaced wire remained assigned")
	}
}
