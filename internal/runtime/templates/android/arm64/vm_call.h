/*
 * vm_call.h — packed-function lookup, validated bytecode installation, and
 * dynamically bounded protected-to-protected call frames.
 */
#ifndef VM_CALL_H
#define VM_CALL_H

#include "vm_decode.h"
#include "vm_sys.h"
#include "vm_token.h"
#include "vm_types.h"

typedef struct {
  u8 *bc;
  u32 bc_len;
  u32 pc;
  u32 oc_key;
  u8 reverse;
  u64 func_addr;
  u32 func_size;
  addr_map_entry_t *addr_map;
  u32 map_count;
} vm_code_state_t;

static inline token_desc_t *vm_token_table(u64 *self_va_out) {
  u64 self_va;
  u64 table_offset;
  __asm__ volatile("adr %0, _token_table_va" : "=r"(self_va));
  table_offset = *(volatile u64 *)&_token_table_va;
  if (self_va_out)
    *self_va_out = self_va;
  if (table_offset == 0 || table_offset > ~(u64)0 - self_va)
    return 0;
  return (token_desc_t *)(self_va + table_offset);
}

static inline int vm_file_bias(vm_ctx_t *vm, u64 *bias_out) {
  u64 file_va = *(volatile u64 *)&_image_file_va;
  if (!bias_out || file_va == 0) {
    vm_fault_set(vm, VM_FAULT_DESCRIPTOR);
    return 0;
  }
  *bias_out = vm->image_anchor - file_va;
  return 1;
}

/* -1 = malformed runtime metadata, 0 = native/unpacked, 1 = packed. */
static inline int vm_lookup_packed(vm_ctx_t *vm, u64 address,
                                   u32 *func_id_out) {
  u64 self_va;
  u64 count64 = *(volatile u64 *)&_token_count;
  u64 bias;
  token_desc_t *table = vm_token_table(&self_va);
  if (!table || count64 == 0 || count64 > TOKEN_MAX_FUNCS ||
      !vm_file_bias(vm, &bias)) {
    vm_fault_set(vm, VM_FAULT_DESCRIPTOR);
    return -1;
  }

  u32 count = (u32)count64;
  for (u32 i = 0; i < count; i++) {
    u64 size = table[i].func_size;
    if (table[i].func_file_va > ~(u64)0 - bias) {
      vm_fault_set(vm, VM_FAULT_DESCRIPTOR);
      return -1;
    }
    u64 lo = table[i].func_file_va + bias;
    if (size != 0 && address >= lo && address - lo < size) {
      if (func_id_out)
        *func_id_out = i;
      return 1;
    }
  }
  (void)self_va;
  return 0;
}

static inline int vm_parse_code(vm_ctx_t *vm, u8 *bc_buf, u32 total_len,
                                vm_code_state_t *state) {
  if (!bc_buf || !state || total_len < 21) {
    vm_fault_set(vm, VM_FAULT_DESCRIPTOR);
    return 0;
  }

  u32 func_size = rd32(&bc_buf[total_len - 4]);
  u64 func_file_va = rd64(&bc_buf[total_len - 12]);
  u32 map_count = rd32(&bc_buf[total_len - 16]);
  u32 opcode_key = rd32(&bc_buf[total_len - 20]);
  u8 reverse = bc_buf[total_len - 21];

  if (reverse > 1 || func_size == 0 || map_count == 0 ||
      map_count > (total_len - 21u) / 8u) {
    vm_fault_set(vm, VM_FAULT_DESCRIPTOR);
    return 0;
  }

  u32 trailer_size = map_count * 8u + 21u;
  u32 code_len = total_len - trailer_size;
  if (code_len == 0) {
    vm_fault_set(vm, VM_FAULT_DESCRIPTOR);
    return 0;
  }

  addr_map_entry_t *map = (addr_map_entry_t *)&bc_buf[code_len];
  u32 previous_arm = 0;
  u32 previous_vm = 0;
  for (u32 i = 0; i < map_count; i++) {
    u32 arm_offset = map[i].arm64_off;
    u32 vm_offset = map[i].vm_off;
    int vm_valid = reverse ? (vm_offset != 0 && vm_offset <= code_len)
                           : (vm_offset < code_len);
    if (arm_offset > func_size || !vm_valid ||
        (i == 0 && (arm_offset != 0 ||
                    vm_offset != (reverse ? code_len : 0))) ||
        (i != 0 && arm_offset <= previous_arm) ||
        (i != 0 && !reverse && vm_offset < previous_vm) ||
        (i != 0 && reverse && vm_offset > previous_vm)) {
      vm_fault_set(vm, VM_FAULT_DESCRIPTOR);
      return 0;
    }
    previous_arm = arm_offset;
    previous_vm = vm_offset;
  }
  if (previous_arm != func_size) {
    vm_fault_set(vm, VM_FAULT_DESCRIPTOR);
    return 0;
  }

  u64 bias;
  if (!vm_file_bias(vm, &bias) || func_file_va > ~(u64)0 - bias) {
    vm_fault_set(vm, VM_FAULT_DESCRIPTOR);
    return 0;
  }

  state->bc = bc_buf;
  state->bc_len = code_len;
  state->pc = reverse ? code_len : 0;
  state->oc_key = opcode_key;
  state->reverse = reverse;
  state->func_addr = func_file_va + bias;
  state->func_size = func_size;
  state->addr_map = map;
  state->map_count = map_count;
  return 1;
}

static inline void vm_install_code(vm_ctx_t *vm,
                                   const vm_code_state_t *state,
                                   u8 *bc_buf, u32 bc_alloc) {
  vm->bc = state->bc;
  vm->bc_buf = bc_buf;
  vm->bc_len = state->bc_len;
  vm->bc_alloc = bc_alloc;
  vm->pc = state->pc;
  vm->oc_key = state->oc_key;
  vm->reverse = state->reverse;
  vm->func_addr = state->func_addr;
  vm->func_size = state->func_size;
  vm->addr_map = state->addr_map;
  vm->map_count = state->map_count;
}

static inline int vm_reserve_frame(vm_ctx_t *vm) {
  if (vm->depth < vm->frame_capacity)
    return 1;
  if (vm->depth >= VM_CALL_DEPTH_MAX) {
    vm_fault_set(vm, VM_FAULT_RESOURCE);
    return 0;
  }

  u32 capacity = vm->frame_capacity ? vm->frame_capacity * 2u
                                    : VM_CALL_FRAME_INITIAL;
  if (capacity < vm->frame_capacity || capacity > VM_CALL_DEPTH_MAX)
    capacity = VM_CALL_DEPTH_MAX;
  if (capacity <= vm->depth) {
    vm_fault_set(vm, VM_FAULT_RESOURCE);
    return 0;
  }

  u32 allocation = vm_round_mapping_size((u64)capacity * sizeof(vm_frame_t));
  if (allocation == 0) {
    vm_fault_set(vm, VM_FAULT_RESOURCE);
    return 0;
  }
  vm_frame_t *frames = (vm_frame_t *)sys_mmap(allocation);
  if (sys_result_failed((long)frames)) {
    vm_fault_set(vm, VM_FAULT_RESOURCE);
    return 0;
  }
  for (u32 i = 0; i < vm->depth; i++)
    frames[i] = vm->frames[i];
  if (vm->frames && vm->frame_alloc)
    sys_munmap(vm->frames, vm->frame_alloc);
  vm->frames = frames;
  vm->frame_capacity = capacity;
  vm->frame_alloc = allocation;
  return 1;
}

static inline void vm_release_frames(vm_ctx_t *vm) {
  if (vm->frames && vm->frame_alloc)
    sys_munmap(vm->frames, vm->frame_alloc);
  vm->frames = 0;
  vm->frame_capacity = 0;
  vm->frame_alloc = 0;
}

static inline int vm_load_func(vm_ctx_t *vm, u32 func_id) {
  u64 self_va;
  u64 count64 = *(volatile u64 *)&_token_count;
  token_desc_t *table = vm_token_table(&self_va);
  if (!table || count64 == 0 || count64 > TOKEN_MAX_FUNCS ||
      func_id >= (u32)count64) {
    vm_fault_set(vm, VM_FAULT_DESCRIPTOR);
    return 0;
  }

  u64 bc_offset = table[func_id].bc_off;
  u32 bc_len = table[func_id].bc_len;
  if (bc_offset == 0 || bc_offset > ~(u64)0 - self_va || bc_len == 0 ||
      bc_len > VM_BYTECODE_MAX) {
    vm_fault_set(vm, VM_FAULT_DESCRIPTOR);
    return 0;
  }
  u8 *enc_bc = (u8 *)(self_va + bc_offset);
  u32 allocation = vm_round_mapping_size(bc_len);
  if (allocation == 0) {
    vm_fault_set(vm, VM_FAULT_RESOURCE);
    return 0;
  }
  u8 *bc_buf = (u8 *)sys_mmap(allocation);
  if (sys_result_failed((long)bc_buf)) {
    vm_fault_set(vm, VM_FAULT_RESOURCE);
    return 0;
  }

  u8 xor_key = table[func_id].xor_key;
  u64 xk8 = (u64)xor_key;
  xk8 |= xk8 << 8;
  xk8 |= xk8 << 16;
  xk8 |= xk8 << 32;
  u32 n8 = bc_len >> 3;
  u64 *dst8 = (u64 *)bc_buf;
  const u64 *src8 = (const u64 *)enc_bc;
  for (u32 i = 0; i < n8; i++)
    dst8[i] = src8[i] ^ xk8;
  for (u32 i = n8 << 3; i < bc_len; i++)
    bc_buf[i] = enc_bc[i] ^ xor_key;

  vm_code_state_t state;
  if (!vm_parse_code(vm, bc_buf, bc_len, &state)) {
    sys_munmap(bc_buf, allocation);
    return 0;
  }
  vm_install_code(vm, &state, bc_buf, allocation);
  return 1;
}

static inline int vm_pop_frame(vm_ctx_t *vm) {
  if (vm->depth == 0) {
    vm_fault_set(vm, VM_FAULT_CONTROL);
    return 0;
  }
  if (vm->bc_buf && vm->bc_alloc && vm->bc_buf != vm->root_bc_buf)
    sys_munmap(vm->bc_buf, vm->bc_alloc);

  vm->depth--;
  vm_frame_t *frame = &vm->frames[vm->depth];
  vm->bc = frame->bc;
  vm->bc_buf = frame->bc_buf;
  vm->bc_len = frame->bc_len;
  vm->bc_alloc = frame->bc_alloc;
  vm->pc = frame->pc;
  vm->oc_key = frame->oc_key;
  vm->reverse = frame->reverse;
  vm->func_addr = frame->func_addr;
  vm->func_size = frame->func_size;
  vm->addr_map = frame->addr_map;
  vm->map_count = frame->map_count;
  vm->R[30] = frame->lr;
  return 1;
}

static inline void vm_unwind_frames(vm_ctx_t *vm) {
  while (vm->depth > 0)
    (void)vm_pop_frame(vm);
}

/* 1 = switched to packed callee, 0 = native/unpacked, -1 = fault. */
static inline int vm_try_packed_call(vm_ctx_t *vm, u64 address,
                                     u32 resume_pc) {
  u32 func_id;
  int lookup = vm_lookup_packed(vm, address, &func_id);
  if (lookup <= 0)
    return lookup;
  if (!vm_reserve_frame(vm))
    return -1;

  vm_frame_t *frame = &vm->frames[vm->depth];
  frame->bc = vm->bc;
  frame->bc_buf = vm->bc_buf;
  frame->bc_len = vm->bc_len;
  frame->bc_alloc = vm->bc_alloc;
  frame->pc = vm->reverse ? vm->pc : resume_pc;
  frame->oc_key = vm->oc_key;
  frame->reverse = vm->reverse;
  frame->func_addr = vm->func_addr;
  frame->func_size = vm->func_size;
  frame->addr_map = vm->addr_map;
  frame->map_count = vm->map_count;
  frame->lr = vm->R[30];

  if (!vm_load_func(vm, func_id))
    return -1;
  vm->depth++;
  vm->R[30] = VM_PACKED_LR;
  return 1;
}

/* 1 = switched in place, 0 = native/unpacked, -1 = fault. */
static inline int vm_try_packed_tail(vm_ctx_t *vm, u64 address) {
  u32 func_id;
  int lookup = vm_lookup_packed(vm, address, &func_id);
  if (lookup <= 0)
    return lookup;

  u8 *old_bc_buf = vm->bc_buf;
  u32 old_bc_alloc = vm->bc_alloc;
  if (!vm_load_func(vm, func_id))
    return -1;
  if (old_bc_buf && old_bc_alloc && old_bc_buf != vm->root_bc_buf)
    sys_munmap(old_bc_buf, old_bc_alloc);
  return 1;
}

#endif /* VM_CALL_H */