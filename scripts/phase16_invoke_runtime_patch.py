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
# 1. Runtime invoke generator and metadata.
# ---------------------------------------------------------------------------
Path("internal/runtime/invokegen.go").write_text(r'''package runtime

import (
	"fmt"
	"sort"
	"strings"

	"github.com/vmpacker/internal/unwind"
)

var exceptionInvokeLayout = unwind.InvokeThunkLayout{
	CallOffset: 24, CallLength: 4, LandingOffset: 36, RangeLength: 60,
}

type ExceptionInvokeConfig struct {
	FunctionAddress uint64
	Plan            *unwind.ExceptionBridgePlan
}

type ExceptionInvokeImage struct {
	FunctionAddress     uint64
	Personality         uint64
	PersonalityEncoding byte
	PersonalityAnchor   string
	Thunk               unwind.InvokeThunk
	ThunkSymbol         string
	LSDASymbol          string
	LSDA                *unwind.BridgeLSDA
}

func generateExceptionInvokeThunks(configs []ExceptionInvokeConfig) (assembly []byte, normalized []ExceptionInvokeImage, err error) {
	ordered := append([]ExceptionInvokeConfig(nil), configs...)
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].FunctionAddress != ordered[j].FunctionAddress {
			return ordered[i].FunctionAddress < ordered[j].FunctionAddress
		}
		var left, right uint64
		if ordered[i].Plan != nil {
			left = ordered[i].Plan.Personality
		}
		if ordered[j].Plan != nil {
			right = ordered[j].Plan.Personality
		}
		return left < right
	})

	seenID := map[uint32]bool{}
	seenCall := map[[2]uint64]bool{}
	var anchors []struct {
		name     string
		encoding byte
	}

	for _, cfg := range ordered {
		if cfg.FunctionAddress == 0 || cfg.Plan == nil || cfg.Plan.Personality == 0 || len(cfg.Plan.Thunks) == 0 {
			return nil, nil, fmt.Errorf("exception invoke requires a function address and non-empty personality plan")
		}
		if cfg.Plan.PersonalityEncoding != unwind.PEPcrel|unwind.PESdata4 &&
			cfg.Plan.PersonalityEncoding != unwind.PEIndirect|unwind.PEPcrel|unwind.PESdata4 {
			return nil, nil, fmt.Errorf("exception personality encoding 0x%x is not supported by the runtime invoke wrapper", cfg.Plan.PersonalityEncoding)
		}
		anchor := fmt.Sprintf("vm_personality_anchor_%016x", cfg.FunctionAddress)
		anchors = append(anchors, struct {
			name     string
			encoding byte
		}{anchor, cfg.Plan.PersonalityEncoding})

		thunks := append([]unwind.InvokeThunk(nil), cfg.Plan.Thunks...)
		sort.Slice(thunks, func(i, j int) bool {
			if thunks[i].OriginalPC != thunks[j].OriginalPC {
				return thunks[i].OriginalPC < thunks[j].OriginalPC
			}
			return thunks[i].ID < thunks[j].ID
		})
		for _, thunk := range thunks {
			if thunk.ID == 0 || thunk.OriginalPC < cfg.FunctionAddress || thunk.OriginalLandingPad < cfg.FunctionAddress {
				return nil, nil, fmt.Errorf("invoke thunk 0x%08x has invalid original call/landing identity", thunk.ID)
			}
			if seenID[thunk.ID] {
				return nil, nil, fmt.Errorf("duplicate invoke thunk ID 0x%08x", thunk.ID)
			}
			seenID[thunk.ID] = true
			callKey := [2]uint64{cfg.FunctionAddress, thunk.OriginalPC}
			if seenCall[callKey] {
				return nil, nil, fmt.Errorf("duplicate invoke call identity 0x%x", thunk.OriginalPC)
			}
			seenCall[callKey] = true
			bridge, err := unwind.BuildBridgeLSDA(cfg.Plan, thunk, exceptionInvokeLayout)
			if err != nil {
				return nil, nil, fmt.Errorf("build invoke thunk 0x%08x LSDA: %w", thunk.ID, err)
			}
			normalized = append(normalized, ExceptionInvokeImage{
				FunctionAddress: cfg.FunctionAddress, Personality: cfg.Plan.Personality,
				PersonalityEncoding: cfg.Plan.PersonalityEncoding, PersonalityAnchor: anchor,
				Thunk: thunk, ThunkSymbol: fmt.Sprintf("vm_invoke_%08x", thunk.ID),
				LSDASymbol: fmt.Sprintf("vm_invoke_lsda_%08x", thunk.ID), LSDA: cloneBridgeLSDA(bridge),
			})
		}
	}

	var s strings.Builder
	s.WriteString("#include \"vm_abi.h\"\n")
	for _, anchor := range anchors {
		fmt.Fprintf(&s, ".section .rodata.invoke_meta,\"a\",%%progbits\n.p2align 3\n.global %s\n.hidden %s\n.type %s, %%object\n%s:\n.byte 0\n.size %s, .-%s\n", anchor.name, anchor.name, anchor.name, anchor.name, anchor.name, anchor.name)
	}
	for _, item := range normalized {
		fmt.Fprintf(&s, ".section .gcc_except_table.invoke,\"a\",%%progbits\n.p2align 2\n.global %s\n.hidden %s\n.type %s, %%object\n%s:\n", item.LSDASymbol, item.LSDASymbol, item.LSDASymbol, item.LSDASymbol)
		emitAssemblyBytes(&s, item.LSDA.Bytes)
		fmt.Fprintf(&s, ".size %s, .-%s\n", item.LSDASymbol, item.LSDASymbol)
	}
	for _, item := range normalized {
		returnLabel := fmt.Sprintf(".Linvoke_return_%08x", item.Thunk.ID)
		landingLabel := fmt.Sprintf(".Linvoke_landing_%08x", item.Thunk.ID)
		fmt.Fprintf(&s, ".section .text.invoke,\"ax\",%%progbits\n.p2align 2\n.global %s\n.hidden %s\n.type %s, %%function\n%s:\n", item.ThunkSymbol, item.ThunkSymbol, item.ThunkSymbol, item.ThunkSymbol)
		s.WriteString(".cfi_startproc\n")
		fmt.Fprintf(&s, ".cfi_personality 0x%02x, %s\n", item.PersonalityEncoding, item.PersonalityAnchor)
		fmt.Fprintf(&s, ".cfi_lsda 0x1b, %s\n", item.LSDASymbol)
		s.WriteString("bti c\npaciasp\n.cfi_negate_ra_state\nstp x29, x30, [sp, #-32]!\n.cfi_def_cfa_offset 32\n.cfi_offset x29, -32\n.cfi_offset x30, -24\nstr x19, [sp, #16]\n.cfi_offset x19, -16\nmov x29, sp\nmov x19, x0\nbl vm_native_call\nmov w0, #0\n")
		fmt.Fprintf(&s, "b %s\n%s:\n", returnLabel, landingLabel)
		s.WriteString("stp x0, x1, [x19, #VM_CTX_R]\nmov w0, #1\n")
		fmt.Fprintf(&s, "%s:\n", returnLabel)
		s.WriteString("ldr x19, [sp, #16]\n.cfi_restore x19\nldp x29, x30, [sp], #32\n.cfi_def_cfa_offset 0\n.cfi_restore x29\n.cfi_restore x30\nautiasp\n.cfi_negate_ra_state\nret\n.cfi_endproc\n")
		fmt.Fprintf(&s, ".size %s, .-%s\n", item.ThunkSymbol, item.ThunkSymbol)
	}
	s.WriteString(".section .note.gnu.property,\"a\",%note\n.p2align 3\n.long 4\n.long 16\n.long 5\n.asciz \"GNU\"\n.p2align 3\n.long 0xc0000000\n.long 4\n.long 3\n.long 0\n.section .note.GNU-stack,\"\",%progbits\n")
	return []byte(s.String()), normalized, nil
}

func emitAssemblyBytes(s *strings.Builder, data []byte) {
	if len(data) == 0 {
		s.WriteString(".byte 0\n")
		return
	}
	for offset := 0; offset < len(data); offset += 16 {
		end := offset + 16
		if end > len(data) {
			end = len(data)
		}
		s.WriteString(".byte ")
		for i := offset; i < end; i++ {
			if i > offset {
				s.WriteByte(',')
			}
			fmt.Fprintf(s, "0x%02x", data[i])
		}
		s.WriteByte('\n')
	}
}

func cloneBridgeLSDA(source *unwind.BridgeLSDA) *unwind.BridgeLSDA {
	if source == nil {
		return nil
	}
	return &unwind.BridgeLSDA{
		Bytes: append([]byte(nil), source.Bytes...),
		Relocations: append([]unwind.LSDARelocation(nil), source.Relocations...),
	}
}
''')

# ---------------------------------------------------------------------------
# 2. Runtime BuildConfig/Image and compile/link pipeline.
# ---------------------------------------------------------------------------
replace_once(
    "internal/runtime/runtime.go",
    '''\tExclusiveRegions   []vm.ExclusiveRegion
\tFPSIMDInstructions []uint32
}
''',
    '''\tExclusiveRegions   []vm.ExclusiveRegion
\tFPSIMDInstructions []uint32
\tExceptionInvokes   []ExceptionInvokeConfig
}
''',
)
replace_once(
    "internal/runtime/runtime.go",
    '''\tExclusiveRegions   []vm.ExclusiveRegion
\tFPSIMDInstructions []uint32
}
''',
    '''\tExclusiveRegions   []vm.ExclusiveRegion
\tFPSIMDInstructions []uint32
\tExceptionInvokes   []ExceptionInvokeImage
}
''',
)
replace_once(
    "internal/runtime/runtime.go",
    '''\tif err := os.WriteFile(filepath.Join(tempDir, "vm_fpsimd.S"), fpSIMDAssembly, 0600); err != nil {
\t\treturn nil, fmt.Errorf("write generated runtime FP/SIMD assembly")
\t}

\tcObject := filepath.Join(tempDir, "vm_interp.o")
''',
    '''\tif err := os.WriteFile(filepath.Join(tempDir, "vm_fpsimd.S"), fpSIMDAssembly, 0600); err != nil {
\t\treturn nil, fmt.Errorf("write generated runtime FP/SIMD assembly")
\t}
\tinvokeAssembly, exceptionInvokes, err := generateExceptionInvokeThunks(cfg.ExceptionInvokes)
\tif err != nil {
\t\treturn nil, err
\t}
\tif err := os.WriteFile(filepath.Join(tempDir, "vm_invoke.S"), invokeAssembly, 0600); err != nil {
\t\treturn nil, fmt.Errorf("write generated runtime invoke assembly")
\t}

\tcObject := filepath.Join(tempDir, "vm_interp.o")
''',
)
replace_once(
    "internal/runtime/runtime.go",
    '''\tfpSIMDObject := filepath.Join(tempDir, "vm_fpsimd.o")
\toutputObject := filepath.Join(tempDir, "runtime.o")
''',
    '''\tfpSIMDObject := filepath.Join(tempDir, "vm_fpsimd.o")
\tinvokeObject := filepath.Join(tempDir, "vm_invoke.o")
\toutputObject := filepath.Join(tempDir, "runtime.o")
''',
)
replace_once(
    "internal/runtime/runtime.go",
    '''\tif err := runTool(ctx, clang, "compile runtime FP/SIMD thunks", append(append([]string{}, common...), "-o", fpSIMDObject, filepath.Join(tempDir, "vm_fpsimd.S"))...); err != nil {
\t\treturn nil, err
\t}
\tif err := runTool(ctx, linker, "link relocatable runtime", "-r", "--build-id=none", "-o", outputObject, cObject, entryObject, nativeObject, svcObject, exclusiveObject, fpSIMDObject); err != nil {
''',
    '''\tif err := runTool(ctx, clang, "compile runtime FP/SIMD thunks", append(append([]string{}, common...), "-o", fpSIMDObject, filepath.Join(tempDir, "vm_fpsimd.S"))...); err != nil {
\t\treturn nil, err
\t}
\tif err := runTool(ctx, clang, "compile runtime invoke thunks", append(append([]string{}, common...), "-o", invokeObject, filepath.Join(tempDir, "vm_invoke.S"))...); err != nil {
\t\treturn nil, err
\t}
\tif err := runTool(ctx, linker, "link relocatable runtime", "-r", "--build-id=none", "-o", outputObject, cObject, entryObject, nativeObject, svcObject, exclusiveObject, fpSIMDObject, invokeObject); err != nil {
''',
)
replace_once(
    "internal/runtime/runtime.go",
    '''\timage.ExclusiveRegions = append([]vm.ExclusiveRegion(nil), exclusiveRegions...)
\timage.FPSIMDInstructions = append([]uint32(nil), fpSIMDInstructions...)
\treturn image, nil
}
''',
    '''\timage.ExclusiveRegions = append([]vm.ExclusiveRegion(nil), exclusiveRegions...)
\timage.FPSIMDInstructions = append([]uint32(nil), fpSIMDInstructions...)
\timage.ExceptionInvokes = append([]ExceptionInvokeImage(nil), exceptionInvokes...)
\treturn image, nil
}
''',
)

# Runtime unwind validation must include generated invoke symbols.
replace_once(
    "internal/runtime/image.go",
    '''\t\tif symbol.Name == "vm_entry_token" || symbol.Name == "vm_native_call" || symbol.Name == "vm_atomic_native" || strings.HasPrefix(symbol.Name, "vm_svc_") || strings.HasPrefix(symbol.Name, "vm_exclusive_") || strings.HasPrefix(symbol.Name, "vm_fpsimd_") {
''',
    '''\t\tif symbol.Name == "vm_entry_token" || symbol.Name == "vm_native_call" || symbol.Name == "vm_atomic_native" || strings.HasPrefix(symbol.Name, "vm_svc_") || strings.HasPrefix(symbol.Name, "vm_exclusive_") || strings.HasPrefix(symbol.Name, "vm_fpsimd_") || strings.HasPrefix(symbol.Name, "vm_invoke_") {
''',
)

# ---------------------------------------------------------------------------
# 3. Generator and exact-r29 tests.
# ---------------------------------------------------------------------------
replace_once(
    "internal/runtime/runtime_test.go",
    '''\t"github.com/vmpacker/internal/vm"
)
''',
    '''\t"github.com/vmpacker/internal/unwind"
\t"github.com/vmpacker/internal/vm"
)
''',
)
insert_marker = '''func TestBuildInstalledExactR29Object(t *testing.T) {
'''
new_tests = r'''func testExceptionInvokeConfig() ExceptionInvokeConfig {
	return ExceptionInvokeConfig{
		FunctionAddress: 0x1000,
		Plan: &unwind.ExceptionBridgePlan{
			Personality: 0x6000, PersonalityEncoding: unwind.PEIndirect | unwind.PEPcrel | unwind.PESdata4,
			TypeEncoding: unwind.PEOmit,
			Thunks: []unwind.InvokeThunk{{
				ID: 0x1234abcd, OriginalPC: 0x1010, OriginalLandingPad: 0x1020,
				VMCallOffset: 12, VMLandingPad: 44,
			}},
		},
	}
}

func TestGenerateExceptionInvokeThunksIsDeterministicAndUnwindReady(t *testing.T) {
	cfg := testExceptionInvokeConfig()
	before := cfg.Plan.Thunks[0]
	assembly, got, err := generateExceptionInvokeThunks([]ExceptionInvokeConfig{cfg})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Thunk.ID != before.ID || got[0].Personality != cfg.Plan.Personality || got[0].LSDA == nil {
		t.Fatalf("invoke metadata=%+v", got)
	}
	if cfg.Plan.Thunks[0] != before {
		t.Fatal("invoke generator mutated input plan")
	}
	text := string(assembly)
	for _, token := range []string{
		"vm_personality_anchor_0000000000001000", "vm_invoke_lsda_1234abcd", "vm_invoke_1234abcd:",
		".cfi_personality 0x9b", ".cfi_lsda 0x1b", "bti c", "paciasp", "bl vm_native_call",
		"stp x0, x1, [x19, #VM_CTX_R]", "autiasp", ".note.gnu.property",
	} {
		if !strings.Contains(text, token) {
			t.Errorf("invoke assembly lacks %q", token)
		}
	}
	wantLSDA, err := unwind.BuildBridgeLSDA(cfg.Plan, before, exceptionInvokeLayout)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got[0].LSDA.Bytes, wantLSDA.Bytes) || len(got[0].LSDA.Relocations) != len(wantLSDA.Relocations) {
		t.Fatalf("invoke LSDA=%+v want=%+v", got[0].LSDA, wantLSDA)
	}
}

func TestGenerateExceptionInvokeThunksRejectsUnsupportedOrDuplicateIdentity(t *testing.T) {
	cfg := testExceptionInvokeConfig()
	cfg.Plan.PersonalityEncoding = unwind.PEAbsptr
	if _, _, err := generateExceptionInvokeThunks([]ExceptionInvokeConfig{cfg}); err == nil {
		t.Fatal("unsupported personality encoding was accepted")
	}
	cfg = testExceptionInvokeConfig()
	dup := testExceptionInvokeConfig()
	if _, _, err := generateExceptionInvokeThunks([]ExceptionInvokeConfig{cfg, dup}); err == nil {
		t.Fatal("duplicate invoke identity was accepted")
	}
}

'''
replace_once("internal/runtime/runtime_test.go", insert_marker, new_tests + insert_marker)
replace_once(
    "internal/runtime/runtime_test.go",
    '''\timage, err := Build(context.Background(), BuildConfig{
\t\tNDKDir:        root,
\t\tOpcodes:       vm.IdentityOpcodeMap(),
\t\tSVCImmediates: []uint16{0x0000, 0x0001, 0xffff},
''',
    '''\timage, err := Build(context.Background(), BuildConfig{
\t\tNDKDir:        root,
\t\tOpcodes:       vm.IdentityOpcodeMap(),
\t\tSVCImmediates: []uint16{0x0000, 0x0001, 0xffff},
\t\tExceptionInvokes: []ExceptionInvokeConfig{testExceptionInvokeConfig()},
''',
)
replace_once(
    "internal/runtime/runtime_test.go",
    '''\tif len(image.ExclusiveRegions) != 2 || len(image.FPSIMDInstructions) != 6 {
\t\tt.Fatalf("generated thunks: exclusive=%d fpsimd=%d", len(image.ExclusiveRegions), len(image.FPSIMDInstructions))
\t}
}
''',
    '''\tif len(image.ExclusiveRegions) != 2 || len(image.FPSIMDInstructions) != 6 || len(image.ExceptionInvokes) != 1 {
\t\tt.Fatalf("generated thunks: exclusive=%d fpsimd=%d invoke=%d", len(image.ExclusiveRegions), len(image.FPSIMDInstructions), len(image.ExceptionInvokes))
\t}
\tfoundInvoke := false
\tfor _, symbol := range image.Symbols {
\t\tif symbol.Name == "vm_invoke_1234abcd" {
\t\t\tfoundInvoke = true
\t\t}
\t}
\tif !foundInvoke {
\t\tt.Fatal("exact-r29 runtime lacks generated invoke symbol")
\t}
}
''',
)

print("phase16 invoke runtime patch applied")
