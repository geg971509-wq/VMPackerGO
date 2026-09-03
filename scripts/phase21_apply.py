from pathlib import Path
import re


def replace_once(path: str, old: str, new: str) -> None:
    p = Path(path)
    text = p.read_text()
    if text.count(old) != 1:
        raise SystemExit(f"{path}: expected one match, found {text.count(old)} for {old[:100]!r}")
    p.write_text(text.replace(old, new, 1))


def regex_once(path: str, pattern: str, repl: str) -> None:
    p = Path(path)
    text = p.read_text()
    new, count = re.subn(pattern, repl, text, count=1, flags=re.S)
    if count != 1:
        raise SystemExit(f"{path}: expected one regex match, found {count}: {pattern}")
    p.write_text(new)


# Translator: keep outlined helper semantics at the original tail-B source PC.
replace_once(
    "internal/arch/arm64/translator.go",
    "\tnativeCallSites    []NativeCallSite\n\tentryBTI           Op",
    "\tnativeCallSites    []NativeCallSite\n\toutlinedTailInlines map[int][]uint32\n\tentryBTI           Op",
)
replace_once(
    "internal/arch/arm64/translator.go",
    "\t\tnativeCallSites:    nil,\n\t\tentryBTI:           0,",
    "\t\tnativeCallSites:    nil,\n\t\toutlinedTailInlines: make(map[int][]uint32),\n\t\tentryBTI:           0,",
) if "\t\tnativeCallSites:    nil,\n\t\tentryBTI:           0," in Path("internal/arch/arm64/translator.go").read_text() else None
# Current constructor does not spell out nil fields; patch the last initialized map instead.
if "outlinedTailInlines: make(map[int][]uint32)" not in Path("internal/arch/arm64/translator.go").read_text():
    replace_once(
        "internal/arch/arm64/translator.go",
        "\t\tfpSIMDInstructions: make(map[uint32]bool),\n\t}, nil",
        "\t\tfpSIMDInstructions: make(map[uint32]bool),\n\t\toutlinedTailInlines: make(map[int][]uint32),\n\t}, nil",
    )
replace_once(
    "internal/arch/arm64/translator.go",
    "\tcase B:\n\t\treturn 0, t.trBranch(inst)",
    "\tcase B:\n\t\treturn 0, t.trBranchOrOutlined(inst)",
)

# Branch implementation: evidence-bounded pack-time inline, no generic external-B widening.
branch = Path("internal/arch/arm64/tr_branch.go")
text = branch.read_text()
marker = "// ============================================================\n\nfunc (t *Translator) trBranch(inst vm.Instruction) error {"
insert = r'''// ============================================================

// MaxOutlinedTailHelperInstructions bounds the compiler-generated helper body
// accepted for pack-time tail inlining. Exact NDK r29 currently emits at most
// five instructions including the terminal RET; the wider bound leaves modest
// headroom without turning this into a generic external-code importer.
const MaxOutlinedTailHelperInstructions = 16

// ValidateOutlinedTailHelper deliberately accepts only the exact semantic
// class proven by the exact-r29 machine-outliner audit: one or more unshifted
// 32-bit EOR(register) instructions followed by RET X30. Any future helper
// shape must first re-enter the compiler-derived audit rather than silently
// widening product behavior.
func ValidateOutlinedTailHelper(raws []uint32) error {
	if len(raws) < 2 {
		return fmt.Errorf("outlined helper must contain a body and terminal RET")
	}
	if len(raws) > MaxOutlinedTailHelperInstructions {
		return fmt.Errorf("outlined helper has %d instructions; maximum is %d", len(raws), MaxOutlinedTailHelperInstructions)
	}
	if raws[len(raws)-1] != 0xd65f03c0 {
		return fmt.Errorf("outlined helper must terminate with RET X30")
	}
	decoder := NewDecoder()
	for i, raw := range raws[:len(raws)-1] {
		inst := decoder.Decode(raw, i*4)
		if Op(inst.Op) != EOR_REG || inst.SF || inst.Shift != 0 || inst.ShiftType != 0 {
			return fmt.Errorf("outlined helper instruction %d raw=0x%08x is not unshifted EOR Wd, Wn, Wm", i, raw)
		}
		if err := validateInstructionPolicy(inst); err != nil {
			return fmt.Errorf("outlined helper instruction %d: %w", i, err)
		}
	}
	return nil
}

// SetOutlinedTailInline binds a proven helper body to the caller's original
// final B instruction. No synthetic ARM64 offsets are created; SourceMap and
// exception identities remain anchored to the selected function.
func (t *Translator) SetOutlinedTailInline(branchOffset int, raws []uint32) error {
	if branchOffset < 0 || branchOffset%4 != 0 || branchOffset+4 != t.funcSize {
		return fmt.Errorf("outlined tail branch offset 0x%x is not the final instruction of a 0x%x-byte function", branchOffset, t.funcSize)
	}
	if _, exists := t.outlinedTailInlines[branchOffset]; exists {
		return fmt.Errorf("outlined tail branch offset 0x%x is configured more than once", branchOffset)
	}
	if err := ValidateOutlinedTailHelper(raws); err != nil {
		return err
	}
	t.outlinedTailInlines[branchOffset] = append([]uint32(nil), raws...)
	return nil
}

func (t *Translator) trBranchOrOutlined(inst vm.Instruction) error {
	raws, ok := t.outlinedTailInlines[inst.Offset]
	if !ok {
		return t.trBranch(inst)
	}
	target := int64(inst.Offset) + inst.Imm
	if target >= 0 && target < int64(t.funcSize) {
		return fmt.Errorf("outlined tail inline at 0x%x is configured for an in-function branch target 0x%x", inst.Offset, target)
	}
	return t.trOutlinedTailInline(raws)
}

func (t *Translator) trOutlinedTailInline(raws []uint32) error {
	if err := ValidateOutlinedTailHelper(raws); err != nil {
		return err
	}
	for i, raw := range raws[:len(raws)-1] {
		inst := t.decoder.Decode(raw, i*4)
		if err := t.trStackAluReg(inst, vm.OpSXor); err != nil {
			return fmt.Errorf("inline outlined helper instruction %d: %w", i, err)
		}
	}
	// Original A64 semantics are tail B to helper followed by helper RET using
	// the caller's existing LR. Inlined helper body + VM RET is equivalent.
	t.emitOp(vm.OpRet, 0)
	return nil
}

func (t *Translator) trBranch(inst vm.Instruction) error {'''
if marker not in text:
    raise SystemExit("tr_branch.go insertion marker missing")
branch.write_text(text.replace(marker, insert, 1))

# ELF-side resolver: symbol identity + file-backed body + raw semantic proof.
Path("internal/elf/outliner.go").write_text(r'''package elf

import (
	"encoding/binary"
	"fmt"
	"strings"

	"github.com/vmpacker/internal/arch/arm64"
	"github.com/vmpacker/internal/vm"
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

func configureOutlinedTailInlines(input []byte, meta *elfMetadata, symbols *symbolIndex, selection Selection, instructions []vm.Instruction, translator *arm64.Translator) error {
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
		names, err := symbols.directTransferNames(branchSite, target)
		if err != nil {
			return err
		}
		helper, matched, err := resolveOutlinedTailHelper(input, meta, symbols, selection, branchSite, target, names)
		if err != nil {
			return err
		}
		if !matched {
			return fmt.Errorf("external unconditional branch at 0x%X to 0x%X is not a validated compiler outlined helper", branchSite, target)
		}
		if err := translator.SetOutlinedTailInline(inst.Offset, helper.raws); err != nil {
			return fmt.Errorf("outlined helper %q: %w", helper.name, err)
		}
	}
	return nil
}
''')

# Selection analysis: allow only structurally proven outlined tail helpers.
replace_once(
    "internal/elf/selection.go",
    '"external native tail branches remain fail-closed until the non-returning native-tail ABI bridge is proven; packed tail support is handled separately",',
    '"generic external native tail branches remain fail-closed; exact compiler-generated outlined-helper tails may be inlined only after symbol and body validation",',
)
replace_once(
    "internal/elf/selection.go",
    "\tif err := rejectUnsupportedDirectTransfers(input, symbols, selection); err != nil {",
    "\tif err := rejectUnsupportedDirectTransfers(input, meta, symbols, selection); err != nil {",
)
replace_once(
    "internal/elf/selection.go",
    "func rejectUnsupportedDirectTransfers(input []byte, symbols *symbolIndex, selection Selection) error {",
    "func rejectUnsupportedDirectTransfers(input []byte, meta *elfMetadata, symbols *symbolIndex, selection Selection) error {",
)
old_b = '''\t\tif op == arm64.B && (target < selection.Address || target >= selection.End || len(symbols.relocatedAt[address]) != 0 || target != selection.Address && len(names) != 0) {\n\t\t\treturn fmt.Errorf("function %q has unsupported external unconditional branch at 0x%X to 0x%X; explicit range cannot make this tail call translatable", selection.Name, address, target)\n\t\t}\n\t\tif op != arm64.BL {\n\t\t\tcontinue\n\t\t}'''
new_b = '''\t\tif op == arm64.B {\n\t\t\tdisallowed := target < selection.Address || target >= selection.End || len(symbols.relocatedAt[address]) != 0 || target != selection.Address && len(names) != 0\n\t\t\tif !disallowed {\n\t\t\t\tcontinue\n\t\t\t}\n\t\t\tif target < selection.Address || target >= selection.End {\n\t\t\t\tif _, matched, helperErr := resolveOutlinedTailHelper(input, meta, symbols, selection, address, target, names); helperErr != nil {\n\t\t\t\t\treturn fmt.Errorf("function %q outlined tail at 0x%X: %w", selection.Name, address, helperErr)\n\t\t\t\t} else if matched {\n\t\t\t\t\tcontinue\n\t\t\t\t}\n\t\t\t}\n\t\t\treturn fmt.Errorf("function %q has unsupported external unconditional branch at 0x%X to 0x%X; explicit range cannot make this tail call translatable", selection.Name, address, target)\n\t\t}\n\t\tif op != arm64.BL {\n\t\t\tcontinue\n\t\t}'''
replace_once("internal/elf/selection.go", old_b, new_b)

# Preparation: independently re-resolve helper evidence before translation.
replace_once(
    "internal/elf/preparation.go",
    "\topcodeMapDigest, err := req.Opcodes.Digest()\n\tif err != nil {\n\t\treturn nil, fmt.Errorf(\"digest opcode map for translation: %w\", err)\n\t}\n\n\tpreparation :=",
    "\topcodeMapDigest, err := req.Opcodes.Digest()\n\tif err != nil {\n\t\treturn nil, fmt.Errorf(\"digest opcode map for translation: %w\", err)\n\t}\n\tmode := AndroidMode(strings.ToLower(req.Mode))\n\tif mode == \"\" {\n\t\tmode = AndroidModeAuto\n\t}\n\tmeta, err := parseELFMetadata(req.Input, mode)\n\tif err != nil {\n\t\treturn nil, fmt.Errorf(\"parse ELF for translation preparation: %w\", err)\n\t}\n\tdefer meta.file.Close()\n\tsymbols, err := readFunctionSymbols(meta)\n\tif err != nil {\n\t\treturn nil, fmt.Errorf(\"read symbols for translation preparation: %w\", err)\n\t}\n\n\tpreparation :=",
)
replace_once(
    "internal/elf/preparation.go",
    "\t\ttranslator.SetDebug(req.Debug)\n\t\ttranslation, err := translator.Translate(instructions)",
    "\t\ttranslator.SetDebug(req.Debug)\n\t\tif err := configureOutlinedTailInlines(req.Input, meta, symbols, selection, instructions, translator); err != nil {\n\t\t\treturn nil, fmt.Errorf(\"function %q outlined-tail preparation: %w\", selection.Name, err)\n\t\t}\n\t\ttranslation, err := translator.Translate(instructions)",
)

# Exact-r29 compiler gate: configure real helper groups and remove the last exemption.
cc = Path("internal/arch/arm64/compiler_corpus_test.go")
text = cc.read_text()
text = re.sub(r'type compilerCoverageReport struct \{\n\tUnexpected\s+\[\]string\n\tIntentional\s+\[\]string\n\tIntentionalKinds map\[string\]int\n\}', 'type compilerCoverageReport struct {\n\tUnexpected []string\n}', text, count=1)
text = re.sub(r'\nvar exactR29OutlinedTailRaws = map\[uint32\]bool\{.*?\n\}\n', '\n', text, count=1, flags=re.S)
text = re.sub(r'\nfunc exactR29IntentionalBoundary\(record compilerCorpusRecord, issue string\) \(string, bool\) \{.*?\n\}\n', '\n', text, count=1, flags=re.S)
text = re.sub(r'func addCompilerIssue\(report \*compilerCoverageReport, record compilerCorpusRecord, issue string\) \{.*?\n\}', 'func addCompilerIssue(report *compilerCoverageReport, record compilerCorpusRecord, issue string) {\n\treport.Unexpected = append(report.Unexpected, compilerRecordLabel(record)+": "+issue)\n}', text, count=1, flags=re.S)
text = text.replace('report := compilerCoverageReport{Unexpected: append([]string(nil), groupGaps...), IntentionalKinds: map[string]int{}}', 'report := compilerCoverageReport{Unexpected: append([]string(nil), groupGaps...)}')
text = text.replace('\treport.Intentional = sortedUniqueStrings(report.Intentional)\n', '')
# Configure helper before Translate.
needle = '''\t\ttranslator, err := NewTranslator(start, funcSize, vm.IdentityOpcodeMap())\n\t\tif err != nil {\n\t\t\treport.Unexpected = append(report.Unexpected, fmt.Sprintf("-%s/%s %s: construct translator: %v",\n\t\t\t\tkey.Optimization, key.Profile, key.Function, err))\n\t\t\tcontinue\n\t\t}\n\t\tresult, err := translator.Translate(instructions)'''
replacement = '''\t\ttranslator, err := NewTranslator(start, funcSize, vm.IdentityOpcodeMap())\n\t\tif err != nil {\n\t\t\treport.Unexpected = append(report.Unexpected, fmt.Sprintf("-%s/%s %s: construct translator: %v",\n\t\t\t\tkey.Optimization, key.Profile, key.Function, err))\n\t\t\tcontinue\n\t\t}\n\t\tif err := configureCompilerOutlinedTailInlines(translator, key, group, groups); err != nil {\n\t\t\treport.Unexpected = append(report.Unexpected, fmt.Sprintf("-%s/%s %s: outlined-tail configuration: %v",\n\t\t\t\tkey.Optimization, key.Profile, key.Function, err))\n\t\t\tcontinue\n\t\t}\n\t\tresult, err := translator.Translate(instructions)'''
if needle not in text:
    raise SystemExit("compiler translator marker missing")
text = text.replace(needle, replacement, 1)
# Remove stale intentional-boundary unit test.
text, n = re.subn(r'\nfunc TestCompilerIntentionalBoundaryRequiresExactEvidence\(t \*testing\.T\) \{.*?\n\}\n', '\n', text, count=1, flags=re.S)
if n != 1:
    raise SystemExit("stale intentional-boundary test missing")
# Remove stale required expectation and simplify final error.
text = text.replace('''\treport := classifyCompilerCorpus(records)\n\tif report.IntentionalKinds["machine-outliner"] == 0 {\n\t\tt.Errorf("exact-r29 compiler corpus no longer exercises intentional boundary %q; audit and remove/update the expectation", "machine-outliner")\n\t}\n\tif len(report.Unexpected) != 0 {\n\t\tt.Fatalf("exact-r29 compiler coverage has %d unexpected gap(s) (%d intentional fail-closed record(s)):\\n%s",\n\t\t\tlen(report.Unexpected), len(report.Intentional), strings.Join(report.Unexpected, "\\n"))\n\t}\n''', '''\treport := classifyCompilerCorpus(records)\n\tif len(report.Unexpected) != 0 {\n\t\tt.Fatalf("exact-r29 compiler coverage has %d unexpected gap(s):\\n%s",\n\t\t\tlen(report.Unexpected), strings.Join(report.Unexpected, "\\n"))\n\t}\n''')
# Add corpus resolver helpers before classifyCompilerCorpus.
marker = "func classifyCompilerCorpus(records []compilerCorpusRecord) compilerCoverageReport {"
helper_code = r'''func compilerAddressDelta(address uint64, delta int64) (uint64, bool) {
	if delta >= 0 {
		if uint64(delta) > ^uint64(0)-address {
			return 0, false
		}
		return address + uint64(delta), true
	}
	amount := uint64(-(delta + 1)) + 1
	if amount > address {
		return 0, false
	}
	return address - amount, true
}

func configureCompilerOutlinedTailInlines(translator *Translator, key compilerCorpusKey, group []compilerCorpusRecord, groups map[compilerCorpusKey][]compilerCorpusRecord) error {
	if len(group) == 0 {
		return nil
	}
	start := group[0].Address
	end := group[len(group)-1].Address + 4
	decoder := NewDecoder()
	for _, record := range group {
		inst := decoder.Decode(record.Raw, int(record.Address-start))
		if Op(inst.Op) != B {
			continue
		}
		target, ok := compilerAddressDelta(record.Address, inst.Imm)
		if !ok {
			return fmt.Errorf("B at 0x%x target overflows", record.Address)
		}
		if target >= start && target < end {
			continue
		}
		if record.Address+4 != end {
			return fmt.Errorf("external B at 0x%x is not the function tail", record.Address)
		}
		var helperKey compilerCorpusKey
		var helper []compilerCorpusRecord
		for candidateKey, candidate := range groups {
			if candidateKey.Optimization != key.Optimization || candidateKey.Profile != key.Profile ||
				!strings.HasPrefix(candidateKey.Function, "OUTLINED_FUNCTION_") || len(candidate) == 0 || candidate[0].Address != target {
				continue
			}
			if helper != nil {
				return fmt.Errorf("external B at 0x%x has multiple outlined helpers at 0x%x", record.Address, target)
			}
			helperKey, helper = candidateKey, candidate
		}
		if helper == nil {
			return fmt.Errorf("external B at 0x%x to 0x%x has no exact outlined helper", record.Address, target)
		}
		raws := make([]uint32, len(helper))
		for i := range helper {
			raws[i] = helper[i].Raw
		}
		if err := translator.SetOutlinedTailInline(int(record.Address-start), raws); err != nil {
			return fmt.Errorf("%s: %w", helperKey.Function, err)
		}
	}
	return nil
}

func classifyCompilerCorpus(records []compilerCorpusRecord) compilerCoverageReport {'''
if marker not in text:
    raise SystemExit("classify marker missing")
text = text.replace(marker, helper_code, 1)
# Add a compact synthetic closure test.
test_marker = "func TestCompilerCorpusVerifierReportsRejectedInstruction(t *testing.T) {"
new_test = r'''func TestCompilerCorpusVerifierClosesOutlinedTailHelper(t *testing.T) {
	input := compilerCorpusHeader + "\n" +
		"Oz\tbase\tcaller\t0\t14000002\tb\t0x8 <OUTLINED_FUNCTION_0>\n" +
		"Oz\tbase\tOUTLINED_FUNCTION_0\t8\t4a0d0100\teor\tw0, w8, w13\n" +
		"Oz\tbase\tOUTLINED_FUNCTION_0\tc\td65f03c0\tret\t\n"
	records, err := parseCompilerCorpus(bufio.NewScanner(strings.NewReader(input)))
	if err != nil {
		t.Fatal(err)
	}
	if gaps := verifyCompilerCorpus(records); len(gaps) != 0 {
		t.Fatalf("outlined-tail corpus gaps=%v", gaps)
	}
}

func TestCompilerCorpusVerifierReportsRejectedInstruction(t *testing.T) {'''
if test_marker not in text:
    raise SystemExit("compiler test marker missing")
text = text.replace(test_marker, new_test, 1)
cc.write_text(text)

# ARM64 focused tests.
Path("internal/arch/arm64/outlined_tail_test.go").write_text(r'''package arm64

import (
	"strings"
	"testing"

	"github.com/vmpacker/internal/vm"
)

var exactR29BaseOutlinedHelper = []uint32{
	0x4a080128,
	0x4a0a0108,
	0x4a0c0108,
	0x4a0b0100,
	0xd65f03c0,
}

var exactR29LSEOutlinedHelper = []uint32{
	0x4a0d0100,
	0xd65f03c0,
}

func TestValidateOutlinedTailHelperExactR29Shapes(t *testing.T) {
	for _, raws := range [][]uint32{exactR29BaseOutlinedHelper, exactR29LSEOutlinedHelper} {
		if err := ValidateOutlinedTailHelper(raws); err != nil {
			t.Fatalf("valid helper rejected: %v", err)
		}
	}
	for _, raws := range [][]uint32{
		{0xd65f03c0},
		{0xd503201f, 0xd65f03c0},
		{0xca0d0100, 0xd65f03c0},
		{0x4a0d0100, 0xd65f0000},
	} {
		if err := ValidateOutlinedTailHelper(raws); err == nil {
			t.Fatalf("invalid outlined helper accepted: %08x", raws)
		}
	}
}

func TestOutlinedTailInlineKeepsOriginalSourceMap(t *testing.T) {
	decoder := NewDecoder()
	branch := decoder.Decode(0x14000002, 0) // B +8, outside a 4-byte selected function.
	translator, err := NewTranslator(0x1000, 4, vm.IdentityOpcodeMap())
	if err != nil {
		t.Fatal(err)
	}
	if err := translator.SetOutlinedTailInline(0, exactR29LSEOutlinedHelper); err != nil {
		t.Fatal(err)
	}
	result, err := translator.Translate([]vm.Instruction{branch})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Unsupported) != 0 {
		t.Fatalf("unsupported=%v", result.Unsupported)
	}
	if result.TotalInsts != 1 || result.TransInsts != 1 {
		t.Fatalf("instruction accounting total=%d translated=%d", result.TotalInsts, result.TransInsts)
	}
	if len(result.SourceMap) != 2 || result.SourceMap[0].ARM64Offset != 0 || result.SourceMap[1].ARM64Offset != 4 {
		t.Fatalf("source map=%+v", result.SourceMap)
	}
}

func TestExternalBranchWithoutOutlinedEvidenceRemainsRejected(t *testing.T) {
	branch := NewDecoder().Decode(0x14000002, 0)
	translator, err := NewTranslator(0x1000, 4, vm.IdentityOpcodeMap())
	if err != nil {
		t.Fatal(err)
	}
	result, err := translator.Translate([]vm.Instruction{branch})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Unsupported) != 1 || !strings.Contains(result.Unsupported[0], "outside function range") {
		t.Fatalf("unsupported=%v", result.Unsupported)
	}
}

func TestOutlinedTailInlineRequiresFinalBranch(t *testing.T) {
	translator, err := NewTranslator(0x1000, 8, vm.IdentityOpcodeMap())
	if err != nil {
		t.Fatal(err)
	}
	if err := translator.SetOutlinedTailInline(0, exactR29LSEOutlinedHelper); err == nil {
		t.Fatal("non-tail outlined branch was accepted")
	}
}
''')

# ELF end-to-end resolver tests using existing deterministic fixture builder.
Path("internal/elf/outliner_test.go").write_text(r'''package elf

import (
	"strings"
	"testing"

	"github.com/vmpacker/internal/vm"
)

func outlinedFixture(helperName string, callerCode []uint32, helperCode []uint32, callerSize uint64, helperSize uint64) elfFixture {
	code := append(append([]uint32(nil), callerCode...), helperCode...)
	const callerAddr = uint64(0x1200)
	helperAddr := callerAddr + uint64(len(callerCode))*4
	return buildELFFixture(fixtureOptions{
		dynamic: true,
		code:    code,
		symtab: []fixtureSymbol{
			{name: "caller", addr: callerAddr, size: callerSize},
			{name: helperName, addr: helperAddr, size: helperSize},
		},
	})
}

func TestAnalyzeAndPrepareInlineValidatedOutlinedTail(t *testing.T) {
	fixture := outlinedFixture("OUTLINED_FUNCTION_0",
		[]uint32{0xd503201f, 0xd503201f, 0x14000001},
		[]uint32{0x4a0d0100, 0xd65f03c0}, 12, 8)
	req := Request{
		Input: fixture.data,
		Mode:  string(AndroidModeAuto),
		Opcodes: vm.IdentityOpcodeMap(),
		Selections: []SelectionRequest{{Name: "caller"}},
	}
	analysis, err := Analyze(req)
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	preparation, err := PrepareTranslations(req, analysis)
	if err != nil {
		t.Fatalf("PrepareTranslations: %v", err)
	}
	if len(preparation.Functions) != 1 || len(preparation.Functions[0].Translation.Unsupported) != 0 {
		t.Fatalf("preparation=%+v", preparation.Functions)
	}
	mapping := preparation.Functions[0].Translation.SourceMap
	if len(mapping) == 0 || mapping[len(mapping)-1].ARM64Offset != 12 {
		t.Fatalf("source map=%+v", mapping)
	}
	for _, entry := range mapping {
		if entry.ARM64Offset > 12 {
			t.Fatalf("synthetic helper source offset leaked into map: %+v", mapping)
		}
	}
}

func TestAnalyzeRejectsGenericExternalTailBranch(t *testing.T) {
	fixture := outlinedFixture("helper",
		[]uint32{0xd503201f, 0xd503201f, 0x14000001},
		[]uint32{0x4a0d0100, 0xd65f03c0}, 12, 8)
	_, err := Analyze(Request{Input: fixture.data, Mode: string(AndroidModeAuto), Selections: []SelectionRequest{{Name: "caller"}}})
	if err == nil || !strings.Contains(err.Error(), "unsupported external unconditional branch") {
		t.Fatalf("generic external branch err=%v", err)
	}
}

func TestAnalyzeRejectsNonTailOutlinedBranch(t *testing.T) {
	// B at 0x1204 targets helper at 0x1210; caller continues for two instructions.
	fixture := outlinedFixture("OUTLINED_FUNCTION_0",
		[]uint32{0xd503201f, 0x14000003, 0xd503201f, 0xd503201f},
		[]uint32{0x4a0d0100, 0xd65f03c0}, 16, 8)
	_, err := Analyze(Request{Input: fixture.data, Mode: string(AndroidModeAuto), Selections: []SelectionRequest{{Name: "caller"}}})
	if err == nil || !strings.Contains(err.Error(), "not the final instruction") {
		t.Fatalf("non-tail outlined branch err=%v", err)
	}
}

func TestAnalyzeRejectsUnprovenOutlinedHelperBody(t *testing.T) {
	fixture := outlinedFixture("OUTLINED_FUNCTION_0",
		[]uint32{0xd503201f, 0xd503201f, 0x14000001},
		[]uint32{0xd503201f, 0xd65f03c0}, 12, 8)
	_, err := Analyze(Request{Input: fixture.data, Mode: string(AndroidModeAuto), Selections: []SelectionRequest{{Name: "caller"}}})
	if err == nil || !strings.Contains(err.Error(), "not unshifted EOR") {
		t.Fatalf("unproven helper err=%v", err)
	}
}

func TestAnalyzeRejectsZeroSizedOutlinedHelper(t *testing.T) {
	fixture := outlinedFixture("OUTLINED_FUNCTION_0",
		[]uint32{0xd503201f, 0xd503201f, 0x14000001},
		[]uint32{0x4a0d0100, 0xd65f03c0}, 12, 0)
	_, err := Analyze(Request{Input: fixture.data, Mode: string(AndroidModeAuto), Selections: []SelectionRequest{{Name: "caller"}}})
	if err == nil || !strings.Contains(err.Error(), "invalid symbol size") {
		t.Fatalf("zero-sized helper err=%v", err)
	}
}
''')

# Recreate the plan on the clean branch with exact audit facts.
Path(".omx/plans/vmp-phase21-machine-outliner.md").write_text(r'''# VMP Phase 21 — exact-r29 machine-outliner closure

## Goal

Remove the final exact-NDK-r29 compiler-derived intentional boundary, `machine-outliner`, without adding a generic external `B` or native tail-transfer ABI.

## Exact-r29 audit evidence

The macOS 15 / Android NDK `29.0.14206865` audit completed successfully before implementation.

At `-Oz -march=armv8-a`, one local `OUTLINED_FUNCTION_0` exists at `0x480`, size `0x14`. `vmp_atomic8`, `vmp_atomic16`, and `vmp_atomic32` end with unconditional `B` instructions to that exact start. The helper body is four unshifted 32-bit EOR(register) instructions followed by `RET X30`.

At `-Oz -march=armv8.1-a+lse`, one local `OUTLINED_FUNCTION_0` exists at `0x424`, size `0x8`. The same three atomic callers end with unconditional `B` instructions to that exact start. The helper body is one unshifted 32-bit EOR(register) followed by `RET X30`.

No caller executes an instruction after the outliner transfer. The helper bodies are contiguous, image-local, explicitly sized symbols, and RET-terminated.

## Architecture consensus

Use pack-time semantic inlining at the original tail-B source offset:

- selection analysis accepts an external `B` only when it is the selected function's final instruction;
- the target must have one exact `OUTLINED_FUNCTION_<n>` identity and no conflicting/branch-site relocation;
- the helper symbol must have nonzero aligned size, remain within one executable file-backed PT_LOAD, and not overlap the caller;
- raw helper bytes must pass the deliberately narrow exact-r29 semantic validator: one or more unshifted `EOR Wd, Wn, Wm`, then exact `RET X30`;
- the Translator emits those EOR semantics and a VM return at the original B's VM location;
- no synthetic ARM64 helper offsets are added to SourceMap/trailer/exception identities;
- generic external `B`, non-tail transfers, ambiguous helpers, shifted/64-bit EOR, other instructions, and future un-audited outliner shapes remain fail-closed.

No VM opcode, wire format, runtime native-tail bridge, or arbitrary helper importer is added.

## Compiler gate

The exact-r29 whole-compiler verifier resolves external tail-B targets against same-profile `OUTLINED_FUNCTION_*` corpus groups, validates the same helper raw semantics, configures the Translator inline, and requires zero compiler-derived intentional boundaries.

The old `machine-outliner` raw-word exemption and stale expectation are deleted. Any future external compiler transfer or helper shape becomes an unexpected exact-r29 gap.

## Verification / merge policy

Temporary audit/apply workflows and scripts must be absent from the final diff. Focused ARM64/ELF tests, full Go tests/race, exact-r29 FP/SIMD, exact-r29 whole-compiler corpus, exact-r29 runtime build, vet, and macOS ARM64 CLI must pass on the exact PR head. The squash-merged `main` must pass the same push Verification before all historical branches are synchronized to the final verified main SHA.
''')
