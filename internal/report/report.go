package report

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/vmpacker/internal/abi"
	elfpacker "github.com/vmpacker/internal/elf"
)

const SchemaVersion = 1

type Tool struct {
	Version string `json:"version"`
	Commit  string `json:"commit"`
}

type ABI struct {
	Params []string `json:"params"`
	Result string   `json:"result"`
}

type Function struct {
	Source       string `json:"source"`
	Selector     string `json:"selector"`
	Name         string `json:"name,omitempty"`
	Address      string `json:"address,omitempty"`
	Range        string `json:"range,omitempty"`
	ABI          ABI    `json:"abi"`
	Section      string `json:"section,omitempty"`
	SymbolSource string `json:"symbol_source,omitempty"`
	Size         uint64 `json:"size,omitempty"`
	Instructions int    `json:"instructions"`
	Translated   int    `json:"translated"`
	Bytecode     int    `json:"bytecode_bytes"`
}

type Report struct {
	SchemaVersion       int        `json:"schema_version"`
	Tool                Tool       `json:"tool"`
	Input               string     `json:"input"`
	Output              string     `json:"output"`
	Mode                string     `json:"mode"`
	TargetKind          string     `json:"target_kind,omitempty"`
	DevelopmentStrategy string     `json:"development_strategy,omitempty"`
	OpcodeMapDigest     string     `json:"opcode_map_digest,omitempty"`
	RuntimeStrategy     string     `json:"runtime_strategy,omitempty"`
	SegmentStrategy     string     `json:"segment_strategy,omitempty"`
	VeneerStrategy      string     `json:"veneer_strategy,omitempty"`
	UnwindStrategy      string     `json:"unwind_strategy,omitempty"`
	Functions           []Function `json:"functions"`
	OutputSHA256        string     `json:"output_sha256,omitempty"`
	Status              string     `json:"status"`
	Error               string     `json:"error,omitempty"`
	ReleaseReady        bool       `json:"release_ready"`
	Limitations         []string   `json:"limitations"`
	Warnings            []string   `json:"warnings,omitempty"`
}

type Selection struct {
	Source   string
	Selector string
	Name     string
	Address  string
	Range    string
	ABI      abi.Signature
}

func New(version, commit, input, output, mode string, selections []Selection) Report {
	r := Report{
		SchemaVersion: SchemaVersion,
		Tool:          Tool{Version: version, Commit: commit},
		Input:         input,
		Output:        output,
		Mode:          mode,
		Functions:     make([]Function, 0, len(selections)),
		Status:        "failed",
		ReleaseReady:  false,
		Limitations:   []string{"development runtime and ELF rewriting are not release-ready"},
	}
	for _, selection := range selections {
		params := make([]string, len(selection.ABI.Params))
		for i, param := range selection.ABI.Params {
			params[i] = string(param)
		}
		r.Functions = append(r.Functions, Function{
			Source: selection.Source, Selector: selection.Selector, Name: selection.Name,
			Address: selection.Address, Range: selection.Range,
			ABI: ABI{Params: params, Result: selection.ABI.ResultName()},
		})
	}
	return r
}

func (r *Report) Success(result elfpacker.Result) {
	r.TargetKind = string(result.TargetKind)
	r.DevelopmentStrategy = result.DevelopmentStrategy
	r.OpcodeMapDigest = result.OpcodeMapDigest
	r.RuntimeStrategy = result.RuntimeStrategy
	r.Limitations = append(r.Limitations, result.AnalysisLimitations...)
	r.Warnings = append([]string(nil), result.Warnings...)
	r.mergeFunctions(result.Functions)
	sum := sha256.Sum256(result.Artifact)
	r.OutputSHA256 = hex.EncodeToString(sum[:])
	r.Status = "ok"
	r.Error = ""
}

func (r *Report) mergeFunctions(facts []elfpacker.FunctionFact) {
	for i := range r.Functions {
		fact := findFact(r.Functions[i], facts)
		if fact == nil {
			continue
		}
		r.Functions[i].Source = fact.Source
		r.Functions[i].Name = fact.Name
		end := fact.End
		if end == 0 {
			end = fact.Address + fact.Size
		}
		r.Functions[i].Address = fmt.Sprintf("0x%x", fact.Address)
		if end > fact.Address {
			r.Functions[i].Range = fmt.Sprintf("0x%x-0x%x", fact.Address, end)
		}
		r.Functions[i].Section = fact.Section
		r.Functions[i].SymbolSource = fact.SymbolSource
		r.Functions[i].Size = fact.Size
		r.Functions[i].Instructions = fact.Instructions
		r.Functions[i].Translated = fact.Translated
		r.Functions[i].Bytecode = fact.Bytecode
	}
}

func (r *Report) Fail(err error, result elfpacker.Result) {
	if result.TargetKind != "" {
		r.TargetKind = string(result.TargetKind)
	}
	if result.DevelopmentStrategy != "" {
		r.DevelopmentStrategy = result.DevelopmentStrategy
	}
	if result.OpcodeMapDigest != "" {
		r.OpcodeMapDigest = result.OpcodeMapDigest
	}
	if result.RuntimeStrategy != "" {
		r.RuntimeStrategy = result.RuntimeStrategy
	}
	if len(result.AnalysisLimitations) != 0 {
		r.Limitations = append(r.Limitations, result.AnalysisLimitations...)
	}
	r.Warnings = append([]string(nil), result.Warnings...)
	r.mergeFunctions(result.Functions)
	r.Status = "failed"
	r.Error = err.Error()
	r.OutputSHA256 = ""
}

func (r Report) Marshal() ([]byte, error) {
	if r.Functions == nil {
		r.Functions = []Function{}
	}
	if r.Limitations == nil {
		r.Limitations = []string{}
	}
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

func findFact(fn Function, facts []elfpacker.FunctionFact) *elfpacker.FunctionFact {
	var address uint64
	addressParsed := false
	selector := fn.Address
	if selector == "" {
		selector = fn.Range
	}
	if selector != "" {
		if spec, err := elfpacker.ParseAddrSpec(selector); err == nil {
			address = spec.Addr
			addressParsed = true
		}
	}
	for i := range facts {
		if addressParsed && facts[i].Address == address {
			return &facts[i]
		}
		if fn.Address == "" && fn.Range == "" && facts[i].Name == fn.Name {
			return &facts[i]
		}
	}
	return nil
}
