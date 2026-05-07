#include <stdio.h>

__attribute__((noinline, visibility("default")))
int protected_calc(int value) {
    int mixed = (value * 13) ^ 0x2468;
    if ((mixed & 7) == 3) {
        mixed += 17;
    } else {
        mixed -= 9;
    }
    return mixed;
}

int main(void) {
    int a = protected_calc(321);
    int b = protected_calc(654);
    printf("calc(321)=%d calc(654)=%d\n", a, b);
    return (a == ((321 * 13) ^ 0x2468) - 9 && b == ((654 * 13) ^ 0x2468) - 9) ? 0 : 7;
}
