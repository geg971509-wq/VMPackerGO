#include <dlfcn.h>
#include <stdio.h>
#include <stdlib.h>

typedef int (*check_fn)(void *, void *, int);

int main(int argc, char **argv) {
    if (argc != 2) {
        fprintf(stderr, "usage: %s libnative_demo.so\n", argv[0]);
        return 2;
    }
    void *h = dlopen(argv[1], RTLD_NOW);
    if (!h) {
        fprintf(stderr, "dlopen failed: %s\n", dlerror());
        return 3;
    }
    check_fn fn = (check_fn)dlsym(h, "Java_com_example_demo_NativeBridge_checkLicense");
    if (!fn) {
        fprintf(stderr, "dlsym failed: %s\n", dlerror());
        return 4;
    }
    int a = fn(NULL, NULL, 1234);
    int b = fn(NULL, NULL, 1111);
    printf("check(1234)=%d check(1111)=%d\n", a, b);
    dlclose(h);
    return (a == 29711 && b == 19398) ? 0 : 5;
}
