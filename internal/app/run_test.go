package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	elfpacker "github.com/geg971509-wq/VMPackerGO/internal/elf"
)

func TestRunVersion(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := RunWithConfig(context.Background(), []string{"-version"}, &stdout, &stderr, Config{Version: "v1.2.3", Commit: "abc123"})
	if err != nil || stdout.String() != "vmpacker v1.2.3 (abc123)\n" || stderr.Len() != 0 {
		t.Fatalf("err=%v stdout=%q stderr=%q", err, stdout.String(), stderr.String())
	}
}

func TestRunHelpAndUsageExitCodes(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if err := Run(context.Background(), []string{"-h"}, &stdout, &stderr); !errors.Is(err, flag.ErrHelp) || ExitCode(err) != 0 {
		t.Fatalf("help err=%v code=%d", err, ExitCode(err))
	}
	if strings.Contains(stderr.String(), "vmpacker: flag: help requested") {
		t.Fatalf("help appended error: %q", stderr.String())
	}
	if err := Run(context.Background(), nil, &stdout, &stderr); err == nil || ExitCode(err) != 2 {
		t.Fatalf("usage err=%v code=%d", err, ExitCode(err))
	}
}

func TestRunPublishesExplicitOutputsOnly(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "raw name.so")
	output := filepath.Join(dir, "packed.so")
	reportPath := filepath.Join(dir, "report.json")
	debugPath := filepath.Join(dir, "map.txt")
	if err := os.WriteFile(input, []byte("ELF"), 0750); err != nil {
		t.Fatal(err)
	}
	fake := func(req elfpacker.Request) (elfpacker.Result, error) {
		if req.Context == nil || req.Mode != "so" || len(req.Selections) != 1 || req.Selections[0].Name != "foo" || !req.Debug {
			t.Fatalf("unexpected request: %#v", req)
		}
		return elfpacker.Result{Artifact: []byte("packed"), Debug: []byte("mapping"), TargetKind: elfpacker.TargetKindAndroidSO, DevelopmentStrategy: "note", Functions: []elfpacker.FunctionFact{{Name: "foo", Instructions: 2, Translated: 2, Bytecode: 4}}}, nil
	}
	var stdout, stderr bytes.Buffer
	err := RunWithConfig(context.Background(), []string{"-mode", "so", "-func", "foo", "-abi", "i32(ptr)", "-o", output, "-report", reportPath, "-debug-map", debugPath, input}, &stdout, &stderr, Config{Version: "dev", Commit: "test", Process: fake})
	if err != nil {
		t.Fatalf("err=%v stderr=%s", err, stderr.String())
	}
	for path, want := range map[string]string{output: "packed", debugPath: "mapping"} {
		data, err := os.ReadFile(path)
		if err != nil || string(data) != want {
			t.Fatalf("%s: %q %v", path, data, err)
		}
	}
	if _, err := os.Stat(output + ".debug.txt"); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("hidden debug output was created")
	}
	var got map[string]any
	data, err := os.ReadFile(reportPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got["input"] != input || got["output"] != output || got["status"] != "ok" {
		t.Fatalf("unexpected report: %s", data)
	}
	if strings.Contains(string(data), os.Getenv("HOME")) && !strings.Contains(input, os.Getenv("HOME")) {
		t.Fatal("report leaked home path")
	}
}

func TestRunFailurePublishesOnlyRequestedReport(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "in.so")
	output := filepath.Join(dir, "out.so")
	reportPath := filepath.Join(dir, "failure.json")
	if err := os.WriteFile(input, []byte("ELF"), 0644); err != nil {
		t.Fatal(err)
	}
	fake := func(elfpacker.Request) (elfpacker.Result, error) {
		return elfpacker.Result{
			TargetKind:          elfpacker.TargetKindAndroidSO,
			AnalysisLimitations: []string{"CFG inference is conservative"},
		}, errors.New("unsupported instruction")
	}
	err := RunWithConfig(context.Background(), []string{"-func", "foo", "-abi", "void()", "-o", output, "-report", reportPath, input}, &bytes.Buffer{}, &bytes.Buffer{}, Config{Process: fake, Version: "dev", Commit: "test"})
	if err == nil || ExitCode(err) != 1 {
		t.Fatalf("err=%v code=%d", err, ExitCode(err))
	}
	if _, err := os.Stat(output); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("failure created artifact")
	}
	data, err := os.ReadFile(reportPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte(`"status": "failed"`)) ||
		!bytes.Contains(data, []byte(`"target_kind": "android-so"`)) ||
		!bytes.Contains(data, []byte("CFG inference is conservative")) || bytes.Contains(data, []byte("output_sha256")) {
		t.Fatalf("unexpected report: %s", data)
	}
}

func TestRunCanceledBeforeProcessorOrPublish(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "in.so")
	output := filepath.Join(dir, "out.so")
	if err := os.WriteFile(input, []byte("ELF"), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	called := false
	err := RunWithConfig(ctx, []string{"-func", "foo", "-abi", "void()", "-o", output, input}, ioDiscard{}, ioDiscard{}, Config{Process: func(elfpacker.Request) (elfpacker.Result, error) {
		called = true
		return elfpacker.Result{}, nil
	}})
	if !errors.Is(err, context.Canceled) || called {
		t.Fatalf("err=%v called=%v", err, called)
	}
	if _, err := os.Stat(output); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("canceled run published output: %v", err)
	}
}

type ioDiscard struct{}

func (ioDiscard) Write(p []byte) (int, error) { return len(p), nil }
