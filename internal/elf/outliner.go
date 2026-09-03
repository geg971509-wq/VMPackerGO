package elf

import (
	"encoding/binary"
	"fmt"
	"strings"

	"github.com/geg971509-wq/VMPackerGO/internal/arch/arm64"
	"github.com/geg971509-wq/VMPackerGO/internal/vm"
)

type outlinedTailHelper struct {
	name    string
	address uint64
	raws    []uint32
}

func isCompilerOutlinedFunctionName(name string) bool {
	const prefix = "OUTLINED_FUNCTION_"
	if !strings.HasPrefix(name, prefix) {
		return false
	}
	suffix := strings.TrimPrefix(name, prefix)
	if suffix == "" {
		return false
	}
	for _, r := range suffix {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func outlinedHelperName(names []string) (string, bool, error) {
	candidate := ""
	for _, name := range names {
		base := baseSymbolName(name)
		if !isCompilerOutlinedFunctionName(base) {
			continue
		}
		if candidate != "" && candidate != base {
			return "", true, fmt.Errorf("external branch has multiple outlined-helper identities %q and %q", candidate, base)
		}
		candidate = base
	}
	if candidate == "" {
		return "", false, nil
	}
	for _, name := range names {
		if baseSymbolName(name) != candidate {
			return "", true, fmt.Errorf("outlined-helper target %q has conflicting symbol identity %q", candidate, name)
		}
	}
	return candidate, true, nil
}

func resolveOutlinedTailHelper(input []byte, meta *elfMetadata, symbols *symbolIndex, selection Selection, branchSite, target uint64, names []string) (outlinedTailHelper, bool, error) {
	name, matched, err := outlinedHelperName(names)
	if err != nil || !matched {
		return outlinedTailHelper{}, matched, err
	}
	branchEnd, ok := checkedAdd(branchSite, 4)
	if !ok || branchEnd != selection.End {
		return outlinedTailHelper{}, true, fmt.Errorf("outlined-helper branch at 0x%X is not the final instruction of function %q", branchSite, selection.Name)
	}
	if len(symbols.relocatedAt[branchSite]) != 0 || symbols.relocationErrAt[branchSite] != nil {
		return outlinedTailHelper{}, true, fmt.Errorf("outlined-helper branch at 0x%X is relocation-backed or ambiguous", branchSite)
	}
	symbol, err := symbols.resolve(name)
	if err != nil {
		return outlinedTailHelper{}, true, err
	}
	if symbol.addr != target {
		return outlinedTailHelper{}, true, fmt.Errorf("outlined helper %q starts at 0x%X, branch targets 0x%X", name, symbol.addr, target)
	}
	if symbol.size == 0 || symbol.size%4 != 0 {
		return outlinedTailHelper{}, true, fmt.Errorf("outlined helper %q has invalid symbol size %d", name, symbol.size)
	}
	instructionCount := symbol.size / 4
	if instructionCount > arm64.MaxOutlinedTailHelperInstructions {
		return outlinedTailHelper{}, true, fmt.Errorf("outlined helper %q has %d instructions; maximum is %d", name, instructionCount, arm64.MaxOutlinedTailHelperInstructions)
	}
	helperEnd, ok := checkedAdd(symbol.addr, symbol.size)
	if !ok {
		return outlinedTailHelper{}, true, fmt.Errorf("outlined helper %q address range overflows", name)
	}
	if rangesOverlap(selection.Address, selection.End, symbol.addr, helperEnd) {
		return outlinedTailHelper{}, true, fmt.Errorf("outlined helper %q overlaps selected function %q", name, selection.Name)
	}
	mapping, ok := meta.executableMapping(symbol.addr, helperEnd)
	if !ok {
		return outlinedTailHelper{}, true, fmt.Errorf("outlined helper %q is not fully file-backed by one executable PT_LOAD", name)
	}
	fileOff, ok := mappingFileOffset(mapping, symbol.addr)
	if !ok || fileOff > uint64(len(input)) || symbol.size > uint64(len(input))-fileOff {
		return outlinedTailHelper{}, true, fmt.Errorf("outlined helper %q file range is unavailable", name)
	}
	raws := make([]uint32, int(instructionCount))
	for i := range raws {
		address := symbol.addr + uint64(i)*4
		if len(symbols.relocatedAt[address]) != 0 || symbols.relocationErrAt[address] != nil {
			return outlinedTailHelper{}, true, fmt.Errorf("outlined helper %q instruction at 0x%X has a direct relocation", name, address)
		}
		raws[i] = binary.LittleEndian.Uint32(input[fileOff+uint64(i)*4:])
	}
	if err := arm64.ValidateOutlinedTailHelper(raws); err != nil {
		return outlinedTailHelper{}, true, fmt.Errorf("outlined helper %q: %w", name, err)
	}
	return outlinedTailHelper{name: name, address: symbol.addr, raws: raws}, true, nil
}

// configureOutlinedTailInlines is retained for focused outliner tests. Product
// translation uses configureExternalTailTransfers with the complete immutable
// set of selected entry addresses.
func configureOutlinedTailInlines(input []byte, meta *elfMetadata, symbols *symbolIndex, selection Selection, instructions []vm.Instruction, translator *arm64.Translator) error {
	return configureExternalTailTransfers(input, meta, symbols, selection, instructions, translator, nil)
}

func configureExternalTailTransfers(input []byte, meta *elfMetadata, symbols *symbolIndex, selection Selection, instructions []vm.Instruction, translator *arm64.Translator, packedTargets map[uint64]struct{}) error {
	for _, inst := range instructions {
		if arm64.Op(inst.Op) != arm64.B {
			continue
		}
		branchSite, ok := checkedAdd(selection.Address, uint64(inst.Offset))
		if !ok {
			return fmt.Errorf("branch source address overflows")
		}
		target, ok := branchAddress(branchSite, inst.Imm)
		if !ok {
			return fmt.Errorf("branch at 0x%X has an overflowing target", branchSite)
		}
		if target >= selection.Address && target < selection.End {
			continue
		}

		if _, selected := packedTargets[target]; selected {
			if err := translator.SetPackedTailTransfer(inst.Offset); err != nil {
				return fmt.Errorf("packed tail target 0x%X: %w", target, err)
			}
			continue
		}

		if symbols == nil {
			return fmt.Errorf("external unconditional branch at 0x%X to 0x%X is neither a selected packed tail target nor a validated compiler outlined helper", branchSite, target)
		}
		names, err := symbols.directTransferNames(branchSite, target)
		if err != nil {
			return err
		}
		helper, matched, err := resolveOutlinedTailHelper(input, meta, symbols, selection, branchSite, target, names)
		if err != nil {
			return err
		}
		if !matched {
			return fmt.Errorf("external unconditional branch at 0x%X to 0x%X is neither a selected packed tail target nor a validated compiler outlined helper", branchSite, target)
		}
		if err := translator.SetOutlinedTailInline(inst.Offset, helper.raws); err != nil {
			return fmt.Errorf("outlined helper %q: %w", helper.name, err)
		}

	}
	return nil
}
