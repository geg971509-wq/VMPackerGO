package elf

import (
	"debug/elf"
	"encoding/binary"
	"fmt"
)

type payloadSegmentSlot struct {
	ehdr   elf64Ehdr
	index  int
	off    uint64
	source string
}

func (p *Packer) allocatePayloadSegmentSlot(ehdr elf64Ehdr) (*payloadSegmentSlot, error) {
	switch p.selectedInjector {
	case InjectorNoteHijack:
		for i := 0; i < int(ehdr.Phnum); i++ {
			phOff := ehdr.Phoff + uint64(i)*uint64(ehdr.Phentsize)
			ph := readPhdr64(p.data, phOff)
			if ph.Type == uint32(elf.PT_NOTE) {
				return &payloadSegmentSlot{ehdr: ehdr, index: i, off: phOff, source: "pt_note"}, nil
			}
		}
		return nil, fmt.Errorf("PT_NOTE segment not found")

	case InjectorAddSegment:
		for i := 0; i < int(ehdr.Phnum); i++ {
			phOff := ehdr.Phoff + uint64(i)*uint64(ehdr.Phentsize)
			ph := readPhdr64(p.data, phOff)
			if ph.Type == uint32(elf.PT_NULL) {
				return &payloadSegmentSlot{ehdr: ehdr, index: i, off: phOff, source: "pt_null"}, nil
			}
		}
		return p.appendPayloadSegmentSlot(ehdr)

	default:
		return nil, fmt.Errorf("injector %q is not implemented", p.selectedInjector)
	}
}

func (p *Packer) appendPayloadSegmentSlot(ehdr elf64Ehdr) (*payloadSegmentSlot, error) {
	if ehdr.Phentsize != 56 {
		return nil, fmt.Errorf("cannot append PHDR with unexpected e_phentsize=%d", ehdr.Phentsize)
	}
	if ehdr.Phnum == ^uint16(0) {
		return nil, fmt.Errorf("cannot append PHDR: e_phnum is already max")
	}

	tableEnd := ehdr.Phoff + uint64(ehdr.Phnum)*uint64(ehdr.Phentsize)
	newEnd := tableEnd + uint64(ehdr.Phentsize)
	if newEnd > uint64(len(p.data)) {
		return nil, fmt.Errorf("cannot append PHDR: table end 0x%X exceeds file size", newEnd)
	}
	if limit := p.phdrGrowthLimit(ehdr, tableEnd); newEnd > limit {
		return nil, fmt.Errorf("cannot append PHDR safely: need 0x%X bytes but next known file-backed region starts at 0x%X", newEnd, limit)
	}
	for off := tableEnd; off < newEnd; off++ {
		if p.data[off] != 0 {
			return nil, fmt.Errorf("cannot append PHDR safely: non-zero byte at file offset 0x%X", off)
		}
	}

	oldPhnum := ehdr.Phnum
	ehdr.Phnum++
	binary.LittleEndian.PutUint16(p.data[0x38:], ehdr.Phnum)
	p.growPTPHDR(ehdr)

	return &payloadSegmentSlot{
		ehdr:   ehdr,
		index:  int(oldPhnum),
		off:    tableEnd,
		source: "phdr_append",
	}, nil
}

func (p *Packer) phdrGrowthLimit(ehdr elf64Ehdr, tableEnd uint64) uint64 {
	limit := uint64(len(p.data))
	consider := func(off, size uint64) {
		if off == 0 || size == 0 {
			return
		}
		if off < tableEnd && off+size > tableEnd {
			limit = tableEnd
			return
		}
		if off >= tableEnd && off < limit {
			limit = off
		}
	}

	for i := 0; i < int(ehdr.Phnum); i++ {
		ph := readPhdr64(p.data, ehdr.Phoff+uint64(i)*uint64(ehdr.Phentsize))
		if ph.Type == uint32(elf.PT_LOAD) || ph.Type == uint32(elf.PT_PHDR) || ph.Type == uint32(elf.PT_NULL) {
			continue
		}
		consider(ph.Off, ph.Filesz)
	}

	if ehdr.Shoff != 0 && ehdr.Shentsize >= 64 && ehdr.Shnum > 0 {
		for i := 0; i < int(ehdr.Shnum); i++ {
			shOff := ehdr.Shoff + uint64(i)*uint64(ehdr.Shentsize)
			if shOff+40 > uint64(len(p.data)) {
				break
			}
			shType := binary.LittleEndian.Uint32(p.data[shOff+4:])
			if shType == 0 { // SHT_NULL
				continue
			}
			secOff := binary.LittleEndian.Uint64(p.data[shOff+24:])
			secSz := binary.LittleEndian.Uint64(p.data[shOff+32:])
			consider(secOff, secSz)
		}
	}

	return limit
}

func (p *Packer) growPTPHDR(ehdr elf64Ehdr) {
	newSize := uint64(ehdr.Phnum) * uint64(ehdr.Phentsize)
	for i := 0; i < int(ehdr.Phnum); i++ {
		phOff := ehdr.Phoff + uint64(i)*uint64(ehdr.Phentsize)
		ph := readPhdr64(p.data, phOff)
		if ph.Type != uint32(elf.PT_PHDR) {
			continue
		}
		ph.Filesz = newSize
		ph.Memsz = newSize
		writePhdr64(p.data, phOff, ph)
		return
	}
}
