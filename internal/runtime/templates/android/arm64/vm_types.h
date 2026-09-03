/*
 * vm_types.h — architectural VM state and bounded runtime resources.
 */
#ifndef VM_TYPES_H
#define VM_TYPES_H

#include "vm_abi.h"

typedef unsigned char u8;
typedef unsigned short u16;
typedef unsigned int u32;
typedef unsigned long long u64;
typedef int i32;
typedef long long i64;
typedef short i16;

#define VM_REG_COUNT 32
#define VM_STACK_SIZE 32
#define VM_EVAL_STACK_SIZE 256
#define VM_VECTOR_COUNT 32
#define VM_VECTOR_BYTES 16
#define VM_BYTECODE_MAX (256u * 1024u)

/* Runtime mappings use 16 KiB granules so guard operations are valid on both
 * 4 KiB and 16 KiB Android kernels. The memory stack is a separately mapped,
 * demand-paged resource rather than a fixed array embedded in vm_ctx_t. */
#define VM_MAPPING_GRANULE 0x4000u
#define VM_MEMORY_STACK_SIZE (8u * 1024u * 1024u)
#define VM_CALL_FRAME_INITIAL 16u
#define VM_CALL_DEPTH_MAX 4096u
#define VM_PACKED_LR 1ull

_Static_assert(VM_BYTECODE_MAX <= 0xFFFFC000u,
               "VM bytecode limit must leave room for mapping rounding");
_Static_assert((VM_MEMORY_STACK_SIZE % VM_MAPPING_GRANULE) == 0,
               "VM memory stack must be mapping-granule aligned");

#define FL_V 0x1u
#define FL_C 0x2u
#define FL_Z 0x4u
#define FL_N 0x8u

/* Fault classes never alias architectural NZCV. Every nonzero fault is fatal
 * at the root VM boundary; it can never be returned as a normal integer zero. */
#define VM_FAULT_STACK       0x00000001u
#define VM_FAULT_SYSTEM      0x00000002u
#define VM_FAULT_BYTECODE    0x00000004u
#define VM_FAULT_CONTROL     0x00000008u
#define VM_FAULT_RESOURCE    0x00000010u
#define VM_FAULT_DESCRIPTOR  0x00000020u
#define VM_FAULT_EVAL_STACK  0x00000040u
#define VM_FAULT_INTERNAL    0x80000000u

typedef struct {
  u32 arm64_off;
  u32 vm_off;
} addr_map_entry_t;

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

typedef struct {
  /* Assembly-visible prefix. Keep vm_abi.h static assertions in sync. */
  u64 R[VM_REG_COUNT];
  u8 V[VM_VECTOR_COUNT][VM_VECTOR_BYTES];
  u32 FPCR;
  u32 FPSR;
  u32 FL;
  u32 fault;

  u32 pc;
  u8 *bc;
  u32 bc_len;

  u64 stk[VM_STACK_SIZE];
  int sp;
  u64 eval_stk[VM_EVAL_STACK_SIZE];
  int eval_sp;

  /* Separately mapped architectural stack with guard granules. */
  u8 *stack_base;
  u32 stack_size;
  void *stack_mapping;
  u32 stack_mapping_size;

  u64 native_target;
  u64 native_sp;
  u32 native_integer_mask;
  u32 native_vector_mask;
  u32 native_stack_bytes;
  u8 native_result_class;
  u8 native_tail_call;
  u16 native_reserved;

  u64 image_anchor;
  u64 func_addr;
  u32 func_size;
  addr_map_entry_t *addr_map;
  u32 map_count;

  u32 oc_key;
  u8 reverse;

  u8 *bc_buf;
  u8 *root_bc_buf;
  u32 bc_alloc;

  /* Packed-call control frames grow independently of vm_ctx_t. */
  u32 depth;
  vm_frame_t *frames;
  u32 frame_capacity;
  u32 frame_alloc;
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

#define VM_STK_LO(vm) ((u64)(vm)->stack_base)
#define VM_STK_HI(vm) ((u64)(vm)->stack_base + (u64)(vm)->stack_size)

static inline void vm_fault_set(vm_ctx_t *vm, u32 fault) {
  if (vm)
    vm->fault |= fault ? fault : VM_FAULT_INTERNAL;
}

static inline int vm_stack_range_valid(const vm_ctx_t *vm, u64 address,
                                       u64 width) {
  u64 lo;
  u64 hi;
  if (!vm || !vm->stack_base || vm->stack_size == 0)
    return 0;
  lo = VM_STK_LO(vm);
  hi = VM_STK_HI(vm);
  if (address < lo || address > hi)
    return 0;
  return width <= hi - address;
}

static inline u32 vm_round_mapping_size(u64 size) {
  u64 rounded;
  if (size == 0 || size > 0xffffffffULL - (VM_MAPPING_GRANULE - 1u))
    return 0;
  rounded = (size + (VM_MAPPING_GRANULE - 1u)) &
            ~((u64)VM_MAPPING_GRANULE - 1u);
  return (u32)rounded;
}

__attribute__((noreturn)) static inline void vm_runtime_abort(u32 fault) {
  u64 code = fault ? fault : VM_FAULT_INTERNAL;
  __asm__ volatile("mov x0, %0\n\tbrk #0" : : "r"(code) : "x0", "memory");
  __builtin_unreachable();
}

static inline void vm_ctx_init(vm_ctx_t *vm, u64 *args, u8 *bytecode, u32 len,
                               u64 image_anchor, u8 *stack_base,
                               u32 stack_size, void *stack_mapping,
                               u32 stack_mapping_size) {
  for (int i = 0; i < VM_REG_COUNT; i++)
    vm->R[i] = 0;
  for (int i = 0; i < VM_VECTOR_COUNT; i++)
    for (int j = 0; j < VM_VECTOR_BYTES; j++)
      vm->V[i][j] = 0;

  for (int i = 0; i < 8; i++)
    vm->R[i] = args[i];
  vm->R[29] = args[8];
  vm->R[30] = args[9];

  vm->stack_base = stack_base;
  vm->stack_size = stack_size;
  vm->stack_mapping = stack_mapping;
  vm->stack_mapping_size = stack_mapping_size;
  vm->R[31] = (u64)stack_base + stack_size;

  vm->bc = bytecode;
  vm->bc_buf = bytecode;
  vm->root_bc_buf = bytecode;
  vm->bc_len = len;
  vm->bc_alloc = 0;
  vm->depth = 0;
  vm->frames = 0;
  vm->frame_capacity = 0;
  vm->frame_alloc = 0;

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
  vm->eval_sp = -1;

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
  vm->oc_key = 0;
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
  vm->FL = (result == 0 ? FL_Z : 0) |
           (result & vm_sign_mask(sf) ? FL_N : 0);
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

static inline u64 vm_sdiv64(u64 dividend, u64 divisor_bits) {
  i64 divisor = (i64)divisor_bits;
  i64 value = (i64)dividend;
  if (divisor == 0)
    return 0;
  /* AArch64 SDIV returns INT_MIN for INT_MIN / -1. Express it explicitly to
   * avoid C signed-division overflow undefined behavior. */
  if (value == (i64)(1ULL << 63) && divisor == -1)
    return dividend;
  return (u64)(value / divisor);
}

#endif /* VM_TYPES_H */