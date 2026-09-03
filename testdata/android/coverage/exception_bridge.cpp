#include <stdio.h>

static int destructor_count;

struct Guard {
  int value;
  explicit Guard(int v) : value(v) {}
  ~Guard() { destructor_count += value; }
};

__attribute__((noinline)) static void native_throw(int value) {
  if (value != 0)
    throw value;
}

extern "C" __attribute__((noinline, visibility("default")))
int protected_exception_bridge(int mode) {
  Guard outer(1);
  try {
    Guard inner(2);
    native_throw(mode == 0 ? 0 : 7);
    return 10;
  } catch (int value) {
    if (mode == 2)
      throw;
    return value + 20;
  }
}

int main(void) {
  destructor_count = 0;
  int normal = protected_exception_bridge(0);
  int caught = protected_exception_bridge(1);
  int rethrown = 0;
  try {
    (void)protected_exception_bridge(2);
  } catch (int value) {
    rethrown = value;
  }
  printf("normal=%d caught=%d rethrown=%d destructors=%d\n",
         normal, caught, rethrown, destructor_count);
  /* mode0 destroys inner+outer (3), mode1 destroys inner+outer (3),
     mode2 unwinds inner+outer through the protected boundary (3). */
  return normal == 10 && caught == 27 && rethrown == 7 &&
                 destructor_count == 9
             ? 0
             : 5;
}
