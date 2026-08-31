/*
 * Exact-NDK-r29 FP/SIMD compiler corpus.
 *
 * This file intentionally has no libc dependency.  Each noinline entry keeps a
 * single language-level operation visible at -O0, -O2, and -Oz so the compiler
 * output can be inventoried without relying on handwritten assembly.
 */

typedef float f32x4 __attribute__((vector_size(16)));
typedef double f64x2 __attribute__((vector_size(16)));
typedef unsigned int u32x4 __attribute__((vector_size(16)));
typedef unsigned char u8x16 __attribute__((vector_size(16)));
typedef unsigned long long u64x2 __attribute__((vector_size(16)));

#define KEEP __attribute__((noinline, used, visibility("hidden")))

KEEP float corpus_f32_add(float a, float b) { return a + b; }
KEEP float corpus_f32_sub(float a, float b) { return a - b; }
KEEP float corpus_f32_mul(float a, float b) { return a * b; }
KEEP float corpus_f32_div(float a, float b) { return a / b; }
KEEP float corpus_f32_neg(float a) { return -a; }
KEEP float corpus_f32_abs(float a) { return __builtin_fabsf(a); }

KEEP double corpus_f64_add(double a, double b) { return a + b; }
KEEP double corpus_f64_sub(double a, double b) { return a - b; }
KEEP double corpus_f64_mul(double a, double b) { return a * b; }
KEEP double corpus_f64_div(double a, double b) { return a / b; }
KEEP double corpus_f64_neg(double a) { return -a; }
KEEP double corpus_f64_abs(double a) { return __builtin_fabs(a); }

KEEP int corpus_f32_eq(float a, float b) { return a == b; }
KEEP int corpus_f32_lt(float a, float b) { return a < b; }
KEEP int corpus_f64_le(double a, double b) { return a <= b; }

KEEP int corpus_f32_to_i32(float a) { return (int)a; }
KEEP unsigned corpus_f64_to_u32(double a) { return (unsigned)a; }
KEEP float corpus_i32_to_f32(int a) { return (float)a; }
KEEP double corpus_u32_to_f64(unsigned a) { return (double)a; }
KEEP double corpus_f32_to_f64(float a) { return (double)a; }
KEEP float corpus_f64_to_f32(double a) { return (float)a; }

KEEP f32x4 corpus_v4f_add(f32x4 a, f32x4 b) { return a + b; }
KEEP f32x4 corpus_v4f_sub(f32x4 a, f32x4 b) { return a - b; }
KEEP f32x4 corpus_v4f_mul(f32x4 a, f32x4 b) { return a * b; }
KEEP f64x2 corpus_v2d_add(f64x2 a, f64x2 b) { return a + b; }
KEEP f64x2 corpus_v2d_mul(f64x2 a, f64x2 b) { return a * b; }

KEEP u32x4 corpus_v4u_and(u32x4 a, u32x4 b) { return a & b; }
KEEP u32x4 corpus_v4u_or(u32x4 a, u32x4 b) { return a | b; }
KEEP u32x4 corpus_v4u_xor(u32x4 a, u32x4 b) { return a ^ b; }
KEEP u32x4 corpus_v4u_not(u32x4 a) { return ~a; }

KEEP u8x16 corpus_v16u8_imm1(void) {
  return (u8x16){1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1};
}
KEEP u8x16 corpus_v16u8_imm55(void) {
  return (u8x16){0x55, 0x55, 0x55, 0x55, 0x55, 0x55, 0x55, 0x55,
                 0x55, 0x55, 0x55, 0x55, 0x55, 0x55, 0x55, 0x55};
}
KEEP u8x16 corpus_v16u8_immaa(void) {
  return (u8x16){0xaa, 0xaa, 0xaa, 0xaa, 0xaa, 0xaa, 0xaa, 0xaa,
                 0xaa, 0xaa, 0xaa, 0xaa, 0xaa, 0xaa, 0xaa, 0xaa};
}
KEEP u64x2 corpus_v2u64_zero(void) { return (u64x2){0, 0}; }
KEEP u64x2 corpus_v2u64_ones(void) { return (u64x2){~0ULL, ~0ULL}; }
KEEP u64x2 corpus_v2u64_mask(void) {
  return (u64x2){0xff00ff00ff00ff00ULL, 0xff00ff00ff00ff00ULL};
}

KEEP f32x4 corpus_v4f_load(const f32x4 *p) { return *p; }
KEEP void corpus_v4f_store(f32x4 *p, f32x4 v) { *p = v; }
