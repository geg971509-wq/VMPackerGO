package corpus

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	ManifestSchema = 1
	DemoCount      = 85
	CCount         = 83
	GoCount        = 1
	RustCount      = 1
)

type Entry struct {
	ID       string `json:"id"`
	Language string `json:"language"`
	Source   string `json:"source"`
}

type Manifest struct {
	SchemaVersion int     `json:"schema_version"`
	NDKRevision   string  `json:"ndk_revision"`
	AndroidAPI    int     `json:"android_api"`
	Entries       []Entry `json:"entries"`
}

func Load(path string) (*Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read corpus manifest: %w", err)
	}
	var manifest Manifest
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return nil, fmt.Errorf("decode corpus manifest: %w", err)
	}
	return &manifest, nil
}

func (manifest *Manifest) Validate(repositoryRoot string) error {
	if manifest == nil || manifest.SchemaVersion != ManifestSchema || manifest.NDKRevision != "29.0.14206865" || manifest.AndroidAPI != 23 {
		return fmt.Errorf("corpus manifest contract mismatch")
	}
	if len(manifest.Entries) != DemoCount {
		return fmt.Errorf("corpus has %d entries; want %d", len(manifest.Entries), DemoCount)
	}
	counts := map[string]int{}
	ids := map[string]bool{}
	sources := map[string]bool{}
	for _, entry := range manifest.Entries {
		if entry.ID == "" || ids[entry.ID] || entry.Source == "" || sources[entry.Source] {
			return fmt.Errorf("duplicate or empty corpus entry %q", entry.ID)
		}
		if filepath.IsAbs(entry.Source) || filepath.Clean(entry.Source) != entry.Source || strings.HasPrefix(entry.Source, "..") {
			return fmt.Errorf("unsafe corpus source %q", entry.Source)
		}
		if _, err := os.Stat(filepath.Join(repositoryRoot, filepath.FromSlash(entry.Source))); err != nil {
			return fmt.Errorf("corpus source %q is unavailable", entry.Source)
		}
		ids[entry.ID], sources[entry.Source] = true, true
		counts[entry.Language]++
	}
	if counts["c"] != CCount || counts["go"] != GoCount || counts["rust"] != RustCount || len(counts) != 3 {
		return fmt.Errorf("corpus language counts are c=%d go=%d rust=%d", counts["c"], counts["go"], counts["rust"])
	}
	return nil
}
