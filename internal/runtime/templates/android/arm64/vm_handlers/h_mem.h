/*
 * h_mem.h — scalar memory handlers with explicit VM-stack bounds faults.
 */
#ifndef H_MEM_H
#define H_MEM_H

#include "../vm_decode.h"
#include "../vm_types.h"

static inline int vm_require_stack_access(vm_ctx_t *vm, u8 base, u64 address,
                                          u64 width) {
  if ((base & 31u) != 31u)
    return 1;
  if (vm_stack_range_valid(vm, address, width))
    return 1;
  vm_fault_set(vm, VM_FAULT_STACK);
  return 0;
}

static inline u32 h_load8(vm_ctx_t *vm) {
  u8 d = vm->bc[vm->pc + 1], n = vm->bc[vm->pc + 2];
  i16 off = (i16)rd16(&vm->bc[vm->pc + 3]);
  u64 address = vm->R[n & 31] + (i64)off;
  if (!vm_require_stack_access(vm, n, address, 1))
    return 5;
  vm->R[d & 31] = *(u8 *)address;
  return 5;
}

static inline u32 h_load16(vm_ctx_t *vm) {
  u8 d = vm->bc[vm->pc + 1], n = vm->bc[vm->pc + 2];
  i16 off = (i16)rd16(&vm->bc[vm->pc + 3]);
  u64 address = vm->R[n & 31] + (i64)off;
  if (!vm_require_stack_access(vm, n, address, 2))
    return 5;
  vm->R[d & 31] = *(u16 *)address;
  return 5;
}

static inline u32 h_load32(vm_ctx_t *vm) {
  u8 d = vm->bc[vm->pc + 1], n = vm->bc[vm->pc + 2];
  i16 off = (i16)rd16(&vm->bc[vm->pc + 3]);
  u64 address = vm->R[n & 31] + (i64)off;
  if (!vm_require_stack_access(vm, n, address, 4))
    return 5;
  vm->R[d & 31] = *(u32 *)address;
  return 5;
}

static inline u32 h_load64(vm_ctx_t *vm) {
  u8 d = vm->bc[vm->pc + 1], n = vm->bc[vm->pc + 2];
  i16 off = (i16)rd16(&vm->bc[vm->pc + 3]);
  u64 address = vm->R[n & 31] + (i64)off;
  if (!vm_require_stack_access(vm, n, address, 8))
    return 5;
  vm->R[d & 31] = *(u64 *)address;
  return 5;
}

static inline u32 h_store8(vm_ctx_t *vm) {
  u8 base = vm->bc[vm->pc + 1], source = vm->bc[vm->pc + 2];
  i16 off = (i16)rd16(&vm->bc[vm->pc + 3]);
  u64 address = vm->R[base & 31] + (i64)off;
  if (!vm_require_stack_access(vm, base, address, 1))
    return 5;
  *(u8 *)address = (u8)vm->R[source & 31];
  return 5;
}

static inline u32 h_store16(vm_ctx_t *vm) {
  u8 base = vm->bc[vm->pc + 1], source = vm->bc[vm->pc + 2];
  i16 off = (i16)rd16(&vm->bc[vm->pc + 3]);
  u64 address = vm->R[base & 31] + (i64)off;
  if (!vm_require_stack_access(vm, base, address, 2))
    return 5;
  *(u16 *)address = (u16)vm->R[source & 31];
  return 5;
}

static inline u32 h_store32(vm_ctx_t *vm) {
  u8 base = vm->bc[vm->pc + 1], source = vm->bc[vm->pc + 2];
  i16 off = (i16)rd16(&vm->bc[vm->pc + 3]);
  u64 address = vm->R[base & 31] + (i64)off;
  if (!vm_require_stack_access(vm, base, address, 4))
    return 5;
  *(u32 *)address = (u32)vm->R[source & 31];
  return 5;
}

static inline u32 h_store64(vm_ctx_t *vm) {
  u8 base = vm->bc[vm->pc + 1], source = vm->bc[vm->pc + 2];
  i16 off = (i16)rd16(&vm->bc[vm->pc + 3]);
  u64 address = vm->R[base & 31] + (i64)off;
  if (!vm_require_stack_access(vm, base, address, 8))
    return 5;
  *(u64 *)address = vm->R[source & 31];
  return 5;
}

#endif /* H_MEM_H */