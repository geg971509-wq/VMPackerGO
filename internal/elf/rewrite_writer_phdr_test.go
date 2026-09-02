package elf

import (
	"bytes"
	"debug/elf"
	"encoding/binary"
	"testing"
)

func TestApplyRewritePlanMaterializesRelocatedPTPHDR(t *testing.T) {
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
	if got := copySectionHeaderTable(t, artifact); !bytes.Equal(got, sectionHeadersBefore) {
		t.Fatal("relocated program-header write changed section headers")
	}
	assertRewrittenArtifactParses(t, artifact, TargetKindAndroidSO)
}
