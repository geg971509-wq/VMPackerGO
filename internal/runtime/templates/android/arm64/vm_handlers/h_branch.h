/*
 * h_branch.h — validated VM control-flow handlers.
 */
#ifndef H_BRANCH_H
#define H_BRANCH_H

#include "../vm_decode.h"
#include "../vm_types.h"

#define BRANCH_FALLTHROUGH(vm, size)                                           \
  do {                                                                         \
    if (!(vm)->reverse)                                                        \
      (vm)->pc += (size);                                                      \
  } while (0)

static inline int vm_branch_target_valid(vm_ctx_t *vm, u32 target) {
  if (target < vm->bc_len)
    return 1;
  vm_fault_set(vm, VM_FAULT_CONTROL);
  return 0;
}

static inline u32 h_jmp(vm_ctx_t *vm) {
  u32 target = rd32(&vm->bc[vm->pc + 1]);
  if (vm_branch_target_valid(vm, target))
    vm->pc = target;
  return 0;
}

static inline u32 h_je(vm_ctx_t *vm) {
  u32 target = rd32(&vm->bc[vm->pc + 1]);
  if (!vm_branch_target_valid(vm, target))
    return 0;
  if (vm->FL & FL_Z)
    vm->pc = target;
  else
    BRANCH_FALLTHROUGH(vm, 5);
  return 0;
}

static inline u32 h_jne(vm_ctx_t *vm) {
  u32 target = rd32(&vm->bc[vm->pc + 1]);
  if (!vm_branch_target_valid(vm, target))
    return 0;
  if (!(vm->FL & FL_Z))
    vm->pc = target;
  else
    BRANCH_FALLTHROUGH(vm, 5);
  return 0;
}

static inline u32 h_jl(vm_ctx_t *vm) {
  u32 target = rd32(&vm->bc[vm->pc + 1]);
  if (!vm_branch_target_valid(vm, target))
    return 0;
  if (!!(vm->FL & FL_N) != !!(vm->FL & FL_V))
    vm->pc = target;
  else
    BRANCH_FALLTHROUGH(vm, 5);
  return 0;
}

static inline u32 h_jge(vm_ctx_t *vm) {
  u32 target = rd32(&vm->bc[vm->pc + 1]);
  if (!vm_branch_target_valid(vm, target))
    return 0;
  if (!!(vm->FL & FL_N) == !!(vm->FL & FL_V))
    vm->pc = target;
  else
    BRANCH_FALLTHROUGH(vm, 5);
  return 0;
}

static inline u32 h_jgt(vm_ctx_t *vm) {
  u32 target = rd32(&vm->bc[vm->pc + 1]);
  if (!vm_branch_target_valid(vm, target))
    return 0;
  if (!(vm->FL & FL_Z) &&
      (!!(vm->FL & FL_N) == !!(vm->FL & FL_V)))
    vm->pc = target;
  else
    BRANCH_FALLTHROUGH(vm, 5);
  return 0;
}

static inline u32 h_jle(vm_ctx_t *vm) {
  u32 target = rd32(&vm->bc[vm->pc + 1]);
  if (!vm_branch_target_valid(vm, target))
    return 0;
  if ((vm->FL & FL_Z) ||
      (!!(vm->FL & FL_N) != !!(vm->FL & FL_V)))
    vm->pc = target;
  else
    BRANCH_FALLTHROUGH(vm, 5);
  return 0;
}

static inline u32 h_jb(vm_ctx_t *vm) {
  u32 target = rd32(&vm->bc[vm->pc + 1]);
  if (!vm_branch_target_valid(vm, target))
    return 0;
  if (!(vm->FL & FL_C))
    vm->pc = target;
  else
    BRANCH_FALLTHROUGH(vm, 5);
  return 0;
}

static inline u32 h_jae(vm_ctx_t *vm) {
  u32 target = rd32(&vm->bc[vm->pc + 1]);
  if (!vm_branch_target_valid(vm, target))
    return 0;
  if (vm->FL & FL_C)
    vm->pc = target;
  else
    BRANCH_FALLTHROUGH(vm, 5);
  return 0;
}

static inline u32 h_jbe(vm_ctx_t *vm) {
  u32 target = rd32(&vm->bc[vm->pc + 1]);
  if (!vm_branch_target_valid(vm, target))
    return 0;
  if (!(vm->FL & FL_C) || (vm->FL & FL_Z))
    vm->pc = target;
  else
    BRANCH_FALLTHROUGH(vm, 5);
  return 0;
}

static inline u32 h_ja(vm_ctx_t *vm) {
  u32 target = rd32(&vm->bc[vm->pc + 1]);
  if (!vm_branch_target_valid(vm, target))
    return 0;
  if ((vm->FL & FL_C) && !(vm->FL & FL_Z))
    vm->pc = target;
  else
    BRANCH_FALLTHROUGH(vm, 5);
  return 0;
}

static inline u32 h_tbz(vm_ctx_t *vm) {
  u8 reg = vm->bc[vm->pc + 1];
  u8 bit = vm->bc[vm->pc + 2];
  u32 target = rd32(&vm->bc[vm->pc + 3]);
  if (!vm_branch_target_valid(vm, target))
    return 0;
  if (!(vm->R[reg & 31] & ((u64)1 << (bit & 63))))
    vm->pc = target;
  else
    BRANCH_FALLTHROUGH(vm, 7);
  return 0;
}

static inline u32 h_tbnz(vm_ctx_t *vm) {
  u8 reg = vm->bc[vm->pc + 1];
  u8 bit = vm->bc[vm->pc + 2];
  u32 target = rd32(&vm->bc[vm->pc + 3]);
  if (!vm_branch_target_valid(vm, target))
    return 0;
  if (vm->R[reg & 31] & ((u64)1 << (bit & 63)))
    vm->pc = target;
  else
    BRANCH_FALLTHROUGH(vm, 7);
  return 0;
}

static inline u32 h_jcond(vm_ctx_t *vm) {
  u8 cond = vm->bc[vm->pc + 1] & 0xFu;
  u32 target = rd32(&vm->bc[vm->pc + 2]);
  if (!vm_branch_target_valid(vm, target))
    return 0;
  if (vm_cond_holds(vm, cond))
    vm->pc = target;
  else
    BRANCH_FALLTHROUGH(vm, 6);
  return 0;
}

static inline u32 h_cbz(vm_ctx_t *vm) {
  u8 reg = vm->bc[vm->pc + 1];
  u32 target = rd32(&vm->bc[vm->pc + 2]);
  u64 value = vm->R[reg & 31];
  if (!vm_branch_target_valid(vm, target))
    return 0;
  if (!(reg & 0x80u))
    value &= 0xFFFFFFFFULL;
  if (value == 0)
    vm->pc = target;
  else
    BRANCH_FALLTHROUGH(vm, 6);
  return 0;
}

static inline u32 h_cbnz(vm_ctx_t *vm) {
  u8 reg = vm->bc[vm->pc + 1];
  u32 target = rd32(&vm->bc[vm->pc + 2]);
  u64 value = vm->R[reg & 31];
  if (!vm_branch_target_valid(vm, target))
    return 0;
  if (!(reg & 0x80u))
    value &= 0xFFFFFFFFULL;
  if (value != 0)
    vm->pc = target;
  else
    BRANCH_FALLTHROUGH(vm, 6);
  return 0;
}

#undef BRANCH_FALLTHROUGH

#endif /* H_BRANCH_H */