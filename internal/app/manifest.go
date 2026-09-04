package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/geg971509-wq/VMPackerGO/internal/abi"
	elfpacker "github.com/geg971509-wq/VMPackerGO/internal/elf"
)

const maxFunctions = 4096
const maxManifestSize int64 = 16 << 20

type selectedFunction struct {
	Source   string
	Selector string
	Name     string
	Address  string
	Range    string
	ABI      abi.Signature
}

type manifestV1 struct {
	SchemaVersion int                `json:"schema_version"`
	Functions     []manifestFunction `json:"functions"`
}

type manifestFunction struct {
	Name    string      `json:"name,omitempty"`
	Address string      `json:"address,omitempty"`
	Range   string      `json:"range,omitempty"`
	ABI     manifestABI `json:"abi"`
}

type manifestABI struct {
	Params json.RawMessage `json:"params"`
	Result *string         `json:"result"`
}

func loadManifest(path string) ([]string, []elfpacker.AddrSpec, []selectedFunction, error) {
	data, err := readManifest(path)
	if err != nil {
		return nil, nil, nil, err
	}
	if err := rejectDuplicateJSONKeys(data); err != nil {
		return nil, nil, nil, fmt.Errorf("invalid manifest: %w", err)
	}
	var manifest manifestV1
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&manifest); err != nil {
		return nil, nil, nil, fmt.Errorf("invalid manifest: %w", err)
	}
	if err := requireJSONEOF(dec); err != nil {
		return nil, nil, nil, fmt.Errorf("invalid manifest: %w", err)
	}
	if manifest.SchemaVersion != 1 {
		return nil, nil, nil, fmt.Errorf("manifest schema_version must be 1")
	}
	if len(manifest.Functions) == 0 {
		return nil, nil, nil, fmt.Errorf("manifest functions must not be empty")
	}
	if len(manifest.Functions) > maxFunctions {
		return nil, nil, nil, fmt.Errorf("manifest has %d functions; maximum is %d", len(manifest.Functions), maxFunctions)
	}

	var funcs []string
	var addrSpecs []elfpacker.AddrSpec
	selected := make([]selectedFunction, 0, len(manifest.Functions))
	seen := make(map[string]bool, len(manifest.Functions))
	for i, fn := range manifest.Functions {
		rawName, rawAddress, rawRange := fn.Name, fn.Address, fn.Range
		fn.Name = strings.TrimSpace(fn.Name)
		fn.Address = strings.TrimSpace(fn.Address)
		fn.Range = strings.TrimSpace(fn.Range)
		selectorCount := 0
		if fn.Address != "" {
			selectorCount++
		}
		if fn.Range != "" {
			selectorCount++
		}
		if selectorCount == 0 && fn.Name != "" {
			selectorCount++
		}
		if selectorCount != 1 {
			return nil, nil, nil, fmt.Errorf("manifest function %d must select exactly one name, address, or range", i+1)
		}
		var params []string
		if len(fn.ABI.Params) == 0 || bytes.Equal(bytes.TrimSpace(fn.ABI.Params), []byte("null")) {
			return nil, nil, nil, fmt.Errorf("manifest function %d ABI params must be an array", i+1)
		}
		if err := json.Unmarshal(fn.ABI.Params, &params); err != nil {
			return nil, nil, nil, fmt.Errorf("manifest function %d ABI params: %w", i+1, err)
		}
		if fn.ABI.Result == nil {
			return nil, nil, nil, fmt.Errorf("manifest function %d ABI result must be a string", i+1)
		}
		sig, err := abi.FromParts(params, *fn.ABI.Result)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("manifest function %d ABI: %w", i+1, err)
		}
		selection := selectedFunction{Source: "manifest", Name: fn.Name, ABI: sig}
		var key string
		switch {
		case fn.Address != "":
			spec, err := elfpacker.ParseAddrSpec(fn.Address)
			if err != nil || spec.End != 0 {
				if err == nil {
					err = fmt.Errorf("address must not be a range")
				}
				return nil, nil, nil, fmt.Errorf("manifest function %d address: %w", i+1, err)
			}
			if fn.Name != "" {
				spec.Name = fn.Name
			} else if strings.Contains(fn.Address, ":") {
				selection.Name = spec.Name
			}
			addrSpecs = append(addrSpecs, spec)
			selection.Selector = rawAddress
			selection.Address = fmt.Sprintf("0x%x", spec.Addr)
			key = fmt.Sprintf("address:%x", spec.Addr)
		case fn.Range != "":
			spec, err := elfpacker.ParseAddrSpec(fn.Range)
			if err != nil || spec.End == 0 {
				if err == nil {
					err = fmt.Errorf("range must include start and end addresses")
				}
				return nil, nil, nil, fmt.Errorf("manifest function %d range: %w", i+1, err)
			}
			if fn.Name != "" {
				spec.Name = fn.Name
			} else if strings.Contains(fn.Range, ":") {
				selection.Name = spec.Name
			}
			addrSpecs = append(addrSpecs, spec)
			selection.Selector = rawRange
			selection.Range = fmt.Sprintf("0x%x-0x%x", spec.Addr, spec.End)
			key = fmt.Sprintf("range:%x-%x", spec.Addr, spec.End)
		default:
			funcs = append(funcs, fn.Name)
			selection.Selector = rawName
			key = "name:" + fn.Name
		}
		if seen[key] {
			return nil, nil, nil, fmt.Errorf("manifest contains duplicate selector %q", selection.Selector)
		}
		seen[key] = true
		selected = append(selected, selection)
	}
	return funcs, addrSpecs, selected, nil
}

func readManifest(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open manifest: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat opened manifest: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("manifest must be a regular file")
	}
	if info.Size() > maxManifestSize {
		return nil, fmt.Errorf("manifest exceeds the 16 MiB limit")
	}
	data, err := io.ReadAll(io.LimitReader(file, maxManifestSize+1))
	if err != nil {
		return nil, fmt.Errorf("read manifest: %w", err)
	}
	if int64(len(data)) > maxManifestSize {
		return nil, fmt.Errorf("manifest exceeds the 16 MiB limit")
	}
	return data, nil
}

func rejectDuplicateJSONKeys(data []byte) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	var walk func(string) error
	walk = func(path string) error {
		tok, err := dec.Token()
		if err != nil {
			return err
		}
		delim, ok := tok.(json.Delim)
		if !ok {
			return nil
		}
		switch delim {
		case '{':
			seen := map[string]bool{}
			for dec.More() {
				keyToken, err := dec.Token()
				if err != nil {
					return err
				}
				key := keyToken.(string)
				if seen[key] {
					return fmt.Errorf("duplicate field %q at %s", key, path)
				}
				seen[key] = true
				if err := walk(path + "." + key); err != nil {
					return err
				}
			}
			_, err = dec.Token()
			return err
		case '[':
			for i := 0; dec.More(); i++ {
				if err := walk(fmt.Sprintf("%s[%d]", path, i)); err != nil {
					return err
				}
			}
			_, err = dec.Token()
			return err
		default:
			return fmt.Errorf("unexpected JSON delimiter %q", delim)
		}
	}
	if err := walk("$"); err != nil {
		return err
	}
	return requireJSONEOF(dec)
}

func requireJSONEOF(dec *json.Decoder) error {
	if _, err := dec.Token(); err == io.EOF {
		return nil
	}
	return fmt.Errorf("manifest must contain one JSON object")
}
