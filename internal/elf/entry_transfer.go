package elf

import "fmt"

const (
	entryTransferReg = uint32(17) // X17/IP1, caller-saved veneer scratch register.
)

// buildEntryTransfer returns the exact instruction sequence that transfers
// control from branchVA to targetVA. Prefer the ordinary B immediate encoding;
// when that cannot reach, use an inline ADRP+ADD+BR veneer. The long form is
// ASLR-safe because it is based only on the final planned virtual addresses.
func buildEntryTransfer(branchVA, targetVA uint64) ([]uint32, error) {
	if branchVA&3 != 0 || targetVA&3 != 0 {
		return nil, fmt.Errorf("entry transfer requires 4-byte aligned source and target")
	}
	if delta, ok := signedDifference(targetVA, branchVA); ok &&
		delta >= -(1<<27) && delta <= (1<<27)-4 && delta%4 == 0 {
		word, err := encodeBranch26(branchVA, targetVA, 0x14000000)
		if err != nil {
			return nil, err
		}
		return []uint32{word}, nil
	}

	fromPage := branchVA &^ uint64(0xfff)
	toPage := targetVA &^ uint64(0xfff)
	pageDeltaBytes, ok := signedDifference(toPage, fromPage)
	if !ok || pageDeltaBytes%0x1000 != 0 {
		return nil, fmt.Errorf("entry long transfer page delta is not representable")
	}
	pageDelta := pageDeltaBytes / 0x1000
	if pageDelta < -(1<<20) || pageDelta > (1<<20)-1 {
		return nil, fmt.Errorf("entry long transfer target is outside ADRP +/-4 GiB range")
	}
	imm := uint64(pageDelta) & 0x1fffff
	adrp := uint32(0x90000000) |
		uint32(imm&0x3)<<29 |
		uint32((imm>>2)&0x7ffff)<<5 |
		entryTransferReg
	add := uint32(0x91000000) |
		uint32(targetVA&0xfff)<<10 |
		entryTransferReg<<5 |
		entryTransferReg
	br := uint32(0xd61f0000) | entryTransferReg<<5
	return []uint32{adrp, add, br}, nil
}
