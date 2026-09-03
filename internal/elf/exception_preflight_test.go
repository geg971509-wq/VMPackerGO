package elf

import (
	"strings"
	"testing"

	"github.com/geg971509-wq/VMPackerGO/internal/arch/arm64"
	"github.com/geg971509-wq/VMPackerGO/internal/unwind"
)

func exceptionFixture() (Selection, *arm64.TranslateResult, *unwind.CIE, *unwind.FDE, *unwind.LSDA) {
	personality := uint64(0x7000)
	selection := Selection{Name: "cpp_target", Address: 0x1000, End: 0x1020}
	translation := &arm64.TranslateResult{
		SourceMap: []arm64.SourceMapEntry{
			{ARM64Offset: 0, VMOffset: 0},
			{ARM64Offset: 4, VMOffset: 11},
			{ARM64Offset: 8, VMOffset: 20},
			{ARM64Offset: 16, VMOffset: 28},
			{ARM64Offset: 24, VMOffset: 36},
			{ARM64Offset: 32, VMOffset: 44},
		},
		NativeCallSites: []arm64.NativeCallSite{{ARM64Offset: 4, VMOffset: 11}},
	}
	cie := &unwind.CIE{Offset: 0x10, Personality: &personality, PersonalityEncoding: unwind.PEAbsptr}
	lsdaVA := uint64(0x5000)
	fde := &unwind.FDE{Offset: 0x40, CIEOffset: cie.Offset, InitialLocation: 0x1000, AddressRange: 0x20, LSDA: &lsdaVA}
	lsda := &unwind.LSDA{
		CallSites:    []unwind.CallSite{{Start: 0x1000, Length: 0x10, LandingPad: 0x1018, Action: 1}},
		ActionChains: map[uint64][]unwind.ActionRecord{1: {{Offset: 1, TypeFilter: 0, Next: 0}}},
		TypeInfos:    map[uint64]unwind.TypeInfo{},
		TypeEncoding: unwind.PEOmit,
	}
	return selection, translation, cie, fde, lsda
}

func TestPlanPreparedExceptionBridgeDetectsLocalLandingNativeCall(t *testing.T) {
	selection, translation, cie, fde, lsda := exceptionFixture()
	bridge, err := planPreparedExceptionBridge(selection, translation, cie, fde, lsda)
	if err != nil {
		t.Fatal(err)
	}
	if bridge == nil || bridge.Plan == nil || len(bridge.Plan.Thunks) != 1 {
		t.Fatalf("bridge=%+v", bridge)
	}
	thunk := bridge.Plan.Thunks[0]
	if thunk.OriginalPC != 0x1004 || thunk.OriginalLandingPad != 0x1018 || thunk.VMCallOffset != 11 || thunk.VMLandingPad != 36 {
		t.Fatalf("thunk=%+v", thunk)
	}
	if len(bridge.Routes) != 0 {
		t.Fatalf("planPreparedExceptionBridge must not invent final routes before preparation integration: %+v", bridge.Routes)
	}
}

func TestPlanPreparedExceptionBridgeAllowsUnwindThroughAndCallsOutsideEHRange(t *testing.T) {
	selection, translation, cie, fde, lsda := exceptionFixture()
	lsda.CallSites[0].LandingPad = 0
	bridge, err := planPreparedExceptionBridge(selection, translation, cie, fde, lsda)
	if err != nil || bridge != nil {
		t.Fatalf("unwind-through bridge=%+v err=%v", bridge, err)
	}

	_, translation, cie, fde, lsda = exceptionFixture()
	lsda.CallSites[0] = unwind.CallSite{Start: 0x1010, Length: 0x10, LandingPad: 0x1018, Action: 1}
	bridge, err = planPreparedExceptionBridge(selection, translation, cie, fde, lsda)
	if err != nil || bridge != nil {
		t.Fatalf("outside-range bridge=%+v err=%v", bridge, err)
	}
}

func TestPlanPreparedExceptionBridgeFailsClosedOnMissingLandingMappingOrPersonality(t *testing.T) {
	selection, translation, cie, fde, lsda := exceptionFixture()
	translation.SourceMap = translation.SourceMap[:4]
	translation.SourceMap = append(translation.SourceMap, arm64.SourceMapEntry{ARM64Offset: 32, VMOffset: 44})
	if bridge, err := planPreparedExceptionBridge(selection, translation, cie, fde, lsda); err == nil || bridge != nil || !strings.Contains(err.Error(), "landing") {
		t.Fatalf("missing landing bridge=%+v err=%v", bridge, err)
	}

	selection, translation, _, fde, lsda = exceptionFixture()
	if bridge, err := planPreparedExceptionBridge(selection, translation, &unwind.CIE{}, fde, lsda); err == nil || bridge != nil || !strings.Contains(err.Error(), "personality") {
		t.Fatalf("missing personality bridge=%+v err=%v", bridge, err)
	}
}

func TestFindSelectionFDEFailsClosedOnPartialOrAmbiguousCoverage(t *testing.T) {
	selection := Selection{Name: "target", Address: 0x1000, End: 0x1020}
	frame := &unwind.Frame{FDEs: []unwind.FDE{{Offset: 1, InitialLocation: 0x1000, AddressRange: 0x10}}}
	if fde, err := findSelectionFDE(frame, selection); err == nil || fde != nil || !strings.Contains(err.Error(), "partially") {
		t.Fatalf("partial fde=%+v err=%v", fde, err)
	}

	frame.FDEs = []unwind.FDE{
		{Offset: 1, InitialLocation: 0x1000, AddressRange: 0x20},
		{Offset: 2, InitialLocation: 0x0ff0, AddressRange: 0x40},
	}
	if fde, err := findSelectionFDE(frame, selection); err == nil || fde != nil || !strings.Contains(err.Error(), "multiple") {
		t.Fatalf("ambiguous fde=%+v err=%v", fde, err)
	}

	frame.FDEs = []unwind.FDE{{Offset: 3, InitialLocation: 0x2000, AddressRange: 0x20}}
	fde, err := findSelectionFDE(frame, selection)
	if err != nil || fde != nil {
		t.Fatalf("unrelated fde=%+v err=%v", fde, err)
	}
}

func TestExceptionBridgeRuntimeRequirementsValidateCompletedRoutes(t *testing.T) {
	selection, translation, cie, fde, lsda := exceptionFixture()
	bridge, err := planPreparedExceptionBridge(selection, translation, cie, fde, lsda)
	if err != nil {
		t.Fatal(err)
	}
	bridge.Routes = make([]PreparedExceptionRoute, len(bridge.Plan.Thunks))
	for index, thunk := range bridge.Plan.Thunks {
		bridge.Routes[index] = PreparedExceptionRoute{
			ThunkID: thunk.ID, FinalVMCallOffset: uint32(64 + index*8),
			FinalVMLandingOffset: uint32(32 + index*8),
		}
	}
	preparation := &TranslationPreparation{ExceptionBridges: []PreparedExceptionBridge{*bridge}}
	if err := preparation.ValidateRuntimeRequirements(); err != nil {
		t.Fatalf("completed bridge requirements: %v", err)
	}
	if err := preparation.ValidateRuntimeImage(nil); err == nil || !strings.Contains(err.Error(), "runtime image is required") {
		t.Fatalf("runtime validation err=%v", err)
	}

	incomplete := *bridge
	incomplete.Routes = nil
	if err := (&TranslationPreparation{ExceptionBridges: []PreparedExceptionBridge{incomplete}}).ValidateRuntimeRequirements(); err == nil || !strings.Contains(err.Error(), "final routes") {
		t.Fatalf("incomplete bridge err=%v", err)
	}
	if err := (&TranslationPreparation{}).ValidateRuntimeRequirements(); err != nil {
		t.Fatalf("empty requirements: %v", err)
	}
}
