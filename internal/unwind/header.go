package unwind

import (
	"encoding/binary"
	"fmt"
	"math"
	"sort"
)

type HeaderEntry struct {
	InitialLocation uint64
	FDEAddress      uint64
}

type FrameHeader struct {
	EHFrameAddress uint64
	Entries        []HeaderEntry
}

func ParseEHFrameHeader(data []byte, address uint64, order binary.ByteOrder, pointerSize int) (*FrameHeader, error) {
	if len(data) < 4 {
		return nil, fmt.Errorf(".eh_frame_hdr is truncated")
	}
	if data[0] != 1 {
		return nil, fmt.Errorf("unsupported .eh_frame_hdr version %d", data[0])
	}
	ehEncoding, countEncoding, tableEncoding := data[1], data[2], data[3]
	if ehEncoding == PEOmit || countEncoding == PEOmit {
		return nil, fmt.Errorf(".eh_frame_hdr omits required fields")
	}
	if tableEncoding == PEOmit {
		return nil, fmt.Errorf(".eh_frame_hdr search table is omitted")
	}
	offset := 4
	ehAddress, err := DecodePointer(data, &offset, ehEncoding, order, pointerSize, Bases{Field: address})
	if err != nil {
		return nil, fmt.Errorf("decode .eh_frame pointer: %w", err)
	}
	count, err := DecodePointer(data, &offset, countEncoding, order, pointerSize, Bases{Field: address})
	if err != nil {
		return nil, fmt.Errorf("decode FDE count: %w", err)
	}
	if count > uint64(len(data)) || count > uint64(^uint(0)>>1) {
		return nil, fmt.Errorf(".eh_frame_hdr FDE count is unreasonable")
	}
	header := &FrameHeader{EHFrameAddress: ehAddress, Entries: make([]HeaderEntry, 0, int(count))}
	var previous uint64
	for index := uint64(0); index < count; index++ {
		initial, err := DecodePointer(data, &offset, tableEncoding, order, pointerSize, Bases{Field: address, Data: address})
		if err != nil {
			return nil, fmt.Errorf("decode table entry %d initial location: %w", index, err)
		}
		fde, err := DecodePointer(data, &offset, tableEncoding, order, pointerSize, Bases{Field: address, Data: address})
		if err != nil {
			return nil, fmt.Errorf("decode table entry %d FDE address: %w", index, err)
		}
		if index > 0 && initial <= previous {
			return nil, fmt.Errorf(".eh_frame_hdr search table is not strictly ordered")
		}
		previous = initial
		header.Entries = append(header.Entries, HeaderEntry{InitialLocation: initial, FDEAddress: fde})
	}
	if offset != len(data) {
		return nil, fmt.Errorf(".eh_frame_hdr has %d trailing bytes", len(data)-offset)
	}
	return header, nil
}

// BuildEHFrameHeader emits the canonical GNU/AArch64 search header used by the
// rewrite writer: pcrel+sdata4 .eh_frame pointer, udata4 count, and
// datarel+sdata4 sorted search-table pairs.
func BuildEHFrameHeader(address, ehFrameAddress uint64, entries []HeaderEntry) ([]byte, error) {
	if len(entries) > math.MaxUint32 {
		return nil, fmt.Errorf(".eh_frame_hdr entry count exceeds u32")
	}
	sorted := append([]HeaderEntry(nil), entries...)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].InitialLocation != sorted[j].InitialLocation {
			return sorted[i].InitialLocation < sorted[j].InitialLocation
		}
		return sorted[i].FDEAddress < sorted[j].FDEAddress
	})
	for i, entry := range sorted {
		if entry.FDEAddress == 0 {
			return nil, fmt.Errorf(".eh_frame_hdr entry %d has a zero FDE address", i)
		}
		if i > 0 && sorted[i-1].InitialLocation == entry.InitialLocation {
			return nil, fmt.Errorf(".eh_frame_hdr has duplicate initial location 0x%x", entry.InitialLocation)
		}
	}

	result := []byte{1, PEPcrel | PESdata4, PEUdata4, PEDatarel | PESdata4}
	delta, err := signed32Difference(ehFrameAddress, address+uint64(len(result)))
	if err != nil {
		return nil, fmt.Errorf(".eh_frame pointer: %w", err)
	}
	result = binary.LittleEndian.AppendUint32(result, uint32(delta))
	result = binary.LittleEndian.AppendUint32(result, uint32(len(sorted)))
	for i, entry := range sorted {
		initialDelta, err := signed32Difference(entry.InitialLocation, address)
		if err != nil {
			return nil, fmt.Errorf("table entry %d initial location: %w", i, err)
		}
		fdeDelta, err := signed32Difference(entry.FDEAddress, address)
		if err != nil {
			return nil, fmt.Errorf("table entry %d FDE address: %w", i, err)
		}
		result = binary.LittleEndian.AppendUint32(result, uint32(initialDelta))
		result = binary.LittleEndian.AppendUint32(result, uint32(fdeDelta))
	}
	return result, nil
}

func signed32Difference(target, base uint64) (int32, error) {
	if target >= base {
		delta := target - base
		if delta > math.MaxInt32 {
			return 0, fmt.Errorf("positive signed-32 displacement overflows")
		}
		return int32(delta), nil
	}
	delta := base - target
	if delta > uint64(1)<<31 {
		return 0, fmt.Errorf("negative signed-32 displacement overflows")
	}
	if delta == uint64(1)<<31 {
		return math.MinInt32, nil
	}
	return -int32(delta), nil
}
