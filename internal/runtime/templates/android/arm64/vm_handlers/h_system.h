/*
 * h_system.h — 系统/特殊指令 handler
 *
 * NOP / HALT / RET / CALL_NAT / VLD16 / VST16
 */
#ifndef H_SYSTEM_H
#define H_SYSTEM_H

#include "../vm_call.h"
#include "../vm_decode.h"
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
  default: vm->fault |= VM_FAULT_SYSTEM; break;
  }
  return 2;
}

/* NOP  [1B] */
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
  default: vm->fault |= VM_FAULT_SYSTEM; break;                                \
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
    vm->fault |= VM_FAULT_SYSTEM;
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

static inline u32 h_atomic(vm_ctx_t *vm) {
  u8 kind = vm->bc[vm->pc + 1];
  u8 width = vm->bc[vm->pc + 2];
  u8 order = vm->bc[vm->pc + 3];
  u8 rd = vm->bc[vm->pc + 4];
  u8 rn = vm->bc[vm->pc + 5];
  u8 rm = vm->bc[vm->pc + 6];

  if (kind == 12) {
    if ((width != 4 && width != 8) || order > 3 || rn > 31 || rd > 28 ||
        rm > 28 || (rd & 1u) != 0 || (rm & 1u) != 0) {
      vm->fault |= VM_FAULT_SYSTEM;
      return 7;
    }
    u64 address = vm->R[rn];
    u64 pair_bytes = (u64)width * 2u;
    if ((address & (pair_bytes - 1u)) != 0) {
      vm->fault |= VM_FAULT_SYSTEM;
      return 7;
    }
    vm_atomic_pair_t old = vm_atomic_pair_native(
        order, width, address, vm->R[rm], vm->R[rm + 1], vm->R[rd],
        vm->R[rd + 1]);
    vm_atomic_reg_write(vm, rm, old.lo, width);
    vm_atomic_reg_write(vm, rm + 1, old.hi, width);
    return 7;
  }

  if (kind > 11 || (width != 1 && width != 2 && width != 4 && width != 8) ||
      order > 3 || rn > 31) {
    vm->fault |= VM_FAULT_SYSTEM;
    return 7;
  }
  u64 address = vm->R[rn];
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
  if (target == 0 || vm->R[31] < VM_STK_LO(vm) ||
      vm->R[31] > VM_STK_HI(vm) || (vm->R[31] & 15u) != 0) {
    vm->fault |= VM_FAULT_SYSTEM;
    return 0;
  }
  vm->native_target = target;
  vm->native_sp = vm->R[31];
  vm->native_integer_mask = 0x1ffu; /* X0-X8, including sret in X8. */
  vm->native_vector_mask = 0xffu;   /* V0-V7 argument/result bank. */
  vm->native_stack_bytes = (u32)(VM_STK_HI(vm) - vm->R[31]);
  vm->native_result_class = 0xffu;  /* Preserve integer and V0-V7 results. */
  vm->native_tail_call = tail_call;
  return 1;
}

/* CALL_NAT: BLR 绝对地址调用  [9B: op | addr64] */
static inline u32 h_call_nat(vm_ctx_t *vm) {
  u64 addr = rd64(&vm->bc[vm->pc + 1]);
  if (vm_try_packed_call(vm, addr, vm->pc + 9))
    return 0;
  if (vm_prepare_native_call(vm, addr, 0))
    vm_native_call(vm, addr);
  return 9;
}

static inline u32 h_call_image(vm_ctx_t *vm) {
  i64 delta = (i64)rd64(&vm->bc[vm->pc + 1]);
  u64 addr = vm->image_anchor + (u64)delta;
  if (vm_try_packed_call(vm, addr, vm->pc + 9))
    return 0;
  if (vm_prepare_native_call(vm, addr, 0))
    vm_native_call(vm, addr);
  return 9;
}

/* CALL_REG: BLR Xn (寄存器间接调用) [2B: op | rn] */
static inline u32 h_call_reg(vm_ctx_t *vm) {
  u8 rn = vm->bc[vm->pc + 1];
  u64 addr = vm->R[rn & 31];
  if (vm_try_packed_call(vm, addr, vm->pc + 2))
    return 0;
  if (vm_prepare_native_call(vm, addr, 0))
    vm_native_call(vm, addr);
  return 2;
}

/* BR_REG: BR Xn (寄存器间接跳转) [2B: op | rn]
 * 内部目标查映射表；packed 外部目标原位 tail，native 外部目标失败关闭。
 * 返回 0 表示已直接设置 vm->pc (内部跳转) */
static inline u32 h_br_reg(vm_ctx_t *vm) {
  u8 rn = vm->bc[vm->pc + 1];
  u64 addr = vm->R[rn & 31];

  /* 检查目标是否在被保护函数的地址范围内 */
  if (vm->map_count > 0 && addr >= vm->func_addr &&
      addr < vm->func_addr + vm->func_size) {
    u32 arm64_off = (u32)(addr - vm->func_addr);
    /* 二分查找 (addr_map 已按 arm64_off 升序排序) */
    u32 lo = 0, hi = vm->map_count;
    while (lo < hi) {
      u32 mid = lo + ((hi - lo) >> 1);
      u32 mid_off = vm->addr_map[mid].arm64_off;
      if (mid_off < arm64_off)
        lo = mid + 1;
      else if (mid_off > arm64_off)
        hi = mid;
      else {
        vm->pc = vm->addr_map[mid].vm_off;
        return 0; /* 已设置 pc, 不再 advance */
      }
    }
    /* 未找到映射 */
    return 2; /* skip, 继续 */
  }

  if (addr == VM_PACKED_LR) {
    if (vm_pop_frame(vm))
      return 0;
    vm->pc = vm->bc_len;
    return 0;
  }
  if (vm_try_packed_tail(vm, addr))
    return 0;

  vm->fault |= VM_FAULT_SYSTEM;
  return 0;
}

/* VLD16: LD1 {Vn.16B}, [Xn]  [3B: op | rn | len] */
static inline u32 h_vld16(vm_ctx_t *vm) {
  u8 vd = vm->bc[vm->pc + 1];
  u8 rn = vm->bc[vm->pc + 2];
  u8 len = vm->bc[vm->pc + 3];
  const u8 *src = (const u8 *)vm->R[rn & 31];
  for (int i = 0; i < len && i < 64; i++)
    vm->V[((vd & 31) + (i >> 4)) & 31][i & 15] = src[i];
  return 4;
}

/* VST16: ST1 {Vn.16B}, [Xn]  [3B: op | rn | len] */
static inline u32 h_vst16(vm_ctx_t *vm) {
  u8 vd = vm->bc[vm->pc + 1];
  u8 rn = vm->bc[vm->pc + 2];
  u8 len = vm->bc[vm->pc + 3];
  u8 *dst = (u8 *)vm->R[rn & 31];
  for (int i = 0; i < len && i < 64; i++)
    dst[i] = vm->V[((vd & 31) + (i >> 4)) & 31][i & 15];
  return 4;
}

/* MRS Xd, <sysreg>  [4B: op | d | sysreg_lo | sysreg_hi]
 * 读取 ARM64 系统寄存器到 VM 虚拟寄存器。
 * sysreg 是 15-bit 编码 = bits[19:5] of the MRS instruction.
 * 支持的系统寄存器:
 *   0x5F02 = cntvct_el0 (timer count)
 *   0x5F00 = cntfrq_el0 (timer frequency)
 *   0x5E82 = TPIDR_EL0   (Software Thread ID)
 *   0x5E83 = TPIDRRO_EL0 (Read-only Software Thread ID)
 *   0x5A10 = NZCV        (标志位寄存器)
 *   0x5A20 = FPCR
 *   0x5A21 = FPSR
 */
static inline u32 h_mrs(vm_ctx_t *vm) {
  u8 d = vm->bc[vm->pc + 1];
  u16 sysreg = (u16)vm->bc[vm->pc + 2] | ((u16)vm->bc[vm->pc + 3] << 8);
  u64 val = 0;
  switch (sysreg) {
  case 0x5F02: /* cntvct_el0 */
    __asm__ volatile("mrs %0, cntvct_el0" : "=r"(val));
    break;
  case 0x5F00: /* cntfrq_el0 */
    __asm__ volatile("mrs %0, cntfrq_el0" : "=r"(val));
    break;
  case 0x5E82: /* TPIDR_EL0 - Software Thread ID */
    __asm__ volatile("mrs %0, tpidr_el0" : "=r"(val));
    break;
  case 0x5E83: /* TPIDRRO_EL0 - Read-only Software Thread ID */
    __asm__ volatile("mrs %0, tpidrro_el0" : "=r"(val));
    break;
  case 0x5A10: /* NZCV - flags */
    val = (u64)(vm->FL & 0xFu) << 28;
    break;
  case 0x5A20:
    val = vm->FPCR;
    break;
  case 0x5A21:
    val = vm->FPSR;
    break;
  default:
    vm->fault |= VM_FAULT_SYSTEM;
    break;
  }
  vm->R[d & 31] = val;
  return 4;
}

static inline u32 h_msr(vm_ctx_t *vm) {
  u8 s = vm->bc[vm->pc + 1];
  u16 sysreg = (u16)vm->bc[vm->pc + 2] | ((u16)vm->bc[vm->pc + 3] << 8);
  u64 val = s == 0xff ? 0 : vm->R[s & 31];
  switch (sysreg) {
  case 0x5A10:
    vm->FL = (u32)((val >> 28) & 0xFu);
    break;
  case 0x5A20:
    vm->FPCR = (u32)val;
    break;
  case 0x5A21:
    vm->FPSR = (u32)val;
    break;
  default:
    vm->fault |= VM_FAULT_SYSTEM;
    break;
  }
  return 4;
}

#endif /* H_SYSTEM_H */