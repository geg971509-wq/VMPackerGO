package main

import (
	"bytes"
	"context"
	"testing"

	"github.com/geg971509-wq/VMPackerGO/internal/app"
)

func TestDefaultVersion(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if err := app.RunWithConfig(context.Background(), []string{"-version"}, &stdout, &stderr, app.Config{Version: version, Commit: commit}); err != nil {
		t.Fatalf("err=%v stderr=%s", err, stderr.String())
	}
	if stdout.String() != "vmpacker dev (unknown)\n" {
		t.Fatalf("got %q", stdout.String())
	}
}
