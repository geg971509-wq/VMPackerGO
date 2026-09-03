from pathlib import Path
import re


def replace_once(path: str, old: str, new: str) -> None:
    p = Path(path)
    text = p.read_text()
    if old not in text:
        raise SystemExit(f"missing expected text in {path}: {old[:100]!r}")
    if text.count(old) != 1:
        raise SystemExit(f"expected one match in {path}, found {text.count(old)}")
    p.write_text(text.replace(old, new, 1))


def regex_once(path: str, pattern: str, repl: str) -> None:
    p = Path(path)
    text = p.read_text()
    new, count = re.subn(pattern, repl, text, count=1, flags=re.S)
    if count != 1:
        raise SystemExit(f"expected one regex match in {path}, found {count}: {pattern}")
    p.write_text(new)


# Decoder Op/name surface.
replace_once(
    "internal/arch/arm64/decoder.go",
    "\tLDADD\n\tCAS\n\tPACIASP",
    "\tLDADD\n\tCAS\n\tCASP\n\tPACIASP",
)
replace_once(
    "internal/arch/arm64/decoder.go",
    'LDPSW: "LDPSW", LDADD: "LDADD", CAS: "CAS",',
    'LDPSW: "LDPSW", LDADD: "LDADD", CAS: "CAS", CASP: "CASP",',
)

# Explicit CASP LSE pattern. NP=0 distinguishes pair CASP from scalar CAS.
replace_once(
    "internal/arch/arm64/decode_lse_atomic.go",
    "var lseAtomicPatterns = []InstrPattern{\n",
    """var lseAtomicPatterns = []InstrPattern{\n\t{\n\t\tName: \"CASP\", Mask: 0xBFA07C00, Value: 0x08207C00, Op: CASP,\n\t\tFields: []FieldDef{{Name: \"size\", Hi: 31, Lo: 30}, fRm16, fRn, fRd},\n\t\tPost:   postCasp,\n\t},\n""",
)
replace_once(
    "internal/arch/arm64/decode_lse_atomic.go",
    "func isLoadReturnLSE(op Op) bool {",
    """func postCasp(f map[string]int64, inst *vm.Instruction) {\n\tsz := f[\"size\"]\n\t// CASP encodes two architectural pair widths: W-pair (00) and X-pair (01).\n\t// The mask fixes bit31=0, so reserved size encodings cannot reach this post hook.\n\tinst.Shift = 4 << int(sz)\n\tinst.SF = sz == 1\n}\n\nfunc isLoadReturnLSE(op Op) bool {""",
)
# Add vm import because postCasp now references vm.Instruction.
replace_once(
    "internal/arch/arm64/decode_lse_atomic.go",
    "package arm64\n\n",
    'package arm64\n\nimport "github.com/vmpacker/internal/vm"\n\n',
)
replace_once(
    "internal/arch/arm64/decode_lse_atomic.go",
    "return isLoadReturnLSE(op) || op == CAS",
    "return isLoadReturnLSE(op) || op == CAS || op == CASP",
)

# CASP-specific policy: pair lows are even and conservatively bounded to X0-X28.
replace_once(
    "internal/arch/arm64/policy.go",
    "allow([]Op{LDAR, STLR, LDADD, CAS, SWP, LDCLR, LDEOR, LDSET, LDSMAX, LDSMIN, LDUMAX, LDUMIN}, validateAtomicNative)",
    "allow([]Op{LDAR, STLR, LDADD, CAS, CASP, SWP, LDCLR, LDEOR, LDSET, LDSMAX, LDSMIN, LDUMAX, LDUMIN}, validateAtomicNative)",
)
replace_once(
    "internal/arch/arm64/policy.go",
    "func validateAtomicNative(inst vm.Instruction) error {\n",
    """func validateAtomicNative(inst vm.Instruction) error {\n\tif Op(inst.Op) == CASP {\n\t\tif inst.Shift != 4 && inst.Shift != 8 {\n\t\t\treturn fmt.Errorf(\"CASP pair member width %d is unsupported\", inst.Shift)\n\t\t}\n\t\tif inst.Rn < 0 || inst.Rn > 31 {\n\t\t\treturn fmt.Errorf(\"CASP address register X%d is invalid\", inst.Rn)\n\t\t}\n\t\tvalidPairLow := func(reg int) bool {\n\t\t\t// Keep encoding 31 out of the VM pair transport until its architectural\n\t\t\t// register-31 behavior is independently proven. Exact-r29 uses 0..28.\n\t\t\treturn reg >= 0 && reg <= 28 && reg&1 == 0\n\t\t}\n\t\tif !validPairLow(inst.Rm) {\n\t\t\treturn fmt.Errorf(\"CASP expected/result pair low register %d is invalid\", inst.Rm)\n\t\t}\n\t\tif !validPairLow(inst.Rd) {\n\t\t\treturn fmt.Errorf(\"CASP replacement pair low register %d is invalid\", inst.Rd)\n\t\t}\n\t\treturn nil\n\t}\n""",
)

# Translator keeps OpAtomic 7-byte format and adds kind 12.
replace_once(
    "internal/arch/arm64/tr_atomic.go",
    "\tcase CAS:\n",
    "\tcase CAS, CASP:\n",
)
replace_once(
    "internal/arch/arm64/tr_atomic.go",
    "\t\tLDAR: 0, STLR: 1, LDADD: 2, CAS: 3,\n",
    "\t\tLDAR: 0, STLR: 1, LDADD: 2, CAS: 3, CASP: 12,\n",
)
replace_once(
    "internal/arch/arm64/translator.go",
    "case LDAR, STLR, LDADD, CAS, SWP, LDCLR, LDEOR, LDSET, LDSMAX, LDSMIN, LDUMAX, LDUMIN:",
    "case LDAR, STLR, LDADD, CAS, CASP, SWP, LDCLR, LDEOR, LDSET, LDSMAX, LDSMIN, LDUMAX, LDUMIN:",
)

# Compiler-derived gate: CASP is now required to close, not intentionally exempted.
regex_once(
    "internal/arch/arm64/compiler_corpus_test.go",
    r"\nvar exactR29CASPBoundaryRaws = map\[uint32\]bool\{.*?\n\}\n",
    "\n",
)
regex_once(
    "internal/arch/arm64/compiler_corpus_test.go",
    r"\n\tif record\.Profile == \"lse\" && record\.Function == \"vmp_atomic128\" && exactR29CASPBoundaryRaws\[record\.Raw\] &&.*?\n\t\}\n",
    "\n",
)
regex_once(
    "internal/arch/arm64/compiler_corpus_test.go",
    r"func TestCompilerIntentionalBoundaryRequiresExactEvidence\(t \*testing\.T\) \{\n\tcasp :=.*?\n\tcasp\.Raw \^= 1 << 16\n\tif _, ok := exactR29IntentionalBoundary\(casp, .*?\n\t\}\n\toutlined :=",
    "func TestCompilerIntentionalBoundaryRequiresExactEvidence(t *testing.T) {\n\toutlined :=",
)
replace_once(
    "internal/arch/arm64/compiler_corpus_test.go",
    'for _, kind := range []string{"casp128", "machine-outliner"} {',
    'for _, kind := range []string{"machine-outliner"} {',
)

# Dedicated two-register return ABI for CASP.
Path("internal/runtime/templates/android/arm64/vm_native.h").write_text("""#ifndef VMPACKER_VM_NATIVE_H\n#define VMPACKER_VM_NATIVE_H\n\ntypedef struct {\n  u64 lo;\n  u64 hi;\n} vm_atomic_pair_t;\n\nvoid vm_native_call(vm_ctx_t *vm, u64 target);\nu64 vm_atomic_native(u64 kind, u64 width, u64 order, u64 address, u64 first,\n                     u64 second);\nvm_atomic_pair_t vm_atomic_pair_native(u64 order, u64 width, u64 address,\n                                       u64 expected_lo, u64 expected_hi,\n                                       u64 new_lo, u64 new_hi);\n\n#endif\n""")

# Runtime bytecode handler branch for kind 12; scalar path is kept byte-for-byte below it.
handler = Path("internal/runtime/templates/android/arm64/vm_handlers/h_system.h")
text = handler.read_text()
start = text.index("static inline u32 h_atomic(vm_ctx_t *vm) {")
end = text.index("\nstatic inline int vm_prepare_native_call", start)
new_handler = r'''static inline u32 h_atomic(vm_ctx_t *vm) {
  u8 kind = vm->bc[vm->pc + 1];
  u8 width = vm->bc[vm->pc + 2];
  u8 order = vm->bc[vm->pc + 3];
  u8 rd = vm->bc[vm->pc + 4];
  u8 rn = vm->bc[vm->pc + 5];
  u8 rm = vm->bc[vm->pc + 6];

  if (kind == 12) {
    if ((width != 4 && width != 8) || order > 3 || rn > 31 || rd > 28 ||
        rm > 28 || (rd & 1u) != 0 || (rm & 1u) != 0) {
      vm->fault |= VM_FAULT_SYSTEM;
      return 7;
    }
    u64 address = vm->R[rn];
    u64 pair_bytes = (u64)width * 2u;
    if ((address & (pair_bytes - 1u)) != 0) {
      vm->fault |= VM_FAULT_SYSTEM;
      return 7;
    }
    vm_atomic_pair_t old = vm_atomic_pair_native(
        order, width, address, vm->R[rm], vm->R[rm + 1], vm->R[rd],
        vm->R[rd + 1]);
    vm_atomic_reg_write(vm, rm, old.lo, width);
    vm_atomic_reg_write(vm, rm + 1, old.hi, width);
    return 7;
  }

  if (kind > 11 || (width != 1 && width != 2 && width != 4 && width != 8) ||
      order > 3 || rn > 31) {
    vm->fault |= VM_FAULT_SYSTEM;
    return 7;
  }
  u64 address = vm->R[rn];
  u64 first = 0, second = 0;
  if (kind == 1)
    first = vm_atomic_reg_read(vm, rd);
  else if (kind == 2 || kind >= 4)
    first = vm_atomic_reg_read(vm, rm);
  else if (kind == 3) {
    first = vm_atomic_reg_read(vm, rm);
    second = vm_atomic_reg_read(vm, rd);
  }
  u64 old = vm_atomic_native(kind, width, order, address, first, second);
  if (kind == 0 || kind == 2 || kind >= 4)
    vm_atomic_reg_write(vm, rd, old, width);
  else if (kind == 3)
    vm_atomic_reg_write(vm, rm, old, width);
  return 7;
}
'''
handler.write_text(text[:start] + new_handler + text[end:])

# Fixed legal CASP scratch pairs. x0/x1 are the AAPCS64 pair return.
native = Path("internal/runtime/templates/android/arm64/vm_native.S")
text = native.read_text()
needle = "\t.size vm_atomic_native, .-vm_atomic_native\n\t.purgem VM_ATOMIC_ORDER4"
helper = r'''	.size vm_atomic_native, .-vm_atomic_native

	.p2align 2
	.global vm_atomic_pair_native
	.hidden vm_atomic_pair_native
	.type vm_atomic_pair_native, %function
vm_atomic_pair_native:
	.cfi_startproc
	bti c
	cmp w0, #3
	b.hi .Latomic_pair_bad
	cmp w1, #4
	b.eq .Latomic_pair_w
	cmp w1, #8
	b.ne .Latomic_pair_bad

	mov x8, x3
	mov x9, x4
	mov x10, x5
	mov x11, x6
	cbz w0, .Lpair_x_relaxed
	cmp w0, #1
	b.eq .Lpair_x_acquire
	cmp w0, #2
	b.eq .Lpair_x_release
	caspal x8, x9, x10, x11, [x2]
	b .Lpair_x_done
.Lpair_x_relaxed:
	casp x8, x9, x10, x11, [x2]
	b .Lpair_x_done
.Lpair_x_acquire:
	caspa x8, x9, x10, x11, [x2]
	b .Lpair_x_done
.Lpair_x_release:
	caspl x8, x9, x10, x11, [x2]
.Lpair_x_done:
	mov x0, x8
	mov x1, x9
	ret

.Latomic_pair_w:
	mov w8, w3
	mov w9, w4
	mov w10, w5
	mov w11, w6
	cbz w0, .Lpair_w_relaxed
	cmp w0, #1
	b.eq .Lpair_w_acquire
	cmp w0, #2
	b.eq .Lpair_w_release
	caspal w8, w9, w10, w11, [x2]
	b .Lpair_w_done
.Lpair_w_relaxed:
	casp w8, w9, w10, w11, [x2]
	b .Lpair_w_done
.Lpair_w_acquire:
	caspa w8, w9, w10, w11, [x2]
	b .Lpair_w_done
.Lpair_w_release:
	caspl w8, w9, w10, w11, [x2]
.Lpair_w_done:
	mov w0, w8
	mov w1, w9
	ret

.Latomic_pair_bad:
	mov x0, #0
	mov x1, #0
	ret
	.cfi_endproc
	.size vm_atomic_pair_native, .-vm_atomic_pair_native
	.purgem VM_ATOMIC_ORDER4'''
if needle not in text or text.count(needle) != 1:
    raise SystemExit("vm_atomic_native tail marker missing or duplicated")
native.write_text(text.replace(needle, helper, 1))
