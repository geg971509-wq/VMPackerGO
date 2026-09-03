#include <pthread.h>
#include <stdatomic.h>
#include <stdint.h>
#include <stdio.h>

#define THREADS 4
#define ITERATIONS 10000

static _Atomic uint64_t counter;

__attribute__((noinline, visibility("default")))
uint64_t protected_increment(_Atomic uint64_t *value) {
  uint64_t previous = atomic_fetch_add_explicit(value, 1, memory_order_acq_rel);
  __asm__ volatile("nop; nop; nop; nop" ::: "memory");
  return previous + 1;
}

static void *worker(void *unused) {
  (void)unused;
  for (int i = 0; i < ITERATIONS; ++i)
    (void)protected_increment(&counter);
  return NULL;
}

int main(void) {
  pthread_t threads[THREADS];
  atomic_store_explicit(&counter, 0, memory_order_relaxed);
  for (int i = 0; i < THREADS; ++i) {
    if (pthread_create(&threads[i], NULL, worker, NULL) != 0)
      return 2;
  }
  for (int i = 0; i < THREADS; ++i) {
    if (pthread_join(threads[i], NULL) != 0)
      return 3;
  }
  uint64_t actual = atomic_load_explicit(&counter, memory_order_acquire);
  uint64_t expected = (uint64_t)THREADS * ITERATIONS;
  printf("counter=%llu expected=%llu\n", (unsigned long long)actual,
         (unsigned long long)expected);
  return actual == expected ? 0 : 4;
}
