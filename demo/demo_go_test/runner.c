#include <dlfcn.h>
#include <stdint.h>
#include <stdio.h>

int main(int argc, char **argv) {
  if (argc != 2) {
    fprintf(stderr, "usage: %s <go-shared-library>\n", argv[0]);
    return 2;
  }
  void *handle = dlopen(argv[1], RTLD_NOW | RTLD_LOCAL);
  if (!handle) {
    fprintf(stderr, "dlopen failed\n");
    return 3;
  }
  uint64_t (*check_key)(uint64_t) = (uint64_t(*)(uint64_t))dlsym(handle, "check_key");
  if (!check_key) {
    fprintf(stderr, "dlsym failed\n");
    dlclose(handle);
    return 4;
  }
  uint64_t result = check_key(10);
  printf("checkKey(10) = %llu\n", (unsigned long long)result);
  if (result == 143) {
    printf("[+] OK\n");
    dlclose(handle);
    return 0;
  }
  printf("[-] FAIL\n");
  dlclose(handle);
  return 1;
}
