package elf

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// TargetKind describes the concrete artifact shape selected from the ELF and
// target flags. It is intentionally finer-grained than targetOS so higher-level
// APK/native workflows can share one packer entry point.
type TargetKind string

const (
	TargetKindLinuxELF    TargetKind = "linux-elf"
	TargetKindAndroidSO   TargetKind = "android-so"
	TargetKindAndroidPIE  TargetKind = "android-pie"
	TargetKindAndroidExec TargetKind = "android-exec"
	TargetKindAPK         TargetKind = "apk"
)

// AndroidMode narrows Android handling when the caller knows whether an input
// should be loaded by an APK linker path or executed directly.
type AndroidMode string

const (
	AndroidModeAuto   AndroidMode = "auto"
	AndroidModeSO     AndroidMode = "so"
	AndroidModeNative AndroidMode = "native"
)

// InjectorKind selects the payload injection strategy. Note-hijack is the
// historical fast path; add-segment handles no-note inputs when a safe PHDR
// slot or growth gap is available.
type InjectorKind string

const (
	InjectorAuto       InjectorKind = "auto"
	InjectorNoteHijack InjectorKind = "note"
	InjectorAddSegment InjectorKind = "add-segment"
)

// ProfileKind captures policy presets for future compatibility/strength
// tuning. The current implementation records the selected profile for reports.
type ProfileKind string

const (
	ProfileCompat   ProfileKind = "compat"
	ProfileBalanced ProfileKind = "balanced"
	ProfileStrong   ProfileKind = "strong"
)

// PackerOptions holds product-facing strategy knobs without expanding the
// historical constructor signature.
type PackerOptions struct {
	AndroidMode string
	Injector    string
	Profile     string
	ReportPath  string
}

// PackReport is a stable JSON summary for smoke tests, APK workflows, and GUI
// integrations. It deliberately records strategy choice and limits rather than
// exposing sensitive original function bytes.
type PackReport struct {
	Input             string           `json:"input"`
	Output            string           `json:"output"`
	TargetOS          string           `json:"target_os"`
	TargetKind        TargetKind       `json:"target_kind"`
	AndroidMode       AndroidMode      `json:"android_mode,omitempty"`
	InjectorRequested InjectorKind     `json:"injector_requested"`
	InjectorSelected  InjectorKind     `json:"injector_selected,omitempty"`
	InjectorReason    string           `json:"injector_reason,omitempty"`
	Profile           ProfileKind      `json:"profile"`
	Functions         []FunctionReport `json:"functions"`
	Injection         *InjectionReport `json:"injection,omitempty"`
	Status            string           `json:"status"`
	Error             string           `json:"error,omitempty"`
	Warnings          []string         `json:"warnings,omitempty"`
}

type FunctionReport struct {
	Name       string `json:"name"`
	Address    uint64 `json:"address"`
	Size       uint64 `json:"size"`
	Section    string `json:"section"`
	Bytecode   int    `json:"bytecode"`
	Translated int    `json:"translated"`
	Total      int    `json:"total"`
}

type InjectionReport struct {
	Strategy      InjectorKind `json:"strategy"`
	NoteIndex     *int         `json:"note_index,omitempty"`
	PhdrIndex     *int         `json:"phdr_index,omitempty"`
	SegmentSource string       `json:"segment_source,omitempty"`
	PayloadOffset uint64       `json:"payload_offset"`
	PayloadVA     uint64       `json:"payload_va"`
	PayloadSize   uint64       `json:"payload_size"`
	VMEntryVA     uint64       `json:"vm_entry_va"`
	TokenEntryVA  uint64       `json:"token_entry_va,omitempty"`
}

func (p *Packer) Configure(opts PackerOptions) error {
	if opts.AndroidMode != "" {
		mode := AndroidMode(strings.ToLower(opts.AndroidMode))
		switch mode {
		case AndroidModeAuto, AndroidModeSO, AndroidModeNative:
			p.androidMode = mode
		default:
			return fmt.Errorf("unsupported android mode %q (supported: auto, so, native)", opts.AndroidMode)
		}
	}
	if opts.Injector != "" {
		injector := InjectorKind(strings.ToLower(opts.Injector))
		switch injector {
		case InjectorAuto, InjectorNoteHijack, InjectorAddSegment:
			p.injector = injector
		default:
			return fmt.Errorf("unsupported injector %q (supported: auto, note, add-segment)", opts.Injector)
		}
	}
	if opts.Profile != "" {
		profile := ProfileKind(strings.ToLower(opts.Profile))
		switch profile {
		case ProfileCompat, ProfileBalanced, ProfileStrong:
			p.profile = profile
		default:
			return fmt.Errorf("unsupported profile %q (supported: compat, balanced, strong)", opts.Profile)
		}
	}
	p.reportPath = opts.ReportPath
	return nil
}

func (p *Packer) initDefaults() {
	if p.androidMode == "" {
		p.androidMode = AndroidModeAuto
	}
	if p.injector == "" {
		p.injector = InjectorAuto
	}
	if p.profile == "" {
		p.profile = ProfileBalanced
	}
}

func (p *Packer) startReport() {
	if p.reportPath == "" {
		return
	}
	p.report = &PackReport{
		Input:             p.inputPath,
		Output:            p.outputPath,
		TargetOS:          p.targetOS,
		AndroidMode:       p.androidMode,
		InjectorRequested: p.injector,
		Profile:           p.profile,
		Status:            "running",
	}
}

func (p *Packer) writeReport() error {
	if p.reportPath == "" || p.report == nil {
		return nil
	}
	if dir := filepath.Dir(p.reportPath); dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
	}
	data, err := json.MarshalIndent(p.report, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(p.reportPath, data, 0644)
}
