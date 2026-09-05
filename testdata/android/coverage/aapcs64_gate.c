#include <stdint.h>

__attribute__((noinline))
uint64_t protected_aapcs64(uint64_t input, uint64_t *memory) {
    memory[0] = input ^ UINT64_C(0x13579bdf2468ace0);
    memory[1] = (input + UINT64_C(1)) * UINT64_C(0x1020304050607080);
    return memory[0] ^ memory[1];
}
