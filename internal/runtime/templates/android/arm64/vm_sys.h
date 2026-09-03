/*
 * vm_sys.h — minimal Linux/Android mapping syscalls without libc.
 */
#ifndef VM_SYS_H
#define VM_SYS_H

static inline int sys_result_failed(long result) {
  return result < 0 && result >= -4095;
}

static inline void *sys_mmap(unsigned long size) {
  register long x8 __asm__("x8") = 222; /* __NR_mmap */
  register long x0 __asm__("x0") = 0;
  register long x1 __asm__("x1") = (long)size;
  register long x2 __asm__("x2") = 3;    /* PROT_READ | PROT_WRITE */
  register long x3 __asm__("x3") = 0x22; /* MAP_PRIVATE | MAP_ANONYMOUS */
  register long x4 __asm__("x4") = -1;
  register long x5 __asm__("x5") = 0;
  __asm__ volatile("svc #0"
                   : "+r"(x0)
                   : "r"(x8), "r"(x1), "r"(x2), "r"(x3), "r"(x4), "r"(x5)
                   : "memory");
  return (void *)x0;
}

static inline long sys_mprotect(void *addr, unsigned long size, long prot) {
  register long x8 __asm__("x8") = 226; /* __NR_mprotect */
  register long x0 __asm__("x0") = (long)addr;
  register long x1 __asm__("x1") = (long)size;
  register long x2 __asm__("x2") = prot;
  __asm__ volatile("svc #0"
                   : "+r"(x0)
                   : "r"(x8), "r"(x1), "r"(x2)
                   : "memory");
  return x0;
}

static inline void sys_munmap(void *addr, unsigned long size) {
  register long x8 __asm__("x8") = 215; /* __NR_munmap */
  register long x0 __asm__("x0") = (long)addr;
  register long x1 __asm__("x1") = (long)size;
  __asm__ volatile("svc #0" : "+r"(x0) : "r"(x8), "r"(x1) : "memory");
}

#endif /* VM_SYS_H */