/* AArch64 compare, condition, and conditional-compare handlers. */
#ifndef H_CMP_H
#define H_CMP_H

#include "../vm_decode.h"
#include "../vm_types.h"

static inline int vm_cond_holds(vm_ctx_t *vm, u8 cond) {
  u32 nzcv = vm->FL;
  u32 n = !!(nzcv & FL_N), z = !!(nzcv & FL_Z);
  u32 c = !!(nzcv & FL_C), v = !!(nzcv & FL_V);
  switch (cond & 0xFu) {
  case 0x0: return z;                 /* EQ */
  case 0x1: return !z;                /* NE */
  case 0x2: return c;                 /* CS/HS */
  case 0x3: return !c;                /* CC/LO */
  case 0x4: return n;                 /* MI */
  case 0x5: return !n;                /* PL */
  case 0x6: return v;                 /* VS */
  case 0x7: return !v;                /* VC */
  case 0x8: return c && !z;           /* HI */
  case 0x9: return !c || z;           /* LS */
  case 0xA: return n == v;            /* GE */
  case 0xB: return n != v;            /* LT */
  case 0xC: return !z && (n == v);    /* GT */
  case 0xD: return z || (n != v);     /* LE */
  case 0xE: return 1;                 /* AL */
  default: return 1;                  /* NV */
  }
}

static inline void vm_set_nzcv(vm_ctx_t *vm, u8 nzcv) {
  vm->FL = nzcv & 0xFu;
}

/* Legacy compare bytecodes are 64-bit. New translated flag-setting
 * arithmetic uses the width-aware stack opcodes below. */
static inline u32 h_cmp(vm_ctx_t *vm) {
  u8 a = vm->bc[vm->pc + 1], b = vm->bc[vm->pc + 2];
  (void)vm_sub_flags(vm, vm->R[a & 31], vm->R[b & 31], 1, 1);
  return 3;
}

static inline u32 h_cmp_imm(vm_ctx_t *vm) {
  u8 r = vm->bc[vm->pc + 1];
  u32 imm = rd32(&vm->bc[vm->pc + 2]);
  (void)vm_sub_flags(vm, vm->R[r & 31], (u64)imm, 1, 1);
  return 6;
}

static inline u32 h_ccmp_reg(vm_ctx_t *vm) {
  u8 cond = vm->bc[vm->pc + 1], nzcv = vm->bc[vm->pc + 2];
  u8 rn = vm->bc[vm->pc + 3], rm = vm->bc[vm->pc + 4];
  u8 sf = vm->bc[vm->pc + 5] & 1u;
  if (vm_cond_holds(vm, cond))
    (void)vm_sub_flags(vm, vm->R[rn & 31], vm->R[rm & 31], 1, sf);
  else
    vm_set_nzcv(vm, nzcv);
  return 6;
}

static inline u32 h_ccmp_imm(vm_ctx_t *vm) {
  u8 cond = vm->bc[vm->pc + 1], nzcv = vm->bc[vm->pc + 2];
  u8 rn = vm->bc[vm->pc + 3], imm5 = vm->bc[vm->pc + 4] & 0x1Fu;
  u8 sf = vm->bc[vm->pc + 5] & 1u;
  if (vm_cond_holds(vm, cond))
    (void)vm_sub_flags(vm, vm->R[rn & 31], (u64)imm5, 1, sf);
  else
    vm_set_nzcv(vm, nzcv);
  return 6;
}

static inline u32 h_ccmn_reg(vm_ctx_t *vm) {
  u8 cond = vm->bc[vm->pc + 1], nzcv = vm->bc[vm->pc + 2];
  u8 rn = vm->bc[vm->pc + 3], rm = vm->bc[vm->pc + 4];
  u8 sf = vm->bc[vm->pc + 5] & 1u;
  if (vm_cond_holds(vm, cond))
    (void)vm_add_flags(vm, vm->R[rn & 31], vm->R[rm & 31], 0, sf);
  else
    vm_set_nzcv(vm, nzcv);
  return 6;
}

static inline u32 h_ccmn_imm(vm_ctx_t *vm) {
  u8 cond = vm->bc[vm->pc + 1], nzcv = vm->bc[vm->pc + 2];
  u8 rn = vm->bc[vm->pc + 3], imm5 = vm->bc[vm->pc + 4] & 0x1Fu;
  u8 sf = vm->bc[vm->pc + 5] & 1u;
  if (vm_cond_holds(vm, cond))
    (void)vm_add_flags(vm, vm->R[rn & 31], (u64)imm5, 0, sf);
  else
    vm_set_nzcv(vm, nzcv);
  return 6;
}

#endif
