package runtime

import (
	"context"
	"encoding/binary"
	"strings"
	"testing"
)

func syntheticDarwinObject() []byte {
	const segSize = darwinSegment64Size + darwinSection64Size
	const symoff = 0x200
	const stroff = symoff + darwinNList64Size
	out := make([]byte, 0x240)
	binary.LittleEndian.PutUint32(out[0:], darwinMagic64)
	binary.LittleEndian.PutUint32(out[4:], darwinCPUTypeARM64)
	binary.LittleEndian.PutUint32(out[8:], darwinSubtypeARM64)
	binary.LittleEndian.PutUint32(out[12:], darwinMHObject)
	binary.LittleEndian.PutUint32(out[16:], 2)
	binary.LittleEndian.PutUint32(out[20:], segSize+24)
	// __TEXT,__text
	p := uint64(darwinHeader64Size)
	binary.LittleEndian.PutUint32(out[p:], darwinLCSegment64)
	binary.LittleEndian.PutUint32(out[p+4:], segSize)
	copy(out[p+8:p+24], []byte("__TEXT"))
	binary.LittleEndian.PutUint32(out[p+64:], 1)
	s := p + darwinSegment64Size
	copy(out[s:s+16], []byte("__text"))
	copy(out[s+16:s+32], []byte("__TEXT"))
	binary.LittleEndian.PutUint64(out[s+40:], 4)
	binary.LittleEndian.PutUint32(out[s+48:], 0x180)
	binary.LittleEndian.PutUint32(out[s+52:], 2)
	binary.LittleEndian.PutUint32(out[s+64:], 0x80000400)
	binary.LittleEndian.PutUint32(out[0x180:], 0xd65f03c0) // ret
	// LC_SYMTAB, one external text symbol named _vm_entry.
	p += segSize
	binary.LittleEndian.PutUint32(out[p:], darwinLCSymtab)
	binary.LittleEndian.PutUint32(out[p+4:], 24)
	binary.LittleEndian.PutUint32(out[p+8:], symoff)
	binary.LittleEndian.PutUint32(out[p+12:], 1)
	binary.LittleEndian.PutUint32(out[p+16:], stroff)
	binary.LittleEndian.PutUint32(out[p+20:], uint32(len("\x00_vm_entry\x00")))
	binary.LittleEndian.PutUint32(out[symoff:], 1)
	out[symoff+4] = 0x0f // N_EXT|N_SECT
	out[symoff+5] = 1
	binary.LittleEndian.PutUint64(out[symoff+8:], 0)
	copy(out[stroff:], []byte("\x00_vm_entry\x00"))
	return out
}

func TestParseDarwinObject(t *testing.T) {
	img, err := ParseDarwinObject(syntheticDarwinObject())
	if err != nil {
		t.Fatal(err)
	}
	if len(img.Sections) != 1 || img.Sections[0].Name != "__text" {
		t.Fatalf("sections=%#v", img.Sections)
	}
	if sym, ok := img.Symbol("vm_entry"); !ok || sym.Name != "_vm_entry" || sym.Size != 4 {
		t.Fatalf("vm_entry=%#v found=%v", sym, ok)
	}
}

func TestParseDarwinObjectRejectsWrongFileType(t *testing.T) {
	data := syntheticDarwinObject()
	binary.LittleEndian.PutUint32(data[12:], 0x6)
	if _, err := ParseDarwinObject(data); err == nil || !strings.Contains(err.Error(), "MH_OBJECT") {
		t.Fatalf("err=%v", err)
	}
}

func TestBuildDarwinRequiresSourceAndToolchain(t *testing.T) {
	if _, err := BuildDarwin(context.Background(), DarwinBuildConfig{}); err == nil || !strings.Contains(err.Error(), "source") {
		t.Fatalf("source err=%v", err)
	}
	if _, err := BuildDarwin(context.Background(), DarwinBuildConfig{Source: []byte("ret\n"), Clang: "/definitely/missing/clang"}); err == nil {
		t.Fatal("missing clang unexpectedly accepted")
	}
}

func TestBuildDarwinRejectsAmbiguousSourceLanguage(t *testing.T) {
	_, err := BuildDarwin(context.Background(), DarwinBuildConfig{SourceName: "runtime.txt", Source: []byte("ret\n"), Clang: "/definitely/missing/clang"})
	if err == nil || !strings.Contains(err.Error(), "source name") {
		t.Fatalf("err=%v", err)
	}
}
