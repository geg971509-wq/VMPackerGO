package vm

import (
	"crypto/sha256"
	"encoding/binary"
)

// REG_XZR is the marker used for the AArch64 zero register.
const REG_XZR = -2

// Instruction is the decoded instruction representation shared by the active
// AArch64 decoder and translator.
type Instruction struct {
	Raw       uint32
	Op        int
	Rd        int
	Rn        int
	Rm        int
	Rt2       int
	Imm       int64
	Shift     int
	ShiftType int
	Cond      int
	SF        bool
	Offset    int
	WB        int
}

// ExclusiveRegion is a complete, contiguous exclusive-monitor CFG beginning
// at a load-exclusive instruction. It may contain retry branches, multiple
// store-exclusive paths, or CLREX, and must execute without returning to the interpreter. ID is derived from the exact
// instruction words so bytecode and generated runtime thunks can be joined
// without process-local numbering.
type ExclusiveRegion struct {
	ID           uint32
	Instructions []uint32
}

func NewExclusiveRegion(instructions []uint32) ExclusiveRegion {
	h := sha256.New()
	h.Write([]byte("vmpacker-exclusive-region-v1\x00"))
	var encoded [4]byte
	for _, raw := range instructions {
		binary.LittleEndian.PutUint32(encoded[:], raw)
		h.Write(encoded[:])
	}
	sum := h.Sum(nil)
	return ExclusiveRegion{
		ID:           binary.LittleEndian.Uint32(sum[:4]),
		Instructions: append([]uint32(nil), instructions...),
	}
}

func (region ExclusiveRegion) Valid() bool {
	if len(region.Instructions) < 2 {
		return false
	}
	return NewExclusiveRegion(region.Instructions).ID == region.ID
}

// FuncInfo identifies a function in an ELF input.
type FuncInfo struct {
	Name    string
	Addr    uint64
	Size    uint64
	Offset  uint64
	Section string
}
