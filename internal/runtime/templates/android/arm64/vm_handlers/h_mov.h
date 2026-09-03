/*
 * h_mov.h — data movement handlers.
 */
#ifndef H_MOV_H
#define H_MOV_H

#include "../vm_decode.h"
#include "../vm_types.h"

static inline u32 h_mov_image(vm_ctx_t *vm) {
  u8 destination = vm->bc[vm->pc + 1];
  i64 delta = (i64)rd64(&vm->bc[vm->pc + 2]);
  u64 address;
  if (delta >= 0) {
    if ((u64)delta > ~(u64)0 - vm->image_anchor) {
      vm_fault_set(vm, VM_FAULT_CONTROL);
      return 10;
    }
    address = vm->image_anchor + (u64)delta;
  } else {
    u64 amount = (u64)(-(delta + 1)) + 1u;
    if (amount > vm->image_anchor) {
      vm_fault_set(vm, VM_FAULT_CONTROL);
      return 10;
    }
    address = vm->image_anchor - amount;
  }
  vm->R[destination & 31] = address;
  return 10;
}

static inline u32 h_mov_imm(vm_ctx_t *vm) {
  u8 destination = vm->bc[vm->pc + 1];
  vm->R[destination & 31] = rd64(&vm->bc[vm->pc + 2]);
  return 10;
}

static inline u32 h_mov_imm32(vm_ctx_t *vm) {
  u8 destination = vm->bc[vm->pc + 1];
  vm->R[destination & 31] = (u64)rd32(&vm->bc[vm->pc + 2]);
  return 6;
}

static inline u32 h_mov_reg(vm_ctx_t *vm) {
  u8 destination = vm->bc[vm->pc + 1];
  u8 source = vm->bc[vm->pc + 2];
  vm->R[destination & 31] = vm->R[source & 31];
  return 3;
}

#endif /* H_MOV_H */