#!/usr/bin/env python3
from pathlib import Path


def replace_once(path: str, old: str, new: str) -> None:
    p = Path(path)
    text = p.read_text()
    count = text.count(old)
    if count != 1:
        raise SystemExit(f"{path}: expected exactly one match, found {count}\n--- expected ---\n{old}")
    p.write_text(text.replace(old, new, 1))


# 1) Decoder semantic ops, LSE decoder table dispatch, and names.
replace_once(
    "internal/arch/arm64/decoder.go",
    "\tBTI\n\tFPSIMD_NATIVE\n\tUNSUPPORTED\n",
    "\tBTI\n\tFPSIMD_NATIVE\n\tSWP\n\tLDCLR\n\tLDEOR\n\tLDSET\n\tUNSUPPORTED\n",
)
replace_once(
    "internal/arch/arm64/decoder.go",
    "\tcase op0&0b0101 == 0b0100:\n\t\tmatched = matchAndDecode(raw, ldstPatterns, &inst)\n",
    "\tcase op0&0b0101 == 0b0100:\n\t\tmatched = matchAndDecode(raw, lseAtomicPatterns, &inst)\n\t\tif !matched {\n\t\t\tmatched = matchAndDecode(raw, ldstPatterns, &inst)\n\t\t}\n",
)
replace_once(
    "internal/arch/arm64/decoder.go",
    "\t\tLDPSW: \"LDPSW\", LDADD: \"LDADD\", CAS: \"CAS\",\n",
    "\t\tLDPSW: \"LDPSW\", LDADD: \"LDADD\", CAS: \"CAS\",\n\t\tSWP: \"SWP\", LDCLR: \"LDCLR\", LDEOR: \"LDEOR\", LDSET: \"LDSET\",\n",
)

Path("internal/arch/arm64/decode_lse_atomic.go").write_text(r'''package arm64

import "github.com/vmpacker/internal/vm"

// lseAtomicPatterns covers the single-register FEAT_LSE read-modify-write
// family that shares LDADD's width/register/order encoding. Keep these
// separate from the general load/store table so the product surface is
// explicit and the generic table does not become an unreviewed LSE catch-all.
var lseAtomicPatterns = []InstrPattern{
	{
		Name: "SWP", Mask: 0x3F20FC00, Value: 0x38208000, Op: SWP,
		Fields: []FieldDef{{Name: "size", Hi: 31, Lo: 30}, fRm16, fRn, fRd},
		Post:   postLdadd,
	},
	{
		Name: "LDCLR", Mask: 0x3F20FC00, Value: 0x38201000, Op: LDCLR,
		Fields: []FieldDef{{Name: "size", Hi: 31, Lo: 30}, fRm16, fRn, fRd},
		Post:   postLdadd,
	},
	{
		Name: "LDEOR", Mask: 0x3F20FC00, Value: 0x38202000, Op: LDEOR,
		Fields: []FieldDef{{Name: "size", Hi: 31, Lo: 30}, fRm16, fRn, fRd},
		Post:   postLdadd,
	},
	{
		Name: "LDSET", Mask: 0x3F20FC00, Value: 0x38203000, Op: LDSET,
		Fields: []FieldDef{{Name: "size", Hi: 31, Lo: 30}, fRm16, fRn, fRd},
		Post:   postLdadd,
	},
}

func isLoadReturnLSE(op Op) bool {
	switch op {
	case LDADD, SWP, LDCLR, LDEOR, LDSET:
		return true
	default:
		return false
	}
}

func atomicUsesRm(op Op) bool {
	return isLoadReturnLSE(op) || op == CAS
}

// Compile-time use of vm keeps the decoder table's register semantics tied to
// the shared decoded instruction type rather than introducing a parallel type.
var _ = vm.REG_XZR
''')

# 2) Product policy: whitelist the completed LSE RMW family and validate Rs.
replace_once(
    "internal/arch/arm64/policy.go",
    "\tallow([]Op{LDAR, STLR, LDADD, CAS}, validateAtomicNative)\n",
    "\tallow([]Op{LDAR, STLR, LDADD, CAS, SWP, LDCLR, LDEOR, LDSET}, validateAtomicNative)\n",
)
replace_once(
    "internal/arch/arm64/policy.go",
    "\tif (Op(inst.Op) == LDADD || Op(inst.Op) == CAS) && !validDataReg(inst.Rm) {\n",
    "\tif atomicUsesRm(Op(inst.Op)) && !validDataReg(inst.Rm) {\n",
)

# 3) Translator dispatch.
replace_once(
    "internal/arch/arm64/translator.go",
    "\tcase LDAR, STLR, LDADD, CAS:\n\t\treturn 0, t.trAtomic(inst)\n",
    "\tcase LDAR, STLR, LDADD, CAS, SWP, LDCLR, LDEOR, LDSET:\n\t\treturn 0, t.trAtomic(inst)\n",
)

# 4) Atomic bytecode encoding: kinds 4..7 plus architectural acquire suppression.
replace_once(
    "internal/arch/arm64/tr_atomic.go",
    '''func atomicMemoryOrder(op Op, raw uint32) byte {
\tswitch op {
\tcase LDAR:
\t\treturn 1
\tcase STLR:
\t\treturn 2
\tcase LDADD:
\t\tacquire := byte((raw >> 23) & 1)
\t\trelease := byte((raw >> 22) & 1)
\t\treturn acquire | release<<1
\tcase CAS:
\t\tacquire := byte((raw >> 22) & 1)
\t\trelease := byte((raw >> 15) & 1)
\t\treturn acquire | release<<1
\tdefault:
\t\tpanic("atomicMemoryOrder called for a non-atomic operation")
\t}
}
''',
    '''func atomicMemoryOrder(inst vm.Instruction) byte {
\top := Op(inst.Op)
\tswitch op {
\tcase LDAR:
\t\treturn 1
\tcase STLR:
\t\treturn 2
\tcase LDADD, SWP, LDCLR, LDEOR, LDSET:
\t\tacquire := byte((inst.Raw >> 23) & 1)
\t\t// ARM suppresses acquire for this LSE RMW class when Rt is XZR.
\t\tif inst.Rd == vm.REG_XZR {
\t\t\tacquire = 0
\t\t}
\t\trelease := byte((inst.Raw >> 22) & 1)
\t\treturn acquire | release<<1
\tcase CAS:
\t\tacquire := byte((inst.Raw >> 22) & 1)
\t\trelease := byte((inst.Raw >> 15) & 1)
\t\treturn acquire | release<<1
\tdefault:
\t\tpanic("atomicMemoryOrder called for a non-atomic operation")
\t}
}
''',
)
replace_once(
    "internal/arch/arm64/tr_atomic.go",
    '''func (t *Translator) trAtomic(inst vm.Instruction) error {
\top := Op(inst.Op)
\tkind := map[Op]byte{LDAR: 0, STLR: 1, LDADD: 2, CAS: 3}[op]
\trd, err := encodeAtomicRegister(inst.Rd)
\tif err != nil {
\t\treturn err
\t}
\trn, err := t.mapReg(inst.Rn)
\tif err != nil {
\t\treturn err
\t}
\trm := atomicZeroRegister
\tif op == LDADD || op == CAS {
\t\trm, err = encodeAtomicRegister(inst.Rm)
\t\tif err != nil {
\t\t\treturn err
\t\t}
\t}
\tt.emitOp(vm.OpAtomic, kind, byte(inst.Shift), atomicMemoryOrder(op, inst.Raw), rd, rn, rm)
\treturn nil
}
''',
    '''func (t *Translator) trAtomic(inst vm.Instruction) error {
\top := Op(inst.Op)
\tkind := map[Op]byte{
\t\tLDAR: 0, STLR: 1, LDADD: 2, CAS: 3,
\t\tSWP: 4, LDCLR: 5, LDEOR: 6, LDSET: 7,
\t}[op]
\trd, err := encodeAtomicRegister(inst.Rd)
\tif err != nil {
\t\treturn err
\t}
\trn, err := t.mapReg(inst.Rn)
\tif err != nil {
\t\treturn err
\t}
\trm := atomicZeroRegister
\tif atomicUsesRm(op) {
\t\trm, err = encodeAtomicRegister(inst.Rm)
\t\tif err != nil {
\t\t\treturn err
\t\t}
\t}
\tt.emitOp(vm.OpAtomic, kind, byte(inst.Shift), atomicMemoryOrder(inst), rd, rn, rm)
\treturn nil
}
''',
)

# 5) Interpreter handler understands kinds 4..7 as source/Rt-return RMW operations.
replace_once(
    "internal/runtime/templates/android/arm64/vm_handlers/h_system.h",
    '''  if (kind > 3 || (width != 1 && width != 2 && width != 4 && width != 8) ||
      order > 3 || rn > 31) {
''',
    '''  if (kind > 7 || (width != 1 && width != 2 && width != 4 && width != 8) ||
      order > 3 || rn > 31) {
''',
)
replace_once(
    "internal/runtime/templates/android/arm64/vm_handlers/h_system.h",
    '''  if (kind == 1)
    first = vm_atomic_reg_read(vm, rd);
  else if (kind == 2)
    first = vm_atomic_reg_read(vm, rm);
  else if (kind == 3) {
''',
    '''  if (kind == 1)
    first = vm_atomic_reg_read(vm, rd);
  else if (kind == 2 || kind >= 4)
    first = vm_atomic_reg_read(vm, rm);
  else if (kind == 3) {
''',
)
replace_once(
    "internal/runtime/templates/android/arm64/vm_handlers/h_system.h",
    '''  if (kind == 0 || kind == 2)
    vm_atomic_reg_write(vm, rd, old, width);
  else if (kind == 3)
''',
    '''  if (kind == 0 || kind == 2 || kind >= 4)
    vm_atomic_reg_write(vm, rd, old, width);
  else if (kind == 3)
''',
)

# 6) Native LSE helper implementation.
replace_once(
    "internal/runtime/templates/android/arm64/vm_native.S",
    '''\tcmp w0, #3
\tb.eq .Latomic_cas
\tb .Latomic_bad
''',
    '''\tcmp w0, #3
\tb.eq .Latomic_cas
\tcmp w0, #4
\tb.eq .Latomic_swp
\tcmp w0, #5
\tb.eq .Latomic_clr
\tcmp w0, #6
\tb.eq .Latomic_eor
\tcmp w0, #7
\tb.eq .Latomic_set
\tb .Latomic_bad
''',
)
replace_once(
    "internal/runtime/templates/android/arm64/vm_native.S",
    '''.Lcas_4:
\tVM_ATOMIC_ORDER4 cas, casa, casl, casal, w0, w5

.Latomic_bad:
''',
    '''.Lcas_4:
\tVM_ATOMIC_ORDER4 cas, casa, casl, casal, w0, w5

.Latomic_swp:
\tcmp w1, #1
\tb.eq .Lswp_1
\tcmp w1, #2
\tb.eq .Lswp_2
\tcmp w1, #4
\tb.eq .Lswp_4
\tcmp w1, #8
\tb.ne .Latomic_bad
\tVM_ATOMIC_ORDER4 swp, swpa, swpl, swpal, x4, x0
.Lswp_1:
\tVM_ATOMIC_ORDER4 swpb, swpab, swplb, swpalb, w4, w0
.Lswp_2:
\tVM_ATOMIC_ORDER4 swph, swpah, swplh, swpalh, w4, w0
.Lswp_4:
\tVM_ATOMIC_ORDER4 swp, swpa, swpl, swpal, w4, w0

.Latomic_clr:
\tcmp w1, #1
\tb.eq .Lclr_1
\tcmp w1, #2
\tb.eq .Lclr_2
\tcmp w1, #4
\tb.eq .Lclr_4
\tcmp w1, #8
\tb.ne .Latomic_bad
\tVM_ATOMIC_ORDER4 ldclr, ldclra, ldclrl, ldclral, x4, x0
.Lclr_1:
\tVM_ATOMIC_ORDER4 ldclrb, ldclrab, ldclrlb, ldclralb, w4, w0
.Lclr_2:
\tVM_ATOMIC_ORDER4 ldclrh, ldclrah, ldclrlh, ldclralh, w4, w0
.Lclr_4:
\tVM_ATOMIC_ORDER4 ldclr, ldclra, ldclrl, ldclral, w4, w0

.Latomic_eor:
\tcmp w1, #1
\tb.eq .Leor_1
\tcmp w1, #2
\tb.eq .Leor_2
\tcmp w1, #4
\tb.eq .Leor_4
\tcmp w1, #8
\tb.ne .Latomic_bad
\tVM_ATOMIC_ORDER4 ldeor, ldeora, ldeorl, ldeoral, x4, x0
.Leor_1:
\tVM_ATOMIC_ORDER4 ldeorb, ldeorab, ldeorlb, ldeoralb, w4, w0
.Leor_2:
\tVM_ATOMIC_ORDER4 ldeorh, ldeorah, ldeorlh, ldeoralh, w4, w0
.Leor_4:
\tVM_ATOMIC_ORDER4 ldeor, ldeora, ldeorl, ldeoral, w4, w0

.Latomic_set:
\tcmp w1, #1
\tb.eq .Lset_1
\tcmp w1, #2
\tb.eq .Lset_2
\tcmp w1, #4
\tb.eq .Lset_4
\tcmp w1, #8
\tb.ne .Latomic_bad
\tVM_ATOMIC_ORDER4 ldset, ldseta, ldsetl, ldsetal, x4, x0
.Lset_1:
\tVM_ATOMIC_ORDER4 ldsetb, ldsetab, ldsetlb, ldsetalb, w4, w0
.Lset_2:
\tVM_ATOMIC_ORDER4 ldseth, ldsetah, ldsetlh, ldsetalh, w4, w0
.Lset_4:
\tVM_ATOMIC_ORDER4 ldset, ldseta, ldsetl, ldsetal, w4, w0

.Latomic_bad:
''',
)

# 7) Focused semantic/decoder regression tests.
Path("internal/arch/arm64/lse_atomic_test.go").write_text(r'''package arm64

import (
	"testing"

	"github.com/vmpacker/internal/vm"
)

func TestLSEAtomicDecoderFamiliesAndWidths(t *testing.T) {
	decoder := NewDecoder()
	families := []struct {
		name string
		base uint32
		op   Op
	}{
		{"SWP", 0x38208000, SWP},
		{"LDCLR", 0x38201000, LDCLR},
		{"LDEOR", 0x38202000, LDEOR},
		{"LDSET", 0x38203000, LDSET},
	}
	for _, family := range families {
		for size := uint32(0); size < 4; size++ {
			raw := family.base | size<<30 | 3<<16 | 2<<5 | 1
			inst := decoder.Decode(raw, 0)
			if got := Op(inst.Op); got != family.op {
				t.Fatalf("%s size=%d decoded as %s raw=%#08x", family.name, size, OpName(got), raw)
			}
			if inst.Shift != 1<<size || inst.Rm != 3 || inst.Rn != 2 || inst.Rd != 1 {
				t.Fatalf("%s size=%d decoded=%+v", family.name, size, inst)
			}
		}
	}
}

func TestLSEAtomicFamiliesPreserveKindWidthOrderAndRegisters(t *testing.T) {
	for _, tc := range []struct {
		inst vm.Instruction
		want []byte
	}{
		{vm.Instruction{Op: int(SWP), Rd: 1, Rn: 2, Rm: 3, Shift: 8, Raw: 3 << 22}, []byte{4, 8, 3, 1, 2, 3}},
		{vm.Instruction{Op: int(LDCLR), Rd: 4, Rn: 5, Rm: 6, Shift: 1, Raw: 1 << 23}, []byte{5, 1, 1, 4, 5, 6}},
		{vm.Instruction{Op: int(LDEOR), Rd: 7, Rn: 8, Rm: 9, Shift: 2, Raw: 1 << 22}, []byte{6, 2, 2, 7, 8, 9}},
		{vm.Instruction{Op: int(LDSET), Rd: 10, Rn: 11, Rm: 12, Shift: 4}, []byte{7, 4, 0, 10, 11, 12}},
	} {
		result := translateForPhase5(t, []vm.Instruction{tc.inst})
		if len(result.Unsupported) != 0 {
			t.Fatalf("%s unsupported=%v", OpName(Op(tc.inst.Op)), result.Unsupported)
		}
		ops, operands := translatedOps(t, result)
		if len(ops) < 1 || ops[0] != vm.OpAtomic || len(operands[0]) != len(tc.want) {
			t.Fatalf("%s ops=%v operands=%v", OpName(Op(tc.inst.Op)), ops, operands)
		}
		for i := range tc.want {
			if operands[0][i] != tc.want[i] {
				t.Fatalf("%s operands=%v want=%v", OpName(Op(tc.inst.Op)), operands[0], tc.want)
			}
		}
	}
}

func TestLoadReturnLSESuppressesAcquireWhenRtIsXZR(t *testing.T) {
	for _, op := range []Op{LDADD, SWP, LDCLR, LDEOR, LDSET} {
		inst := vm.Instruction{
			Op: int(op), Rd: vm.REG_XZR, Rn: 2, Rm: 3,
			Shift: 8, Raw: 1 << 23,
		}
		result := translateForPhase5(t, []vm.Instruction{inst})
		if len(result.Unsupported) != 0 {
			t.Fatalf("%s unsupported=%v", OpName(op), result.Unsupported)
		}
		ops, operands := translatedOps(t, result)
		if len(ops) < 1 || ops[0] != vm.OpAtomic || operands[0][2] != 0 || operands[0][3] != 0xff {
			t.Fatalf("%s operands=%v", OpName(op), operands[0])
		}
	}
}

func TestLSEAtomicPolicyFailsClosedOnInvalidWidth(t *testing.T) {
	for _, op := range []Op{SWP, LDCLR, LDEOR, LDSET} {
		if err := validateInstructionPolicy(vm.Instruction{Op: int(op), Rd: 1, Rn: 2, Rm: 3, Shift: 16}); err == nil {
			t.Fatalf("%s accepted invalid 16-byte scalar width", OpName(op))
		}
	}
}
''')

print("phase10 LSE patch applied")
