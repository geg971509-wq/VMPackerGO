# VMP Phase 20A — Android loaded-PHDR correctness

## Goal

Repair an Android loader blocker where a rewritten ET_DYN/PIE can remain host-parseable while the final `PT_PHDR` points at a program-header table that is not fully covered by the serialized `PT_LOAD` extents seen by `linker64`.

Observed device shape:

- original `e_phoff` is inside the initial loadable ELF header mapping;
- rewrite expands the program-header count and relocates the table into appended read-only storage;
- `PT_PHDR` is updated to the relocated file/virtual address;
- the final serialized `PT_LOAD.p_filesz` may still advertise the pre-PHDR boundary;
- Android `linker64` therefore rejects the loaded PHDR range before normal execution begins.

## Consensus

Android does not require the PHDR table to live specifically in the first `PT_LOAD` when a valid `PT_PHDR` exists. It does require the complete loaded PHDR range to be contained by a loadable segment. The writer must therefore enforce the actual loader invariant instead of relying only on host `debug/elf` parsing.

Keep `RewritePlan` authoritative. Do not recalculate placement or shift arbitrary ELF contents in the writer. The writer may, however, serialize added `PT_LOAD` entries from the final approved segment extents so the output table cannot retain stale pre-growth `p_filesz/p_memsz` values.

## Repair

1. Materialize the program-header table into a fresh buffer before publication.
2. Re-encode every added runtime `PT_LOAD` from the final `RewritePlan.segments` offsets, VAs, flags, file sizes and memory sizes.
3. Preserve planned `PT_PHDR` and `PT_GNU_EH_FRAME` mutations.
4. After artifact assembly, validate an Android-equivalent loaded-PHDR invariant:
   - one exact `PT_PHDR`, when present, must describe `e_phoff/e_phnum`;
   - otherwise the first `PT_LOAD` must have `p_offset == 0` so the ELF-header fallback is valid;
   - the complete PHDR file range and loaded VA range must both fit within one `PT_LOAD.p_filesz` range;
   - the file and VA offsets within that load must be identical.
5. Reject instead of publishing if the invariant cannot be proven.

## Regression coverage

- normal relocated `PT_PHDR` remains structurally valid and Android-loadable;
- reproduce stale serialized trailing-load extent while the plan segment already includes the PHDR bytes, and prove the writer synchronizes the output without mutating the plan;
- corrupt `PT_PHDR.p_vaddr` outside all loadable segments and require fail-closed rejection.

## Merge policy

This blocker is isolated from CASP work. Merge only after the existing full Verification gate executes successfully on the exact PR head. After merge, run the same Verification on `main`, then re-run the physical Android boot/app test after reboot as required by the device workflow.
