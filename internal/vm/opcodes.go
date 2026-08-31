package vm

import (
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"io"
	"math/big"
	"strings"
)

const (
	RegCount   = 32
	StackSize  = 128
	MaxExtFunc = 16
)

type Opcode uint8

const (
	OpNop Opcode = iota
	OpMovImm
	OpMovImm32
	OpMovReg
	OpLoad8
	OpLoad32
	OpLoad64
	OpStore8
	OpStore32
	OpStore64
	OpLoad16
	OpStore16
	OpAdd
	OpSub
	OpMul
	OpXor
	OpAnd
	OpOr
	OpShl
	OpShr
	OpAsr
	OpUmulh
	OpNot
	OpRor
	OpAddImm
	OpSubImm
	OpXorImm
	OpAndImm
	OpOrImm
	OpMulImm
	OpShlImm
	OpShrImm
	OpAsrImm
	OpCmp
	OpCmpImm
	OpJmp
	OpJe
	OpJne
	OpJl
	OpJge
	OpJgt
	OpJle
	OpJb
	OpJae
	OpJbe
	OpJa
	OpPush
	OpPop
	OpCallNative
	OpCallReg
	OpBrReg
	OpRet
	OpHalt
	OpVld16
	OpVst16
	OpTbz
	OpTbnz
	OpCcmpReg
	OpCcmpImm
	OpCcmnReg
	OpCcmnImm
	OpSvc
	OpUdiv
	OpSdiv
	OpMrs
	OpSmulh
	OpClz
	OpCls
	OpRbit
	OpRev
	OpRev16
	OpRev32
	OpAdc
	OpSbc
	OpSVload
	OpSVstore
	OpSPushImm32
	OpSPushImm64
	OpSDup
	OpSSwap
	OpSDrop
	OpSAdd
	OpSSub
	OpSMul
	OpSXor
	OpSAnd
	OpSOr
	OpSShl
	OpSShr
	OpSAsr
	OpSRor
	OpSUmulh
	OpSSmulh
	OpSUdiv
	OpSSdiv
	OpSAdc
	OpSSbc
	OpSNot
	OpSClz
	OpSCls
	OpSRbit
	OpSRev
	OpSRev16
	OpSRev32
	OpSTrunc32
	OpSSext32
	OpSCmp
	OpSLd8
	OpSLd16
	OpSLd32
	OpSLd64
	OpSSt8
	OpSSt16
	OpSSt32
	OpSSt64
	OpJCond
	OpSAddFlags
	OpSSubFlags
	OpSAndFlags
	OpSAdcFlags
	OpSSbcFlags
	OpCbz
	OpCbnz
	OpMovImage
	OpSPushImage
	OpCallImage
	OpPAuth
	OpBarrier
	OpAtomic
	OpExclusive
	OpFPSIMD
	OpMsr
	OpcodeCount
)

type OpcodeDefinition struct {
	Opcode             Opcode
	Name               string
	Size               int
	CMacro             string
	IdentityWire       byte
	BranchTargetOffset int
}

var opcodeDefinitions = [OpcodeCount]OpcodeDefinition{
	{OpNop, "NOP", 1, "OP_NOP", 0xC3, -1},
	{OpMovImm, "MOV_IMM64", 10, "OP_MOV_IMM", 0x5A, -1},
	{OpMovImm32, "MOV_IMM32", 6, "OP_MOV_IMM32", 0x49, -1},
	{OpMovReg, "MOV_REG", 3, "OP_MOV_REG", 0x2F, -1},
	{OpLoad8, "LOAD8", 5, "OP_LOAD8", 0x91, -1},
	{OpLoad32, "LOAD32", 5, "OP_LOAD32", 0xA4, -1},
	{OpLoad64, "LOAD64", 5, "OP_LOAD64", 0xB7, -1},
	{OpStore8, "STORE8", 5, "OP_STORE8", 0xD2, -1},
	{OpStore32, "STORE32", 5, "OP_STORE32", 0x19, -1},
	{OpStore64, "STORE64", 5, "OP_STORE64", 0x2A, -1},
	{OpLoad16, "LOAD16", 5, "OP_LOAD16", 0xE7, -1},
	{OpStore16, "STORE16", 5, "OP_STORE16", 0xE8, -1},
	{OpAdd, "ADD", 4, "OP_ADD", 0x37, -1},
	{OpSub, "SUB", 4, "OP_SUB", 0x6E, -1},
	{OpMul, "MUL", 4, "OP_MUL", 0x83, -1},
	{OpXor, "XOR", 4, "OP_XOR", 0x1B, -1},
	{OpAnd, "AND", 4, "OP_AND", 0x4D, -1},
	{OpOr, "OR", 4, "OP_OR", 0x72, -1},
	{OpShl, "SHL", 4, "OP_SHL", 0xAE, -1},
	{OpShr, "SHR", 4, "OP_SHR", 0xF1, -1},
	{OpAsr, "ASR", 4, "OP_ASR", 0xDA, -1},
	{OpUmulh, "UMULH", 4, "OP_UMULH", 0xF2, -1},
	{OpNot, "NOT", 3, "OP_NOT", 0x08, -1},
	{OpRor, "ROR", 4, "OP_ROR", 0x3D, -1},
	{OpAddImm, "ADD_IMM", 7, "OP_ADD_IMM", 0xE5, -1},
	{OpSubImm, "SUB_IMM", 7, "OP_SUB_IMM", 0x78, -1},
	{OpXorImm, "XOR_IMM", 7, "OP_XOR_IMM", 0x3C, -1},
	{OpAndImm, "AND_IMM", 7, "OP_AND_IMM", 0xD9, -1},
	{OpOrImm, "OR_IMM", 7, "OP_OR_IMM", 0x6B, -1},
	{OpMulImm, "MUL_IMM", 7, "OP_MUL_IMM", 0xB3, -1},
	{OpShlImm, "SHL_IMM", 7, "OP_SHL_IMM", 0x7A, -1},
	{OpShrImm, "SHR_IMM", 7, "OP_SHR_IMM", 0x8C, -1},
	{OpAsrImm, "ASR_IMM", 7, "OP_ASR_IMM", 0x9D, -1},
	{OpCmp, "CMP", 3, "OP_CMP", 0x9F, -1},
	{OpCmpImm, "CMP_IMM", 6, "OP_CMP_IMM", 0xA1, -1},
	{OpJmp, "JMP", 5, "OP_JMP", 0x44, 1},
	{OpJe, "JE", 5, "OP_JE", 0x58, 1},
	{OpJne, "JNE", 5, "OP_JNE", 0xBB, 1},
	{OpJl, "JL", 5, "OP_JL", 0x15, 1},
	{OpJge, "JGE", 5, "OP_JGE", 0x29, 1},
	{OpJgt, "JGT", 5, "OP_JGT", 0x36, 1},
	{OpJle, "JLE", 5, "OP_JLE", 0x47, 1},
	{OpJb, "JB", 5, "OP_JB", 0x52, 1},
	{OpJae, "JAE", 5, "OP_JAE", 0x64, 1},
	{OpJbe, "JBE", 5, "OP_JBE", 0x53, 1},
	{OpJa, "JA", 5, "OP_JA", 0x65, 1},
	{OpPush, "PUSH", 2, "OP_PUSH", 0x63, -1},
	{OpPop, "POP", 2, "OP_POP", 0x27, -1},
	{OpCallNative, "CALL_NATIVE", 9, "OP_CALL_NAT", 0xAB, -1},
	{OpCallReg, "CALL_REG", 2, "OP_CALL_REG", 0xBC, -1},
	{OpBrReg, "BR_REG", 2, "OP_BR_REG", 0xCD, -1},
	{OpRet, "RET", 2, "OP_RET", 0xEE, -1},
	{OpHalt, "HALT", 1, "OP_HALT", 0x00, -1},
	{OpVld16, "VLD16", 4, "OP_VLD16", 0xC1, -1},
	{OpVst16, "VST16", 4, "OP_VST16", 0xC2, -1},
	{OpTbz, "TBZ", 7, "OP_TBZ", 0x16, 3},
	{OpTbnz, "TBNZ", 7, "OP_TBNZ", 0x17, 3},
	{OpCcmpReg, "CCMP_REG", 6, "OP_CCMP_REG", 0x18, -1},
	{OpCcmpImm, "CCMP_IMM", 6, "OP_CCMP_IMM", 0x1A, -1},
	{OpCcmnReg, "CCMN_REG", 6, "OP_CCMN_REG", 0x1C, -1},
	{OpCcmnImm, "CCMN_IMM", 6, "OP_CCMN_IMM", 0x1D, -1},
	{OpSvc, "SVC", 3, "OP_SVC", 0x1E, -1},
	{OpUdiv, "UDIV", 4, "OP_UDIV", 0x1F, -1},
	{OpSdiv, "SDIV", 4, "OP_SDIV", 0x21, -1},
	{OpMrs, "MRS", 4, "OP_MRS", 0x20, -1},
	{OpSmulh, "SMULH", 4, "OP_SMULH", 0x22, -1},
	{OpClz, "CLZ", 3, "OP_CLZ", 0x23, -1},
	{OpCls, "CLS", 3, "OP_CLS", 0x24, -1},
	{OpRbit, "RBIT", 3, "OP_RBIT", 0x25, -1},
	{OpRev, "REV", 3, "OP_REV", 0x26, -1},
	{OpRev16, "REV16", 3, "OP_REV16", 0x28, -1},
	{OpRev32, "REV32", 3, "OP_REV32", 0x2B, -1},
	{OpAdc, "ADC", 4, "OP_ADC", 0x2C, -1},
	{OpSbc, "SBC", 4, "OP_SBC", 0x2D, -1},
	{OpSVload, "S_VLOAD", 2, "OP_S_VLOAD", 0x01, -1},
	{OpSVstore, "S_VSTORE", 2, "OP_S_VSTORE", 0x02, -1},
	{OpSPushImm32, "S_PUSH32", 5, "OP_S_PUSH_IMM32", 0x03, -1},
	{OpSPushImm64, "S_PUSH64", 9, "OP_S_PUSH_IMM64", 0x04, -1},
	{OpSDup, "S_DUP", 1, "OP_S_DUP", 0x05, -1},
	{OpSSwap, "S_SWAP", 1, "OP_S_SWAP", 0x06, -1},
	{OpSDrop, "S_DROP", 1, "OP_S_DROP", 0x07, -1},
	{OpSAdd, "S_ADD", 1, "OP_S_ADD", 0x09, -1},
	{OpSSub, "S_SUB", 1, "OP_S_SUB", 0x0A, -1},
	{OpSMul, "S_MUL", 1, "OP_S_MUL", 0x0B, -1},
	{OpSXor, "S_XOR", 1, "OP_S_XOR", 0x0C, -1},
	{OpSAnd, "S_AND", 1, "OP_S_AND", 0x0D, -1},
	{OpSOr, "S_OR", 1, "OP_S_OR", 0x0E, -1},
	{OpSShl, "S_SHL", 1, "OP_S_SHL", 0x0F, -1},
	{OpSShr, "S_SHR", 1, "OP_S_SHR", 0x10, -1},
	{OpSAsr, "S_ASR", 1, "OP_S_ASR", 0x11, -1},
	{OpSRor, "S_ROR", 1, "OP_S_ROR", 0x12, -1},
	{OpSUmulh, "S_UMULH", 1, "OP_S_UMULH", 0x13, -1},
	{OpSSmulh, "S_SMULH", 1, "OP_S_SMULH", 0x14, -1},
	{OpSUdiv, "S_UDIV", 1, "OP_S_UDIV", 0x7B, -1},
	{OpSSdiv, "S_SDIV", 1, "OP_S_SDIV", 0x7C, -1},
	{OpSAdc, "S_ADC", 1, "OP_S_ADC", 0x7D, -1},
	{OpSSbc, "S_SBC", 1, "OP_S_SBC", 0x7E, -1},
	{OpSNot, "S_NOT", 1, "OP_S_NOT", 0x7F, -1},
	{OpSClz, "S_CLZ", 1, "OP_S_CLZ", 0x80, -1},
	{OpSCls, "S_CLS", 1, "OP_S_CLS", 0x81, -1},
	{OpSRbit, "S_RBIT", 1, "OP_S_RBIT", 0x82, -1},
	{OpSRev, "S_REV", 1, "OP_S_REV", 0x84, -1},
	{OpSRev16, "S_REV16", 1, "OP_S_REV16", 0x85, -1},
	{OpSRev32, "S_REV32", 1, "OP_S_REV32", 0x86, -1},
	{OpSTrunc32, "S_TRUNC32", 1, "OP_S_TRUNC32", 0x87, -1},
	{OpSSext32, "S_SEXT32", 1, "OP_S_SEXT32", 0x88, -1},
	{OpSCmp, "S_CMP", 1, "OP_S_CMP", 0x89, -1},
	{OpSLd8, "S_LD8", 1, "OP_S_LD8", 0x8A, -1},
	{OpSLd16, "S_LD16", 1, "OP_S_LD16", 0x8B, -1},
	{OpSLd32, "S_LD32", 1, "OP_S_LD32", 0x92, -1},
	{OpSLd64, "S_LD64", 1, "OP_S_LD64", 0x93, -1},
	{OpSSt8, "S_ST8", 1, "OP_S_ST8", 0x94, -1},
	{OpSSt16, "S_ST16", 1, "OP_S_ST16", 0x95, -1},
	{OpSSt32, "S_ST32", 1, "OP_S_ST32", 0x96, -1},
	{OpSSt64, "S_ST64", 1, "OP_S_ST64", 0x97, -1},
	{OpJCond, "JCOND", 6, "OP_JCOND", 0x2E, 2},
	{OpSAddFlags, "S_ADD_FLAGS", 2, "OP_S_ADD_FLAGS", 0x30, -1},
	{OpSSubFlags, "S_SUB_FLAGS", 2, "OP_S_SUB_FLAGS", 0x31, -1},
	{OpSAndFlags, "S_AND_FLAGS", 2, "OP_S_AND_FLAGS", 0x32, -1},
	{OpSAdcFlags, "S_ADC_FLAGS", 2, "OP_S_ADC_FLAGS", 0x33, -1},
	{OpSSbcFlags, "S_SBC_FLAGS", 2, "OP_S_SBC_FLAGS", 0x34, -1},
	{OpCbz, "CBZ", 6, "OP_CBZ", 0x35, 2},
	{OpCbnz, "CBNZ", 6, "OP_CBNZ", 0x38, 2},
	{OpMovImage, "MOV_IMAGE", 10, "OP_MOV_IMAGE", 0x39, -1},
	{OpSPushImage, "S_PUSH_IMAGE", 9, "OP_S_PUSH_IMAGE", 0x3A, -1},
	{OpCallImage, "CALL_IMAGE", 9, "OP_CALL_IMAGE", 0x3B, -1},
	{OpPAuth, "PAUTH", 2, "OP_PAUTH", 0x3E, -1},
	{OpBarrier, "BARRIER", 3, "OP_BARRIER", 0x40, -1},
	{OpAtomic, "ATOMIC", 7, "OP_ATOMIC", 0x41, -1},
	{OpExclusive, "EXCLUSIVE", 5, "OP_EXCLUSIVE", 0x42, -1},
	{OpFPSIMD, "FPSIMD", 5, "OP_FPSIMD", 0x43, -1},
	{OpMsr, "MSR", 4, "OP_MSR", 0x45, -1},
}

func OpcodeDefinitionFor(op Opcode) (OpcodeDefinition, bool) {
	if op >= OpcodeCount {
		return OpcodeDefinition{}, false
	}
	return opcodeDefinitions[op], true
}

type OpcodeMap struct {
	initialized    bool
	semanticToWire [OpcodeCount]byte
	wireToSemantic [256]Opcode
	valid          [256]bool
}

func NewOpcodeMap(r io.Reader) (OpcodeMap, error) {
	if r == nil {
		return OpcodeMap{}, fmt.Errorf("nil opcode randomness reader")
	}

	var pool [256]byte
	for i := range pool {
		pool[i] = byte(i)
	}
	for i := len(pool) - 1; i > 0; i-- {
		j, err := rand.Int(r, big.NewInt(int64(i+1)))
		if err != nil {
			return OpcodeMap{}, fmt.Errorf("shuffle opcode bytes: %w", err)
		}
		pool[i], pool[j.Int64()] = pool[j.Int64()], pool[i]
	}

	var m OpcodeMap
	m.initialized = true
	for op := Opcode(0); op < OpcodeCount; op++ {
		wire := pool[op]
		m.semanticToWire[op] = wire
		m.wireToSemantic[wire] = op
		m.valid[wire] = true
	}
	return m, nil
}

func IdentityOpcodeMap() OpcodeMap {
	var m OpcodeMap
	m.initialized = true
	for _, def := range opcodeDefinitions {
		m.semanticToWire[def.Opcode] = def.IdentityWire
		m.wireToSemantic[def.IdentityWire] = def.Opcode
		m.valid[def.IdentityWire] = true
	}
	return m
}

func (m OpcodeMap) Validate() error {
	if !m.initialized {
		return fmt.Errorf("opcode map is uninitialized")
	}

	seen := [256]bool{}
	for op := Opcode(0); op < OpcodeCount; op++ {
		wire := m.semanticToWire[op]
		if seen[wire] || !m.valid[wire] || m.wireToSemantic[wire] != op {
			return fmt.Errorf("invalid opcode mapping for semantic %d", op)
		}
		seen[wire] = true
	}
	for wire, valid := range m.valid {
		if valid != seen[wire] {
			return fmt.Errorf("invalid opcode mapping for wire 0x%02X", wire)
		}
	}
	return nil
}

func (m OpcodeMap) Wire(op Opcode) (byte, error) {
	if err := m.Validate(); err != nil {
		return 0, err
	}
	if op >= OpcodeCount {
		return 0, fmt.Errorf("invalid semantic opcode %d", op)
	}
	return m.semanticToWire[op], nil
}

func (m OpcodeMap) Decode(wire byte) (Opcode, error) {
	if err := m.Validate(); err != nil {
		return 0, err
	}
	if !m.valid[wire] {
		return 0, fmt.Errorf("unassigned opcode wire 0x%02X", wire)
	}
	return m.wireToSemantic[wire], nil
}

func (m OpcodeMap) Digest() ([sha256.Size]byte, error) {
	if err := m.Validate(); err != nil {
		return [sha256.Size]byte{}, err
	}
	return sha256.Sum256(m.semanticToWire[:]), nil
}

func (m OpcodeMap) CHeader() (string, error) {
	if err := m.Validate(); err != nil {
		return "", err
	}

	var b strings.Builder
	b.WriteString("#ifndef VMPACKER_VM_OPCODE_MAP_H\n#define VMPACKER_VM_OPCODE_MAP_H\n\n")
	for _, def := range opcodeDefinitions {
		fmt.Fprintf(&b, "#define %s 0x%02X\n", def.CMacro, m.semanticToWire[def.Opcode])
	}
	b.WriteString("\n#endif /* VMPACKER_VM_OPCODE_MAP_H */\n")
	return b.String(), nil
}

const (
	FlagZero  uint32 = 1 << 0
	FlagSign  uint32 = 1 << 1
	FlagCarry uint32 = 1 << 2
)
