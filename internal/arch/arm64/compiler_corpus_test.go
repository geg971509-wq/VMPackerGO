package arm64

import (
	"bufio"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/vmpacker/internal/vm"
)

type compilerCorpusRecord struct {
	Optimization string
	Profile      string
	Function     string
	Address      uint64
	Raw          uint32
	Mnemonic     string
	Operands     string
}

type compilerCorpusKey struct {
	Optimization string
	Profile      string
	Function     string
}

type compilerCoverageReport struct {
	Unexpected       []string
	Intentional      []string
	IntentionalKinds map[string]int
}

const compilerCorpusHeader = "optimization\tprofile\tfunction\taddress\traw\tmnemonic\toperands"

var exactR29CASPBoundaryRaws = map[uint32]bool{
	0x48207d02: true, // O0 CASP
	0x48607d02: true, // O0 CASPA
	0x4820fd02: true, // O0 CASPL
	0x4860fd02: true, // O0 CASPAL
	0x48267c04: true, // O2/Oz CASP
	0x48647c04: true, // O2/Oz CASPA
	0x482afc02: true, // O2/Oz CASPL
	0x4868fc0a: true, // O2/Oz CASPAL
	0x486afc02: true, // O2/Oz CASPAL
}

var exactR29OutlinedTailRaws = map[uint32]bool{
	0x14000050: true,
	0x1400003b: true,
	0x14000065: true,
	0x14000045: true,
	0x14000036: true,
	0x14000054: true,
}

func parseCompilerCorpus(scanner *bufio.Scanner) ([]compilerCorpusRecord, error) {
	scanner.Buffer(make([]byte, 4096), 1024*1024)
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("compiler corpus is empty")
	}
	if scanner.Text() != compilerCorpusHeader {
		return nil, fmt.Errorf("compiler corpus header mismatch: %q", scanner.Text())
	}

	var records []compilerCorpusRecord
	line := 1
	for scanner.Scan() {
		line++
		fields := strings.SplitN(scanner.Text(), "\t", 7)
		if len(fields) != 7 {
			return nil, fmt.Errorf("compiler corpus line %d has %d fields; want 7", line, len(fields))
		}
		if fields[0] != "O0" && fields[0] != "O2" && fields[0] != "Oz" {
			return nil, fmt.Errorf("compiler corpus line %d has invalid optimization %q", line, fields[0])
		}
		if fields[1] != "base" && fields[1] != "lse" {
			return nil, fmt.Errorf("compiler corpus line %d has invalid profile %q", line, fields[1])
		}
		if fields[2] == "" || fields[5] == "" {
			return nil, fmt.Errorf("compiler corpus line %d has empty function or mnemonic", line)
		}
		address, err := strconv.ParseUint(fields[3], 16, 64)
		if err != nil {
			return nil, fmt.Errorf("compiler corpus line %d address %q: %w", line, fields[3], err)
		}
		if len(fields[4]) != 8 {
			return nil, fmt.Errorf("compiler corpus line %d raw word %q is not 8 hex digits", line, fields[4])
		}
		raw, err := strconv.ParseUint(fields[4], 16, 32)
		if err != nil {
			return nil, fmt.Errorf("compiler corpus line %d raw %q: %w", line, fields[4], err)
		}
		records = append(records, compilerCorpusRecord{
			Optimization: fields[0], Profile: fields[1], Function: fields[2],
			Address: address, Raw: uint32(raw), Mnemonic: fields[5], Operands: fields[6],
		})
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if len(records) == 0 {
		return nil, fmt.Errorf("compiler corpus contains no instruction records")
	}
	return records, nil
}

func groupCompilerCorpus(records []compilerCorpusRecord) (map[compilerCorpusKey][]compilerCorpusRecord, []string) {
	groups := make(map[compilerCorpusKey][]compilerCorpusRecord)
	var gaps []string
	for _, record := range records {
		key := compilerCorpusKey{record.Optimization, record.Profile, record.Function}
		group := groups[key]
		if len(group) != 0 {
			previous := group[len(group)-1]
			if previous.Address > ^uint64(0)-4 || record.Address != previous.Address+4 {
				gaps = append(gaps, fmt.Sprintf("-%s/%s %s: non-contiguous instruction addresses 0x%x -> 0x%x",
					key.Optimization, key.Profile, key.Function, previous.Address, record.Address))
			}
		}
		groups[key] = append(group, record)
	}
	return groups, gaps
}

func compilerRecordLabel(record compilerCorpusRecord) string {
	operands := record.Operands
	if operands != "" {
		operands = " " + operands
	}
	return fmt.Sprintf("-%s/%s %s+0x%x raw=%08x %s%s",
		record.Optimization, record.Profile, record.Function, record.Address, record.Raw, record.Mnemonic, operands)
}

func exactR29IntentionalBoundary(record compilerCorpusRecord, issue string) (string, bool) {

	if record.Profile == "lse" && record.Function == "vmp_atomic128" && exactR29CASPBoundaryRaws[record.Raw] &&
		(record.Mnemonic == "casp" || record.Mnemonic == "caspa" || record.Mnemonic == "caspl" || record.Mnemonic == "caspal") &&
		strings.Contains(issue, "rejected by the product whitelist") {
		return "casp128", true
	}

	if record.Optimization == "Oz" && strings.HasPrefix(record.Function, "vmp_atomic") &&
		exactR29OutlinedTailRaws[record.Raw] && record.Mnemonic == "b" &&
		strings.Contains(record.Operands, "OUTLINED_FUNCTION_") && strings.Contains(issue, "outside function range") {
		return "machine-outliner", true
	}
	return "", false
}

func addCompilerIssue(report *compilerCoverageReport, record compilerCorpusRecord, issue string) {
	line := compilerRecordLabel(record) + ": " + issue
	if kind, ok := exactR29IntentionalBoundary(record, issue); ok {
		if report.IntentionalKinds == nil {
			report.IntentionalKinds = map[string]int{}
		}
		report.IntentionalKinds[kind]++
		report.Intentional = append(report.Intentional, "["+kind+"] "+line)
		return
	}
	report.Unexpected = append(report.Unexpected, line)
}

func classifyCompilerCorpus(records []compilerCorpusRecord) compilerCoverageReport {
	groups, groupGaps := groupCompilerCorpus(records)
	report := compilerCoverageReport{Unexpected: append([]string(nil), groupGaps...), IntentionalKinds: map[string]int{}}
	keys := make([]compilerCorpusKey, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].Optimization != keys[j].Optimization {
			return keys[i].Optimization < keys[j].Optimization
		}
		if keys[i].Profile != keys[j].Profile {
			return keys[i].Profile < keys[j].Profile
		}
		return keys[i].Function < keys[j].Function
	})

	decoder := NewDecoder()
	for _, key := range keys {
		group := groups[key]
		if len(group) == 0 {
			continue
		}
		start := group[0].Address
		last := group[len(group)-1].Address
		if last < start || last-start > uint64(int(^uint(0)>>1))-4 {
			report.Unexpected = append(report.Unexpected, fmt.Sprintf("-%s/%s %s: function address span is not representable",
				key.Optimization, key.Profile, key.Function))
			continue
		}
		funcSize := int(last-start) + 4
		instructions := make([]vm.Instruction, 0, len(group))
		byOffset := make(map[int]compilerCorpusRecord, len(group))
		for _, record := range group {
			offset := int(record.Address - start)
			inst := decoder.Decode(record.Raw, offset)
			instructions = append(instructions, inst)
			byOffset[offset] = record

			op := Op(inst.Op)
			rule, ok := instructionRules[op]
			if !ok {
				report.Unexpected = append(report.Unexpected, compilerRecordLabel(record)+": decoder result has no product policy rule")
				continue
			}
			switch rule.disposition {
			case dispositionReject, dispositionNativeThunk:
				// Whole-function translation below is authoritative. Rejected
				// instructions are reported there with their exact source offset;
				// native-thunk instructions may only be closed as validated regions.
			case dispositionVirtual:
				if rule.validate != nil {
					if err := rule.validate(inst); err != nil {
						report.Unexpected = append(report.Unexpected, compilerRecordLabel(record)+": product policy validation: "+err.Error())
					}
				}
			default:
				report.Unexpected = append(report.Unexpected, compilerRecordLabel(record)+": invalid product policy disposition")
			}
		}

		translator, err := NewTranslator(start, funcSize, vm.IdentityOpcodeMap())
		if err != nil {
			report.Unexpected = append(report.Unexpected, fmt.Sprintf("-%s/%s %s: construct translator: %v",
				key.Optimization, key.Profile, key.Function, err))
			continue
		}
		result, err := translator.Translate(instructions)
		if err != nil {
			report.Unexpected = append(report.Unexpected, fmt.Sprintf("-%s/%s %s: whole-function translation: %v",
				key.Optimization, key.Profile, key.Function, err))
			continue
		}
		for _, unsupported := range result.Unsupported {
			if offset, ok := unsupportedOffset(unsupported); ok {
				if record, found := byOffset[offset]; found {
					addCompilerIssue(&report, record, "translator: "+unsupported)
					continue
				}
			}
			report.Unexpected = append(report.Unexpected, fmt.Sprintf("-%s/%s %s: translator: %s",
				key.Optimization, key.Profile, key.Function, unsupported))
		}

		// Side metadata and source-map closure are product obligations only for
		// functions that actually translated. Intentional fail-closed functions
		// are rejected before any partial metadata can be consumed.
		if len(result.Unsupported) != 0 {
			continue
		}
		for _, region := range result.ExclusiveRegions {
			if !region.Valid() {
				report.Unexpected = append(report.Unexpected, fmt.Sprintf("-%s/%s %s: invalid exclusive-region content identity 0x%08x",
					key.Optimization, key.Profile, key.Function, region.ID))
			}
		}
		for _, raw := range result.FPSIMDInstructions {
			if err := ValidateFPSIMDInstruction(raw); err != nil {
				report.Unexpected = append(report.Unexpected, fmt.Sprintf("-%s/%s %s: FP/SIMD side instruction %08x: %v",
					key.Optimization, key.Profile, key.Function, raw, err))
			}
		}
		if len(result.SourceMap) == 0 || result.SourceMap[len(result.SourceMap)-1].ARM64Offset != funcSize {
			report.Unexpected = append(report.Unexpected, fmt.Sprintf("-%s/%s %s: translator source map does not terminate at function size 0x%x",
				key.Optimization, key.Profile, key.Function, funcSize))
		} else {
			previousARM, previousVM := -1, -1
			for _, entry := range result.SourceMap {
				if entry.ARM64Offset <= previousARM || entry.VMOffset < previousVM || entry.VMOffset > result.CodeLen {
					report.Unexpected = append(report.Unexpected, fmt.Sprintf("-%s/%s %s: invalid source-map entry arm64=0x%x vm=0x%x",
						key.Optimization, key.Profile, key.Function, entry.ARM64Offset, entry.VMOffset))
					break
				}
				previousARM, previousVM = entry.ARM64Offset, entry.VMOffset
			}
		}
	}

	report.Unexpected = sortedUniqueStrings(report.Unexpected)
	report.Intentional = sortedUniqueStrings(report.Intentional)
	return report
}

func verifyCompilerCorpus(records []compilerCorpusRecord) []string {
	return classifyCompilerCorpus(records).Unexpected
}

func sortedUniqueStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	sort.Strings(values)
	unique := values[:0]
	for _, value := range values {
		if len(unique) == 0 || unique[len(unique)-1] != value {
			unique = append(unique, value)
		}
	}
	return unique
}

func unsupportedOffset(message string) (int, bool) {
	const prefix = "offset 0x"
	start := strings.Index(message, prefix)
	if start < 0 {
		return 0, false
	}
	start += len(prefix)
	end := strings.IndexByte(message[start:], ':')
	if end < 0 {
		return 0, false
	}
	value, err := strconv.ParseUint(message[start:start+end], 16, 32)
	if err != nil {
		return 0, false
	}
	return int(value), true
}

func TestCompilerCorpusVerifierUsesWholeFunctionTranslation(t *testing.T) {
	input := compilerCorpusHeader + "\n" +
		"O2\tbase\ttiny\t0\td503201f\tnop\t\n" +
		"O2\tbase\ttiny\t4\td65f03c0\tret\t\n"
	records, err := parseCompilerCorpus(bufio.NewScanner(strings.NewReader(input)))
	if err != nil {
		t.Fatal(err)
	}
	if gaps := verifyCompilerCorpus(records); len(gaps) != 0 {
		t.Fatalf("supported tiny function gaps=%v", gaps)
	}
}

func TestCompilerCorpusVerifierReportsRejectedInstruction(t *testing.T) {
	input := compilerCorpusHeader + "\n" +
		"Oz\tlse\tbad\t0\td4200000\tbrk\t#0\n" +
		"Oz\tlse\tbad\t4\td65f03c0\tret\t\n"
	records, err := parseCompilerCorpus(bufio.NewScanner(strings.NewReader(input)))
	if err != nil {
		t.Fatal(err)
	}
	gaps := verifyCompilerCorpus(records)
	if len(gaps) == 0 || !strings.Contains(strings.Join(gaps, "\n"), "BRK") {
		t.Fatalf("rejected instruction gaps=%v", gaps)
	}
}

func TestCompilerIntentionalBoundaryRequiresExactEvidence(t *testing.T) {
	casp := compilerCorpusRecord{Optimization: "O2", Profile: "lse", Function: "vmp_atomic128", Raw: 0x48267c04, Mnemonic: "casp"}
	if kind, ok := exactR29IntentionalBoundary(casp, "translator: offset 0x0: UNKNOWN - rejected by the product whitelist"); !ok || kind != "casp128" {
		t.Fatalf("exact CASP boundary kind=%q ok=%v", kind, ok)
	}
	casp.Raw ^= 1 << 16
	if _, ok := exactR29IntentionalBoundary(casp, "translator: offset 0x0: UNKNOWN - rejected by the product whitelist"); ok {
		t.Fatal("unobserved CASP raw was accepted as an intentional boundary")
	}
	outlined := compilerCorpusRecord{Optimization: "Oz", Profile: "base", Function: "vmp_atomic16", Raw: 0x14000050, Mnemonic: "b", Operands: "0x480 <OUTLINED_FUNCTION_0>"}
	if kind, ok := exactR29IntentionalBoundary(outlined, "translator: offset 0x50: B - branch target is outside function range"); !ok || kind != "machine-outliner" {
		t.Fatalf("exact outliner boundary kind=%q ok=%v", kind, ok)
	}
	outlined.Operands = "0x480 <some_other_symbol>"
	if _, ok := exactR29IntentionalBoundary(outlined, "translator: offset 0x50: B - branch target is outside function range"); ok {
		t.Fatal("non-outliner external branch was accepted as intentional")
	}
}

func TestCompilerCorpusParserRejectsMalformedRows(t *testing.T) {
	input := compilerCorpusHeader + "\nO3\tbase\tf\t0\td503201f\tnop\t\n"
	if _, err := parseCompilerCorpus(bufio.NewScanner(strings.NewReader(input))); err == nil {
		t.Fatal("invalid optimization was accepted")
	}
}

func TestExactR29CompilerCorpusCoverage(t *testing.T) {
	path := os.Getenv("VMPACKER_COMPILER_CORPUS")
	if path == "" {
		t.Skip("exact-r29 compiler corpus listing is not configured")
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	records, err := parseCompilerCorpus(bufio.NewScanner(file))
	if err != nil {
		t.Fatal(err)
	}

	requiredFunctions := []string{
		"vmp_integer64", "vmp_integer32", "vmp_select", "vmp_control", "vmp_calls",
		"vmp_memory", "vmp_wide", "vmp_abi_pressure", "vmp_atomic8", "vmp_atomic16",
		"vmp_atomic32", "vmp_atomic64", "vmp_atomic128",
	}
	for _, optimization := range []string{"O0", "O2", "Oz"} {
		for _, profile := range []string{"base", "lse"} {
			functions := map[string]bool{}
			count := 0
			for _, record := range records {
				if record.Optimization == optimization && record.Profile == profile {
					count++
					functions[record.Function] = true
				}
			}
			if count < 50 {
				t.Errorf("-%s/%s compiler corpus has only %d instruction records", optimization, profile, count)
			}
			for _, function := range requiredFunctions {
				if !functions[function] {
					t.Errorf("-%s/%s compiler corpus lacks %s", optimization, profile, function)
				}
			}
		}
	}

	report := classifyCompilerCorpus(records)
	for _, kind := range []string{"casp128", "machine-outliner"} {
		if report.IntentionalKinds[kind] == 0 {
			t.Errorf("exact-r29 compiler corpus no longer exercises intentional boundary %q; audit and remove/update the expectation", kind)
		}
	}
	if len(report.Unexpected) != 0 {
		t.Fatalf("exact-r29 compiler coverage has %d unexpected gap(s) (%d intentional fail-closed record(s)):\n%s",
			len(report.Unexpected), len(report.Intentional), strings.Join(report.Unexpected, "\n"))
	}
}
