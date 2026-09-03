package app

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestInfoModeIgnoresUnrelatedNDKEnvironment(t *testing.T) {
	for _, name := range []string{"ANDROID_NDK", "ANDROID_NDK_HOME", "ANDROID_NDK_ROOT", "NDK_HOME"} {
		t.Setenv(name, "")
	}
	t.Setenv("ANDROID_NDK", filepath.Join(t.TempDir(), "missing-ndk"))

	input := filepath.Join(t.TempDir(), "input.so")
	if err := os.WriteFile(input, []byte("not-an-elf-yet"), 0600); err != nil {
		t.Fatal(err)
	}
	opts, err := parseOptions([]string{"-info", input}, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("-info unexpectedly depends on Android NDK state: %v", err)
	}
	if !opts.Info || opts.Input != input {
		t.Fatalf("unexpected info options: %#v", opts)
	}
}
