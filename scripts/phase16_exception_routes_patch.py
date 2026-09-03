#!/usr/bin/env python3
from pathlib import Path


def replace_once(path: str, old: str, new: str) -> None:
    p = Path(path)
    text = p.read_text()
    count = text.count(old)
    if count != 1:
        raise SystemExit(f"{path}: expected exactly one match, found {count}\n--- expected ---\n{old}")
    p.write_text(text.replace(old, new, 1))


# Prepared bridge gains a writer-facing final-route model without mutating unwind.InvokeThunk.
replace_once(
    "internal/elf/exception_preflight.go",
    '''type PreparedExceptionBridge struct {
\tSelection Selection
\tPlan      *unwind.ExceptionBridgePlan
}
''',
    '''type PreparedExceptionRoute struct {
\tThunkID              uint32
\tOriginalCallPC       uint64
\tOriginalLandingPad   uint64
\tFinalVMCallOffset    uint32
\tFinalVMLandingOffset uint32
}

type PreparedExceptionBridge struct {
\tSelection Selection
\tPlan      *unwind.ExceptionBridgePlan
\tRoutes    []PreparedExceptionRoute
}
''',
)

# Integrate route resolution after Phase 13 preflight has produced deterministic bridge plans.
replace_once(
    "internal/elf/preparation.go",
    '''\texceptionBridges, err := prepareExceptionBridges(req, preparation.Functions)
\tif err != nil {
\t\treturn nil, err
\t}
\tpreparation.ExceptionBridges = exceptionBridges
\treturn preparation, nil
''',
    '''\texceptionBridges, err := prepareExceptionBridges(req, preparation.Functions)
\tif err != nil {
\t\treturn nil, err
\t}
\tif err := resolvePreparedExceptionRoutes(exceptionBridges, preparation.Functions, req.Opcodes); err != nil {
\t\treturn nil, err
\t}
\tpreparation.ExceptionBridges = exceptionBridges
\treturn preparation, nil
''',
)

Path("internal/elf/exception_routes.go").write_text(r'''package elf

import (
    "fmt"
    "math"

    "github.com/vmpacker/internal/arch/arm64"
    "github.com/vmpacker/internal/unwind"
    "github.com/vmpacker/internal/vm"
)

func resolvePreparedExceptionRoutes(bridges []PreparedExceptionBridge, functions []PreparedFunction, opcodes vm.OpcodeMap) error {
    if len(bridges) == 0 {
        return nil
    }
    functionBySelection := make(map[string]*arm64.TranslateResult, len(functions))
    for i := range functions {
        function := &functions[i]
        key := exceptionSelectionKey(function.Selection)
        if _, exists := functionBySelection[key]; exists {
            return fmt.Errorf("duplicate prepared function selection for exception routing")
        }
        functionBySelection[key] = function.Translation
    }
    for i := range bridges {
        bridge := &bridges[i]
        translation, ok := functionBySelection[exceptionSelectionKey(bridge.Selection)]
        if !ok || translation == nil {
            return fmt.Errorf("function %q exception route has no prepared translation", bridge.Selection.Name)
        }
        routes, err := resolveExceptionRoutes(bridge.Selection, translation, bridge.Plan, opcodes)
        if err != nil {
            return fmt.Errorf("function %q final exception routes: %w", bridge.Selection.Name, err)
        }
        bridge.Routes = routes
    }
    return nil
}

func exceptionSelectionKey(selection Selection) string {
    return fmt.Sprintf("%s\x00%d\x00%d\x00%s", selection.Name, selection.Address, selection.End, selection.Section)
}

func resolveExceptionRoutes(selection Selection, translation *arm64.TranslateResult, plan *unwind.ExceptionBridgePlan, opcodes vm.OpcodeMap) ([]PreparedExceptionRoute, error) {
    if translation == nil || plan == nil {
        return nil, fmt.Errorf("translation and exception bridge plan are required")
    }
    if len(plan.Thunks) == 0 {
        return nil, fmt.Errorf("exception bridge plan has no invoke thunks")
    }
    if _, err := validateBytecodeTrailer(translation.Bytecode, translation.CodeLen); err != nil {
        return nil, fmt.Errorf("validate translated bytecode trailer: %w", err)
    }
    _, offsetMap, err := reverseInstructions(translation.Bytecode, translation.CodeLen, opcodes)
    if err != nil {
        return nil, fmt.Errorf("build final bytecode offset map: %w", err)
    }
    sourceVM, err := exceptionSourceVMOffsets(selection, translation.SourceMap)
    if err != nil {
        return nil, err
    }

    routes := make([]PreparedExceptionRoute, 0, len(plan.Thunks))
    seenIDs := make(map[uint32]struct{}, len(plan.Thunks))
    for _, thunk := range plan.Thunks {
        if thunk.ID == 0 {
            return nil, fmt.Errorf("invoke thunk has zero identifier")
        }
        if _, duplicate := seenIDs[thunk.ID]; duplicate {
            return nil, fmt.Errorf("duplicate invoke thunk identifier 0x%08x", thunk.ID)
        }
        seenIDs[thunk.ID] = struct{}{}
        if thunk.OriginalPC < selection.Address || thunk.OriginalPC >= selection.End {
            return nil, fmt.Errorf("invoke thunk 0x%08x call PC 0x%x is outside selected function", thunk.ID, thunk.OriginalPC)
        }
        if thunk.OriginalLandingPad < selection.Address || thunk.OriginalLandingPad >= selection.End {
            return nil, fmt.Errorf("invoke thunk 0x%08x landing PC 0x%x is outside selected function", thunk.ID, thunk.OriginalLandingPad)
        }
        callOld, ok := sourceVM[thunk.OriginalPC]
        if !ok || callOld != thunk.VMCallOffset {
            return nil, fmt.Errorf("invoke thunk 0x%08x call offset does not match translation source map", thunk.ID)
        }
        landingOld, ok := sourceVM[thunk.OriginalLandingPad]
        if !ok || landingOld != thunk.VMLandingPad {
            return nil, fmt.Errorf("invoke thunk 0x%08x landing offset does not match translation source map", thunk.ID)
        }
        callFinal, ok := offsetMap[int(callOld)]
        if !ok || callFinal < 0 || uint64(callFinal) > math.MaxUint32 {
            return nil, fmt.Errorf("invoke thunk 0x%08x call offset is not a final VM boundary", thunk.ID)
        }
        landingFinal, ok := offsetMap[int(landingOld)]
        if !ok || landingFinal < 0 || uint64(landingFinal) > math.MaxUint32 {
            return nil, fmt.Errorf("invoke thunk 0x%08x landing offset is not a final VM boundary", thunk.ID)
        }
        routes = append(routes, PreparedExceptionRoute{
            ThunkID: thunk.ID,
            OriginalCallPC: thunk.OriginalPC,
            OriginalLandingPad: thunk.OriginalLandingPad,
            FinalVMCallOffset: uint32(callFinal),
            FinalVMLandingOffset: uint32(landingFinal),
        })
    }
    return routes, nil
}

func exceptionSourceVMOffsets(selection Selection, entries []arm64.SourceMapEntry) (map[uint64]uint32, error) {
    if len(entries) == 0 {
        return nil, fmt.Errorf("translation source map is empty")
    }
    result := make(map[uint64]uint32, len(entries))
    previousOffset := -1
    for _, entry := range entries {
        if entry.ARM64Offset < 0 || entry.VMOffset < 0 || uint64(entry.VMOffset) > math.MaxUint32 {
            return nil, fmt.Errorf("translation source map contains an invalid entry %+v", entry)
        }
        if entry.ARM64Offset <= previousOffset || uint64(entry.ARM64Offset) > selection.Size() {
            return nil, fmt.Errorf("translation source map is not strictly valid for selected function")
        }
        pc, ok := checkedAdd(selection.Address, uint64(entry.ARM64Offset))
        if !ok {
            return nil, fmt.Errorf("translation source PC overflows")
        }
        if _, duplicate := result[pc]; duplicate {
            return nil, fmt.Errorf("duplicate translation source PC 0x%x", pc)
        }
        result[pc] = uint32(entry.VMOffset)
        previousOffset = entry.ARM64Offset
    }
    return result, nil
}
''')

Path("internal/elf/exception_routes_test.go").write_text(r'''package elf

import (
    "encoding/binary"
    "reflect"
    "testing"

    "github.com/vmpacker/internal/arch/arm64"
    "github.com/vmpacker/internal/unwind"
    "github.com/vmpacker/internal/vm"
)

func TestResolveExceptionRoutesMatchesFinalReversedTrailer(t *testing.T) {
    opcodes := vm.IdentityOpcodeMap()
    decoder := arm64.NewDecoder()
    raws := []uint32{
        0xD503201F, // nop
        0x94000002, // bl +8
        0xD503201F, // landing identity for this focused route test
        0xD503201F,
    }
    insts := make([]vm.Instruction, len(raws))
    for i, raw := range raws {
        insts[i] = decoder.Decode(raw, i*4)
    }
    translator, err := arm64.NewTranslator(0x1000, len(raws)*4, opcodes)
    if err != nil { t.Fatal(err) }
    result, err := translator.Translate(insts)
    if err != nil { t.Fatal(err) }
    if len(result.Unsupported) != 0 { t.Fatalf("unsupported=%v", result.Unsupported) }

    source := map[int]int{}
    for _, entry := range result.SourceMap { source[entry.ARM64Offset] = entry.VMOffset }
    callOld, callOK := source[4]
    landingOld, landingOK := source[8]
    if !callOK || !landingOK { t.Fatalf("source map=%v", result.SourceMap) }
    plan := &unwind.ExceptionBridgePlan{Thunks: []unwind.InvokeThunk{{
        ID: 0x12345678, OriginalPC: 0x1004, OriginalLandingPad: 0x1008,
        VMCallOffset: uint32(callOld), VMLandingPad: uint32(landingOld),
    }}}
    originalPlan := plan.Thunks[0]
    originalBytecode := append([]byte(nil), result.Bytecode...)
    routes, err := resolveExceptionRoutes(Selection{Name: "f", Address: 0x1000, End: 0x1010}, result, plan, opcodes)
    if err != nil { t.Fatal(err) }
    if len(routes) != 1 { t.Fatalf("routes=%v", routes) }
    if routes[0].FinalVMCallOffset == uint32(callOld) && routes[0].FinalVMLandingOffset == uint32(landingOld) {
        t.Fatal("focused fixture did not move either exception route during reversal")
    }
    if plan.Thunks[0] != originalPlan || !reflect.DeepEqual(result.Bytecode, originalBytecode) {
        t.Fatal("route resolution mutated source plan or translation bytecode")
    }

    xorKey := byte(0x5a)
    final, err := finalizePreparedBytecode(result, 0x8000, opcodes, xorKey, 0x01020304)
    if err != nil { t.Fatal(err) }
    decoded := append([]byte(nil), final...)
    for i := range decoded { decoded[i] ^= xorKey }
    mapCount := binary.LittleEndian.Uint32(decoded[len(decoded)-16:])
    trailerLen := int(mapCount)*8 + 21
    codeLen := len(decoded) - trailerLen
    entries := map[uint32]uint32{}
    for i := 0; i < int(mapCount); i++ {
        off := codeLen + i*8
        entries[binary.LittleEndian.Uint32(decoded[off:])] = binary.LittleEndian.Uint32(decoded[off+4:])
    }
    if got := entries[4]; got != routes[0].FinalVMCallOffset {
        t.Fatalf("final call trailer=%d route=%d", got, routes[0].FinalVMCallOffset)
    }
    if got := entries[8]; got != routes[0].FinalVMLandingOffset {
        t.Fatalf("final landing trailer=%d route=%d", got, routes[0].FinalVMLandingOffset)
    }
}

func TestResolveExceptionRoutesFailsClosedOnProvenanceErrors(t *testing.T) {
    opcodes := vm.IdentityOpcodeMap()
    result := translateForExceptionRouteTest(t, opcodes)
    source := map[int]int{}
    for _, entry := range result.SourceMap { source[entry.ARM64Offset] = entry.VMOffset }
    base := unwind.InvokeThunk{ID: 7, OriginalPC: 0x1004, OriginalLandingPad: 0x1008, VMCallOffset: uint32(source[4]), VMLandingPad: uint32(source[8])}
    selection := Selection{Name: "f", Address: 0x1000, End: 0x1010}

    cases := map[string][]unwind.InvokeThunk{
        "duplicate-id": {base, base},
        "bad-call-offset": {{ID: 8, OriginalPC: base.OriginalPC, OriginalLandingPad: base.OriginalLandingPad, VMCallOffset: base.VMCallOffset + 1, VMLandingPad: base.VMLandingPad}},
        "missing-landing-pc": {{ID: 9, OriginalPC: base.OriginalPC, OriginalLandingPad: 0x100c, VMCallOffset: base.VMCallOffset, VMLandingPad: base.VMLandingPad}},
    }
    for name, thunks := range cases {
        t.Run(name, func(t *testing.T) {
            if _, err := resolveExceptionRoutes(selection, result, &unwind.ExceptionBridgePlan{Thunks: thunks}, opcodes); err == nil {
                t.Fatal("invalid exception route provenance was accepted")
            }
        })
    }
}

func translateForExceptionRouteTest(t *testing.T, opcodes vm.OpcodeMap) *arm64.TranslateResult {
    t.Helper()
    decoder := arm64.NewDecoder()
    raws := []uint32{0xD503201F, 0x94000002, 0xD503201F, 0xD503201F}
    insts := make([]vm.Instruction, len(raws))
    for i, raw := range raws { insts[i] = decoder.Decode(raw, i*4) }
    translator, err := arm64.NewTranslator(0x1000, 16, opcodes)
    if err != nil { t.Fatal(err) }
    result, err := translator.Translate(insts)
    if err != nil { t.Fatal(err) }
    return result
}
''')

# Extend the existing Phase 13 focused fixture to assert that route materialization is wired into preparation.
replace_once(
    "internal/elf/exception_preflight_test.go",
    '''\tthunk := bridge.Plan.Thunks[0]
\tif thunk.OriginalPC != 0x1004 || thunk.OriginalLandingPad != 0x1018 || thunk.VMCallOffset != 11 || thunk.VMLandingPad != 36 {
\t\tt.Fatalf("thunk=%+v", thunk)
\t}
''',
    '''\tthunk := bridge.Plan.Thunks[0]
\tif thunk.OriginalPC != 0x1004 || thunk.OriginalLandingPad != 0x1018 || thunk.VMCallOffset != 11 || thunk.VMLandingPad != 36 {
\t\tt.Fatalf("thunk=%+v", thunk)
\t}
\tif len(bridge.Routes) != 0 {
\t\tt.Fatalf("planPreparedExceptionBridge must not invent final routes before preparation integration: %+v", bridge.Routes)
\t}
''',
)

print("phase16 exception final-route patch applied")
