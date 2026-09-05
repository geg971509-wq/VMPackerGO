package unwind

import (
	"encoding/binary"
	"strings"
	"testing"
)

func TestParseLSDARejectsTruncatedHeaderAfterEncodedLPStart(t *testing.T) {
	// The LPStart encoding consumes the remaining two bytes. ParseLSDA must
	// report the missing type encoding instead of indexing one byte past input.
	_, err := ParseLSDA([]byte{PEUdata2, 0, 0}, 0x2000, 0x1000, binary.LittleEndian, 8)
	if err == nil {
		t.Fatal("truncated LSDA header was accepted")
	}
	if !strings.Contains(err.Error(), "type encoding") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestParseLSDAMetadataRejectsExtremeNegativeFilter(t *testing.T) {
	data := append([]byte{}, []byte{
		0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x40,
		0x00,
	}...)
	lsda := &LSDA{
		CallSites:    []CallSite{{Action: 1}},
		ActionChains: map[uint64][]ActionRecord{},
		TypeInfos:    map[uint64]TypeInfo{},
		TypeEncoding: PEAbsptr,
	}
	if err := parseLSDAMetadata(data, 0, 0, 2, 0, lsda, binary.LittleEndian, 8); err == nil {
		t.Fatal("extreme negative LSDA filter was accepted")
	}
}

func TestParseLSDAMetadataRejectsExtremeTypeIndex(t *testing.T) {
	data := []byte{
		0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0x01,
		0x00,
	}
	lsda := &LSDA{
		CallSites:    []CallSite{{Action: 1}},
		ActionChains: map[uint64][]ActionRecord{},
		TypeInfos:    map[uint64]TypeInfo{},
		TypeEncoding: PEAbsptr,
	}
	if err := parseLSDAMetadata(data, 0, 0, len(data), 0, lsda, binary.LittleEndian, 8); err == nil {
		t.Fatal("extreme LSDA type index was accepted")
	}
}
