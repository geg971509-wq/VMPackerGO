package elf

import (
	"bytes"
	"encoding/binary"
	"io"
)

// ============================================================
// ARM64 跳板代码生成 + ELF64 二进制结构读写
// ============================================================

// BuildTrampoline 构造 ARM64 跳板代码（动态参数传递）
//
//	MOV X9, X29                  ; 暂存 caller FP
//	MOV X10, X30                 ; 暂存 caller LR
//	STP X29, X30, [SP, #-96]!   ; 保存 FP/LR + 分配 96B 栈帧
//	MOV X29, SP                  ; 建立栈帧
//	STP X0, X1, [SP, #16]       ; args[0..1]
//	STP X2, X3, [SP, #32]       ; args[2..3]
//	STP X4, X5, [SP, #48]       ; args[4..5]
//	STP X6, X7, [SP, #64]       ; args[6..7]
//	STP X9, X10, [SP, #80]      ; args[8]=callerFP, args[9]=callerLR
//	ADD X0, SP, #16              ; X0 = args 指针 (10 个 u64)
//	MOV X1, bcVA                 ; 加密字节码地址
//	MOV X2, bcLen                ; 字节码长度
//	MOV X3, xorKey               ; XOR 密钥
//	BL  interpVA                 ; 调用 VM 解释器
//	LDP X29, X30, [SP], #96     ; 恢复 FP/LR + 释放栈帧
//	RET                          ; 返回 (结果在 X0)
/* STANDARD_MODE_DISABLED: BuildTrampoline 已禁用，只保留 BuildTokenTrampoline
func BuildTrampoline(funcAddr, interpVA, bcVA uint64, bcLen uint32, xorKey byte) []byte {
	var buf bytes.Buffer

	// MOV X9, X29 (暂存 caller FP)
	writeU32(&buf, 0xAA1D03E9)

	// MOV X10, X30 (暂存 caller LR)
	writeU32(&buf, 0xAA1E03EA)

	// STP X29, X30, [SP, #-96]!
	writeU32(&buf, 0xA9BA7BFD)

	// MOV X29, SP
	writeU32(&buf, 0x910003FD)

	// STP X0, X1, [SP, #16]
	writeU32(&buf, 0xA90107E0)

	// STP X2, X3, [SP, #32]
	writeU32(&buf, 0xA9020FE2)

	// STP X4, X5, [SP, #48]
	writeU32(&buf, 0xA90317E4)

	// STP X6, X7, [SP, #64]
	writeU32(&buf, 0xA9041FE6)

	// STP X9, X10, [SP, #80]
	writeU32(&buf, 0xA9052BE9)

	// ADD X0, SP, #16 (X0 = args 指针)
	writeU32(&buf, 0x910043E0)

	// Load bcVA into X1
	writeARM64MovZ(&buf, 1, uint16(bcVA&0xFFFF), 0)
	writeARM64MovK(&buf, 1, uint16((bcVA>>16)&0xFFFF), 1)
	writeARM64MovK(&buf, 1, uint16((bcVA>>32)&0xFFFF), 2)

	// Load bcLen into X2
	writeARM64MovZ(&buf, 2, uint16(bcLen&0xFFFF), 0)
	if bcLen > 0xFFFF {
		writeARM64MovK(&buf, 2, uint16((bcLen>>16)&0xFFFF), 1)
	}

	// Load xorKey into X3
	writeARM64MovZ(&buf, 3, uint16(xorKey), 0)

	// BL interpVA
	blPC := funcAddr + uint64(buf.Len())
	blOffset := int64(interpVA) - int64(blPC)
	blImm26 := (blOffset >> 2) & 0x03FFFFFF
	blInst := uint32(0x94000000) | uint32(blImm26)
	writeU32(&buf, blInst)

	// LDP X29, X30, [SP], #96
	writeU32(&buf, 0xA8C67BFD)

	// RET
	writeU32(&buf, 0xD65F03C0)

	return buf.Bytes()
}
STANDARD_MODE_DISABLED */

// BuildTokenTrampoline 构造 Token 化入口跳板（3 条 ARM64 指令, 12 字节）
//
//	MOV  W16, #token_lo16          ; token 低 16 位 → W16
//	MOVK W16, #token_hi16, LSL#16  ; token 高 16 位合并
//	B    vm_entry_token             ; 跳转到 Token 入口
//
// X16 (IP0) 传递 token，X0-X7 保持调用方原始参数不变。
func BuildTokenTrampoline(funcAddr, vmEntryTokenVA uint64, token uint32) []byte {
	var buf bytes.Buffer

	// MOV W16, #token_lo16  (MOVZ W16, sf=0, hw=0)
	lo16 := token & 0xFFFF
	writeU32(&buf, 0x52800010|uint32(lo16)<<5)

	// MOVK W16, #token_hi16, LSL#16  (MOVK W16, sf=0, hw=1)
	hi16 := (token >> 16) & 0xFFFF
	writeU32(&buf, 0x72A00010|uint32(hi16)<<5)

	// B vm_entry_token  (PC = funcAddr + 8)
	bPC := funcAddr + 8
	bOffset := int64(vmEntryTokenVA) - int64(bPC)
	bImm26 := (bOffset >> 2) & 0x03FFFFFF
	writeU32(&buf, 0x14000000|uint32(bImm26))

	return buf.Bytes()
}

// ============================================================
// ELF64 二进制结构读写
// ============================================================

type elf64Ehdr struct {
	Phoff     uint64
	Shoff     uint64
	Phentsize uint16
	Phnum     uint16
	Shentsize uint16
	Shnum     uint16
}

func readEhdr64(d []byte) elf64Ehdr {
	return elf64Ehdr{
		Phoff:     binary.LittleEndian.Uint64(d[0x20:]),
		Shoff:     binary.LittleEndian.Uint64(d[0x28:]),
		Phentsize: binary.LittleEndian.Uint16(d[0x36:]),
		Phnum:     binary.LittleEndian.Uint16(d[0x38:]),
		Shentsize: binary.LittleEndian.Uint16(d[0x3A:]),
		Shnum:     binary.LittleEndian.Uint16(d[0x3C:]),
	}
}

type elf64Phdr struct {
	Type   uint32
	Flags  uint32
	Off    uint64
	Vaddr  uint64
	Paddr  uint64
	Filesz uint64
	Memsz  uint64
	Align  uint64
}

func readPhdr64(d []byte, off uint64) elf64Phdr {
	return elf64Phdr{
		Type:   binary.LittleEndian.Uint32(d[off:]),
		Flags:  binary.LittleEndian.Uint32(d[off+4:]),
		Off:    binary.LittleEndian.Uint64(d[off+8:]),
		Vaddr:  binary.LittleEndian.Uint64(d[off+16:]),
		Paddr:  binary.LittleEndian.Uint64(d[off+24:]),
		Filesz: binary.LittleEndian.Uint64(d[off+32:]),
		Memsz:  binary.LittleEndian.Uint64(d[off+40:]),
		Align:  binary.LittleEndian.Uint64(d[off+48:]),
	}
}

func writePhdr64(d []byte, off uint64, ph elf64Phdr) {
	binary.LittleEndian.PutUint32(d[off:], ph.Type)
	binary.LittleEndian.PutUint32(d[off+4:], ph.Flags)
	binary.LittleEndian.PutUint64(d[off+8:], ph.Off)
	binary.LittleEndian.PutUint64(d[off+16:], ph.Vaddr)
	binary.LittleEndian.PutUint64(d[off+24:], ph.Paddr)
	binary.LittleEndian.PutUint64(d[off+32:], ph.Filesz)
	binary.LittleEndian.PutUint64(d[off+40:], ph.Memsz)
	binary.LittleEndian.PutUint64(d[off+48:], ph.Align)
}

// ============================================================
// ARM64 指令编码辅助
// ============================================================

func writeARM64MovZ(w io.Writer, rd int, imm16 uint16, hw int) {
	inst := uint32(0xD2800000) | (uint32(hw) << 21) | (uint32(imm16) << 5) | uint32(rd)
	writeU32(w, inst)
}

func writeARM64MovK(w io.Writer, rd int, imm16 uint16, hw int) {
	inst := uint32(0xF2800000) | (uint32(hw) << 21) | (uint32(imm16) << 5) | uint32(rd)
	writeU32(w, inst)
}

func writeU32(w io.Writer, v uint32) {
	b := make([]byte, 4)
	binary.LittleEndian.PutUint32(b, v)
	w.Write(b)
}
