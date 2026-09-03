package elf

import (
	"encoding/binary"
	"fmt"

	"github.com/geg971509-wq/VMPackerGO/internal/arch/arm64"
	"github.com/geg971509-wq/VMPackerGO/internal/vm"
)

const maxInferredInstructions = 4096

var unsupportedRecoveryAPIs = map[string]bool{
	"setjmp": true, "_setjmp": true, "sigsetjmp": true,
	"longjmp": true, "_longjmp": true, "siglongjmp": true,
	"signal": true, "sigaction": true, "sigaltstack": true,
}

func inferFunctionRange(input []byte, meta *elfMetadata, symbols *symbolIndex, entry, hardEnd uint64) (uint64, error) {
	mapping, ok := meta.executableMapping(entry, entry+4)
	if !ok {
		return 0, explicitRangeError(entry, "entry is not in an executable file-backed PT_LOAD")
	}
	mappingEnd := mapping.vaddr + mapping.filesz
	if hardEnd == 0 || hardEnd > mappingEnd {
		hardEnd = mappingEnd
	}
	if entry%4 != 0 || hardEnd <= entry {
		return 0, explicitRangeError(entry, "entry or inferred boundary is invalid")
	}
	limit := int((hardEnd - entry) / 4)
	if limit > maxInferredInstructions {
		limit = maxInferredInstructions
	}
	if limit == 0 {
		return 0, explicitRangeError(entry, "no instructions are available")
	}

	decoder := arm64.NewDecoder()
	queue := []uint64{entry}
	queued := map[uint64]bool{entry: true}
	visited := make(map[uint64]vm.Instruction)
	maxAddress := entry

	queueTarget := func(from, target uint64, conditional bool) error {
		if target%4 != 0 {
			return explicitRangeError(entry, fmt.Sprintf("branch at 0x%X has an unaligned target", from))
		}
		if !conditional && target != entry && symbols.starts[target] {
			return nil
		}
		if target < entry {
			return explicitRangeError(entry, fmt.Sprintf("branch at 0x%X targets before the entry", from))
		}
		if target >= hardEnd || target >= mappingEnd {
			if !conditional {
				targetEnd, ok := checkedAdd(target, 4)
				if ok {
					if _, mapped := meta.executableMapping(target, targetEnd); mapped {
						return nil
					}
				}
			}
			return explicitRangeError(entry, fmt.Sprintf("branch at 0x%X has an unresolved target 0x%X", from, target))
		}
		if symbols.starts[target] && target != entry {
			return explicitRangeError(entry, fmt.Sprintf("conditional branch at 0x%X enters known function 0x%X", from, target))
		}
		if !queued[target] && !hasInstruction(visited, target) {
			queued[target] = true
			queue = append(queue, target)
		}
		return nil
	}

	for len(queue) != 0 {
		address := queue[0]
		queue = queue[1:]
		delete(queued, address)
		if hasInstruction(visited, address) {
			continue
		}
		if len(visited) >= limit {
			return 0, explicitRangeError(entry, fmt.Sprintf("CFG exceeds the %d-instruction inference limit", limit))
		}
		off, ok := mappingFileOffset(mapping, address)
		if !ok || off > uint64(len(input))-4 {
			return 0, explicitRangeError(entry, fmt.Sprintf("instruction 0x%X is not file-backed", address))
		}
		raw := binary.LittleEndian.Uint32(input[off : off+4])
		instruction := decoder.Decode(raw, int(address-entry))
		if instruction.Op == int(arm64.UNKNOWN) || instruction.Op == int(arm64.UNSUPPORTED) {
			return 0, explicitRangeError(entry, fmt.Sprintf("unsupported instruction 0x%08X at 0x%X", raw, address))
		}
		visited[address] = instruction
		if address > maxAddress {
			maxAddress = address
		}

		next := address + 4
		target := func() (uint64, bool) {
			if instruction.Imm >= 0 {
				value, ok := checkedAdd(address, uint64(instruction.Imm))
				return value, ok
			}
			delta := uint64(-instruction.Imm)
			if delta > address {
				return 0, false
			}
			return address - delta, true
		}
		queueFallthrough := func() error {
			if next >= hardEnd || next >= mappingEnd {
				return explicitRangeError(entry, fmt.Sprintf("fallthrough from 0x%X has no resolved successor", address))
			}
			return queueTarget(address, next, true)
		}

		switch arm64.Op(instruction.Op) {
		case arm64.RET:
			continue
		case arm64.BR:
			return 0, explicitRangeError(entry, fmt.Sprintf("indirect BR at 0x%X is ambiguous", address))
		case arm64.B:
			branchTarget, valid := target()
			if !valid {
				return 0, explicitRangeError(entry, fmt.Sprintf("branch target at 0x%X overflows", address))
			}
			names, err := symbols.directTransferNames(address, branchTarget)
			if err != nil {
				return 0, explicitRangeError(entry, err.Error())
			}
			if branchTarget < entry || branchTarget >= hardEnd || len(symbols.relocatedAt[address]) != 0 || branchTarget != entry && len(names) != 0 {
				return 0, explicitRangeError(entry, fmt.Sprintf("unsupported external unconditional branch at 0x%X to 0x%X", address, branchTarget))
			}
			if err := queueTarget(address, branchTarget, false); err != nil {
				return 0, err
			}
		case arm64.B_COND, arm64.CBZ, arm64.CBNZ, arm64.TBZ, arm64.TBNZ:
			branchTarget, valid := target()
			if !valid {
				return 0, explicitRangeError(entry, fmt.Sprintf("branch target at 0x%X overflows", address))
			}
			if err := queueTarget(address, branchTarget, true); err != nil {
				return 0, err
			}
			if err := queueFallthrough(); err != nil {
				return 0, err
			}
		case arm64.BL:
			callTarget, valid := target()
			if !valid {
				return 0, explicitRangeError(entry, fmt.Sprintf("call target at 0x%X overflows", address))
			}
			names, err := symbols.directTransferNames(address, callTarget)
			if err != nil {
				return 0, explicitRangeError(entry, err.Error())
			}
			for _, name := range names {
				if unsupportedRecoveryAPIs[baseSymbolName(name)] {
					return 0, fmt.Errorf("selected function directly calls unsupported recovery API %q at 0x%X", name, address)
				}
			}
			if err := queueFallthrough(); err != nil {
				return 0, err
			}
		case arm64.BLR:
			if err := queueFallthrough(); err != nil {
				return 0, err
			}
		default:
			if err := queueFallthrough(); err != nil {
				return 0, err
			}
		}
	}

	end := maxAddress + 4
	if end-entry < currentMinEntryPatch {
		return 0, explicitRangeError(entry, fmt.Sprintf("inferred function is shorter than %d bytes", currentMinEntryPatch))
	}
	for address := entry; address < end; address += 4 {
		if !hasInstruction(visited, address) {
			return 0, explicitRangeError(entry, fmt.Sprintf("reachable instructions leave a gap at 0x%X", address))
		}
	}
	return end, nil
}

func hasInstruction(instructions map[uint64]vm.Instruction, address uint64) bool {
	_, ok := instructions[address]
	return ok
}

func explicitRangeError(entry uint64, reason string) error {
	return fmt.Errorf("cannot infer function at 0x%X: %s; explicit START-END required", entry, reason)
}
