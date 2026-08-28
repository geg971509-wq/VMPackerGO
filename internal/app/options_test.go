package app

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOptionsContract(t *testing.T) {
	clearNDKEnvironment(t)
	input := filepath.Join(t.TempDir(), "in.so")
	if err := os.WriteFile(input, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	var help bytes.Buffer
	opts, err := parseOptions([]string{"-func", "foo", "-abi", "i32(ptr)", input}, &help)
	if err != nil {
		t.Fatal(err)
	}
	if opts.Output != input+".vmp" || len(opts.Funcs) != 1 || !opts.Strip {
		t.Fatalf("unexpected options: %#v", opts)
	}
	if _, err := parseOptions([]string{"-func", "foo,bar", "-abi", "void()", input}, &help); err == nil {
		t.Fatal("accepted multiple direct selections")
	}
	if _, err := parseOptions([]string{"-func", "foo", input}, &help); err == nil {
		t.Fatal("accepted missing ABI")
	}
	for _, old := range []string{"-target", "-android-mode", "-injector", "-token", "-debug"} {
		if _, err := parseOptions([]string{old, "x", input}, &help); err == nil {
			t.Fatalf("accepted removed flag %s", old)
		}
	}
	if strings.Contains(help.String(), "保护") {
		t.Fatal("help is not English")
	}
}

func TestSeedAcceptsExactlyThirtyTwoBytes(t *testing.T) {
	clearNDKEnvironment(t)
	input := filepath.Join(t.TempDir(), "in.so")
	seed := strings.Repeat("a", 64)
	opts, err := parseOptions([]string{"-seed", seed, "-func", "x", "-abi", "void()", input}, &bytes.Buffer{})
	if err != nil || opts.Seed != seed {
		t.Fatalf("seed=%q err=%v", opts.Seed, err)
	}
	for _, invalid := range []string{"abc", strings.Repeat("g", 64)} {
		if _, err := parseOptions([]string{"-seed", invalid, "-func", "x", "-abi", "void()", input}, &bytes.Buffer{}); err == nil {
			t.Fatalf("accepted invalid seed %q", invalid)
		}
	}
}

func TestDefaultNDKPathPrecedence(t *testing.T) {
	clearNDKEnvironment(t)
	t.Setenv("NDK_HOME", "/ndk-home")
	t.Setenv("ANDROID_NDK_ROOT", "/ndk-root")
	t.Setenv("ANDROID_NDK_HOME", "/android-ndk-home")
	t.Setenv("ANDROID_NDK", "/android-ndk")
	if got := defaultNDKPath(); got != "/android-ndk" {
		t.Fatalf("defaultNDKPath()=%q", got)
	}
}

func clearNDKEnvironment(t *testing.T) {
	t.Helper()
	for _, name := range []string{"ANDROID_NDK", "ANDROID_NDK_HOME", "ANDROID_NDK_ROOT", "NDK_HOME"} {
		t.Setenv(name, "")
	}
}

func TestReadBoundedInput(t *testing.T) {
	dir := t.TempDir()
	if _, _, err := readBoundedInput(dir); err == nil {
		t.Fatal("accepted directory")
	}
	large := filepath.Join(dir, "large")
	f, err := os.Create(large)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(maxInputSize + 1); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if _, _, err := readBoundedInput(large); err == nil {
		t.Fatal("accepted >1 GiB input")
	}
}

func TestReadBoundedInputStatsOpenedDescriptor(t *testing.T) {
	path := filepath.Join(t.TempDir(), "opened")
	if err := os.WriteFile(path, []byte("descriptor"), 0750); err != nil {
		t.Fatal(err)
	}
	called := false
	data, mode, err := readBoundedInputWith("path-must-not-be-statted", func(string) (openedInput, error) {
		called = true
		return os.Open(path)
	})
	if err != nil || !called || string(data) != "descriptor" || mode.Perm() != 0750 {
		t.Fatalf("called=%v data=%q mode=%o err=%v", called, data, mode.Perm(), err)
	}
}

func TestValidatePathsAliasesAndExisting(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "input")
	if err := os.WriteFile(input, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	hard := filepath.Join(dir, "hard")
	if err := os.Link(input, hard); err != nil {
		t.Fatal(err)
	}
	if err := validatePaths(options{Input: input, Output: hard}); err == nil {
		t.Fatal("accepted hardlink alias")
	}
	symlink := filepath.Join(dir, "symlink")
	if err := os.Symlink(input, symlink); err != nil {
		t.Fatal(err)
	}
	if err := validatePaths(options{Input: input, Output: symlink}); err == nil {
		t.Fatal("accepted symlink alias")
	}
	output := filepath.Join(dir, "output")
	if err := os.WriteFile(output, []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := validatePaths(options{Input: input, Output: output}); err == nil {
		t.Fatal("accepted existing output without force")
	}
	if err := validatePaths(options{Input: input, Output: output, Force: true}); err != nil {
		t.Fatal(err)
	}
	if err := validatePaths(options{Input: input, Output: filepath.Join(dir, "same"), Report: filepath.Join(dir, "same")}); err == nil {
		t.Fatal("accepted output/report collision")
	}
}

func TestValidatePathsResolvesSymlinkedParents(t *testing.T) {
	dir := t.TempDir()
	realDir := filepath.Join(dir, "real")
	if err := os.Mkdir(realDir, 0700); err != nil {
		t.Fatal(err)
	}
	aliasDir := filepath.Join(dir, "alias")
	if err := os.Symlink(realDir, aliasDir); err != nil {
		t.Fatal(err)
	}
	input := filepath.Join(realDir, "same")
	if err := os.WriteFile(input, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := validatePaths(options{Input: input, Output: filepath.Join(aliasDir, "same")}); err == nil {
		t.Fatal("accepted real/same versus alias/same collision")
	}
}

func TestValidatePathsManifestCollisions(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "input")
	manifest := filepath.Join(dir, "manifest")
	if err := os.WriteFile(input, []byte("ELF"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifest, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	for name, output := range map[string]string{
		"self-overwrite": manifest,
		"hardlink":       filepath.Join(dir, "manifest-hard"),
		"symlink":        filepath.Join(dir, "manifest-symlink"),
	} {
		t.Run(name, func(t *testing.T) {
			if name == "hardlink" {
				if err := os.Link(manifest, output); err != nil {
					t.Fatal(err)
				}
			}
			if name == "symlink" {
				if err := os.Symlink(manifest, output); err != nil {
					t.Fatal(err)
				}
			}
			if err := validatePaths(options{Input: input, Manifest: manifest, Output: output}); err == nil {
				t.Fatal("accepted manifest/output collision")
			}
		})
	}
	if err := validatePaths(options{Input: manifest, Manifest: manifest, Output: filepath.Join(dir, "out")}); err == nil {
		t.Fatal("accepted manifest/input collision")
	}
}

func TestValidatePathsRejectsForceSymlinkLeaves(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "input")
	target := filepath.Join(dir, "target")
	if err := os.WriteFile(input, []byte("ELF"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	for name, targetPath := range map[string]string{"symlink": target, "dangling": filepath.Join(dir, "missing")} {
		t.Run(name, func(t *testing.T) {
			output := filepath.Join(dir, name)
			if err := os.Symlink(targetPath, output); err != nil {
				t.Fatal(err)
			}
			if err := validatePaths(options{Input: input, Output: output, Force: true}); err == nil {
				t.Fatal("accepted force symlink destination")
			}
		})
	}
}
