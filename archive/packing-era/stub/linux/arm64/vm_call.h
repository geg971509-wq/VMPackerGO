/*
 * vm_call.h — packed-function lookup and same-ctx VM-to-VM call
 */
#ifndef VM_CALL_H
#define VM_CALL_H

#include "vm_decode.h"
#include "vm_sys.h"
#include "vm_token.h"
#include "vm_types.h"

static inline token_desc_t *vm_token_table(u64 *self_va_out) {
  u64 self_va;
  __asm__ volatile("adr %0, _token_table_va" : "=r"(self_va));
  u64 tbl_off = *(volatile u64 *)&_token_table_va;
  if (self_va_out)
    *self_va_out = self_va;
  if (tbl_off == 0)
    return 0;
  return (token_desc_t *)(self_va + tbl_off);
}

static inline int vm_lookup_packed(vm_ctx_t *vm, u64 addr, u32 *func_id_out) {
  u64 self_va;
  token_desc_t *table = vm_token_table(&self_va);
  u32 n = *(volatile u32 *)&_token_count;
  u32 i;
  if (!table || n == 0 || n > TOKEN_MAX_FUNCS)
    return 0;
  for (i = 0; i < n; i++) {
    u64 lo = table[i].func_file_va + vm->load_bias;
    u64 hi = lo + table[i].func_size;
    if (table[i].func_size != 0 && addr >= lo && addr < hi) {
      if (func_id_out)
        *func_id_out = i;
      return 1;
    }
  }
  (void)self_va;
  return 0;
}

static inline void vm_apply_trailer(vm_ctx_t *vm, u8 *bc_buf, u32 bc_len,
                                    u64 load_bias) {
  if (bc_len < 21)
    return;
  {
    u32 trail_func_size = rd32(&bc_buf[bc_len - 4]);
    u64 trail_func_addr = rd64(&bc_buf[bc_len - 12]);
    u32 trail_map_count = rd32(&bc_buf[bc_len - 16]);
    u32 trail_oc_key = rd32(&bc_buf[bc_len - 20]);
    u8 trail_reverse = bc_buf[bc_len - 21];
    u32 map_data_size = trail_map_count * 8 + 21;
    vm->oc_key = trail_oc_key;
    vm->reverse = trail_reverse;
    if (trail_func_addr != 0 && trail_map_count > 0 &&
        map_data_size <= bc_len) {
      u32 j;
      vm->func_addr = trail_func_addr + load_bias;
      vm->func_size = trail_func_size;
      vm->map_count = trail_map_count;
      vm->addr_map = (addr_map_entry_t *)&bc_buf[bc_len - map_data_size];
      vm->bc_len = bc_len - map_data_size;
      for (j = 1; j < vm->map_count; j++) {
        u32 t_arm = vm->addr_map[j].arm64_off;
        u32 t_vm = vm->addr_map[j].vm_off;
        int k = (int)j - 1;
        while (k >= 0 && vm->addr_map[k].arm64_off > t_arm) {
          vm->addr_map[k + 1].arm64_off = vm->addr_map[k].arm64_off;
          vm->addr_map[k + 1].vm_off = vm->addr_map[k].vm_off;
          k--;
        }
        vm->addr_map[k + 1].arm64_off = t_arm;
        vm->addr_map[k + 1].vm_off = t_vm;
      }
    } else {
      vm->bc_len = bc_len - 21;
    }
  }
}

static inline int vm_load_func(vm_ctx_t *vm, u32 func_id) {
  u64 self_va;
  token_desc_t *table = vm_token_table(&self_va);
  u32 n = *(volatile u32 *)&_token_count;
  u32 bc_len;
  u8 xor_key;
  u8 *enc_bc;
  u32 alloc_size;
  u8 *bc_buf;
  u64 xk8;
  if (!table || func_id >= n || n > TOKEN_MAX_FUNCS)
    return 0;
  bc_len = table[func_id].bc_len;
  xor_key = table[func_id].xor_key;
  enc_bc = (u8 *)(self_va + table[func_id].bc_off);
  if (enc_bc == (u8 *)self_va || bc_len == 0)
    return 0;
  if (bc_len > VM_BYTECODE_MAX)
    bc_len = VM_BYTECODE_MAX;
  alloc_size = (bc_len + 4095u) & ~4095u;
  bc_buf = (u8 *)sys_mmap(alloc_size);
  if ((long)bc_buf < 0)
    return 0;
  xk8 = (u64)xor_key;
  xk8 |= xk8 << 8;
  xk8 |= xk8 << 16;
  xk8 |= xk8 << 32;
  {
    u32 n8 = bc_len >> 3;
    u64 *d8 = (u64 *)bc_buf;
    const u64 *s8 = (const u64 *)enc_bc;
    u32 i;
    for (i = 0; i < n8; i++)
      d8[i] = s8[i] ^ xk8;
    for (i = n8 << 3; i < bc_len; i++)
      bc_buf[i] = enc_bc[i] ^ xor_key;
  }
  vm->bc = bc_buf;
  vm->bc_buf = bc_buf;
  vm->bc_len = bc_len;
  vm->bc_alloc = alloc_size;
  vm->pc = 0;
  vm->oc_key = 0;
  vm->reverse = 0;
  vm->func_addr = 0;
  vm->func_size = 0;
  vm->addr_map = 0;
  vm->map_count = 0;
  vm_apply_trailer(vm, bc_buf, bc_len, vm->load_bias);
  if (vm->reverse)
    vm->pc = vm->bc_len;
  return 1;
}

static inline int vm_pop_frame(vm_ctx_t *vm) {
  vm_frame_t *fr;
  if (vm->depth == 0)
    return 0;
  if (vm->bc_buf && vm->bc_alloc)
    sys_munmap(vm->bc_buf, vm->bc_alloc);
  vm->depth--;
  fr = &vm->frames[vm->depth];
  vm->bc = fr->bc;
  vm->bc_buf = fr->bc_buf;
  vm->bc_len = fr->bc_len;
  vm->bc_alloc = fr->bc_alloc;
  vm->pc = fr->pc;
  vm->oc_key = fr->oc_key;
  vm->reverse = fr->reverse;
  vm->func_addr = fr->func_addr;
  vm->func_size = fr->func_size;
  vm->addr_map = fr->addr_map;
  vm->map_count = fr->map_count;
  vm->R[30] = fr->lr;
  return 1;
}

static inline void vm_unwind_frames(vm_ctx_t *vm) {
  while (vm->depth > 0)
    vm_pop_frame(vm);
}

/* 1 = switched to packed callee (pc already set, handler must return 0).
 * 0 = not packed. */
static inline int vm_try_packed_call(vm_ctx_t *vm, u64 addr, u32 resume_pc) {
  u32 func_id;
  vm_frame_t *fr;
  if (!vm_lookup_packed(vm, addr, &func_id))
    return 0;
  if (vm->depth >= VM_CALL_DEPTH_MAX) {
    vm->pc = vm->bc_len;
    return 1;
  }
  fr = &vm->frames[vm->depth];
  fr->bc = vm->bc;
  fr->bc_buf = vm->bc_buf;
  fr->bc_len = vm->bc_len;
  fr->bc_alloc = vm->bc_alloc;
  /* reverse: stay on the CALL so DISPATCH walks to the next original insn.
   * pc+N would land on the size marker and decode as HALT/garbage. */
  fr->pc = vm->reverse ? vm->pc : resume_pc;
  fr->oc_key = vm->oc_key;
  fr->reverse = vm->reverse;
  fr->func_addr = vm->func_addr;
  fr->func_size = vm->func_size;
  fr->addr_map = vm->addr_map;
  fr->map_count = vm->map_count;
  fr->lr = vm->R[30];
  vm->depth++;
  vm->R[30] = VM_PACKED_LR;
  if (!vm_load_func(vm, func_id)) {
    vm->depth--;
    vm->R[30] = fr->lr;
    vm->pc = vm->bc_len;
    return 1;
  }
  return 1;
}

#endif /* VM_CALL_H */
