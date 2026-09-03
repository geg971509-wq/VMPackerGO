package elf

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/geg971509-wq/VMPackerGO/internal/arch/arm64"
	vmruntime "github.com/geg971509-wq/VMPackerGO/internal/runtime"
	"github.com/geg971509-wq/VMPackerGO/internal/vm"
)

type PreparedFunction struct {
	Selection   Selection
	Translation *arm64.TranslateResult
	Debug       []arm64.DebugEntry
}

type TranslationPreparation struct {
	Functions          []PreparedFunction
	SVCImmediates      []uint16
	ExclusiveRegions   []vm.ExclusiveRegion
	FPSIMDInstructions []uint32
	ExceptionBridges   []PreparedExceptionBridge

	opcodeMapDigest [sha256.Size]byte
}

func PrepareTranslations(req Request, analysis Analysis) (*TranslationPreparation, error) {
	if req.Context != nil {
		if err := req.Context.Err(); err != nil {
			return nil, err
		}
	}
	if err := req.Opcodes.Validate(); err != nil {
		return nil, fmt.Errorf("validate opcode map for translation: %w", err)
	}
	opcodeMapDigest, err := req.Opcodes.Digest()
	if err != nil {
		return nil, fmt.Errorf("digest opcode map for translation: %w", err)
	}
	mode := AndroidMode(strings.ToLower(req.Mode))
	if mode == "" {
		mode = AndroidModeAuto
	}
	meta, err := parseELFMetadata(req.Input, mode)
	if err != nil {
		return nil, fmt.Errorf("parse ELF for translation preparation: %w", err)
	}
	defer meta.file.Close()
	symbols, err := readFunctionSymbols(meta)
	if err != nil {
		return nil, fmt.Errorf("read symbols for translation preparation: %w", err)
	}

	packedTailTargets := make(map[uint64]struct{}, len(analysis.Selections))
	for _, selection := range analysis.Selections {
		if _, duplicate := packedTailTargets[selection.Address]; duplicate {
			return nil, fmt.Errorf("selected functions share entry address 0x%x", selection.Address)
		}
		packedTailTargets[selection.Address] = struct{}{}
	}

	preparation := &TranslationPreparation{Functions: make([]PreparedFunction, 0, len(analysis.Selections)), opcodeMapDigest: opcodeMapDigest}
	svc := make(map[uint16]struct{})
	exclusive := make(map[uint32]vm.ExclusiveRegion)
	fpSIMD := make(map[uint32]struct{})
	decoder := arm64.NewDecoder()

	for _, selection := range analysis.Selections {
		if req.Context != nil {
			if err := req.Context.Err(); err != nil {
				return nil, err
			}
		}
		code, err := selectedCode(req.Input, selection)
		if err != nil {
			return nil, err
		}
		instructions := make([]vm.Instruction, 0, len(code)/4)
		for offset := 0; offset < len(code); offset += 4 {
			instructions = append(instructions, decoder.Decode(binary.LittleEndian.Uint32(code[offset:offset+4]), offset))
		}
		translator, err := arm64.NewTranslator(selection.Address, len(code), req.Opcodes)
		if err != nil {
			return nil, fmt.Errorf("function %q: create translator: %w", selection.Name, err)
		}
		translator.SetDebug(req.Debug)
		if err := configureExternalTailTransfers(req.Input, meta, symbols, selection, instructions, translator, packedTailTargets); err != nil {
			return nil, fmt.Errorf("function %q external-tail preparation: %w", selection.Name, err)
		}
		translation, err := translator.Translate(instructions)
		if err != nil {
			return nil, fmt.Errorf("function %q: translate: %w", selection.Name, err)
		}
		if len(translation.Unsupported) != 0 {
			return nil, fmt.Errorf("function %q has unsupported instruction(s): %s", selection.Name, strings.Join(translation.Unsupported, "; "))
		}

		for _, immediate := range translation.SVCImmediates {
			svc[immediate] = struct{}{}
		}
		for _, region := range translation.ExclusiveRegions {
			if previous, ok := exclusive[region.ID]; ok && !slices.Equal(previous.Instructions, region.Instructions) {
				return nil, fmt.Errorf("function %q produced exclusive region identifier collision 0x%08x", selection.Name, region.ID)
			}
			exclusive[region.ID] = region
		}
		for _, raw := range translation.FPSIMDInstructions {
			fpSIMD[raw] = struct{}{}
		}
		preparation.Functions = append(preparation.Functions, PreparedFunction{
			Selection: selection, Translation: translation, Debug: append([]arm64.DebugEntry(nil), translator.DebugLog()...),
		})
	}

	for immediate := range svc {
		preparation.SVCImmediates = append(preparation.SVCImmediates, immediate)
	}
	sort.Slice(preparation.SVCImmediates, func(i, j int) bool { return preparation.SVCImmediates[i] < preparation.SVCImmediates[j] })
	for _, region := range exclusive {
		preparation.ExclusiveRegions = append(preparation.ExclusiveRegions, region)
	}
	sort.Slice(preparation.ExclusiveRegions, func(i, j int) bool {
		return preparation.ExclusiveRegions[i].ID < preparation.ExclusiveRegions[j].ID
	})
	for raw := range fpSIMD {
		preparation.FPSIMDInstructions = append(preparation.FPSIMDInstructions, raw)
	}
	sort.Slice(preparation.FPSIMDInstructions, func(i, j int) bool {
		return preparation.FPSIMDInstructions[i] < preparation.FPSIMDInstructions[j]
	})
	exceptionBridges, err := prepareExceptionBridges(req, preparation.Functions)
	if err != nil {
		return nil, err
	}
	if err := resolvePreparedExceptionRoutes(exceptionBridges, preparation.Functions, req.Opcodes); err != nil {
		return nil, err
	}
	preparation.ExceptionBridges = exceptionBridges
	return preparation, nil
}

func (preparation *TranslationPreparation) ValidateOpcodeMap(opcodes vm.OpcodeMap) error {
	if preparation == nil {
		return fmt.Errorf("translation preparation is required")
	}
	digest, err := opcodes.Digest()
	if err != nil {
		return fmt.Errorf("validate opcode map for translation preparation: %w", err)
	}
	if preparation.opcodeMapDigest != digest {
		return fmt.Errorf("translation preparation opcode-map provenance mismatch")
	}
	return nil
}

func (preparation *TranslationPreparation) FunctionFacts() []FunctionFact {
	if preparation == nil {
		return nil
	}
	facts := make([]FunctionFact, 0, len(preparation.Functions))
	for _, function := range preparation.Functions {
		selection := function.Selection
		translation := function.Translation
		fact := FunctionFact{
			Source: selection.Source, Selector: selection.Selector, Name: selection.Name,
			Address: selection.Address, End: selection.End, Size: selection.Size(), Section: selection.Section,
			SymbolSource: selection.SymbolSource,
		}
		if translation != nil {
			fact.Bytecode = len(translation.Bytecode)
			fact.Translated = translation.TransInsts
			fact.Instructions = translation.TotalInsts
		}
		facts = append(facts, fact)
	}
	return facts
}

func (preparation *TranslationPreparation) ValidateAnalysis(analysis Analysis) error {
	if preparation == nil {
		return fmt.Errorf("translation preparation is required")
	}
	if len(preparation.Functions) != len(analysis.Selections) {
		return fmt.Errorf("translation preparation does not match analyzed selections")
	}
	for i, function := range preparation.Functions {
		if !sameSelection(function.Selection, analysis.Selections[i]) {
			return fmt.Errorf("translation preparation does not match analyzed selection %d", i)
		}
	}
	return nil
}

func (preparation *TranslationPreparation) ValidateRuntimeImage(image *vmruntime.Image) error {
	if preparation == nil {
		return fmt.Errorf("translation preparation is required")
	}
	if image == nil {
		return fmt.Errorf("runtime image is required")
	}
	if !slices.Equal(preparation.SVCImmediates, image.SVCImmediates) {
		return fmt.Errorf("runtime image SVC requirements mismatch")
	}
	if !equalExclusiveRequirements(preparation.ExclusiveRegions, image.ExclusiveRegions) {
		return fmt.Errorf("runtime image exclusive requirements mismatch")
	}
	if !slices.Equal(preparation.FPSIMDInstructions, image.FPSIMDInstructions) {
		return fmt.Errorf("runtime image FP/SIMD requirements mismatch")
	}
	if err := preparation.validateRuntimeExceptionInvokes(image); err != nil {
		return err
	}
	return nil
}

func selectedCode(input []byte, selection Selection) ([]byte, error) {
	size := selection.Size()
	if size == 0 || size%4 != 0 {
		return nil, fmt.Errorf("function %q has invalid translated size %d", selection.Name, size)
	}
	end, ok := checkedAdd(selection.Offset, size)
	if !ok || end > uint64(len(input)) {
		return nil, fmt.Errorf("function %q translated range exceeds the input", selection.Name)
	}
	return input[int(selection.Offset):int(end)], nil
}

func equalExclusiveRequirements(a, b []vm.ExclusiveRegion) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].ID != b[i].ID || !slices.Equal(a[i].Instructions, b[i].Instructions) {
			return false
		}
	}
	return true
}

func sameSelection(a, b Selection) bool {
	return a.Source == b.Source && a.Selector == b.Selector && a.Name == b.Name &&
		a.Address == b.Address && a.End == b.End && a.Offset == b.Offset && a.Section == b.Section &&
		a.SymbolSource == b.SymbolSource && a.ABI.String() == b.ABI.String()
}
