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

func TestParseLSDARejectsCallSiteAddressOverflow(t *testing.T) {
	for _, test := range []struct {
		name    string
		start   uint64
		landing uint64
	}{
		{name: "start", start: 1},
		{name: "landing", landing: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			data := append([]byte{PEOmit, PEOmit, PEUdata8, 25}, make([]byte, 25)...)
			binary.LittleEndian.PutUint64(data[4:12], test.start)
			binary.LittleEndian.PutUint64(data[12:20], 0)
			binary.LittleEndian.PutUint64(data[20:28], test.landing)
			if _, err := ParseLSDA(data, 0, ^uint64(0), binary.LittleEndian, 8); err == nil {
				t.Fatal("overflowing LSDA call-site address was accepted")
			}
		})
	}
}

func TestParseLSDAMetadataRejectsOverlappingActionAndTypeTables(t *testing.T) {
	lsda := &LSDA{
		CallSites:    []CallSite{{Action: 1}},
		ActionChains: map[uint64][]ActionRecord{},
		TypeInfos:    map[uint64]TypeInfo{},
		TypeEncoding: PEAbsptr,
	}
	if err := parseLSDAMetadata([]byte{0, 0}, 0, 0, 1, 0, lsda, binary.LittleEndian, 8); err == nil {
		t.Fatal("overlapping LSDA action and type tables were accepted")
	}
}

func TestParseLSDAMetadataRejectsExtremeNegativeFilter(t *testing.T) {
	data := append([]byte{}, []byte{
		0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x7f,
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
