#ifndef VMPACKER_VM_ABI_H
#define VMPACKER_VM_ABI_H

/* Assembly-visible vm_ctx_t prefix. Keep vm_types.h static assertions in sync. */
#define VM_CTX_R 0
#define VM_CTX_V 256
#define VM_CTX_FPCR 768
#define VM_CTX_FPSR 772
#define VM_CTX_FL 776
#define VM_CTX_FAULT 780

#endif
