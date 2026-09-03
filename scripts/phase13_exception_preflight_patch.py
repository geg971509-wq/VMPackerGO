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
# 1. ARM64 translator: expose an always-on sorted source map independent of
#    debug logging. This reuses the authoritative labels used by the trailer.
# ---------------------------------------------------------------------------
replace_once(
    "internal/arch/arm64/translator.go",
    '''\tNativeCallSites    []NativeCallSite
\tEntryBTI           Op
''',
    '''\tNativeCallSites    []NativeCallSite
\tSourceMap          []SourceMapEntry
\tEntryBTI           Op
''',
)
replace_once(
    "internal/arch/arm64/translator.go",
    '''type NativeCallSite struct {
\tARM64Offset int
\tVMOffset    int
}

// DebugEntry 单条指令的 debug 对照信息
''',
    '''type NativeCallSite struct {
\tARM64Offset int
\tVMOffset    int
}

type SourceMapEntry struct {
\tARM64Offset int
\tVMOffset    int
}

// DebugEntry 单条指令的 debug 对照信息
''',
)
replace_once(
    "internal/arch/arm64/translator.go",
    '''\tfor _, arm64Off := range arm64Offsets {
\t\tvmOff := t.labels[arm64Off]
\t\tt.emitU32(uint32(arm64Off))
\t\tt.emitU32(uint32(vmOff))
\t}
''',
    '''\tfor _, arm64Off := range arm64Offsets {
\t\tvmOff := t.labels[arm64Off]
\t\tresult.SourceMap = append(result.SourceMap, SourceMapEntry{ARM64Offset: arm64Off, VMOffset: vmOff})
\t\tt.emitU32(uint32(arm64Off))
\t\tt.emitU32(uint32(vmOff))
\t}
''',
)

Path("internal/arch/arm64/source_map_test.go").write_text(r'''package arm64

import (
	"testing"

	"github.com/vmpacker/internal/vm"
)

func TestTranslateResultSourceMapCoversMergedOffsetsAndFunctionEnd(t *testing.T) {
	decoder := NewDecoder()
	raws := []uint32{
		0xc85ffc20, // ldaxr x0, [x1]
		0x91000400, // add x0, x0, #1
		0xc802fc20, // stlxr w2, x0, [x1]
	}
	instructions := make([]vm.Instruction, len(raws))
	for i, raw := range raws {
		instructions[i] = decoder.Decode(raw, i*4)
	}
	translator, err := NewTranslator(0x1000, len(raws)*4, vm.IdentityOpcodeMap())
	if err != nil {
		t.Fatal(err)
	}
	result, err := translator.Translate(instructions)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Unsupported) != 0 {
		t.Fatalf("unsupported=%v", result.Unsupported)
	}
	wantOffsets := []int{0, 4, 8, 12}
	if len(result.SourceMap) != len(wantOffsets) {
		t.Fatalf("source map=%v", result.SourceMap)
	}
	for i, want := range wantOffsets {
		if result.SourceMap[i].ARM64Offset != want {
			t.Fatalf("source map[%d]=%+v want offset=%d", i, result.SourceMap[i], want)
		}
		if i > 0 && result.SourceMap[i-1].ARM64Offset >= result.SourceMap[i].ARM64Offset {
			t.Fatalf("source map is not strictly sorted: %v", result.SourceMap)
		}
	}
	if result.SourceMap[1].VMOffset != result.SourceMap[2].VMOffset {
		t.Fatalf("merged exclusive offsets did not share VM continuation: %v", result.SourceMap)
	}
	if result.SourceMap[len(result.SourceMap)-1].VMOffset != result.SourceMap[2].VMOffset {
		t.Fatalf("function-end mapping=%v", result.SourceMap)
	}
}

func TestNativeCallSiteUsesSameSourceMapVMOffset(t *testing.T) {
	instructions := []vm.Instruction{
		{Op: int(BL), Offset: 0, Imm: 0x100},
		{Op: int(NOP), Offset: 4},
		{Op: int(NOP), Offset: 8},
	}
	translator, err := NewTranslator(0x2000, 12, vm.IdentityOpcodeMap())
	if err != nil {
		t.Fatal(err)
	}
	result, err := translator.Translate(instructions)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Unsupported) != 0 || len(result.NativeCallSites) != 1 {
		t.Fatalf("unsupported=%v calls=%v", result.Unsupported, result.NativeCallSites)
	}
	call := result.NativeCallSites[0]
	found := false
	for _, entry := range result.SourceMap {
		if entry.ARM64Offset == call.ARM64Offset {
			found = true
			if entry.VMOffset != call.VMOffset {
				t.Fatalf("call=%+v source=%+v", call, entry)
			}
		}
	}
	if !found {
		t.Fatalf("native call offset %d missing from source map %v", call.ARM64Offset, result.SourceMap)
	}
}
''')

# ---------------------------------------------------------------------------
# 2. Translation preparation: retain validated exception bridge requirements
#    and compute them after ordinary native requirements are collected.
# ---------------------------------------------------------------------------
replace_once(
    "internal/elf/preparation.go",
    '''\tFPSIMDInstructions []uint32

\topcodeMapDigest [sha256.Size]byte
''',
    '''\tFPSIMDInstructions []uint32
\tExceptionBridges   []PreparedExceptionBridge

\topcodeMapDigest [sha256.Size]byte
''',
)
replace_once(
    "internal/elf/preparation.go",
    '''\tfor raw := range fpSIMD {
\t\tpreparation.FPSIMDInstructions = append(preparation.FPSIMDInstructions, raw)
\t}
\tsort.Slice(preparation.FPSIMDInstructions, func(i, j int) bool {
\t\treturn preparation.FPSIMDInstructions[i] < preparation.FPSIMDInstructions[j]
\t})
\treturn preparation, nil
}
''',
    '''\tfor raw := range fpSIMD {
\t\tpreparation.FPSIMDInstructions = append(preparation.FPSIMDInstructions, raw)
\t}
\tsort.Slice(preparation.FPSIMDInstructions, func(i, j int) bool {
\t\treturn preparation.FPSIMDInstructions[i] < preparation.FPSIMDInstructions[j]
\t})
\texceptionBridges, err := prepareExceptionBridges(req, preparation.Functions)
\tif err != nil {
\t\treturn nil, err
\t}
\tpreparation.ExceptionBridges = exceptionBridges
\treturn preparation, nil
}
''',
)
replace_once(
    "internal/elf/preparation.go",
    '''func (preparation *TranslationPreparation) ValidateRuntimeImage(image *vmruntime.Image) error {
\tif preparation == nil {
\t\treturn fmt.Errorf("translation preparation is required")
\t}
\tif image == nil {
''',
    '''func (preparation *TranslationPreparation) ValidateRuntimeImage(image *vmruntime.Image) error {
\tif preparation == nil {
\t\treturn fmt.Errorf("translation preparation is required")
\t}
\tif err := preparation.ValidateRuntimeRequirements(); err != nil {
\t\treturn err
\t}
\tif image == nil {
''',
)

Path("internal/elf/exception_preflight.go").write_text(r'''package elf

import (
	"debug/elf"
	"encoding/binary"
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/vmpacker/internal/arch/arm64"
	"github.com/vmpacker/internal/unwind"
)

type PreparedExceptionBridge struct {
	Selection Selection
	Plan      *unwind.ExceptionBridgePlan
}

func (preparation *TranslationPreparation) ValidateRuntimeRequirements() error {
	if preparation == nil {
		return fmt.Errorf("translation preparation is required")
	}
	if len(preparation.ExceptionBridges) == 0 {
		return nil
	}
	bridge := preparation.ExceptionBridges[0]
	thunks := 0
	if bridge.Plan != nil {
		thunks = len(bridge.Plan.Thunks)
	}
	return fmt.Errorf("function %q requires %d C++ exception landing bridge(s); runtime exception bridge is not integrated", bridge.Selection.Name, thunks)
}

func prepareExceptionBridges(req Request, functions []PreparedFunction) ([]PreparedExceptionBridge, error) {
	hasNativeCalls := false
	for _, function := range functions {
		if function.Translation != nil && len(function.Translation.NativeCallSites) != 0 {
			hasNativeCalls = true
			break
		}
	}
	if !hasNativeCalls {
		return nil, nil
	}

	mode := AndroidMode(strings.ToLower(req.Mode))
	if mode == "" {
		mode = AndroidModeAuto
	}
	meta, err := parseELFMetadata(req.Input, mode)
	if err != nil {
		return nil, fmt.Errorf("exception preflight ELF metadata: %w", err)
	}
	defer meta.file.Close()

	ehSection := meta.file.Section(".eh_frame")
	if ehSection == nil {
		if meta.file.Section(".gcc_except_table") != nil {
			return nil, fmt.Errorf("exception preflight found .gcc_except_table without an accessible .eh_frame section")
		}
		return nil, nil
	}
	ehData, err := ehSection.Data()
	if err != nil {
		return nil, fmt.Errorf("read .eh_frame for exception preflight: %w", err)
	}
	frame, err := unwind.ParseEHFrame(ehData, ehSection.Addr, binary.LittleEndian, 8)
	if err != nil {
		return nil, fmt.Errorf("parse .eh_frame for exception preflight: %w", err)
	}

	var result []PreparedExceptionBridge
	for _, function := range functions {
		translation := function.Translation
		if translation == nil || len(translation.NativeCallSites) == 0 {
			continue
		}
		fde, err := findSelectionFDE(frame, function.Selection)
		if err != nil {
			return nil, fmt.Errorf("function %q exception preflight: %w", function.Selection.Name, err)
		}
		if fde == nil || fde.LSDA == nil {
			continue
		}
		lsdaBytes, err := allocSectionBytesAtVA(meta.file, *fde.LSDA)
		if err != nil {
			return nil, fmt.Errorf("function %q resolve LSDA: %w", function.Selection.Name, err)
		}
		lsda, err := unwind.ParseLSDA(lsdaBytes, *fde.LSDA, function.Selection.Address, binary.LittleEndian, 8)
		if err != nil {
			return nil, fmt.Errorf("function %q parse LSDA: %w", function.Selection.Name, err)
		}
		cie := frame.CIEs[fde.CIEOffset]
		bridge, err := planPreparedExceptionBridge(function.Selection, translation, cie, fde, lsda)
		if err != nil {
			return nil, fmt.Errorf("function %q exception bridge: %w", function.Selection.Name, err)
		}
		if bridge != nil {
			result = append(result, *bridge)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Selection.Address != result[j].Selection.Address {
			return result[i].Selection.Address < result[j].Selection.Address
		}
		return result[i].Selection.Name < result[j].Selection.Name
	})
	return result, nil
}

func findSelectionFDE(frame *unwind.Frame, selection Selection) (*unwind.FDE, error) {
	if frame == nil {
		return nil, fmt.Errorf("unwind frame is required")
	}
	var candidate *unwind.FDE
	partialOverlap := false
	for i := range frame.FDEs {
		fde := &frame.FDEs[i]
		if fde.AddressRange == 0 {
			continue
		}
		end, ok := checkedAdd(fde.InitialLocation, fde.AddressRange)
		if !ok {
			return nil, fmt.Errorf("FDE at 0x%x address range overflows", fde.Offset)
		}
		if !rangesOverlap(selection.Address, selection.End, fde.InitialLocation, end) {
			continue
		}
		if fde.InitialLocation <= selection.Address && end >= selection.End {
			if candidate != nil {
				return nil, fmt.Errorf("selection is covered by multiple FDEs")
			}
			candidate = fde
			continue
		}
		partialOverlap = true
	}
	if partialOverlap {
		return nil, fmt.Errorf("selection partially overlaps unwind FDE coverage")
	}
	return candidate, nil
}

func allocSectionBytesAtVA(file *elf.File, address uint64) ([]byte, error) {
	if file == nil {
		return nil, fmt.Errorf("ELF file is required")
	}
	var found []byte
	var foundName string
	for _, section := range file.Sections {
		if section == nil || section.Type == elf.SHT_NOBITS || section.Flags&elf.SHF_ALLOC == 0 || section.Size == 0 {
			continue
		}
		end, ok := checkedAdd(section.Addr, section.Size)
		if !ok || address < section.Addr || address >= end {
			continue
		}
		if found != nil {
			return nil, fmt.Errorf("virtual address 0x%x is covered by multiple allocatable sections (%s, %s)", address, foundName, section.Name)
		}
		data, err := section.Data()
		if err != nil {
			return nil, fmt.Errorf("read allocatable section %q: %w", section.Name, err)
		}
		offset := address - section.Addr
		if offset > uint64(len(data)) {
			return nil, fmt.Errorf("virtual address 0x%x exceeds section %q data", address, section.Name)
		}
		found = append([]byte(nil), data[offset:]...)
		foundName = section.Name
	}
	if found == nil {
		return nil, fmt.Errorf("virtual address 0x%x is not in an allocatable file-backed section", address)
	}
	return found, nil
}

func planPreparedExceptionBridge(selection Selection, translation *arm64.TranslateResult, cie *unwind.CIE, fde *unwind.FDE, lsda *unwind.LSDA) (*PreparedExceptionBridge, error) {
	if translation == nil || fde == nil || lsda == nil {
		return nil, fmt.Errorf("translation, FDE, and LSDA are required")
	}
	mapper, err := sourceMapPCMapper(selection, translation.SourceMap)
	if err != nil {
		return nil, err
	}
	calls, err := exceptionNativeCallLocations(selection, translation.NativeCallSites, mapper)
	if err != nil {
		return nil, err
	}
	needed, err := nativeCallNeedsLocalLanding(lsda, calls)
	if err != nil {
		return nil, err
	}
	if !needed {
		return nil, nil
	}
	if cie == nil {
		return nil, fmt.Errorf("FDE references a missing CIE")
	}
	mapped, err := unwind.MapCallSites(lsda, mapper)
	if err != nil {
		return nil, err
	}
	plan, err := unwind.PlanExceptionBridge(cie, fde, lsda, mapped, calls)
	if err != nil {
		return nil, err
	}
	return &PreparedExceptionBridge{Selection: selection, Plan: plan}, nil
}

func sourceMapPCMapper(selection Selection, entries []arm64.SourceMapEntry) (func(uint64) (uint32, bool), error) {
	if len(entries) == 0 {
		return nil, fmt.Errorf("translation source map is empty")
	}
	byPC := make(map[uint64]uint32, len(entries))
	previousOffset := -1
	for _, entry := range entries {
		if entry.ARM64Offset < 0 || entry.VMOffset < 0 || entry.VMOffset > math.MaxUint32 {
			return nil, fmt.Errorf("translation source map contains an invalid entry %+v", entry)
		}
		if entry.ARM64Offset <= previousOffset {
			return nil, fmt.Errorf("translation source map is not strictly sorted")
		}
		if uint64(entry.ARM64Offset) > selection.Size() {
			return nil, fmt.Errorf("source offset 0x%x exceeds selected function", entry.ARM64Offset)
		}
		pc, ok := checkedAdd(selection.Address, uint64(entry.ARM64Offset))
		if !ok {
			return nil, fmt.Errorf("source PC overflows")
		}
		if _, duplicate := byPC[pc]; duplicate {
			return nil, fmt.Errorf("duplicate source PC 0x%x", pc)
		}
		byPC[pc] = uint32(entry.VMOffset)
		previousOffset = entry.ARM64Offset
	}
	if _, ok := byPC[selection.End]; !ok {
		return nil, fmt.Errorf("translation source map has no function-end entry")
	}
	return func(pc uint64) (uint32, bool) {
		value, ok := byPC[pc]
		return value, ok
	}, nil
}

func exceptionNativeCallLocations(selection Selection, sites []arm64.NativeCallSite, mapper func(uint64) (uint32, bool)) ([]unwind.NativeCallLocation, error) {
	calls := make([]unwind.NativeCallLocation, 0, len(sites))
	for _, site := range sites {
		if site.ARM64Offset < 0 || site.VMOffset < 0 || site.VMOffset > math.MaxUint32 || uint64(site.ARM64Offset) >= selection.Size() {
			return nil, fmt.Errorf("native call site %+v is outside the selected function", site)
		}
		pc, ok := checkedAdd(selection.Address, uint64(site.ARM64Offset))
		if !ok {
			return nil, fmt.Errorf("native call PC overflows")
		}
		mapped, ok := mapper(pc)
		if !ok || mapped != uint32(site.VMOffset) {
			return nil, fmt.Errorf("native call at 0x%x does not match translation source map", pc)
		}
		calls = append(calls, unwind.NativeCallLocation{OriginalPC: pc, VMOffset: uint32(site.VMOffset)})
	}
	return calls, nil
}

func nativeCallNeedsLocalLanding(lsda *unwind.LSDA, calls []unwind.NativeCallLocation) (bool, error) {
	if lsda == nil {
		return false, fmt.Errorf("LSDA is required")
	}
	for _, site := range lsda.CallSites {
		end, ok := checkedAdd(site.Start, site.Length)
		if !ok {
			return false, fmt.Errorf("LSDA call-site range at 0x%x overflows", site.Start)
		}
		if site.LandingPad == 0 {
			continue
		}
		for _, call := range calls {
			if call.OriginalPC >= site.Start && call.OriginalPC < end {
				return true, nil
			}
		}
	}
	return false, nil
}
''')

Path("internal/elf/exception_preflight_test.go").write_text(r'''package elf

import (
	"strings"
	"testing"

	"github.com/vmpacker/internal/arch/arm64"
	"github.com/vmpacker/internal/unwind"
)

func exceptionFixture() (Selection, *arm64.TranslateResult, *unwind.CIE, *unwind.FDE, *unwind.LSDA) {
	personality := uint64(0x7000)
	selection := Selection{Name: "cpp_target", Address: 0x1000, End: 0x1020}
	translation := &arm64.TranslateResult{
		SourceMap: []arm64.SourceMapEntry{
			{ARM64Offset: 0, VMOffset: 0},
			{ARM64Offset: 4, VMOffset: 11},
			{ARM64Offset: 8, VMOffset: 20},
			{ARM64Offset: 16, VMOffset: 28},
			{ARM64Offset: 24, VMOffset: 36},
			{ARM64Offset: 32, VMOffset: 44},
		},
		NativeCallSites: []arm64.NativeCallSite{{ARM64Offset: 4, VMOffset: 11}},
	}
	cie := &unwind.CIE{Offset: 0x10, Personality: &personality, PersonalityEncoding: unwind.PEAbsptr}
	lsdaVA := uint64(0x5000)
	fde := &unwind.FDE{Offset: 0x40, CIEOffset: cie.Offset, InitialLocation: 0x1000, AddressRange: 0x20, LSDA: &lsdaVA}
	lsda := &unwind.LSDA{
		CallSites: []unwind.CallSite{{Start: 0x1000, Length: 0x10, LandingPad: 0x1018, Action: 1}},
		ActionChains: map[uint64][]unwind.ActionRecord{1: {{Offset: 1, TypeFilter: 0, Next: 0}}},
		TypeInfos: map[uint64]unwind.TypeInfo{},
		TypeEncoding: unwind.PEOmit,
	}
	return selection, translation, cie, fde, lsda
}

func TestPlanPreparedExceptionBridgeDetectsLocalLandingNativeCall(t *testing.T) {
	selection, translation, cie, fde, lsda := exceptionFixture()
	bridge, err := planPreparedExceptionBridge(selection, translation, cie, fde, lsda)
	if err != nil {
		t.Fatal(err)
	}
	if bridge == nil || bridge.Plan == nil || len(bridge.Plan.Thunks) != 1 {
		t.Fatalf("bridge=%+v", bridge)
	}
	thunk := bridge.Plan.Thunks[0]
	if thunk.OriginalPC != 0x1004 || thunk.VMCallOffset != 11 || thunk.VMLandingPad != 36 {
		t.Fatalf("thunk=%+v", thunk)
	}
}

func TestPlanPreparedExceptionBridgeAllowsUnwindThroughAndCallsOutsideEHRange(t *testing.T) {
	selection, translation, cie, fde, lsda := exceptionFixture()
	lsda.CallSites[0].LandingPad = 0
	bridge, err := planPreparedExceptionBridge(selection, translation, cie, fde, lsda)
	if err != nil || bridge != nil {
		t.Fatalf("unwind-through bridge=%+v err=%v", bridge, err)
	}

	_, translation, cie, fde, lsda = exceptionFixture()
	lsda.CallSites[0] = unwind.CallSite{Start: 0x1010, Length: 0x10, LandingPad: 0x1018, Action: 1}
	bridge, err = planPreparedExceptionBridge(selection, translation, cie, fde, lsda)
	if err != nil || bridge != nil {
		t.Fatalf("outside-range bridge=%+v err=%v", bridge, err)
	}
}

func TestPlanPreparedExceptionBridgeFailsClosedOnMissingLandingMappingOrPersonality(t *testing.T) {
	selection, translation, cie, fde, lsda := exceptionFixture()
	translation.SourceMap = translation.SourceMap[:4]
	translation.SourceMap = append(translation.SourceMap, arm64.SourceMapEntry{ARM64Offset: 32, VMOffset: 44})
	if bridge, err := planPreparedExceptionBridge(selection, translation, cie, fde, lsda); err == nil || bridge != nil || !strings.Contains(err.Error(), "landing") {
		t.Fatalf("missing landing bridge=%+v err=%v", bridge, err)
	}

	selection, translation, _, fde, lsda = exceptionFixture()
	if bridge, err := planPreparedExceptionBridge(selection, translation, &unwind.CIE{}, fde, lsda); err == nil || bridge != nil || !strings.Contains(err.Error(), "personality") {
		t.Fatalf("missing personality bridge=%+v err=%v", bridge, err)
	}
}

func TestFindSelectionFDEFailsClosedOnPartialOrAmbiguousCoverage(t *testing.T) {
	selection := Selection{Name: "target", Address: 0x1000, End: 0x1020}
	frame := &unwind.Frame{FDEs: []unwind.FDE{{Offset: 1, InitialLocation: 0x1000, AddressRange: 0x10}}}
	if fde, err := findSelectionFDE(frame, selection); err == nil || fde != nil || !strings.Contains(err.Error(), "partially") {
		t.Fatalf("partial fde=%+v err=%v", fde, err)
	}

	frame.FDEs = []unwind.FDE{
		{Offset: 1, InitialLocation: 0x1000, AddressRange: 0x20},
		{Offset: 2, InitialLocation: 0x0ff0, AddressRange: 0x40},
	}
	if fde, err := findSelectionFDE(frame, selection); err == nil || fde != nil || !strings.Contains(err.Error(), "multiple") {
		t.Fatalf("ambiguous fde=%+v err=%v", fde, err)
	}

	frame.FDEs = []unwind.FDE{{Offset: 3, InitialLocation: 0x2000, AddressRange: 0x20}}
	fde, err := findSelectionFDE(frame, selection)
	if err != nil || fde != nil {
		t.Fatalf("unrelated fde=%+v err=%v", fde, err)
	}
}

func TestPendingExceptionBridgeBlocksRuntimeValidation(t *testing.T) {
	selection, translation, cie, fde, lsda := exceptionFixture()
	bridge, err := planPreparedExceptionBridge(selection, translation, cie, fde, lsda)
	if err != nil {
		t.Fatal(err)
	}
	preparation := &TranslationPreparation{ExceptionBridges: []PreparedExceptionBridge{*bridge}}
	if err := preparation.ValidateRuntimeRequirements(); err == nil || !strings.Contains(err.Error(), "runtime exception bridge is not integrated") {
		t.Fatalf("pending bridge err=%v", err)
	}
	if err := preparation.ValidateRuntimeImage(nil); err == nil || !strings.Contains(err.Error(), "runtime exception bridge is not integrated") {
		t.Fatalf("runtime validation err=%v", err)
	}
	if err := (&TranslationPreparation{}).ValidateRuntimeRequirements(); err != nil {
		t.Fatalf("empty requirements: %v", err)
	}
}
''')

# ---------------------------------------------------------------------------
# 3. CLI production path: refuse pending bridge requirements before spending
#    work on a runtime that cannot satisfy them.
# ---------------------------------------------------------------------------
replace_once(
    "internal/app/run.go",
    '''\t\t\t\t\tif prepareErr != nil {
\t\t\t\t\t\ttransformErr = prepareErr
\t\t\t\t\t} else {
\t\t\t\t\t\tbuilder := cfg.BuildRuntime
''',
    '''\t\t\t\t\tif prepareErr != nil {
\t\t\t\t\t\ttransformErr = prepareErr
\t\t\t\t\t} else if requirementsErr := preparation.ValidateRuntimeRequirements(); requirementsErr != nil {
\t\t\t\t\t\ttransformErr = requirementsErr
\t\t\t\t\t} else {
\t\t\t\t\t\tbuilder := cfg.BuildRuntime
''',
)

print("phase13 exception preflight patch applied")
