package elf

import (
	"context"
	"fmt"
	"io"
	"strings"

	vmruntime "github.com/geg971509-wq/VMPackerGO/internal/runtime"
	"github.com/geg971509-wq/VMPackerGO/internal/vm"
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

type Request struct {
	Context      context.Context
	Input        []byte
	Selections   []SelectionRequest
	Mode         string
	Verbose      bool
	Strip        bool
	Debug        bool
	Opcodes      vm.OpcodeMap
	Preparation  *TranslationPreparation
	RuntimeImage *vmruntime.Image
	Log          io.Writer
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

type Result struct {
	Artifact            []byte
	Debug               []byte
	TargetKind          TargetKind
	DevelopmentStrategy string
	OpcodeMapDigest     string
	RuntimeStrategy     string
	Functions           []FunctionFact
	AnalysisLimitations []string
	Warnings            []string
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
	return ProcessAnalyzed(req, analysis)
}

func ProcessAnalyzed(req Request, analysis Analysis) (Result, error) {
	result := Result{
		TargetKind: analysis.TargetKind, AnalysisLimitations: append([]string(nil), analysis.Limitations...),
		Warnings: append([]string(nil), analysis.Warnings...),
	}
	if req.Context != nil {
		if err := req.Context.Err(); err != nil {
			return result, err
		}
	}
	if err := analysis.ValidateInput(req.Input); err != nil {
		return result, err
	}
	if req.RuntimeImage == nil {
		return result, fmt.Errorf("validated runtime image is required")
	}
	if err := req.RuntimeImage.ValidateOpcodeMap(req.Opcodes); err != nil {
		return result, err
	}
	preparation := req.Preparation
	if preparation == nil {
		var err error
		preparation, err = PrepareTranslations(req, analysis)
		if err != nil {
			return result, err
		}
	}
	if err := preparation.ValidateOpcodeMap(req.Opcodes); err != nil {
		return result, err
	}
	if err := preparation.ValidateAnalysis(analysis); err != nil {
		return result, err
	}
	if err := preparation.ValidateRuntimeImage(req.RuntimeImage); err != nil {
		return result, err
	}
	plan, err := buildRewritePlan(req, analysis, preparation)
	if err != nil {
		return result, err
	}
	result.Functions = preparation.FunctionFacts()
	result.OpcodeMapDigest = req.RuntimeImage.OpcodeMapDigest
	result.RuntimeStrategy = "ndk-r29-et-rel-validated"
	result.DevelopmentStrategy = "rewrite-plan-ready"
	if req.Context != nil {
		if err := req.Context.Err(); err != nil {
			return result, err
		}
	}
	artifact, err := applyRewritePlan(req.Input, plan)
	if err != nil {
		return result, fmt.Errorf("apply rewrite plan: %w", err)
	}
	switch analysis.TargetKind {
	case TargetKindAndroidSO, TargetKindAndroidPIE, TargetKindAndroidExec:
		if err := validateAndroidLoadedProgramHeaders(artifact); err != nil {
			return result, fmt.Errorf("validate rewritten Android program headers: %w", err)
		}
	}
	mode := AndroidMode(strings.ToLower(req.Mode))
	if mode == "" {
		mode = AndroidModeAuto
	}
	meta, err := parseELFMetadata(artifact, mode)
	if err != nil {
		return result, fmt.Errorf("validate rewritten ELF: %w", err)
	}
	defer meta.file.Close()
	if meta.kind != analysis.TargetKind {
		return result, fmt.Errorf("rewritten ELF target kind does not match analysis")
	}
	result.DevelopmentStrategy = "rewrite-artifact-ready"
	result.Artifact = artifact
	return result, nil
}
