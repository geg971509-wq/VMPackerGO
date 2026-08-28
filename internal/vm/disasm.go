package vm

import (
	"encoding/binary"
	"fmt"
)

func InstructionSize(op Opcode) int {
	if def, ok := OpcodeDefinitionFor(op); ok {
		return def.Size
	}
	return 0
}

func OpcodeName(op Opcode) string {
	if def, ok := OpcodeDefinitionFor(op); ok {
		return def.Name
	}
	return fmt.Sprintf("UNKNOWN(0x%02X)", op)
}

func BranchTargetOffset(op Opcode) int {
	if def, ok := OpcodeDefinitionFor(op); ok {
		return def.BranchTargetOffset
	}
	return -1
}

func DisasmOne(code []byte, pc int, opcodeMap OpcodeMap) (string, int, error) {
	if err := opcodeMap.Validate(); err != nil {
		return "", 0, err
	}
	return disasmOne(code, pc, opcodeMap)
}

func disasmOne(code []byte, pc int, opcodeMap OpcodeMap) (string, int, error) {
	if pc < 0 {
		return "", 0, fmt.Errorf("negative program counter %d", pc)
	}
	if pc >= len(code) {
		return "EOF", 0, nil
	}

	wire := code[pc]
	op, err := opcodeMap.Decode(wire)
	if err != nil {
		return "", 0, err
	}
	def := opcodeDefinitions[op]
	remain := len(code) - pc
	if def.Size > remain {
		return fmt.Sprintf("%04X: %s (truncated)", pc, def.Name), remain, nil
	}

	switch op {
	case OpNop:
		return fmt.Sprintf("%04X: NOP", pc), 1, nil
	case OpHalt:
		return fmt.Sprintf("%04X: HALT", pc), 1, nil

	case OpMovImm:
		r := code[pc+1]
		v := binary.LittleEndian.Uint64(code[pc+2:])
		return fmt.Sprintf("%04X: MOV R%d, 0x%X", pc, r, v), 10, nil

	case OpMovImm32:
		r := code[pc+1]
		v := binary.LittleEndian.Uint32(code[pc+2:])
		return fmt.Sprintf("%04X: MOV32 R%d, 0x%X", pc, r, v), 6, nil

	case OpMovReg:
		return fmt.Sprintf("%04X: MOV R%d, R%d", pc, code[pc+1], code[pc+2]), 3, nil

	case OpLoad8, OpLoad16, OpLoad32, OpLoad64:
		dst := code[pc+1]
		base := code[pc+2]
		imm := binary.LittleEndian.Uint16(code[pc+3:])
		width := map[Opcode]string{OpLoad8: "8", OpLoad16: "16", OpLoad32: "32", OpLoad64: "64"}[op]
		return fmt.Sprintf("%04X: LOAD%s R%d, [R%d + %d]", pc, width, dst, base, imm), 5, nil

	case OpStore8, OpStore16, OpStore32, OpStore64:
		base := code[pc+1]
		src := code[pc+2]
		imm := binary.LittleEndian.Uint16(code[pc+3:])
		width := map[Opcode]string{OpStore8: "8", OpStore16: "16", OpStore32: "32", OpStore64: "64"}[op]
		return fmt.Sprintf("%04X: STORE%s [R%d + %d], R%d", pc, width, base, imm, src), 5, nil

	case OpAdd, OpSub, OpMul, OpXor, OpAnd, OpOr, OpShl, OpShr, OpAsr, OpRor, OpUmulh, OpUdiv, OpSdiv, OpSmulh, OpAdc, OpSbc:
		return fmt.Sprintf("%04X: %s R%d, R%d, R%d", pc, def.Name, code[pc+1], code[pc+2], code[pc+3]), 4, nil

	case OpNot, OpClz, OpCls, OpRbit, OpRev, OpRev16, OpRev32:
		return fmt.Sprintf("%04X: %s R%d, R%d", pc, def.Name, code[pc+1], code[pc+2]), 3, nil

	case OpAddImm, OpSubImm, OpXorImm, OpAndImm, OpOrImm, OpMulImm, OpShlImm, OpShrImm, OpAsrImm:
		d := code[pc+1]
		s := code[pc+2]
		imm := binary.LittleEndian.Uint32(code[pc+3:])
		return fmt.Sprintf("%04X: %s R%d, R%d, 0x%X", pc, def.Name, d, s, imm), 7, nil

	case OpCmp:
		return fmt.Sprintf("%04X: CMP R%d, R%d", pc, code[pc+1], code[pc+2]), 3, nil

	case OpCmpImm:
		r := code[pc+1]
		imm := binary.LittleEndian.Uint32(code[pc+2:])
		return fmt.Sprintf("%04X: CMP R%d, 0x%X", pc, r, imm), 6, nil

	case OpJmp, OpJe, OpJne, OpJl, OpJge, OpJgt, OpJle, OpJb, OpJae, OpJbe, OpJa:
		target := binary.LittleEndian.Uint32(code[pc+1:])
		return fmt.Sprintf("%04X: %s 0x%04X", pc, def.Name, target), 5, nil

	case OpPush:
		return fmt.Sprintf("%04X: PUSH R%d", pc, code[pc+1]), 2, nil
	case OpPop:
		return fmt.Sprintf("%04X: POP R%d", pc, code[pc+1]), 2, nil

	case OpCallNative:
		target := binary.LittleEndian.Uint64(code[pc+1:])
		return fmt.Sprintf("%04X: CALL 0x%X", pc, target), 9, nil

	case OpCallReg:
		return fmt.Sprintf("%04X: BLR R%d", pc, code[pc+1]), 2, nil

	case OpBrReg:
		return fmt.Sprintf("%04X: BR R%d", pc, code[pc+1]), 2, nil

	case OpRet:
		return fmt.Sprintf("%04X: RET R%d", pc, code[pc+1]), 2, nil

	case OpVld16:
		return fmt.Sprintf("%04X: VLD16 R%d, %d", pc, code[pc+1], code[pc+2]), 3, nil
	case OpVst16:
		return fmt.Sprintf("%04X: VST16 R%d, %d", pc, code[pc+1], code[pc+2]), 3, nil

	case OpTbz, OpTbnz:
		reg := code[pc+1]
		bit := code[pc+2]
		target := binary.LittleEndian.Uint32(code[pc+3:])
		return fmt.Sprintf("%04X: %s R%d, #%d, 0x%04X", pc, def.Name, reg, bit, target), 7, nil

	case OpCcmpReg, OpCcmpImm, OpCcmnReg, OpCcmnImm:
		cond := code[pc+1]
		nzcv := code[pc+2]
		rn := code[pc+3]
		rmOrImm := code[pc+4]
		sf := code[pc+5]
		if op == OpCcmpImm || op == OpCcmnImm {
			return fmt.Sprintf("%04X: %s R%d, #%d, #%d, cond=%d sf=%d", pc, def.Name, rn, rmOrImm, nzcv, cond, sf), 6, nil
		}
		return fmt.Sprintf("%04X: %s R%d, R%d, #%d, cond=%d sf=%d", pc, def.Name, rn, rmOrImm, nzcv, cond, sf), 6, nil

	case OpSvc:
		imm := binary.LittleEndian.Uint16(code[pc+1:])
		return fmt.Sprintf("%04X: SVC #0x%X", pc, imm), 3, nil

	case OpMrs:
		dst := code[pc+1]
		sysreg := binary.LittleEndian.Uint16(code[pc+2:])
		return fmt.Sprintf("%04X: MRS R%d, sysreg=0x%04X", pc, dst, sysreg), 4, nil

	default:
		return fmt.Sprintf("%04X: %s", pc, def.Name), def.Size, nil
	}
}

func DisasmRange(code []byte, start, end int, opcodeMap OpcodeMap) ([]string, error) {
	if err := opcodeMap.Validate(); err != nil {
		return nil, err
	}

	var lines []string
	pc := start
	for pc < end && pc < len(code) {
		text, size, err := disasmOne(code, pc, opcodeMap)
		if err != nil {
			return nil, err
		}
		if size == 0 {
			break
		}
		lines = append(lines, text)
		pc += size
	}
	return lines, nil
}

func DisasmAll(code []byte, opcodeMap OpcodeMap) ([]string, error) {
	return DisasmRange(code, 0, len(code), opcodeMap)
}
