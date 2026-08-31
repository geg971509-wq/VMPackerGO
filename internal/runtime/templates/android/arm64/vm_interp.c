/*
 * vm_interp.c — 模块化 Android/AArch64 PIC VM runtime
 *
 * 架构:
 *   vm_types.h       → 类型 + vm_ctx_t 结构体
 *   vm_opcodes.h     → 操作码定义
 *   vm_decode.h      → 字节码读取工具
 *   vm_handlers      → 分模块指令 handler
 *
 * internal/runtime 使用准确版本的 Android NDK r29，将本文件与显式汇编入口
 * 编译并以 ld.lld -r 链接为保留节、符号、重定位与展开信息的 ET_REL Image。
 */

/* ---- 基础设施 ---- */

#include "vm_decode.h"
#include "vm_opcodes.h"
#include "vm_types.h"
#include "vm_native.h"
#include "vm_sys.h"
#include "vm_call.h"

/* ---- 指令 Handler 模块 ---- */
#include "vm_handlers/h_alu.h" /* ADD/SUB/MUL/XOR/AND/OR/SHL/SHR/ASR/NOT/ROR + _IMM */
#include "vm_handlers/h_cmp.h"    /* CMP, CMP_IMM */
#include "vm_handlers/h_branch.h" /* JMP/JE/JNE/JL/JGE/JGT/JLE/JB/JAE */
#include "vm_handlers/h_mem.h"    /* LOAD/STORE 8/32/64 */
#include "vm_handlers/h_mov.h"    /* MOV_IMM, MOV_IMM32, MOV_REG */
#include "vm_handlers/h_stack.h"  /* PUSH, POP */
#include "vm_handlers/h_stack_ops.h" /* 栈机器操作 handler (VLOAD/VSTORE/VADD...) */
#include "vm_handlers/h_system.h" /* NOP, CALL_NAT, BR_REG, VLD16, VST16 */
#include "vm_svc.h"               /* per-pack SVC immediate thunks */
#include "vm_exclusive.h"         /* continuous LDAXR...STLXR thunks */
#include "vm_fpsimd.h"            /* exact-r29-derived FP/SIMD thunks */


/* ---- 间接 Dispatch 跳转表 ---- */
#include "vm_dispatch.h"

/* ---- Token 化入口 (条件编译) ---- */
/* TOKEN_ONLY: Token 入口始终编译 */
#include "vm_token.h"

/*
 * vm_entry — VM 解释器入口
 *
 * 参数:
 *   args    : 指向保存的 X0-X7, callerFP, callerLR (共10个u64)
 *   enc_bc  : XOR 加密的字节码
 *   bc_len  : 字节码长度
 *   xor_key : XOR 解密密钥
 *
 * 返回: R[0] (模拟 X0 返回值)
 */
__attribute__((section(".text.entry"))) u64 vm_entry(u64 *args, u8 *enc_bc,
                                                     u32 bc_len, u8 xor_key,
                                                     u64 image_anchor);

/* ================================================================
 * Token 化入口 (条件编译)
 *
 * Token trampoline (3 条指令):
 *   MOV  W16, #token_lo16
 *   MOVK W16, #token_hi16, LSL#16
 *   B    vm_entry_token
 *
 * X16 (IP0) 传递 token，X0-X7 保持调用方原始参数不变。
 * vm_entry_token_asm 负责保存寄存器并调用 vm_entry_token_inner。
 * ================================================================ */
/* TOKEN_ONLY: Token 入口始终编译 */

/* Packer 在 payload 中 patch 此变量为 token 描述符表的 VA */
__attribute__((section(".data.entry"), used)) volatile u64 _token_table_va = 0;
__attribute__((section(".data.entry"), used)) volatile u64 _image_file_va = 0;
__attribute__((section(".data.entry"), used)) volatile u64 _token_count = 0;

/* 内部 C 函数: 解码 token 并调用 vm_entry */
__attribute__((noinline, section(".text.entry"))) u64
vm_entry_token_inner(u64 *args, u32 token) {
  u32 func_id = TOKEN_FUNC_ID(token);

  /* PIE 兼容: _token_table_va 存储的是相对于自身地址的偏移
   * 用 ADR 获取 _token_table_va 的运行时地址 (PC-relative, ±1MB)
   * 然后加上偏移得到 token 描述符表的实际地址 */
  u64 self_va;
  __asm__ volatile("adr %0, _token_table_va" : "=r"(self_va));
  u64 tbl_off = *(volatile u64 *)&_token_table_va;
  u32 n = (u32)(*(volatile u64 *)&_token_count);
  if (__builtin_expect(tbl_off == 0, 0))
    return 0; /* 表未初始化, 安全退出 */
  if (__builtin_expect(n == 0 || func_id >= n || n > TOKEN_MAX_FUNCS, 0))
    return 0;

  token_desc_t *table = (token_desc_t *)(self_va + tbl_off);
  /* bc_off 也是相对于 _token_table_va 的偏移 */
  u8 *enc_bc = (u8 *)(self_va + table[func_id].bc_off);
  u32 bc_len = table[func_id].bc_len;
  u8 xor_key = table[func_id].xor_key;

  if (__builtin_expect(enc_bc == (u8 *)self_va || bc_len == 0, 0))
    return 0; /* 无效条目, 安全退出 */

  return vm_entry(args, enc_bc, bc_len, xor_key, self_va);
}

/* vm_entry_token is implemented in vm_entry.S so BTI, PAC, and CFI are explicit. */

/* ---- vm_entry 实现 ---- */
__attribute__((section(".text.entry"))) u64 vm_entry(u64 *args, u8 *enc_bc,
                                                     u32 bc_len, u8 xor_key,
                                                     u64 image_anchor) {
  u64 ret = 0;

  /* ---- 1. 动态分配字节码缓冲区 (mmap, 替代栈上 64KB) ---- */
  if (bc_len > VM_BYTECODE_MAX)
    bc_len = VM_BYTECODE_MAX;
  u32 alloc_size = (bc_len + 4095u) & ~4095u; /* 页对齐向上取整 */
  u8 *bc_buf = (u8 *)sys_mmap(alloc_size);
  if ((long)bc_buf < 0)
    return 0; /* mmap 失败, 安全退出 */

  /* ---- 1b. XOR 解密 (8 字节加宽, ~8x 加速) ---- */
  u64 xk8 = (u64)xor_key;
  xk8 |= xk8 << 8;
  xk8 |= xk8 << 16;
  xk8 |= xk8 << 32;
  {
    u32 n8 = bc_len >> 3;
    u64 *d8 = (u64 *)bc_buf;
    const u64 *s8 = (const u64 *)enc_bc;
    for (u32 i = 0; i < n8; i++)
      d8[i] = s8[i] ^ xk8;
    for (u32 i = n8 << 3; i < bc_len; i++)
      bc_buf[i] = enc_bc[i] ^ xor_key;
  }

  /* ---- 2b. 初始化 VM 上下文 (mmap 堆分配, 避免 Go/Rust 协程栈溢出) ---- */
  u32 ctx_alloc = (sizeof(vm_ctx_t) + 4095u) & ~4095u;
  vm_ctx_t *vm = (vm_ctx_t *)sys_mmap(ctx_alloc);
  if ((long)vm < 0) {
    sys_munmap(bc_buf, alloc_size);
    return 0;
  }
  vm_ctx_init(vm, args, bc_buf, bc_len, image_anchor);
  vm->bc_alloc = alloc_size;
  vm_apply_trailer(vm, bc_buf, bc_len);

/* ---- OpcodeCryptor 解密宏 (间接分发路径使用) ---- */
#define OC_DECRYPT(pc, key) ((u8)((key) ^ ((pc) * 0x9E3779B9u)))

  /* ================================================================
   * 唯一的间接 Dispatch 路径: 相对偏移跳转表 + 函数指针间接调用，
   * 使 IDA Pro 等静态分析工具无法追踪所有 handler 目标地址。
   * ================================================================ */

  /* ---- 运行时初始化跳转表（栈上分配，避免共享可写分发表） ---- */
  vm_handler_fn vm_jump_table[256];
  vm_init_jump_table(vm_jump_table);

  /* ---- PC 初始化: reverse 模式从 bc_len 开始 ---- */
  if (vm->reverse) {
    vm->pc = vm->bc_len;
  }

  /* ---- 间接 Dispatch 主循环 ---- */
  for (;;) {
    /* -- 反向/正向 PC 定位 -- */
    if (vm->reverse) {
      if (__builtin_expect((i64)vm->pc <= 0, 0))
        break;
      vm->pc--;
      if (__builtin_expect(vm->pc >= vm->bc_len, 0))
        break;
      u8 _sz = vm->bc[vm->pc];
      if (__builtin_expect(_sz > vm->pc, 0))
        break;
      vm->pc -= _sz;
    } else {
      if (__builtin_expect(vm->pc >= vm->bc_len, 0))
        break;
    }

    /* -- OpcodeCryptor 解密 -- */
    u8 _raw_op = vm->bc[vm->pc];
    u8 _dec_op = _raw_op ^ OC_DECRYPT(vm->pc, vm->oc_key);

    /* -- 指令大小校验 -- */
    u8 _isz = vm_insn_size(_dec_op);
    if (__builtin_expect(_isz == 0 || vm->pc + _isz > vm->bc_len, 0))
      break;

    /* -- 特殊处理: HALT / RET (不经过跳转表) -- */
    if (_dec_op == OP_HALT) {
      ret = vm->R[0];
      if (vm->depth > 0) {
        vm_pop_frame(vm);
        continue;
      }
      goto cleanup;
    }
    if (_dec_op == OP_RET) {
      u8 _r = vm->bc[vm->pc + 1];
      ret = vm->R[_r & 31];
      if (vm->depth > 0) {
        vm->R[0] = ret;
        vm_pop_frame(vm);
        continue;
      }
      goto cleanup;
    }

    /* -- 间接 Dispatch: 直接从跳转表取函数指针调用 -- */
#ifdef VM_DEBUG_TRACE
    /* -- Debug trace: 输出 pc+op 到 stderr -- */
    {
      u8 _tbuf[16];
/* 内联计算十六进制字符 (避免 static 数据引用) */
#define _HX(n) ((u8)((n) < 10 ? '0' + (n) : 'A' + (n) - 10))
      _tbuf[0] = _HX((vm->pc >> 12) & 0xF);
      _tbuf[1] = _HX((vm->pc >> 8) & 0xF);
      _tbuf[2] = _HX((vm->pc >> 4) & 0xF);
      _tbuf[3] = _HX(vm->pc & 0xF);
      _tbuf[4] = ':';
      _tbuf[5] = _HX((_dec_op >> 4) & 0xF);
      _tbuf[6] = _HX(_dec_op & 0xF);
      _tbuf[7] = '\n';
#undef _HX
      register long _x8 __asm__("x8") = 64; /* __NR_write */
      register long _x0 __asm__("x0") = 2;  /* stderr */
      register long _x1 __asm__("x1") = (long)_tbuf;
      register long _x2 __asm__("x2") = 8;
      __asm__ volatile("svc #0"
                       : "+r"(_x0)
                       : "r"(_x8), "r"(_x1), "r"(_x2)
                       : "memory");
    }
#endif
    vm_handler_fn _handler = vm_jump_table[_dec_op];
    u32 _step = _handler(vm);

    if (__builtin_expect(vm->fault != 0, 0)) {
      ret = 0;
      goto cleanup;
    }

    /* -- 检查 HALT 哨兵 (wrap_unknown 等返回) -- */
    if (__builtin_expect(_step == VM_STEP_HALT || _step == VM_STEP_RET, 0)) {
      ret = vm->R[0];
      if (vm->depth > 0) {
        vm_pop_frame(vm);
        continue;
      }
      goto cleanup;
    }

    /* -- 推进 PC -- */
    /* _step == 0: 分支 handler 已直接设置 pc, 不推进 */
    /* _step > 0 且非 reverse: 正常推进 */
    if (_step > 0 && !vm->reverse) {
      vm->pc += _step;
    }
  }



  /* ---- 统一退出: 释放 mmap 防止泄漏 ---- */
cleanup:
  vm_unwind_frames(vm);
  if (vm->bc_buf && vm->bc_alloc && vm->bc_buf != bc_buf)
    sys_munmap(vm->bc_buf, vm->bc_alloc);
  sys_munmap(vm, ctx_alloc);
  sys_munmap(bc_buf, alloc_size);
  return ret;
}
