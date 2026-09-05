#include <stdint.h>

__attribute__((noinline))
uint64_t protected_aapcs64(uint64_t input, uint64_t *memory) {
    uint64_t incoming_x19;
    uint64_t incoming_v8;
    uint64_t incoming_v15;
    __asm__ volatile("mov %0, x19" : "=r"(incoming_x19));
    __asm__ volatile("umov %0, v8.d[0]" : "=r"(incoming_v8));
    __asm__ volatile("umov %0, v15.d[0]" : "=r"(incoming_v15));
    memory[0] = input ^ UINT64_C(0x13579bdf2468ace0);
    memory[1] = (input + UINT64_C(1)) * UINT64_C(0x1020304050607080);
    memory[2] = incoming_x19 ^ incoming_v8 ^ incoming_v15;
    return memory[0] ^ memory[1];
}
