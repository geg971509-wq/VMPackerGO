/* AAPCS64 token entry with explicit BTI, PAC, and unwind metadata. */

	.section .text.entry,"ax",@progbits
	.p2align 2
	.globl vm_entry_token
	.type vm_entry_token, %function
vm_entry_token:
	.cfi_startproc
	bti jc
	mov x9, x29
	mov x10, x30
	paciasp
	.cfi_negate_ra_state
	stp x29, x30, [sp, #-256]!
	.cfi_def_cfa_offset 256
	.cfi_offset x29, -256
	.cfi_offset x30, -248
	mov x29, sp
	.cfi_def_cfa_register x29
	stp x0, x1, [sp, #16]
	stp x2, x3, [sp, #32]
	stp x4, x5, [sp, #48]
	stp x6, x7, [sp, #64]
	stp x9, x10, [sp, #80]
	stp x19, x20, [sp, #96]
	stp x21, x22, [sp, #112]
	stp x23, x24, [sp, #128]
	stp x25, x26, [sp, #144]
	stp x27, x28, [sp, #160]
	umov x9, v8.d[0]
	str x9, [sp, #176]
	umov x9, v9.d[0]
	str x9, [sp, #184]
	umov x9, v10.d[0]
	str x9, [sp, #192]
	umov x9, v11.d[0]
	str x9, [sp, #200]
	umov x9, v12.d[0]
	str x9, [sp, #208]
	umov x9, v13.d[0]
	str x9, [sp, #216]
	umov x9, v14.d[0]
	str x9, [sp, #224]
	umov x9, v15.d[0]
	str x9, [sp, #232]
	add x0, sp, #16
	mov w1, w16
	bl vm_entry_token_inner
	ldp x29, x30, [sp], #256
	.cfi_def_cfa sp, 0
	.cfi_restore x29
	.cfi_restore x30
	autiasp
	.cfi_negate_ra_state
	ret
	.cfi_endproc
	.size vm_entry_token, .-vm_entry_token

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
