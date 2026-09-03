package runtime

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"debug/elf"

	"github.com/geg971509-wq/VMPackerGO/internal/unwind"
	"github.com/geg971509-wq/VMPackerGO/internal/vm"
)

func TestBuildUsesExactNDKToolsAndCleansPrivateExtraction(t *testing.T) {
	root, capture, fixture := fakeNDK(t)
	t.Setenv("VMPACKER_TEST_CAPTURE", capture)
	t.Setenv("VMPACKER_TEST_FIXTURE", fixture)
	t.Setenv("PATH", t.TempDir())

	opcodes := vm.IdentityOpcodeMap()
	image, err := Build(context.Background(), BuildConfig{NDKDir: root, Opcodes: opcodes, SVCImmediates: []uint16{0x1234, 0, 0x1234}})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if err := image.ValidateOpcodeMap(opcodes); err != nil {
		t.Fatal(err)
	}
	if len(image.SVCImmediates) != 2 || image.SVCImmediates[0] != 0 || image.SVCImmediates[1] != 0x1234 {
		t.Fatalf("SVC immediates=%v", image.SVCImmediates)
	}
	header, err := os.ReadFile(capture + ".header")
	if err != nil {
		t.Fatal(err)
	}
	wantHeader, _ := opcodes.CHeader()
	if string(header) != wantHeader {
		t.Fatal("compiled opcode header did not exactly match the map")
	}
	buildDirBytes, err := os.ReadFile(capture + ".dir")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(strings.TrimSpace(string(buildDirBytes))); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("runtime build directory was not cleaned: %v", err)
	}
}

func TestGenerateSVCThunksIsSortedDeduplicatedAndExact(t *testing.T) {
	header, assembly, got := generateSVCThunks([]uint16{0xFFFF, 1, 0, 1})
	want := []uint16{0, 1, 0xFFFF}
	if len(got) != len(want) {
		t.Fatalf("immediates=%v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("immediates=%v, want %v", got, want)
		}
	}
	for _, token := range []string{"case 0x0000: vm_svc_0000", "case 0x0001: vm_svc_0001", "case 0xffff: vm_svc_ffff"} {
		if !strings.Contains(string(header), token) {
			t.Errorf("generated header lacks %q", token)
		}
	}
	for _, token := range []string{"svc #0x0000", "svc #0x0001", "svc #0xffff", ".cfi_startproc", "bti c", ".note.gnu.property", "0xc0000000"} {
		if !strings.Contains(string(assembly), token) {
			t.Errorf("generated assembly lacks %q", token)
		}
	}
	if strings.Count(string(assembly), "vm_svc_0001:") != 1 {
		t.Fatal("duplicate SVC immediate generated more than one thunk")
	}
}

func TestGenerateExclusiveThunksIsContinuousContentAddressedAndCFI(t *testing.T) {
	region := vm.NewExclusiveRegion([]uint32{
		0xc85ffc20, // ldaxr x0, [x1]
		0x91000400, // add x0, x0, #1
		0xc802fc20, // stlxr w2, x0, [x1]
	})
	header, assembly, got, err := generateExclusiveThunks([]vm.ExclusiveRegion{region, region})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != region.ID {
		t.Fatalf("regions=%#v", got)
	}
	name := fmt.Sprintf("vm_exclusive_%08x", region.ID)
	for _, token := range []string{name, "return 5", "VM_FAULT_SYSTEM"} {
		if !strings.Contains(string(header), token) {
			t.Errorf("generated header lacks %q", token)
		}
	}
	for _, token := range []string{name + ":", "#include \"vm_abi.h\"", ".inst 0xc85ffc20", ".inst 0x91000400", ".inst 0xc802fc20", ".cfi_startproc", "bti c", "ldr w17, [x16, #VM_CTX_FL]", "msr nzcv, x17", "mrs x17, nzcv", "str w17, [x16, #VM_CTX_FL]", ".note.gnu.property"} {
		if !strings.Contains(string(assembly), token) {
			t.Errorf("generated assembly lacks %q", token)
		}
	}
	if strings.Index(string(assembly), ".inst 0xc85ffc20") > strings.Index(string(assembly), ".inst 0xc802fc20") {
		t.Fatal("exclusive instruction order changed")
	}
	if strings.Count(string(assembly), "msr nzcv, x17") != 1 || strings.Count(string(assembly), "mrs x17, nzcv") != 1 {
		t.Fatal("exclusive thunk did not bridge NZCV exactly once")
	}

	bad := region
	bad.Instructions[1] = 0x14000000
	bad = vm.NewExclusiveRegion(bad.Instructions)
	if _, _, _, err := generateExclusiveThunks([]vm.ExclusiveRegion{bad}); err == nil {
		t.Fatal("branching exclusive region was accepted")
	}
}

func TestGenerateExclusiveThunksRemapsHighGuestRegisters(t *testing.T) {
	region := vm.NewExclusiveRegion([]uint32{
		0xc85ffe34,
		0x91000694,
		0xc813fe34,
	})
	_, assembly, _, err := generateExclusiveThunks([]vm.ExclusiveRegion{region})
	if err != nil {
		t.Fatal(err)
	}
	for _, token := range []string{
		"ldr x0, [x16, #136]", "ldr x1, [x16, #152]", "ldr x2, [x16, #160]",
		".inst 0xc85ffc02", ".inst 0x91000442", ".inst 0xc801fc02",
		"str x0, [x16, #136]", "str x1, [x16, #152]", "str x2, [x16, #160]",
	} {
		if !strings.Contains(string(assembly), token) {
			t.Errorf("generated assembly lacks %q", token)
		}
	}
}

func TestGenerateFPSIMDThunksPreservesArchitecturalStateAndFlags(t *testing.T) {
	header, assembly, got, err := generateFPSIMDThunks([]uint32{0x1e212000, 0x1e202820, 0x1e2203a0, 0x1e38001e, 0xfd0007e0, 0x1e212000})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 5 {
		t.Fatalf("instructions=%#v", got)
	}
	for _, token := range []string{"case 0x1e202820u", "case 0x1e2203a0u", "case 0x1e38001eu", "case 0xfd0007e0u", "VM_FAULT_SYSTEM", "vm_fpsimd_stack_access_ok"} {
		if !strings.Contains(string(header), token) {
			t.Errorf("header lacks %q", token)
		}
	}
	for _, token := range []string{"bti c", ".cfi_startproc", "VM_CTX_FPCR", "VM_CTX_FPSR", "ldp q30, q31", "stp q30, q31", "ldr x9, [x16, #(VM_CTX_R + 29 * 8)]", "str x9, [x16, #(VM_CTX_R + 30 * 8)]", "ldr x9, [x16, #(VM_CTX_R + 31 * 8)]", ".inst 0x1e220120", ".inst 0x1e380009", ".inst 0xfd000520", "mrs x17, nzcv", "msr fpcr, x17", "msr fpsr, x17", ".note.gnu.property"} {
		if !strings.Contains(string(assembly), token) {
			t.Errorf("assembly lacks %q", token)
		}
	}
	for _, token := range []string{"mov sp, x", " str x18", " ldr x18", " x19", " x20", " x21", " x22", " x23", " x24", " x25", " x26", " x27", " x28", " x29", " x30"} {
		if strings.Contains(string(assembly), token) {
			t.Errorf("assembly unexpectedly uses reserved host register token %q", token)
		}
	}
	if strings.Count(string(assembly), "mrs x17, nzcv") != 1 {
		t.Fatal("NZCV was not limited to the FCMP thunk")
	}
	if _, _, _, err := generateFPSIMDThunks([]uint32{0x00000000}); err == nil {
		t.Fatal("unknown FP/SIMD encoding was accepted")
	}
}

func testExceptionInvokeConfig() ExceptionInvokeConfig {
	return ExceptionInvokeConfig{
		FunctionAddress: 0x1000,
		Routes: []ExceptionRouteConfig{{
			ThunkID: 0x1234abcd, FinalVMCallOffset: 64, FinalVMLandingOffset: 32,
		}},
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
	_, assembly, got, err := generateExceptionInvokeThunks([]ExceptionInvokeConfig{cfg})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Thunk.ID != before.ID || got[0].Personality != cfg.Plan.Personality || got[0].LSDA == nil {
		t.Fatalf("invoke metadata=%+v", got)
	}
	after := cfg.Plan.Thunks[0]
	if after.ID != before.ID || after.OriginalPC != before.OriginalPC || after.OriginalLandingPad != before.OriginalLandingPad || after.VMCallOffset != before.VMCallOffset || after.VMLandingPad != before.VMLandingPad || after.Action != before.Action || len(after.Actions) != len(before.Actions) {
		t.Fatal("invoke generator mutated input plan")
	}
	text := string(assembly)
	for _, token := range []string{
		"vm_personality_bridge_0000000000001000", "vm_lsda_invoke_1234abcd", "vm_invoke_1234abcd:",
		".cfi_personality 0x1b", ".cfi_lsda 0x1b", "bti c", "paciasp", "bl vm_native_call",
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
	if string(got[0].LSDA.Bytes) != string(wantLSDA.Bytes) || len(got[0].LSDA.Relocations) != len(wantLSDA.Relocations) {
		t.Fatalf("invoke LSDA=%+v want=%+v", got[0].LSDA, wantLSDA)
	}
}

func TestGenerateExceptionInvokeThunksRejectsUnsupportedOrDuplicateIdentity(t *testing.T) {
	cfg := testExceptionInvokeConfig()
	cfg.Plan.PersonalityEncoding = unwind.PEDatarel | unwind.PESdata4
	if _, _, _, err := generateExceptionInvokeThunks([]ExceptionInvokeConfig{cfg}); err == nil {
		t.Fatal("unsupported personality encoding was accepted")
	}
	cfg = testExceptionInvokeConfig()
	dup := testExceptionInvokeConfig()
	if _, _, _, err := generateExceptionInvokeThunks([]ExceptionInvokeConfig{cfg, dup}); err == nil {
		t.Fatal("duplicate invoke identity was accepted")
	}
}

func TestBuildInstalledExactR29Object(t *testing.T) {
	root := os.Getenv("ANDROID_NDK")
	if root == "" {
		root = os.Getenv("ANDROID_NDK_HOME")
	}
	if root == "" {
		t.Skip("exact Android NDK r29 is not configured")
	}
	image, err := Build(context.Background(), BuildConfig{
		NDKDir:           root,
		Opcodes:          vm.IdentityOpcodeMap(),
		SVCImmediates:    []uint16{0x0000, 0x0001, 0xffff},
		ExceptionInvokes: []ExceptionInvokeConfig{testExceptionInvokeConfig()},
		ExclusiveRegions: []vm.ExclusiveRegion{
			vm.NewExclusiveRegion([]uint32{0xc85ffc20, 0x91000400, 0xc802fc20}),
			vm.NewExclusiveRegion([]uint32{0xc85ffe34, 0x91000694, 0xc813fe34}),
			vm.NewExclusiveRegion([]uint32{0xc87f0440, 0xc8239444}),
		},
		FPSIMDInstructions: []uint32{0x1e202820, 0x1e212000, 0x3dc00000, 0x1e2203a0, 0x1e38001e, 0xfd0007e0},
	})
	if err != nil {
		t.Fatalf("Build with installed r29: %v", err)
	}
	if len(image.EHFrame) == 0 || len(image.GNUPropertyNote) == 0 || len(image.Relocations) == 0 {
		t.Fatalf("incomplete r29 image: eh_frame=%d note=%d relocations=%d", len(image.EHFrame), len(image.GNUPropertyNote), len(image.Relocations))
	}
	if len(image.ExclusiveRegions) != 3 || len(image.FPSIMDInstructions) != 6 || len(image.ExceptionInvokes) != 1 {
		t.Fatalf("generated thunks: exclusive=%d fpsimd=%d invoke=%d", len(image.ExclusiveRegions), len(image.FPSIMDInstructions), len(image.ExceptionInvokes))
	}
	foundInvoke := false
	for _, symbol := range image.Symbols {
		if symbol.Name == "vm_invoke_1234abcd" {
			foundInvoke = true
		}
	}
	if !foundInvoke {
		t.Fatal("exact-r29 runtime lacks generated invoke symbol")
	}
}

func TestBuildRejectsNDKAndToolErrorsWithoutPaths(t *testing.T) {
	t.Run("missing-ndk", func(t *testing.T) {
		if _, err := Build(context.Background(), BuildConfig{Opcodes: vm.IdentityOpcodeMap()}); err == nil {
			t.Fatal("accepted missing NDK")
		}
	})
	t.Run("wrong-revision", func(t *testing.T) {
		root := t.TempDir()
		if err := os.WriteFile(filepath.Join(root, "source.properties"), []byte("Pkg.Revision = 28.0.0\n"), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := Build(context.Background(), BuildConfig{NDKDir: root, Opcodes: vm.IdentityOpcodeMap()}); err == nil || !strings.Contains(err.Error(), NDKRevision) {
			t.Fatalf("err=%v", err)
		}
	})
	t.Run("missing-tool", func(t *testing.T) {
		root := t.TempDir()
		if err := os.WriteFile(filepath.Join(root, "source.properties"), []byte("Pkg.Revision = "+NDKRevision+"\n"), 0600); err != nil {
			t.Fatal(err)
		}
		errText := ""
		if _, err := Build(context.Background(), BuildConfig{NDKDir: root, Opcodes: vm.IdentityOpcodeMap()}); err != nil {
			errText = err.Error()
		}
		if !strings.Contains(errText, "missing or not executable") || strings.Contains(errText, root) {
			t.Fatalf("path-neutral error=%q", errText)
		}
	})
	t.Run("compiler-error", func(t *testing.T) {
		root, capture, fixture := fakeNDK(t)
		t.Setenv("VMPACKER_TEST_CAPTURE", capture)
		t.Setenv("VMPACKER_TEST_FIXTURE", fixture)
		t.Setenv("VMPACKER_TEST_FAIL", "1")
		_, err := Build(context.Background(), BuildConfig{NDKDir: root, Opcodes: vm.IdentityOpcodeMap()})
		if err == nil || !strings.Contains(err.Error(), "compile runtime C failed") || strings.Contains(err.Error(), root) {
			t.Fatalf("path-neutral compile error=%v", err)
		}
	})
}

func TestBuildCancellationCleansExtraction(t *testing.T) {
	root, capture, fixture := fakeNDK(t)
	t.Setenv("VMPACKER_TEST_CAPTURE", capture)
	t.Setenv("VMPACKER_TEST_FIXTURE", fixture)
	t.Setenv("VMPACKER_TEST_BLOCK", "1")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := Build(ctx, BuildConfig{NDKDir: root, Opcodes: vm.IdentityOpcodeMap()})
		done <- err
	}()
	dirPath := capture + ".dir"
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(dirPath); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("compiler never started")
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	err := <-done
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v", err)
	}
	buildDirBytes, readErr := os.ReadFile(dirPath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if _, statErr := os.Stat(strings.TrimSpace(string(buildDirBytes))); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("canceled build directory was not cleaned: %v", statErr)
	}
}

func TestExtractTemplatesPermissionsAndGeneratedHeaderAuthority(t *testing.T) {
	root := filepath.Join(t.TempDir(), "runtime")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	if err := extractTemplates(root); err != nil {
		t.Fatal(err)
	}
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		want := os.FileMode(0600)
		if info.IsDir() {
			want = 0700
		}
		if info.Mode().Perm() != want {
			t.Errorf("%s mode=%o, want %o", filepath.Base(path), info.Mode().Perm(), want)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "vm_opcodes.h")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("fixed opcode header is still embedded")
	}
}

func TestNDKBinDirPrefersDarwinArm64(t *testing.T) {
	root := t.TempDir()
	arm64 := filepath.Join(root, "toolchains", "llvm", "prebuilt", "darwin-arm64", "bin")
	x86 := filepath.Join(root, "toolchains", "llvm", "prebuilt", "darwin-x86_64", "bin")
	if err := os.MkdirAll(arm64, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(x86, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(arm64, "aarch64-linux-android23-clang"), []byte("#!/bin/sh\n"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(x86, "aarch64-linux-android23-clang"), []byte("#!/bin/sh\n"), 0700); err != nil {
		t.Fatal(err)
	}
	if got := ndkBinDir(root); got != arm64 {
		t.Fatalf("ndkBinDir()=%q, want %q", got, arm64)
	}
}

func TestNDKBinDirFallsBackToDarwinX86(t *testing.T) {
	root := t.TempDir()
	x86 := filepath.Join(root, "toolchains", "llvm", "prebuilt", "darwin-x86_64", "bin")
	if got := ndkBinDir(root); got != x86 {
		t.Fatalf("ndkBinDir()=%q, want %q", got, x86)
	}
}

func fakeNDK(t *testing.T) (root, capture, fixture string) {
	t.Helper()
	root = t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "source.properties"), []byte("Pkg.Revision = "+NDKRevision+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(root, "toolchains", "llvm", "prebuilt", "darwin-x86_64", "bin")
	if err := os.MkdirAll(bin, 0700); err != nil {
		t.Fatal(err)
	}
	script := `#!/bin/sh
set -eu
out=""
include=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    -o) out="$2"; shift 2 ;;
    -I) include="$2"; shift 2 ;;
    *) shift ;;
  esac
done
if [ -n "$include" ]; then
  printf '%s\n' "$include" > "${VMPACKER_TEST_CAPTURE}.dir"
  /bin/cp "$include/vm_opcodes.h" "${VMPACKER_TEST_CAPTURE}.header"
fi
if [ "${VMPACKER_TEST_BLOCK:-}" = 1 ]; then
  while :; do :; done
fi
if [ "${VMPACKER_TEST_FAIL:-}" = 1 ]; then
  echo "private path: $include" >&2
  exit 1
fi
case "$0" in
  *ld.lld) /bin/cp "$VMPACKER_TEST_FIXTURE" "$out" ;;
  *) : > "$out" ;;
esac
`
	for _, name := range []string{"aarch64-linux-android23-clang", "ld.lld"} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte(script), 0700); err != nil {
			t.Fatal(err)
		}
	}
	capture = filepath.Join(t.TempDir(), "capture")
	fixture = filepath.Join(t.TempDir(), "runtime.o")
	object := buildRuntimeFixture(t, fixtureConfig{relocationType: elf.R_AARCH64_PREL32, features: 3})
	if err := os.WriteFile(fixture, object, 0600); err != nil {
		t.Fatal(err)
	}
	return root, capture, fixture
}
