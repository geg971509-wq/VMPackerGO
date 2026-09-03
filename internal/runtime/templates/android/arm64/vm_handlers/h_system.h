/*
 * h_system.h — system, native-call, atomic, and indirect-transfer handlers.
 */
#ifndef H_SYSTEM_H
#define H_SYSTEM_H

#include "../vm_call.h"
#include "../vm_decode.h"
#include "../vm_invoke.h"
#include "../vm_types.h"

static inline u64 vm_pacia_value(u64 value, u64 modifier) {
  __asm__ volatile(".arch_extension pauth\npacia %0, %1"
                   : "+r"(value) : "r"(modifier));
  return value;
}

static inline u64 vm_autia_value(u64 value, u64 modifier) {
  __asm__ volatile(".arch_extension pauth\nautia %0, %1"
                   : "+r"(value) : "r"(modifier));
  return value;
}

static inline u64 vm_pacib_value(u64 value, u64 modifier) {
  __asm__ volatile(".arch_extension pauth\npacib %0, %1"
                   : "+r"(value) : "r"(modifier));
  return value;
}

static inline u64 vm_autib_value(u64 value, u64 modifier) {
  __asm__ volatile(".arch_extension pauth\nautib %0, %1"
                   : "+r"(value) : "r"(modifier));
  return value;
}

static inline u64 vm_xpaci_value(u64 value) {
  __asm__ volatile(".arch_extension pauth\nxpaci %0" : "+r"(value));
  return value;
}

static inline u32 h_pauth(vm_ctx_t *vm) {
  u8 kind = vm->bc[vm->pc + 1];
  switch (kind) {
  case 0: vm->R[30] = vm_pacia_value(vm->R[30], vm->R[31]); break;
  case 1: vm->R[30] = vm_autia_value(vm->R[30], vm->R[31]); break;
  case 2: vm->R[30] = vm_pacia_value(vm->R[30], 0); break;
  case 3: vm->R[30] = vm_autia_value(vm->R[30], 0); break;
  case 4: vm->R[30] = vm_pacib_value(vm->R[30], vm->R[31]); break;
  case 5: vm->R[30] = vm_autib_value(vm->R[30], vm->R[31]); break;
  case 6: vm->R[30] = vm_xpaci_value(vm->R[30]); break;
  default: vm_fault_set(vm, VM_FAULT_BYTECODE); break;
  }
  return 2;
}

static inline u32 h_nop(vm_ctx_t *vm) {
  (void)vm;
  return 1;
}

#define VM_BARRIER_OPTIONS(kind)                                               \
  switch (option) {                                                            \
  case 0x1: __asm__ volatile(kind " oshld" ::: "memory"); break;              \
  case 0x2: __asm__ volatile(kind " oshst" ::: "memory"); break;              \
  case 0x3: __asm__ volatile(kind " osh" ::: "memory"); break;                \
  case 0x5: __asm__ volatile(kind " nshld" ::: "memory"); break;              \
  case 0x6: __asm__ volatile(kind " nshst" ::: "memory"); break;              \
  case 0x7: __asm__ volatile(kind " nsh" ::: "memory"); break;                \
  case 0x9: __asm__ volatile(kind " ishld" ::: "memory"); break;              \
  case 0xa: __asm__ volatile(kind " ishst" ::: "memory"); break;              \
  case 0xb: __asm__ volatile(kind " ish" ::: "memory"); break;                \
  case 0xd: __asm__ volatile(kind " ld" ::: "memory"); break;                 \
  case 0xe: __asm__ volatile(kind " st" ::: "memory"); break;                 \
  case 0xf: __asm__ volatile(kind " sy" ::: "memory"); break;                 \
  default: vm_fault_set(vm, VM_FAULT_BYTECODE); break;                         \
  }

static inline u32 h_barrier(vm_ctx_t *vm) {
  u8 kind = vm->bc[vm->pc + 1];
  u8 option = vm->bc[vm->pc + 2];
  if (kind == 0) {
    VM_BARRIER_OPTIONS("dmb")
  } else if (kind == 1) {
    VM_BARRIER_OPTIONS("dsb")
  } else if (kind == 2 && option == 0xf) {
    __asm__ volatile("isb" ::: "memory");
  } else {
    vm_fault_set(vm, VM_FAULT_BYTECODE);
  }
  return 3;
}

#undef VM_BARRIER_OPTIONS

static inline u64 vm_atomic_reg_read(vm_ctx_t *vm, u8 reg) {
  return reg == 0xffu ? 0 : vm->R[reg & 31];
}

static inline void vm_atomic_reg_write(vm_ctx_t *vm, u8 reg, u64 value,
                                       u8 width) {
  if (reg == 0xffu)
    return;
  if (width < 8)
    value &= (1ULL << (width * 8)) - 1;
  vm->R[reg & 31] = value;
}

static inline u64 vm_atomic_pair_high_read(vm_ctx_t *vm, u8 low) {
  return low == 30 ? 0 : vm->R[low + 1];
}

static inline void vm_atomic_pair_high_write(vm_ctx_t *vm, u8 low, u64 value,
                                             u8 width) {
  if (low != 30)
    vm_atomic_reg_write(vm, low + 1, value, width);
}

static inline int vm_atomic_stack_valid(vm_ctx_t *vm, u8 base, u64 address,
                                        u64 width) {
  if (base != 31 || vm_stack_range_valid(vm, address, width))
    return 1;
  vm_fault_set(vm, VM_FAULT_STACK);
  return 0;
}

static inline u32 h_atomic(vm_ctx_t *vm) {
  u8 kind = vm->bc[vm->pc + 1];
  u8 width = vm->bc[vm->pc + 2];
  u8 order = vm->bc[vm->pc + 3];
  u8 rd = vm->bc[vm->pc + 4];
  u8 rn = vm->bc[vm->pc + 5];
  u8 rm = vm->bc[vm->pc + 6];

  if (kind == 12) {
    if ((width != 4 && width != 8) || order > 3 || rn > 31 || rd > 30 ||
        rm > 30 || (rd & 1u) != 0 || (rm & 1u) != 0) {
      vm_fault_set(vm, VM_FAULT_BYTECODE);
      return 7;
    }
    u64 address = vm->R[rn];
    u64 pair_bytes = (u64)width * 2u;
    if ((address & (pair_bytes - 1u)) != 0 ||
        !vm_atomic_stack_valid(vm, rn, address, pair_bytes)) {
      vm_fault_set(vm, VM_FAULT_SYSTEM);
      return 7;
    }
    vm_atomic_pair_t old = vm_atomic_pair_native(
        order, width, address, vm->R[rm], vm_atomic_pair_high_read(vm, rm),
        vm->R[rd], vm_atomic_pair_high_read(vm, rd));
    vm_atomic_reg_write(vm, rm, old.lo, width);
    vm_atomic_pair_high_write(vm, rm, old.hi, width);
    return 7;
  }

  if (kind > 11 || (width != 1 && width != 2 && width != 4 && width != 8) ||
      order > 3 || rn > 31) {
    vm_fault_set(vm, VM_FAULT_BYTECODE);
    return 7;
  }
  u64 address = vm->R[rn];
  if ((address & (width - 1u)) != 0 ||
      !vm_atomic_stack_valid(vm, rn, address, width)) {
    vm_fault_set(vm, VM_FAULT_SYSTEM);
    return 7;
  }

  u64 first = 0, second = 0;
  if (kind == 1)
    first = vm_atomic_reg_read(vm, rd);
  else if (kind == 2 || kind >= 4)
    first = vm_atomic_reg_read(vm, rm);
  else if (kind == 3) {
    first = vm_atomic_reg_read(vm, rm);
    second = vm_atomic_reg_read(vm, rd);
  }
  u64 old = vm_atomic_native(kind, width, order, address, first, second);
  if (kind == 0 || kind == 2 || kind >= 4)
    vm_atomic_reg_write(vm, rd, old, width);
  else if (kind == 3)
    vm_atomic_reg_write(vm, rm, old, width);
  return 7;
}

static inline int vm_prepare_native_call(vm_ctx_t *vm, u64 target,
                                         u8 tail_call) {
  if (target == 0 || !vm_stack_range_valid(vm, vm->R[31], 0) ||
      (vm->R[31] & 15u) != 0) {
    vm_fault_set(vm, VM_FAULT_SYSTEM);
    return 0;
  }
  vm->native_target = target;
  vm->native_sp = vm->R[31];
  vm->native_integer_mask = 0x1ffu;
  vm->native_vector_mask = 0xffu;
  vm->native_stack_bytes = (u32)(VM_STK_HI(vm) - vm->R[31]);
  vm->native_result_class = 0xffu;
  vm->native_tail_call = tail_call;
  return 1;
}

static inline int vm_image_address(vm_ctx_t *vm, i64 delta, u64 *address) {
  if (!address) {
    vm_fault_set(vm, VM_FAULT_INTERNAL);
    return 0;
  }
  if (delta >= 0) {
    if ((u64)delta > ~(u64)0 - vm->image_anchor) {
      vm_fault_set(vm, VM_FAULT_CONTROL);
      return 0;
    }
    *address = vm->image_anchor + (u64)delta;
    return 1;
  }
  u64 amount = (u64)(-(delta + 1)) + 1u;
  if (amount > vm->image_anchor) {
    vm_fault_set(vm, VM_FAULT_CONTROL);
    return 0;
  }
  *address = vm->image_anchor - amount;
  return 1;
}

static inline u32 vm_run_native_call(vm_ctx_t *vm, u64 address,
                                     u32 instruction_size) {
  if (!vm_prepare_native_call(vm, address, 0))
    return 0;
  int invoke = vm_try_exception_invoke(vm, address);
  if (invoke == VM_INVOKE_NONE) {
    vm_native_call(vm, address);
    return instruction_size;
  }
  if (invoke == VM_INVOKE_NORMAL)
    return instruction_size;
  if (invoke == VM_INVOKE_LANDING || invoke == VM_INVOKE_ERROR)
    return 0;
  vm_fault_set(vm, VM_FAULT_INTERNAL);
  return 0;
}

static inline u32 h_call_nat(vm_ctx_t *vm) {
  u64 address = rd64(&vm->bc[vm->pc + 1]);
  int packed = vm_try_packed_call(vm, address, vm->pc + 9);
  if (packed != 0)
    return 0;
  return vm_run_native_call(vm, address, 9);
}

static inline u32 h_call_image(vm_ctx_t *vm) {
  u64 address;
  if (!vm_image_address(vm, (i64)rd64(&vm->bc[vm->pc + 1]), &address))
    return 0;
  int packed = vm_try_packed_call(vm, address, vm->pc + 9);
  if (packed != 0)
    return 0;
  return vm_run_native_call(vm, address, 9);
}

static inline u32 h_call_reg(vm_ctx_t *vm) {
  u8 rn = vm->bc[vm->pc + 1];
  u64 address = vm->R[rn & 31];
  int packed = vm_try_packed_call(vm, address, vm->pc + 2);
  if (packed != 0)
    return 0;
  return vm_run_native_call(vm, address, 2);
}

static inline u32 h_br_reg(vm_ctx_t *vm) {
  u8 rn = vm->bc[vm->pc + 1];
  u64 address = vm->R[rn & 31];

  if (vm->map_count > 0 && address >= vm->func_addr &&
      address - vm->func_addr < vm->func_size) {
    u32 arm_offset = (u32)(address - vm->func_addr);
    u32 lo = 0, hi = vm->map_count;
    while (lo < hi) {
      u32 mid = lo + ((hi - lo) >> 1);
      u32 mid_offset = vm->addr_map[mid].arm64_off;
      if (mid_offset < arm_offset) {
        lo = mid + 1;
      } else if (mid_offset > arm_offset) {
        hi = mid;
      } else {
        u32 target = vm->addr_map[mid].vm_off;
        if (vm_branch_target_valid(vm, target))
          vm->pc = target;
        return 0;
      }
    }
    vm_fault_set(vm, VM_FAULT_CONTROL);
    return 0;
  }

  if (address == VM_PACKED_LR) {
    (void)vm_pop_frame(vm);
    return 0;
  }
  if (vm_try_packed_tail(vm, address) != 0)
    return 0;

  /* A native external BR requires a dedicated LR/PAC-preserving tail bridge.
   * Until that bridge is generated, rejecting it is more accurate than
   * approximating it with BLR + RET. */
  vm_fault_set(vm, VM_FAULT_CONTROL);
  return 0;
}

static inline int vm_vector_transfer_length_valid(u8 length) {
  return length == 16 || length == 32 || length == 48 || length == 64;
}

static inline u32 h_vld16(vm_ctx_t *vm) {
  u8 vd = vm->bc[vm->pc + 1];
  u8 rn = vm->bc[vm->pc + 2];
  u8 length = vm->bc[vm->pc + 3];
  if (!vm_vector_transfer_length_valid(length)) {
    vm_fault_set(vm, VM_FAULT_BYTECODE);
    return 4;
  }
  const u8 *source = (const u8 *)vm->R[rn & 31];
  for (u32 i = 0; i < length; i++)
    vm->V[((vd & 31) + (i >> 4)) & 31][i & 15] = source[i];
  return 4;
}

static inline u32 h_vst16(vm_ctx_t *vm) {
  u8 vd = vm->bc[vm->pc + 1];
  u8 rn = vm->bc[vm->pc + 2];
  u8 length = vm->bc[vm->pc + 3];
  if (!vm_vector_transfer_length_valid(length)) {
    vm_fault_set(vm, VM_FAULT_BYTECODE);
    return 4;
  }
  u8 *destination = (u8 *)vm->R[rn & 31];
  for (u32 i = 0; i < length; i++)
    destination[i] = vm->V[((vd & 31) + (i >> 4)) & 31][i & 15];
  return 4;
}

static inline u32 h_mrs(vm_ctx_t *vm) {
  u8 destination = vm->bc[vm->pc + 1];
  u16 sysreg = (u16)vm->bc[vm->pc + 2] |
               ((u16)vm->bc[vm->pc + 3] << 8);
  u64 value = 0;
  switch (sysreg) {
  case 0x5F02: __asm__ volatile("mrs %0, cntvct_el0" : "=r"(value)); break;
  case 0x5F00: __asm__ volatile("mrs %0, cntfrq_el0" : "=r"(value)); break;
  case 0x5E82: __asm__ volatile("mrs %0, tpidr_el0" : "=r"(value)); break;
  case 0x5E83: __asm__ volatile("mrs %0, tpidrro_el0" : "=r"(value)); break;
  case 0x5A10: value = (u64)(vm->FL & 0xfu) << 28; break;
  case 0x5A20: value = vm->FPCR; break;
  case 0x5A21: value = vm->FPSR; break;
  default: vm_fault_set(vm, VM_FAULT_BYTECODE); break;
  }
  vm->R[destination & 31] = value;
  return 4;
}

static inline u32 h_msr(vm_ctx_t *vm) {
  u8 source = vm->bc[vm->pc + 1];
  u16 sysreg = (u16)vm->bc[vm->pc + 2] |
               ((u16)vm->bc[vm->pc + 3] << 8);
  u64 value = source == 0xff ? 0 : vm->R[source & 31];
  switch (sysreg) {
  case 0x5A10: vm->FL = (u32)((value >> 28) & 0xfu); break;
  case 0x5A20: vm->FPCR = (u32)value; break;
  case 0x5A21: vm->FPSR = (u32)value; break;
  default: vm_fault_set(vm, VM_FAULT_BYTECODE); break;
  }
  return 4;
}

#endif /* H_SYSTEM_H */