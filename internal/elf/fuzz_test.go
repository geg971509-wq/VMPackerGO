package elf

import "testing"

func FuzzELFMetadataNeverPanics(f *testing.F) {
	f.Add([]byte{})
	f.Add([]byte{0x7f, 'E', 'L', 'F'})
	f.Add(make([]byte, 64))
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 1<<20 {
			t.Skip()
		}
		meta, err := parseELFMetadata(data, AndroidModeAuto)
		if err == nil && meta != nil && meta.file != nil {
			_ = meta.file.Close()
		}
	})
}
