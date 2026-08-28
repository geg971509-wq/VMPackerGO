# VMPacker Productization Handoff

**Snapshot date:** 2026-08-28

**Repository:** `/Volumes/work/android/VMPacker`

**Branch:** `master`

**HEAD:** `6a981e8316de4f1fd307cac6c0ad0f21970e42c0`

**Approved master plan:** `/Users/king/.claude/plans/crispy-giggling-fog.md`
**Overall status:** In progress. Phases 0–3 are implemented and independently verified. Phase 4 is partially implemented and the repository is currently between compiling states because ELF tests have not yet been updated for the new opcode-map helper signatures. The product is not release-ready.

## 1. Non-negotiable product contract

The finished product must satisfy all of the following:

- Publish one signed and notarized macOS ARM64 CLI.
- Do not publish a GUI, APK/AAB workflow, Linux/Windows host binary, or stable public Go SDK.
- Accept compiled Android AArch64 ELF64 inputs:
  - shared objects: `ET_DYN + PT_DYNAMIC`, without `PT_INTERP`;
  - Android PIE executables: `ET_DYN` with interpreter/dynamic metadata;
  - Android `ET_EXEC` native executables.
- Support Android API 23+ and both 4 KiB and 16 KiB device page configurations.
- Preserve correct ASLR, BTI, PAC, common FP/SIMD, atomic/exclusive/barrier, raw SVC, complete internal AAPCS64 calls, and C++ exception/unwind behavior.
- Require an explicit protected-entry ABI:
  - at most eight integer/pointer parameters;
  - result is `void` or one integer/pointer value.
- Support complete AAPCS64 for internal native calls in the later bridge phase.
- Reject or fail closed for unsupported control flow, including unresolved CFGs, statically provable `setjmp`/`longjmp`/signal-recovery paths, and exclusive regions that cannot be closed.
- Limits:
  - input: 1 GiB;
  - functions per invocation: 4096;
  - final bytecode per function: 64 KiB.
- Rebuild the runtime from embedded templates for every pack using exact Android NDK r29 revision `29.0.14206865`.
- Use `crypto/rand` by default. A 64-hex-character `-seed` is test/debug-only and makes a run reproducible.
- Never record the seed, raw opcode map, NDK absolute path, temp paths, signing credentials, keys, or secrets in reports/logs.
- Default to no-clobber. `-force` must use safe same-directory atomic replacement.
- Default to stripping static symbols while preserving dynamic symbols.
- Debug map is off by default and, when requested, is mode `0600`.
- Keep report schema v1 stable and include only a one-way opcode-map digest.
- Raise reverse-analysis cost; do not claim absolute protection.
- License: AGPL-3.0-only; `Copyright (C) 2026 LeoChen`.

## 2. Git and workspace safety

- No commit, push, PR, release, or new tag has been requested.
- The working tree contains a large mixture of staged, unstaged, renamed, and untracked Phase 0–4 work. Do not reset, restore, clean, or overwrite it.
- Do not use `git add -A`, `git reset --hard`, `git checkout .`, `git clean`, or any force operation.
- Preserve all current archive moves and new `internal/` files.
- Before any future commit, inspect staged and unstaged changes separately and stage explicit paths only.
- Tool-generated untracked directories currently exist and are not product assets:
  - `.claude/workflow-runs/`
  - `.jspace/`
- Remove those tool artifacts only after confirming they contain no required handoff evidence. Do not include them in a product commit.

## 3. Completed and verified work

### Phase 0 — Product contract and guards

Implemented:

- `docs/product-contract.md`
- `docs/development.md`
- `docs/report-schema-v1.md`
- `NOTICE`
- `scripts/check-contract.sh`
- `scripts/check-contract-test.sh`
- README scope synchronization and `.gitignore` updates

Verified:

```text
bash scripts/check-contract.sh
[contract] active product contract passed

bash scripts/check-contract-test.sh
check-contract self-test passed
```

### Phase 1 — Archive old product surfaces and internalize packages

Implemented:

- GUI moved under `archive/vmp-gui/`.
- APK package/workflow moved under `archive/apk-workflow/`.
- Archive modules have nested `go.mod` boundaries so root `go list ./...` excludes them.
- Active Go packages moved from `pkg/` to `internal/`.
- Root product surface is CLI + internal implementation + runtime/demo/test material.

Do not repair or reconnect archived code.

### Phase 2 — CLI, ABI, reports, and safe publication

Implemented:

- `internal/app` CLI boundary with context-aware `Run`/`RunWithConfig`.
- Strict direct and manifest selections.
- Explicit integer/pointer entry ABI parser under `internal/abi`.
- Strict manifest-v1 parsing, duplicate-key rejection, selection limits, and path collision checks.
- Bounded opened-file input reads.
- Typed report schema v1 under `internal/report`.
- Same-directory temporary publication, no-clobber hard-link publication, atomic force replacement, rollback snapshots, error aggregation, and artifact-last publication under `internal/publish`.
- Version/commit injection boundary in `cmd/vmpacker/version.go`.

Previously independently verified against path aliases, existing destinations, symlinks, rollback failures, write/sync/close failures, permissions, cancellation, report privacy, and artifact-last behavior.

### Phase 3 — Bounded ELF analysis and conservative CFG

Implemented:

- `internal/elf/parse.go`
- `internal/elf/symbols.go`
- `internal/elf/selection.go`
- `internal/elf/cfg.go`
- extensive generated/malformed tests in `internal/elf/analysis_test.go`

Key properties:

- Little-endian AArch64 ELF64 only.
- Guarded header/segment/section/range arithmetic.
- Extended numbering fails closed before the legacy writer.
- Stale `PT_NULL` fields are sanitized only in the private parse copy.
- LOAD overlap validation uses sorted sweeps rather than attacker-controlled O(n²) production checks.
- Symtab/dynsym definitions are merged; identical definitions deduplicate; conflicting names fail while valid addresses still constrain CFG boundaries.
- Explicit ranges are aligned, executable, file-backed, bounded, non-overlapping, and long enough.
- CFG inference supports direct successors, loops, multiple returns, and calls with fallthrough; indirect/gap/unknown/external tail behavior fails closed.
- Direct recovery-API detection covers defined symbols, `CALL26`/`JUMP26` relocations, and exact r29 PLT/JUMP_SLOT structure.
- PLT entries are bound to their actual GOT slot by decoded `ADRP/LDR/ADD/BR` arithmetic.
- `.got.plt` must be exactly `SHF_ALLOC|SHF_WRITE`, non-executable, aligned, and file-backed by one writable non-executable LOAD.
- Renamed/non-PLT JUMP_SLOT relocations fail closed.
- Runtime offsets were validated before mutation in the legacy blob path.
- Limits and partial failure reporting are enforced.

Final Phase 3 independent result:

- Security review: `APPROVE`.
- Verification verdict: `PASS`.
- Full tests, full race, vet, temp CLI build, >10-second parser fuzz, exact installed-r29 PLT test, contract guards, gofmt, and active-tree whitespace scan passed at the Phase 3 gate.

Important: the repository is no longer globally green because Phase 4 changed VM/translator APIs after that gate. The Phase 3 verdict remains evidence for the parser/CFG implementation, not a claim that the current mixed Phase 4 tree builds.

## 4. Phase 4 current work state

### 4.1 Completed VM opcode core

Current files:

- `internal/vm/opcodes.go`
- `internal/vm/disasm.go`
- `internal/vm/opcode_test.go`

Implemented:

- Exactly 115 dense semantic `Opcode` IDs.
- One canonical definition table with:
  - debug name;
  - instruction size;
  - C macro name;
  - legacy identity wire byte;
  - branch target operand offset.
- `OpcodeMap` with initialized-state validation, semantic-to-wire and wire-to-semantic mappings.
- Reader-driven collision-free shuffle over all 256 byte values.
- Identity map for the temporary fixed-runtime bridge.
- Stable SHA-256 digest over semantic order.
- Generated C opcode header.
- Map-aware disassembly and explicit unassigned-wire errors.
- Independent 115-entry literal historical golden test, not derived from the production table.

Independent gate after the golden repair: `PASS`.

Current evidence:

```text
go test -count=1 ./internal/vm
ok

go test -race -count=1 ./internal/vm
ok

go vet ./internal/vm
no output
```

The independent literal/header probe confirmed 115 unique semantics, wires, names, and C macros, and exact equality with `stub/linux/arm64/vm_opcodes.h`.

### 4.2 Completed ARM64 semantic opcode emission migration

Current files changed under `internal/arch/arm64/` include:

- `translator.go`
- `tr_alu.go`
- `tr_bitfield.go`
- `tr_branch.go`
- `tr_loadstore.go`
- `tr_special.go`
- `tr_stack.go`
- existing package tests updated for the constructor

Implemented:

- `Translator` stores a validated `vm.OpcodeMap`.
- Constructor is now:

```go
func NewTranslator(funcAddr uint64, funcSize int, opcodes vm.OpcodeMap) (*Translator, error)
```

- Raw bytes use `emit`.
- Semantic instructions use `emitOp`, which maps only the opcode and appends operands unchanged.
- The known raw trailer/fixup zero remains raw.
- Opcode variables and helper parameters use `vm.Opcode` rather than `byte`.
- Stack opcode converters return explicit success rather than using semantic zero as an unsupported sentinel.

Current evidence:

```text
go test -count=1 ./internal/arch/arm64
ok

go test -race -count=1 ./internal/arch/arm64
ok

go vet ./internal/arch/arm64
no output
```

Still required before closing the opcode migration:

- Add a dedicated two-map translator test proving:
  - semantic instruction sequence and operands/fixups are identical after map-aware decoding;
  - wire opcode positions differ;
  - HALT is not assumed to be wire zero;
  - some other semantic opcode may own wire zero;
  - raw placeholder/trailer bytes are not mapped.
- Run an independent review of the ARM64 diff after those tests.

### 4.3 Partially completed ELF opcode helper migration

Current production state in `internal/elf/packer.go`:

- `Packer` has `opcodes vm.OpcodeMap`.
- `processBytes` currently sets `p.opcodes = vm.IdentityOpcodeMap()` before translation. This is a temporary explicit bridge to the still-fixed C runtime.
- `arm64.NewTranslator` receives `p.opcodes` and constructor errors are handled.
- Debug disassembly calls map-aware `vm.DisasmRange` and handles errors.
- `reverseInstructions` now accepts `vm.OpcodeMap`, decodes wire to semantic opcode, uses semantic size, copies the original wire/operands, and appends raw size markers.
- `remapBranchTargets` decodes with `p.opcodes`, uses semantic branch metadata, and only rewrites the raw target operand.
- `encryptOpcodes` accepts `vm.OpcodeMap`, decodes before sizing, and XORs only the original wire byte.
- Production call sites pass `p.opcodes`.

The interrupted workflow already completed the production helper migration. Do not revert it.

### 4.4 Exact current compile break

`internal/vm` and `internal/arch/arm64` pass. `internal/elf` currently fails only because `internal/elf/analysis_test.go` still uses old helper signatures/raw semantic constants:

```text
internal/elf/analysis_test.go:1034:52: not enough arguments in call to reverseInstructions
internal/elf/analysis_test.go:1037:45: cannot use vm.OpMovImm as byte in slice literal
internal/elf/analysis_test.go:1037:59: not enough arguments in call to reverseInstructions
internal/elf/analysis_test.go:1040:47: not enough arguments in call to encryptOpcodes
internal/elf/analysis_test.go:1043:19: cannot use vm.OpJmp as byte in slice literal
```

The next change should be test-only and minimal:

```go
opcodes := vm.IdentityOpcodeMap()
movImmWire, err := opcodes.Wire(vm.OpMovImm)
jmpWire, err := opcodes.Wire(vm.OpJmp)
```

- Find an unassigned wire by iterating `0..255` until `opcodes.Decode(byte(i))` returns an error; do not assume `0xff` is unassigned.
- Pass `opcodes` to `reverseInstructions` and `encryptOpcodes`.
- Build branch test bytes from `jmpWire`.
- Use `&Packer{opcodes: opcodes}` for `remapBranchTargets`.
- Add randomized-map helper tests proving marker/operand preservation and opcode-only encryption.

Then run:

```sh
gofmt -w internal/elf/analysis_test.go
go test -count=1 ./internal/elf
go test -race -count=1 ./internal/elf
go vet ./internal/elf
go test -count=1 ./...
go test -race -count=1 ./...
go vet ./...
bash scripts/check-contract.sh
bash scripts/check-contract-test.sh
git diff --check
```

Do not mark the opcode migration complete until the full repository is green and an independent verifier passes.

## 5. Remaining Phase 4 work

### 5.1 Finish and verify opcode-map propagation

1. Fix the five current `analysis_test.go` compile points.
2. Add focused randomized-map tests for ARM64 and ELF helpers.
3. Verify no active Go bytecode walker interprets a wire byte as a semantic `Opcode` without `Decode`.
4. Verify no opcode position is serialized directly from a dense semantic ID.
5. Remove verbose logging of per-function opcode-encryption keys.
6. Later, replace the temporary `IdentityOpcodeMap()` boundary with the per-pack map created by the application.

### 5.2 Move and embed runtime templates

Move the active runtime source tree:

```text
stub/linux/arm64/**
  -> internal/runtime/templates/android/arm64/**
```

Known active closure:

- `vm_interp.c`
- `vm_decode.h`
- `vm_dispatch.h`
- generated replacement for `vm_opcodes.h`
- `vm_sections.h`
- `vm_token.h`
- `vm_types.h`
- eight `vm_handlers/h_*.h` files

`vm_crc.h` is currently unused; verify again and remove it rather than embedding dead code if no active include exists.

Do not reuse the old `vm_interp.lds` behavior. It merges text/rodata/data/bss into WAX and discards `.note*` and `.eh_frame*`.

### 5.3 Implement `internal/runtime`

Required API shape:

```go
type BuildConfig struct {
    NDKDir  string
    Opcodes vm.OpcodeMap
}

func Build(ctx context.Context, cfg BuildConfig) (*Image, error)
```

`Image` must retain validated:

- allocatable sections and flags;
- `SHT_NOBITS` information;
- symbols, including local relocation targets;
- relocations and addends;
- GNU property note;
- `.eh_frame` and related relocation information;
- opcode-map digest.

Build requirements:

- Exact NDK revision: `29.0.14206865`.
- Host tools under:

```text
<NDK>/toolchains/llvm/prebuilt/darwin-x86_64/bin/
```

- Use absolute tool paths; never PATH lookup or shell command composition.
- Use `exec.CommandContext`.
- Extract embedded templates into a private mode-`0700` temp directory; files mode `0600`; clean on success, failure, and cancellation.
- Generate `vm_opcodes.h` from the current `OpcodeMap`; delete the tracked fixed numeric authority once runtime migration is complete.
- Compile PIC C and explicit assembly bridge with API 23 target.
- Use explicit `-mbranch-protection=pac-ret+bti`; do not use `standard`, because the verified r29 toolchain also emitted GCS.
- Preserve unwind tables.
- Link with `ld.lld -r`, not the destructive legacy linker script.

### 5.4 Replace naked token entry

Current `vm_entry_token` is naked inline assembly without BTI, PAC, or explicit CFI.

Create an explicit `.S` entry that:

- starts with correct BTI;
- signs/authenticates LR using the selected PAC convention;
- describes the manual frame with `.cfi_*`;
- preserves the existing Phase 4 entry behavior;
- calls the C inner function;
- restores/authenticates on return.

Validate disassembly and unwind metadata; compiler flags alone are not evidence for naked/manual assembly.

### 5.5 Parse and validate the actual r29 object

Fail closed unless the result is:

- ELF64;
- little-endian;
- AArch64;
- `ET_REL`;
- contains required symbols:
  - `vm_entry`;
  - `vm_entry_token`;
  - `_token_table_va`;
- contains `.eh_frame`;
- contains the required AArch64 GNU branch-protection property;
- contains no unresolved/unsupported symbols;
- uses only explicitly supported relocation types.

Previously observed representative relocation evidence includes:

- `R_AARCH64_ADR_PREL_LO21`
- `R_AARCH64_CALL26`
- `R_AARCH64_GOT_LD_PREL19`
- `R_AARCH64_LD_PREL_LO19` in the existing object
- `R_AARCH64_JUMP26`
- `R_AARCH64_PREL32`

Do not treat that list as authoritative. Rebuild with the final templates and derive the whitelist from actual `llvm-readelf -S -s -r -n --unwind` output. Reject anything else.

### 5.6 Remove the fixed blob path

Remove/refactor all active references:

- `cmd/vmpacker/vm_interp.bin`
- `scripts/make_stub_blob.py`
- `//go:embed vm_interp.bin` from `cmd/vmpacker/main.go`
- `app.Config.InterpBlob`
- `elf.Request.InterpBlob`
- `Packer.interpBlob`
- `runtimeBlob`/`parseRuntimeBlob`
- Makefile `android-stub`/flat blob prerequisites
- CI transitional blob build
- fixed-stub documentation

Keep contract guards that assert those legacy paths remain absent.

### 5.7 Seed and entropy integration

- Stop rejecting a valid 64-hex `-seed`.
- No seed: `crypto/rand.Reader`.
- Seed: create a local deterministic reader from the decoded 32 bytes; do not use global random state.
- Consume entropy in stable order from the same per-run reader for:
  1. opcode map;
  2. runtime randomized inputs;
  3. per-function opcode-encryption keys;
  4. replacement garbage/random bytes.
- Never report/log the seed or encryption keys.
- Same seed in clean directories must reproduce the map/runtime and eventually the whole artifact.
- Default runs should produce different opcode-map digests.

### 5.8 Honest Phase 4 / Phase 8 boundary

The current injector accepts one pre-linked contiguous RX byte slice. It cannot consume an `ET_REL Image` while preserving:

- section permissions/alignment;
- symbols;
- relocations;
- GNU properties;
- unwind metadata;
- RW/NOBITS sections;
- PHDR/SHDR integrity.

Do not flatten the validated object back into a compatibility blob.

Smallest honest integration before Phase 8:

1. path preflight and bounded input read;
2. `elf.Analyze`;
3. create per-pack opcode map;
4. `runtime.Build` and validate `Image`;
5. pass `Image` + map to a post-analysis pack boundary;
6. fail before translation/mutation with a typed “Phase 8 rewrite planner required” error;
7. publish no artifact or debug map; at most publish a requested sanitized failure report.

This temporarily means the development CLI validates the new pipeline but cannot produce a packed artifact until Phase 8. That is preferable to reintroducing metadata loss.

### 5.9 Report and privacy changes

Add optional schema-v1 fields for:

- `opcode_map_digest`
- `runtime_strategy`
- later `segment_strategy`, `veneer_strategy`, `unwind_strategy`

Phase 4 should report the digest and truthful runtime-image validation strategy. Do not claim segment/veneer/unwind integration before Phase 8 applies it.

Sanitize public build errors so reports do not contain:

- NDK root;
- absolute tool paths;
- temp extraction path;
- compiler-generated temp paths.

Update `docs/report-schema-v1.md` so the opcode-map digest is the explicit allowed one-way derivative; seed/raw map remain forbidden.

### 5.10 Phase 4 tests and gate

Required tests:

- exact/wrong/missing NDK;
- missing/non-executable tools;
- no PATH lookup;
- extraction permissions and cleanup;
- cancellation;
- path-neutral errors;
- deterministic same-seed build across clean temp roots;
- differing default map digest;
- generated C header exactly matches map;
- `ET_REL` class/machine/endian/type;
- section/symbol/relocation bounds;
- required symbols;
- relocation whitelist and unknown relocation rejection;
- GNU BTI/PAC property;
- `.eh_frame` and correct entry FDE/CFI;
- no fixed blob/script/reference;
- Build failure before mutation/publication;
- map/Image digest mismatch fails closed.

Independent verification is mandatory before closing Phase 4.

## 6. Remaining Phase 5 — Core ARM64 semantics

Implement and verify:

- Table-driven instruction + operand/addressing whitelist.
- Correct binary `UMULH` semantics.
- Full N/Z/C/V state and width-aware flag operations.
- Correct ADDS/SUBS/ADC/SBC/CCMP/CCMN/ANDS and every condition code.
- ASLR-correct ADR/ADRP/literal LDR/direct BL using link-time addresses plus runtime load bias.
- ET_EXEC zero-bias behavior.
- BTI-preserving entries and wrappers.
- PAC/AUT/XPAC via correct native helpers; no NOP approximation.
- One native SVC thunk per observed immediate, preserving syscall ABI register state.
- Fail closed for unsupported system registers, exceptions, hints, or semantics.

Gate: host differential vectors plus repeated physical-device ASLR, BTI/PAC, and SVC proof.

## 7. Remaining Phase 6 — AAPCS64, FP/SIMD, atomics

Implement and verify:

- VM V0–V31, FPCR, FPSR, full NZCV, architectural SP, native-call metadata.
- Entry ABI remains intentionally limited; do not pretend to support entry FP/HFA/sret.
- Native call assembly bridge for:
  - X0–X7;
  - X8/sret;
  - stack args;
  - V0–V7;
  - integer/FP/SIMD/complex/indirect returns.
- Remove C `u64` function-pointer casts.
- Build r29 instruction corpus at `-O0`, `-O2`, and `-Oz` and implement only observed common FP/SIMD forms before whitelisting.
- Native LSE atomic/RMW/barrier helpers.
- Recognize closed LDXR/LDAXR → STXR/STLXR CFG regions and relocate each whole reservation region as one continuous native thunk.
- Reject unclosed reservation regions; do not emulate with locks or CAS.
- Emit and validate `.cfi_*` for all bridges/thunks.

Gate: physical-device AAPCS64 differential and multithreaded atomic/exclusive contention proof.

## 8. Remaining Phase 7 — C++ exception/unwind bridge

Implement and prove before final writer integration:

- Parse `.eh_frame`, `.eh_frame_hdr`, `.gcc_except_table`, DW_EH_PE encodings, CIE/FDE, and LSDA tables.
- Map original call-site PC → VM offset → landing pad.
- Generate unique native call-thunk ranges with FDE/LSDA.
- Reuse Android C++ personality from input metadata.
- Landing-pad bridge restores VM state and resumes translated landing pad.
- Wrapper CFI allows exceptions to escape a protected function to outer native catch.
- Preserve runtime `.eh_frame`/LSDA.
- Do not use Apple-only `__register_frame`.
- Prove the architecture first in a standalone Android shared-library test on physical devices.

Hard gate: if Android loader/unwinder cannot discover the generated metadata, stop and report evidence. Do not document-away or bypass this requirement.

## 9. Remaining Phase 8 — Plan-first ELF writer

Replace legacy direct mutation/note hijacking with:

```go
type RewritePlan struct { /* validated complete layout */ }
func PlanRewrite(...) (*RewritePlan, error)
func ApplyPlan(original []byte, plan *RewritePlan) ([]byte, error)
```

Required behavior:

- Complete layout/relocation/unwind/trampoline/PHDR plan before any byte mutation.
- Preserve notes, GNU properties, build ID, dynsym/dynstr, dynamic relocations, and original unwind metadata.
- New LOAD congruence/alignment at `0x4000` for 16 KiB support, while loading on 4 KiB devices.
- Reuse only a genuinely safe PT_NULL or relocate the entire PHDR table and update all affected metadata.
- Apply only validated runtime relocation types.
- Cluster functions by B-imm26 reach and add 16 KiB veneer islands where needed; never truncate branch immediates.
- Preserve BTI/PAC entry wrapper; reject functions shorter than the real prefix/trampoline.
- Merge generated FDE/LSDA and update `.eh_frame_hdr`/`PT_GNU_EH_FRAME`.
- Strip `.symtab`/private `.strtab` by default while preserving `.dynsym/.dynstr`.
- Apply the plan to a clone and publish only after every invariant passes.
- Remove all PT_NOTE reuse and blind PHDR append behavior.

Gate: `llvm-readelf -l -S -s -r -n --unwind`, malformed/failure injection, far-veneer fixtures, and physical 4 KiB/16 KiB load/run proof for SO and PIE/ET_EXEC.

## 10. Remaining Phase 9 — Demo corpus and physical-device differential gate

Required work:

- Create a machine-readable manifest covering exactly 85 legacy demos:
  - 83 C;
  - 1 Go;
  - 1 Rust.
- Record build mode/optimization/ABI/selector/inputs/expected output/exit/side effects/device capability.
- Repair unreliable oracles such as programs that print failure but exit zero.
- Cross-compile with r29 and actually pack every target.
- Run baseline vs packed comparisons on physical devices only.
- Detect and reject emulators.
- Record API, ABI, PAGE_SIZE, PAC/BTI/CPU features at runner startup.
- Cover API 23, mainstream API, 4 KiB/16 KiB, PAC/BTI, atomics, and exceptions.
- Store machine-readable CI evidence without host absolute paths.

Known non-blocking diagnostic to revisit during corpus cleanup:

- `demo/demo_insn_ubfm.c` has format warnings around lines 95, 101, and 107 because the format expects `long` while `int64_t` is `long long` on the current host compiler.

No expected-failure waivers are allowed for the release gate.

## 11. Remaining Phase 10 — Documentation and macOS ARM64 release

Required work:

- Final English README as source of truth; section-synchronized Chinese translation.
- Remove outdated fixed-runtime/Linux/APK/GUI/profile/security claims.
- Update canonical repository/module/import/release URLs after the user supplies the final public repository.
- Build only `GOOS=darwin GOARCH=arm64 CGO_ENABLED=0`.
- Inject exact tag and commit.
- Pin CI actions to reviewed commit SHAs.
- Minimal default workflow permissions; write only in release job.
- Require exact tag → HEAD.
- Run all unit/corpus/physical-device gates before release.
- Developer ID sign with hardened runtime and timestamp.
- Verify with `codesign` and `spctl`.
- Submit ZIP through `notarytool`; do not use stapler on a bare CLI/ZIP and do not use `codesign --deep`.
- Generate exact-commit source archive and `SHA256SUMS`.
- Release only:
  - notarized macOS ARM64 CLI ZIP;
  - exact source archive;
  - checksums.

No release may occur while canonical repo, Apple credentials/runner, or device matrix is missing.

## 12. Remaining Phase 11 — Independent adversarial release rehearsal

From a clean checkout/tag:

1. Run contract guards, all tests, race/vet/lint, and forbidden-pattern scans.
2. Rebuild with same seed in two clean roots and compare.
3. Confirm default runs have different opcode-map digests.
4. Re-run malformed/boundary/stripped/dynsym/unwind/far-veneer/PHDR/16 KiB fixtures.
5. Confirm every failure path leaves input unchanged and artifact absent.
6. Check final ELFs with both Go validators and r29 `llvm-readelf`.
7. Run full physical-device differential matrix.
8. Exercise the downloaded notarized ZIP, not the workspace binary.
9. Rebuild from source archive and confirm version/commit/checksum.
10. Obtain final independent verification-agent `PASS` with command/output evidence.

Only after this gate may the product be described as complete or a formal release be created.

## 13. External blockers requiring user-provided resources

These do not block local implementation but block release:

1. Final public repository URL and canonical Go module path. Current `github.com/vmpacker` is a placeholder.
2. Apple Developer ID, notary API credentials, and an approved macOS ARM64 release runner. Never store credentials in the repository or logs.
3. Self-hosted physical Android device inventory and runner labels proving:
   - API 23;
   - mainstream API;
   - 4 KiB and 16 KiB pages;
   - PAC/BTI;
   - atomics/exclusive contention;
   - C++ exceptions/unwind.

## 14. Immediate resume checklist

Resume in this exact order:

1. Read this file and the approved master plan.
2. Run `git status --short`; preserve mixed staged/unstaged work.
3. Confirm no background workflow/agent is still modifying the tree.
4. Fix only the five current `internal/elf/analysis_test.go` compile points using `vm.IdentityOpcodeMap()` and identity wire lookups.
5. Add map-aware ELF helper tests for randomized maps, unknown wires, opcode-only encryption, and raw marker/operand preservation.
6. Run full Go tests/race/vet/contract/format gates.
7. Obtain independent PASS for the complete VM + ARM64 + ELF opcode-map slice.
8. Implement `internal/runtime` templates/build/Image and explicit PAC/BTI/CFI assembly entry.
9. Remove fixed blob pipeline.
10. Integrate analysis → opcode map → runtime build → explicit Phase-8-required fail-closed boundary, including seed/report/privacy behavior.
11. Independently verify Phase 4.
12. Continue Phases 5 through 11 in dependency order.

## 15. Commands for a cold handoff

```sh
cd /Volumes/work/android/VMPacker

git status --short
git rev-parse --abbrev-ref HEAD
git rev-parse HEAD

# Currently expected green
go test -count=1 ./internal/vm
go test -count=1 ./internal/arch/arm64

# Currently expected to fail at analysis_test.go until immediate resume step 4
go test -count=1 ./internal/elf

# Run after the test migration is repaired
go test -count=1 ./...
go test -race -count=1 ./...
go vet ./...
bash scripts/check-contract.sh
bash scripts/check-contract-test.sh
git diff --check
```

## 16. Completion truth statement

At this snapshot:

- Product contract and archive boundary: complete and verified.
- Safe CLI/ABI/report/publication boundary: complete and verified.
- Bounded ELF analysis/CFG: complete and independently verified.
- Semantic opcode core: complete and independently verified.
- ARM64 map-aware emission: compiles and passes package checks; dedicated randomized-map tests and independent review remain.
- ELF map-aware helper production code: implemented; tests are in an interrupted signature-migration state.
- Dynamic r29 runtime build/Image: not implemented.
- Core ISA correctness, full AAPCS64/FP/SIMD/atomics, C++ unwind, final ELF writer, complete demo/device gates, signing/notarization, and final rehearsal: not implemented.
- No release has been made and the product must not be represented as finished.
