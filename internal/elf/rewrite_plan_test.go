package elf

import (
	"bytes"
	"context"
	"debug/elf"
	"encoding/binary"
	"encoding/hex"
	"os"
	"strings"
	"testing"

	"github.com/vmpacker/internal/arch/arm64"
	vmruntime "github.com/vmpacker/internal/runtime"
	"github.com/vmpacker/internal/vm"
)

func TestRewritePlanBuildsRuntimeLayoutRelocationsBytecodeAndTrampoline(t *testing.T) {
	fixture := buildELFFixture(fixtureOptions{dynamic: true, code: []uint32{0xD503201F, 0xD503201F, 0xD65F03C0}})
	request, analysis, preparation := rewritePlanPreparation(t, fixture, false)
	request.RuntimeImage = rewritePlanRuntimeImage(t, request.Opcodes)

	inputBefore := append([]byte(nil), request.Input...)
	runtimeBefore := cloneRuntimeImageData(request.RuntimeImage)
	bytecodeBefore := append([]byte(nil), preparation.Functions[0].Translation.Bytecode...)

	plan, err := buildRewritePlan(request, analysis, preparation)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.segments) != 3 {
		t.Fatalf("segments=%d, want RX/RW/R", len(plan.segments))
	}
	wantFlags := []elf.ProgFlag{elf.PF_R | elf.PF_X, elf.PF_R | elf.PF_W, elf.PF_R}
	for i, segment := range plan.segments {
		if segment.flags != wantFlags[i] {
			t.Fatalf("segment[%d] flags=%s, want %s", i, segment.flags, wantFlags[i])
		}
		if segment.fileOffset%rewriteLoadAlignment != 0 || segment.vaddr%rewriteLoadAlignment != 0 {
			t.Fatalf("segment[%d] is not 0x%x aligned: off=0x%x va=0x%x", i, rewriteLoadAlignment, segment.fileOffset, segment.vaddr)
		}
		if segment.fileSize != uint64(len(segment.data)) || segment.memSize != segment.fileSize || segment.fileSize == 0 {
			t.Fatalf("segment[%d] size mismatch: file=%d mem=%d data=%d", i, segment.fileSize, segment.memSize, len(segment.data))
		}
	}

	entryVA := mustPlannedSymbolVA(t, plan, "vm_entry_token")
	anchorVA := mustPlannedSymbolVA(t, plan, "_token_table_va")
	if plan.tokenTableVA <= anchorVA {
		t.Fatalf("token table VA=0x%x must follow anchor=0x%x", plan.tokenTableVA, anchorVA)
	}
	if len(plan.functions) != 1 || len(plan.functions[0].entryPatch) != 12 {
		t.Fatalf("function plan=%+v", plan.functions)
	}
	if got := binary.LittleEndian.Uint32(plan.functions[0].entryPatch[8:12]) & 0xfc000000; got != 0x14000000 {
		t.Fatalf("entry branch opcode=0x%08x", binary.LittleEndian.Uint32(plan.functions[0].entryPatch[8:12]))
	}
	if plan.functions[0].entryTargetVA != entryVA || plan.functions[0].bytecodeLen == 0 {
		t.Fatalf("function plan target/bytecode=%+v", plan.functions[0])
	}

	tokenTableOff := plan.tokenTableVA - plan.segments[2].vaddr
	desc := plan.segments[2].data[tokenTableOff : tokenTableOff+tokenDescriptorSize]
	if got := binary.LittleEndian.Uint64(desc[0:8]); got != plan.functions[0].bytecodeVA-anchorVA {
		t.Fatalf("descriptor bc_off=0x%x, want 0x%x", got, plan.functions[0].bytecodeVA-anchorVA)
	}
	if got := binary.LittleEndian.Uint32(desc[8:12]); got != plan.functions[0].bytecodeLen {
		t.Fatalf("descriptor bc_len=%d, want %d", got, plan.functions[0].bytecodeLen)
	}
	if desc[12] != plan.functions[0].xorKey {
		t.Fatalf("descriptor xor key=0x%x, want 0x%x", desc[12], plan.functions[0].xorKey)
	}
	if got := binary.LittleEndian.Uint64(desc[16:24]); got != analysis.Selections[0].Address {
		t.Fatalf("descriptor func_file_va=0x%x", got)
	}

	if got := plannedRuntimeUint64(t, plan, ".rodata", 0); got != entryVA {
		t.Fatalf("runtime ABS64 relocation=0x%x, want vm_entry_token=0x%x", got, entryVA)
	}
	if got := plannedRuntimeUint64(t, plan, ".data.entry", 0); got != plan.tokenTableVA-anchorVA {
		t.Fatalf("_token_table_va=0x%x, want 0x%x", got, plan.tokenTableVA-anchorVA)
	}
	if got := plannedRuntimeUint64(t, plan, ".data.entry", 8); got != anchorVA {
		t.Fatalf("_image_file_va=0x%x, want 0x%x", got, anchorVA)
	}
	if got := plannedRuntimeUint64(t, plan, ".data.entry", 16); got != 1 {
		t.Fatalf("_token_count=%d, want 1", got)
	}

	if !bytes.Equal(request.Input, inputBefore) || !equalRuntimeImageData(request.RuntimeImage, runtimeBefore) ||
		!bytes.Equal(preparation.Functions[0].Translation.Bytecode, bytecodeBefore) {
		t.Fatal("rewrite planning mutated source input, runtime image, or prepared bytecode")
	}
}

func TestRewritePlanAcceptsInstalledExactR29RuntimeImage(t *testing.T) {
	root := os.Getenv("ANDROID_NDK")
	if root == "" {
		root = os.Getenv("ANDROID_NDK_HOME")
	}
	if root == "" {
		t.Skip("exact Android NDK r29 is not configured")
	}

	fixture := buildELFFixture(fixtureOptions{dynamic: true})
	request, analysis, preparation := rewritePlanPreparation(t, fixture, false)
	image, err := vmruntime.Build(context.Background(), vmruntime.BuildConfig{
		NDKDir: root, Opcodes: request.Opcodes,
		SVCImmediates: preparation.SVCImmediates, ExclusiveRegions: preparation.ExclusiveRegions,
		FPSIMDInstructions: preparation.FPSIMDInstructions,
	})
	if err != nil {
		t.Fatalf("Build exact-r29 runtime: %v", err)
	}
	request.RuntimeImage = image

	plan, err := buildRewritePlan(request, analysis, preparation)
	if err != nil {
		t.Fatalf("build rewrite plan with exact-r29 runtime: %v", err)
	}
	if len(plan.segments) != 3 || len(plan.programHeaders.newLoads) != 3 {
		t.Fatalf("incomplete exact-r29 rewrite plan: segments=%d loads=%d", len(plan.segments), len(plan.programHeaders.newLoads))
	}
}

func TestRewritePlanPatchesBytecodeImageRelocationAgainstFinalAnchor(t *testing.T) {
	fixture := buildELFFixture(fixtureOptions{dynamic: true, code: []uint32{0x10000800, 0xD503201F, 0xD65F03C0}})
	request, analysis, preparation := rewritePlanPreparation(t, fixture, false)
	request.RuntimeImage = rewritePlanRuntimeImage(t, request.Opcodes)
	translation := preparation.Functions[0].Translation
	if len(translation.Relocations) != 1 || translation.Relocations[0].TargetVA != 0x1300 {
		t.Fatalf("bytecode relocations=%+v", translation.Relocations)
	}

	plan, err := buildRewritePlan(request, analysis, preparation)
	if err != nil {
		t.Fatal(err)
	}
	function := plan.functions[0]
	segment := plan.segments[rewriteSegmentR]
	start := function.bytecodeSegmentOffset
	end := start + uint64(function.bytecodeLen)
	plain := append([]byte(nil), segment.data[start:end]...)
	for i := range plain {
		plain[i] ^= function.xorKey
	}

	relocation := translation.Relocations[0]
	instructionStart, instructionSize := originalInstructionContaining(t, translation, request.Opcodes, relocation.Offset)
	_, offsetMap, err := reverseInstructions(translation.Bytecode, translation.CodeLen, request.Opcodes)
	if err != nil {
		t.Fatal(err)
	}
	reversedEnd := offsetMap[instructionStart]
	reversedStart := reversedEnd - instructionSize - 1
	relocatedOffset := reversedStart + relocation.Offset - instructionStart
	gotDelta := int64(binary.LittleEndian.Uint64(plain[relocatedOffset : relocatedOffset+8]))
	wantDelta, ok := signedDifference(relocation.TargetVA, mustPlannedSymbolVA(t, plan, "_token_table_va"))
	if !ok || gotDelta != wantDelta {
		t.Fatalf("image delta=%d, want %d", gotDelta, wantDelta)
	}

	mapCount := binary.LittleEndian.Uint32(translation.Bytecode[len(translation.Bytecode)-16:])
	trailerSize := len(translation.Bytecode) - translation.CodeLen
	finalCodeLen := int(function.bytecodeLen) - trailerSize
	reverseOffset := finalCodeLen + int(mapCount)*8
	if plain[reverseOffset] != 1 || binary.LittleEndian.Uint32(plain[reverseOffset+1:reverseOffset+5]) != function.opcodeKey {
		t.Fatalf("final trailer reverse=%d opcode_key=0x%x", plain[reverseOffset], binary.LittleEndian.Uint32(plain[reverseOffset+1:reverseOffset+5]))
	}
}

func TestTrampolinePreservesBTIEntryAndRequiresSixteenBytes(t *testing.T) {
	fixture := buildELFFixture(fixtureOptions{dynamic: true, code: []uint32{0xD503245F, 0xD503201F, 0xD503201F, 0xD65F03C0}})
	request, analysis, preparation := rewritePlanPreparation(t, fixture, true)
	request.RuntimeImage = rewritePlanRuntimeImage(t, request.Opcodes)

	plan, err := buildRewritePlan(request, analysis, preparation)
	if err != nil {
		t.Fatal(err)
	}
	patch := plan.functions[0].entryPatch
	if len(patch) != 16 || !bytes.Equal(patch[:4], request.Input[analysis.Selections[0].Offset:analysis.Selections[0].Offset+4]) {
		t.Fatalf("BTI patch=%x", patch)
	}

	shortAnalysis := analysis
	shortAnalysis.Selections = append([]Selection(nil), analysis.Selections...)
	shortAnalysis.Selections[0].End = shortAnalysis.Selections[0].Address + 12
	shortPreparation := *preparation
	shortPreparation.Functions = append([]PreparedFunction(nil), preparation.Functions...)
	shortPreparation.Functions[0].Selection = shortAnalysis.Selections[0]
	before := append([]byte(nil), request.Input...)
	if _, err := buildRewritePlan(request, shortAnalysis, &shortPreparation); err == nil || !strings.Contains(err.Error(), "BTI") {
		t.Fatalf("short BTI err=%v", err)
	}
	if !bytes.Equal(request.Input, before) {
		t.Fatal("failed BTI planning mutated input")
	}
}

func TestRuntimeLayoutRejectsWritableExecutableSectionWithoutMutation(t *testing.T) {
	fixture := buildELFFixture(fixtureOptions{dynamic: true})
	request, analysis, preparation := rewritePlanPreparation(t, fixture, false)
	request.RuntimeImage = rewritePlanRuntimeImage(t, request.Opcodes)
	request.RuntimeImage.Sections[2].Flags |= elf.SHF_EXECINSTR
	before := cloneRuntimeImageData(request.RuntimeImage)

	if _, err := buildRewritePlan(request, analysis, preparation); err == nil || !strings.Contains(err.Error(), "writable executable") {
		t.Fatalf("err=%v", err)
	}
	if !equalRuntimeImageData(request.RuntimeImage, before) {
		t.Fatal("failed runtime layout planning mutated runtime image")
	}
}

func TestRuntimeRelocationFormulaVectors(t *testing.T) {
	tests := []struct {
		name  string
		type_ elf.R_AARCH64
		word  uint32
		S     uint64
		A     int64
		P     uint64
		want  uint32
	}{
		{name: "prel32", type_: elf.R_AARCH64_PREL32, S: 0x5010, P: 0x5000, want: 0x10},
		{name: "ld-prel-lo19", type_: elf.R_AARCH64_LD_PREL_LO19, word: 0x58000000, S: 0x5100, P: 0x5000, want: 0x58000800},
		{name: "adr-prel-lo21", type_: elf.R_AARCH64_ADR_PREL_LO21, word: 0x10000000, S: 0x5100, P: 0x5000, want: 0x10000800},
		{name: "adr-page-hi21", type_: elf.R_AARCH64_ADR_PREL_PG_HI21, word: 0x90000000, S: 0x9000, P: 0x5000, want: 0x90000020},
		{name: "add-lo12", type_: elf.R_AARCH64_ADD_ABS_LO12_NC, word: 0x91000000, S: 0x5128, want: 0x9104a000},
		{name: "call26", type_: elf.R_AARCH64_CALL26, word: 0x94000000, S: 0x5100, P: 0x5000, want: 0x94000040},
		{name: "jump26", type_: elf.R_AARCH64_JUMP26, word: 0x14000000, S: 0x4f00, P: 0x5000, want: 0x17ffffc0},
		{name: "ldst64-lo12", type_: elf.R_AARCH64_LDST64_ABS_LO12_NC, word: 0xf9400000, S: 0x5128, want: 0xf9409400},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			data := make([]byte, 8)
			binary.LittleEndian.PutUint32(data, test.word)
			if err := applyAArch64Relocation(data, 0, test.type_, test.S, test.A, test.P); err != nil {
				t.Fatal(err)
			}
			if got := binary.LittleEndian.Uint32(data); got != test.want {
				t.Fatalf("relocated word=0x%08x, want 0x%08x", got, test.want)
			}
		})
	}

	abs := make([]byte, 8)
	if err := applyAArch64Relocation(abs, 0, elf.R_AARCH64_ABS64, 0x1000000000001234, 0x10, 0); err != nil {
		t.Fatal(err)
	}
	if got := binary.LittleEndian.Uint64(abs); got != 0x1000000000001244 {
		t.Fatalf("ABS64=0x%x", got)
	}

	unaligned := make([]byte, 4)
	if err := applyAArch64Relocation(unaligned, 0, elf.R_AARCH64_LDST64_ABS_LO12_NC, 0x5124, 0, 0); err == nil {
		t.Fatal("unaligned LDST64 relocation was accepted")
	}
	far := make([]byte, 4)
	if err := applyAArch64Relocation(far, 0, elf.R_AARCH64_CALL26, 0x08000000, 0, 0); err == nil {
		t.Fatal("out-of-range CALL26 relocation was accepted")
	}
}

func TestRuntimeRelocationRejectsWrongInstructionClass(t *testing.T) {
	tests := []struct {
		name  string
		type_ elf.R_AARCH64
		word  uint32
	}{
		{name: "ld-prel-lo19", type_: elf.R_AARCH64_LD_PREL_LO19, word: 0xd503201f},
		{name: "got-ld-prel19", type_: elf.R_AARCH64_GOT_LD_PREL19, word: 0xd503201f},
		{name: "adr", type_: elf.R_AARCH64_ADR_PREL_LO21, word: 0xd503201f},
		{name: "adrp", type_: elf.R_AARCH64_ADR_PREL_PG_HI21, word: 0xd503201f},
		{name: "add-lo12", type_: elf.R_AARCH64_ADD_ABS_LO12_NC, word: 0xd503201f},
		{name: "jump26", type_: elf.R_AARCH64_JUMP26, word: 0x94000000},
		{name: "call26", type_: elf.R_AARCH64_CALL26, word: 0x14000000},
		{name: "ldst64-lo12", type_: elf.R_AARCH64_LDST64_ABS_LO12_NC, word: 0xd503201f},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			data := make([]byte, 4)
			binary.LittleEndian.PutUint32(data, test.word)
			before := append([]byte(nil), data...)
			if err := applyAArch64Relocation(data, 0, test.type_, 0x5100, 0, 0x5000); err == nil || !strings.Contains(err.Error(), "instruction class") {
				t.Fatalf("err=%v", err)
			}
			if !bytes.Equal(data, before) {
				t.Fatal("failed relocation validation mutated target bytes")
			}
		})
	}
}

func TestProgramHeaderPlanPrefersTrailingPTNULLAndRelocatesUnsafeGrowth(t *testing.T) {
	fixture := buildELFFixture(fixtureOptions{dynamic: true})
	bo := binary.LittleEndian
	bo.PutUint16(fixture.data[56:58], 5)
	request, analysis, preparation := rewritePlanPreparation(t, fixture, false)
	request.RuntimeImage = rewritePlanRuntimeImage(t, request.Opcodes)

	plan, err := buildRewritePlan(request, analysis, preparation)
	if err != nil {
		t.Fatal(err)
	}
	if plan.programHeaders.phnumBefore != 5 || plan.programHeaders.phnumAfter != 5 || len(plan.programHeaders.newLoads) != 3 {
		t.Fatalf("PHDR plan=%+v", plan.programHeaders)
	}
	for i, mutation := range plan.programHeaders.newLoads {
		if mutation.index != i+2 || mutation.source != programHeaderSourceNull {
			t.Fatalf("new load[%d]=%+v", i, mutation)
		}
	}

	unsafe := buildELFFixture(fixtureOptions{dynamic: true})
	oldEnd := unsafe.phoff + 2*elf64ProgramSize
	unsafe.data[oldEnd] = 0x7f
	request, analysis, preparation = rewritePlanPreparation(t, unsafe, false)
	request.RuntimeImage = rewritePlanRuntimeImage(t, request.Opcodes)
	before := append([]byte(nil), request.Input...)
	plan, err = buildRewritePlan(request, analysis, preparation)
	if err != nil {
		t.Fatalf("relocate occupied PHDR growth: %v", err)
	}
	if !bytes.Equal(request.Input, before) {
		t.Fatal("relocated PHDR planning mutated input")
	}
	if !plan.programHeaders.relocated || plan.programHeaders.phoffAfter == plan.programHeaders.phoffBefore {
		t.Fatalf("PHDR relocation plan=%+v", plan.programHeaders)
	}
	if got, want := len(plan.programHeaders.tableData), int(plan.programHeaders.phnumAfter)*elf64ProgramSize; got != want {
		t.Fatalf("relocated PHDR table bytes=%d, want %d", got, want)
	}
	ro := plan.segments[rewriteSegmentR]
	if plan.programHeaders.phoffAfter < ro.fileOffset || plan.programHeaders.phoffAfter+uint64(len(plan.programHeaders.tableData)) > ro.fileOffset+ro.fileSize {
		t.Fatalf("relocated PHDR table is outside planned R segment: phoff=0x%x segment=%+v", plan.programHeaders.phoffAfter, ro)
	}
	tableOffset := plan.programHeaders.phoffAfter - ro.fileOffset
	if !bytes.Equal(ro.data[tableOffset:tableOffset+uint64(len(plan.programHeaders.tableData))], plan.programHeaders.tableData) {
		t.Fatal("relocated PHDR bytes were not materialized into planned R segment")
	}
}

func TestProgramHeaderRelocationUpdatesPTPHDR(t *testing.T) {
	const phnum = 3
	input := make([]byte, 0x300)
	bo := binary.LittleEndian
	bo.PutUint64(input[32:40], elf64HeaderSize)
	bo.PutUint16(input[54:56], elf64ProgramSize)
	bo.PutUint16(input[56:58], phnum)
	writeProgram := func(index int, type_ elf.ProgType, flags elf.ProgFlag, off, vaddr, filesz, memsz, align uint64) {
		base := elf64HeaderSize + index*elf64ProgramSize
		bo.PutUint32(input[base:base+4], uint32(type_))
		bo.PutUint32(input[base+4:base+8], uint32(flags))
		bo.PutUint64(input[base+8:base+16], off)
		bo.PutUint64(input[base+16:base+24], vaddr)
		bo.PutUint64(input[base+24:base+32], vaddr)
		bo.PutUint64(input[base+32:base+40], filesz)
		bo.PutUint64(input[base+40:base+48], memsz)
		bo.PutUint64(input[base+48:base+56], align)
	}
	oldTableSize := uint64(phnum * elf64ProgramSize)
	writeProgram(0, elf.PT_PHDR, elf.PF_R, elf64HeaderSize, 0x1040, oldTableSize, oldTableSize, 8)
	writeProgram(1, elf.PT_LOAD, elf.PF_R|elf.PF_X, 0, 0x1000, uint64(len(input)), uint64(len(input)), 0x1000)
	writeProgram(2, elf.PT_NULL, 0, 0, 0, 0, 0, 0)
	input[elf64HeaderSize+phnum*elf64ProgramSize] = 0x7f
	segments := []rewriteSegment{
		{flags: elf.PF_R | elf.PF_X, fileOffset: 0x4000, vaddr: 0x5000, fileSize: 0x100, memSize: 0x100, data: make([]byte, 0x100)},
		{flags: elf.PF_R | elf.PF_W, fileOffset: 0x8000, vaddr: 0x9000, fileSize: 0x100, memSize: 0x100, data: make([]byte, 0x100)},
		{flags: elf.PF_R, fileOffset: 0xc000, vaddr: 0xd000, fileSize: 0x100, memSize: 0x100, data: make([]byte, 0x100)},
	}

	plan, err := planProgramHeaders(input, &elfMetadata{}, segments)
	if err != nil {
		t.Fatal(err)
	}
	if !plan.relocated || plan.phdrUpdate == nil || plan.phdrUpdate.index != 0 {
		t.Fatalf("PT_PHDR relocation plan=%+v", plan)
	}
	if plan.phdrUpdate.header.off != plan.phoffAfter || plan.phdrUpdate.header.vaddr != plan.phdrTableVA ||
		plan.phdrUpdate.header.filesz != uint64(len(plan.tableData)) || plan.phdrUpdate.header.memsz != uint64(len(plan.tableData)) {
		t.Fatalf("PT_PHDR update=%+v plan=%+v", plan.phdrUpdate.header, plan)
	}
}

func TestProgramHeaderPlanRejectsUnsortedExistingLoadsWithoutMutation(t *testing.T) {
	input := make([]byte, elf64HeaderSize+3*elf64ProgramSize)
	bo := binary.LittleEndian
	bo.PutUint64(input[32:40], elf64HeaderSize)
	bo.PutUint16(input[54:56], elf64ProgramSize)
	bo.PutUint16(input[56:58], 3)
	writeLoad := func(index int, vaddr uint64) {
		off := elf64HeaderSize + index*elf64ProgramSize
		bo.PutUint32(input[off:off+4], uint32(elf.PT_LOAD))
		bo.PutUint64(input[off+16:off+24], vaddr)
	}
	writeLoad(0, 0x3000)
	writeLoad(1, 0x2000)
	before := append([]byte(nil), input...)

	_, err := planProgramHeaders(input, &elfMetadata{}, []rewriteSegment{{flags: elf.PF_R, fileOffset: 0x4000, vaddr: 0x4000, fileSize: 0x100, memSize: 0x100}})
	if err == nil || !strings.Contains(err.Error(), "PT_LOAD") || !strings.Contains(err.Error(), "order") {
		t.Fatalf("err=%v", err)
	}
	if !bytes.Equal(input, before) {
		t.Fatal("unsorted PT_LOAD validation mutated input")
	}
}

func TestProcessAnalyzedRejectsAnalysisForDifferentInput(t *testing.T) {
	first := buildELFFixture(fixtureOptions{dynamic: true, code: []uint32{0xD503201F, 0xD503201F, 0xD65F03C0}})
	second := buildELFFixture(fixtureOptions{dynamic: true, code: []uint32{0x91000400, 0xD503201F, 0xD65F03C0}})
	request, analysis, preparation := rewritePlanPreparation(t, first, false)
	request.Input = second.data
	request.RuntimeImage = rewritePlanRuntimeImage(t, request.Opcodes)
	request.Preparation = preparation
	before := append([]byte(nil), request.Input...)

	result, err := ProcessAnalyzed(request, analysis)
	if err == nil || !strings.Contains(err.Error(), "input provenance") || len(result.Artifact) != 0 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if !bytes.Equal(request.Input, before) {
		t.Fatal("input provenance failure mutated input")
	}
}

func rewritePlanPreparation(t *testing.T, fixture elfFixture, wantBTI bool) (Request, Analysis, *TranslationPreparation) {
	t.Helper()
	const entry = 0x1200
	end := entry + 12
	if wantBTI {
		end = entry + 16
	}
	request := Request{
		Input: fixture.data, Selections: []SelectionRequest{addressSelection(entry, uint64(end))}, Opcodes: vm.IdentityOpcodeMap(),
	}
	analysis, err := Analyze(request)
	if err != nil {
		t.Fatal(err)
	}
	preparation, err := PrepareTranslations(request, analysis)
	if err != nil {
		t.Fatal(err)
	}
	if preparation.Functions[0].Translation.HasEntryBTI != wantBTI {
		t.Fatalf("HasEntryBTI=%v, want %v", preparation.Functions[0].Translation.HasEntryBTI, wantBTI)
	}
	return request, analysis, preparation
}

func rewritePlanRuntimeImage(t *testing.T, opcodes vm.OpcodeMap) *vmruntime.Image {
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
			{Index: 3, Name: ".rodata", Type: elf.SHT_PROGBITS, Flags: elf.SHF_ALLOC, Alignment: 8, Size: 8, Data: make([]byte, 8)},
			{Index: 4, Name: ".eh_frame", Type: elf.SHT_PROGBITS, Flags: elf.SHF_ALLOC, Alignment: 8, Size: 8, Data: make([]byte, 8)},
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
		Relocations: []vmruntime.Relocation{{TargetIndex: 3, Offset: 0, Type: elf.R_AARCH64_ABS64, SymbolIndex: 1}},
	}
}

func cloneRuntimeImageData(image *vmruntime.Image) [][]byte {
	clones := make([][]byte, len(image.Sections))
	for i := range image.Sections {
		clones[i] = append([]byte(nil), image.Sections[i].Data...)
	}
	return clones
}

func equalRuntimeImageData(image *vmruntime.Image, clones [][]byte) bool {
	if len(image.Sections) != len(clones) {
		return false
	}
	for i := range image.Sections {
		if !bytes.Equal(image.Sections[i].Data, clones[i]) {
			return false
		}
	}
	return true
}

func mustPlannedSymbolVA(t *testing.T, plan *RewritePlan, name string) uint64 {
	t.Helper()
	for _, symbol := range plan.symbols {
		if symbol.name == name {
			return symbol.vaddr
		}
	}
	t.Fatalf("missing planned symbol %q", name)
	return 0
}

func plannedRuntimeUint64(t *testing.T, plan *RewritePlan, sectionName string, offset uint64) uint64 {
	t.Helper()
	for _, section := range plan.runtimeSections {
		if section.name != sectionName {
			continue
		}
		segment := plan.segments[section.segment]
		start := section.segmentOffset + offset
		if start+8 > uint64(len(segment.data)) {
			t.Fatalf("section %q read exceeds segment", sectionName)
		}
		return binary.LittleEndian.Uint64(segment.data[start : start+8])
	}
	t.Fatalf("missing runtime section %q", sectionName)
	return 0
}

func originalInstructionContaining(t *testing.T, translation *arm64.TranslateResult, opcodes vm.OpcodeMap, offset int) (int, int) {
	t.Helper()
	for pc := 0; pc < translation.CodeLen; {
		opcode, err := opcodes.Decode(translation.Bytecode[pc])
		if err != nil {
			t.Fatal(err)
		}
		size := vm.InstructionSize(opcode)
		if size == 0 || pc+size > translation.CodeLen {
			t.Fatalf("invalid VM instruction at 0x%x", pc)
		}
		if offset >= pc && offset+8 <= pc+size {
			return pc, size
		}
		pc += size
	}
	t.Fatalf("relocation offset %d is not contained by one VM instruction", offset)
	return 0, 0
}
