package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeManifest(t *testing.T, text string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "manifest.json")
	if err := os.WriteFile(path, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestManifestValid(t *testing.T) {
	path := writeManifest(t, `{"schema_version":1,"functions":[{"name":"foo","abi":{"params":["ptr"],"result":"i32"}},{"name":"bar","range":" 0X100-0x120 ","abi":{"params":[],"result":"void"}}]}`)
	funcs, specs, selected, err := loadManifest(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(funcs) != 1 || len(specs) != 1 || len(selected) != 2 {
		t.Fatalf("unexpected result: %#v %#v %#v", funcs, specs, selected)
	}
	if selected[1].Selector != " 0X100-0x120 " || selected[1].Range != "0x100-0x120" || selected[1].Name != "bar" {
		t.Fatalf("raw/normalized selector mismatch: %#v", selected[1])
	}
}

func TestManifestRejectsInvalidContracts(t *testing.T) {
	cases := map[string]string{
		"version":          `{"schema_version":2,"functions":[{"name":"x","abi":{"params":[],"result":"void"}}]}`,
		"unknown":          `{"schema_version":1,"extra":true,"functions":[{"name":"x","abi":{"params":[],"result":"void"}}]}`,
		"nested-unknown":   `{"schema_version":1,"functions":[{"name":"x","extra":true,"abi":{"params":[],"result":"void"}}]}`,
		"trailing-object":  `{"schema_version":1,"functions":[{"name":"x","abi":{"params":[],"result":"void"}}]} {}`,
		"duplicate":        `{"schema_version":1,"schema_version":1,"functions":[]}`,
		"nested-duplicate": `{"schema_version":1,"functions":[{"name":"x","abi":{"params":[],"params":[],"result":"void"}}]}`,
		"conflict":         `{"schema_version":1,"functions":[{"name":"x","address":"0x10","range":"0x10-0x20","abi":{"params":[],"result":"void"}}]}`,
		"empty":            `{"schema_version":1,"functions":[]}`,
		"missing-params":   `{"schema_version":1,"functions":[{"name":"x","abi":{"result":"void"}}]}`,
		"null-params":      `{"schema_version":1,"functions":[{"name":"x","abi":{"params":null,"result":"void"}}]}`,
		"comma-param":      `{"schema_version":1,"functions":[{"name":"x","abi":{"params":["i32,u32"],"result":"void"}}]}`,
		"space-param":      `{"schema_version":1,"functions":[{"name":"x","abi":{"params":["i32 u32"],"result":"void"}}]}`,
		"empty-param":      `{"schema_version":1,"functions":[{"name":"x","abi":{"params":[""],"result":"void"}}]}`,
		"unknown-param":    `{"schema_version":1,"functions":[{"name":"x","abi":{"params":["f32"],"result":"void"}}]}`,
		"missing-result":   `{"schema_version":1,"functions":[{"name":"x","abi":{"params":[]}}]}`,
		"null-result":      `{"schema_version":1,"functions":[{"name":"x","abi":{"params":[],"result":null}}]}`,
	}
	for name, text := range cases {
		t.Run(name, func(t *testing.T) {
			if _, _, _, err := loadManifest(writeManifest(t, text)); err == nil {
				t.Fatal("accepted invalid manifest")
			}
		})
	}
}

func TestManifestRejectsDuplicateEquivalentSelectors(t *testing.T) {
	for name, selectors := range map[string]string{
		"address": `{"address":"0x10"},{"address":"0X10"}`,
		"range":   `{"range":"0x10-0x20"},{"range":"0X10-0X20"}`,
	} {
		t.Run(name, func(t *testing.T) {
			entries := strings.ReplaceAll(selectors, `}`, `,"abi":{"params":[],"result":"void"}}`)
			text := `{"schema_version":1,"functions":[` + entries + `]}`
			if _, _, _, err := loadManifest(writeManifest(t, text)); err == nil {
				t.Fatal("accepted equivalent selectors")
			}
		})
	}
}

func TestManifestRejectsMoreThan4096Functions(t *testing.T) {
	entry := `{"name":"x","abi":{"params":[],"result":"void"}}`
	path := writeManifest(t, `{"schema_version":1,"functions":[`+strings.Repeat(entry+",", 4096)+entry+`]}`)
	if _, _, _, err := loadManifest(path); err == nil {
		t.Fatal("accepted oversized manifest")
	}
}
