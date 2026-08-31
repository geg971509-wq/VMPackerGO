/*
 * vm_types.h — VM 类型定义 + CPU 上下文结构体
 *
 * 所有 VM 状态封装在 vm_ctx_t 中，方便传递和扩展。
 */
#ifndef VM_TYPES_H
#define VM_TYPES_H

#include "vm_abi.h"

/* ---- 基础类型 ---- */
typedef unsigned char u8;
typedef unsigned short u16;
typedef unsigned int u32;
typedef unsigned long long u64;
typedef int i32;
typedef long long i64;
typedef short i16;

/* ---- VM 配置常量 ---- */
#define VM_REG_COUNT 32        /* X0-X30, X31=SP */
#define VM_STACK_SIZE 32       /* PUSH/POP 操作栈深度 */
#define VM_EVAL_STACK_SIZE 256 /* 栈机器操作栈深度 */
#define VM_MEM_STACK 16384     /* 内存栈 (SP 指向的空间, 16KB) */
#define VM_BYTECODE_MAX 65536  /* 最大字节码长度 (64KB, 含映射表) */
#define VM_VECTOR_COUNT 32     /* V0-V31 */
#define VM_VECTOR_BYTES 16     /* architectural 128-bit vector width */
#define VM_CALL_DEPTH_MAX 16
#define VM_PACKED_LR 1ull

/* ---- AArch64 architectural NZCV bits (same order as the NZCV nibble) ---- */
#define FL_V 0x1u
#define FL_C 0x2u
#define FL_Z 0x4u
#define FL_N 0x8u

/* VM faults are kept separate from architectural flags. */
#define VM_FAULT_STACK 0x1u
#define VM_FAULT_SYSTEM 0x2u

/* ---- BR 间接跳转映射表条目 ---- */
typedef struct {
  u32 arm64_off; /* ARM64 函数内偏移 */
  u32 vm_off;    /* 对应的 VM 字节码偏移 */
} addr_map_entry_t;

/* ---- VM-to-VM 控制面帧 (不含寄存器/虚拟栈) ---- */
typedef struct {
  u8 *bc;
  u8 *bc_buf;
  u32 bc_len;
  u32 bc_alloc;
  u32 pc;
  u32 oc_key;
  u8 reverse;
  u64 func_addr;
  u32 func_size;
  addr_map_entry_t *addr_map;
  u32 map_count;
  u64 lr;
} vm_frame_t;

/* ---- VM CPU 上下文 ---- */
typedef struct {
  /* 寄存器文件: R[0]-R[30] = X0-X30, R[31] = SP */
  u64 R[VM_REG_COUNT];

  /* 完整 SIMD/FP 状态；每个 V 寄存器保留全部 128 位。 */
  u8 V[VM_VECTOR_COUNT][VM_VECTOR_BYTES];
  u32 FPCR;
  u32 FPSR;

  /* 条件标志 */
  u32 FL;

  /* Fail-closed runtime fault state; never alias this with NZCV. */
  u32 fault;

  /* 虚拟程序计数器 */
  u32 pc;

  /* 字节码 (解密后) */
  u8 *bc;
  u32 bc_len;

  /* PUSH/POP 操作栈 (旧 register-based 兼容) */
  u64 stk[VM_STACK_SIZE];
  int sp;

  /* 栈机器操作栈 (Stack Machine eval stack) */
  u64 eval_stk[VM_EVAL_STACK_SIZE];
  int eval_sp; /* 栈顶指针, -1 = 空 */

  /* 内存栈 (R[31] 指向这里的末尾) */
  u8 vm_stk[VM_MEM_STACK];

  /* 最近一次原生调用的可审计 ABI 元数据。 */
  u64 native_target;
  u64 native_sp;
  u32 native_integer_mask;
  u32 native_vector_mask;
  u32 native_stack_bytes;
  u8 native_result_class;
  u8 native_tail_call;
  u16 native_reserved;

  /* BR 间接跳转支持 */
  u64 image_anchor;           /* runtime VA corresponding to rewrite anchor */
  u64 func_addr;              /* 被保护函数的原始起始地址 */
  u32 func_size;              /* 被保护函数的大小 */
  addr_map_entry_t *addr_map; /* ARM64偏移→VM偏移 映射表 */
  u32 map_count;              /* 映射表条目数 */

  /* OpcodeCryptor: 逐指令 opcode 加密 */
  u32 oc_key; /* opcode 加密密钥 (4B, 从 trailer 读取) */

  /* PC 反向遍历 */
  u8 reverse; /* 1=反向执行 (pc 递减), 0=正向 */

  u8 *bc_buf;
  u8 *root_bc_buf;
  u32 bc_alloc;
  u32 depth;
  vm_frame_t frames[VM_CALL_DEPTH_MAX];
} vm_ctx_t;

_Static_assert(__builtin_offsetof(vm_ctx_t, R) == VM_CTX_R,
               "vm_ctx_t R offset");
_Static_assert(__builtin_offsetof(vm_ctx_t, V) == VM_CTX_V,
               "vm_ctx_t V offset");
_Static_assert(__builtin_offsetof(vm_ctx_t, FPCR) == VM_CTX_FPCR,
               "vm_ctx_t FPCR offset");
_Static_assert(__builtin_offsetof(vm_ctx_t, FPSR) == VM_CTX_FPSR,
               "vm_ctx_t FPSR offset");
_Static_assert(__builtin_offsetof(vm_ctx_t, FL) == VM_CTX_FL,
               "vm_ctx_t FL offset");
_Static_assert(__builtin_offsetof(vm_ctx_t, fault) == VM_CTX_FAULT,
               "vm_ctx_t fault offset");

/* ---- SP 栈边界检查 ---- */
/* 检查地址是否在 vm_stk 范围内 (仅对 SP 相关访问使用) */
#define VM_STK_LO(vm) ((u64)(vm)->vm_stk)
#define VM_STK_HI(vm) ((u64)(vm)->vm_stk + VM_MEM_STACK)

/* ---- VM 初始化 ---- */
static inline void vm_ctx_init(vm_ctx_t *vm, u64 *args, u8 *bytecode, u32 len,
                               u64 image_anchor) {
  /* 清零所有寄存器 */
  for (int i = 0; i < VM_REG_COUNT; i++)
    vm->R[i] = 0;
  for (int i = 0; i < VM_VECTOR_COUNT; i++)
    for (int j = 0; j < VM_VECTOR_BYTES; j++)
      vm->V[i][j] = 0;

  /* 从 args 指针恢复参数寄存器 X0-X7 */
  for (int i = 0; i < 8; i++)
    vm->R[i] = args[i];

  vm->R[29] = args[8]; /* X29 = caller FP */
  vm->R[30] = args[9]; /* X30 = caller LR */

  /* SP 指向内存栈顶 */
  vm->R[31] = (u64)&vm->vm_stk[VM_MEM_STACK];

  /* 字节码 */
  vm->bc = bytecode;
  vm->bc_buf = bytecode;
  vm->root_bc_buf = bytecode;
  vm->bc_len = len;
  vm->bc_alloc = 0;
  vm->depth = 0;

  /* 状态初始化 */
  vm->FL = 0;
  vm->fault = 0;
  {
    u64 fpcr = 0, fpsr = 0;
    __asm__ volatile("mrs %0, fpcr" : "=r"(fpcr));
    __asm__ volatile("mrs %0, fpsr" : "=r"(fpsr));
    vm->FPCR = (u32)fpcr;
    vm->FPSR = (u32)fpsr;
  }
  vm->pc = 0;
  vm->sp = 0;
  vm->eval_sp = -1; /* 栈机器操作栈初始为空 */

  /* BR 间接跳转映射表：默认无 */
  vm->image_anchor = image_anchor;
  vm->func_addr = 0;
  vm->func_size = 0;
  vm->addr_map = 0;
  vm->map_count = 0;
  vm->native_target = 0;
  vm->native_sp = vm->R[31];
  vm->native_integer_mask = 0;
  vm->native_vector_mask = 0;
  vm->native_stack_bytes = 0;
  vm->native_result_class = 0;
  vm->native_tail_call = 0;
  vm->native_reserved = 0;

  /* OpcodeCryptor: 默认无加密 (key=0 时解密为恒等) */
  vm->oc_key = 0;

  /* PC 反向遍历: 默认正向 */
  vm->reverse = 0;
}

static inline u64 vm_width_mask(u8 sf) {
  return sf ? ~(u64)0 : 0xFFFFFFFFULL;
}

static inline u64 vm_sign_mask(u8 sf) {
  return sf ? (1ULL << 63) : (1ULL << 31);
}

static inline void vm_set_nz(vm_ctx_t *vm, u64 result, u8 sf) {
  u64 mask = vm_width_mask(sf);
  result &= mask;
  vm->FL = (result == 0 ? FL_Z : 0) | (result & vm_sign_mask(sf) ? FL_N : 0);
}

static inline u64 vm_add_flags(vm_ctx_t *vm, u64 a, u64 b, u32 carry_in,
                               u8 sf) {
  u64 mask = vm_width_mask(sf);
  u64 sign = vm_sign_mask(sf);
  a &= mask;
  b &= mask;
  unsigned __int128 wide = (unsigned __int128)a + b + (carry_in & 1u);
  u64 result = (u64)wide & mask;
  u32 carry = sf ? (u32)(wide >> 64) : (u32)(wide >> 32);
  u32 overflow = ((~(a ^ b) & (a ^ result) & sign) != 0);
  vm->FL = (result == 0 ? FL_Z : 0) | (result & sign ? FL_N : 0) |
           (carry ? FL_C : 0) | (overflow ? FL_V : 0);
  return result;
}

static inline u64 vm_sub_flags(vm_ctx_t *vm, u64 a, u64 b, u32 carry_in,
                               u8 sf) {
  u64 mask = vm_width_mask(sf);
  u64 sign = vm_sign_mask(sf);
  a &= mask;
  b &= mask;
  u64 borrow = carry_in ? 0 : 1;
  u64 subtrahend = (b + borrow) & mask;
  u64 result = (a - subtrahend) & mask;
  u32 no_borrow = carry_in ? (a >= b) : (a > b);
  u32 overflow = (((a ^ subtrahend) & (a ^ result) & sign) != 0);
  vm->FL = (result == 0 ? FL_Z : 0) | (result & sign ? FL_N : 0) |
           (no_borrow ? FL_C : 0) | (overflow ? FL_V : 0);
  return result;
}

#endif /* VM_TYPES_H */
