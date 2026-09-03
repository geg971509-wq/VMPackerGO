typedef unsigned char u8;
typedef signed char s8;
typedef unsigned short u16;
typedef signed short s16;
typedef unsigned int u32;
typedef signed int s32;
typedef unsigned long long u64;
typedef signed long long s64;
typedef unsigned __int128 u128;
typedef __int128 s128;

#define KEEP __attribute__((noinline, used))

typedef u64 (*vmp_binary_fn)(u64, u64);

struct vmp_pair {
    u64 a;
    u64 b;
};

KEEP u64 vmp_integer64(u64 a, u64 b, u64 shift) {
    u64 r = (a + b) ^ (a - b);
    r ^= a * (b | 1u);
    r ^= a / (b | 1u);
    r ^= a % (b | 1u);
    r ^= (a & b) | (a ^ ~b);
    r ^= a << (shift & 63u);
    r ^= b >> (shift & 63u);
    r ^= (a >> (shift & 63u)) | (a << ((64u - shift) & 63u));
    return r;
}

KEEP u32 vmp_integer32(u32 a, u32 b, u32 shift) {
    u32 r = (a + b) ^ (a - b);
    r ^= a * (b | 1u);
    r ^= a / (b | 1u);
    r ^= (a & b) | (a ^ ~b);
    r ^= a << (shift & 31u);
    r ^= b >> (shift & 31u);
    return r;
}

KEEP s64 vmp_select(s64 a, s64 b, int choose) {
    s64 lo = a < b ? a : b;
    s64 hi = a > b ? a : b;
    return choose == 0 ? lo : (choose > 0 ? hi : -lo);
}

KEEP u64 vmp_control(u64 value, unsigned mode) {
    u64 acc = value;
    for (unsigned i = 0; i < 7; ++i) {
        if ((acc ^ i) & 1u) {
            acc += (u64)i * 3u;
        } else {
            acc ^= acc >> ((i + 1u) & 7u);
        }
    }
    switch (mode & 7u) {
    case 0: return acc + 1u;
    case 1: return acc ^ 0x55aa55aa55aa55aaULL;
    case 2: return acc - 9u;
    case 3: return acc << 3;
    case 4: return acc >> 5;
    case 5: return acc * 17u;
    case 6: return acc | 0x8000000000000000ULL;
    default: return ~acc;
    }
}

KEEP u64 vmp_direct_target(u64 a, u64 b) {
    return (a * 5u) + (b ^ 0x123456789abcdef0ULL);
}

KEEP u64 vmp_calls(vmp_binary_fn fn, u64 a, u64 b) {
    u64 direct = vmp_direct_target(a, b);
    u64 indirect = fn(a ^ direct, b + 3u);
    return direct ^ indirect;
}

KEEP u64 vmp_memory(const u8 *bytes, const s8 *sbytes,
                    const u16 *halves, const s16 *shalves,
                    const u32 *words, const s32 *swords,
                    const u64 *dwords, const struct vmp_pair *pairs,
                    u64 index, u64 *out) {
    u64 local[4];
    local[0] = bytes[index & 7u];
    local[1] = (u64)(s64)sbytes[(index + 1u) & 7u];
    local[2] = halves[index & 3u] ^ (u64)(s64)shalves[(index + 1u) & 3u];
    local[3] = words[index & 3u] ^ (u64)(s64)swords[(index + 2u) & 3u];
    struct vmp_pair pair = pairs[index & 1u];
    u64 result = local[0] + local[1] + local[2] + local[3] + dwords[index & 1u];
    result ^= pair.a + pair.b;
    out[0] = result;
    out[1] = local[3];
    return result;
}

KEEP u64 vmp_wide(u64 a, u64 b, s64 sa, s64 sb, u32 narrow) {
    u128 wide = (u128)a * (u128)b;
    s128 signed_wide = (s128)sa * (s128)sb;
    u64 high = (u64)(wide >> 64);
    u64 signed_high = (u64)(signed_wide >> 64);
    u64 madd = a * b + (u64)narrow;
    return high ^ signed_high ^ madd ^ (u64)(u32)a ^ (u64)(s64)(s32)narrow;
}

KEEP u64 vmp_abi_pressure(u64 a0, u64 a1, u64 a2, u64 a3,
                          u64 a4, u64 a5, u64 a6, u64 a7,
                          u64 a8, u64 a9, u64 a10, u64 a11) {
    u64 x0 = a0 + a8;
    u64 x1 = a1 ^ a9;
    u64 x2 = a2 * (a10 | 1u);
    u64 x3 = a3 + a11;
    u64 x4 = (a4 << 7) | (a4 >> 57);
    u64 x5 = (a5 & a6) | (~a5 & a7);
    return (x0 ^ x1) + (x2 ^ x3) + (x4 ^ x5);
}

#define DEFINE_ATOMIC_CORPUS(bits, type) \
KEEP type vmp_atomic##bits(_Atomic(type) *p, type value) { \
    type relaxed = __c11_atomic_load(p, __ATOMIC_RELAXED); \
    type acquired = __c11_atomic_load(p, __ATOMIC_ACQUIRE); \
    __c11_atomic_store(p, value, __ATOMIC_RELAXED); \
    __c11_atomic_store(p, (type)(value + 1), __ATOMIC_RELEASE); \
    type added = __c11_atomic_fetch_add(p, (type)3, __ATOMIC_ACQ_REL); \
    type exchanged = __c11_atomic_exchange(p, value, __ATOMIC_SEQ_CST); \
    type expected = exchanged; \
    (void)__c11_atomic_compare_exchange_strong(p, &expected, (type)(value ^ 1), \
                                                __ATOMIC_ACQ_REL, __ATOMIC_ACQUIRE); \
    return (type)(relaxed ^ acquired ^ added ^ exchanged ^ expected); \
}

DEFINE_ATOMIC_CORPUS(8, u8)
DEFINE_ATOMIC_CORPUS(16, u16)
DEFINE_ATOMIC_CORPUS(32, u32)
DEFINE_ATOMIC_CORPUS(64, u64)

KEEP u128 vmp_atomic128(_Atomic(u128) *p, u128 value) {
    u128 relaxed = __c11_atomic_load(p, __ATOMIC_RELAXED);
    u128 acquired = __c11_atomic_load(p, __ATOMIC_ACQUIRE);
    __c11_atomic_store(p, value, __ATOMIC_RELEASE);
    u128 exchanged = __c11_atomic_exchange(p, value + 1u, __ATOMIC_SEQ_CST);
    u128 expected = exchanged;
    (void)__c11_atomic_compare_exchange_strong(p, &expected, value ^ 1u,
                                                __ATOMIC_ACQ_REL, __ATOMIC_ACQUIRE);
    return relaxed ^ acquired ^ exchanged ^ expected;
}
