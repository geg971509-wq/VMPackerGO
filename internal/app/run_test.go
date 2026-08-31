package app

import (
	"bytes"
	"context"
	"debug/elf"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	elfpacker "github.com/vmpacker/internal/elf"
	vmruntime "github.com/vmpacker/internal/runtime"
	"github.com/vmpacker/internal/vm"
)

func TestRunVersion(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := RunWithConfig(context.Background(), []string{"-version"}, &stdout, &stderr, Config{Version: "v1.2.3", Commit: "abc123"})
	if err != nil || stdout.String() != "vmpacker v1.2.3 (abc123)\n" || stderr.Len() != 0 {
		t.Fatalf("err=%v stdout=%q stderr=%q", err, stdout.String(), stderr.String())
	}
}

func TestRunHelpAndUsageExitCodes(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if err := Run(context.Background(), []string{"-h"}, &stdout, &stderr); !errors.Is(err, flag.ErrHelp) || ExitCode(err) != 0 {
		t.Fatalf("help err=%v code=%d", err, ExitCode(err))
	}
	if strings.Contains(stderr.String(), "vmpacker: flag: help requested") {
		t.Fatalf("help appended error: %q", stderr.String())
	}
	if err := Run(context.Background(), nil, &stdout, &stderr); err == nil || ExitCode(err) != 2 {
		t.Fatalf("usage err=%v code=%d", err, ExitCode(err))
	}
}

func TestRunPublishesExplicitOutputsOnly(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "raw name.so")
	output := filepath.Join(dir, "packed.so")
	reportPath := filepath.Join(dir, "report.json")
	debugPath := filepath.Join(dir, "map.txt")
	if err := os.WriteFile(input, []byte("ELF"), 0750); err != nil {
		t.Fatal(err)
	}
	fake := func(req elfpacker.Request) (elfpacker.Result, error) {
		if req.Context == nil || req.Mode != "so" || len(req.Selections) != 1 || req.Selections[0].Name != "foo" || !req.Debug {
			t.Fatalf("unexpected request: %#v", req)
		}
		return elfpacker.Result{Artifact: []byte("packed"), Debug: []byte("mapping"), TargetKind: elfpacker.TargetKindAndroidSO, DevelopmentStrategy: "note", Functions: []elfpacker.FunctionFact{{Name: "foo", Instructions: 2, Translated: 2, Bytecode: 4}}}, nil
	}
	var stdout, stderr bytes.Buffer
	err := RunWithConfig(context.Background(), []string{"-mode", "so", "-func", "foo", "-abi", "i32(ptr)", "-o", output, "-report", reportPath, "-debug-map", debugPath, input}, &stdout, &stderr, Config{Version: "dev", Commit: "test", Process: fake})
	if err != nil {
		t.Fatalf("err=%v stderr=%s", err, stderr.String())
	}
	for path, want := range map[string]string{output: "packed", debugPath: "mapping"} {
		data, err := os.ReadFile(path)
		if err != nil || string(data) != want {
			t.Fatalf("%s: %q %v", path, data, err)
		}
	}
	if _, err := os.Stat(output + ".debug.txt"); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("hidden debug output was created")
	}
	var got map[string]any
	data, err := os.ReadFile(reportPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got["input"] != input || got["output"] != output || got["status"] != "ok" {
		t.Fatalf("unexpected report: %s", data)
	}
	if strings.Contains(string(data), os.Getenv("HOME")) && !strings.Contains(input, os.Getenv("HOME")) {
		t.Fatal("report leaked home path")
	}
}

func TestRunFailurePublishesOnlyRequestedReport(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "in.so")
	output := filepath.Join(dir, "out.so")
	reportPath := filepath.Join(dir, "failure.json")
	if err := os.WriteFile(input, []byte("ELF"), 0644); err != nil {
		t.Fatal(err)
	}
	fake := func(elfpacker.Request) (elfpacker.Result, error) {
		return elfpacker.Result{
			TargetKind:          elfpacker.TargetKindAndroidSO,
			AnalysisLimitations: []string{"CFG inference is conservative"},
		}, errors.New("unsupported instruction")
	}
	err := RunWithConfig(context.Background(), []string{"-func", "foo", "-abi", "void()", "-o", output, "-report", reportPath, input}, &bytes.Buffer{}, &bytes.Buffer{}, Config{Process: fake, Version: "dev", Commit: "test"})
	if err == nil || ExitCode(err) != 1 {
		t.Fatalf("err=%v code=%d", err, ExitCode(err))
	}
	if _, err := os.Stat(output); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("failure created artifact")
	}
	data, err := os.ReadFile(reportPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte(`"status": "failed"`)) ||
		!bytes.Contains(data, []byte(`"target_kind": "android-so"`)) ||
		!bytes.Contains(data, []byte("CFG inference is conservative")) || bytes.Contains(data, []byte("output_sha256")) {
		t.Fatalf("unexpected report: %s", data)
	}
}

func TestRunCanceledBeforeProcessorOrPublish(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "in.so")
	output := filepath.Join(dir, "out.so")
	if err := os.WriteFile(input, []byte("ELF"), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	called := false
	err := RunWithConfig(ctx, []string{"-func", "foo", "-abi", "void()", "-o", output, input}, ioDiscard{}, ioDiscard{}, Config{Process: func(elfpacker.Request) (elfpacker.Result, error) {
		called = true
		return elfpacker.Result{}, nil
	}})
	if !errors.Is(err, context.Canceled) || called {
		t.Fatalf("err=%v called=%v", err, called)
	}
	if _, err := os.Stat(output); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("canceled run published output: %v", err)
	}
}

func TestRunBuildsRuntimeFromPreparedTranslationRequirements(t *testing.T) {
	// Given: one explicitly selected function that needs SVC, FP/SIMD, and
	// exclusive-region runtime thunks.
	dir := t.TempDir()
	input := filepath.Join(dir, "input.so")
	output := filepath.Join(dir, "output.so")
	data := buildSectionlessAndroidSO([]uint32{
		0xD4000021,
		0x1E212000,
		0xC85FFC20,
		0x91000400,
		0xC802FC20,
	})
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
		"-ndk", "", "-addr", "0x1200-0x1214:entry", "-abi", "void()", "-o", output, input,
	}, &bytes.Buffer{}, &bytes.Buffer{}, Config{Version: "dev", Commit: "test", BuildRuntime: builder})

	// Then: translation happens first and the exact aggregated requirements are
	// passed to the runtime builder once.
	if !errors.Is(err, elfpacker.ErrRewriteWriterRequired) {
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
	if _, statErr := os.Stat(output); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("rewrite-writer boundary published output: %v", statErr)
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

type ioDiscard struct{}

func (ioDiscard) Write(p []byte) (int, error) { return len(p), nil }
