package runtime

import (
	"bytes"
	"context"
	"debug/elf"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/geg971509-wq/VMPackerGO/internal/vm"
)

const (
	NDKRevision  = "29.0.14206865"
	templateRoot = "templates/android/arm64"
)

//go:embed templates/android/arm64
var templates embed.FS

type BuildConfig struct {
	NDKDir             string
	Opcodes            vm.OpcodeMap
	SVCImmediates      []uint16
	ExclusiveRegions   []vm.ExclusiveRegion
	FPSIMDInstructions []uint32
	ExceptionInvokes   []ExceptionInvokeConfig
}

type Section struct {
	Index     int
	Name      string
	Type      elf.SectionType
	Flags     elf.SectionFlag
	Alignment uint64
	Size      uint64
	NOBITS    bool
	Data      []byte
}

type Symbol struct {
	Index   uint32
	Name    string
	Info    byte
	Other   byte
	Section elf.SectionIndex
	Value   uint64
	Size    uint64
}

type Relocation struct {
	SectionIndex uint32
	TargetIndex  uint32
	Offset       uint64
	Type         elf.R_AARCH64
	SymbolIndex  uint32
	Addend       int64
}

type Image struct {
	Object             []byte
	Sections           []Section
	Symbols            []Symbol
	Relocations        []Relocation
	GNUPropertyNote    []byte
	EHFrame            []byte
	OpcodeMapDigest    string
	SVCImmediates      []uint16
	ExclusiveRegions   []vm.ExclusiveRegion
	FPSIMDInstructions []uint32
	ExceptionInvokes   []ExceptionInvokeImage
}

func (image *Image) ValidateOpcodeMap(opcodes vm.OpcodeMap) error {
	if image == nil {
		return fmt.Errorf("runtime image is required")
	}
	digest, err := opcodes.Digest()
	if err != nil {
		return fmt.Errorf("validate opcode map: %w", err)
	}
	if image.OpcodeMapDigest != hex.EncodeToString(digest[:]) {
		return fmt.Errorf("runtime image opcode-map digest mismatch")
	}
	return nil
}

func Build(ctx context.Context, cfg BuildConfig) (*Image, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if cfg.NDKDir == "" {
		return nil, fmt.Errorf("Android NDK r29 path is required")
	}
	if err := cfg.Opcodes.Validate(); err != nil {
		return nil, fmt.Errorf("validate opcode map: %w", err)
	}
	if err := validateNDK(cfg.NDKDir); err != nil {
		return nil, err
	}

	binDir := ndkBinDir(cfg.NDKDir)
	clang, err := executableTool(binDir, "aarch64-linux-android23-clang")
	if err != nil {
		return nil, err
	}
	linker, err := executableTool(binDir, "ld.lld")
	if err != nil {
		return nil, err
	}

	tempDir, err := os.MkdirTemp("", "vmpacker-runtime-")
	if err != nil {
		return nil, fmt.Errorf("create private runtime build directory")
	}
	defer os.RemoveAll(tempDir)
	if err := os.Chmod(tempDir, 0700); err != nil {
		return nil, fmt.Errorf("secure runtime build directory")
	}
	if err := extractTemplates(tempDir); err != nil {
		return nil, err
	}
	header, err := cfg.Opcodes.CHeader()
	if err != nil {
		return nil, fmt.Errorf("generate runtime opcode header: %w", err)
	}
	if err := os.WriteFile(filepath.Join(tempDir, "vm_opcodes.h"), []byte(header), 0600); err != nil {
		return nil, fmt.Errorf("write generated runtime opcode header")
	}
	svcHeader, svcAssembly, svcImmediates := generateSVCThunks(cfg.SVCImmediates)
	if err := os.WriteFile(filepath.Join(tempDir, "vm_svc.h"), svcHeader, 0600); err != nil {
		return nil, fmt.Errorf("write generated runtime SVC header")
	}
	if err := os.WriteFile(filepath.Join(tempDir, "vm_svc.S"), svcAssembly, 0600); err != nil {
		return nil, fmt.Errorf("write generated runtime SVC assembly")
	}
	exclusiveHeader, exclusiveAssembly, exclusiveRegions, err := generateExclusiveThunks(cfg.ExclusiveRegions)
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(tempDir, "vm_exclusive.h"), exclusiveHeader, 0600); err != nil {
		return nil, fmt.Errorf("write generated runtime exclusive header")
	}
	if err := os.WriteFile(filepath.Join(tempDir, "vm_exclusive.S"), exclusiveAssembly, 0600); err != nil {
		return nil, fmt.Errorf("write generated runtime exclusive assembly")
	}
	fpSIMDHeader, fpSIMDAssembly, fpSIMDInstructions, err := generateFPSIMDThunks(cfg.FPSIMDInstructions)
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(tempDir, "vm_fpsimd.h"), fpSIMDHeader, 0600); err != nil {
		return nil, fmt.Errorf("write generated runtime FP/SIMD header")
	}
	if err := os.WriteFile(filepath.Join(tempDir, "vm_fpsimd.S"), fpSIMDAssembly, 0600); err != nil {
		return nil, fmt.Errorf("write generated runtime FP/SIMD assembly")
	}
	invokeHeader, invokeAssembly, exceptionInvokes, err := generateExceptionInvokeThunks(cfg.ExceptionInvokes)
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(tempDir, "vm_invoke.h"), invokeHeader, 0600); err != nil {
		return nil, fmt.Errorf("write generated runtime invoke header")
	}
	if err := os.WriteFile(filepath.Join(tempDir, "vm_invoke.S"), invokeAssembly, 0600); err != nil {
		return nil, fmt.Errorf("write generated runtime invoke assembly")
	}

	cObject := filepath.Join(tempDir, "vm_interp.o")
	entryObject := filepath.Join(tempDir, "vm_entry.o")
	nativeObject := filepath.Join(tempDir, "vm_native.o")
	svcObject := filepath.Join(tempDir, "vm_svc.o")
	exclusiveObject := filepath.Join(tempDir, "vm_exclusive.o")
	fpSIMDObject := filepath.Join(tempDir, "vm_fpsimd.o")
	invokeObject := filepath.Join(tempDir, "vm_invoke.o")
	outputObject := filepath.Join(tempDir, "runtime.o")
	common := []string{
		"-c", "-Os", "-fPIC", "-ffreestanding", "-fno-builtin", "-fno-stack-protector",
		"-fvisibility=hidden", "-funwind-tables", "-fasynchronous-unwind-tables",
		"-mbranch-protection=pac-ret+bti", "-nostdlib", "-I", tempDir,
	}
	if err := runTool(ctx, clang, "compile runtime C", append(append([]string{}, common...), "-o", cObject, filepath.Join(tempDir, "vm_interp.c"))...); err != nil {
		return nil, err
	}
	if err := runTool(ctx, clang, "compile runtime entry", append(append([]string{}, common...), "-o", entryObject, filepath.Join(tempDir, "vm_entry.S"))...); err != nil {
		return nil, err
	}
	if err := runTool(ctx, clang, "compile native-call bridge", append(append([]string{}, common...), "-o", nativeObject, filepath.Join(tempDir, "vm_native.S"))...); err != nil {
		return nil, err
	}
	if err := runTool(ctx, clang, "compile runtime SVC thunks", append(append([]string{}, common...), "-o", svcObject, filepath.Join(tempDir, "vm_svc.S"))...); err != nil {
		return nil, err
	}
	if err := runTool(ctx, clang, "compile runtime exclusive thunks", append(append([]string{}, common...), "-o", exclusiveObject, filepath.Join(tempDir, "vm_exclusive.S"))...); err != nil {
		return nil, err
	}
	if err := runTool(ctx, clang, "compile runtime FP/SIMD thunks", append(append([]string{}, common...), "-o", fpSIMDObject, filepath.Join(tempDir, "vm_fpsimd.S"))...); err != nil {
		return nil, err
	}
	if err := runTool(ctx, clang, "compile runtime invoke thunks", append(append([]string{}, common...), "-o", invokeObject, filepath.Join(tempDir, "vm_invoke.S"))...); err != nil {
		return nil, err
	}
	if err := runTool(ctx, linker, "link relocatable runtime", "-r", "--build-id=none", "-o", outputObject, cObject, entryObject, nativeObject, svcObject, exclusiveObject, fpSIMDObject, invokeObject); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	object, err := os.ReadFile(outputObject)
	if err != nil {
		return nil, fmt.Errorf("read linked runtime object")
	}
	image, err := ParseImage(object, cfg.Opcodes)
	if err != nil {
		return nil, fmt.Errorf("validate linked runtime object: %w", err)
	}
	if err := validateExceptionInvokeImage(image, exceptionInvokes); err != nil {
		return nil, fmt.Errorf("validate linked exception invoke artifacts: %w", err)
	}
	image.SVCImmediates = append([]uint16(nil), svcImmediates...)
	image.ExclusiveRegions = append([]vm.ExclusiveRegion(nil), exclusiveRegions...)
	image.FPSIMDInstructions = append([]uint32(nil), fpSIMDInstructions...)
	image.ExceptionInvokes = append([]ExceptionInvokeImage(nil), exceptionInvokes...)
	return image, nil
}

func validateNDK(root string) error {
	data, err := os.ReadFile(filepath.Join(root, "source.properties"))
	if err != nil {
		return fmt.Errorf("read Android NDK revision metadata")
	}
	for _, line := range strings.Split(string(data), "\n") {
		key, value, ok := strings.Cut(line, "=")
		if ok && strings.TrimSpace(key) == "Pkg.Revision" {
			found := strings.TrimSpace(value)
			if found != NDKRevision {
				return fmt.Errorf("Android NDK revision %s is required; found %q", NDKRevision, found)
			}
			return nil
		}
	}
	return fmt.Errorf("Android NDK revision metadata is incomplete")
}

func ndkBinDir(ndkDir string) string {
	prebuilt := filepath.Join(ndkDir, "toolchains", "llvm", "prebuilt")
	arm64 := filepath.Join(prebuilt, "darwin-arm64", "bin")
	if _, err := executableTool(arm64, "aarch64-linux-android23-clang"); err == nil {
		return arm64
	}
	return filepath.Join(prebuilt, "darwin-x86_64", "bin")
}

func executableTool(binDir, name string) (string, error) {
	path := filepath.Join(binDir, name)
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 {
		return "", fmt.Errorf("Android NDK tool %s is missing or not executable", name)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve Android NDK tool %s", name)
	}
	return abs, nil
}

func extractTemplates(destination string) error {
	return fs.WalkDir(templates, templateRoot, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return fmt.Errorf("read embedded runtime templates")
		}
		rel, err := filepath.Rel(templateRoot, path)
		if err != nil || rel == "." {
			return nil
		}
		target := filepath.Join(destination, filepath.FromSlash(rel))
		if entry.IsDir() {
			if err := os.Mkdir(target, 0700); err != nil && !errors.Is(err, os.ErrExist) {
				return fmt.Errorf("create runtime template directory")
			}
			return nil
		}
		data, err := templates.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read embedded runtime template")
		}
		if err := os.WriteFile(target, data, 0600); err != nil {
			return fmt.Errorf("write runtime template")
		}
		return nil
	})
}

func runTool(ctx context.Context, path, stage string, args ...string) error {
	cmd := exec.CommandContext(ctx, path, args...)
	var stderr bytes.Buffer
	cmd.Stdout = ioDiscard{}
	cmd.Stderr = &stderr
	err := cmd.Run()
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	if err != nil {
		return fmt.Errorf("%s failed", stage)
	}
	return nil
}

// ioDiscard avoids allowing a tool to inherit product stdout while keeping the
// command construction explicit and shell-free.
type ioDiscard struct{}

func (ioDiscard) Write(p []byte) (int, error) { return len(p), nil }
