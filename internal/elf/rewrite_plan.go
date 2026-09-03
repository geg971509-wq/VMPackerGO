package elf

import (
	"crypto/rand"
	"debug/elf"
	"encoding/binary"
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/geg971509-wq/VMPackerGO/internal/arch/arm64"
	vmruntime "github.com/geg971509-wq/VMPackerGO/internal/runtime"
	"github.com/geg971509-wq/VMPackerGO/internal/unwind"
	"github.com/geg971509-wq/VMPackerGO/internal/vm"
)

const (
	rewriteLoadAlignment = uint64(0x4000)
	tokenDescriptorSize  = uint64(32)
)

const (
	rewriteSegmentRX = iota
	rewriteSegmentRW
	rewriteSegmentR
)

type rewriteSegment struct {
	flags      elf.ProgFlag
	fileOffset uint64
	vaddr      uint64
	fileSize   uint64
	memSize    uint64
	data       []byte
}

type runtimeSectionPlan struct {
	index         int
	name          string
	segment       int
	segmentOffset uint64
	vaddr         uint64
	size          uint64
}

type runtimeSymbolPlan struct {
	index uint32
	name  string
	vaddr uint64
	size  uint64
}

type gnuEHFramePlan struct {
	programIndex    int
	original        *unwind.FrameHeader
	runtimeFDECount int
	segmentOffset   uint64
	fileOffset      uint64
	vaddr           uint64
	size            uint64
	header          []byte
}

type functionRewritePlan struct {
	selection             Selection
	functionID            uint32
	token                 uint32
	xorKey                byte
	opcodeKey             uint32
	bytecodeSegmentOffset uint64
	bytecodeVA            uint64
	bytecodeLen           uint32
	entryFileOffset       uint64
	entryTargetVA         uint64
	entryPatch            []byte
}

type programHeaderSource string

const (
	programHeaderSourceNull   programHeaderSource = "pt-null"
	programHeaderSourceAppend programHeaderSource = "append"
)

type plannedProgramHeader struct {
	type_  elf.ProgType
	flags  elf.ProgFlag
	off    uint64
	vaddr  uint64
	paddr  uint64
	filesz uint64
	memsz  uint64
	align  uint64
}

type programHeaderMutation struct {
	index  int
	source programHeaderSource
	header plannedProgramHeader
}

type programHeaderPlan struct {
	phoffBefore      uint64
	phoffAfter       uint64
	phdrTableVA      uint64
	phnumBefore      uint16
	phnumAfter       uint16
	relocated        bool
	tableData        []byte
	newLoads         []programHeaderMutation
	phdrUpdate       *programHeaderMutation
	gnuEHFrameUpdate *programHeaderMutation
}

type RewritePlan struct {
	segments        []rewriteSegment
	runtimeSections []runtimeSectionPlan
	symbols         []runtimeSymbolPlan
	functions       []functionRewritePlan
	tokenTableVA    uint64
	gnuEHFrame      *gnuEHFramePlan
	programHeaders  programHeaderPlan
}

type rewritePlanner struct {
	req                Request
	analysis           Analysis
	preparation        *TranslationPreparation
	meta               *elfMetadata
	image              *vmruntime.Image
	plan               RewritePlan
	sectionPlanByIndex map[int]int
	symbolVAByIndex    map[uint32]uint64
	gotOffsetBySymbol  map[uint32]uint64
	tokenTableOffset   uint64
}

func buildRewritePlan(req Request, analysis Analysis, preparation *TranslationPreparation) (*RewritePlan, error) {
	if req.RuntimeImage == nil {
		return nil, fmt.Errorf("validated runtime image is required")
	}
	if err := analysis.ValidateInput(req.Input); err != nil {
		return nil, err
	}
	if err := preparation.ValidateOpcodeMap(req.Opcodes); err != nil {
		return nil, err
	}
	if err := preparation.ValidateAnalysis(analysis); err != nil {
		return nil, err
	}
	if err := preparation.ValidateRuntimeImage(req.RuntimeImage); err != nil {
		return nil, err
	}
	mode := AndroidMode(strings.ToLower(req.Mode))
	if mode == "" {
		mode = AndroidModeAuto
	}
	meta, err := parseELFMetadata(req.Input, mode)
	if err != nil {
		return nil, err
	}
	defer meta.file.Close()
	if meta.kind != analysis.TargetKind {
		return nil, fmt.Errorf("rewrite plan target kind does not match analysis")
	}

	planner := &rewritePlanner{
		req: req, analysis: analysis, preparation: preparation, meta: meta, image: req.RuntimeImage,
		sectionPlanByIndex: make(map[int]int), symbolVAByIndex: make(map[uint32]uint64), gotOffsetBySymbol: make(map[uint32]uint64),
	}
	planner.plan.segments = []rewriteSegment{
		{flags: elf.PF_R | elf.PF_X},
		{flags: elf.PF_R | elf.PF_W},
		{flags: elf.PF_R},
	}
	if err := planner.reserveRuntimeLayout(); err != nil {
		return nil, err
	}
	if err := planner.reserveGNUUnwindIndex(); err != nil {
		return nil, err
	}
	if err := planner.placeSegments(); err != nil {
		return nil, err
	}
	if err := planner.resolveRuntimeSymbols(); err != nil {
		return nil, err
	}
	if err := planner.materializeGOT(); err != nil {
		return nil, err
	}
	if err := planner.applyRuntimeRelocations(); err != nil {
		return nil, err
	}
	if err := planner.materializeGNUUnwindIndex(); err != nil {
		return nil, err
	}
	if err := planner.finalizeRuntimeGlobalsAndFunctions(); err != nil {
		return nil, err
	}
	phdrs, err := planProgramHeaders(req.Input, meta, planner.plan.segments, planner.plan.gnuEHFrame)
	if err != nil {
		return nil, err
	}
	planner.plan.programHeaders = phdrs
	if err := planner.validate(); err != nil {
		return nil, err
	}
	return &planner.plan, nil
}

func (planner *rewritePlanner) reserveRuntimeLayout() error {
	for _, section := range planner.image.Sections {
		if section.Flags&elf.SHF_ALLOC == 0 {
			continue
		}
		if section.Flags&elf.SHF_WRITE != 0 && section.Flags&elf.SHF_EXECINSTR != 0 {
			return fmt.Errorf("runtime section %q is writable executable", section.Name)
		}
		segment := rewriteSegmentR
		if section.Flags&elf.SHF_EXECINSTR != 0 {
			segment = rewriteSegmentRX
		} else if section.Flags&elf.SHF_WRITE != 0 {
			segment = rewriteSegmentRW
		}
		alignment := section.Alignment
		if alignment == 0 {
			alignment = 1
		}
		if !isPowerOfTwo(alignment) {
			return fmt.Errorf("runtime section %q alignment 0x%x is not a power of two", section.Name, alignment)
		}
		if section.Size > uint64(math.MaxInt) {
			return fmt.Errorf("runtime section %q is too large", section.Name)
		}
		if !section.NOBITS && uint64(len(section.Data)) != section.Size {
			return fmt.Errorf("runtime section %q data length %d does not match size %d", section.Name, len(section.Data), section.Size)
		}
		segmentOffset, err := growAligned(&planner.plan.segments[segment].data, alignment, section.Size)
		if err != nil {
			return fmt.Errorf("layout runtime section %q: %w", section.Name, err)
		}
		if !section.NOBITS {
			copy(planner.plan.segments[segment].data[segmentOffset:], section.Data)
		}
		planner.sectionPlanByIndex[section.Index] = len(planner.plan.runtimeSections)
		planner.plan.runtimeSections = append(planner.plan.runtimeSections, runtimeSectionPlan{
			index: section.Index, name: section.Name, segment: segment, segmentOffset: uint64(segmentOffset), size: section.Size,
		})
	}

	gotSymbols := make(map[uint32]struct{})
	for _, relocation := range planner.image.Relocations {
		if relocation.Type != elf.R_AARCH64_GOT_LD_PREL19 {
			continue
		}
		if _, ok := planner.sectionPlanByIndex[int(relocation.TargetIndex)]; !ok {
			continue
		}
		gotSymbols[relocation.SymbolIndex] = struct{}{}
	}
	gotIDs := make([]uint32, 0, len(gotSymbols))
	for id := range gotSymbols {
		gotIDs = append(gotIDs, id)
	}
	sort.Slice(gotIDs, func(i, j int) bool { return gotIDs[i] < gotIDs[j] })
	for _, id := range gotIDs {
		off, err := growAligned(&planner.plan.segments[rewriteSegmentR].data, 8, 8)
		if err != nil {
			return fmt.Errorf("layout runtime GOT: %w", err)
		}
		planner.gotOffsetBySymbol[id] = uint64(off)
	}

	tokenTableSize, ok := checkedMul(uint64(len(planner.preparation.Functions)), tokenDescriptorSize)
	if !ok {
		return fmt.Errorf("token descriptor table size overflows")
	}
	off, err := growAligned(&planner.plan.segments[rewriteSegmentR].data, 8, tokenTableSize)
	if err != nil {
		return fmt.Errorf("layout token descriptor table: %w", err)
	}
	planner.tokenTableOffset = uint64(off)

	for i, function := range planner.preparation.Functions {
		if i >= maxSelections {
			return fmt.Errorf("function count exceeds token limit %d", maxSelections)
		}
		finalSize, err := plannedFinalBytecodeSize(function.Translation, planner.req.Opcodes)
		if err != nil {
			return fmt.Errorf("function %q bytecode layout: %w", function.Selection.Name, err)
		}
		bytecodeOff, err := growAligned(&planner.plan.segments[rewriteSegmentR].data, 8, uint64(finalSize))
		if err != nil {
			return fmt.Errorf("function %q bytecode layout: %w", function.Selection.Name, err)
		}
		planner.plan.functions = append(planner.plan.functions, functionRewritePlan{
			selection: function.Selection, functionID: uint32(i), bytecodeSegmentOffset: uint64(bytecodeOff), bytecodeLen: uint32(finalSize),
		})
	}

	for i := range planner.plan.segments {
		if len(planner.plan.segments[i].data) == 0 {
			return fmt.Errorf("runtime layout produced an empty required segment %d", i)
		}
	}
	return nil
}

func (planner *rewritePlanner) reserveGNUUnwindIndex() error {
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

func (planner *rewritePlanner) placeSegments() error {
	fileCursor, ok := alignUpChecked(uint64(len(planner.req.Input)), rewriteLoadAlignment)
	if !ok {
		return fmt.Errorf("runtime file placement overflows")
	}
	var maxVA uint64
	for _, load := range planner.meta.loads {
		end, ok := checkedAdd(load.vaddr, load.memsz)
		if !ok {
			return fmt.Errorf("existing PT_LOAD %d virtual range overflows", load.index)
		}
		if end > maxVA {
			maxVA = end
		}
	}
	vaCursor, ok := alignUpChecked(maxVA, rewriteLoadAlignment)
	if !ok {
		return fmt.Errorf("runtime virtual placement overflows")
	}
	for i := range planner.plan.segments {
		fileCursor, ok = alignUpChecked(fileCursor, rewriteLoadAlignment)
		if !ok {
			return fmt.Errorf("runtime segment %d file alignment overflows", i)
		}
		vaCursor, ok = alignUpChecked(vaCursor, rewriteLoadAlignment)
		if !ok {
			return fmt.Errorf("runtime segment %d virtual alignment overflows", i)
		}
		segment := &planner.plan.segments[i]
		segment.fileOffset = fileCursor
		segment.vaddr = vaCursor
		segment.fileSize = uint64(len(segment.data))
		segment.memSize = segment.fileSize
		fileCursor, ok = checkedAdd(fileCursor, segment.fileSize)
		if !ok {
			return fmt.Errorf("runtime segment %d file range overflows", i)
		}
		vaCursor, ok = checkedAdd(vaCursor, segment.memSize)
		if !ok {
			return fmt.Errorf("runtime segment %d virtual range overflows", i)
		}
	}
	for i := range planner.plan.runtimeSections {
		section := &planner.plan.runtimeSections[i]
		section.vaddr = planner.plan.segments[section.segment].vaddr + section.segmentOffset
	}
	planner.plan.tokenTableVA = planner.plan.segments[rewriteSegmentR].vaddr + planner.tokenTableOffset
	if planner.plan.gnuEHFrame != nil {
		segment := planner.plan.segments[rewriteSegmentR]
		planner.plan.gnuEHFrame.fileOffset = segment.fileOffset + planner.plan.gnuEHFrame.segmentOffset
		planner.plan.gnuEHFrame.vaddr = segment.vaddr + planner.plan.gnuEHFrame.segmentOffset
	}
	for i := range planner.plan.functions {
		planner.plan.functions[i].bytecodeVA = planner.plan.segments[rewriteSegmentR].vaddr + planner.plan.functions[i].bytecodeSegmentOffset
	}
	return nil
}

func (planner *rewritePlanner) resolveRuntimeSymbols() error {
	for _, symbol := range planner.image.Symbols {
		var va uint64
		switch symbol.Section {
		case elf.SHN_ABS:
			va = symbol.Value
		case elf.SHN_UNDEF:
			return fmt.Errorf("runtime symbol %q is undefined", symbol.Name)
		default:
			sectionIndex := int(symbol.Section)
			planIndex, ok := planner.sectionPlanByIndex[sectionIndex]
			if !ok {
				continue
			}
			section := planner.plan.runtimeSections[planIndex]
			if symbol.Value > section.size || symbol.Size > section.size-symbol.Value {
				return fmt.Errorf("runtime symbol %q exceeds section %q", symbol.Name, section.name)
			}
			var okAdd bool
			va, okAdd = checkedAdd(section.vaddr, symbol.Value)
			if !okAdd {
				return fmt.Errorf("runtime symbol %q address overflows", symbol.Name)
			}
		}
		if _, exists := planner.symbolVAByIndex[symbol.Index]; exists {
			return fmt.Errorf("runtime has duplicate symbol index %d", symbol.Index)
		}
		planner.symbolVAByIndex[symbol.Index] = va
		planner.plan.symbols = append(planner.plan.symbols, runtimeSymbolPlan{index: symbol.Index, name: symbol.Name, vaddr: va, size: symbol.Size})
	}
	return nil
}

func (planner *rewritePlanner) materializeGOT() error {
	segment := &planner.plan.segments[rewriteSegmentR]
	for symbolIndex, offset := range planner.gotOffsetBySymbol {
		va, ok := planner.symbolVAByIndex[symbolIndex]
		if !ok {
			return fmt.Errorf("runtime GOT references unavailable symbol index %d", symbolIndex)
		}
		if offset > uint64(len(segment.data)) || 8 > uint64(len(segment.data))-offset {
			return fmt.Errorf("runtime GOT slot for symbol %d is out of bounds", symbolIndex)
		}
		binary.LittleEndian.PutUint64(segment.data[offset:offset+8], va)
	}
	return nil
}

func (planner *rewritePlanner) applyRuntimeRelocations() error {
	for _, relocation := range planner.image.Relocations {
		planIndex, ok := planner.sectionPlanByIndex[int(relocation.TargetIndex)]
		if !ok {
			continue
		}
		section := planner.plan.runtimeSections[planIndex]
		segment := &planner.plan.segments[section.segment]
		patchOffset, ok := checkedAdd(section.segmentOffset, relocation.Offset)
		if !ok || patchOffset > uint64(len(segment.data)) {
			return fmt.Errorf("runtime relocation target offset overflows section %q", section.name)
		}
		P, ok := checkedAdd(section.vaddr, relocation.Offset)
		if !ok {
			return fmt.Errorf("runtime relocation site address overflows section %q", section.name)
		}
		S, ok := planner.symbolVAByIndex[relocation.SymbolIndex]
		if relocation.Type == elf.R_AARCH64_GOT_LD_PREL19 {
			gotOffset, exists := planner.gotOffsetBySymbol[relocation.SymbolIndex]
			if !exists {
				return fmt.Errorf("runtime GOT relocation has no slot for symbol %d", relocation.SymbolIndex)
			}
			S, ok = checkedAdd(planner.plan.segments[rewriteSegmentR].vaddr, gotOffset)
		}
		if !ok {
			return fmt.Errorf("runtime relocation references unavailable symbol index %d", relocation.SymbolIndex)
		}
		if err := applyAArch64Relocation(segment.data, patchOffset, relocation.Type, S, relocation.Addend, P); err != nil {
			return fmt.Errorf("runtime relocation %s in section %q at 0x%x: %w", relocation.Type, section.name, relocation.Offset, err)
		}
	}
	return nil
}

func (planner *rewritePlanner) finalizeRuntimeGlobalsAndFunctions() error {
	anchorVA, err := planner.namedSymbolVA("_token_table_va")
	if err != nil {
		return err
	}
	if planner.plan.tokenTableVA <= anchorVA {
		return fmt.Errorf("token descriptor table must follow _token_table_va")
	}
	if err := planner.writeRuntimeGlobal("_token_table_va", planner.plan.tokenTableVA-anchorVA); err != nil {
		return err
	}
	if err := planner.writeRuntimeGlobal("_image_file_va", anchorVA); err != nil {
		return err
	}
	if err := planner.writeRuntimeGlobal("_token_count", uint64(len(planner.plan.functions))); err != nil {
		return err
	}

	entryVA, err := planner.namedSymbolVA("vm_entry_token")
	if err != nil {
		return err
	}
	for i := range planner.plan.functions {
		function := &planner.plan.functions[i]
		prepared := planner.preparation.Functions[i]
		var keys [5]byte
		if _, err := rand.Read(keys[:]); err != nil {
			return fmt.Errorf("generate bytecode keys: %w", err)
		}
		function.xorKey = keys[0]
		function.opcodeKey = binary.LittleEndian.Uint32(keys[1:5])
		bytecode, err := finalizePreparedBytecode(prepared.Translation, anchorVA, planner.req.Opcodes, function.xorKey, function.opcodeKey)
		if err != nil {
			return fmt.Errorf("function %q finalize bytecode: %w", function.selection.Name, err)
		}
		if len(bytecode) != int(function.bytecodeLen) {
			return fmt.Errorf("function %q final bytecode size changed from %d to %d", function.selection.Name, function.bytecodeLen, len(bytecode))
		}
		segment := &planner.plan.segments[rewriteSegmentR]
		end, ok := checkedAdd(function.bytecodeSegmentOffset, uint64(len(bytecode)))
		if !ok || end > uint64(len(segment.data)) {
			return fmt.Errorf("function %q bytecode exceeds planned read-only segment", function.selection.Name)
		}
		copy(segment.data[function.bytecodeSegmentOffset:end], bytecode)
		function.token = uint32(function.xorKey)<<24 | function.functionID
		function.entryTargetVA = entryVA
		function.entryFileOffset = function.selection.Offset
		patch, err := buildPlannedTokenTrampoline(planner.req.Input, function.selection, prepared.Translation, entryVA, function.token)
		if err != nil {
			return fmt.Errorf("function %q entry trampoline: %w", function.selection.Name, err)
		}
		function.entryPatch = patch

		descOff, ok := checkedAdd(planner.tokenTableOffset, uint64(i)*tokenDescriptorSize)
		if !ok || descOff > uint64(len(segment.data)) || tokenDescriptorSize > uint64(len(segment.data))-descOff {
			return fmt.Errorf("function %q token descriptor exceeds planned segment", function.selection.Name)
		}
		desc := segment.data[descOff : descOff+tokenDescriptorSize]
		if function.bytecodeVA <= anchorVA {
			return fmt.Errorf("function %q bytecode must follow _token_table_va", function.selection.Name)
		}
		binary.LittleEndian.PutUint64(desc[0:8], function.bytecodeVA-anchorVA)
		binary.LittleEndian.PutUint32(desc[8:12], function.bytecodeLen)
		desc[12] = function.xorKey
		binary.LittleEndian.PutUint64(desc[16:24], function.selection.Address)
		if function.selection.Size() > math.MaxUint32 {
			return fmt.Errorf("function %q size exceeds token descriptor", function.selection.Name)
		}
		binary.LittleEndian.PutUint32(desc[24:28], uint32(function.selection.Size()))
	}
	return nil
}

func (planner *rewritePlanner) namedSymbolVA(name string) (uint64, error) {
	found := false
	var va uint64
	for _, symbol := range planner.plan.symbols {
		if symbol.name != name {
			continue
		}
		if found {
			return 0, fmt.Errorf("runtime has duplicate planned symbol %q", name)
		}
		found = true
		va = symbol.vaddr
	}
	if !found {
		return 0, fmt.Errorf("runtime is missing planned symbol %q", name)
	}
	return va, nil
}

func (planner *rewritePlanner) writeRuntimeGlobal(name string, value uint64) error {
	for _, symbol := range planner.image.Symbols {
		if symbol.Name != name {
			continue
		}
		if symbol.Size < 8 {
			return fmt.Errorf("runtime global %q is smaller than 8 bytes", name)
		}
		planIndex, ok := planner.sectionPlanByIndex[int(symbol.Section)]
		if !ok {
			return fmt.Errorf("runtime global %q is not in a planned allocatable section", name)
		}
		section := planner.plan.runtimeSections[planIndex]
		if planner.plan.segments[section.segment].flags != elf.PF_R|elf.PF_W {
			return fmt.Errorf("runtime global %q is not in writable non-executable storage", name)
		}
		off, ok := checkedAdd(section.segmentOffset, symbol.Value)
		segment := &planner.plan.segments[section.segment]
		if !ok || off > uint64(len(segment.data)) || 8 > uint64(len(segment.data))-off {
			return fmt.Errorf("runtime global %q write exceeds section", name)
		}
		binary.LittleEndian.PutUint64(segment.data[off:off+8], value)
		return nil
	}
	return fmt.Errorf("runtime is missing global %q", name)
}

func (planner *rewritePlanner) validate() error {
	for i, segment := range planner.plan.segments {
		if segment.flags&elf.PF_W != 0 && segment.flags&elf.PF_X != 0 {
			return fmt.Errorf("planned segment %d violates W^X", i)
		}
		if segment.fileOffset%rewriteLoadAlignment != 0 || segment.vaddr%rewriteLoadAlignment != 0 {
			return fmt.Errorf("planned segment %d is not 0x%x aligned", i, rewriteLoadAlignment)
		}
		if segment.fileSize != uint64(len(segment.data)) || segment.memSize != segment.fileSize {
			return fmt.Errorf("planned segment %d has inconsistent sizes", i)
		}
		if i > 0 {
			previous := planner.plan.segments[i-1]
			prevFileEnd, okFile := checkedAdd(previous.fileOffset, previous.fileSize)
			prevVAEnd, okVA := checkedAdd(previous.vaddr, previous.memSize)
			if !okFile || !okVA || segment.fileOffset < prevFileEnd || segment.vaddr < prevVAEnd {
				return fmt.Errorf("planned segments overlap")
			}
		}
	}
	if len(planner.plan.functions) != len(planner.analysis.Selections) {
		return fmt.Errorf("rewrite plan function count does not match analysis")
	}
	if planner.plan.gnuEHFrame != nil {
		eh := planner.plan.gnuEHFrame
		ro := planner.plan.segments[rewriteSegmentR]
		if len(eh.header) == 0 || uint64(len(eh.header)) != eh.size || eh.fileOffset < ro.fileOffset || eh.fileOffset+eh.size > ro.fileOffset+ro.fileSize || eh.vaddr < ro.vaddr || eh.vaddr+eh.size > ro.vaddr+ro.memSize {
			return fmt.Errorf("planned GNU unwind index is not contained by the read-only runtime load")
		}
		if planner.plan.programHeaders.gnuEHFrameUpdate == nil || planner.plan.programHeaders.gnuEHFrameUpdate.index != eh.programIndex {
			return fmt.Errorf("planned PT_GNU_EH_FRAME update is missing")
		}
	}
	return nil
}

func plannedFinalBytecodeSize(translation *arm64.TranslateResult, opcodes vm.OpcodeMap) (int, error) {
	if translation == nil {
		return 0, fmt.Errorf("translation is missing")
	}
	if _, err := validateBytecodeTrailer(translation.Bytecode, translation.CodeLen); err != nil {
		return 0, err
	}
	for _, relocation := range translation.Relocations {
		if relocation.Offset < 0 || relocation.Offset+8 > translation.CodeLen {
			return 0, fmt.Errorf("bytecode relocation offset %d exceeds code length %d", relocation.Offset, translation.CodeLen)
		}
	}
	reversed, _, err := reverseInstructions(translation.Bytecode, translation.CodeLen, opcodes)
	if err != nil {
		return 0, err
	}
	trailerLen := len(translation.Bytecode) - translation.CodeLen
	finalSize := len(reversed) + trailerLen
	if err := validateFinalBytecodeSize(finalSize); err != nil {
		return 0, err
	}
	return finalSize, nil
}

func finalizePreparedBytecode(translation *arm64.TranslateResult, imageAnchorVA uint64, opcodes vm.OpcodeMap, xorKey byte, opcodeKey uint32) ([]byte, error) {
	if translation == nil {
		return nil, fmt.Errorf("translation is missing")
	}
	mapCount, err := validateBytecodeTrailer(translation.Bytecode, translation.CodeLen)
	if err != nil {
		return nil, err
	}
	source := append([]byte(nil), translation.Bytecode...)
	for _, relocation := range translation.Relocations {
		if relocation.Offset < 0 || relocation.Offset+8 > translation.CodeLen {
			return nil, fmt.Errorf("bytecode relocation offset %d exceeds code length %d", relocation.Offset, translation.CodeLen)
		}
		delta, ok := signedDifference(relocation.TargetVA, imageAnchorVA)
		if !ok {
			return nil, fmt.Errorf("bytecode image relocation target 0x%x is outside signed 64-bit range from anchor 0x%x", relocation.TargetVA, imageAnchorVA)
		}
		binary.LittleEndian.PutUint64(source[relocation.Offset:relocation.Offset+8], uint64(delta))
	}
	reversed, offsetMap, err := reverseInstructions(source, translation.CodeLen, opcodes)
	if err != nil {
		return nil, err
	}
	packer := Packer{opcodes: opcodes}
	if err := packer.remapBranchTargets(reversed, len(reversed), offsetMap); err != nil {
		return nil, err
	}
	trailer := append([]byte(nil), source[translation.CodeLen:]...)
	for i := 0; i < int(mapCount); i++ {
		off := i * 8
		oldVM := binary.LittleEndian.Uint32(trailer[off+4 : off+8])
		newVM, ok := offsetMap[int(oldVM)]
		if !ok {
			return nil, fmt.Errorf("trailer address map references unknown VM offset 0x%x", oldVM)
		}
		binary.LittleEndian.PutUint32(trailer[off+4:off+8], uint32(newVM))
	}
	final := make([]byte, 0, len(reversed)+len(trailer))
	final = append(final, reversed...)
	final = append(final, trailer...)
	reverseOffset := len(reversed) + int(mapCount)*8
	if reverseOffset+5 > len(final) {
		return nil, fmt.Errorf("final bytecode trailer is truncated")
	}
	final[reverseOffset] = 1
	binary.LittleEndian.PutUint32(final[reverseOffset+1:reverseOffset+5], opcodeKey)
	if err := encryptOpcodes(final, len(reversed), opcodeKey, true, opcodes); err != nil {
		return nil, err
	}
	for i := range final {
		final[i] ^= xorKey
	}
	if err := validateFinalBytecodeSize(len(final)); err != nil {
		return nil, err
	}
	return final, nil
}

func buildPlannedTokenTrampoline(input []byte, selection Selection, translation *arm64.TranslateResult, targetVA uint64, token uint32) ([]byte, error) {
	if translation == nil {
		return nil, fmt.Errorf("translation is missing")
	}
	code, err := selectedCode(input, selection)
	if err != nil {
		return nil, err
	}
	patchOffset := 0
	patchSize := currentMinEntryPatch
	if translation.HasEntryBTI {
		patchOffset = 4
		patchSize = 16
		if selection.Size() < uint64(patchSize) {
			return nil, fmt.Errorf("BTI entry requires at least %d bytes, got %d", patchSize, selection.Size())
		}
		decoded := arm64.NewDecoder().Decode(binary.LittleEndian.Uint32(code[:4]), 0)
		if arm64.Op(decoded.Op) != translation.EntryBTI {
			return nil, fmt.Errorf("BTI entry metadata does not match input encoding")
		}
	} else if selection.Size() < uint64(patchSize) {
		return nil, fmt.Errorf("entry trampoline requires at least %d bytes, got %d", patchSize, selection.Size())
	}
	patch := make([]byte, patchSize)
	if patchOffset != 0 {
		copy(patch[:patchOffset], code[:patchOffset])
	}
	lo16 := token & 0xffff
	hi16 := token >> 16
	binary.LittleEndian.PutUint32(patch[patchOffset:patchOffset+4], 0x52800010|lo16<<5)
	binary.LittleEndian.PutUint32(patch[patchOffset+4:patchOffset+8], 0x72A00010|hi16<<5)
	branchVA, ok := checkedAdd(selection.Address, uint64(patchOffset+8))
	if !ok {
		return nil, fmt.Errorf("entry branch address overflows")
	}
	branch, err := encodeBranch26(branchVA, targetVA, 0x14000000)
	if err != nil {
		return nil, err
	}
	binary.LittleEndian.PutUint32(patch[patchOffset+8:patchOffset+12], branch)
	return patch, nil
}

func planProgramHeaders(input []byte, meta *elfMetadata, segments []rewriteSegment, gnuEHFrames ...*gnuEHFramePlan) (programHeaderPlan, error) {
	if len(input) < elf64HeaderSize {
		return programHeaderPlan{}, fmt.Errorf("ELF header is truncated")
	}
	bo := binary.LittleEndian
	phoff := bo.Uint64(input[32:40])
	phentsize := bo.Uint16(input[54:56])
	phnum := bo.Uint16(input[56:58])
	if phentsize != elf64ProgramSize || phnum == 0 {
		return programHeaderPlan{}, fmt.Errorf("program header table is unavailable for rewrite")
	}
	programs := make([]plannedProgramHeader, int(phnum))
	lastLoad := -1
	var previousLoadVA uint64
	havePreviousLoad := false
	for i := range programs {
		off := phoff + uint64(i)*uint64(phentsize)
		programs[i] = readPlannedProgramHeader(input, off)
		if programs[i].type_ == elf.PT_LOAD {
			if havePreviousLoad && programs[i].vaddr < previousLoadVA {
				return programHeaderPlan{}, fmt.Errorf("existing PT_LOAD program headers are not ordered by virtual address")
			}
			previousLoadVA = programs[i].vaddr
			havePreviousLoad = true
			lastLoad = i
		}
	}
	if lastLoad < 0 {
		return programHeaderPlan{}, fmt.Errorf("rewrite requires at least one PT_LOAD")
	}
	var reusable []int
	for i := lastLoad + 1; i < len(programs) && len(reusable) < len(segments); i++ {
		if programs[i].type_ == elf.PT_NULL {
			reusable = append(reusable, i)
		}
	}
	appendCount := len(segments) - len(reusable)
	if int(phnum)+appendCount >= 0xffff {
		return programHeaderPlan{}, fmt.Errorf("program header count would enter unsupported extended numbering")
	}
	newPhnum := uint16(int(phnum) + appendCount)
	oldTableSize, ok := checkedMul(uint64(phnum), uint64(phentsize))
	if !ok {
		return programHeaderPlan{}, fmt.Errorf("program header table size overflows")
	}
	newTableSize, ok := checkedMul(uint64(newPhnum), uint64(phentsize))
	if !ok {
		return programHeaderPlan{}, fmt.Errorf("expanded program header table size overflows")
	}
	oldEnd, ok := checkedAdd(phoff, oldTableSize)
	if !ok || oldEnd > uint64(len(input)) {
		return programHeaderPlan{}, fmt.Errorf("program header table end overflows")
	}
	newEnd, ok := checkedAdd(phoff, newTableSize)
	if !ok {
		return programHeaderPlan{}, fmt.Errorf("expanded program header table end overflows")
	}

	plan := programHeaderPlan{
		phoffBefore: phoff, phoffAfter: phoff, phnumBefore: phnum, phnumAfter: newPhnum,
	}
	if tableVA, ok := fileOffsetVA(programs, phoff); ok {
		plan.phdrTableVA = tableVA
	}
	if appendCount != 0 && !programHeaderTableCanGrowInPlace(input, meta, programs, oldEnd, newEnd) {
		if len(segments) == 0 || segments[len(segments)-1].flags != elf.PF_R {
			return programHeaderPlan{}, fmt.Errorf("program header relocation requires a trailing read-only runtime segment")
		}
		segment := &segments[len(segments)-1]
		tableOffset, err := growAligned(&segment.data, 8, newTableSize)
		if err != nil {
			return programHeaderPlan{}, fmt.Errorf("relocate program header table: %w", err)
		}
		segment.fileSize = uint64(len(segment.data))
		segment.memSize = segment.fileSize
		plan.phoffAfter, ok = checkedAdd(segment.fileOffset, uint64(tableOffset))
		if !ok {
			return programHeaderPlan{}, fmt.Errorf("relocated program header file offset overflows")
		}
		plan.phdrTableVA, ok = checkedAdd(segment.vaddr, uint64(tableOffset))
		if !ok {
			return programHeaderPlan{}, fmt.Errorf("relocated program header virtual address overflows")
		}
		plan.relocated = true
	}

	finalPrograms := make([]plannedProgramHeader, int(newPhnum))
	copy(finalPrograms, programs)
	var gnuEHFrame *gnuEHFramePlan
	if len(gnuEHFrames) > 1 {
		return programHeaderPlan{}, fmt.Errorf("multiple GNU unwind replacement plans are unsupported")
	}
	if len(gnuEHFrames) == 1 {
		gnuEHFrame = gnuEHFrames[0]
	}
	slots := append([]int(nil), reusable...)
	for i := 0; i < appendCount; i++ {
		slots = append(slots, int(phnum)+i)
	}
	for i, segment := range segments {
		source := programHeaderSourceAppend
		if slots[i] < int(phnum) {
			source = programHeaderSourceNull
		}
		mutation := programHeaderMutation{
			index: slots[i], source: source,
			header: plannedProgramHeader{
				type_: elf.PT_LOAD, flags: segment.flags, off: segment.fileOffset, vaddr: segment.vaddr, paddr: segment.vaddr,
				filesz: segment.fileSize, memsz: segment.memSize, align: rewriteLoadAlignment,
			},
		}
		plan.newLoads = append(plan.newLoads, mutation)
		finalPrograms[slots[i]] = mutation.header
	}
	if gnuEHFrame != nil {
		if gnuEHFrame.programIndex < 0 || gnuEHFrame.programIndex >= len(programs) || programs[gnuEHFrame.programIndex].type_ != elf.PT_GNU_EH_FRAME {
			return programHeaderPlan{}, fmt.Errorf("GNU unwind replacement does not match the original program header")
		}
		program := programs[gnuEHFrame.programIndex]
		program.flags = elf.PF_R
		program.off = gnuEHFrame.fileOffset
		program.vaddr = gnuEHFrame.vaddr
		program.paddr = gnuEHFrame.vaddr
		program.filesz = gnuEHFrame.size
		program.memsz = gnuEHFrame.size
		program.align = 4
		mutation := programHeaderMutation{index: gnuEHFrame.programIndex, header: program}
		plan.gnuEHFrameUpdate = &mutation
		finalPrograms[gnuEHFrame.programIndex] = program
	}
	if appendCount != 0 {
		var phdrIndex = -1
		for i, program := range programs {
			if program.type_ != elf.PT_PHDR {
				continue
			}
			if phdrIndex != -1 {
				return programHeaderPlan{}, fmt.Errorf("multiple PT_PHDR entries are unsupported")
			}
			phdrIndex = i
		}
		if phdrIndex >= 0 {
			program := programs[phdrIndex]
			expectedVA, ok := fileOffsetVA(programs, phoff)
			if !ok || program.off != phoff || program.vaddr != expectedVA || program.filesz != oldTableSize || program.memsz != oldTableSize {
				return programHeaderPlan{}, fmt.Errorf("PT_PHDR does not exactly describe the current program header table")
			}
			if plan.relocated {
				program.off = plan.phoffAfter
				program.vaddr = plan.phdrTableVA
				program.paddr = plan.phdrTableVA
			}
			program.filesz = newTableSize
			program.memsz = newTableSize
			mutation := programHeaderMutation{index: phdrIndex, header: program}
			plan.phdrUpdate = &mutation
			finalPrograms[phdrIndex] = program
		}
	}
	plan.tableData = encodePlannedProgramHeaders(finalPrograms)
	if plan.relocated {
		segment := &segments[len(segments)-1]
		tableOffset := plan.phoffAfter - segment.fileOffset
		copy(segment.data[tableOffset:tableOffset+uint64(len(plan.tableData))], plan.tableData)
	}
	return plan, nil
}

func programHeaderTableCanGrowInPlace(input []byte, meta *elfMetadata, programs []plannedProgramHeader, oldEnd, newEnd uint64) bool {
	if newEnd > uint64(len(input)) || !phdrRangeIsFileBacked(programs, binary.LittleEndian.Uint64(input[32:40]), newEnd) {
		return false
	}
	for _, b := range input[oldEnd:newEnd] {
		if b != 0 {
			return false
		}
	}
	return validatePHDRGrowthOverlap(meta, programs, oldEnd, newEnd) == nil
}

func encodePlannedProgramHeaders(programs []plannedProgramHeader) []byte {
	data := make([]byte, len(programs)*elf64ProgramSize)
	for i, program := range programs {
		off := i * elf64ProgramSize
		binary.LittleEndian.PutUint32(data[off:off+4], uint32(program.type_))
		binary.LittleEndian.PutUint32(data[off+4:off+8], uint32(program.flags))
		binary.LittleEndian.PutUint64(data[off+8:off+16], program.off)
		binary.LittleEndian.PutUint64(data[off+16:off+24], program.vaddr)
		binary.LittleEndian.PutUint64(data[off+24:off+32], program.paddr)
		binary.LittleEndian.PutUint64(data[off+32:off+40], program.filesz)
		binary.LittleEndian.PutUint64(data[off+40:off+48], program.memsz)
		binary.LittleEndian.PutUint64(data[off+48:off+56], program.align)
	}
	return data
}

func validatePHDRGrowthOverlap(meta *elfMetadata, programs []plannedProgramHeader, start, end uint64) error {
	growth := fileRange{start: start, end: end}
	for _, program := range programs {
		if program.type_ == elf.PT_LOAD || program.type_ == elf.PT_PHDR || program.filesz == 0 {
			continue
		}
		programEnd, ok := checkedAdd(program.off, program.filesz)
		if !ok {
			return fmt.Errorf("program header content range overflows during growth validation")
		}
		if _, overlaps := intersectRanges(growth, fileRange{start: program.off, end: programEnd}); overlaps {
			return fmt.Errorf("program header extension overlaps %s contents", program.type_)
		}
	}
	for _, section := range meta.sections {
		if section.type_ == elf.SHT_NOBITS || section.size == 0 {
			continue
		}
		sectionEnd, ok := checkedAdd(section.off, section.size)
		if !ok {
			return fmt.Errorf("section %q range overflows during program header growth validation", section.name)
		}
		if _, overlaps := intersectRanges(growth, fileRange{start: section.off, end: sectionEnd}); overlaps {
			return fmt.Errorf("program header extension overlaps section %q", section.name)
		}
	}
	if len(meta.data) >= elf64HeaderSize {
		shoff := binary.LittleEndian.Uint64(meta.data[40:48])
		shentsize := uint64(binary.LittleEndian.Uint16(meta.data[58:60]))
		shnum := uint64(binary.LittleEndian.Uint16(meta.data[60:62]))
		if shnum != 0 {
			tableSize, ok := checkedMul(shentsize, shnum)
			if !ok {
				return fmt.Errorf("section header table size overflows during program header growth validation")
			}
			shend, ok := checkedAdd(shoff, tableSize)
			if !ok {
				return fmt.Errorf("section header table range overflows during program header growth validation")
			}
			if _, overlaps := intersectRanges(growth, fileRange{start: shoff, end: shend}); overlaps {
				return fmt.Errorf("program header extension overlaps section header table")
			}
		}
	}
	return nil
}

func phdrRangeIsFileBacked(programs []plannedProgramHeader, start, end uint64) bool {
	for _, program := range programs {
		if program.type_ != elf.PT_LOAD {
			continue
		}
		programEnd, ok := checkedAdd(program.off, program.filesz)
		if ok && start >= program.off && end <= programEnd {
			return true
		}
	}
	return false
}

func fileOffsetVA(programs []plannedProgramHeader, off uint64) (uint64, bool) {
	for _, program := range programs {
		if program.type_ != elf.PT_LOAD || off < program.off {
			continue
		}
		programEnd, ok := checkedAdd(program.off, program.filesz)
		if !ok || off >= programEnd {
			continue
		}
		delta := off - program.off
		return checkedAdd(program.vaddr, delta)
	}
	return 0, false
}

func readPlannedProgramHeader(input []byte, off uint64) plannedProgramHeader {
	return plannedProgramHeader{
		type_:  elf.ProgType(binary.LittleEndian.Uint32(input[off : off+4])),
		flags:  elf.ProgFlag(binary.LittleEndian.Uint32(input[off+4 : off+8])),
		off:    binary.LittleEndian.Uint64(input[off+8 : off+16]),
		vaddr:  binary.LittleEndian.Uint64(input[off+16 : off+24]),
		paddr:  binary.LittleEndian.Uint64(input[off+24 : off+32]),
		filesz: binary.LittleEndian.Uint64(input[off+32 : off+40]),
		memsz:  binary.LittleEndian.Uint64(input[off+40 : off+48]),
		align:  binary.LittleEndian.Uint64(input[off+48 : off+56]),
	}
}

func applyAArch64Relocation(data []byte, off uint64, type_ elf.R_AARCH64, S uint64, A int64, P uint64) error {
	if err := validateAArch64RelocationInstruction(data, off, type_); err != nil {
		return err
	}
	value, ok := addSigned(S, A)
	if !ok {
		return fmt.Errorf("S+A overflows")
	}
	switch type_ {
	case elf.R_AARCH64_ABS64:
		if !sliceHas(data, off, 8) {
			return fmt.Errorf("ABS64 target is truncated")
		}
		binary.LittleEndian.PutUint64(data[off:off+8], value)
		return nil
	case elf.R_AARCH64_PREL32:
		delta, ok := signedDifference(value, P)
		if !ok || delta < math.MinInt32 || delta > math.MaxInt32 || !sliceHas(data, off, 4) {
			return fmt.Errorf("PREL32 target is out of range")
		}
		binary.LittleEndian.PutUint32(data[off:off+4], uint32(int32(delta)))
		return nil
	case elf.R_AARCH64_LD_PREL_LO19, elf.R_AARCH64_GOT_LD_PREL19:
		delta, ok := signedDifference(value, P)
		if !ok || P%4 != 0 || delta%4 != 0 || !fitsSigned(delta>>2, 19) || !sliceHas(data, off, 4) {
			return fmt.Errorf("LD_PREL_LO19 target is out of range or unaligned")
		}
		instruction := binary.LittleEndian.Uint32(data[off : off+4])
		instruction = instruction&^(uint32(0x7ffff)<<5) | (uint32(delta>>2)&0x7ffff)<<5
		binary.LittleEndian.PutUint32(data[off:off+4], instruction)
		return nil
	case elf.R_AARCH64_ADR_PREL_LO21:
		delta, ok := signedDifference(value, P)
		if !ok || !fitsSigned(delta, 21) || !sliceHas(data, off, 4) {
			return fmt.Errorf("ADR_PREL_LO21 target is out of range")
		}
		patchADRImmediate(data, off, delta)
		return nil
	case elf.R_AARCH64_ADR_PREL_PG_HI21, elf.R_AARCH64_ADR_PREL_PG_HI21_NC:
		pageDelta, ok := signedDifference(value&^0xfff, P&^0xfff)
		if !ok || pageDelta%0x1000 != 0 || !fitsSigned(pageDelta>>12, 21) || !sliceHas(data, off, 4) {
			return fmt.Errorf("ADR_PREL_PG_HI21 target is out of range")
		}
		patchADRImmediate(data, off, pageDelta>>12)
		return nil
	case elf.R_AARCH64_ADD_ABS_LO12_NC:
		if !sliceHas(data, off, 4) {
			return fmt.Errorf("ADD_ABS_LO12_NC target is truncated")
		}
		instruction := binary.LittleEndian.Uint32(data[off : off+4])
		instruction = instruction&^(uint32(0xfff)<<10) | uint32(value&0xfff)<<10
		binary.LittleEndian.PutUint32(data[off:off+4], instruction)
		return nil
	case elf.R_AARCH64_JUMP26, elf.R_AARCH64_CALL26:
		delta, ok := signedDifference(value, P)
		if !ok || P%4 != 0 || delta%4 != 0 || !fitsSigned(delta>>2, 26) || !sliceHas(data, off, 4) {
			return fmt.Errorf("branch relocation target is out of range or unaligned")
		}
		instruction := binary.LittleEndian.Uint32(data[off : off+4])
		instruction = instruction&0xfc000000 | uint32(delta>>2)&0x03ffffff
		binary.LittleEndian.PutUint32(data[off:off+4], instruction)
		return nil
	case elf.R_AARCH64_LDST64_ABS_LO12_NC:
		if value&7 != 0 || !sliceHas(data, off, 4) {
			return fmt.Errorf("LDST64_ABS_LO12_NC target is not 8-byte aligned")
		}
		instruction := binary.LittleEndian.Uint32(data[off : off+4])
		instruction = instruction&^(uint32(0xfff)<<10) | uint32((value&0xfff)>>3)<<10
		binary.LittleEndian.PutUint32(data[off:off+4], instruction)
		return nil
	default:
		return fmt.Errorf("unsupported runtime relocation %s", type_)
	}
}

func validateAArch64RelocationInstruction(data []byte, off uint64, type_ elf.R_AARCH64) error {
	switch type_ {
	case elf.R_AARCH64_ABS64, elf.R_AARCH64_PREL32:
		return nil
	}
	if !sliceHas(data, off, 4) {
		return fmt.Errorf("%s relocation target is truncated", type_)
	}

	instruction := binary.LittleEndian.Uint32(data[off : off+4])
	valid := true
	switch type_ {
	case elf.R_AARCH64_LD_PREL_LO19:
		valid = instruction&0x3b000000 == 0x18000000 && instruction&0xc0000000 != 0xc0000000
	case elf.R_AARCH64_GOT_LD_PREL19:
		valid = instruction&0xff000000 == 0x58000000
	case elf.R_AARCH64_ADR_PREL_LO21:
		valid = instruction&0x9f000000 == 0x10000000
	case elf.R_AARCH64_ADR_PREL_PG_HI21, elf.R_AARCH64_ADR_PREL_PG_HI21_NC:
		valid = instruction&0x9f000000 == 0x90000000
	case elf.R_AARCH64_ADD_ABS_LO12_NC:
		valid = instruction&0xffc00000 == 0x91000000
	case elf.R_AARCH64_JUMP26:
		valid = instruction&0xfc000000 == 0x14000000
	case elf.R_AARCH64_CALL26:
		valid = instruction&0xfc000000 == 0x94000000
	case elf.R_AARCH64_LDST64_ABS_LO12_NC:
		valid = instruction&0x3b000000 == 0x39000000 && instruction&0xc0000000 == 0xc0000000 && (instruction>>22)&3 <= 1
	default:
		return nil
	}
	if !valid {
		return fmt.Errorf("%s relocation does not target the expected instruction class (0x%08x)", type_, instruction)
	}
	return nil
}

func patchADRImmediate(data []byte, off uint64, immediate int64) {
	instruction := binary.LittleEndian.Uint32(data[off : off+4])
	encoded := uint32(immediate) & 0x1fffff
	instruction &^= uint32(3)<<29 | uint32(0x7ffff)<<5
	instruction |= (encoded & 3) << 29
	instruction |= (encoded >> 2) << 5
	binary.LittleEndian.PutUint32(data[off:off+4], instruction)
}

func encodeBranch26(from, to uint64, opcode uint32) (uint32, error) {
	if from%4 != 0 || to%4 != 0 {
		return 0, fmt.Errorf("branch source and target must be 4-byte aligned")
	}
	delta, ok := signedDifference(to, from)
	if !ok || delta%4 != 0 || !fitsSigned(delta>>2, 26) {
		return 0, fmt.Errorf("B imm26 range exceeded")
	}
	return opcode | uint32(delta>>2)&0x03ffffff, nil
}

func growAligned(data *[]byte, alignment, size uint64) (int, error) {
	start, ok := alignUpChecked(uint64(len(*data)), alignment)
	if !ok {
		return 0, fmt.Errorf("alignment overflows")
	}
	end, ok := checkedAdd(start, size)
	if !ok || end > uint64(math.MaxInt) {
		return 0, fmt.Errorf("range exceeds addressable buffer")
	}
	if uint64(len(*data)) < end {
		*data = append(*data, make([]byte, int(end)-len(*data))...)
	}
	return int(start), nil
}

func alignUpChecked(value, alignment uint64) (uint64, bool) {
	if alignment == 0 || !isPowerOfTwo(alignment) {
		return 0, false
	}
	mask := alignment - 1
	if value > math.MaxUint64-mask {
		return 0, false
	}
	return (value + mask) &^ mask, true
}

func checkedMul(a, b uint64) (uint64, bool) {
	if a != 0 && b > math.MaxUint64/a {
		return 0, false
	}
	return a * b, true
}

func addSigned(base uint64, delta int64) (uint64, bool) {
	if delta >= 0 {
		return checkedAdd(base, uint64(delta))
	}
	amount := uint64(-(delta + 1)) + 1
	if amount > base {
		return 0, false
	}
	return base - amount, true
}

func signedDifference(a, b uint64) (int64, bool) {
	if a >= b {
		delta := a - b
		if delta > math.MaxInt64 {
			return 0, false
		}
		return int64(delta), true
	}
	delta := b - a
	if delta > uint64(math.MaxInt64)+1 {
		return 0, false
	}
	if delta == uint64(math.MaxInt64)+1 {
		return math.MinInt64, true
	}
	return -int64(delta), true
}

func fitsSigned(value int64, bits uint) bool {
	if bits == 0 || bits >= 64 {
		return bits == 64
	}
	limit := int64(1) << (bits - 1)
	return value >= -limit && value < limit
}

func sliceHas(data []byte, off, size uint64) bool {
	return off <= uint64(len(data)) && size <= uint64(len(data))-off
}
