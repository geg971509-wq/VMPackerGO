package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestManifestInputMustBeRegularAndBounded(t *testing.T) {
	t.Run("directory", func(t *testing.T) {
		_, _, _, err := loadManifest(t.TempDir())
		if err == nil || !strings.Contains(err.Error(), "regular file") {
			t.Fatalf("err=%v", err)
		}
	})

	t.Run("oversized", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "manifest.json")
		if err := os.WriteFile(path, nil, 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.Truncate(path, maxManifestSize+1); err != nil {
			t.Fatal(err)
		}
		_, _, _, err := loadManifest(path)
		if err == nil || !strings.Contains(err.Error(), "16 MiB limit") {
			t.Fatalf("err=%v", err)
		}
	})
}
