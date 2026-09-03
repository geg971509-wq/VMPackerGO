#!/usr/bin/env python3
from pathlib import Path


def replace_once(path: str, old: str, new: str) -> None:
    p = Path(path)
    text = p.read_text()
    count = text.count(old)
    if count != 1:
        raise SystemExit(f"{path}: expected exactly one match, found {count}\n--- expected ---\n{old}")
    p.write_text(text.replace(old, new, 1))


# ---------------------------------------------------------------------------
# 1. Canonical GNU .eh_frame_hdr builder.
# ---------------------------------------------------------------------------
replace_once(
    "internal/unwind/header.go",
    '''import (
\t"encoding/binary"
\t"fmt"
)
''',
    '''import (
\t"encoding/binary"
\t"fmt"
\t"math"
\t"sort"
)
''',
)
Path("internal/unwind/header.go").write_text(Path("internal/unwind/header.go").read_text() + r'''

// BuildEHFrameHeader emits the canonical GNU/AArch64 search header used by the
// rewrite writer: pcrel+sdata4 .eh_frame pointer, udata4 count, and
// datarel+sdata4 sorted search-table pairs.
func BuildEHFrameHeader(address, ehFrameAddress uint64, entries []HeaderEntry) ([]byte, error) {
	if len(entries) > math.MaxUint32 {
		return nil, fmt.Errorf(".eh_frame_hdr entry count exceeds u32")
	}
	sorted := append([]HeaderEntry(nil), entries...)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].InitialLocation != sorted[j].InitialLocation {
			return sorted[i].InitialLocation < sorted[j].InitialLocation
		}
		return sorted[i].FDEAddress < sorted[j].FDEAddress
	})
	for i, entry := range sorted {
		if entry.FDEAddress == 0 {
			return nil, fmt.Errorf(".eh_frame_hdr entry %d has a zero FDE address", i)
		}
		if i > 0 && sorted[i-1].InitialLocation == entry.InitialLocation {
			return nil, fmt.Errorf(".eh_frame_hdr has duplicate initial location 0x%x", entry.InitialLocation)
		}
	}

	result := []byte{1, PEPcrel | PESdata4, PEUdata4, PEDatarel | PESdata4}
	delta, err := signed32Difference(ehFrameAddress, address+uint64(len(result)))
	if err != nil {
		return nil, fmt.Errorf(".eh_frame pointer: %w", err)
	}
	result = binary.LittleEndian.AppendUint32(result, uint32(delta))
	result = binary.LittleEndian.AppendUint32(result, uint32(len(sorted)))
	for i, entry := range sorted {
		initialDelta, err := signed32Difference(entry.InitialLocation, address)
		if err != nil {
			return nil, fmt.Errorf("table entry %d initial location: %w", i, err)
		}
		fdeDelta, err := signed32Difference(entry.FDEAddress, address)
		if err != nil {
			return nil, fmt.Errorf("table entry %d FDE address: %w", i, err)
		}
		result = binary.LittleEndian.AppendUint32(result, uint32(initialDelta))
		result = binary.LittleEndian.AppendUint32(result, uint32(fdeDelta))
	}
	return result, nil
}

func signed32Difference(target, base uint64) (int32, error) {
	if target >= base {
		delta := target - base
		if delta > math.MaxInt32 {
			return 0, fmt.Errorf("positive signed-32 displacement overflows")
		}
		return int32(delta), nil
	}
	delta := base - target
	if delta > uint64(1)<<31 {
		return 0, fmt.Errorf("negative signed-32 displacement overflows")
	}
	if delta == uint64(1)<<31 {
		return math.MinInt32, nil
	}
	return -int32(delta), nil
}
''')

# Add focused builder tests before malformed-input test.
replace_once(
    "internal/unwind/unwind_test.go",
    '''func TestUnwindParsersFailClosedOnMalformedInput(t *testing.T) {
''',
    r'''func TestBuildEHFrameHeaderCanonicalRoundTrip(t *testing.T) {
	const address = uint64(0x8000)
	entries := []HeaderEntry{
		{InitialLocation: 0xa000, FDEAddress: 0x8300},
		{InitialLocation: 0x9000, FDEAddress: 0x8200},
	}
	before := append([]HeaderEntry(nil), entries...)
	data, err := BuildEHFrameHeader(address, 0x7000, entries)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) != 12+len(entries)*8 || data[0] != 1 || data[1] != PEPcrel|PESdata4 || data[2] != PEUdata4 || data[3] != PEDatarel|PESdata4 {
		t.Fatalf("header bytes=%x", data)
	}
	parsed, err := ParseEHFrameHeader(data, address, binary.LittleEndian, 8)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.EHFrameAddress != 0x7000 || len(parsed.Entries) != 2 ||
		parsed.Entries[0].InitialLocation != 0x9000 || parsed.Entries[0].FDEAddress != 0x8200 ||
		parsed.Entries[1].InitialLocation != 0xa000 || parsed.Entries[1].FDEAddress != 0x8300 {
		t.Fatalf("parsed=%+v", parsed)
	}
	for i := range entries {
		if entries[i] != before[i] {
			t.Fatal("BuildEHFrameHeader mutated caller entries")
		}
	}
}

func TestBuildEHFrameHeaderRejectsDuplicateAndOutOfRangeEntries(t *testing.T) {
	if _, err := BuildEHFrameHeader(0x8000, 0x7000, []HeaderEntry{
		{InitialLocation: 0x9000, FDEAddress: 0x8200},
		{InitialLocation: 0x9000, FDEAddress: 0x8300},
	}); err == nil {
		t.Fatal("duplicate initial location was accepted")
	}
	if _, err := BuildEHFrameHeader(0x1000, 0x100000000, []HeaderEntry{{InitialLocation: 0x2000, FDEAddress: 0x3000}}); err == nil {
		t.Fatal("out-of-range .eh_frame displacement was accepted")
	}
	if _, err := BuildEHFrameHeader(0x1000, 0x2000, []HeaderEntry{{InitialLocation: 0x100000000, FDEAddress: 0x3000}}); err == nil {
		t.Fatal("out-of-range table displacement was accepted")
	}
}

func TestUnwindParsersFailClosedOnMalformedInput(t *testing.T) {
''',
)

# ---------------------------------------------------------------------------
# 2. Rewrite planner state and sequencing.
# ---------------------------------------------------------------------------
replace_once(
    "internal/elf/rewrite_plan.go",
    '''\t"github.com/vmpacker/internal/arch/arm64"
\tvmruntime "github.com/vmpacker/internal/runtime"
\t"github.com/vmpacker/internal/vm"
''',
    '''\t"github.com/vmpacker/internal/arch/arm64"
\tvmruntime "github.com/vmpacker/internal/runtime"
\t"github.com/vmpacker/internal/unwind"
\t"github.com/vmpacker/internal/vm"
''',
)
replace_once(
    "internal/elf/rewrite_plan.go",
    '''type runtimeSymbolPlan struct {
\tindex uint32
\tname  string
\tvaddr uint64
\tsize  uint64
}
''',
    '''type runtimeSymbolPlan struct {
\tindex uint32
\tname  string
\tvaddr uint64
\tsize  uint64
}

type gnuEHFramePlan struct {
\tprogramIndex    int
\toriginal        *unwind.FrameHeader
\truntimeFDECount int
\tsegmentOffset   uint64
\tfileOffset      uint64
\tvaddr           uint64
\tsize            uint64
\theader          []byte
}
''',
)
replace_once(
    "internal/elf/rewrite_plan.go",
    '''type programHeaderPlan struct {
\tphoffBefore uint64
\tphoffAfter  uint64
\tphdrTableVA uint64
\tphnumBefore uint16
\tphnumAfter  uint16
\trelocated   bool
\ttableData   []byte
\tnewLoads    []programHeaderMutation
\tphdrUpdate  *programHeaderMutation
}
''',
    '''type programHeaderPlan struct {
\tphoffBefore      uint64
\tphoffAfter       uint64
\tphdrTableVA      uint64
\tphnumBefore      uint16
\tphnumAfter       uint16
\trelocated        bool
\ttableData        []byte
\tnewLoads         []programHeaderMutation
\tphdrUpdate       *programHeaderMutation
\tgnuEHFrameUpdate *programHeaderMutation
}
''',
)
replace_once(
    "internal/elf/rewrite_plan.go",
    '''\tfunctions       []functionRewritePlan
\ttokenTableVA    uint64
\tprogramHeaders  programHeaderPlan
}
''',
    '''\tfunctions       []functionRewritePlan
\ttokenTableVA    uint64
\tgnuEHFrame      *gnuEHFramePlan
\tprogramHeaders  programHeaderPlan
}
''',
)
replace_once(
    "internal/elf/rewrite_plan.go",
    '''\tif err := planner.reserveRuntimeLayout(); err != nil {
\t\treturn nil, err
\t}
\tif err := planner.placeSegments(); err != nil {
''',
    '''\tif err := planner.reserveRuntimeLayout(); err != nil {
\t\treturn nil, err
\t}
\tif err := planner.reserveGNUUnwindIndex(); err != nil {
\t\treturn nil, err
\t}
\tif err := planner.placeSegments(); err != nil {
''',
)
replace_once(
    "internal/elf/rewrite_plan.go",
    '''\tif err := planner.applyRuntimeRelocations(); err != nil {
\t\treturn nil, err
\t}
\tif err := planner.finalizeRuntimeGlobalsAndFunctions(); err != nil {
''',
    '''\tif err := planner.applyRuntimeRelocations(); err != nil {
\t\treturn nil, err
\t}
\tif err := planner.materializeGNUUnwindIndex(); err != nil {
\t\treturn nil, err
\t}
\tif err := planner.finalizeRuntimeGlobalsAndFunctions(); err != nil {
''',
)
replace_once(
    "internal/elf/rewrite_plan.go",
    '''\tphdrs, err := planProgramHeaders(req.Input, meta, planner.plan.segments)
''',
    '''\tphdrs, err := planProgramHeaders(req.Input, meta, planner.plan.segments, planner.plan.gnuEHFrame)
''',
)

# Place final EH header VA/file offset when segments are placed.
replace_once(
    "internal/elf/rewrite_plan.go",
    '''\tplanner.plan.tokenTableVA = planner.plan.segments[rewriteSegmentR].vaddr + planner.tokenTableOffset
\tfor i := range planner.plan.functions {
''',
    '''\tplanner.plan.tokenTableVA = planner.plan.segments[rewriteSegmentR].vaddr + planner.tokenTableOffset
\tif planner.plan.gnuEHFrame != nil {
\t\tsegment := planner.plan.segments[rewriteSegmentR]
\t\tplanner.plan.gnuEHFrame.fileOffset = segment.fileOffset + planner.plan.gnuEHFrame.segmentOffset
\t\tplanner.plan.gnuEHFrame.vaddr = segment.vaddr + planner.plan.gnuEHFrame.segmentOffset
\t}
\tfor i := range planner.plan.functions {
''',
)

# Insert GNU unwind planner helpers before placeSegments.
marker = '''func (planner *rewritePlanner) placeSegments() error {
'''
helpers = r'''func (planner *rewritePlanner) reserveGNUUnwindIndex() error {
	index, fileOffset, vaddr, size, found, err := findGNUUnwindProgram(planner.req.Input)
	if err != nil {
		return err
	}
	if !found {
		return nil
	}
	if fileOffset > uint64(len(planner.req.Input)) || size > uint64(len(planner.req.Input))-fileOffset {
		return fmt.Errorf("PT_GNU_EH_FRAME exceeds input file")
	}
	original, err := unwind.ParseEHFrameHeader(planner.req.Input[fileOffset:fileOffset+size], vaddr, binary.LittleEndian, 8)
	if err != nil {
		return fmt.Errorf("parse target GNU unwind index: %w", err)
	}
	runtimeData, _, err := runtimeEHFrameSection(planner.image)
	if err != nil {
		return err
	}
	runtimeFrame, err := unwind.ParseEHFrame(runtimeData, 0, binary.LittleEndian, 8)
	if err != nil {
		return fmt.Errorf("parse runtime .eh_frame structure: %w", err)
	}
	if len(runtimeFrame.FDEs) == 0 {
		return fmt.Errorf("runtime .eh_frame has no FDEs for GNU unwind integration")
	}
	count := len(original.Entries) + len(runtimeFrame.FDEs)
	if count > math.MaxUint32 || count > (math.MaxInt-12)/8 {
		return fmt.Errorf("combined GNU unwind index is too large")
	}
	headerSize := 12 + count*8
	off, err := growAligned(&planner.plan.segments[rewriteSegmentR].data, 4, uint64(headerSize))
	if err != nil {
		return fmt.Errorf("reserve GNU unwind index: %w", err)
	}
	planner.plan.gnuEHFrame = &gnuEHFramePlan{
		programIndex: index, original: original, runtimeFDECount: len(runtimeFrame.FDEs),
		segmentOffset: uint64(off), size: uint64(headerSize),
	}
	return nil
}

func (planner *rewritePlanner) materializeGNUUnwindIndex() error {
	plan := planner.plan.gnuEHFrame
	if plan == nil {
		return nil
	}
	_, section, err := runtimeEHFrameSection(planner.image)
	if err != nil {
		return err
	}
	planIndex, ok := planner.sectionPlanByIndex[section.Index]
	if !ok {
		return fmt.Errorf("runtime .eh_frame is not in planned allocatable storage")
	}
	sectionPlan := planner.plan.runtimeSections[planIndex]
	segment := &planner.plan.segments[sectionPlan.segment]
	if sectionPlan.segmentOffset > uint64(len(segment.data)) || sectionPlan.size > uint64(len(segment.data))-sectionPlan.segmentOffset {
		return fmt.Errorf("planned runtime .eh_frame exceeds read-only segment")
	}
	frameBytes := segment.data[sectionPlan.segmentOffset : sectionPlan.segmentOffset+sectionPlan.size]
	frame, err := unwind.ParseEHFrame(frameBytes, sectionPlan.vaddr, binary.LittleEndian, 8)
	if err != nil {
		return fmt.Errorf("parse relocated runtime .eh_frame: %w", err)
	}
	if len(frame.FDEs) != plan.runtimeFDECount {
		return fmt.Errorf("runtime FDE count changed from %d to %d after relocation", plan.runtimeFDECount, len(frame.FDEs))
	}
	entries := append([]unwind.HeaderEntry(nil), plan.original.Entries...)
	for _, fde := range frame.FDEs {
		fdeVA, ok := checkedAdd(sectionPlan.vaddr, fde.Offset)
		if !ok {
			return fmt.Errorf("runtime FDE address overflows")
		}
		entries = append(entries, unwind.HeaderEntry{InitialLocation: fde.InitialLocation, FDEAddress: fdeVA})
	}
	header, err := unwind.BuildEHFrameHeader(plan.vaddr, plan.original.EHFrameAddress, entries)
	if err != nil {
		return fmt.Errorf("build final GNU unwind index: %w", err)
	}
	if uint64(len(header)) != plan.size {
		return fmt.Errorf("final GNU unwind index size changed from %d to %d", plan.size, len(header))
	}
	ro := &planner.plan.segments[rewriteSegmentR]
	if plan.segmentOffset > uint64(len(ro.data)) || plan.size > uint64(len(ro.data))-plan.segmentOffset {
		return fmt.Errorf("final GNU unwind index exceeds read-only segment")
	}
	copy(ro.data[plan.segmentOffset:plan.segmentOffset+plan.size], header)
	plan.header = append([]byte(nil), header...)
	return nil
}

func runtimeEHFrameSection(image *vmruntime.Image) ([]byte, vmruntime.Section, error) {
	if image == nil {
		return nil, vmruntime.Section{}, fmt.Errorf("runtime image is required")
	}
	found := false
	var section vmruntime.Section
	for _, candidate := range image.Sections {
		if candidate.Name != ".eh_frame" {
			continue
		}
		if found {
			return nil, vmruntime.Section{}, fmt.Errorf("runtime has duplicate .eh_frame sections")
		}
		found = true
		section = candidate
	}
	if !found || section.NOBITS || section.Flags&elf.SHF_ALLOC == 0 || len(section.Data) == 0 || uint64(len(section.Data)) != section.Size {
		return nil, vmruntime.Section{}, fmt.Errorf("runtime .eh_frame is unavailable for GNU unwind integration")
	}
	return section.Data, section, nil
}

func findGNUUnwindProgram(input []byte) (index int, fileOffset, vaddr, size uint64, found bool, err error) {
	if len(input) < elf64HeaderSize {
		return 0, 0, 0, 0, false, fmt.Errorf("ELF header is truncated")
	}
	bo := binary.LittleEndian
	phoff := bo.Uint64(input[32:40])
	phentsize := uint64(bo.Uint16(input[54:56]))
	phnum := uint64(bo.Uint16(input[56:58]))
	if phnum == 0 {
		return 0, 0, 0, 0, false, nil
	}
	if phentsize != elf64ProgramSize {
		return 0, 0, 0, 0, false, fmt.Errorf("invalid program-header entry size for GNU unwind index")
	}
	for i := uint64(0); i < phnum; i++ {
		off := phoff + i*phentsize
		if off > uint64(len(input)) || phentsize > uint64(len(input))-off {
			return 0, 0, 0, 0, false, fmt.Errorf("program header table is truncated")
		}
		entry := input[off : off+phentsize]
		if elf.ProgType(bo.Uint32(entry[0:4])) != elf.PT_GNU_EH_FRAME {
			continue
		}
		if found {
			return 0, 0, 0, 0, false, fmt.Errorf("multiple PT_GNU_EH_FRAME entries are unsupported")
		}
		found = true
		index = int(i)
		fileOffset = bo.Uint64(entry[8:16])
		vaddr = bo.Uint64(entry[16:24])
		size = bo.Uint64(entry[32:40])
		memsz := bo.Uint64(entry[40:48])
		if size == 0 || memsz != size {
			return 0, 0, 0, 0, false, fmt.Errorf("PT_GNU_EH_FRAME has invalid size")
		}
	}
	return index, fileOffset, vaddr, size, found, nil
}

'''
replace_once("internal/elf/rewrite_plan.go", marker, helpers + marker)

# ---------------------------------------------------------------------------
# 3. Program-header mutation is optional/variadic so existing direct unit calls
#    remain source-compatible.
# ---------------------------------------------------------------------------
replace_once(
    "internal/elf/rewrite_plan.go",
    '''func planProgramHeaders(input []byte, meta *elfMetadata, segments []rewriteSegment) (programHeaderPlan, error) {
''',
    '''func planProgramHeaders(input []byte, meta *elfMetadata, segments []rewriteSegment, gnuEHFrames ...*gnuEHFramePlan) (programHeaderPlan, error) {
''',
)
replace_once(
    "internal/elf/rewrite_plan.go",
    '''\tfinalPrograms := make([]plannedProgramHeader, int(newPhnum))
\tcopy(finalPrograms, programs)
\tslots := append([]int(nil), reusable...)
''',
    '''\tfinalPrograms := make([]plannedProgramHeader, int(newPhnum))
\tcopy(finalPrograms, programs)
\tvar gnuEHFrame *gnuEHFramePlan
\tif len(gnuEHFrames) > 1 {
\t\treturn programHeaderPlan{}, fmt.Errorf("multiple GNU unwind replacement plans are unsupported")
\t}
\tif len(gnuEHFrames) == 1 {
\t\tgnuEHFrame = gnuEHFrames[0]
\t}
\tslots := append([]int(nil), reusable...)
''',
)
replace_once(
    "internal/elf/rewrite_plan.go",
    '''\tif appendCount != 0 {
\t\tvar phdrIndex = -1
''',
    '''\tif gnuEHFrame != nil {
\t\tif gnuEHFrame.programIndex < 0 || gnuEHFrame.programIndex >= len(programs) || programs[gnuEHFrame.programIndex].type_ != elf.PT_GNU_EH_FRAME {
\t\t\treturn programHeaderPlan{}, fmt.Errorf("GNU unwind replacement does not match the original program header")
\t\t}
\t\tprogram := programs[gnuEHFrame.programIndex]
\t\tprogram.flags = elf.PF_R
\t\tprogram.off = gnuEHFrame.fileOffset
\t\tprogram.vaddr = gnuEHFrame.vaddr
\t\tprogram.paddr = gnuEHFrame.vaddr
\t\tprogram.filesz = gnuEHFrame.size
\t\tprogram.memsz = gnuEHFrame.size
\t\tprogram.align = 4
\t\tmutation := programHeaderMutation{index: gnuEHFrame.programIndex, header: program}
\t\tplan.gnuEHFrameUpdate = &mutation
\t\tfinalPrograms[gnuEHFrame.programIndex] = program
\t}
\tif appendCount != 0 {
\t\tvar phdrIndex = -1
''',
)

# Validate the planned header sits inside the R load and table mutation exists.
replace_once(
    "internal/elf/rewrite_plan.go",
    '''\tif len(planner.plan.functions) != len(planner.analysis.Selections) {
\t\treturn fmt.Errorf("rewrite plan function count does not match analysis")
\t}
\treturn nil
}
''',
    '''\tif len(planner.plan.functions) != len(planner.analysis.Selections) {
\t\treturn fmt.Errorf("rewrite plan function count does not match analysis")
\t}
\tif planner.plan.gnuEHFrame != nil {
\t\teh := planner.plan.gnuEHFrame
\t\tro := planner.plan.segments[rewriteSegmentR]
\t\tif len(eh.header) == 0 || uint64(len(eh.header)) != eh.size || eh.fileOffset < ro.fileOffset || eh.fileOffset+eh.size > ro.fileOffset+ro.fileSize || eh.vaddr < ro.vaddr || eh.vaddr+eh.size > ro.vaddr+ro.memSize {
\t\t\treturn fmt.Errorf("planned GNU unwind index is not contained by the read-only runtime load")
\t\t}
\t\tif planner.plan.programHeaders.gnuEHFrameUpdate == nil || planner.plan.programHeaders.gnuEHFrameUpdate.index != eh.programIndex {
\t\t\treturn fmt.Errorf("planned PT_GNU_EH_FRAME update is missing")
\t\t}
\t}
\treturn nil
}
''',
)

# ---------------------------------------------------------------------------
# 4. Focused rewrite-plan test helpers and tests.
# ---------------------------------------------------------------------------
replace_once(
    "internal/elf/rewrite_plan_test.go",
    '''\tvmruntime "github.com/vmpacker/internal/runtime"
\t"github.com/vmpacker/internal/vm"
''',
    '''\tvmruntime "github.com/vmpacker/internal/runtime"
\t"github.com/vmpacker/internal/unwind"
\t"github.com/vmpacker/internal/vm"
''',
)

insert_before = '''func TestRewritePlanAcceptsInstalledExactR29RuntimeImage(t *testing.T) {
'''
new_tests = r'''func TestRewritePlanMergesRuntimeFDEsIntoExistingGNUUnwindIndex(t *testing.T) {
	fixture := buildELFFixture(fixtureOptions{dynamic: true})
	fixture = addGNUUnwindHeaderFixture(t, fixture)
	request, analysis, preparation := rewritePlanPreparation(t, fixture, false)
	request.RuntimeImage = rewritePlanRuntimeImageWithFDE(t, request.Opcodes)

	plan, err := buildRewritePlan(request, analysis, preparation)
	if err != nil {
		t.Fatal(err)
	}
	if plan.gnuEHFrame == nil || plan.programHeaders.gnuEHFrameUpdate == nil {
		t.Fatalf("missing GNU unwind plan: %+v", plan.gnuEHFrame)
	}
	header, err := unwind.ParseEHFrameHeader(plan.gnuEHFrame.header, plan.gnuEHFrame.vaddr, binary.LittleEndian, 8)
	if err != nil {
		t.Fatal(err)
	}
	vmNative := mustPlannedSymbolVA(t, plan, "vm_native_call")
	if len(header.Entries) != 2 || header.Entries[0].InitialLocation != 0x1200 || header.Entries[1].InitialLocation != vmNative {
		t.Fatalf("merged header=%+v vm_native=0x%x", header, vmNative)
	}
	fdeFound := false
	for _, entry := range header.Entries {
		if entry.InitialLocation == vmNative {
			fdeFound = entry.FDEAddress >= plan.segments[rewriteSegmentR].vaddr && entry.FDEAddress < plan.segments[rewriteSegmentR].vaddr+plan.segments[rewriteSegmentR].memSize
		}
	}
	if !fdeFound {
		t.Fatalf("runtime FDE was not indexed in appended R load: %+v", header.Entries)
	}
	update := plan.programHeaders.gnuEHFrameUpdate.header
	if update.type_ != elf.PT_GNU_EH_FRAME || update.off != plan.gnuEHFrame.fileOffset || update.vaddr != plan.gnuEHFrame.vaddr || update.filesz != plan.gnuEHFrame.size {
		t.Fatalf("PT_GNU_EH_FRAME update=%+v unwind=%+v", update, plan.gnuEHFrame)
	}
}

func TestRewritePlanWithoutGNUUnwindHeaderKeepsExistingBehavior(t *testing.T) {
	fixture := buildELFFixture(fixtureOptions{dynamic: true})
	request, analysis, preparation := rewritePlanPreparation(t, fixture, false)
	request.RuntimeImage = rewritePlanRuntimeImageWithFDE(t, request.Opcodes)
	plan, err := buildRewritePlan(request, analysis, preparation)
	if err != nil {
		t.Fatal(err)
	}
	if plan.gnuEHFrame != nil || plan.programHeaders.gnuEHFrameUpdate != nil {
		t.Fatalf("unexpected GNU unwind plan=%+v update=%+v", plan.gnuEHFrame, plan.programHeaders.gnuEHFrameUpdate)
	}
}

func TestRewritePlanRejectsDuplicateGNUUnwindProgramHeaders(t *testing.T) {
	fixture := buildELFFixture(fixtureOptions{dynamic: true})
	fixture = addGNUUnwindHeaderFixture(t, fixture)
	bo := binary.LittleEndian
	phnum := int(bo.Uint16(fixture.data[56:58]))
	if fixture.phoff+(phnum+1)*elf64ProgramSize > 0x180 {
		t.Fatal("fixture has no PHDR slack for duplicate GNU EH header")
	}
	first := fixture.phoff + (phnum-1)*elf64ProgramSize
	second := fixture.phoff + phnum*elf64ProgramSize
	copy(fixture.data[second:second+elf64ProgramSize], fixture.data[first:first+elf64ProgramSize])
	bo.PutUint16(fixture.data[56:58], uint16(phnum+1))
	request, analysis, preparation := rewritePlanPreparation(t, fixture, false)
	request.RuntimeImage = rewritePlanRuntimeImageWithFDE(t, request.Opcodes)
	if _, err := buildRewritePlan(request, analysis, preparation); err == nil || !strings.Contains(err.Error(), "multiple PT_GNU_EH_FRAME") {
		t.Fatalf("duplicate GNU EH err=%v", err)
	}
}

'''
replace_once("internal/elf/rewrite_plan_test.go", insert_before, new_tests + insert_before)

# Extend exact-r29 test to exercise the GNU index merge when available.
replace_once(
    "internal/elf/rewrite_plan_test.go",
    '''\tfixture := buildELFFixture(fixtureOptions{dynamic: true})
\trequest, analysis, preparation := rewritePlanPreparation(t, fixture, false)
\timage, err := vmruntime.Build(context.Background(), vmruntime.BuildConfig{
''',
    '''\tfixture := buildELFFixture(fixtureOptions{dynamic: true})
\tfixture = addGNUUnwindHeaderFixture(t, fixture)
\trequest, analysis, preparation := rewritePlanPreparation(t, fixture, false)
\timage, err := vmruntime.Build(context.Background(), vmruntime.BuildConfig{
''',
)
replace_once(
    "internal/elf/rewrite_plan_test.go",
    '''\tif len(plan.segments) != 3 || len(plan.programHeaders.newLoads) != 3 {
\t\tt.Fatalf("incomplete exact-r29 rewrite plan: segments=%d loads=%d", len(plan.segments), len(plan.programHeaders.newLoads))
\t}
}
''',
    '''\tif len(plan.segments) != 3 || len(plan.programHeaders.newLoads) != 3 || plan.gnuEHFrame == nil || plan.programHeaders.gnuEHFrameUpdate == nil {
\t\tt.Fatalf("incomplete exact-r29 rewrite plan: segments=%d loads=%d unwind=%+v", len(plan.segments), len(plan.programHeaders.newLoads), plan.gnuEHFrame)
\t}
\theader, err := unwind.ParseEHFrameHeader(plan.gnuEHFrame.header, plan.gnuEHFrame.vaddr, binary.LittleEndian, 8)
\tif err != nil {
\t\tt.Fatal(err)
\t}
\twant := map[uint64]bool{mustPlannedSymbolVA(t, plan, "vm_entry_token"): false, mustPlannedSymbolVA(t, plan, "vm_native_call"): false}
\tfor _, entry := range header.Entries {
\t\tif _, ok := want[entry.InitialLocation]; ok {
\t\t\twant[entry.InitialLocation] = true
\t\t}
\t}
\tfor address, found := range want {
\t\tif !found {
\t\t\tt.Fatalf("exact-r29 GNU unwind index lacks runtime FDE initial location 0x%x", address)
\t\t}
\t}
}
''',
)

# Add fixture/runtime helpers before rewritePlanPreparation.
helper_marker = '''func rewritePlanPreparation(t *testing.T, fixture elfFixture, wantBTI bool) (Request, Analysis, *TranslationPreparation) {
'''
helper_code = r'''func addGNUUnwindHeaderFixture(t *testing.T, fixture elfFixture) elfFixture {
	t.Helper()
	const headerOffset = 0x1c0
	const headerVA = 0x11c0
	data, err := unwind.BuildEHFrameHeader(headerVA, 0x1180, []unwind.HeaderEntry{{InitialLocation: 0x1200, FDEAddress: 0x1190}})
	if err != nil {
		t.Fatal(err)
	}
	if headerOffset+len(data) > len(fixture.data) || headerOffset < fixture.phoff {
		t.Fatal("fixture has no space for GNU EH frame header")
	}
	copy(fixture.data[headerOffset:headerOffset+len(data)], data)
	bo := binary.LittleEndian
	phnum := int(bo.Uint16(fixture.data[56:58]))
	entry := fixture.phoff + phnum*elf64ProgramSize
	if entry+elf64ProgramSize > 0x180 {
		t.Fatal("fixture has no PHDR slack for GNU EH frame header")
	}
	bo.PutUint32(fixture.data[entry:entry+4], uint32(elf.PT_GNU_EH_FRAME))
	bo.PutUint32(fixture.data[entry+4:entry+8], uint32(elf.PF_R))
	bo.PutUint64(fixture.data[entry+8:entry+16], headerOffset)
	bo.PutUint64(fixture.data[entry+16:entry+24], headerVA)
	bo.PutUint64(fixture.data[entry+24:entry+32], headerVA)
	bo.PutUint64(fixture.data[entry+32:entry+40], uint64(len(data)))
	bo.PutUint64(fixture.data[entry+40:entry+48], uint64(len(data)))
	bo.PutUint64(fixture.data[entry+48:entry+56], 4)
	bo.PutUint16(fixture.data[56:58], uint16(phnum+1))
	return fixture
}

func rewritePlanRuntimeImageWithFDE(t *testing.T, opcodes vm.OpcodeMap) *vmruntime.Image {
	t.Helper()
	image := rewritePlanRuntimeImage(t, opcodes)
	cieContent := []byte{1, 'z', 'R', 0, 1, 0x78, 30, 1, unwind.PEPcrel | unwind.PESdata4, 0x0c}
	cieBody := append([]byte{0, 0, 0, 0}, cieContent...)
	frame := appendTestEHLength(nil, cieBody)
	fdeOffset := len(frame)
	idFieldOffset := fdeOffset + 4
	fdeBody := make([]byte, 4)
	binary.LittleEndian.PutUint32(fdeBody, uint32(idFieldOffset))
	fdeBody = append(fdeBody, 0, 0, 0, 0) // R_AARCH64_PREL32 -> vm_native_call
	fdeBody = binary.LittleEndian.AppendUint32(fdeBody, 4)
	fdeBody = append(fdeBody, 0, 0x0c)
	frame = appendTestEHLength(frame, fdeBody)
	frame = append(frame, 0, 0, 0, 0)
	image.Sections[4].Data = frame
	image.Sections[4].Size = uint64(len(frame))
	image.Relocations = append(image.Relocations, vmruntime.Relocation{
		TargetIndex: 4, Offset: uint64(fdeOffset + 8), Type: elf.R_AARCH64_PREL32, SymbolIndex: 3,
	})
	return image
}

func appendTestEHLength(dst, body []byte) []byte {
	dst = binary.LittleEndian.AppendUint32(dst, uint32(len(body)))
	return append(dst, body...)
}

'''
replace_once("internal/elf/rewrite_plan_test.go", helper_marker, helper_code + helper_marker)

print("phase14 runtime unwind index patch applied")
