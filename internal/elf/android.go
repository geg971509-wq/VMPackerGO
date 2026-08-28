package elf

import (
	"debug/elf"
	"fmt"
)

type elfTargetMetadata struct {
	Kind        TargetKind
	HasDynamic  bool
	HasExecLoad bool
	HasInterp   bool
	HasNote     bool
}

func classifyELFTarget(targetOS string, mode AndroidMode, f *elf.File) (*elfTargetMetadata, error) {
	meta := &elfTargetMetadata{Kind: TargetKindLinuxELF}
	for _, prog := range f.Progs {
		switch prog.Type {
		case elf.PT_DYNAMIC:
			meta.HasDynamic = true
		case elf.PT_INTERP:
			meta.HasInterp = true
		case elf.PT_NOTE:
			meta.HasNote = true
		}
		if prog.Type == elf.PT_LOAD && prog.Flags&elf.PF_X != 0 {
			meta.HasExecLoad = true
		}
	}

	switch targetOS {
	case "", "linux":
		return meta, nil
	case "android":
		if f.Type != elf.ET_DYN && f.Type != elf.ET_EXEC {
			return nil, fmt.Errorf("android target expects an ET_DYN shared object/PIE executable or ET_EXEC native executable, got %s", f.Type)
		}
		if !meta.HasExecLoad {
			return nil, fmt.Errorf("android target requires an executable PT_LOAD segment")
		}

		switch {
		case f.Type == elf.ET_EXEC:
			meta.Kind = TargetKindAndroidExec
		case meta.HasInterp:
			meta.Kind = TargetKindAndroidPIE
		default:
			meta.Kind = TargetKindAndroidSO
		}

		switch mode {
		case AndroidModeSO:
			if meta.Kind != TargetKindAndroidSO || !meta.HasDynamic {
				return nil, fmt.Errorf("android-mode so expects an dynamic-linker-loadable ET_DYN shared object with PT_DYNAMIC, got %s", meta.Kind)
			}
		case AndroidModeNative:
			if meta.Kind == TargetKindAndroidSO {
				return nil, fmt.Errorf("android-mode native expects a PIE/native executable, got an Android shared object")
			}
		case AndroidModeAuto, "":
			// Accept classified target.
		default:
			return nil, fmt.Errorf("unsupported android mode %q", mode)
		}

		if meta.Kind == TargetKindAndroidSO && !meta.HasDynamic {
			return nil, fmt.Errorf("android shared-library target expects PT_DYNAMIC for dynamic-linker loading")
		}
		return meta, nil
	default:
		return nil, fmt.Errorf("unsupported target %q (supported: linux, android)", targetOS)
	}
}

func (p *Packer) validateTargetELF(f *elf.File) (*elfTargetMetadata, error) {
	return classifyELFTarget(p.targetOS, p.androidMode, f)
}

func (p *Packer) selectInjector(meta *elfTargetMetadata) error {
	switch p.injector {
	case "", InjectorAuto:
		if meta.HasNote {
			p.selectedInjector = InjectorNoteHijack
			p.injectorReason = "auto selected note-hijack because PT_NOTE is present"
			return nil
		}
		p.selectedInjector = InjectorAddSegment
		p.injectorReason = "auto selected add-segment because PT_NOTE is absent"
		return nil
	case InjectorNoteHijack:
		if !meta.HasNote {
			return fmt.Errorf("note injection requires a spare PT_NOTE segment; rebuild with a note segment or use the current automatic strategy selection")
		}
		p.selectedInjector = InjectorNoteHijack
		p.injectorReason = "explicit note-hijack injector requested"
		return nil
	case InjectorAddSegment:
		p.selectedInjector = InjectorAddSegment
		p.injectorReason = "explicit add-segment injector requested"
		return nil
	default:
		return fmt.Errorf("unsupported injector %q", p.injector)
	}
}
