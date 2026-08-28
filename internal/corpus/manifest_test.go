package corpus

import (
	"path/filepath"
	"testing"
)

func TestApprovedDemoManifestIsExactAndComplete(t *testing.T) {
	repositoryRoot := filepath.Clean(filepath.Join("..", ".."))
	manifest, err := Load(filepath.Join(repositoryRoot, "demo", "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := manifest.Validate(repositoryRoot); err != nil {
		t.Fatal(err)
	}
}
