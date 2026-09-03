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
