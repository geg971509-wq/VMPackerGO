package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"debug/elf"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	vmruntime "github.com/vmpacker/internal/runtime"
	"github.com/vmpacker/internal/vm"
)

func TestRunBuildsRuntimeFromPreparedTranslationRequirements(t *testing.T) {
	// Given: one explicitly selected function that needs SVC, FP/SIMD, and
	// exclusive-region runtime thunks.
	dir := t.TempDir()
	input := filepath.Join(dir, "input.so")
	output := filepath.Join(dir, "output.so")
	reportPath := filepath.Join(dir, "report.json")
	data := buildSectionlessAndroidSO([]uint32{
		0xD4000021,
		0x1E212000,
		0xC85FFC20,
		0x91000400,
		0xC802FC20,
	})
	inputBefore := append([]byte(nil), data...)
	if err := os.WriteFile(input, data, 0750); err != nil {
		t.Fatal(err)
	}
	var captured vmruntime.BuildConfig
	buildCalls := 0
	builder := func(_ context.Context, cfg vmruntime.BuildConfig) (*vmruntime.Image, error) {
		buildCalls++
		captured = cfg
		image := rewriteReadyRuntimeImage(t, cfg.Opcodes)
		image.SVCImmediates = append([]uint16(nil), cfg.SVCImmediates...)
		image.ExclusiveRegions = append([]vm.ExclusiveRegion(nil), cfg.ExclusiveRegions...)
		image.FPSIMDInstructions = append([]uint32(nil), cfg.FPSIMDInstructions...)
		return image, nil
	}

	// When: the CLI reaches runtime construction on the normal transform path.
	err := RunWithConfig(context.Background(), []string{
		"-ndk", "", "-addr", "0x1200-0x1214:entry", "-abi", "void()", "-report", reportPath, "-o", output, input,
	}, &bytes.Buffer{}, &bytes.Buffer{}, Config{Version: "dev", Commit: "test", BuildRuntime: builder})

	// Then: translation happens first, the exact aggregated requirements are
	// passed to the runtime builder once, and the rewritten artifact is published.
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if buildCalls != 1 {
		t.Fatalf("runtime build calls=%d, want 1", buildCalls)
	}
	if len(captured.SVCImmediates) != 1 || captured.SVCImmediates[0] != 1 {
		t.Fatalf("SVC immediates=%v", captured.SVCImmediates)
	}
	if len(captured.FPSIMDInstructions) != 1 || captured.FPSIMDInstructions[0] != 0x1E212000 {
		t.Fatalf("FP/SIMD instructions=%#x", captured.FPSIMDInstructions)
	}
	if len(captured.ExclusiveRegions) != 1 || !captured.ExclusiveRegions[0].Valid() {
		t.Fatalf("exclusive regions=%#v", captured.ExclusiveRegions)
	}
	inputAfter, readErr := os.ReadFile(input)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if !bytes.Equal(inputAfter, inputBefore) {
		t.Fatal("normal transform path modified the input file")
	}
	artifact, readErr := os.ReadFile(output)
	if readErr != nil {
		t.Fatal(readErr)
	}
	parsed, parseErr := elf.NewFile(bytes.NewReader(artifact))
	if parseErr != nil {
		t.Fatalf("published artifact is not a valid ELF: %v", parseErr)
	}
	defer parsed.Close()
	if parsed.Machine != elf.EM_AARCH64 {
		t.Fatalf("published machine=%s, want AArch64", parsed.Machine)
	}
	reportData, readErr := os.ReadFile(reportPath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	var report struct {
		Status              string `json:"status"`
		DevelopmentStrategy string `json:"development_strategy"`
		OutputSHA256        string `json:"output_sha256"`
		Functions           []struct {
			Translated int `json:"translated"`
		} `json:"functions"`
	}
	if err := json.Unmarshal(reportData, &report); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(artifact)
	if report.Status != "ok" || report.DevelopmentStrategy != "rewrite-artifact-ready" ||
		report.OutputSHA256 != hex.EncodeToString(sum[:]) || len(report.Functions) != 1 || report.Functions[0].Translated == 0 {
		t.Fatalf("report does not describe the published artifact: %#v", report)
	}
}

func rewriteReadyRuntimeImage(t *testing.T, opcodes vm.OpcodeMap) *vmruntime.Image {
	t.Helper()
	digest, err := opcodes.Digest()
	if err != nil {
		t.Fatal(err)
	}
	return &vmruntime.Image{
		OpcodeMapDigest: hex.EncodeToString(digest[:]),
		Sections: []vmruntime.Section{
			{Index: 0, Name: "", Type: elf.SHT_NULL},
			{Index: 1, Name: ".text.entry", Type: elf.SHT_PROGBITS, Flags: elf.SHF_ALLOC | elf.SHF_EXECINSTR, Alignment: 4, Size: 16, Data: make([]byte, 16)},
			{Index: 2, Name: ".data.entry", Type: elf.SHT_PROGBITS, Flags: elf.SHF_ALLOC | elf.SHF_WRITE, Alignment: 8, Size: 24, Data: make([]byte, 24)},
			{Index: 3, Name: ".eh_frame", Type: elf.SHT_PROGBITS, Flags: elf.SHF_ALLOC, Alignment: 8, Size: 8, Data: make([]byte, 8)},
		},
		Symbols: []vmruntime.Symbol{
			{Index: 1, Name: "vm_entry_token", Info: byte(elf.STB_GLOBAL)<<4 | byte(elf.STT_FUNC), Section: 1, Value: 0, Size: 4},
			{Index: 2, Name: "vm_entry", Info: byte(elf.STB_GLOBAL)<<4 | byte(elf.STT_FUNC), Section: 1, Value: 4, Size: 4},
			{Index: 3, Name: "vm_native_call", Info: byte(elf.STB_GLOBAL)<<4 | byte(elf.STT_FUNC), Section: 1, Value: 8, Size: 4},
			{Index: 4, Name: "vm_atomic_native", Info: byte(elf.STB_GLOBAL)<<4 | byte(elf.STT_FUNC), Section: 1, Value: 12, Size: 4},
			{Index: 5, Name: "_token_table_va", Info: byte(elf.STB_GLOBAL)<<4 | byte(elf.STT_OBJECT), Section: 2, Value: 0, Size: 8},
			{Index: 6, Name: "_image_file_va", Info: byte(elf.STB_GLOBAL)<<4 | byte(elf.STT_OBJECT), Section: 2, Value: 8, Size: 8},
			{Index: 7, Name: "_token_count", Info: byte(elf.STB_GLOBAL)<<4 | byte(elf.STT_OBJECT), Section: 2, Value: 16, Size: 8},
		},
	}
}

func buildSectionlessAndroidSO(code []uint32) []byte {
	const (
		programHeaderOffset = 64
		programHeaderCount  = 2
		dynamicOffset       = 0x180
		codeOffset          = 0x200
		loadAddress         = 0x1000
	)
	data := make([]byte, codeOffset+len(code)*4)
	copy(data[:16], []byte{0x7f, 'E', 'L', 'F', byte(elf.ELFCLASS64), byte(elf.ELFDATA2LSB), byte(elf.EV_CURRENT)})
	bo := binary.LittleEndian
	bo.PutUint16(data[16:18], uint16(elf.ET_DYN))
	bo.PutUint16(data[18:20], uint16(elf.EM_AARCH64))
	bo.PutUint32(data[20:24], uint32(elf.EV_CURRENT))
	bo.PutUint64(data[32:40], programHeaderOffset)
	bo.PutUint16(data[52:54], 64)
	bo.PutUint16(data[54:56], 56)
	bo.PutUint16(data[56:58], programHeaderCount)

	writeProgram := func(index int, kind elf.ProgType, flags elf.ProgFlag, offset, address, size, alignment uint64) {
		entry := programHeaderOffset + index*56
		bo.PutUint32(data[entry:entry+4], uint32(kind))
		bo.PutUint32(data[entry+4:entry+8], uint32(flags))
		bo.PutUint64(data[entry+8:entry+16], offset)
		bo.PutUint64(data[entry+16:entry+24], address)
		bo.PutUint64(data[entry+24:entry+32], address)
		bo.PutUint64(data[entry+32:entry+40], size)
		bo.PutUint64(data[entry+40:entry+48], size)
		bo.PutUint64(data[entry+48:entry+56], alignment)
	}
	writeProgram(0, elf.PT_LOAD, elf.PF_R|elf.PF_X, 0, loadAddress, uint64(len(data)), 0x1000)
	writeProgram(1, elf.PT_DYNAMIC, elf.PF_R, dynamicOffset, loadAddress+dynamicOffset, 16, 8)
	for i, instruction := range code {
		bo.PutUint32(data[codeOffset+i*4:], instruction)
	}
	return data
}
