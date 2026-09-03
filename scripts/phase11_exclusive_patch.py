#!/usr/bin/env python3
from pathlib import Path


def replace_once(path: str, old: str, new: str) -> None:
    p = Path(path)
    text = p.read_text()
    count = text.count(old)
    if count != 1:
        raise SystemExit(f"{path}: expected exactly one match, found {count}\n--- expected ---\n{old}")
    p.write_text(text.replace(old, new, 1))


# Semantic operation inventory and names.
replace_once(
    "internal/arch/arm64/decoder.go",
    "\tLDAR\n\tSTLR\n\tLDAXR\n\tSTLXR\n\tLDPSW\n",
    "\tLDAR\n\tSTLR\n\tLDXR\n\tLDAXR\n\tSTXR\n\tSTLXR\n\tLDPSW\n",
)
replace_once(
    "internal/arch/arm64/decoder.go",
    "\t\tLDAR: \"LDAR\", STLR: \"STLR\", LDAXR: \"LDAXR\", STLXR: \"STLXR\",\n",
    "\t\tLDAR: \"LDAR\", STLR: \"STLR\", LDXR: \"LDXR\", LDAXR: \"LDAXR\", STXR: \"STXR\", STLXR: \"STLXR\",\n",
)

# Decoder patterns. Generic size handling also covers byte/halfword forms.
replace_once(
    "internal/arch/arm64/decode_ldst.go",
    '''\t// LDAXR: size:001000:0:1:0:11111:1:11111:Rn:Rt
\t{
\t\tName: "LDAXR", Mask: 0x3FFFFC00, Value: 0x085FFC00, Op: LDAXR,
\t\tFields: []FieldDef{{Name: "size", Hi: 31, Lo: 30}, fRn, fRd},
\t\tPost:   postAcqRel,
\t},
\t// STLXR: size:001000:0:0:0:Rs:1:11111:Rn:Rt
\t{
\t\tName: "STLXR", Mask: 0x3FE0FC00, Value: 0x0800FC00, Op: STLXR,
\t\tFields: []FieldDef{{Name: "size", Hi: 31, Lo: 30}, fRm16, fRn, fRd},
\t\tPost:   postAcqRel,
\t},
''',
    '''\t// LDXR: size:001000:0:1:0:11111:0:11111:Rn:Rt
\t{
\t\tName: "LDXR", Mask: 0x3FFFFC00, Value: 0x085F7C00, Op: LDXR,
\t\tFields: []FieldDef{{Name: "size", Hi: 31, Lo: 30}, fRn, fRd},
\t\tPost:   postAcqRel,
\t},
\t// LDAXR: size:001000:0:1:0:11111:1:11111:Rn:Rt
\t{
\t\tName: "LDAXR", Mask: 0x3FFFFC00, Value: 0x085FFC00, Op: LDAXR,
\t\tFields: []FieldDef{{Name: "size", Hi: 31, Lo: 30}, fRn, fRd},
\t\tPost:   postAcqRel,
\t},
\t// STXR: size:001000:0:0:0:Rs:0:11111:Rn:Rt
\t{
\t\tName: "STXR", Mask: 0x3FE0FC00, Value: 0x08007C00, Op: STXR,
\t\tFields: []FieldDef{{Name: "size", Hi: 31, Lo: 30}, fRm16, fRn, fRd},
\t\tPost:   postAcqRel,
\t},
\t// STLXR: size:001000:0:0:0:Rs:1:11111:Rn:Rt
\t{
\t\tName: "STLXR", Mask: 0x3FE0FC00, Value: 0x0800FC00, Op: STLXR,
\t\tFields: []FieldDef{{Name: "size", Hi: 31, Lo: 30}, fRm16, fRn, fRd},
\t\tPost:   postAcqRel,
\t},
''',
)

# Product policy: these instructions must still be consumed only as validated regions.
replace_once(
    "internal/arch/arm64/policy.go",
    "\t// single generated LDAXR...STLXR thunk.\n",
    "\t// single generated LDXR/LDAXR...STXR/STLXR thunk.\n",
)
replace_once(
    "internal/arch/arm64/policy.go",
    "\tclassify(dispositionNativeThunk, WFE, WFI, LDAXR, STLXR)\n",
    "\tclassify(dispositionNativeThunk, WFE, WFI, LDXR, LDAXR, STXR, STLXR)\n",
)

# Translator recognizes either exclusive-load flavor as the start of a closed region.
replace_once(
    "internal/arch/arm64/translator.go",
    "\tif op == LDAXR {\n\t\treturn t.trExclusiveRegion(instructions, idx)\n\t}\n",
    "\tif isExclusiveLoadOp(op) {\n\t\treturn t.trExclusiveRegion(instructions, idx)\n\t}\n",
)

# Generalize region validation/remapping while keeping the body whitelist unchanged.
replace_once(
    "internal/arch/arm64/tr_exclusive.go",
    '''// trExclusiveRegion lowers one complete LDAXR...STLXR sequence to a single
// bytecode operation. The generated runtime executes the exact instruction
// words in one leaf thunk, so no interpreter memory access can break the host
// exclusive monitor between the load and store.
func (t *Translator) trExclusiveRegion(instructions []vm.Instruction, start int) (int, error) {
\tif start < 0 || start >= len(instructions) || Op(instructions[start].Op) != LDAXR {
\t\treturn 0, fmt.Errorf("exclusive region must start with LDAXR")
\t}

\tfirst := instructions[start]
\tif err := validateDecodedExclusiveInstruction(t.decoder, first, LDAXR); err != nil {
''',
    '''// trExclusiveRegion lowers one complete LDXR/LDAXR...STXR/STLXR sequence
// to a single bytecode operation. The generated runtime executes the exact
// instruction words in one leaf thunk, so no interpreter memory access can
// break the host exclusive monitor between the load and store.
func (t *Translator) trExclusiveRegion(instructions []vm.Instruction, start int) (int, error) {
\tif start < 0 || start >= len(instructions) || !isExclusiveLoadOp(Op(instructions[start].Op)) {
\t\treturn 0, fmt.Errorf("exclusive region must start with LDXR or LDAXR")
\t}

\tfirst := instructions[start]
\tfirstOp := Op(first.Op)
\tif err := validateDecodedExclusiveInstruction(t.decoder, first, firstOp); err != nil {
''',
)
replace_once(
    "internal/arch/arm64/tr_exclusive.go",
    '''\t\tif Op(inst.Op) == LDAXR {
\t\t\treturn 0, fmt.Errorf("nested LDAXR is not a closed exclusive region")
\t\t}
\t\tif Op(inst.Op) == STLXR {
\t\t\tend = i
\t\t\tbreak
\t\t}
''',
    '''\t\tif isExclusiveLoadOp(Op(inst.Op)) {
\t\t\treturn 0, fmt.Errorf("nested exclusive load is not a closed exclusive region")
\t\t}
\t\tif isExclusiveStoreOp(Op(inst.Op)) {
\t\t\tend = i
\t\t\tbreak
\t\t}
''',
)
replace_once(
    "internal/arch/arm64/tr_exclusive.go",
    '''\tif end < 0 {
\t\treturn 0, fmt.Errorf("LDAXR has no contiguous STLXR within %d instructions", maxExclusiveRegionInstructions)
\t}

\tlast := instructions[end]
\tif err := validateDecodedExclusiveInstruction(t.decoder, last, STLXR); err != nil {
\t\treturn 0, err
\t}
\tif last.Rn != first.Rn || last.Shift != first.Shift {
\t\treturn 0, fmt.Errorf("STLXR address/width does not match LDAXR")
\t}
''',
    '''\tif end < 0 {
\t\treturn 0, fmt.Errorf("exclusive load has no contiguous STXR or STLXR within %d instructions", maxExclusiveRegionInstructions)
\t}

\tlast := instructions[end]
\tlastOp := Op(last.Op)
\tif err := validateDecodedExclusiveInstruction(t.decoder, last, lastOp); err != nil {
\t\treturn 0, err
\t}
\tif last.Rn != first.Rn || last.Shift != first.Shift {
\t\treturn 0, fmt.Errorf("exclusive store address/width does not match exclusive load")
\t}
''',
)
replace_once(
    "internal/arch/arm64/tr_exclusive.go",
    '''func validateExclusiveRegister(reg int) error {
''',
    '''func isExclusiveLoadOp(op Op) bool {
\treturn op == LDXR || op == LDAXR
}

func isExclusiveStoreOp(op Op) bool {
\treturn op == STXR || op == STLXR
}

func validateExclusiveRegister(reg int) error {
''',
)
replace_once(
    "internal/arch/arm64/tr_exclusive.go",
    '''\tif Op(first.Op) != LDAXR || Op(last.Op) != STLXR {
\t\treturn nil, fmt.Errorf("exclusive region must be bounded by LDAXR and STLXR")
\t}
''',
    '''\tif !isExclusiveLoadOp(Op(first.Op)) || !isExclusiveStoreOp(Op(last.Op)) {
\t\treturn nil, fmt.Errorf("exclusive region must be bounded by LDXR/LDAXR and STXR/STLXR")
\t}
''',
)
replace_once(
    "internal/arch/arm64/tr_exclusive.go",
    '''\tswitch Op(inst.Op) {
\tcase LDAXR:
\t\treturn []exclusiveRegisterField{{register: inst.Rn, shift: 5}, {register: inst.Rd, shift: 0}}
\tcase STLXR:
\t\treturn []exclusiveRegisterField{{register: inst.Rm, shift: 16}, {register: inst.Rn, shift: 5}, {register: inst.Rd, shift: 0}}
''',
    '''\tswitch Op(inst.Op) {
\tcase LDXR, LDAXR:
\t\treturn []exclusiveRegisterField{{register: inst.Rn, shift: 5}, {register: inst.Rd, shift: 0}}
\tcase STXR, STLXR:
\t\treturn []exclusiveRegisterField{{register: inst.Rm, shift: 16}, {register: inst.Rn, shift: 5}, {register: inst.Rd, shift: 0}}
''',
)

# Independent focused regression matrix.
Path("internal/arch/arm64/exclusive_ordering_test.go").write_text(r'''package arm64

import (
	"testing"

	"github.com/vmpacker/internal/vm"
)

func TestDecoderRecognizesNonAcquireReleaseExclusiveForms(t *testing.T) {
	decoder := NewDecoder()
	for _, tc := range []struct {
		raw   uint32
		op    Op
		width int
		rd    int
		rn    int
		rm    int
	}{
		{0x085f7c20, LDXR, 1, 0, 1, -1},
		{0x485f7c20, LDXR, 2, 0, 1, -1},
		{0x885f7c20, LDXR, 4, 0, 1, -1},
		{0xc85f7c20, LDXR, 8, 0, 1, -1},
		{0x08027c20, STXR, 1, 0, 1, 2},
		{0x48027c20, STXR, 2, 0, 1, 2},
		{0x88027c20, STXR, 4, 0, 1, 2},
		{0xc8027c20, STXR, 8, 0, 1, 2},
	} {
		inst := decoder.Decode(tc.raw, 0)
		if got := Op(inst.Op); got != tc.op || inst.Shift != tc.width || inst.Rd != tc.rd || inst.Rn != tc.rn || inst.Rm != tc.rm {
			t.Fatalf("raw=%#08x got=%s width=%d rd=%d rn=%d rm=%d", tc.raw, OpName(got), inst.Shift, inst.Rd, inst.Rn, inst.Rm)
		}
	}
}

func TestExclusiveRegionSupportsAllLoadStoreOrderingPairs(t *testing.T) {
	decoder := NewDecoder()
	for _, tc := range []struct {
		name  string
		load  uint32
		store uint32
	}{
		{"relaxed-relaxed", 0xc85f7c20, 0xc8027c20},
		{"acquire-relaxed", 0xc85ffc20, 0xc8027c20},
		{"relaxed-release", 0xc85f7c20, 0xc802fc20},
		{"acquire-release", 0xc85ffc20, 0xc802fc20},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raws := []uint32{tc.load, 0x91000400, tc.store}
			instructions := make([]vm.Instruction, len(raws))
			for i, raw := range raws {
				instructions[i] = decoder.Decode(raw, i*4)
			}
			result := translateForPhase5(t, instructions)
			if len(result.Unsupported) != 0 || len(result.ExclusiveRegions) != 1 {
				t.Fatalf("unsupported=%v regions=%v", result.Unsupported, result.ExclusiveRegions)
			}
			region := result.ExclusiveRegions[0]
			if !sameInstructionWords(region.Instructions, raws) {
				t.Fatalf("region=%#x want=%#x", region.Instructions, raws)
			}
			patched, _, err := PlanExclusiveThunk(region)
			if err != nil {
				t.Fatal(err)
			}
			if patched[0]&0x00008000 != tc.load&0x00008000 || patched[len(patched)-1]&0x00008000 != tc.store&0x00008000 {
				t.Fatalf("ordering bits changed: patched=%#x original=%#x", patched, raws)
			}
		})
	}
}

func TestExclusiveOrderingExtensionsRemainFailClosed(t *testing.T) {
	decoder := NewDecoder()
	cases := map[string][]uint32{
		"standalone-stxr": {0xc8027c20},
		"nested-mixed":    {0xc85f7c20, 0xc85ffc20, 0xc8027c20},
		"branch-inside":   {0xc85f7c20, 0x14000000, 0xc8027c20},
		"sp-address":      {0xc85f7fe0, 0xc8027fe0},
		"width-mismatch":  {0xc85f7c20, 0x88027c20},
	}
	for name, raws := range cases {
		t.Run(name, func(t *testing.T) {
			var instructions []vm.Instruction
			for i, raw := range raws {
				instructions = append(instructions, decoder.Decode(raw, i*4))
			}
			result := translateForPhase5(t, instructions)
			if len(result.Unsupported) == 0 || len(result.ExclusiveRegions) != 0 {
				t.Fatalf("unsupported=%v regions=%v", result.Unsupported, result.ExclusiveRegions)
			}
		})
	}
}
''')

print("phase11 exclusive ordering patch applied")
