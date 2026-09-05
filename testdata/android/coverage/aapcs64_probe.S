.text
.global probe_aapcs64
.type probe_aapcs64, %function
probe_aapcs64:
    sub sp, sp, #128
    stp x29, x30, [sp]
    stp x19, x20, [sp, #16]
    stp x21, x22, [sp, #32]
    stp x23, x24, [sp, #48]
    stp x25, x26, [sp, #64]
    stp x27, x28, [sp, #80]
    str x0, [sp, #96]
    str x2, [sp, #104]
    str x3, [sp, #112]
    mov x19, #0x1900
    mov x20, #0x1a00
    mov x21, #0x1b00
    mov x22, #0x1c00
    mov x23, #0x1d00
    mov x24, #0x1e00
    mov x25, #0x1f00
    mov x26, #0x2000
    mov x27, #0x2100
    mov x28, #0x2200
    mov x29, #0x2300
    ldr x16, [sp, #96]
    mov x0, x1
    ldr x1, [sp, #104]
    blr x16
    ldr x18, [sp, #112]
    str x0, [x18]
    str x19, [x18, #8]
    str x20, [x18, #16]
    str x21, [x18, #24]
    str x22, [x18, #32]
    str x23, [x18, #40]
    str x24, [x18, #48]
    str x25, [x18, #56]
    str x26, [x18, #64]
    str x27, [x18, #72]
    str x28, [x18, #80]
    str x29, [x18, #88]
    mov x9, sp
    str x9, [x18, #96]
    ldr x0, [x18]
    ldp x29, x30, [sp]
    ldp x19, x20, [sp, #16]
    ldp x21, x22, [sp, #32]
    ldp x23, x24, [sp, #48]
    ldp x25, x26, [sp, #64]
    ldp x27, x28, [sp, #80]
    add sp, sp, #128
    ret
