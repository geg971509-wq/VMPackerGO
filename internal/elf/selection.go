package elf

import (
	"crypto/sha256"
	"debug/elf"
	"encoding/binary"
	"fmt"

	"github.com/geg971509-wq/VMPackerGO/internal/abi"
	"github.com/geg971509-wq/VMPackerGO/internal/arch/arm64"
)

const (
	currentMinEntryPatch = 12
	maxSelections        = 4096
)

var phase3AnalysisLimitations = []string{
	"CFG inference is conservative and does not recover arbitrary hand-written or obfuscated functions",
	"indirect and dynamically resolved setjmp/longjmp or signal-recovery usage cannot be proven absent",
	"generic external native tail branches remain fail-closed; exact compiler-generated outlined-helper tails may be inlined only after symbol and body validation",
	"the entry patch requires at least 12 contiguous bytes, or 16 bytes when preserving an entry BTI",
}

type SelectionRequest struct {
	Source   string
	Selector string
	Name     string
	AddrSpec *AddrSpec
	ABI      abi.Signature
}

type Selection struct {
	Source       string
	Selector     string
	Name         string
	Address      uint64
	End          uint64
	Offset       uint64
	Section      string
	SymbolSource string
	ABI          abi.Signature
}

func (selection Selection) Size() uint64 { return selection.End - selection.Address }

type Analysis struct {
	TargetKind  TargetKind
	Warnings    []string
	Limitations []string
	Selections  []Selection

	hasNote     bool
	inputDigest [sha256.Size]byte
}

func Analyze(req Request) (Analysis, error) {
	mode := AndroidMode(req.Mode)
	if mode == "" {
		mode = AndroidModeAuto
	}
	requests := req.Selections
	if len(requests) == 0 {
		return Analysis{}, fmt.Errorf("at least one function selection is required")
	}
	if len(requests) > maxSelections {
		return Analysis{}, fmt.Errorf("request has %d functions; maximum is %d", len(requests), maxSelections)
	}

	meta, err := parseELFMetadata(req.Input, mode)
	if err != nil {
		return Analysis{}, err
	}
	defer meta.file.Close()
	symbols, err := readFunctionSymbols(meta)
	if err != nil {
		return Analysis{}, err
	}

	analysis := Analysis{
		TargetKind: meta.kind, Warnings: append([]string(nil), meta.warnings...),
		Limitations: append([]string(nil), phase3AnalysisLimitations...), hasNote: meta.hasNote,
		inputDigest: sha256.Sum256(req.Input),
	}
	for _, request := range requests {
		selection, err := resolveSelection(req.Input, meta, symbols, request)
		if err != nil {
			return analysis, err
		}
		duplicate := false
		for _, existing := range analysis.Selections {
			if !rangesOverlap(existing.Address, existing.End, selection.Address, selection.End) {
				continue
			}
			if existing.Address == selection.Address && existing.End == selection.End &&
				existing.Source == selection.Source && existing.Selector == selection.Selector && existing.ABI.String() == selection.ABI.String() {
				duplicate = true
				break
			}
			return analysis, fmt.Errorf("selected functions %q and %q overlap at 0x%X-0x%X", existing.Name, selection.Name,
				maxUint64(existing.Address, selection.Address), minUint64(existing.End, selection.End))
		}
		if !duplicate {
			analysis.Selections = append(analysis.Selections, selection)
		}
	}
	return analysis, nil
}

func (analysis Analysis) ValidateInput(input []byte) error {
	if analysis.inputDigest != sha256.Sum256(input) {
		return fmt.Errorf("analysis input provenance mismatch")
	}
	return nil
}

func resolveSelection(input []byte, meta *elfMetadata, symbols *symbolIndex, request SelectionRequest) (Selection, error) {
	selection := Selection{Source: request.Source, Selector: request.Selector, Name: request.Name, ABI: request.ABI}
	if selection.Source == "" {
		selection.Source = "direct"
	}
	if request.AddrSpec == nil {
		symbol, err := symbols.resolve(request.Name)
		if err != nil {
			return Selection{}, err
		}
		selection.Name = symbol.name
		selection.Address = symbol.addr
		selection.End = symbol.addr + symbol.size
		selection.Section = symbol.sectionName
		selection.SymbolSource = symbol.source
		if symbol.size == 0 {
			mapping, _ := meta.executableMapping(symbol.addr, symbol.addr+4)
			hardEnd := symbols.nextStart(symbol.addr, mapping.vaddr+mapping.filesz)
			selection.End, err = inferFunctionRange(input, meta, symbols, symbol.addr, hardEnd)
			if err != nil {
				return Selection{}, fmt.Errorf("function %q: %w", symbol.name, err)
			}
		}
	} else {
		spec := *request.AddrSpec
		selection.Name = spec.Name
		if request.Name != "" {
			selection.Name = request.Name
		}
		selection.Address = spec.Addr
		selection.End = spec.End
		selection.SymbolSource = ""
		if spec.End == 0 {
			mapping, ok := meta.executableMapping(spec.Addr, spec.Addr+4)
			if !ok {
				return Selection{}, fmt.Errorf("address 0x%X is not in an executable file-backed PT_LOAD", spec.Addr)
			}
			hardEnd := symbols.nextStart(spec.Addr, mapping.vaddr+mapping.filesz)
			var err error
			selection.End, err = inferFunctionRange(input, meta, symbols, spec.Addr, hardEnd)
			if err != nil {
				return Selection{}, err
			}
		}
	}
	if err := validateSelectionRange(meta, &selection); err != nil {
		return Selection{}, err
	}
	if err := rejectUnsupportedDirectTransfers(input, meta, symbols, selection); err != nil {
		return Selection{}, err
	}
	return selection, nil
}

func validateSelectionRange(meta *elfMetadata, selection *Selection) error {
	if selection.Address%4 != 0 || selection.End%4 != 0 {
		return fmt.Errorf("function %q range 0x%X-0x%X must be 4-byte aligned", selection.Name, selection.Address, selection.End)
	}
	if selection.End <= selection.Address {
		return fmt.Errorf("function %q range end must be greater than start", selection.Name)
	}
	if selection.Size() < currentMinEntryPatch {
		return fmt.Errorf("function %q is %d bytes; current entry patch requires at least %d bytes", selection.Name, selection.Size(), currentMinEntryPatch)
	}
	if selection.Size()/4 > maxInferredInstructions {
		return fmt.Errorf("function %q has %d instructions; maximum is %d", selection.Name, selection.Size()/4, maxInferredInstructions)
	}
	mapping, ok := meta.executableMapping(selection.Address, selection.End)
	if !ok {
		return fmt.Errorf("function %q range 0x%X-0x%X must be fully file-backed by one executable PT_LOAD", selection.Name, selection.Address, selection.End)
	}
	off, ok := mappingFileOffset(mapping, selection.Address)
	if !ok {
		return fmt.Errorf("function %q has no safe file offset", selection.Name)
	}
	selection.Offset = off
	if selection.Section == "" {
		selection.Section = sectionNameForRange(meta, selection.Address, selection.End)
	}
	return nil
}

func sectionNameForRange(meta *elfMetadata, start, end uint64) string {
	for _, section := range meta.sections {
		sectionEnd, ok := checkedAdd(section.addr, section.size)
		if ok && section.flags&elf.SHF_EXECINSTR != 0 && start >= section.addr && end <= sectionEnd {
			if section.name != "" {
				return section.name
			}
			break
		}
	}
	return "__LOAD_X"
}

func rejectUnsupportedDirectTransfers(input []byte, meta *elfMetadata, symbols *symbolIndex, selection Selection) error {
	decoder := arm64.NewDecoder()
	for address, off := selection.Address, selection.Offset; address < selection.End; address, off = address+4, off+4 {
		raw := binary.LittleEndian.Uint32(input[off : off+4])
		instruction := decoder.Decode(raw, int(address-selection.Address))
		op := arm64.Op(instruction.Op)
		if op != arm64.BL && op != arm64.B {
			continue
		}
		target, ok := branchAddress(address, instruction.Imm)
		if !ok {
			return fmt.Errorf("function %q direct transfer at 0x%X has an overflowing target", selection.Name, address)
		}
		names, err := symbols.directTransferNames(address, target)
		if err != nil {
			return fmt.Errorf("function %q: %w", selection.Name, err)
		}
		if op == arm64.B {
			disallowed := target < selection.Address || target >= selection.End || len(symbols.relocatedAt[address]) != 0 || target != selection.Address && len(names) != 0
			if !disallowed {
				continue
			}
			if target < selection.Address || target >= selection.End {
				if _, matched, helperErr := resolveOutlinedTailHelper(input, meta, symbols, selection, address, target, names); helperErr != nil {
					return fmt.Errorf("function %q outlined tail at 0x%X: %w", selection.Name, address, helperErr)
				} else if matched {
					continue
				}
			}
			return fmt.Errorf("function %q has unsupported external unconditional branch at 0x%X to 0x%X; explicit range cannot make this tail call translatable", selection.Name, address, target)
		}
		if op != arm64.BL {
			continue
		}
		for _, name := range names {
			if unsupportedRecoveryAPIs[baseSymbolName(name)] {
				return fmt.Errorf("selected function %q directly calls unsupported recovery API %q at 0x%X", selection.Name, name, address)
			}
		}
	}
	return nil
}

func branchAddress(address uint64, delta int64) (uint64, bool) {
	if delta >= 0 {
		return checkedAdd(address, uint64(delta))
	}
	amount := uint64(-delta)
	if amount > address {
		return 0, false
	}
	return address - amount, true
}

func baseSymbolName(name string) string {
	for i, char := range name {
		if char == '@' {
			return name[:i]
		}
	}
	return name
}

func rangesOverlap(startA, endA, startB, endB uint64) bool { return startA < endB && startB < endA }
func minUint64(a, b uint64) uint64 {
	if a < b {
		return a
	}
	return b
}
func maxUint64(a, b uint64) uint64 {
	if a > b {
		return a
	}
	return b
}
