#ifndef VMPACKER_VM_NATIVE_H
#define VMPACKER_VM_NATIVE_H

void vm_native_call(vm_ctx_t *vm, u64 target);
u64 vm_atomic_native(u64 kind, u64 width, u64 order, u64 address, u64 first,
                     u64 second);

#endif
