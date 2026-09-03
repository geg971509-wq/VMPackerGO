package elf

import (
	"fmt"
	"math"

	"github.com/vmpacker/internal/arch/arm64"
	"github.com/vmpacker/internal/unwind"
	"github.com/vmpacker/internal/vm"
)

func resolvePreparedExceptionRoutes(bridges []PreparedExceptionBridge, functions []PreparedFunction, opcodes vm.OpcodeMap) error {
	if len(bridges) == 0 {
		return nil
	}
	functionBySelection := make(map[string]*arm64.TranslateResult, len(functions))
	for i := range functions {
		function := &functions[i]
		key := exceptionSelectionKey(function.Selection)
		if _, exists := functionBySelection[key]; exists {
			return fmt.Errorf("duplicate prepared function selection for exception routing")
		}
		functionBySelection[key] = function.Translation
	}
	for i := range bridges {
		bridge := &bridges[i]
		translation, ok := functionBySelection[exceptionSelectionKey(bridge.Selection)]
		if !ok || translation == nil {
			return fmt.Errorf("function %q exception route has no prepared translation", bridge.Selection.Name)
		}
		routes, err := resolveExceptionRoutes(bridge.Selection, translation, bridge.Plan, opcodes)
		if err != nil {
			return fmt.Errorf("function %q final exception routes: %w", bridge.Selection.Name, err)
		}
		bridge.Routes = routes
	}
	return nil
}

func exceptionSelectionKey(selection Selection) string {
	return fmt.Sprintf("%s\x00%d\x00%d\x00%s", selection.Name, selection.Address, selection.End, selection.Section)
}

func resolveExceptionRoutes(selection Selection, translation *arm64.TranslateResult, plan *unwind.ExceptionBridgePlan, opcodes vm.OpcodeMap) ([]PreparedExceptionRoute, error) {
	if translation == nil || plan == nil {
		return nil, fmt.Errorf("translation and exception bridge plan are required")
	}
	if len(plan.Thunks) == 0 {
		return nil, fmt.Errorf("exception bridge plan has no invoke thunks")
	}
	if _, err := validateBytecodeTrailer(translation.Bytecode, translation.CodeLen); err != nil {
		return nil, fmt.Errorf("validate translated bytecode trailer: %w", err)
	}
	_, offsetMap, err := reverseInstructions(translation.Bytecode, translation.CodeLen, opcodes)
	if err != nil {
		return nil, fmt.Errorf("build final bytecode offset map: %w", err)
	}
	sourceVM, err := exceptionSourceVMOffsets(selection, translation.SourceMap)
	if err != nil {
		return nil, err
	}

	routes := make([]PreparedExceptionRoute, 0, len(plan.Thunks))
	seenIDs := make(map[uint32]struct{}, len(plan.Thunks))
	for _, thunk := range plan.Thunks {
		if thunk.ID == 0 {
			return nil, fmt.Errorf("invoke thunk has zero identifier")
		}
		if _, duplicate := seenIDs[thunk.ID]; duplicate {
			return nil, fmt.Errorf("duplicate invoke thunk identifier 0x%08x", thunk.ID)
		}
		seenIDs[thunk.ID] = struct{}{}
		if thunk.OriginalPC < selection.Address || thunk.OriginalPC >= selection.End {
			return nil, fmt.Errorf("invoke thunk 0x%08x call PC 0x%x is outside selected function", thunk.ID, thunk.OriginalPC)
		}
		if thunk.OriginalLandingPad < selection.Address || thunk.OriginalLandingPad >= selection.End {
			return nil, fmt.Errorf("invoke thunk 0x%08x landing PC 0x%x is outside selected function", thunk.ID, thunk.OriginalLandingPad)
		}
		callOld, ok := sourceVM[thunk.OriginalPC]
		if !ok || callOld != thunk.VMCallOffset {
			return nil, fmt.Errorf("invoke thunk 0x%08x call offset does not match translation source map", thunk.ID)
		}
		landingOld, ok := sourceVM[thunk.OriginalLandingPad]
		if !ok || landingOld != thunk.VMLandingPad {
			return nil, fmt.Errorf("invoke thunk 0x%08x landing offset does not match translation source map", thunk.ID)
		}
		callFinal, ok := offsetMap[int(callOld)]
		if !ok || callFinal < 0 || uint64(callFinal) > math.MaxUint32 {
			return nil, fmt.Errorf("invoke thunk 0x%08x call offset is not a final VM boundary", thunk.ID)
		}
		landingFinal, ok := offsetMap[int(landingOld)]
		if !ok || landingFinal < 0 || uint64(landingFinal) > math.MaxUint32 {
			return nil, fmt.Errorf("invoke thunk 0x%08x landing offset is not a final VM boundary", thunk.ID)
		}
		routes = append(routes, PreparedExceptionRoute{
			ThunkID:              thunk.ID,
			OriginalCallPC:       thunk.OriginalPC,
			OriginalLandingPad:   thunk.OriginalLandingPad,
			FinalVMCallOffset:    uint32(callFinal),
			FinalVMLandingOffset: uint32(landingFinal),
		})
	}
	return routes, nil
}

func exceptionSourceVMOffsets(selection Selection, entries []arm64.SourceMapEntry) (map[uint64]uint32, error) {
	if len(entries) == 0 {
		return nil, fmt.Errorf("translation source map is empty")
	}
	result := make(map[uint64]uint32, len(entries))
	previousOffset := -1
	for _, entry := range entries {
		if entry.ARM64Offset < 0 || entry.VMOffset < 0 || uint64(entry.VMOffset) > math.MaxUint32 {
			return nil, fmt.Errorf("translation source map contains an invalid entry %+v", entry)
		}
		if entry.ARM64Offset <= previousOffset || uint64(entry.ARM64Offset) > selection.Size() {
			return nil, fmt.Errorf("translation source map is not strictly valid for selected function")
		}
		pc, ok := checkedAdd(selection.Address, uint64(entry.ARM64Offset))
		if !ok {
			return nil, fmt.Errorf("translation source PC overflows")
		}
		if _, duplicate := result[pc]; duplicate {
			return nil, fmt.Errorf("duplicate translation source PC 0x%x", pc)
		}
		result[pc] = uint32(entry.VMOffset)
		previousOffset = entry.ARM64Offset
	}
	return result, nil
}
