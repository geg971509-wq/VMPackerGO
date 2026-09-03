# VMPacker Productization Handoff

**Snapshot date:** 2026-08-28

**Repository:** `s54mj968zc-eng/VMPacker`

**Branch:** use the branch containing this document

**HEAD:** use the commit containing this document

**Product status:** development only; no release has been made

## 1. Truthful status

Phases 0-3 remain implemented and verified. The interrupted Phase 4 work has been repaired and advanced through the host-side Phase 8 writer boundary:

- semantic opcode maps propagate through VM, ARM64 translation, and ELF bytecode helpers;
- runtime source templates are embedded under `internal/runtime/templates/android/arm64`;
- every default pack attempt creates a fresh opcode map with `crypto/rand`;
- a valid 64-hex `-seed` creates a local deterministic AES-CTR entropy reader for tests/debugging;
- the runtime is rebuilt with exact Android NDK revision `29.0.14206865` and absolute tools under `darwin-x86_64/bin`;
- the output is validated as little-endian AArch64 ELF64 `ET_REL`, retaining sections, `SHT_NOBITS`, symbols, relocations, GNU BTI/PAC properties, and `.eh_frame`;
- `vm_entry_token` is explicit assembly with BTI, PAC/AUT, and CFI;
- the fixed interpreter blob and legacy direct-mutation/note-hijack writer are removed;
- after analysis, translation, runtime validation, and immutable rewrite planning, the CLI applies the validated plan to a fresh in-memory ELF image and reparses it before publication;
- successful host transformations publish an artifact through the existing artifact-last transaction and report `rewrite-artifact-ready`; writer/reparse failures retain `rewrite-plan-ready` and publish no artifact.

This is a host-side structurally rewritten-artifact implementation, but it is not release-ready. Physical-device, final-unwind, full-matrix, signing, and independent-verification gates remain open.

### Continuation status after the original snapshot

The product now closes the host-side Phase 8 plan-first rewrite boundary, and the following bounded slices have been implemented and verified after the original handoff snapshot:

- Phase 5 host semantics: table-driven tri-state ARM64 policy, binary `UMULH`, architectural width-aware NZCV and all conditions, typed ASLR image references, entry BTI metadata, native PAC/AUT/XPAC helpers, and one exact-immediate BTI/CFI SVC thunk per observed immediate. Real-r29 verification passed in workflow runs `33175725869`, `33176634999`, and `33177595969`.
- Phase 6 host implementation: V0-V31, FPCR, FPSR, native-call metadata, an explicit PAC/BTI/CFI AAPCS64 bridge for X0-X8, shadow-stack arguments, V0-V7, and integer/vector returns; native domain-preserving DMB/DSB/ISB; native 1/2/4/8-byte LDAR/STLR/LSE LDADD/CAS helpers; an exact-r29 `-O0/-O2/-Oz` FP/SIMD corpus gate with a mask-based whitelist and per-observed-encoding state-preserving native thunks; and content-addressed continuous closed `LDAXR...STLXR` thunks. Unknown FP/SIMD encodings and unclosed, nested, branching, mismatched, or reserved-register exclusive regions fail closed. Every generated thunk carries BTI/CFI and is required to have an FDE. Real-r29 verification passed most recently in workflow run `33182811799`. The physical-device ABI/contention gate remains open.
- Phase 7 parsing/modeling slice: bounded DW_EH_PE, CIE/FDE, `.eh_frame_hdr`, LSDA call-site/action/type/filter tables, original-PC-to-VM-landing-pad mapping, throw-capable native-call locations, content-addressed invoke-thunk plans, and single-call LSDA rebuilding with explicit final-layout type-info relocations. The complete hosted workflow passed in run `33184075553`. The runtime personality/invoke/landing assembly bridge, final FDE merge, and device hard gate remain open.
- Phase 9 manifest slice: `demo/manifest.json` is the exact machine-readable inventory of 83 C, one Go, and one Rust source, with schema, path, uniqueness, count, and existence validation.
- Host build entry point: root `build.sh` builds the current Git checkout through the canonical `make mac-cli` target, verifies an executable macOS ARM64 Mach-O, and confirms `dist/vmpacker-darwin-arm64` and `dist/vmpacker` are identical.

Still open locally: the Android C++ personality/landing-pad bridge, actual packing and execution of all 85 demos, and adversarial release rehearsal. The Phase 5/6 physical-device semantic, ABI, and multithreaded-contention gates, plus the canonical-module, Apple signing/notary, and independent-verifier gates in section 11, also remain open. No release-ready claim is permitted.

## 2. Non-negotiable product contract

- Publish only a signed and notarized macOS ARM64 CLI.
- Accept compiled Android AArch64 ELF64 shared objects, PIE executables, and `ET_EXEC` native executables.
- Minimum Android API is 23; generated layouts must load with both 4 KiB and 16 KiB pages.
- Preserve correct ASLR, BTI, PAC, common FP/SIMD, atomics/exclusives/barriers, raw SVC, complete internal AAPCS64 calls, and C++ exception/unwind behavior.
- Entry ABI remains explicit and limited to at most eight integer/pointer parameters and `void` or one integer/pointer result.
- Fail closed for unresolved CFG, provable recovery paths, unsupported semantics, and unclosed exclusive regions.
- Limits: 1 GiB input, 4096 functions, 64 KiB final bytecode per function.
- Use exact NDK `29.0.14206865`; do not search `PATH` for runtime tools.
- Never report or log seeds, raw opcode maps, encryption keys, NDK/temp absolute paths, signing credentials, or secrets.
- Default to no-clobber; `-force` uses same-directory atomic replacement.
- Debug maps are opt-in and mode `0600`.
- Report schema remains version 1; the one-way opcode-map digest is explicitly allowed.
- Do not claim absolute protection.
- License is `AGPL-3.0-only`; `Copyright (C) 2026 LeoChen`.

## 3. Phase 4 changes completed in this snapshot

### Opcode-map closure

- Repaired all old `internal/elf/analysis_test.go` helper calls.
- Unknown-wire tests discover an unassigned wire instead of assuming `0xff`.
- Added randomized-map tests for instruction reversal, branch remapping, raw marker/operand preservation, and opcode-only encryption.
- Added a two-map ARM64 translator test proving semantic bytecode and fixups remain equal while wire bytes differ.
- The test map proves HALT is not wire zero and another semantic may own wire zero.
- Trailer emission now sorts ARM64 offsets; previous Go map iteration made seeded output nondeterministic.
- Verbose logging no longer exposes per-function opcode-encryption keys.

### Runtime build and validation

New package: `internal/runtime`.

Build behavior:

1. validates exact NDK revision;
2. resolves only absolute `aarch64-linux-android23-clang` and `ld.lld` paths;
3. extracts embedded templates into a mode-`0700` temporary directory with mode-`0600` files;
4. generates `vm_opcodes.h` from the current map;
5. compiles PIC C and explicit assembly with `-mbranch-protection=pac-ret+bti` and unwind tables;
6. links with `ld.lld -r`;
7. validates and retains the relocatable image;
8. removes the temporary directory on success, failure, or cancellation.

Validation rejects wrong class/data/machine/type, missing or duplicate required symbols, unresolved symbols, invalid symbol storage, missing `.eh_frame`, missing BTI+PAC GNU property, malformed relocation tables, unknown relocation types, out-of-range relocations, and lack of an `.eh_frame` relocation for `vm_entry_token`.

The current explicit relocation whitelist is intentionally narrow and must be confirmed against the real final r29 object. Never expand it speculatively.

### Removed legacy pipeline

Removed active use of:

- `cmd/vmpacker/vm_interp.bin` and `go:embed` for that binary;
- `app.Config.InterpBlob`, `elf.Request.InterpBlob`, and runtime-blob parsing;
- flat blob generation and `scripts/make_stub_blob.py`;
- `stub/linux/arm64` as an active template tree;
- PT_NOTE reuse, blind PHDR append, fixed-runtime Makefile targets, and transitional CI blob builds;
- obsolete host/device transformation smoke scripts that assumed the removed writer.

Contract checks now reject reintroduction of those paths. `--release` deliberately fails while Phases 5-11 and physical-device evidence are incomplete.

### Report and privacy

Schema v1 now permits optional:

- `opcode_map_digest`;
- `runtime_strategy`;
- future `segment_strategy`, `veneer_strategy`, and `unwind_strategy`.

Current validated runtime strategy is `ndk-r29-et-rel-validated`; successful host transformations use `rewrite-artifact-ready`, while final application/reparse failures after planning retain `rewrite-plan-ready`. Build errors omit NDK roots, tool paths, extraction paths, compiler stderr, and compiler temp paths.

## 4. Verification evidence

Executed successfully in the handoff environment with an official, SHA-256-verified Go 1.26.0 toolchain:

```text
go test -count=1 ./...
PASS

go test -race -count=1 ./...
PASS

go vet ./...
PASS

bash -n scripts/*.sh
PASS

bash scripts/check-contract.sh
[contract] active product contract passed

bash scripts/check-contract-test.sh
check-contract self-test passed
```

Focused runtime tests cover exact/wrong/missing NDK metadata, missing tools, no `PATH` lookup, private extraction permissions, generated-header equality, cleanup, cancellation, path-neutral compiler failures, ELF identity, section/symbol/relocation retention, unknown relocation rejection, BTI+PAC property rejection, `.eh_frame`, entry unwind relocation, and map/Image digest mismatch.

### Local limitation and hosted real-toolchain evidence

The handoff environment did not contain Android NDK r29, so it could not execute the real-toolchain integration locally. GitHub Actions run `33174003003` on commit `cbcdb9a8bb413226352a1df734a56f5af3a8b390` resolved exact NDK `29.0.14206865` and passed the complete workflow, including `TestBuildInstalledExactR29Object`, ordinary tests, race tests, vet, contract checks, and CLI build. The integration test compiles the embedded C/assembly templates and passes the resulting real r29 `ET_REL` object through the production parser and validator.

This closes the automated real-r29 build/validation sub-gate. It does not replace manual `llvm-readelf`/`llvm-objdump` inspection, independent review of the final relocation set and `vm_entry_token` FDE/CFI, or release rehearsal on an approved macOS ARM64 runner. Do not close Phase 4 until those reviews pass.

## 5. Immediate resume checklist

1. Confirm the final GitHub branch check remains green after documentation-only updates.
2. For the independent Phase 4 review, on macOS with exact r29 run:

   ```sh
   make runtime-integration ANDROID_NDK=/absolute/path/to/android-ndk-r29
   go test -count=1 ./...
   go test -race -count=1 ./...
   go vet ./...
   bash scripts/check-contract.sh
   bash scripts/check-contract-test.sh
   ```

3. Rebuild the final templates and inspect with the same NDK's tools:

   ```text
   llvm-readelf -h -S -s -r -n --unwind runtime.o
   llvm-objdump -d --no-show-raw-insn runtime.o
   ```

4. Confirm `vm_entry_token` starts with BTI, signs before the manual frame, restores the frame, authenticates before return, and has a discoverable FDE with correct CFA/register rules.
5. Compare every actual relocation against `supportedRelocations`; reject unknown types and add a type only with an application rule and fixture.
6. Obtain an independent PASS for the complete Phase 4 slice.
7. Begin Phase 5 only after Phase 4 is green.

## 6. Remaining Phase 5 - core ARM64 semantics

- Replace ad-hoc acceptance with a table-driven instruction and addressing whitelist.
- Correct binary `UMULH` behavior.
- Implement width-aware N/Z/C/V and correct ADDS/SUBS/ADC/SBC/CCMP/CCMN/ANDS plus every condition.
- Make ADR/ADRP/literal LDR/direct BL correct under ASLR and ET_EXEC zero bias.
- Preserve BTI entries and wrappers; implement PAC/AUT/XPAC with native helpers rather than NOP approximation.
- Emit one native SVC thunk per observed immediate while preserving the syscall ABI.
- Reject unsupported system registers, exceptions, hints, and semantics.

Gate: host differential vectors plus repeated physical-device ASLR, BTI/PAC, and SVC evidence.

## 7. Phase 6 - host implementation complete; device gate open

- Add V0-V31, FPCR, FPSR, full NZCV, architectural SP, and native-call metadata.
- Implement the assembly native-call bridge for X0-X7, X8/sret, stack args, V0-V7, and all required return classes.
- Remove C `u64` function-pointer casts.
- Derive the common FP/SIMD whitelist from an exact-r29 `-O0/-O2/-Oz` corpus.
- Add native LSE atomic/RMW/barrier helpers.
- Relocate complete closed exclusive regions as continuous native thunks; reject unclosed regions.
- Emit and validate CFI for every bridge and thunk.

Host status: all items above are implemented and exact-r29 green in workflow run `33182811799`. Closed exclusive regions are intentionally restricted to a proven branch-free X0-X15 body; other shapes fail closed rather than being approximated. The current runtime is integrated into structurally rewritten host artifacts through the Phase 8 plan-first writer; physical-device ABI and contention evidence remains open.

Remaining gate: physical-device ABI differential and multithreaded contention evidence.

## 8. Remaining Phase 7 - C++ exception/unwind bridge

- Parse `.eh_frame`, `.eh_frame_hdr`, `.gcc_except_table`, CIE/FDE, LSDA, and DW_EH_PE encodings.
- Map original call-site PCs through VM offsets to landing pads.
- Generate unique call-thunk FDE/LSDA ranges, reuse the Android C++ personality, and build landing-pad/wrapper bridges.
- Preserve runtime unwind metadata and do not use Apple-only registration APIs.

Hard gate: prove Android loader/unwinder discovery in a standalone physical-device shared-library test before final unwind integration is considered complete.

## 9. Phase 8 - host writer implemented; completion gates open

Implemented host slice:

- `RewritePlan` is the sole authority for the current runtime segments, runtime relocations, encrypted bytecode/token data, function-entry patches, and final program-header table.
- The planner emits `0x4000`-aligned W^X RX/RW/R loads, reuses only safe trailing `PT_NULL` slots, grows the table in place only when proven safe, and otherwise relocates the full table while updating a valid `PT_PHDR` entry.
- `applyRewritePlan` validates the original header and immutable plan, materializes into a fresh buffer, preserves the caller input and existing section-header table, and returns no partial artifact on failure.
- `ProcessAnalyzed` reparses the transformed ELF and exposes `rewrite-artifact-ready` only after target-kind validation; filesystem publication remains owned by `internal/publish` and keeps the artifact last.

Host verification covers focused and full Go tests, race detection, vet, build/contract checks, PHDR reuse/growth/relocation including `PT_PHDR`, failure atomicity, and one real Android AArch64 shared-object CLI transformation with matching report hash and unchanged input.

Remaining completion work: cluster functions by B-imm26 reach and add 16 KiB veneer islands without truncation; merge final FDE/LSDA data and update `.eh_frame_hdr`/`PT_GNU_EH_FRAME`; then run readelf/objdump, malformed/failure injection, far-veneer, unwind, and physical 4 KiB/16 KiB load/run gates for SO, PIE, and ET_EXEC.

## 10. Remaining Phases 9-11

### Phase 9 - corpus and device differential

- Machine-readable manifest for exactly 85 legacy demos: 83 C, one Go, one Rust.
- Exact-r29 cross-build and actual packing for every target.
- Physical-device baseline-versus-packed comparison; reject emulators.
- Record API, ABI, page size, PAC/BTI/CPU features, exit/output, and side effects.
- No expected-failure release waivers.

### Phase 10 - documentation and macOS ARM64 release

- Final English source-of-truth README and synchronized Chinese sections.
- Canonical repository/module URLs after the owner provides them.
- Only `GOOS=darwin GOARCH=arm64 CGO_ENABLED=0` release binary.
- Exact tag-to-HEAD, pinned actions, minimal permissions, complete gates.
- Developer ID hardened-runtime signing with timestamp, `codesign`/`spctl`, ZIP notarization with `notarytool`, source archive, and `SHA256SUMS`.

### Phase 11 - adversarial rehearsal

- Clean tag checkout; all guards/tests/race/vet/lint/forbidden scans.
- Same-seed builds in two clean roots; differing default map digests.
- Full malformed/boundary/unwind/veneer/PHDR/16 KiB fixtures.
- Every failure leaves input unchanged and artifact absent.
- Full physical-device matrix.
- Exercise the downloaded notarized ZIP and rebuild from the source archive.
- Final independent verifier PASS with command/output evidence.

## 11. External blockers

These do not block local implementation but do block completion/release:

1. Final public repository URL and canonical Go module/import path; `github.com/vmpacker` is still a placeholder.
2. Apple Developer ID, notary API credentials, and an approved macOS ARM64 release runner.
3. Physical Android runner/device inventory proving API 23, mainstream API, 4 KiB and 16 KiB pages, PAC/BTI, atomics/exclusives, and C++ unwind.

Never store those credentials in the repository or logs.

## 12. Completion statement

- Product boundary, ABI/manifest, bounded analysis, reports, and safe publication: implemented.
- Semantic opcode map and ARM64/ELF propagation: implemented and locally green.
- Dynamic exact-r29 runtime build/Image and explicit assembly entry: implemented with simulated-tool tests; real-r29 and independent PASS still required.
- Fixed blob and legacy writer: removed.
- Phase 8 immutable planning, in-memory application, structural reparse, and artifact-last publication: implemented for the current runtime-segment and entry-patch scope.
- Core ISA/AAPCS64/FP-SIMD/atomic/exclusive host semantics: implemented and real-r29 green; physical-device semantic, ABI, and multithreaded-contention gates remain open.
- C++ unwind metadata and relocation-safe invoke/LSDA planning: implemented and host-tested; the Android personality/invoke/landing assembly bridge, final FDE merge, and physical-device unwinder proof remain open.
- Exact 85-demo manifest: implemented and validated; cross-build, packing, and physical-device differential execution remain open.
- Final veneer/unwind integration, signing/notarization, physical-device validation, and release rehearsal: not implemented.
- Release readiness: false.
