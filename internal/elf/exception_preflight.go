package elf

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

type PreparedExceptionRoute struct {
	ThunkID              uint32
	OriginalCallPC       uint64
	OriginalLandingPad   uint64
	FinalVMCallOffset    uint32
	FinalVMLandingOffset uint32
}

type PreparedExceptionBridge struct {
	Selection Selection
	Plan      *unwind.ExceptionBridgePlan
	Routes    []PreparedExceptionRoute
}

func (preparation *TranslationPreparation) ValidateRuntimeRequirements() error {
	if preparation == nil {
		return fmt.Errorf("translation preparation is required")
	}
	_, err := preparation.RuntimeExceptionInvokes()
	return err
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
