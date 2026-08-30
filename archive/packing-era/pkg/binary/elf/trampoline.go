package elf

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
)

// ============================================================
// ARM64 跳板代码生成 + ELF64 二进制结构读写
// ============================================================

// BuildTokenTrampoline 构造 Token 化入口跳板（3 条 ARM64 指令, 12 字节）
//
//	MOV  W16, #token_lo16          ; token 低 16 位 → W16
//	MOVK W16, #token_hi16, LSL#16  ; token 高 16 位合并
//	B    vm_entry_token             ; 跳转到 Token 入口
//
// X16 (IP0) 传递 token，X0-X7 保持调用方原始参数不变。
func BuildTokenTrampoline(funcAddr, vmEntryTokenVA uint64, token uint32) ([]byte, error) {
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
	if bOffset%4 != 0 {
		return nil, fmt.Errorf("branch target is not 4-byte aligned: offset=%d", bOffset)
	}
	if bOffset < -(1<<27) || bOffset > (1<<27)-4 {
		return nil, fmt.Errorf("B imm26 range exceeded: offset=%d (limit ±128MiB)", bOffset)
	}
	bImm26 := (bOffset >> 2) & 0x03FFFFFF
	writeU32(&buf, 0x14000000|uint32(bImm26))

	return buf.Bytes(), nil
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

func writeU32(w io.Writer, v uint32) {
	b := make([]byte, 4)
	binary.LittleEndian.PutUint32(b, v)
	w.Write(b)
}
