# VMP Phase 14 — Runtime FDE Discovery / GNU EH Index Integration

## 1. Initial action plan

Goal: make the already-generated runtime `.eh_frame` entries discoverable from the final rewritten ELF by extending the target's GNU unwind index, without yet enabling local C++ landing bridges.

This is a Phase 8 prerequisite for the Phase 7 runtime invoke/landing bridge. The target-local landing requirement introduced in Phase 13 remains fail-closed throughout this phase.

## 2. Plan audit

### Existing reusable implementation

- `internal/unwind/header.go` already parses version-1 `.eh_frame_hdr` and resolves DW_EH_PE pointer encodings into absolute `HeaderEntry` addresses.
- `internal/unwind/frame.go` parses CIE/FDE records and their resolved initial locations.
- the runtime ET_REL retains `.eh_frame` and PREL32 FDE relocations; `RewritePlan` already copies the allocatable runtime `.eh_frame` into the final read-only runtime load and applies relocations after final VA placement.
- the Phase 8 writer already owns the final program-header table and can mutate existing program-header entries deterministically.

### Missing production link

The final writer copies runtime `.eh_frame` bytes into an appended read-only `PT_LOAD`, but does not update `.eh_frame_hdr` or `PT_GNU_EH_FRAME`. Therefore the existing target unwind index does not describe the newly appended runtime FDEs.

This phase fixes the index/discovery layer. It does not claim Android unwinder behavior is complete until the required physical-device gate passes.

### Scope constraints

- Support targets that already contain exactly one `PT_GNU_EH_FRAME` and a parseable version-1 `.eh_frame_hdr`.
- Preserve all original header entries exactly by resolved address, then append runtime FDE entries and sort by initial PC.
- Rebuild a canonical final header using encodings:
  - `eh_frame_ptr`: `DW_EH_PE_pcrel | DW_EH_PE_sdata4` (`0x1b`)
  - FDE count: `DW_EH_PE_udata4` (`0x03`)
  - search table: `DW_EH_PE_datarel | DW_EH_PE_sdata4` (`0x3b`)
- Reject duplicate initial-location keys, duplicate/ambiguous `PT_GNU_EH_FRAME`, unsupported/truncated original header encodings, out-of-range signed 32-bit deltas, or missing final runtime `.eh_frame` FDEs.
- Do not synthesize a new `PT_GNU_EH_FRAME` when the input has none in this phase; that requires coordinated PHDR-slot growth policy and is a separate bounded slice.
- Do not mutate section headers in this phase. Runtime discovery is driven by `PT_GNU_EH_FRAME`; section-table normalization remains a follow-up product-consistency task.

## 3. Corrected implementation plan

### `internal/unwind/header.go`

Add:

```go
func BuildEHFrameHeader(address, ehFrameAddress uint64, entries []HeaderEntry) ([]byte, error)
```

Behavior:
- deterministic strict sort by `InitialLocation`;
- duplicate initial-location rejection;
- canonical encodings listed above;
- signed-32 range checks for every PC-relative/data-relative field;
- no mutation of caller slices.

Round-trip tests must prove parser(builder(...)) preserves resolved addresses.

### Rewrite planning

Add a `gnuEHFramePlan` to `RewritePlan` with original PHDR index, reserved R-segment offset, final VA/file offset, and header size.

Planning flow becomes:
1. reserve runtime allocatable sections as today;
2. parse the original `PT_GNU_EH_FRAME` header and reserve exact final header size using `original entry count + runtime FDE count`;
3. place segments;
4. apply runtime relocations;
5. parse the now-relocated runtime `.eh_frame` from the planned R segment;
6. combine original and runtime FDE index entries and materialize the canonical final header into the reserved R-segment range;
7. generate the final program-header table with the existing `PT_GNU_EH_FRAME` entry redirected to the new header.

The runtime `.eh_frame` address used in the final header's primary pointer remains the original target `.eh_frame` address from the original header. Runtime FDEs may live in the appended R segment because each search-table entry carries an explicit resolved FDE address.

### Program-header planner

Extend `planProgramHeaders` with an optional GNU EH-frame replacement descriptor. It must:
- find exactly the PHDR index discovered during unwind planning;
- preserve `PT_GNU_EH_FRAME` type and read-only semantics;
- replace file offset, VA/PA, file/memory size with the new header;
- keep all load-planning / PT_PHDR logic unchanged.

## 4. Tests

### Unwind unit tests
- canonical build/parse round trip;
- unsorted input is sorted deterministically;
- duplicate initial locations reject;
- pcrel/datarel signed-32 overflow rejects.

### Rewrite-plan tests
Use a targeted fixture helper that adds `.eh_frame`, `.eh_frame_hdr`, and one `PT_GNU_EH_FRAME`:
- final plan has one redirected `PT_GNU_EH_FRAME` inside the appended R load;
- final header preserves original entries and adds runtime FDEs after runtime relocation;
- combined entries are strictly sorted and reference final runtime FDE VAs;
- duplicate original/runtime initial locations reject;
- missing / duplicate `PT_GNU_EH_FRAME` rejects only when unwind-index integration is requested by a runtime image carrying FDEs;
- failure does not mutate input/runtime image/prepared bytecode.

### Exact-r29 integration
On the existing macOS CI runner:
- build the exact-r29 runtime;
- build a rewrite plan for a fixture with a valid GNU EH frame header;
- reparse the new header and assert it contains the runtime `vm_entry_token` / `vm_native_call` FDE initial locations.

## 5. Exit criteria

- appended runtime FDEs are represented in the final GNU EH search table for supported targets;
- existing target FDE index entries are retained;
- no local exception landing bridge is enabled yet;
- no new unsupported PHDR synthesis is hidden inside this phase;
- focused tests/vet and full exact-r29 repository gates pass before merge.

## 6. Follow-up

Phase 15 can then build per-call invoke wrappers around the existing `vm_native_call`, add original landing identity to `InvokeThunk`, generate custom personality/LSDA FDEs, and route landing state back into the VM. The physical Android unwinder test remains the hard release gate.
