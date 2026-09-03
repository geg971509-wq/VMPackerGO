/*
 * h_stack_ops.h — stack-machine handlers with explicit overflow/underflow
 * faults. No stack failure is converted into a normal zero operand.
 */
#ifndef H_STACK_OPS_H
#define H_STACK_OPS_H

#include "../vm_decode.h"
#include "../vm_types.h"

static inline void vm_eval_push(vm_ctx_t *vm, u64 value) {
  if (__builtin_expect(vm->eval_sp >= VM_EVAL_STACK_SIZE - 1, 0)) {
    vm_fault_set(vm, VM_FAULT_EVAL_STACK);
    return;
  }
  vm->eval_stk[++vm->eval_sp] = value;
}

static inline u64 vm_eval_pop(vm_ctx_t *vm) {
  if (__builtin_expect(vm->eval_sp < 0, 0)) {
    vm_fault_set(vm, VM_FAULT_EVAL_STACK);
    return 0;
  }
  return vm->eval_stk[vm->eval_sp--];
}

static inline u64 vm_eval_peek(vm_ctx_t *vm) {
  if (__builtin_expect(vm->eval_sp < 0, 0)) {
    vm_fault_set(vm, VM_FAULT_EVAL_STACK);
    return 0;
  }
  return vm->eval_stk[vm->eval_sp];
}

#define SPUSH(vm, value) vm_eval_push((vm), (u64)(value))
#define SPOP(vm) vm_eval_pop((vm))
#define SPEEK(vm) vm_eval_peek((vm))

static inline u32 h_s_vload(vm_ctx_t *vm) {
  u8 r = vm->bc[vm->pc + 1];
  SPUSH(vm, vm->R[r & 31]);
  return 2;
}

static inline u32 h_s_vstore(vm_ctx_t *vm) {
  u8 r = vm->bc[vm->pc + 1];
  vm->R[r & 31] = SPOP(vm);
  return 2;
}

static inline u32 h_s_push_imm32(vm_ctx_t *vm) {
  SPUSH(vm, rd32(&vm->bc[vm->pc + 1]));
  return 5;
}

static inline u32 h_s_push_imm64(vm_ctx_t *vm) {
  SPUSH(vm, rd64(&vm->bc[vm->pc + 1]));
  return 9;
}

static inline u32 h_s_push_image(vm_ctx_t *vm) {
  i64 delta = (i64)rd64(&vm->bc[vm->pc + 1]);
  SPUSH(vm, vm->image_anchor + (u64)delta);
  return 9;
}

static inline u32 h_s_dup(vm_ctx_t *vm) {
  u64 value = SPEEK(vm);
  SPUSH(vm, value);
  return 1;
}

static inline u32 h_s_swap(vm_ctx_t *vm) {
  u64 a = SPOP(vm);
  u64 b = SPOP(vm);
  SPUSH(vm, a);
  SPUSH(vm, b);
  return 1;
}

static inline u32 h_s_drop(vm_ctx_t *vm) {
  (void)SPOP(vm);
  return 1;
}

static inline u32 h_s_add(vm_ctx_t *vm) {
  u64 b = SPOP(vm), a = SPOP(vm);
  SPUSH(vm, a + b);
  return 1;
}

static inline u32 h_s_sub(vm_ctx_t *vm) {
  u64 b = SPOP(vm), a = SPOP(vm);
  SPUSH(vm, a - b);
  return 1;
}

static inline u32 h_s_mul(vm_ctx_t *vm) {
  u64 b = SPOP(vm), a = SPOP(vm);
  SPUSH(vm, a * b);
  return 1;
}

static inline u32 h_s_xor(vm_ctx_t *vm) {
  u64 b = SPOP(vm), a = SPOP(vm);
  SPUSH(vm, a ^ b);
  return 1;
}

static inline u32 h_s_and(vm_ctx_t *vm) {
  u64 b = SPOP(vm), a = SPOP(vm);
  SPUSH(vm, a & b);
  return 1;
}

static inline u32 h_s_or(vm_ctx_t *vm) {
  u64 b = SPOP(vm), a = SPOP(vm);
  SPUSH(vm, a | b);
  return 1;
}

static inline u32 h_s_shl(vm_ctx_t *vm) {
  u64 b = SPOP(vm), a = SPOP(vm);
  SPUSH(vm, a << (b & 63));
  return 1;
}

static inline u32 h_s_shr(vm_ctx_t *vm) {
  u64 b = SPOP(vm), a = SPOP(vm);
  SPUSH(vm, a >> (b & 63));
  return 1;
}

static inline u32 h_s_asr(vm_ctx_t *vm) {
  u64 b = SPOP(vm), a = SPOP(vm);
  SPUSH(vm, (u64)((i64)a >> (b & 63)));
  return 1;
}

static inline u32 h_s_ror(vm_ctx_t *vm) {
  u64 bits = SPOP(vm);
  u64 shift = SPOP(vm);
  u64 value = SPOP(vm);
  if (vm->fault)
    return 1;
  if (bits != 32 && bits != 64) {
    vm_fault_set(vm, VM_FAULT_BYTECODE);
    return 1;
  }
  shift &= bits - 1;
  if (bits == 32)
    value &= 0xFFFFFFFFULL;
  if (shift != 0)
    value = (value >> shift) | (value << (bits - shift));
  if (bits == 32)
    value &= 0xFFFFFFFFULL;
  SPUSH(vm, value);
  return 1;
}

static inline u32 h_s_umulh(vm_ctx_t *vm) {
  u64 b = SPOP(vm), a = SPOP(vm);
  __uint128_t result = (__uint128_t)a * (__uint128_t)b;
  SPUSH(vm, (u64)(result >> 64));
  return 1;
}

static inline u32 h_s_smulh(vm_ctx_t *vm) {
  u64 b = SPOP(vm), a = SPOP(vm);
  __int128 result = (__int128)(i64)a * (__int128)(i64)b;
  SPUSH(vm, (u64)((unsigned __int128)result >> 64));
  return 1;
}

static inline u32 h_s_udiv(vm_ctx_t *vm) {
  u64 b = SPOP(vm), a = SPOP(vm);
  SPUSH(vm, b == 0 ? 0 : a / b);
  return 1;
}

static inline u32 h_s_sdiv(vm_ctx_t *vm) {
  u64 b = SPOP(vm), a = SPOP(vm);
  SPUSH(vm, vm_sdiv64(a, b));
  return 1;
}

static inline u32 h_s_adc(vm_ctx_t *vm) {
  u64 b = SPOP(vm), a = SPOP(vm);
  u64 carry = (vm->FL & FL_C) ? 1 : 0;
  SPUSH(vm, a + b + carry);
  return 1;
}

static inline u32 h_s_sbc(vm_ctx_t *vm) {
  u64 b = SPOP(vm), a = SPOP(vm);
  u64 carry = (vm->FL & FL_C) ? 1 : 0;
  SPUSH(vm, a - b - (1 - carry));
  return 1;
}

static inline u32 h_s_add_flags(vm_ctx_t *vm) {
  u8 sf = vm->bc[vm->pc + 1] & 1u;
  u64 b = SPOP(vm), a = SPOP(vm);
  SPUSH(vm, vm_add_flags(vm, a, b, 0, sf));
  return 2;
}

static inline u32 h_s_sub_flags(vm_ctx_t *vm) {
  u8 sf = vm->bc[vm->pc + 1] & 1u;
  u64 b = SPOP(vm), a = SPOP(vm);
  SPUSH(vm, vm_sub_flags(vm, a, b, 1, sf));
  return 2;
}

static inline u32 h_s_and_flags(vm_ctx_t *vm) {
  u8 sf = vm->bc[vm->pc + 1] & 1u;
  u64 b = SPOP(vm), a = SPOP(vm);
  u64 result = (a & b) & vm_width_mask(sf);
  vm_set_nz(vm, result, sf);
  SPUSH(vm, result);
  return 2;
}

static inline u32 h_s_adc_flags(vm_ctx_t *vm) {
  u8 sf = vm->bc[vm->pc + 1] & 1u;
  u32 carry = !!(vm->FL & FL_C);
  u64 b = SPOP(vm), a = SPOP(vm);
  SPUSH(vm, vm_add_flags(vm, a, b, carry, sf));
  return 2;
}

static inline u32 h_s_sbc_flags(vm_ctx_t *vm) {
  u8 sf = vm->bc[vm->pc + 1] & 1u;
  u32 carry = !!(vm->FL & FL_C);
  u64 b = SPOP(vm), a = SPOP(vm);
  SPUSH(vm, vm_sub_flags(vm, a, b, carry, sf));
  return 2;
}

static inline u32 h_s_not(vm_ctx_t *vm) {
  SPUSH(vm, ~SPOP(vm));
  return 1;
}

static inline u32 h_s_clz(vm_ctx_t *vm) {
  u64 value = SPOP(vm);
  SPUSH(vm, value == 0 ? 64 : (u64)__builtin_clzll(value));
  return 1;
}

static inline u32 h_s_cls(vm_ctx_t *vm) {
  u64 value = SPOP(vm);
  if (value == 0 || value == ~(u64)0) {
    SPUSH(vm, 63);
  } else {
    u64 transitions = value ^ (u64)((i64)value >> 1);
    SPUSH(vm, (u64)__builtin_clzll(transitions) - 1);
  }
  return 1;
}

static inline u32 h_s_rbit(vm_ctx_t *vm) {
  u64 value = SPOP(vm);
  u64 result = 0;
  for (int i = 0; i < 64; i++) {
    result = (result << 1) | (value & 1);
    value >>= 1;
  }
  SPUSH(vm, result);
  return 1;
}

static inline u32 h_s_rev(vm_ctx_t *vm) {
  SPUSH(vm, __builtin_bswap64(SPOP(vm)));
  return 1;
}

static inline u32 h_s_rev16(vm_ctx_t *vm) {
  u64 value = SPOP(vm);
  u64 result = 0;
  for (int i = 0; i < 4; i++) {
    u16 half = (u16)(value >> (i * 16));
    half = (u16)((half >> 8) | (half << 8));
    result |= (u64)half << (i * 16);
  }
  SPUSH(vm, result);
  return 1;
}

static inline u32 h_s_rev32(vm_ctx_t *vm) {
  u64 value = SPOP(vm);
  u32 lo = __builtin_bswap32((u32)value);
  u32 hi = __builtin_bswap32((u32)(value >> 32));
  SPUSH(vm, ((u64)hi << 32) | lo);
  return 1;
}

static inline u32 h_s_trunc32(vm_ctx_t *vm) {
  SPUSH(vm, SPOP(vm) & 0xFFFFFFFFULL);
  return 1;
}

static inline u32 h_s_sext32(vm_ctx_t *vm) {
  u64 value = SPOP(vm);
  SPUSH(vm, (u64)(i64)(i32)(u32)value);
  return 1;
}

static inline u32 h_s_cmp(vm_ctx_t *vm) {
  u64 b = SPOP(vm), a = SPOP(vm);
  (void)vm_sub_flags(vm, a, b, 1, 1);
  return 1;
}

static inline u32 h_s_ld8(vm_ctx_t *vm) {
  u64 address = SPOP(vm);
  if (!vm->fault)
    SPUSH(vm, *(u8 *)address);
  return 1;
}

static inline u32 h_s_ld16(vm_ctx_t *vm) {
  u64 address = SPOP(vm);
  if (!vm->fault)
    SPUSH(vm, *(u16 *)address);
  return 1;
}

static inline u32 h_s_ld32(vm_ctx_t *vm) {
  u64 address = SPOP(vm);
  if (!vm->fault)
    SPUSH(vm, *(u32 *)address);
  return 1;
}

static inline u32 h_s_ld64(vm_ctx_t *vm) {
  u64 address = SPOP(vm);
  if (!vm->fault)
    SPUSH(vm, *(u64 *)address);
  return 1;
}

static inline u32 h_s_st8(vm_ctx_t *vm) {
  u64 value = SPOP(vm);
  u64 address = SPOP(vm);
  if (!vm->fault)
    *(u8 *)address = (u8)value;
  return 1;
}

static inline u32 h_s_st16(vm_ctx_t *vm) {
  u64 value = SPOP(vm);
  u64 address = SPOP(vm);
  if (!vm->fault)
    *(u16 *)address = (u16)value;
  return 1;
}

static inline u32 h_s_st32(vm_ctx_t *vm) {
  u64 value = SPOP(vm);
  u64 address = SPOP(vm);
  if (!vm->fault)
    *(u32 *)address = (u32)value;
  return 1;
}

static inline u32 h_s_st64(vm_ctx_t *vm) {
  u64 value = SPOP(vm);
  u64 address = SPOP(vm);
  if (!vm->fault)
    *(u64 *)address = value;
  return 1;
}

#undef SPUSH
#undef SPOP
#undef SPEEK

#endif /* H_STACK_OPS_H */