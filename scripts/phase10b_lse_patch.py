#!/usr/bin/env python3
from pathlib import Path


def replace_once(path: str, old: str, new: str) -> None:
    p = Path(path)
    text = p.read_text()
    count = text.count(old)
    if count != 1:
        raise SystemExit(f"{path}: expected exactly one match, found {count}\n--- expected ---\n{old}")
    p.write_text(text.replace(old, new, 1))


# Decoder semantic operations and names.
replace_once(
    "internal/arch/arm64/decoder.go",
    "\tSWP\n\tLDCLR\n\tLDEOR\n\tLDSET\n\tUNSUPPORTED\n",
    "\tSWP\n\tLDCLR\n\tLDEOR\n\tLDSET\n\tLDSMAX\n\tLDSMIN\n\tLDUMAX\n\tLDUMIN\n\tUNSUPPORTED\n",
)
replace_once(
    "internal/arch/arm64/decoder.go",
    "\t\tSWP: \"SWP\", LDCLR: \"LDCLR\", LDEOR: \"LDEOR\", LDSET: \"LDSET\",\n",
    "\t\tSWP: \"SWP\", LDCLR: \"LDCLR\", LDEOR: \"LDEOR\", LDSET: \"LDSET\",\n\t\tLDSMAX: \"LDSMAX\", LDSMIN: \"LDSMIN\", LDUMAX: \"LDUMAX\", LDUMIN: \"LDUMIN\",\n",
)

# Decoder table: remaining scalar FEAT_LSE min/max RMW families.
replace_once(
    "internal/arch/arm64/decode_lse_atomic.go",
    '''\t{
\t\tName: "LDSET", Mask: 0x3F20FC00, Value: 0x38203000, Op: LDSET,
\t\tFields: []FieldDef{{Name: "size", Hi: 31, Lo: 30}, fRm16, fRn, fRd},
\t\tPost:   postLdadd,
\t},
}
''',
    '''\t{
\t\tName: "LDSET", Mask: 0x3F20FC00, Value: 0x38203000, Op: LDSET,
\t\tFields: []FieldDef{{Name: "size", Hi: 31, Lo: 30}, fRm16, fRn, fRd},
\t\tPost:   postLdadd,
\t},
\t{
\t\tName: "LDSMAX", Mask: 0x3F20FC00, Value: 0x38204000, Op: LDSMAX,
\t\tFields: []FieldDef{{Name: "size", Hi: 31, Lo: 30}, fRm16, fRn, fRd},
\t\tPost:   postLdadd,
\t},
\t{
\t\tName: "LDSMIN", Mask: 0x3F20FC00, Value: 0x38205000, Op: LDSMIN,
\t\tFields: []FieldDef{{Name: "size", Hi: 31, Lo: 30}, fRm16, fRn, fRd},
\t\tPost:   postLdadd,
\t},
\t{
\t\tName: "LDUMAX", Mask: 0x3F20FC00, Value: 0x38206000, Op: LDUMAX,
\t\tFields: []FieldDef{{Name: "size", Hi: 31, Lo: 30}, fRm16, fRn, fRd},
\t\tPost:   postLdadd,
\t},
\t{
\t\tName: "LDUMIN", Mask: 0x3F20FC00, Value: 0x38207000, Op: LDUMIN,
\t\tFields: []FieldDef{{Name: "size", Hi: 31, Lo: 30}, fRm16, fRn, fRd},
\t\tPost:   postLdadd,
\t},
}
''',
)
replace_once(
    "internal/arch/arm64/decode_lse_atomic.go",
    "\tcase LDADD, SWP, LDCLR, LDEOR, LDSET:\n",
    "\tcase LDADD, SWP, LDCLR, LDEOR, LDSET, LDSMAX, LDSMIN, LDUMAX, LDUMIN:\n",
)

# Product policy and translator routing.
replace_once(
    "internal/arch/arm64/policy.go",
    "\tallow([]Op{LDAR, STLR, LDADD, CAS, SWP, LDCLR, LDEOR, LDSET}, validateAtomicNative)\n",
    "\tallow([]Op{LDAR, STLR, LDADD, CAS, SWP, LDCLR, LDEOR, LDSET, LDSMAX, LDSMIN, LDUMAX, LDUMIN}, validateAtomicNative)\n",
)
replace_once(
    "internal/arch/arm64/translator.go",
    "\tcase LDAR, STLR, LDADD, CAS, SWP, LDCLR, LDEOR, LDSET:\n\t\treturn 0, t.trAtomic(inst)\n",
    "\tcase LDAR, STLR, LDADD, CAS, SWP, LDCLR, LDEOR, LDSET, LDSMAX, LDSMIN, LDUMAX, LDUMIN:\n\t\treturn 0, t.trAtomic(inst)\n",
)

# Reuse OpAtomic: extend only kind values and shared memory-order handling.
replace_once(
    "internal/arch/arm64/tr_atomic.go",
    "\tcase LDADD, SWP, LDCLR, LDEOR, LDSET:\n",
    "\tcase LDADD, SWP, LDCLR, LDEOR, LDSET, LDSMAX, LDSMIN, LDUMAX, LDUMIN:\n",
)
replace_once(
    "internal/arch/arm64/tr_atomic.go",
    "\t\tSWP: 4, LDCLR: 5, LDEOR: 6, LDSET: 7,\n",
    "\t\tSWP: 4, LDCLR: 5, LDEOR: 6, LDSET: 7,\n\t\tLDSMAX: 8, LDSMIN: 9, LDUMAX: 10, LDUMIN: 11,\n",
)

# Interpreter bytecode format stays seven bytes; only kind range grows.
replace_once(
    "internal/runtime/templates/android/arm64/vm_handlers/h_system.h",
    "  if (kind > 7 || (width != 1 && width != 2 && width != 4 && width != 8) ||\n",
    "  if (kind > 11 || (width != 1 && width != 2 && width != 4 && width != 8) ||\n",
)

# Native FEAT_LSE dispatcher and min/max implementations.
replace_once(
    "internal/runtime/templates/android/arm64/vm_native.S",
    '''\tcmp w0, #7
\tb.eq .Latomic_set
\tb .Latomic_bad
''',
    '''\tcmp w0, #7
\tb.eq .Latomic_set
\tcmp w0, #8
\tb.eq .Latomic_smax
\tcmp w0, #9
\tb.eq .Latomic_smin
\tcmp w0, #10
\tb.eq .Latomic_umax
\tcmp w0, #11
\tb.eq .Latomic_umin
\tb .Latomic_bad
''',
)
replace_once(
    "internal/runtime/templates/android/arm64/vm_native.S",
    '''.Lset_4:
\tVM_ATOMIC_ORDER4 ldset, ldseta, ldsetl, ldsetal, w4, w0

.Latomic_bad:
''',
    '''.Lset_4:
\tVM_ATOMIC_ORDER4 ldset, ldseta, ldsetl, ldsetal, w4, w0

.Latomic_smax:
\tcmp w1, #1
\tb.eq .Lsmax_1
\tcmp w1, #2
\tb.eq .Lsmax_2
\tcmp w1, #4
\tb.eq .Lsmax_4
\tcmp w1, #8
\tb.ne .Latomic_bad
\tVM_ATOMIC_ORDER4 ldsmax, ldsmaxa, ldsmaxl, ldsmaxal, x4, x0
.Lsmax_1:
\tVM_ATOMIC_ORDER4 ldsmaxb, ldsmaxab, ldsmaxlb, ldsmaxalb, w4, w0
.Lsmax_2:
\tVM_ATOMIC_ORDER4 ldsmaxh, ldsmaxah, ldsmaxlh, ldsmaxalh, w4, w0
.Lsmax_4:
\tVM_ATOMIC_ORDER4 ldsmax, ldsmaxa, ldsmaxl, ldsmaxal, w4, w0

.Latomic_smin:
\tcmp w1, #1
\tb.eq .Lsmin_1
\tcmp w1, #2
\tb.eq .Lsmin_2
\tcmp w1, #4
\tb.eq .Lsmin_4
\tcmp w1, #8
\tb.ne .Latomic_bad
\tVM_ATOMIC_ORDER4 ldsmin, ldsmina, ldsminl, ldsminal, x4, x0
.Lsmin_1:
\tVM_ATOMIC_ORDER4 ldsminb, ldsminab, ldsminlb, ldsminalb, w4, w0
.Lsmin_2:
\tVM_ATOMIC_ORDER4 ldsminh, ldsminah, ldsminlh, ldsminalh, w4, w0
.Lsmin_4:
\tVM_ATOMIC_ORDER4 ldsmin, ldsmina, ldsminl, ldsminal, w4, w0

.Latomic_umax:
\tcmp w1, #1
\tb.eq .Lumax_1
\tcmp w1, #2
\tb.eq .Lumax_2
\tcmp w1, #4
\tb.eq .Lumax_4
\tcmp w1, #8
\tb.ne .Latomic_bad
\tVM_ATOMIC_ORDER4 ldumax, ldumaxa, ldumaxl, ldumaxal, x4, x0
.Lumax_1:
\tVM_ATOMIC_ORDER4 ldumaxb, ldumaxab, ldumaxlb, ldumaxalb, w4, w0
.Lumax_2:
\tVM_ATOMIC_ORDER4 ldumaxh, ldumaxah, ldumaxlh, ldumaxalh, w4, w0
.Lumax_4:
\tVM_ATOMIC_ORDER4 ldumax, ldumaxa, ldumaxl, ldumaxal, w4, w0

.Latomic_umin:
\tcmp w1, #1
\tb.eq .Lumin_1
\tcmp w1, #2
\tb.eq .Lumin_2
\tcmp w1, #4
\tb.eq .Lumin_4
\tcmp w1, #8
\tb.ne .Latomic_bad
\tVM_ATOMIC_ORDER4 ldumin, ldumina, lduminl, lduminal, x4, x0
.Lumin_1:
\tVM_ATOMIC_ORDER4 lduminb, lduminab, lduminlb, lduminalb, w4, w0
.Lumin_2:
\tVM_ATOMIC_ORDER4 lduminh, lduminah, lduminlh, lduminalh, w4, w0
.Lumin_4:
\tVM_ATOMIC_ORDER4 ldumin, ldumina, lduminl, lduminal, w4, w0

.Latomic_bad:
''',
)

# Extend the existing focused semantic matrix; keep one source of truth.
replace_once(
    "internal/arch/arm64/lse_atomic_test.go",
    '''\t\t{"LDSET", 0x38203000, LDSET},
\t}
''',
    '''\t\t{"LDSET", 0x38203000, LDSET},
\t\t{"LDSMAX", 0x38204000, LDSMAX},
\t\t{"LDSMIN", 0x38205000, LDSMIN},
\t\t{"LDUMAX", 0x38206000, LDUMAX},
\t\t{"LDUMIN", 0x38207000, LDUMIN},
\t}
''',
)
replace_once(
    "internal/arch/arm64/lse_atomic_test.go",
    '''\t\t{vm.Instruction{Op: int(LDSET), Rd: 10, Rn: 11, Rm: 12, Shift: 4}, []byte{7, 4, 0, 10, 11, 12}},
\t} {
''',
    '''\t\t{vm.Instruction{Op: int(LDSET), Rd: 10, Rn: 11, Rm: 12, Shift: 4}, []byte{7, 4, 0, 10, 11, 12}},
\t\t{vm.Instruction{Op: int(LDSMAX), Rd: 13, Rn: 14, Rm: 15, Shift: 8, Raw: 1 << 23}, []byte{8, 8, 1, 13, 14, 15}},
\t\t{vm.Instruction{Op: int(LDSMIN), Rd: 16, Rn: 17, Rm: 18, Shift: 4, Raw: 1 << 22}, []byte{9, 4, 2, 16, 17, 18}},
\t\t{vm.Instruction{Op: int(LDUMAX), Rd: 19, Rn: 20, Rm: 21, Shift: 2, Raw: 3 << 22}, []byte{10, 2, 3, 19, 20, 21}},
\t\t{vm.Instruction{Op: int(LDUMIN), Rd: 22, Rn: 23, Rm: 24, Shift: 1}, []byte{11, 1, 0, 22, 23, 24}},
\t} {
''',
)
replace_once(
    "internal/arch/arm64/lse_atomic_test.go",
    "\tfor _, op := range []Op{LDADD, SWP, LDCLR, LDEOR, LDSET} {\n",
    "\tfor _, op := range []Op{LDADD, SWP, LDCLR, LDEOR, LDSET, LDSMAX, LDSMIN, LDUMAX, LDUMIN} {\n",
)
replace_once(
    "internal/arch/arm64/lse_atomic_test.go",
    "\tfor _, op := range []Op{SWP, LDCLR, LDEOR, LDSET} {\n",
    "\tfor _, op := range []Op{SWP, LDCLR, LDEOR, LDSET, LDSMAX, LDSMIN, LDUMAX, LDUMIN} {\n",
)

print("phase10b LSE min/max patch applied")
