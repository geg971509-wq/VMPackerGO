/*
 * h_alu.h — 算术/逻辑指令 handler
 *
 * 分两类:
 *   三寄存器: [4B: op | d | n | m]    ADD/SUB/MUL/EOR/AND/ORR/LSL/LSR/ASR/ROR
 *   寄存器+立即数: [7B: op | d | n | imm32]
 *   双寄存器: [3B: op | d | n]         MVN
 */
#ifndef H_ALU_H
#define H_ALU_H

#include "../vm_decode.h"
#include "../vm_types.h"

static inline u32 h_add(vm_ctx_t *vm) {
  u8 d = vm->bc[vm->pc + 1], a = vm->bc[vm->pc + 2], b = vm->bc[vm->pc + 3];
  vm->R[d & 31] = vm->R[a & 31] + vm->R[b & 31];
  return 4;
}

static inline u32 h_sub(vm_ctx_t *vm) {
  u8 d = vm->bc[vm->pc + 1], a = vm->bc[vm->pc + 2], b = vm->bc[vm->pc + 3];
  vm->R[d & 31] = vm->R[a & 31] - vm->R[b & 31];
  return 4;
}

static inline u32 h_mul(vm_ctx_t *vm) {
  u8 d = vm->bc[vm->pc + 1], a = vm->bc[vm->pc + 2], b = vm->bc[vm->pc + 3];
  vm->R[d & 31] = vm->R[a & 31] * vm->R[b & 31];
  return 4;
}

static inline u32 h_xor(vm_ctx_t *vm) {
  u8 d = vm->bc[vm->pc + 1], a = vm->bc[vm->pc + 2], b = vm->bc[vm->pc + 3];
  vm->R[d & 31] = vm->R[a & 31] ^ vm->R[b & 31];
  return 4;
}

static inline u32 h_and(vm_ctx_t *vm) {
  u8 d = vm->bc[vm->pc + 1], a = vm->bc[vm->pc + 2], b = vm->bc[vm->pc + 3];
  vm->R[d & 31] = vm->R[a & 31] & vm->R[b & 31];
  return 4;
}

static inline u32 h_or(vm_ctx_t *vm) {
  u8 d = vm->bc[vm->pc + 1], a = vm->bc[vm->pc + 2], b = vm->bc[vm->pc + 3];
  vm->R[d & 31] = vm->R[a & 31] | vm->R[b & 31];
  return 4;
}

static inline u32 h_shl(vm_ctx_t *vm) {
  u8 d = vm->bc[vm->pc + 1], a = vm->bc[vm->pc + 2], b = vm->bc[vm->pc + 3];
  vm->R[d & 31] = vm->R[a & 31] << (vm->R[b & 31] & 63);
  return 4;
}

static inline u32 h_shr(vm_ctx_t *vm) {
  u8 d = vm->bc[vm->pc + 1], a = vm->bc[vm->pc + 2], b = vm->bc[vm->pc + 3];
  vm->R[d & 31] = vm->R[a & 31] >> (vm->R[b & 31] & 63);
  return 4;
}

static inline u32 h_asr(vm_ctx_t *vm) {
  u8 d = vm->bc[vm->pc + 1], a = vm->bc[vm->pc + 2], b = vm->bc[vm->pc + 3];
  vm->R[d & 31] = (u64)((i64)vm->R[a & 31] >> (vm->R[b & 31] & 63));
  return 4;
}

static inline u32 h_not(vm_ctx_t *vm) {
  u8 d = vm->bc[vm->pc + 1], n = vm->bc[vm->pc + 2];
  vm->R[d & 31] = ~vm->R[n & 31];
  return 3;
}

static inline u32 h_ror(vm_ctx_t *vm) {
  u8 d = vm->bc[vm->pc + 1], a = vm->bc[vm->pc + 2], b = vm->bc[vm->pc + 3];
  u64 v = vm->R[a & 31];
  u32 n = (u32)(vm->R[b & 31] & 63);
  vm->R[d & 31] = n == 0 ? v : (v >> n) | (v << (64 - n));
  return 4;
}

static inline u32 h_umulh(vm_ctx_t *vm) {
  u8 d = vm->bc[vm->pc + 1], a = vm->bc[vm->pc + 2], b = vm->bc[vm->pc + 3];
  __uint128_t r = (__uint128_t)vm->R[a & 31] * (__uint128_t)vm->R[b & 31];
  vm->R[d & 31] = (u64)(r >> 64);
  return 4;
}

static inline u32 h_add_imm(vm_ctx_t *vm) {
  u8 d = vm->bc[vm->pc + 1], n = vm->bc[vm->pc + 2];
  vm->R[d & 31] = vm->R[n & 31] + (u64)rd32(&vm->bc[vm->pc + 3]);
  return 7;
}

static inline u32 h_sub_imm(vm_ctx_t *vm) {
  u8 d = vm->bc[vm->pc + 1], n = vm->bc[vm->pc + 2];
  vm->R[d & 31] = vm->R[n & 31] - (u64)rd32(&vm->bc[vm->pc + 3]);
  return 7;
}

static inline u32 h_xor_imm(vm_ctx_t *vm) {
  u8 d = vm->bc[vm->pc + 1], n = vm->bc[vm->pc + 2];
  vm->R[d & 31] = vm->R[n & 31] ^ (u64)rd32(&vm->bc[vm->pc + 3]);
  return 7;
}

static inline u32 h_and_imm(vm_ctx_t *vm) {
  u8 d = vm->bc[vm->pc + 1], n = vm->bc[vm->pc + 2];
  vm->R[d & 31] = vm->R[n & 31] & (u64)rd32(&vm->bc[vm->pc + 3]);
  return 7;
}

static inline u32 h_or_imm(vm_ctx_t *vm) {
  u8 d = vm->bc[vm->pc + 1], n = vm->bc[vm->pc + 2];
  vm->R[d & 31] = vm->R[n & 31] | (u64)rd32(&vm->bc[vm->pc + 3]);
  return 7;
}

static inline u32 h_mul_imm(vm_ctx_t *vm) {
  u8 d = vm->bc[vm->pc + 1], n = vm->bc[vm->pc + 2];
  vm->R[d & 31] = vm->R[n & 31] * (u64)rd32(&vm->bc[vm->pc + 3]);
  return 7;
}

static inline u32 h_shl_imm(vm_ctx_t *vm) {
  u8 d = vm->bc[vm->pc + 1], n = vm->bc[vm->pc + 2];
  vm->R[d & 31] = vm->R[n & 31] << (rd32(&vm->bc[vm->pc + 3]) & 63);
  return 7;
}

static inline u32 h_shr_imm(vm_ctx_t *vm) {
  u8 d = vm->bc[vm->pc + 1], n = vm->bc[vm->pc + 2];
  vm->R[d & 31] = vm->R[n & 31] >> (rd32(&vm->bc[vm->pc + 3]) & 63);
  return 7;
}

static inline u32 h_asr_imm(vm_ctx_t *vm) {
  u8 d = vm->bc[vm->pc + 1], n = vm->bc[vm->pc + 2];
  vm->R[d & 31] =
      (u64)((i64)vm->R[n & 31] >> (rd32(&vm->bc[vm->pc + 3]) & 63));
  return 7;
}

static inline u32 h_udiv(vm_ctx_t *vm) {
  u8 d = vm->bc[vm->pc + 1], a = vm->bc[vm->pc + 2], b = vm->bc[vm->pc + 3];
  u64 divisor = vm->R[b & 31];
  vm->R[d & 31] = divisor == 0 ? 0 : vm->R[a & 31] / divisor;
  return 4;
}

static inline u32 h_sdiv(vm_ctx_t *vm) {
  u8 d = vm->bc[vm->pc + 1], a = vm->bc[vm->pc + 2], b = vm->bc[vm->pc + 3];
  vm->R[d & 31] = vm_sdiv64(vm->R[a & 31], vm->R[b & 31]);
  return 4;
}

static inline u32 h_smulh(vm_ctx_t *vm) {
  u8 d = vm->bc[vm->pc + 1], a = vm->bc[vm->pc + 2], b = vm->bc[vm->pc + 3];
  __int128 r = (__int128)(i64)vm->R[a & 31] * (__int128)(i64)vm->R[b & 31];
  vm->R[d & 31] = (u64)((unsigned __int128)r >> 64);
  return 4;
}

static inline u32 h_clz(vm_ctx_t *vm) {
  u8 d = vm->bc[vm->pc + 1], n = vm->bc[vm->pc + 2];
  u64 v = vm->R[n & 31];
  vm->R[d & 31] = v == 0 ? 64 : (u64)__builtin_clzll(v);
  return 3;
}

static inline u32 h_cls(vm_ctx_t *vm) {
  u8 d = vm->bc[vm->pc + 1], n = vm->bc[vm->pc + 2];
  u64 v = vm->R[n & 31];
  if (v == 0 || v == ~(u64)0) {
    vm->R[d & 31] = 63;
  } else {
    u64 transitions = v ^ (u64)((i64)v >> 1);
    vm->R[d & 31] = (u64)__builtin_clzll(transitions) - 1;
  }
  return 3;
}

static inline u32 h_rbit(vm_ctx_t *vm) {
  u8 d = vm->bc[vm->pc + 1], n = vm->bc[vm->pc + 2];
  u64 v = vm->R[n & 31];
  u64 r = 0;
  for (int i = 0; i < 64; i++) {
    r = (r << 1) | (v & 1);
    v >>= 1;
  }
  vm->R[d & 31] = r;
  return 3;
}

static inline u32 h_rev(vm_ctx_t *vm) {
  u8 d = vm->bc[vm->pc + 1], n = vm->bc[vm->pc + 2];
  vm->R[d & 31] = __builtin_bswap64(vm->R[n & 31]);
  return 3;
}

static inline u32 h_rev16(vm_ctx_t *vm) {
  u8 d = vm->bc[vm->pc + 1], n = vm->bc[vm->pc + 2];
  u64 v = vm->R[n & 31];
  u64 r = 0;
  for (int i = 0; i < 4; i++) {
    u16 hw = (u16)(v >> (i * 16));
    hw = (u16)((hw >> 8) | (hw << 8));
    r |= (u64)hw << (i * 16);
  }
  vm->R[d & 31] = r;
  return 3;
}

static inline u32 h_rev32(vm_ctx_t *vm) {
  u8 d = vm->bc[vm->pc + 1], n = vm->bc[vm->pc + 2];
  u64 v = vm->R[n & 31];
  u32 lo = __builtin_bswap32((u32)v);
  u32 hi = __builtin_bswap32((u32)(v >> 32));
  vm->R[d & 31] = ((u64)hi << 32) | (u64)lo;
  return 3;
}

static inline u32 h_adc(vm_ctx_t *vm) {
  u8 d = vm->bc[vm->pc + 1], n = vm->bc[vm->pc + 2], m = vm->bc[vm->pc + 3];
  u64 carry = (vm->FL & FL_C) ? 1 : 0;
  vm->R[d & 31] = vm->R[n & 31] + vm->R[m & 31] + carry;
  return 4;
}

static inline u32 h_sbc(vm_ctx_t *vm) {
  u8 d = vm->bc[vm->pc + 1], n = vm->bc[vm->pc + 2], m = vm->bc[vm->pc + 3];
  u64 carry = (vm->FL & FL_C) ? 1 : 0;
  vm->R[d & 31] = vm->R[n & 31] - vm->R[m & 31] - (1 - carry);
  return 4;
}

#endif /* H_ALU_H */