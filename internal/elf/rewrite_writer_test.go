package elf

import (
	"bytes"
	"encoding/binary"
	"math"
	"reflect"
	"testing"
)

func TestApplyRewritePlanMaterializesValidatedPlan(t *testing.T) {
	fixture := buildELFFixture(fixtureOptions{dynamic: true})
	request, analysis, preparation := rewritePlanPreparation(t, fixture, false)
	request.RuntimeImage = rewritePlanRuntimeImage(t, request.Opcodes)
	plan, err := buildRewritePlan(request, analysis, preparation)
	if err != nil {
		t.Fatal(err)
	}

	inputBefore := append([]byte(nil), request.Input...)
	sectionHeadersBefore := copySectionHeaderTable(t, request.Input)
	planBefore := cloneRewritePlanForTest(plan)

	artifact, err := applyRewritePlan(request.Input, plan)
	if err != nil {
		t.Fatal(err)
	}
	if len(artifact) == 0 || &artifact[0] == &request.Input[0] {
		t.Fatal("writer did not return an independent artifact buffer")
	}
	if !bytes.Equal(request.Input, inputBefore) {
		t.Fatal("writer mutated the caller input")
	}
	if !reflect.DeepEqual(plan, planBefore) {
		t.Fatal("writer mutated the rewrite plan")
	}

	for i, segment := range plan.segments {
		end := segment.fileOffset + segment.fileSize
		if end > uint64(len(artifact)) || !bytes.Equal(artifact[segment.fileOffset:end], segment.data) {
			t.Fatalf("segment %d was not materialized exactly", i)
		}
	}
	phdr := plan.programHeaders
	phdrEnd := phdr.phoffAfter + uint64(len(phdr.tableData))
	if phdrEnd > uint64(len(artifact)) || !bytes.Equal(artifact[phdr.phoffAfter:phdrEnd], phdr.tableData) {
		t.Fatal("final program-header table was not materialized exactly")
	}
	if got := binary.LittleEndian.Uint64(artifact[32:40]); got != phdr.phoffAfter {
		t.Fatalf("e_phoff=0x%x, want 0x%x", got, phdr.phoffAfter)
	}
	if got := binary.LittleEndian.Uint16(artifact[56:58]); got != phdr.phnumAfter {
		t.Fatalf("e_phnum=%d, want %d", got, phdr.phnumAfter)
	}
	for i, function := range plan.functions {
		end := function.entryFileOffset + uint64(len(function.entryPatch))
		if end > uint64(len(artifact)) || !bytes.Equal(artifact[function.entryFileOffset:end], function.entryPatch) {
			t.Fatalf("function entry patch %d was not materialized exactly", i)
		}
	}
	if got := copySectionHeaderTable(t, artifact); !bytes.Equal(got, sectionHeadersBefore) {
		t.Fatal("writer changed the section-header table")
	}
	assertRewrittenArtifactParses(t, artifact, analysis.TargetKind)
}

func TestApplyRewritePlanSupportsProgramHeaderStrategies(t *testing.T) {
	tests := []struct {
		name          string
		prepare       func(*elfFixture)
		wantBefore    uint16
		wantAfter     uint16
		wantRelocated bool
	}{
		{
			name: "reuse trailing PT_NULL",
			prepare: func(fixture *elfFixture) {
				binary.LittleEndian.PutUint16(fixture.data[56:58], 5)
			},
			wantBefore: 5,
			wantAfter:  5,
		},
		{name: "grow in place", wantBefore: 2, wantAfter: 5},
		{
			name: "relocate occupied growth",
			prepare: func(fixture *elfFixture) {
				phnum := int(binary.LittleEndian.Uint16(fixture.data[56:58]))
				fixture.data[fixture.phoff+phnum*elf64ProgramSize] = 0x7f
			},
			wantBefore:    2,
			wantAfter:     5,
			wantRelocated: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := buildELFFixture(fixtureOptions{dynamic: true})
			if test.prepare != nil {
				test.prepare(&fixture)
			}
			request, analysis, preparation := rewritePlanPreparation(t, fixture, false)
			request.RuntimeImage = rewritePlanRuntimeImage(t, request.Opcodes)
			plan, err := buildRewritePlan(request, analysis, preparation)
			if err != nil {
				t.Fatal(err)
			}
			phdr := plan.programHeaders
			if phdr.phnumBefore != test.wantBefore || phdr.phnumAfter != test.wantAfter || phdr.relocated != test.wantRelocated {
				t.Fatalf("program-header plan=%+v", phdr)
			}
			if test.wantRelocated == (phdr.phoffAfter == phdr.phoffBefore) {
				t.Fatalf("unexpected PHDR placement before=0x%x after=0x%x relocated=%v", phdr.phoffBefore, phdr.phoffAfter, phdr.relocated)
			}

			artifact, err := applyRewritePlan(request.Input, plan)
			if err != nil {
				t.Fatal(err)
			}
			assertRewrittenArtifactParses(t, artifact, analysis.TargetKind)
		})
	}
}

func TestApplyRewritePlanRejectsInconsistentInputOrPlan(t *testing.T) {
	fixture := buildELFFixture(fixtureOptions{dynamic: true})
	request, analysis, preparation := rewritePlanPreparation(t, fixture, false)
	request.RuntimeImage = rewritePlanRuntimeImage(t, request.Opcodes)
	base, err := buildRewritePlan(request, analysis, preparation)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name   string
		mutate func([]byte, *RewritePlan) ([]byte, *RewritePlan)
	}{
		{name: "nil plan", mutate: func(input []byte, _ *RewritePlan) ([]byte, *RewritePlan) { return input, nil }},
		{name: "truncated ELF header", mutate: func(_ []byte, plan *RewritePlan) ([]byte, *RewritePlan) { return make([]byte, elf64HeaderSize-1), plan }},
		{name: "input PHDR offset changed", mutate: func(input []byte, plan *RewritePlan) ([]byte, *RewritePlan) {
			binary.LittleEndian.PutUint64(input[32:40], plan.programHeaders.phoffBefore+8)
			return input, plan
		}},
		{name: "final PHDR table length mismatch", mutate: func(input []byte, plan *RewritePlan) ([]byte, *RewritePlan) {
			plan.programHeaders.tableData = plan.programHeaders.tableData[:len(plan.programHeaders.tableData)-1]
			return input, plan
		}},
		{name: "segment size mismatch", mutate: func(input []byte, plan *RewritePlan) ([]byte, *RewritePlan) {
			plan.segments[0].fileSize++
			return input, plan
		}},
		{name: "segment range overflow", mutate: func(input []byte, plan *RewritePlan) ([]byte, *RewritePlan) {
			plan.segments[0].fileOffset = math.MaxUint64
			return input, plan
		}},
		{name: "entry patch outside input", mutate: func(input []byte, plan *RewritePlan) ([]byte, *RewritePlan) {
			plan.functions[0].entryFileOffset = uint64(len(input))
			return input, plan
		}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := append([]byte(nil), request.Input...)
			plan := cloneRewritePlanForTest(base)
			input, plan = test.mutate(input, plan)
			inputBefore := append([]byte(nil), input...)

			artifact, err := applyRewritePlan(input, plan)
			if err == nil || artifact != nil {
				t.Fatalf("artifact=%v err=%v", artifact, err)
			}
			if !bytes.Equal(input, inputBefore) {
				t.Fatal("failed writer mutated input")
			}
		})
	}
}

func TestApplyRewritePlanRejectsRelocatedPHDRMismatch(t *testing.T) {
	fixture := buildELFFixture(fixtureOptions{dynamic: true})
	phnum := int(binary.LittleEndian.Uint16(fixture.data[56:58]))
	fixture.data[fixture.phoff+phnum*elf64ProgramSize] = 0x7f
	request, analysis, preparation := rewritePlanPreparation(t, fixture, false)
	request.RuntimeImage = rewritePlanRuntimeImage(t, request.Opcodes)
	plan, err := buildRewritePlan(request, analysis, preparation)
	if err != nil {
		t.Fatal(err)
	}
	if !plan.programHeaders.relocated {
		t.Fatal("fixture did not produce a relocated PHDR table")
	}

	plan = cloneRewritePlanForTest(plan)
	plan.programHeaders.tableData[0] ^= 0xff
	artifact, err := applyRewritePlan(request.Input, plan)
	if err == nil || artifact != nil {
		t.Fatalf("artifact=%v err=%v", artifact, err)
	}
}

func assertRewrittenArtifactParses(t *testing.T, artifact []byte, want TargetKind) {
	t.Helper()
	meta, err := parseELFMetadata(artifact, AndroidModeAuto)
	if err != nil {
		t.Fatalf("reparse rewritten artifact: %v", err)
	}
	defer meta.file.Close()
	if meta.kind != want {
		t.Fatalf("reparsed target kind=%s, want %s", meta.kind, want)
	}
}

func copySectionHeaderTable(t *testing.T, data []byte) []byte {
	t.Helper()
	if len(data) < elf64HeaderSize {
		t.Fatal("ELF header is truncated")
	}
	bo := binary.LittleEndian
	shoff := bo.Uint64(data[40:48])
	shentsize := uint64(bo.Uint16(data[58:60]))
	shnum := uint64(bo.Uint16(data[60:62]))
	size, ok := checkedMul(shentsize, shnum)
	if !ok {
		t.Fatal("section-header size overflows")
	}
	end, ok := checkedAdd(shoff, size)
	if !ok || end > uint64(len(data)) {
		t.Fatal("section-header table is out of bounds")
	}
	return append([]byte(nil), data[shoff:end]...)
}

func cloneRewritePlanForTest(plan *RewritePlan) *RewritePlan {
	if plan == nil {
		return nil
	}
	clone := *plan
	clone.segments = append([]rewriteSegment(nil), plan.segments...)
	for i := range clone.segments {
		clone.segments[i].data = append([]byte(nil), plan.segments[i].data...)
	}
	clone.runtimeSections = append([]runtimeSectionPlan(nil), plan.runtimeSections...)
	clone.symbols = append([]runtimeSymbolPlan(nil), plan.symbols...)
	clone.functions = append([]functionRewritePlan(nil), plan.functions...)
	for i := range clone.functions {
		clone.functions[i].entryPatch = append([]byte(nil), plan.functions[i].entryPatch...)
	}
	clone.programHeaders.tableData = append([]byte(nil), plan.programHeaders.tableData...)
	clone.programHeaders.newLoads = append([]programHeaderMutation(nil), plan.programHeaders.newLoads...)
	if plan.programHeaders.phdrUpdate != nil {
		update := *plan.programHeaders.phdrUpdate
		clone.programHeaders.phdrUpdate = &update
	}
	return &clone
}
