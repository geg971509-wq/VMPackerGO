package app

import (
	"context"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"

	elfpacker "github.com/geg971509-wq/VMPackerGO/internal/elf"
	"github.com/geg971509-wq/VMPackerGO/internal/publish"
	"github.com/geg971509-wq/VMPackerGO/internal/report"
	vmruntime "github.com/geg971509-wq/VMPackerGO/internal/runtime"
	"github.com/geg971509-wq/VMPackerGO/internal/vm"
)

type Processor func(elfpacker.Request) (elfpacker.Result, error)
type RuntimeBuilder func(context.Context, vmruntime.BuildConfig) (*vmruntime.Image, error)

type Config struct {
	Version      string
	Commit       string
	Process      Processor
	BuildRuntime RuntimeBuilder
}

type exitError struct {
	code int
	err  error
}

func (e *exitError) Error() string { return e.err.Error() }
func (e *exitError) Unwrap() error { return e.err }

func usageError(err error) error { return &exitError{code: 2, err: err} }

func ExitCode(err error) int {
	if err == nil || errors.Is(err, flag.ErrHelp) {
		return 0
	}
	var exitErr *exitError
	if errors.As(err, &exitErr) {
		return exitErr.code
	}
	return 1
}

func Run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	return RunWithConfig(ctx, args, stdout, stderr, Config{Version: "dev", Commit: "unknown"})
}

func RunWithConfig(ctx context.Context, args []string, stdout, stderr io.Writer, cfg Config) error {
	if ctx == nil {
		ctx = context.Background()
	}
	opts, err := parseOptions(args, stderr)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return err
		}
		return usageError(err)
	}
	if opts.Version {
		fmt.Fprintf(stdout, "vmpacker %s (%s)\n", cfg.Version, cfg.Commit)
		return nil
	}
	if !opts.Info {
		if err := validatePaths(opts); err != nil {
			return usageError(err)
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	input, mode, err := readBoundedInput(opts.Input)
	if err != nil {
		return err
	}
	if opts.Info {
		if err := ctx.Err(); err != nil {
			return err
		}
		return elfpacker.PrintELFInfo(input, opts.Input, opts.Mode, stdout)
	}
	opts.InputMode = mode
	selections := make([]report.Selection, len(opts.Selected))
	for i, selected := range opts.Selected {
		selections[i] = report.Selection{
			Source: selected.Source, Selector: selected.Selector, Name: selected.Name,
			Address: selected.Address, Range: selected.Range, ABI: selected.ABI,
		}
	}
	rep := report.New(cfg.Version, cfg.Commit, opts.Input, opts.Output, opts.Mode, selections)
	if err := ctx.Err(); err != nil {
		return err
	}
	analysisSelections := make([]elfpacker.SelectionRequest, 0, len(opts.Selected))
	funcIndex, addrIndex := 0, 0
	for _, selected := range opts.Selected {
		selection := elfpacker.SelectionRequest{
			Source: selected.Source, Selector: selected.Selector, Name: selected.Name, ABI: selected.ABI,
		}
		if selected.Address != "" || selected.Range != "" {
			spec := opts.AddrSpecs[addrIndex]
			addrIndex++
			selection.AddrSpec = &spec
		} else {
			selection.Name = opts.Funcs[funcIndex]
			funcIndex++
		}
		analysisSelections = append(analysisSelections, selection)
	}
	request := elfpacker.Request{
		Context: ctx, Input: input, Selections: analysisSelections, Mode: opts.Mode,
		Verbose: opts.Verbose, Strip: opts.Strip, Debug: opts.DebugMap != "",
		Log: stdout,
	}
	var result elfpacker.Result
	var transformErr error
	if cfg.Process != nil {
		result, transformErr = cfg.Process(request)
	} else {
		analysis, analysisErr := elfpacker.Analyze(request)
		if analysisErr != nil {
			result = elfpacker.Result{
				TargetKind: analysis.TargetKind, AnalysisLimitations: analysis.Limitations, Warnings: analysis.Warnings,
			}
			transformErr = analysisErr
		} else {
			result = elfpacker.Result{
				TargetKind: analysis.TargetKind, AnalysisLimitations: analysis.Limitations, Warnings: analysis.Warnings,
			}
			entropy, entropyErr := runEntropy(opts.Seed)
			if entropyErr != nil {
				transformErr = entropyErr
			} else {
				opcodes, opcodeErr := vm.NewOpcodeMap(entropy)
				if opcodeErr != nil {
					transformErr = fmt.Errorf("create per-pack opcode map: %w", opcodeErr)
				} else {
					digest, _ := opcodes.Digest()
					result.OpcodeMapDigest = hex.EncodeToString(digest[:])
					request.Opcodes = opcodes
					preparation, prepareErr := elfpacker.PrepareTranslations(request, analysis)
					if prepareErr != nil {
						transformErr = prepareErr
					} else {
						exceptionInvokes, invokeErr := preparation.RuntimeExceptionInvokes()
						if invokeErr != nil {
							transformErr = invokeErr
						} else {
							builder := cfg.BuildRuntime
							if builder == nil {
								builder = vmruntime.Build
							}
							image, buildErr := builder(ctx, vmruntime.BuildConfig{
								NDKDir: opts.NDK, Opcodes: opcodes,
								SVCImmediates:      preparation.SVCImmediates,
								ExclusiveRegions:   preparation.ExclusiveRegions,
								FPSIMDInstructions: preparation.FPSIMDInstructions,
								ExceptionInvokes:   exceptionInvokes,
							})
							if buildErr != nil {
								transformErr = buildErr
							} else {
								request.Preparation = preparation
								request.RuntimeImage = image
								result, transformErr = elfpacker.ProcessAnalyzed(request, analysis)
							}
						}
					}
				}
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if transformErr != nil {
		rep.Fail(transformErr, result)
		if opts.Report != "" {
			data, marshalErr := rep.Marshal()
			if marshalErr != nil {
				return errors.Join(transformErr, fmt.Errorf("encode failed report: %w", marshalErr))
			}
			if err := ctx.Err(); err != nil {
				return errors.Join(transformErr, err)
			}
			if publishErr := publish.All([]publish.File{{Path: opts.Report, Data: data, Mode: 0600}}, opts.Force); publishErr != nil {
				return errors.Join(transformErr, fmt.Errorf("failed report was not published: %w", publishErr))
			}
		}
		return transformErr
	}
	rep.Success(result)
	reportData, err := rep.Marshal()
	if err != nil {
		return fmt.Errorf("encode report: %w", err)
	}
	files := make([]publish.File, 0, 3)
	if opts.DebugMap != "" {
		files = append(files, publish.File{Path: opts.DebugMap, Data: result.Debug, Mode: 0600})
	}
	if opts.Report != "" {
		files = append(files, publish.File{Path: opts.Report, Data: reportData, Mode: 0600})
	}
	files = append(files, publish.File{Path: opts.Output, Data: result.Artifact, Mode: artifactMode(opts.InputMode), Artifact: true})
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := publish.All(files, opts.Force); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "Packed Android ELF: %s\n", opts.Output)
	return nil
}
