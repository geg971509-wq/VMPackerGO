#!/usr/bin/env python3
from pathlib import Path


def replace_once(path: str, old: str, new: str) -> None:
    p = Path(path)
    text = p.read_text()
    count = text.count(old)
    if count != 1:
        raise SystemExit(f"{path}: expected exactly one match, found {count}\n--- expected ---\n{old}")
    p.write_text(text.replace(old, new, 1))


# ---------------------------------------------------------------------------
# 1. Shared decoded IR: add an explicit Rt2 field. Do not overload Imm/Shift.
# ---------------------------------------------------------------------------
replace_once(
    "internal/vm/types.go",
    '''\tRd        int
\tRn        int
\tRm        int
\tImm       int64
''',
    '''\tRd        int
\tRn        int
\tRm        int
\tRt2       int
\tImm       int64
''',
)
replace_once(
    "internal/vm/types.go",
    '''// ExclusiveRegion is a complete, contiguous LDAXR...STLXR sequence that must
// execute without returning to the interpreter. ID is derived from the exact
''',
    '''// ExclusiveRegion is a complete, contiguous load-exclusive...store-exclusive
// sequence that must execute without returning to the interpreter. ID is derived from the exact
''',
)

replace_once(
    "internal/arch/arm64/decode_fields.go",
    '''// 约定: Rd→inst.Rd, Rn→inst.Rn, Rm→inst.Rm, sf→inst.SF,
''',
    '''// 约定: Rd→inst.Rd, Rn→inst.Rn, Rm→inst.Rm, Rt2→inst.Rt2, sf→inst.SF,
''',
)
replace_once(
    "internal/arch/arm64/decode_fields.go",
    '''\tif v, ok := fields["Rm"]; ok {
\t\tinst.Rm = int(v)
\t}
\tif v, ok := fields["sf"]; ok {
''',
    '''\tif v, ok := fields["Rm"]; ok {
\t\tinst.Rm = int(v)
\t}
\tif v, ok := fields["Rt2"]; ok {
\t\tinst.Rt2 = int(v)
\t}
\tif v, ok := fields["sf"]; ok {
''',
)

# ---------------------------------------------------------------------------
# 2. Semantic op inventory and decoder state initialization.
# ---------------------------------------------------------------------------
replace_once(
    "internal/arch/arm64/decoder.go",
    '''\tLDXR
\tLDAXR
\tSTXR
\tSTLXR
\tLDPSW
''',
    '''\tLDXR
\tLDAXR
\tLDXP
\tLDAXP
\tSTXR
\tSTLXR
\tSTXP
\tSTLXP
\tLDPSW
''',
)
replace_once(
    "internal/arch/arm64/decoder.go",
    '''\tinst := vm.Instruction{Raw: raw, Op: int(UNKNOWN), Offset: offset, Rd: -1, Rn: -1, Rm: -1}
''',
    '''\tinst := vm.Instruction{Raw: raw, Op: int(UNKNOWN), Offset: offset, Rd: -1, Rn: -1, Rm: -1, Rt2: -1}
''',
)
replace_once(
    "internal/arch/arm64/decoder.go",
    '''\t\tLDAR: "LDAR", STLR: "STLR", LDXR: "LDXR", LDAXR: "LDAXR", STXR: "STXR", STLXR: "STLXR",
''',
    '''\t\tLDAR: "LDAR", STLR: "STLR", LDXR: "LDXR", LDAXR: "LDAXR", LDXP: "LDXP", LDAXP: "LDAXP",
\t\tSTXR: "STXR", STLXR: "STLXR", STXP: "STXP", STLXP: "STLXP",
''',
)

# ---------------------------------------------------------------------------
# 3. Pair-exclusive decoder patterns and post-processing.
# ---------------------------------------------------------------------------
replace_once(
    "internal/arch/arm64/decode_ldst.go",
    '''\t// STLXR: size:001000:0:0:0:Rs:1:11111:Rn:Rt
\t{
\t\tName: "STLXR", Mask: 0x3FE0FC00, Value: 0x0800FC00, Op: STLXR,
\t\tFields: []FieldDef{{Name: "size", Hi: 31, Lo: 30}, fRm16, fRn, fRd},
\t\tPost:   postAcqRel,
\t},

\t// ================================================================
\t// LDADD (atomic add, LSE / ARMv8.1)
''',
    '''\t// STLXR: size:001000:0:0:0:Rs:1:11111:Rn:Rt
\t{
\t\tName: "STLXR", Mask: 0x3FE0FC00, Value: 0x0800FC00, Op: STLXR,
\t\tFields: []FieldDef{{Name: "size", Hi: 31, Lo: 30}, fRm16, fRn, fRd},
\t\tPost:   postAcqRel,
\t},
\t// LDXP: 1:sz:001000:0:1:0:11111:0:Rt2:Rn:Rt
\t{
\t\tName: "LDXP", Mask: 0xBFFF8000, Value: 0x887F0000, Op: LDXP,
\t\tFields: []FieldDef{{Name: "sf", Hi: 30, Lo: 30}, {Name: "Rt2", Hi: 14, Lo: 10}, fRn, fRd},
\t\tPost:   postExclusivePair,
\t},
\t// LDAXP: 1:sz:001000:0:1:0:11111:1:Rt2:Rn:Rt
\t{
\t\tName: "LDAXP", Mask: 0xBFFF8000, Value: 0x887F8000, Op: LDAXP,
\t\tFields: []FieldDef{{Name: "sf", Hi: 30, Lo: 30}, {Name: "Rt2", Hi: 14, Lo: 10}, fRn, fRd},
\t\tPost:   postExclusivePair,
\t},
\t// STXP: 1:sz:001000:0:0:0:Rs:0:Rt2:Rn:Rt
\t{
\t\tName: "STXP", Mask: 0xBFE08000, Value: 0x88200000, Op: STXP,
\t\tFields: []FieldDef{{Name: "sf", Hi: 30, Lo: 30}, fRm16, {Name: "Rt2", Hi: 14, Lo: 10}, fRn, fRd},
\t\tPost:   postExclusivePair,
\t},
\t// STLXP: 1:sz:001000:0:0:0:Rs:1:Rt2:Rn:Rt
\t{
\t\tName: "STLXP", Mask: 0xBFE08000, Value: 0x88208000, Op: STLXP,
\t\tFields: []FieldDef{{Name: "sf", Hi: 30, Lo: 30}, fRm16, {Name: "Rt2", Hi: 14, Lo: 10}, fRn, fRd},
\t\tPost:   postExclusivePair,
\t},

\t// ================================================================
\t// LDADD (atomic add, LSE / ARMv8.1)
''',
)
replace_once(
    "internal/arch/arm64/decode_ldst.go",
    '''// postAcqRel Acquire/Release load/store 后处理
// size[31:30]: 00=1B, 01=2B, 10=4B, 11=8B → inst.Shift = access bytes
func postAcqRel(f map[string]int64, inst *vm.Instruction) {
''',
    '''// postExclusivePair handles LDXP/LDAXP/STXP/STLXP. Bit30 selects the
// element width: 0 = two 32-bit GPRs, 1 = two 64-bit GPRs.
func postExclusivePair(f map[string]int64, inst *vm.Instruction) {
\tinst.SF = f["sf"] != 0
\tif inst.SF {
\t\tinst.Shift = 8
\t} else {
\t\tinst.Shift = 4
\t}
\txzrReplace(&inst.Rd)
\txzrReplace(&inst.Rt2)
\tif inst.Rm >= 0 {
\t\txzrReplace(&inst.Rm)
\t}
}

// postAcqRel Acquire/Release load/store 后处理
// size[31:30]: 00=1B, 01=2B, 10=4B, 11=8B → inst.Shift = access bytes
func postAcqRel(f map[string]int64, inst *vm.Instruction) {
''',
)

# ---------------------------------------------------------------------------
# 4. Product policy: pair exclusives are native-thunk-only.
# ---------------------------------------------------------------------------
replace_once(
    "internal/arch/arm64/policy.go",
    '''\t// single generated LDXR/LDAXR...STXR/STLXR thunk.
''',
    '''\t// single generated scalar/pair load-exclusive...store-exclusive thunk.
''',
)
replace_once(
    "internal/arch/arm64/policy.go",
    '''\tclassify(dispositionNativeThunk, WFE, WFI, LDXR, LDAXR, STXR, STLXR)
''',
    '''\tclassify(dispositionNativeThunk, WFE, WFI, LDXR, LDAXR, LDXP, LDAXP, STXR, STLXR, STXP, STLXP)
''',
)

# ---------------------------------------------------------------------------
# 5. Exclusive-region validation and register remapping.
# ---------------------------------------------------------------------------
replace_once(
    "internal/arch/arm64/tr_exclusive.go",
    '''// trExclusiveRegion lowers one complete LDXR/LDAXR...STXR/STLXR sequence
// to a single bytecode operation. The generated runtime executes the exact
''',
    '''// trExclusiveRegion lowers one complete scalar/pair load-exclusive...
// store-exclusive sequence to a single bytecode operation. The generated runtime executes the exact
''',
)
replace_once(
    "internal/arch/arm64/tr_exclusive.go",
    '''\tif start < 0 || start >= len(instructions) || !isExclusiveLoadOp(Op(instructions[start].Op)) {
\t\treturn 0, fmt.Errorf("exclusive region must start with LDXR or LDAXR")
\t}
''',
    '''\tif start < 0 || start >= len(instructions) || !isExclusiveLoadOp(Op(instructions[start].Op)) {
\t\treturn 0, fmt.Errorf("exclusive region must start with a supported load-exclusive instruction")
\t}
''',
)
replace_once(
    "internal/arch/arm64/tr_exclusive.go",
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
\tfor name, reg := range map[string]int{"store value": last.Rd, "status": last.Rm} {
\t\tif err := validateExclusiveRegister(reg); err != nil {
\t\t\treturn 0, fmt.Errorf("exclusive %s: %w", name, err)
\t\t}
\t}
''',
    '''\tif end < 0 {
\t\treturn 0, fmt.Errorf("exclusive load has no contiguous supported store-exclusive within %d instructions", maxExclusiveRegionInstructions)
\t}

\tlast := instructions[end]
\tlastOp := Op(last.Op)
\tif err := validateDecodedExclusiveInstruction(t.decoder, last, lastOp); err != nil {
\t\treturn 0, err
\t}
\tif err := validateExclusiveBoundary(first, last); err != nil {
\t\treturn 0, err
\t}
''',
)
replace_once(
    "internal/arch/arm64/tr_exclusive.go",
    '''func validateDecodedExclusiveInstruction(decoder *Decoder, inst vm.Instruction, want Op) error {
\tdecoded := decoder.Decode(inst.Raw, inst.Offset)
\tif Op(decoded.Op) != want || decoded.Rd != inst.Rd || decoded.Rn != inst.Rn ||
\t\tdecoded.Rm != inst.Rm || decoded.Shift != inst.Shift {
\t\treturn fmt.Errorf("%s fields do not match raw encoding 0x%08x", OpName(want), inst.Raw)
\t}
\treturn nil
}
''',
    '''func validateDecodedExclusiveInstruction(decoder *Decoder, inst vm.Instruction, want Op) error {
\tdecoded := decoder.Decode(inst.Raw, inst.Offset)
\tif Op(decoded.Op) != want || decoded.Rd != inst.Rd || decoded.Rn != inst.Rn ||
\t\tdecoded.Rm != inst.Rm || decoded.Shift != inst.Shift ||
\t\t(exclusiveRegisterArity(want) == 2 && decoded.Rt2 != inst.Rt2) {
\t\treturn fmt.Errorf("%s fields do not match raw encoding 0x%08x", OpName(want), inst.Raw)
\t}
\treturn nil
}
''',
)
replace_once(
    "internal/arch/arm64/tr_exclusive.go",
    '''\tvar registers []int
\tswitch Op(inst.Op) {
\tcase ADD_IMM, SUB_IMM:
\t\tregisters = []int{inst.Rd, inst.Rn}
\tcase ADD_REG, SUB_REG, AND_REG, ORR_REG, EOR_REG, MUL:
\t\tregisters = []int{inst.Rd, inst.Rn, inst.Rm}
\tdefault:
''',
    '''\tvar registers []int
\tswitch Op(inst.Op) {
\tcase ADD_IMM, SUB_IMM, SUBS_IMM:
\t\tregisters = []int{inst.Rd, inst.Rn}
\tcase ADD_REG, SUB_REG, SUBS_REG, AND_REG, ORR_REG, EOR_REG, MUL,
\t\tCSEL, CSINC, CSINV, CSNEG:
\t\tregisters = []int{inst.Rd, inst.Rn, inst.Rm}
\tdefault:
''',
)
replace_once(
    "internal/arch/arm64/tr_exclusive.go",
    '''func isExclusiveLoadOp(op Op) bool {
\treturn op == LDXR || op == LDAXR
}

func isExclusiveStoreOp(op Op) bool {
\treturn op == STXR || op == STLXR
}

func validateExclusiveRegister(reg int) error {
''',
    '''func isExclusiveSingleLoadOp(op Op) bool {
\treturn op == LDXR || op == LDAXR
}

func isExclusivePairLoadOp(op Op) bool {
\treturn op == LDXP || op == LDAXP
}

func isExclusiveLoadOp(op Op) bool {
\treturn isExclusiveSingleLoadOp(op) || isExclusivePairLoadOp(op)
}

func isExclusiveSingleStoreOp(op Op) bool {
\treturn op == STXR || op == STLXR
}

func isExclusivePairStoreOp(op Op) bool {
\treturn op == STXP || op == STLXP
}

func isExclusiveStoreOp(op Op) bool {
\treturn isExclusiveSingleStoreOp(op) || isExclusivePairStoreOp(op)
}

func exclusiveRegisterArity(op Op) int {
\tswitch {
\tcase isExclusiveSingleLoadOp(op), isExclusiveSingleStoreOp(op):
\t\treturn 1
\tcase isExclusivePairLoadOp(op), isExclusivePairStoreOp(op):
\t\treturn 2
\tdefault:
\t\treturn 0
\t}
}

func validateExclusiveBoundary(first, last vm.Instruction) error {
\tloadArity := exclusiveRegisterArity(Op(first.Op))
\tstoreArity := exclusiveRegisterArity(Op(last.Op))
\tif loadArity == 0 || storeArity == 0 {
\t\treturn fmt.Errorf("exclusive region has an unsupported boundary instruction")
\t}
\tif loadArity != storeArity {
\t\treturn fmt.Errorf("exclusive load/store register-count mismatch")
\t}
\tif first.Rn != last.Rn || first.Shift != last.Shift {
\t\treturn fmt.Errorf("exclusive store address/width does not match exclusive load")
\t}
\tfor name, reg := range []struct {
\t\tname string
\t\treg  int
\t}{
\t\t{"address", first.Rn}, {"load result", first.Rd},
\t\t{"store value", last.Rd}, {"status", last.Rm},
\t} {
\t\tif err := validateExclusiveRegister(reg); err != nil {
\t\t\treturn fmt.Errorf("exclusive %s: %w", name, err)
\t\t}
\t}
\tif loadArity == 2 {
\t\tif err := validateExclusiveRegister(first.Rt2); err != nil {
\t\t\treturn fmt.Errorf("exclusive second load result: %w", err)
\t\t}
\t\tif err := validateExclusiveRegister(last.Rt2); err != nil {
\t\t\treturn fmt.Errorf("exclusive second store value: %w", err)
\t\t}
\t\tif first.Rd == first.Rt2 {
\t\t\treturn fmt.Errorf("pair-exclusive load destinations overlap")
\t\t}
\t}
\t// ARM defines status/data and status/base overlap for Store-Exclusive as
\t// CONSTRAINED UNPREDICTABLE. Reject it rather than inheriting a PE choice.
\tif last.Rm != vm.REG_XZR {
\t\tif last.Rm == last.Rd || (storeArity == 2 && last.Rm == last.Rt2) {
\t\t\treturn fmt.Errorf("exclusive store status overlaps store data")
\t\t}
\t\tif last.Rm == last.Rn {
\t\t\treturn fmt.Errorf("exclusive store status overlaps address register")
\t\t}
\t}
\treturn nil
}

func validateExclusiveRegister(reg int) error {
''',
)
replace_once(
    "internal/arch/arm64/tr_exclusive.go",
    '''\tif !isExclusiveLoadOp(Op(first.Op)) || !isExclusiveStoreOp(Op(last.Op)) {
\t\treturn nil, fmt.Errorf("exclusive region must be bounded by LDXR/LDAXR and STXR/STLXR")
\t}
\tif first.Rn != last.Rn || first.Shift != last.Shift {
\t\treturn nil, fmt.Errorf("exclusive region address/width mismatch")
\t}
\tfor name, reg := range map[string]int{
\t\t"address": first.Rn, "load result": first.Rd,
\t\t"store value": last.Rd, "status": last.Rm,
\t} {
\t\tif err := validateExclusiveRegister(reg); err != nil {
\t\t\treturn nil, fmt.Errorf("exclusive %s: %w", name, err)
\t\t}
\t}
''',
    '''\tif !isExclusiveLoadOp(Op(first.Op)) || !isExclusiveStoreOp(Op(last.Op)) {
\t\treturn nil, fmt.Errorf("exclusive region must be bounded by supported load/store-exclusive instructions")
\t}
\tif err := validateExclusiveBoundary(first, last); err != nil {
\t\treturn nil, err
\t}
''',
)
replace_once(
    "internal/arch/arm64/tr_exclusive.go",
    '''\tswitch Op(inst.Op) {
\tcase LDXR, LDAXR:
\t\treturn []exclusiveRegisterField{{register: inst.Rn, shift: 5}, {register: inst.Rd, shift: 0}}
\tcase STXR, STLXR:
\t\treturn []exclusiveRegisterField{{register: inst.Rm, shift: 16}, {register: inst.Rn, shift: 5}, {register: inst.Rd, shift: 0}}
\tcase ADD_IMM, SUB_IMM:
\t\treturn []exclusiveRegisterField{{register: inst.Rn, shift: 5}, {register: inst.Rd, shift: 0}}
\tcase ADD_REG, SUB_REG, AND_REG, ORR_REG, EOR_REG, MUL:
\t\treturn []exclusiveRegisterField{{register: inst.Rm, shift: 16}, {register: inst.Rn, shift: 5}, {register: inst.Rd, shift: 0}}
''',
    '''\tswitch Op(inst.Op) {
\tcase LDXR, LDAXR:
\t\treturn []exclusiveRegisterField{{register: inst.Rn, shift: 5}, {register: inst.Rd, shift: 0}}
\tcase LDXP, LDAXP:
\t\treturn []exclusiveRegisterField{{register: inst.Rn, shift: 5}, {register: inst.Rt2, shift: 10}, {register: inst.Rd, shift: 0}}
\tcase STXR, STLXR:
\t\treturn []exclusiveRegisterField{{register: inst.Rm, shift: 16}, {register: inst.Rn, shift: 5}, {register: inst.Rd, shift: 0}}
\tcase STXP, STLXP:
\t\treturn []exclusiveRegisterField{{register: inst.Rm, shift: 16}, {register: inst.Rt2, shift: 10}, {register: inst.Rn, shift: 5}, {register: inst.Rd, shift: 0}}
\tcase ADD_IMM, SUB_IMM, SUBS_IMM:
\t\treturn []exclusiveRegisterField{{register: inst.Rn, shift: 5}, {register: inst.Rd, shift: 0}}
\tcase ADD_REG, SUB_REG, SUBS_REG, AND_REG, ORR_REG, EOR_REG, MUL,
\t\tCSEL, CSINC, CSINV, CSNEG:
\t\treturn []exclusiveRegisterField{{register: inst.Rm, shift: 16}, {register: inst.Rn, shift: 5}, {register: inst.Rd, shift: 0}}
''',
)

# ---------------------------------------------------------------------------
# 6. Generated thunk bridges VM FL <-> hardware NZCV around raw instructions.
# ---------------------------------------------------------------------------
replace_once(
    "internal/runtime/exclusivegen.go",
    '''\tvar s strings.Builder
\ts.WriteString(".text\\n.p2align 2\\n")
''',
    '''\tvar s strings.Builder
\ts.WriteString("#include \\"vm_abi.h\\"\\n.text\\n.p2align 2\\n")
''',
)
replace_once(
    "internal/runtime/exclusivegen.go",
    '''\t\tfor host, guest := range registersByID[region.ID] {
\t\t\tfmt.Fprintf(&s, "  ldr x%d, [x16, #%d]\\n", host, guest*8)
\t\t}
\t\tfor _, raw := range patchedByID[region.ID] {
\t\t\tfmt.Fprintf(&s, "  .inst 0x%08x\\n", raw)
\t\t}
\t\tfor host, guest := range registersByID[region.ID] {
''',
    '''\t\tfor host, guest := range registersByID[region.ID] {
\t\t\tfmt.Fprintf(&s, "  ldr x%d, [x16, #%d]\\n", host, guest*8)
\t\t}
\t\ts.WriteString("  ldr w17, [x16, #VM_CTX_FL]\\n  and w17, w17, #0xf\\n  lsl x17, x17, #28\\n  msr nzcv, x17\\n")
\t\tfor _, raw := range patchedByID[region.ID] {
\t\t\tfmt.Fprintf(&s, "  .inst 0x%08x\\n", raw)
\t\t}
\t\ts.WriteString("  mrs x17, nzcv\\n  lsr x17, x17, #28\\n  str w17, [x16, #VM_CTX_FL]\\n")
\t\tfor host, guest := range registersByID[region.ID] {
''',
)

# ---------------------------------------------------------------------------
# 7. Runtime generator tests: assert NZCV bridge and exact-r29 pair generation.
# ---------------------------------------------------------------------------
replace_once(
    "internal/runtime/runtime_test.go",
    '''\tfor _, token := range []string{name + ":", ".inst 0xc85ffc20", ".inst 0x91000400", ".inst 0xc802fc20", ".cfi_startproc", "bti c", ".note.gnu.property"} {
''',
    '''\tfor _, token := range []string{name + ":", "#include \\"vm_abi.h\\"", ".inst 0xc85ffc20", ".inst 0x91000400", ".inst 0xc802fc20", ".cfi_startproc", "bti c", "ldr w17, [x16, #VM_CTX_FL]", "msr nzcv, x17", "mrs x17, nzcv", "str w17, [x16, #VM_CTX_FL]", ".note.gnu.property"} {
''',
)
replace_once(
    "internal/runtime/runtime_test.go",
    '''\tif strings.Index(string(assembly), ".inst 0xc85ffc20") > strings.Index(string(assembly), ".inst 0xc802fc20") {
\t\tt.Fatal("exclusive instruction order changed")
\t}

\tbad := region
''',
    '''\tif strings.Index(string(assembly), ".inst 0xc85ffc20") > strings.Index(string(assembly), ".inst 0xc802fc20") {
\t\tt.Fatal("exclusive instruction order changed")
\t}
\tif strings.Count(string(assembly), "msr nzcv, x17") != 1 || strings.Count(string(assembly), "mrs x17, nzcv") != 1 {
\t\tt.Fatal("exclusive thunk did not bridge NZCV exactly once")
\t}

\tbad := region
''',
)
replace_once(
    "internal/runtime/runtime_test.go",
    '''\t\tExclusiveRegions: []vm.ExclusiveRegion{
\t\t\tvm.NewExclusiveRegion([]uint32{0xc85ffc20, 0x91000400, 0xc802fc20}),
\t\t\tvm.NewExclusiveRegion([]uint32{0xc85ffe34, 0x91000694, 0xc813fe34}),
\t\t},
''',
    '''\t\tExclusiveRegions: []vm.ExclusiveRegion{
\t\t\tvm.NewExclusiveRegion([]uint32{0xc85ffc20, 0x91000400, 0xc802fc20}),
\t\t\tvm.NewExclusiveRegion([]uint32{0xc85ffe34, 0x91000694, 0xc813fe34}),
\t\t\tvm.NewExclusiveRegion([]uint32{0xc87f0440, 0xc8239444}),
\t\t},
''',
)
replace_once(
    "internal/runtime/runtime_test.go",
    '''\tif len(image.ExclusiveRegions) != 2 || len(image.FPSIMDInstructions) != 6 {
''',
    '''\tif len(image.ExclusiveRegions) != 3 || len(image.FPSIMDInstructions) != 6 {
''',
)

# ---------------------------------------------------------------------------
# 8. Pair-exclusive semantic, compiler-shape, remap, and fail-closed tests.
# ---------------------------------------------------------------------------
Path("internal/arch/arm64/exclusive_pair_test.go").write_text(r'''package arm64

import (
	"testing"

	"github.com/vmpacker/internal/vm"
)

func TestDecoderRecognizesPairExclusiveForms(t *testing.T) {
	decoder := NewDecoder()
	cases := []struct {
		raw        uint32
		op         Op
		width      int
		sf         bool
		rd, rt2    int
		rn, status int
	}{
		{0xc87f0440, LDXP, 8, true, 0, 1, 2, -1},
		{0xc87f90a3, LDAXP, 8, true, 3, 4, 5, -1},
		{0xc8262127, STXP, 8, true, 7, 8, 9, 6},
		{0xc82ab1ab, STLXP, 8, true, 11, 12, 13, 10},
		{0x887f3e0e, LDXP, 4, false, 14, 15, 16, -1},
		{0x887fca71, LDAXP, 4, false, 17, 18, 19, -1},
		{0x88345af5, STXP, 4, false, 21, 22, 23, 20},
		{0x8838eb79, STLXP, 4, false, 25, 26, 27, 24},
	}
	for _, tc := range cases {
		inst := decoder.Decode(tc.raw, 0)
		if got := Op(inst.Op); got != tc.op || inst.Shift != tc.width || inst.SF != tc.sf ||
			inst.Rd != tc.rd || inst.Rt2 != tc.rt2 || inst.Rn != tc.rn || inst.Rm != tc.status {
			t.Fatalf("raw=%#08x got=%s width=%d sf=%v rd=%d rt2=%d rn=%d rm=%d", tc.raw, OpName(got), inst.Shift, inst.SF, inst.Rd, inst.Rt2, inst.Rn, inst.Rm)
		}
	}
}

func TestPairExclusiveSupportsAllOrderingBoundaryPairs(t *testing.T) {
	decoder := NewDecoder()
	for _, tc := range []struct {
		name        string
		load, store uint32
	}{
		{"relaxed-relaxed", 0xc87f0440, 0xc8231444},
		{"acquire-relaxed", 0xc87f8440, 0xc8231444},
		{"relaxed-release", 0xc87f0440, 0xc8239444},
		{"acquire-release", 0xc87f8440, 0xc8239444},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raws := []uint32{tc.load, tc.store}
			instructions := []vm.Instruction{decoder.Decode(tc.load, 0), decoder.Decode(tc.store, 4)}
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
			if patched[0]&0x8000 != tc.load&0x8000 || patched[len(patched)-1]&0x8000 != tc.store&0x8000 {
				t.Fatalf("ordering bits changed: patched=%#x original=%#x", patched, raws)
			}
		})
	}
}

func TestPairExclusiveAcceptsAuditedCompiler128BitBody(t *testing.T) {
	decoder := NewDecoder()
	raws := []uint32{
		0xc87fa009, // ldaxp x9, x8, [x0]
		0xeb02013f, // cmp x9, x2
		0x1a9f97ea, // cset w10, hi
		0xeb03011f, // cmp x8, x3
		0x1a9fd7eb, // cset w11, gt
		0x1a8b014a, // csel w10, w10, w11, eq
		0x7100015f, // cmp w10, #0
		0x9a83110a, // csel x10, x8, x3, ne
		0x9a82112b, // csel x11, x9, x2, ne
		0xc82ca80b, // stlxp w12, x11, x10, [x0]
	}
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
		t.Fatal("compiler-style exclusive region words changed before thunk planning")
	}
	patched, registers, err := PlanExclusiveThunk(region)
	if err != nil {
		t.Fatal(err)
	}
	if len(registers) == 0 || len(registers) > maxExclusiveThunkRegisters {
		t.Fatalf("register remap=%v", registers)
	}
	for i, raw := range patched {
		inst := decoder.Decode(raw, i*4)
		for _, field := range exclusiveRegisterFields(inst) {
			if field.register != vm.REG_XZR && (field.register < 0 || field.register >= maxExclusiveThunkRegisters) {
				t.Fatalf("patched instruction %d kept non-thunk register %d: %#08x", i, field.register, raw)
			}
		}
	}
	if Op(decoder.Decode(patched[0], 0).Op) != LDAXP || Op(decoder.Decode(patched[len(patched)-1], 0).Op) != STLXP {
		t.Fatalf("pair-exclusive boundary opcodes changed: %#x", patched)
	}
}

func TestPairExclusiveAndStatusOverlapRemainFailClosed(t *testing.T) {
	decoder := NewDecoder()
	cases := map[string][]uint32{
		"single-load-pair-store": {0xc85f7c40, 0xc8231444},
		"pair-load-single-store": {0xc87f0440, 0xc8037c44},
		"pair-width-mismatch":    {0xc87f0440, 0x88231444},
		"pair-address-mismatch":  {0xc87f0440, 0xc8231464},
		"pair-sp-address":        {0xc87f07e0, 0xc82317e4},
		"pair-load-overlap":      {0xc87f0040, 0xc8231444},
		"pair-status-data":       {0xc87f0440, 0xc8241444},
		"pair-status-base":       {0xc87f0440, 0xc8221444},
		"single-status-data":     {0xc85f7c40, 0xc8047c44},
		"single-status-base":     {0xc85f7c40, 0xc8027c44},
		"nested-pair-load":       {0xc87f0440, 0xc87f8440, 0xc8231444},
		"branch-inside-pair":     {0xc87f0440, 0x14000000, 0xc8231444},
	}
	for name, raws := range cases {
		t.Run(name, func(t *testing.T) {
			instructions := make([]vm.Instruction, len(raws))
			for i, raw := range raws {
				instructions[i] = decoder.Decode(raw, i*4)
			}
			result := translateForPhase5(t, instructions)
			if len(result.Unsupported) == 0 {
				t.Fatalf("unsafe pair-exclusive sequence was accepted: regions=%v", result.ExclusiveRegions)
			}
			if name != "nested-pair-load" && len(result.ExclusiveRegions) != 0 {
				t.Fatalf("unsafe pair-exclusive sequence materialized regions=%v", result.ExclusiveRegions)
			}
		})
	}
}
''')

print("phase12 pair-exclusive patch applied")
