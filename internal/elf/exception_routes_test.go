package elf

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
	if err != nil {
		t.Fatal(err)
	}
	result, err := translator.Translate(insts)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Unsupported) != 0 {
		t.Fatalf("unsupported=%v", result.Unsupported)
	}

	source := map[int]int{}
	for _, entry := range result.SourceMap {
		source[entry.ARM64Offset] = entry.VMOffset
	}
	callOld, callOK := source[4]
	landingOld, landingOK := source[8]
	if !callOK || !landingOK {
		t.Fatalf("source map=%v", result.SourceMap)
	}
	plan := &unwind.ExceptionBridgePlan{Thunks: []unwind.InvokeThunk{{
		ID: 0x12345678, OriginalPC: 0x1004, OriginalLandingPad: 0x1008,
		VMCallOffset: uint32(callOld), VMLandingPad: uint32(landingOld),
	}}}
	originalPlan := plan.Thunks[0]
	originalBytecode := append([]byte(nil), result.Bytecode...)
	routes, err := resolveExceptionRoutes(Selection{Name: "f", Address: 0x1000, End: 0x1010}, result, plan, opcodes)
	if err != nil {
		t.Fatal(err)
	}
	if len(routes) != 1 {
		t.Fatalf("routes=%v", routes)
	}
	if routes[0].FinalVMCallOffset == uint32(callOld) && routes[0].FinalVMLandingOffset == uint32(landingOld) {
		t.Fatal("focused fixture did not move either exception route during reversal")
	}
	if !reflect.DeepEqual(plan.Thunks[0], originalPlan) || !reflect.DeepEqual(result.Bytecode, originalBytecode) {
		t.Fatal("route resolution mutated source plan or translation bytecode")
	}

	xorKey := byte(0x5a)
	final, err := finalizePreparedBytecode(result, 0x8000, opcodes, xorKey, 0x01020304)
	if err != nil {
		t.Fatal(err)
	}
	decoded := append([]byte(nil), final...)
	for i := range decoded {
		decoded[i] ^= xorKey
	}
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
	for _, entry := range result.SourceMap {
		source[entry.ARM64Offset] = entry.VMOffset
	}
	base := unwind.InvokeThunk{ID: 7, OriginalPC: 0x1004, OriginalLandingPad: 0x1008, VMCallOffset: uint32(source[4]), VMLandingPad: uint32(source[8])}
	selection := Selection{Name: "f", Address: 0x1000, End: 0x1010}

	cases := map[string][]unwind.InvokeThunk{
		"duplicate-id":       {base, base},
		"bad-call-offset":    {{ID: 8, OriginalPC: base.OriginalPC, OriginalLandingPad: base.OriginalLandingPad, VMCallOffset: base.VMCallOffset + 1, VMLandingPad: base.VMLandingPad}},
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
	for i, raw := range raws {
		insts[i] = decoder.Decode(raw, i*4)
	}
	translator, err := arm64.NewTranslator(0x1000, 16, opcodes)
	if err != nil {
		t.Fatal(err)
	}
	result, err := translator.Translate(insts)
	if err != nil {
		t.Fatal(err)
	}
	return result
}
