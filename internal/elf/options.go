package elf

import (
	"context"
	"fmt"
	"io"
	"strings"
)

type TargetKind string

const (
	TargetKindLinuxELF    TargetKind = "linux-elf"
	TargetKindAndroidSO   TargetKind = "android-so"
	TargetKindAndroidPIE  TargetKind = "android-pie"
	TargetKindAndroidExec TargetKind = "android-exec"
)

type AndroidMode string

const (
	AndroidModeAuto   AndroidMode = "auto"
	AndroidModeSO     AndroidMode = "so"
	AndroidModeNative AndroidMode = "native"
)

type InjectorKind string

const (
	InjectorAuto       InjectorKind = "auto"
	InjectorNoteHijack InjectorKind = "note"
	InjectorAddSegment InjectorKind = "add-segment"
)

type Request struct {
	Context    context.Context
	Input      []byte
	Selections []SelectionRequest
	Mode       string
	Verbose    bool
	Strip      bool
	Debug      bool
	InterpBlob []byte
	Log        io.Writer
}

type FunctionFact struct {
	Source       string
	Selector     string
	Name         string
	Address      uint64
	End          uint64
	Size         uint64
	Section      string
	SymbolSource string
	Bytecode     int
	Translated   int
	Instructions int
}

type InjectionFact struct {
	Strategy      InjectorKind
	PhdrIndex     *int
	SegmentSource string
	PayloadOffset uint64
	PayloadVA     uint64
	PayloadSize   uint64
	VMEntryVA     uint64
	TokenEntryVA  uint64
}

type Result struct {
	Artifact            []byte
	Debug               []byte
	TargetKind          TargetKind
	DevelopmentStrategy string
	Functions           []FunctionFact
	AnalysisLimitations []string
	Warnings            []string
	Injection           *InjectionFact
}

func Process(req Request) (Result, error) {
	if req.Context != nil {
		if err := req.Context.Err(); err != nil {
			return Result{}, err
		}
	}
	mode := AndroidMode(strings.ToLower(req.Mode))
	if mode == "" {
		mode = AndroidModeAuto
	}
	switch mode {
	case AndroidModeAuto, AndroidModeSO, AndroidModeNative:
	default:
		return Result{}, fmt.Errorf("unsupported mode %q (supported: auto, so, native)", req.Mode)
	}
	req.Mode = string(mode)
	analysis, err := Analyze(req)
	if err != nil {
		return Result{TargetKind: analysis.TargetKind, AnalysisLimitations: analysis.Limitations, Warnings: analysis.Warnings}, err
	}
	if req.Context != nil {
		if err := req.Context.Err(); err != nil {
			return Result{TargetKind: analysis.TargetKind}, err
		}
	}
	if req.Log == nil {
		req.Log = io.Discard
	}
	p := &Packer{
		selections:   analysis.Selections,
		analysis:     analysis,
		verbose:      req.Verbose,
		stripSymbols: req.Strip,
		debug:        req.Debug,
		targetOS:     "android",
		androidMode:  mode,
		injector:     InjectorAuto,
		interpBlob:   req.InterpBlob,
		out:          req.Log,
	}
	err = p.processBytes(req.Input)
	p.result.Debug = append([]byte(nil), p.debugLog.Bytes()...)
	return p.result, err
}
