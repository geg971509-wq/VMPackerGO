package elf

import (
	"bytes"
	"debug/elf"
	"encoding/binary"
	"strings"
	"testing"
)

func relocatedPTPHDRFixture(t *testing.T) ([]byte, []rewriteSegment, programHeaderPlan) {
	t.Helper()
	fixture := buildELFFixture(fixtureOptions{dynamic: true})
	input := append([]byte(nil), fixture.data...)
	bo := binary.LittleEndian
	oldPhnum := int(bo.Uint16(input[56:58]))
	oldTableEnd := fixture.phoff + oldPhnum*elf64ProgramSize
	newPhnum := oldPhnum + 1
	newTableEnd := fixture.phoff + newPhnum*elf64ProgramSize
	copy(input[fixture.phoff+elf64ProgramSize:newTableEnd], input[fixture.phoff:oldTableEnd])
	bo.PutUint16(input[56:58], uint16(newPhnum))

	phdrVA := uint64(0x1000 + fixture.phoff)
	phdrSize := uint64(newPhnum * elf64ProgramSize)
	phdr := input[fixture.phoff : fixture.phoff+elf64ProgramSize]
	bo.PutUint32(phdr[0:4], uint32(elf.PT_PHDR))
	bo.PutUint32(phdr[4:8], uint32(elf.PF_R))
	bo.PutUint64(phdr[8:16], uint64(fixture.phoff))
	bo.PutUint64(phdr[16:24], phdrVA)
	bo.PutUint64(phdr[24:32], phdrVA)
	bo.PutUint64(phdr[32:40], phdrSize)
	bo.PutUint64(phdr[40:48], phdrSize)
	bo.PutUint64(phdr[48:56], 8)

	meta, err := parseELFMetadata(input, AndroidModeAuto)
	if err != nil {
		t.Fatal(err)
	}
	defer meta.file.Close()
	segments := []rewriteSegment{
		{flags: elf.PF_R | elf.PF_X, fileOffset: 0x4000, vaddr: 0x4000, fileSize: 0x100, memSize: 0x100, data: make([]byte, 0x100)},
		{flags: elf.PF_R | elf.PF_W, fileOffset: 0x8000, vaddr: 0x8000, fileSize: 0x100, memSize: 0x100, data: make([]byte, 0x100)},
		{flags: elf.PF_R, fileOffset: 0xc000, vaddr: 0xc000, fileSize: 0x100, memSize: 0x100, data: make([]byte, 0x100)},
	}
	phdrPlan, err := planProgramHeaders(input, meta, segments)
	if err != nil {
		t.Fatal(err)
	}
	if !phdrPlan.relocated || phdrPlan.phdrUpdate == nil || phdrPlan.phdrUpdate.index != 0 {
		t.Fatalf("PT_PHDR relocation plan=%+v", phdrPlan)
	}
	return input, segments, phdrPlan
}

func TestApplyRewritePlanMaterializesRelocatedPTPHDR(t *testing.T) {
	input, segments, phdrPlan := relocatedPTPHDRFixture(t)
	bo := binary.LittleEndian
	inputBefore := append([]byte(nil), input...)
	sectionHeadersBefore := copySectionHeaderTable(t, input)
	artifact, err := applyRewritePlan(input, &RewritePlan{segments: segments, programHeaders: phdrPlan})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(input, inputBefore) {
		t.Fatal("writer modified the PT_PHDR fixture")
	}
	if got := bo.Uint64(artifact[32:40]); got != phdrPlan.phoffAfter {
		t.Fatalf("e_phoff=0x%x, want 0x%x", got, phdrPlan.phoffAfter)
	}
	if got := bo.Uint16(artifact[56:58]); got != phdrPlan.phnumAfter {
		t.Fatalf("e_phnum=%d, want %d", got, phdrPlan.phnumAfter)
	}
	tableEnd := phdrPlan.phoffAfter + uint64(len(phdrPlan.tableData))
	if !bytes.Equal(artifact[phdrPlan.phoffAfter:tableEnd], phdrPlan.tableData) {
		t.Fatal("relocated program-header table differs from the validated plan")
	}
	ptPHDR := readPlannedProgramHeader(artifact, phdrPlan.phoffAfter)
	if ptPHDR != phdrPlan.phdrUpdate.header {
		t.Fatalf("PT_PHDR=%+v, want %+v", ptPHDR, phdrPlan.phdrUpdate.header)
	}
	if err := validateAndroidLoadedProgramHeaders(artifact); err != nil {
		t.Fatalf("Android loaded-PHDR invariant: %v", err)
	}
	if got := copySectionHeaderTable(t, artifact); !bytes.Equal(got, sectionHeadersBefore) {
		t.Fatal("relocated program-header write changed section headers")
	}
	assertRewrittenArtifactParses(t, artifact, TargetKindAndroidSO)
}

func TestApplyRewritePlanResynchronizesRelocatedPHDRLoadExtent(t *testing.T) {
	input, segments, phdrPlan := relocatedPTPHDRFixture(t)
	if len(phdrPlan.newLoads) != len(segments) {
		t.Fatalf("new loads=%d segments=%d", len(phdrPlan.newLoads), len(segments))
	}

	// Reproduce the device failure shape: the RewritePlan segment has already
	// grown to contain the relocated PHDR table, but the serialized trailing
	// PT_LOAD still advertises the pre-PHDR p_filesz/p_memsz boundary.
	last := len(segments) - 1
	loadIndex := phdrPlan.newLoads[last].index
	entryOff := uint64(loadIndex) * elf64ProgramSize
	load := readPlannedProgramHeader(phdrPlan.tableData, entryOff)
	if load.type_ != elf.PT_LOAD || phdrPlan.phoffAfter < load.off {
		t.Fatalf("unexpected relocated load=%+v phoff=0x%x", load, phdrPlan.phoffAfter)
	}
	staleSize := phdrPlan.phoffAfter - load.off
	load.filesz = staleSize
	load.memsz = staleSize
	if err := encodePlannedProgramHeaderAt(phdrPlan.tableData, loadIndex, load); err != nil {
		t.Fatal(err)
	}
	segmentTableOff := phdrPlan.phoffAfter - segments[last].fileOffset
	copy(segments[last].data[segmentTableOff:segmentTableOff+uint64(len(phdrPlan.tableData))], phdrPlan.tableData)
	stalePlanTable := append([]byte(nil), phdrPlan.tableData...)

	artifact, err := applyRewritePlan(input, &RewritePlan{segments: segments, programHeaders: phdrPlan})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(phdrPlan.tableData, stalePlanTable) {
		t.Fatal("writer mutated the authoritative plan table while synchronizing serialization")
	}
	if err := validateAndroidLoadedProgramHeaders(artifact); err != nil {
		t.Fatalf("synchronized artifact is not Android-loadable: %v", err)
	}
	finalLoadOff := phdrPlan.phoffAfter + uint64(loadIndex)*elf64ProgramSize
	finalLoad := readPlannedProgramHeader(artifact, finalLoadOff)
	if finalLoad.filesz != segments[last].fileSize || finalLoad.memsz != segments[last].memSize {
		t.Fatalf("serialized PT_LOAD size file=0x%x mem=0x%x, want file=0x%x mem=0x%x",
			finalLoad.filesz, finalLoad.memsz, segments[last].fileSize, segments[last].memSize)
	}
}

func TestValidateAndroidLoadedProgramHeadersRejectsUnmappedPTPHDR(t *testing.T) {
	input, segments, phdrPlan := relocatedPTPHDRFixture(t)
	artifact, err := applyRewritePlan(input, &RewritePlan{segments: segments, programHeaders: phdrPlan})
	if err != nil {
		t.Fatal(err)
	}
	broken := append([]byte(nil), artifact...)
	phdrOff := phdrPlan.phoffAfter + uint64(phdrPlan.phdrUpdate.index)*elf64ProgramSize
	phdr := readPlannedProgramHeader(broken, phdrOff)
	phdr.vaddr += 0x100000
	phdr.paddr = phdr.vaddr
	entry := broken[phdrOff : phdrOff+elf64ProgramSize]
	binary.LittleEndian.PutUint64(entry[16:24], phdr.vaddr)
	binary.LittleEndian.PutUint64(entry[24:32], phdr.paddr)
	if err := validateAndroidLoadedProgramHeaders(broken); err == nil || !strings.Contains(err.Error(), "not covered by one PT_LOAD") {
		t.Fatalf("unmapped PT_PHDR err=%v", err)
	}
}
