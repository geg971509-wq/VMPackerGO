#include <dlfcn.h>
#include <inttypes.h>
#include <stdint.h>
#include <stdio.h>

struct observation {
    uint64_t result;
    uint64_t callee_saved[11];
    uint64_t sp;
};

extern uint64_t probe_aapcs64(uint64_t (*target)(uint64_t, uint64_t *),
                              uint64_t input, uint64_t *memory,
                              struct observation *observation);

int main(int argc, char **argv) {
    if (argc != 2) {
        fprintf(stderr, "usage: %s <shared-library>\n", argv[0]);
        return 2;
    }
    void *handle = dlopen(argv[1], RTLD_NOW | RTLD_LOCAL);
    if (!handle) {
        fprintf(stderr, "dlopen failed\n");
        return 3;
    }
    uint64_t (*target)(uint64_t, uint64_t *) =
        (uint64_t(*)(uint64_t, uint64_t *))dlsym(handle, "protected_aapcs64");
    if (!target) {
        fprintf(stderr, "dlsym failed\n");
        dlclose(handle);
        return 4;
    }
    uint64_t memory[3] = {UINT64_C(0xaaaaaaaaaaaaaaaa), UINT64_C(0xbbbbbbbbbbbbbbbb),
                          UINT64_C(0xcccccccccccccccc)};
    struct observation observation = {0};
    uint64_t result = probe_aapcs64(target, 10, memory, &observation);
    uint64_t expected0 = UINT64_C(10) ^ UINT64_C(0x13579bdf2468ace0);
    uint64_t expected1 = UINT64_C(11) * UINT64_C(0x1020304050607080);
    uint64_t expected_result = expected0 ^ expected1;
    uint64_t expected2 = UINT64_C(0x1900) ^ UINT64_C(0x3800) ^ UINT64_C(0x3f00);
    printf("AAPCS64 return=%016" PRIx64 " memory=%016" PRIx64 "%016" PRIx64 "%016" PRIx64,
           observation.result, memory[0], memory[1], memory[2]);
    for (size_t index = 0; index < 11; ++index) {
        printf(" x%zu=%016" PRIx64, index + 19, observation.callee_saved[index]);
    }
    printf(" sp=%016" PRIx64 "\n", observation.sp);
    int status = result == expected_result && observation.result == expected_result &&
                 memory[0] == expected0 && memory[1] == expected1 &&
                 memory[2] == expected2 && observation.sp != 0;
    for (size_t index = 0; index < 11; ++index) {
        status = status && observation.callee_saved[index] == UINT64_C(0x1900) + index * UINT64_C(0x100);
    }
    dlclose(handle);
    return status ? 0 : 1;
}
