package unwind

import (
	"encoding/binary"
	"testing"
)

func FuzzEHFrameNeverPanics(f *testing.F) {
	for _, seed := range [][]byte{{}, {0, 0, 0, 0}, {4, 0, 0, 0, 0, 0, 0, 0}} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 1<<20 {
			t.Skip()
		}
		_, _ = ParseEHFrame(data, 0x1000, binary.LittleEndian, 8)
	})
}

func FuzzLSDANeverPanics(f *testing.F) {
	for _, seed := range [][]byte{{}, {PEOmit, PEOmit, PEOmit}, {PEOmit, PEOmit, PEUdata4, 0}} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 1<<20 {
			t.Skip()
		}
		_, _ = ParseLSDA(data, 0x2000, 0x1000, binary.LittleEndian, 8)
	})
}
