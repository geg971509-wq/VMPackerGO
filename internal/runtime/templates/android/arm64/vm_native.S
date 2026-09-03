#include "vm_abi.h"

	.section .text.native,"ax",@progbits
	.p2align 2
	.global vm_native_call
	.hidden vm_native_call
	.type vm_native_call, %function
vm_native_call:
	.cfi_startproc
	bti c
	paciasp
	.cfi_negate_ra_state
	stp x29, x30, [sp, #-224]!
	.cfi_def_cfa_offset 224
	.cfi_offset x29, -224
	.cfi_offset x30, -216
	stp x19, x20, [sp, #16]
	stp x21, x22, [sp, #32]
	stp x23, x24, [sp, #48]
	stp x25, x26, [sp, #64]
	stp x27, x28, [sp, #80]
	.cfi_offset x19, -208
	.cfi_offset x20, -200
	.cfi_offset x21, -192
	.cfi_offset x22, -184
	.cfi_offset x23, -176
	.cfi_offset x24, -168
	.cfi_offset x25, -160
	.cfi_offset x26, -152
	.cfi_offset x27, -144
	.cfi_offset x28, -136
	stp q8, q9, [sp, #96]
	stp q10, q11, [sp, #128]
	stp q12, q13, [sp, #160]
	stp q14, q15, [sp, #192]
	.cfi_offset 72, -128
	.cfi_offset 73, -112
	.cfi_offset 74, -96
	.cfi_offset 75, -80
	.cfi_offset 76, -64
	.cfi_offset 77, -48
	.cfi_offset 78, -32
	.cfi_offset 79, -16

	mov x29, sp
	mov x28, sp
	.cfi_def_cfa x28, 224
	mov x19, x0
	mov x20, x1
	ldr x22, [x19, #(VM_CTX_R + 31 * 8)]
	ldr w9, [x19, #VM_CTX_FPCR]
	msr fpcr, x9
	ldr w9, [x19, #VM_CTX_FPSR]
	msr fpsr, x9
	ldp q0, q1, [x19, #(VM_CTX_V + 0 * 16)]
	ldp q2, q3, [x19, #(VM_CTX_V + 2 * 16)]
	ldp q4, q5, [x19, #(VM_CTX_V + 4 * 16)]
	ldp q6, q7, [x19, #(VM_CTX_V + 6 * 16)]
	mov sp, x22
	ldp x0, x1, [x19, #(VM_CTX_R + 0 * 8)]
	ldp x2, x3, [x19, #(VM_CTX_R + 2 * 8)]
	ldp x4, x5, [x19, #(VM_CTX_R + 4 * 8)]
	ldp x6, x7, [x19, #(VM_CTX_R + 6 * 8)]
	ldr x8, [x19, #(VM_CTX_R + 8 * 8)]
	blr x20
	mrs x9, nzcv
	mrs x10, fpcr
	mrs x11, fpsr
	mov sp, x28
	.cfi_def_cfa sp, 224
	stp x0, x1, [x19, #(VM_CTX_R + 0 * 8)]
	stp q0, q1, [x19, #(VM_CTX_V + 0 * 16)]
	stp q2, q3, [x19, #(VM_CTX_V + 2 * 16)]
	stp q4, q5, [x19, #(VM_CTX_V + 4 * 16)]
	stp q6, q7, [x19, #(VM_CTX_V + 6 * 16)]
	str w10, [x19, #VM_CTX_FPCR]
	str w11, [x19, #VM_CTX_FPSR]
	lsr x9, x9, #28
	str w9, [x19, #VM_CTX_FL]

	ldp q8, q9, [sp, #96]
	ldp q10, q11, [sp, #128]
	ldp q12, q13, [sp, #160]
	ldp q14, q15, [sp, #192]
	.cfi_restore 72
	.cfi_restore 73
	.cfi_restore 74
	.cfi_restore 75
	.cfi_restore 76
	.cfi_restore 77
	.cfi_restore 78
	.cfi_restore 79
	ldp x19, x20, [sp, #16]
	ldp x21, x22, [sp, #32]
	ldp x23, x24, [sp, #48]
	ldp x25, x26, [sp, #64]
	ldp x27, x28, [sp, #80]
	.cfi_restore x19
	.cfi_restore x20
	.cfi_restore x21
	.cfi_restore x22
	.cfi_restore x23
	.cfi_restore x24
	.cfi_restore x25
	.cfi_restore x26
	.cfi_restore x27
	.cfi_restore x28
	ldp x29, x30, [sp], #224
	.cfi_def_cfa sp, 0
	.cfi_restore x29
	.cfi_restore x30
	autiasp
	.cfi_negate_ra_state
	ret
	.cfi_endproc
	.size vm_native_call, .-vm_native_call

	/* Fixed-register native atomic helper. LSE instructions remain isolated in
	 * this function, so non-LSE inputs never execute an unsupported opcode. */
	.arch_extension lse
	.macro VM_ATOMIC_ORDER4 relaxed, acquire, release, acqrel, source, result
	cbz w2, 10f
	cmp w2, #1
	b.eq 11f
	cmp w2, #2
	b.eq 12f
	\acqrel \source, \result, [x3]
	ret
10:	\relaxed \source, \result, [x3]
	ret
11:	\acquire \source, \result, [x3]
	ret
12:	\release \source, \result, [x3]
	ret
	.endm

	.p2align 2
	.global vm_atomic_native
	.hidden vm_atomic_native
	.type vm_atomic_native, %function
vm_atomic_native:
	.cfi_startproc
	bti c
	cmp w0, #0
	b.eq .Latomic_load
	cmp w0, #1
	b.eq .Latomic_store
	cmp w0, #2
	b.eq .Latomic_add
	cmp w0, #3
	b.eq .Latomic_cas
	cmp w0, #4
	b.eq .Latomic_swp
	cmp w0, #5
	b.eq .Latomic_clr
	cmp w0, #6
	b.eq .Latomic_eor
	cmp w0, #7
	b.eq .Latomic_set
	cmp w0, #8
	b.eq .Latomic_smax
	cmp w0, #9
	b.eq .Latomic_smin
	cmp w0, #10
	b.eq .Latomic_umax
	cmp w0, #11
	b.eq .Latomic_umin
	b .Latomic_bad

.Latomic_load:
	cmp w1, #1
	b.eq .Lload_1
	cmp w1, #2
	b.eq .Lload_2
	cmp w1, #4
	b.eq .Lload_4
	cmp w1, #8
	b.ne .Latomic_bad
	ldar x0, [x3]
	ret
.Lload_1:
	ldarb w0, [x3]
	ret
.Lload_2:
	ldarh w0, [x3]
	ret
.Lload_4:
	ldar w0, [x3]
	ret

.Latomic_store:
	cmp w1, #1
	b.eq .Lstore_1
	cmp w1, #2
	b.eq .Lstore_2
	cmp w1, #4
	b.eq .Lstore_4
	cmp w1, #8
	b.ne .Latomic_bad
	stlr x4, [x3]
	mov x0, #0
	ret
.Lstore_1:
	stlrb w4, [x3]
	mov x0, #0
	ret
.Lstore_2:
	stlrh w4, [x3]
	mov x0, #0
	ret
.Lstore_4:
	stlr w4, [x3]
	mov x0, #0
	ret

.Latomic_add:
	cmp w1, #1
	b.eq .Ladd_1
	cmp w1, #2
	b.eq .Ladd_2
	cmp w1, #4
	b.eq .Ladd_4
	cmp w1, #8
	b.ne .Latomic_bad
	VM_ATOMIC_ORDER4 ldadd, ldadda, ldaddl, ldaddal, x4, x0
.Ladd_1:
	VM_ATOMIC_ORDER4 ldaddb, ldaddab, ldaddlb, ldaddalb, w4, w0
.Ladd_2:
	VM_ATOMIC_ORDER4 ldaddh, ldaddah, ldaddlh, ldaddalh, w4, w0
.Ladd_4:
	VM_ATOMIC_ORDER4 ldadd, ldadda, ldaddl, ldaddal, w4, w0

.Latomic_cas:
	mov x0, x4
	cmp w1, #1
	b.eq .Lcas_1
	cmp w1, #2
	b.eq .Lcas_2
	cmp w1, #4
	b.eq .Lcas_4
	cmp w1, #8
	b.ne .Latomic_bad
	VM_ATOMIC_ORDER4 cas, casa, casl, casal, x0, x5
.Lcas_1:
	VM_ATOMIC_ORDER4 casb, casab, caslb, casalb, w0, w5
.Lcas_2:
	VM_ATOMIC_ORDER4 cash, casah, caslh, casalh, w0, w5
.Lcas_4:
	VM_ATOMIC_ORDER4 cas, casa, casl, casal, w0, w5

.Latomic_swp:
	cmp w1, #1
	b.eq .Lswp_1
	cmp w1, #2
	b.eq .Lswp_2
	cmp w1, #4
	b.eq .Lswp_4
	cmp w1, #8
	b.ne .Latomic_bad
	VM_ATOMIC_ORDER4 swp, swpa, swpl, swpal, x4, x0
.Lswp_1:
	VM_ATOMIC_ORDER4 swpb, swpab, swplb, swpalb, w4, w0
.Lswp_2:
	VM_ATOMIC_ORDER4 swph, swpah, swplh, swpalh, w4, w0
.Lswp_4:
	VM_ATOMIC_ORDER4 swp, swpa, swpl, swpal, w4, w0

.Latomic_clr:
	cmp w1, #1
	b.eq .Lclr_1
	cmp w1, #2
	b.eq .Lclr_2
	cmp w1, #4
	b.eq .Lclr_4
	cmp w1, #8
	b.ne .Latomic_bad
	VM_ATOMIC_ORDER4 ldclr, ldclra, ldclrl, ldclral, x4, x0
.Lclr_1:
	VM_ATOMIC_ORDER4 ldclrb, ldclrab, ldclrlb, ldclralb, w4, w0
.Lclr_2:
	VM_ATOMIC_ORDER4 ldclrh, ldclrah, ldclrlh, ldclralh, w4, w0
.Lclr_4:
	VM_ATOMIC_ORDER4 ldclr, ldclra, ldclrl, ldclral, w4, w0

.Latomic_eor:
	cmp w1, #1
	b.eq .Leor_1
	cmp w1, #2
	b.eq .Leor_2
	cmp w1, #4
	b.eq .Leor_4
	cmp w1, #8
	b.ne .Latomic_bad
	VM_ATOMIC_ORDER4 ldeor, ldeora, ldeorl, ldeoral, x4, x0
.Leor_1:
	VM_ATOMIC_ORDER4 ldeorb, ldeorab, ldeorlb, ldeoralb, w4, w0
.Leor_2:
	VM_ATOMIC_ORDER4 ldeorh, ldeorah, ldeorlh, ldeoralh, w4, w0
.Leor_4:
	VM_ATOMIC_ORDER4 ldeor, ldeora, ldeorl, ldeoral, w4, w0

.Latomic_set:
	cmp w1, #1
	b.eq .Lset_1
	cmp w1, #2
	b.eq .Lset_2
	cmp w1, #4
	b.eq .Lset_4
	cmp w1, #8
	b.ne .Latomic_bad
	VM_ATOMIC_ORDER4 ldset, ldseta, ldsetl, ldsetal, x4, x0
.Lset_1:
	VM_ATOMIC_ORDER4 ldsetb, ldsetab, ldsetlb, ldsetalb, w4, w0
.Lset_2:
	VM_ATOMIC_ORDER4 ldseth, ldsetah, ldsetlh, ldsetalh, w4, w0
.Lset_4:
	VM_ATOMIC_ORDER4 ldset, ldseta, ldsetl, ldsetal, w4, w0

.Latomic_smax:
	cmp w1, #1
	b.eq .Lsmax_1
	cmp w1, #2
	b.eq .Lsmax_2
	cmp w1, #4
	b.eq .Lsmax_4
	cmp w1, #8
	b.ne .Latomic_bad
	VM_ATOMIC_ORDER4 ldsmax, ldsmaxa, ldsmaxl, ldsmaxal, x4, x0
.Lsmax_1:
	VM_ATOMIC_ORDER4 ldsmaxb, ldsmaxab, ldsmaxlb, ldsmaxalb, w4, w0
.Lsmax_2:
	VM_ATOMIC_ORDER4 ldsmaxh, ldsmaxah, ldsmaxlh, ldsmaxalh, w4, w0
.Lsmax_4:
	VM_ATOMIC_ORDER4 ldsmax, ldsmaxa, ldsmaxl, ldsmaxal, w4, w0

.Latomic_smin:
	cmp w1, #1
	b.eq .Lsmin_1
	cmp w1, #2
	b.eq .Lsmin_2
	cmp w1, #4
	b.eq .Lsmin_4
	cmp w1, #8
	b.ne .Latomic_bad
	VM_ATOMIC_ORDER4 ldsmin, ldsmina, ldsminl, ldsminal, x4, x0
.Lsmin_1:
	VM_ATOMIC_ORDER4 ldsminb, ldsminab, ldsminlb, ldsminalb, w4, w0
.Lsmin_2:
	VM_ATOMIC_ORDER4 ldsminh, ldsminah, ldsminlh, ldsminalh, w4, w0
.Lsmin_4:
	VM_ATOMIC_ORDER4 ldsmin, ldsmina, ldsminl, ldsminal, w4, w0

.Latomic_umax:
	cmp w1, #1
	b.eq .Lumax_1
	cmp w1, #2
	b.eq .Lumax_2
	cmp w1, #4
	b.eq .Lumax_4
	cmp w1, #8
	b.ne .Latomic_bad
	VM_ATOMIC_ORDER4 ldumax, ldumaxa, ldumaxl, ldumaxal, x4, x0
.Lumax_1:
	VM_ATOMIC_ORDER4 ldumaxb, ldumaxab, ldumaxlb, ldumaxalb, w4, w0
.Lumax_2:
	VM_ATOMIC_ORDER4 ldumaxh, ldumaxah, ldumaxlh, ldumaxalh, w4, w0
.Lumax_4:
	VM_ATOMIC_ORDER4 ldumax, ldumaxa, ldumaxl, ldumaxal, w4, w0

.Latomic_umin:
	cmp w1, #1
	b.eq .Lumin_1
	cmp w1, #2
	b.eq .Lumin_2
	cmp w1, #4
	b.eq .Lumin_4
	cmp w1, #8
	b.ne .Latomic_bad
	VM_ATOMIC_ORDER4 ldumin, ldumina, lduminl, lduminal, x4, x0
.Lumin_1:
	VM_ATOMIC_ORDER4 lduminb, lduminab, lduminlb, lduminalb, w4, w0
.Lumin_2:
	VM_ATOMIC_ORDER4 lduminh, lduminah, lduminlh, lduminalh, w4, w0
.Lumin_4:
	VM_ATOMIC_ORDER4 ldumin, ldumina, lduminl, lduminal, w4, w0

.Latomic_bad:
	mov x0, #0
	ret
	.cfi_endproc
	.size vm_atomic_native, .-vm_atomic_native
	.purgem VM_ATOMIC_ORDER4

	.section .note.gnu.property,"a",@note
	.p2align 3
	.long 4
	.long 16
	.long 5
	.asciz "GNU"
	.p2align 3
	.long 0xc0000000
	.long 4
	.long 3
	.long 0

	.section .note.GNU-stack,"",@progbits
