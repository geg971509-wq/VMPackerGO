package unwind

import (
	"encoding/binary"
	"fmt"
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
