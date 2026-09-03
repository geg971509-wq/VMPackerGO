/*
 * vm_interp.c — Android/AArch64 PIC VM runtime.
 *
 * The runtime is rebuilt for every pack with exact Android NDK r29. All
 * malformed descriptors, bytecode, control flow, and resource failures are
 * fatal after owned mappings are released; no failure is returned as a normal
 * integer result.
 */

#include "vm_decode.h"
#include "vm_opcodes.h"
#include "vm_types.h"
#include "vm_native.h"
#include "vm_sys.h"
#include "vm_call.h"

#include "vm_handlers/h_alu.h"
#include "vm_handlers/h_cmp.h"
#include "vm_handlers/h_branch.h"
#include "vm_handlers/h_mem.h"
#include "vm_handlers/h_mov.h"
#include "vm_handlers/h_stack.h"
#include "vm_handlers/h_stack_ops.h"
#include "vm_handlers/h_system.h"
#include "vm_svc.h"
#include "vm_exclusive.h"
#include "vm_fpsimd.h"
#include "vm_dispatch.h"
#include "vm_token.h"

__attribute__((section(".text.entry"))) u64 vm_entry(u64 *args, u8 *enc_bc,
                                                     u32 bc_len, u8 xor_key,
                                                     u64 image_anchor);

__attribute__((section(".data.entry"), used)) volatile u64 _token_table_va = 0;
__attribute__((section(".data.entry"), used)) volatile u64 _image_file_va = 0;
__attribute__((section(".data.entry"), used)) volatile u64 _token_count = 0;

__attribute__((noinline, section(".text.entry"))) u64
vm_entry_token_inner(u64 *args, u32 token) {
  u64 self_va;
  __asm__ volatile("adr %0, _token_table_va" : "=r"(self_va));

  u64 table_offset = *(volatile u64 *)&_token_table_va;
  u64 count64 = *(volatile u64 *)&_token_count;
  u32 func_id = TOKEN_FUNC_ID(token);
  if (!args || table_offset == 0 || table_offset > ~(u64)0 - self_va ||
      count64 == 0 || count64 > TOKEN_MAX_FUNCS || func_id >= (u32)count64)
    vm_runtime_abort(VM_FAULT_DESCRIPTOR);

  token_desc_t *table = (token_desc_t *)(self_va + table_offset);
  u64 bc_offset = table[func_id].bc_off;
  u32 bc_len = table[func_id].bc_len;
  if (bc_offset == 0 || bc_offset > ~(u64)0 - self_va || bc_len == 0 ||
      bc_len > VM_BYTECODE_MAX)
    vm_runtime_abort(VM_FAULT_DESCRIPTOR);

  return vm_entry(args, (u8 *)(self_va + bc_offset), bc_len,
                  table[func_id].xor_key, self_va);
}

static inline void vm_decrypt_bytecode(u8 *destination, const u8 *source,
                                       u32 length, u8 xor_key) {
  u64 key = (u64)xor_key;
  key |= key << 8;
  key |= key << 16;
  key |= key << 32;

  u32 words = length >> 3;
  u64 *destination_words = (u64 *)destination;
  const u64 *source_words = (const u64 *)source;
  for (u32 i = 0; i < words; i++)
    destination_words[i] = source_words[i] ^ key;
  for (u32 i = words << 3; i < length; i++)
    destination[i] = source[i] ^ xor_key;
}

__attribute__((section(".text.entry"))) u64 vm_entry(u64 *args, u8 *enc_bc,
                                                     u32 bc_len, u8 xor_key,
                                                     u64 image_anchor) {
  if (!args || !enc_bc || bc_len == 0 || bc_len > VM_BYTECODE_MAX)
    vm_runtime_abort(VM_FAULT_DESCRIPTOR);

  u32 bytecode_allocation = vm_round_mapping_size(bc_len);
  if (bytecode_allocation == 0)
    vm_runtime_abort(VM_FAULT_RESOURCE);
  u8 *root_bytecode = (u8 *)sys_mmap(bytecode_allocation);
  if (sys_result_failed((long)root_bytecode))
    vm_runtime_abort(VM_FAULT_RESOURCE);
  vm_decrypt_bytecode(root_bytecode, enc_bc, bc_len, xor_key);

  u32 context_allocation = vm_round_mapping_size(sizeof(vm_ctx_t));
  if (context_allocation == 0) {
    sys_munmap(root_bytecode, bytecode_allocation);
    vm_runtime_abort(VM_FAULT_RESOURCE);
  }
  vm_ctx_t *vm = (vm_ctx_t *)sys_mmap(context_allocation);
  if (sys_result_failed((long)vm)) {
    sys_munmap(root_bytecode, bytecode_allocation);
    vm_runtime_abort(VM_FAULT_RESOURCE);
  }

  u32 stack_mapping_size =
      VM_MEMORY_STACK_SIZE + 2u * VM_MAPPING_GRANULE;
  void *stack_mapping = sys_mmap(stack_mapping_size);
  if (sys_result_failed((long)stack_mapping)) {
    sys_munmap(vm, context_allocation);
    sys_munmap(root_bytecode, bytecode_allocation);
    vm_runtime_abort(VM_FAULT_RESOURCE);
  }
  u8 *stack_base = (u8 *)stack_mapping + VM_MAPPING_GRANULE;
  if (sys_mprotect(stack_mapping, VM_MAPPING_GRANULE, 0) != 0 ||
      sys_mprotect(stack_base + VM_MEMORY_STACK_SIZE, VM_MAPPING_GRANULE, 0) !=
          0) {
    sys_munmap(stack_mapping, stack_mapping_size);
    sys_munmap(vm, context_allocation);
    sys_munmap(root_bytecode, bytecode_allocation);
    vm_runtime_abort(VM_FAULT_RESOURCE);
  }

  vm_ctx_init(vm, args, root_bytecode, bc_len, image_anchor, stack_base,
              VM_MEMORY_STACK_SIZE, stack_mapping, stack_mapping_size);

  vm_code_state_t root_state;
  if (!vm_parse_code(vm, root_bytecode, bc_len, &root_state)) {
    u32 fault = vm->fault ? vm->fault : VM_FAULT_DESCRIPTOR;
    sys_munmap(stack_mapping, stack_mapping_size);
    sys_munmap(vm, context_allocation);
    sys_munmap(root_bytecode, bytecode_allocation);
    vm_runtime_abort(fault);
  }
  vm_install_code(vm, &root_state, root_bytecode, bytecode_allocation);
  vm->root_bc_buf = root_bytecode;

#define OC_DECRYPT(pc, key) ((u8)((key) ^ ((pc) * 0x9E3779B9u)))

  vm_handler_fn jump_table[256];
  vm_init_jump_table(jump_table);
  u64 result = 0;

  for (;;) {
    u8 reverse_marker = 0;
    if (vm->reverse) {
      if (vm->pc == 0) {
        vm_fault_set(vm, VM_FAULT_BYTECODE);
        goto cleanup;
      }
      vm->pc--;
      if (vm->pc >= vm->bc_len) {
        vm_fault_set(vm, VM_FAULT_BYTECODE);
        goto cleanup;
      }
      reverse_marker = vm->bc[vm->pc];
      if (reverse_marker == 0 || reverse_marker > vm->pc) {
        vm_fault_set(vm, VM_FAULT_BYTECODE);
        goto cleanup;
      }
      vm->pc -= reverse_marker;
    } else if (vm->pc >= vm->bc_len) {
      vm_fault_set(vm, VM_FAULT_BYTECODE);
      goto cleanup;
    }

    u8 raw_opcode = vm->bc[vm->pc];
    u8 opcode = raw_opcode ^ OC_DECRYPT(vm->pc, vm->oc_key);
    u8 instruction_size = vm_insn_size(opcode);
    if (instruction_size == 0 || instruction_size > vm->bc_len - vm->pc ||
        (vm->reverse && reverse_marker != instruction_size)) {
      vm_fault_set(vm, VM_FAULT_BYTECODE);
      goto cleanup;
    }

    if (opcode == OP_HALT) {
      result = vm->R[0];
      if (vm->depth > 0) {
        if (!vm_pop_frame(vm))
          goto cleanup;
        continue;
      }
      goto cleanup;
    }
    if (opcode == OP_RET) {
      result = vm->R[vm->bc[vm->pc + 1] & 31];
      if (vm->depth > 0) {
        vm->R[0] = result;
        if (!vm_pop_frame(vm))
          goto cleanup;
        continue;
      }
      goto cleanup;
    }

#ifdef VM_DEBUG_TRACE
    {
      u8 trace[8];
#define VM_HEX(n) ((u8)((n) < 10 ? '0' + (n) : 'A' + (n) - 10))
      trace[0] = VM_HEX((vm->pc >> 12) & 0xf);
      trace[1] = VM_HEX((vm->pc >> 8) & 0xf);
      trace[2] = VM_HEX((vm->pc >> 4) & 0xf);
      trace[3] = VM_HEX(vm->pc & 0xf);
      trace[4] = ':';
      trace[5] = VM_HEX((opcode >> 4) & 0xf);
      trace[6] = VM_HEX(opcode & 0xf);
      trace[7] = '\n';
#undef VM_HEX
      register long x8 __asm__("x8") = 64;
      register long x0 __asm__("x0") = 2;
      register long x1 __asm__("x1") = (long)trace;
      register long x2 __asm__("x2") = 8;
      __asm__ volatile("svc #0"
                       : "+r"(x0)
                       : "r"(x8), "r"(x1), "r"(x2)
                       : "memory");
    }
#endif

    u8 *previous_bytecode = vm->bc;
    u32 previous_pc = vm->pc;
    u8 previous_reverse = vm->reverse;
    vm_handler_fn handler = jump_table[opcode];
    if (!handler) {
      vm_fault_set(vm, VM_FAULT_INTERNAL);
      goto cleanup;
    }
    u32 step = handler(vm);
    if (vm->fault)
      goto cleanup;

    if (step == VM_STEP_HALT || step == VM_STEP_RET) {
      result = vm->R[0];
      if (vm->depth > 0) {
        if (!vm_pop_frame(vm))
          goto cleanup;
        continue;
      }
      goto cleanup;
    }

    if (step == 0) {
      if (!previous_reverse && vm->bc == previous_bytecode &&
          vm->pc == previous_pc) {
        vm_fault_set(vm, VM_FAULT_CONTROL);
        goto cleanup;
      }
      continue;
    }
    if (step != instruction_size) {
      vm_fault_set(vm, VM_FAULT_INTERNAL);
      goto cleanup;
    }
    if (!vm->reverse) {
      if (step > vm->bc_len - vm->pc) {
        vm_fault_set(vm, VM_FAULT_BYTECODE);
        goto cleanup;
      }
      vm->pc += step;
    }
  }

cleanup:
  {
    u32 fault = vm->fault;
    vm_unwind_frames(vm);
    fault |= vm->fault;

    u8 *current_bytecode = vm->bc_buf;
    u32 current_allocation = vm->bc_alloc;
    if (current_bytecode && current_allocation &&
        current_bytecode != root_bytecode)
      sys_munmap(current_bytecode, current_allocation);

    vm_release_frames(vm);
    sys_munmap(stack_mapping, stack_mapping_size);
    sys_munmap(vm, context_allocation);
    sys_munmap(root_bytecode, bytecode_allocation);
    if (fault)
      vm_runtime_abort(fault);
  }

#undef OC_DECRYPT
  return result;
}