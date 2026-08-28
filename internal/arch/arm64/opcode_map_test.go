package arm64

import (
	"bytes"
	"math/rand"
	"testing"

	"github.com/vmpacker/internal/vm"
)

func TestTranslatorUsesOpcodeMapOnlyAtOpcodePositions(t *testing.T) {
	identity := vm.IdentityOpcodeMap()
	randomized := opcodeMapWithAssignedZero(t)

	zeroOwner, err := randomized.Decode(0)
	if err != nil {
		t.Fatalf("randomized wire zero is unassigned: %v", err)
	}
	if zeroOwner == vm.OpHalt {
		t.Fatal("test map assigns wire zero to HALT")
	}
	haltWire, err := randomized.Wire(vm.OpHalt)
	if err != nil {
		t.Fatalf("HALT wire: %v", err)
	}
	if haltWire == 0 {
		t.Fatal("test map leaves HALT at wire zero")
	}

	decoder := NewDecoder()
	raw := []uint32{
		0xd2800020, // movz x0, #1
		0x14000001, // b +4, to ret
		0xd65f03c0, // ret
	}
	instructions := make([]vm.Instruction, len(raw))
	for i, word := range raw {
		instructions[i] = decoder.Decode(word, i*4)
	}

	translate := func(opcodes vm.OpcodeMap) *TranslateResult {
		t.Helper()
		translator, err := NewTranslator(0x1000, len(raw)*4, opcodes)
		if err != nil {
			t.Fatalf("NewTranslator: %v", err)
		}
		result, err := translator.Translate(instructions)
		if err != nil {
			t.Fatalf("Translate: %v", err)
		}
		if len(result.Unsupported) != 0 {
			t.Fatalf("unsupported instructions: %v", result.Unsupported)
		}
		return result
	}

	identityResult := translate(identity)
	randomResult := translate(randomized)
	if identityResult.CodeLen != randomResult.CodeLen {
		t.Fatalf("code lengths differ: identity=%d randomized=%d", identityResult.CodeLen, randomResult.CodeLen)
	}

	identitySemantic, identityPositions := semanticBytecode(t, identityResult.Bytecode, identityResult.CodeLen, identity)
	randomSemantic, randomPositions := semanticBytecode(t, randomResult.Bytecode, randomResult.CodeLen, randomized)
	if !bytes.Equal(identitySemantic, randomSemantic) {
		t.Fatalf("semantic instructions, operands, or fixups differ:\nidentity % x\nrandom   % x", identitySemantic, randomSemantic)
	}
	if len(identityPositions) != len(randomPositions) {
		t.Fatalf("opcode-position counts differ: identity=%v randomized=%v", identityPositions, randomPositions)
	}
	differentWire := false
	for i, pos := range identityPositions {
		if pos != randomPositions[i] {
			t.Fatalf("opcode positions differ: identity=%v randomized=%v", identityPositions, randomPositions)
		}
		if identityResult.Bytecode[pos] != randomResult.Bytecode[pos] {
			differentWire = true
		}
	}
	if !differentWire {
		t.Fatal("two opcode maps produced identical wire bytes at every opcode position")
	}

	identityTrailer := identityResult.Bytecode[identityResult.CodeLen:]
	randomTrailer := randomResult.Bytecode[randomResult.CodeLen:]
	if !bytes.Equal(identityTrailer, randomTrailer) {
		t.Fatalf("raw trailer bytes were mapped or emitted nondeterministically:\nidentity % x\nrandom   % x", identityTrailer, randomTrailer)
	}
	if len(randomTrailer) < 21 || !bytes.Equal(randomTrailer[len(randomTrailer)-21:len(randomTrailer)-16], make([]byte, 5)) {
		t.Fatalf("raw reverse/key placeholders changed: % x", randomTrailer)
	}
}

func semanticBytecode(t *testing.T, code []byte, codeLen int, opcodes vm.OpcodeMap) ([]byte, []int) {
	t.Helper()
	if codeLen < 0 || codeLen > len(code) {
		t.Fatalf("invalid code length %d for %d-byte buffer", codeLen, len(code))
	}
	normalized := make([]byte, 0, codeLen)
	var opcodePositions []int
	for pc := 0; pc < codeLen; {
		op, err := opcodes.Decode(code[pc])
		if err != nil {
			t.Fatalf("decode wire at %d: %v", pc, err)
		}
		size := vm.InstructionSize(op)
		if size <= 0 || pc+size > codeLen {
			t.Fatalf("invalid semantic instruction %d with size %d at %d", op, size, pc)
		}
		opcodePositions = append(opcodePositions, pc)
		normalized = append(normalized, byte(op))
		normalized = append(normalized, code[pc+1:pc+size]...)
		pc += size
	}
	return normalized, opcodePositions
}

func opcodeMapWithAssignedZero(t *testing.T) vm.OpcodeMap {
	t.Helper()
	for seed := int64(1); seed <= 1024; seed++ {
		opcodes, err := vm.NewOpcodeMap(rand.New(rand.NewSource(seed)))
		if err != nil {
			t.Fatalf("NewOpcodeMap(seed=%d): %v", seed, err)
		}
		zeroOwner, zeroErr := opcodes.Decode(0)
		haltWire, haltErr := opcodes.Wire(vm.OpHalt)
		if zeroErr == nil && haltErr == nil && zeroOwner != vm.OpHalt && haltWire != 0 {
			return opcodes
		}
	}
	t.Fatal("failed to construct deterministic map with non-HALT wire zero owner")
	return vm.OpcodeMap{}
}
