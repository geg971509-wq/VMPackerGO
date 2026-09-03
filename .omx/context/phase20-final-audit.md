# Phase 20 pre-PR audit

- Base `main`: `e81bea29d957a17c9d34c6b65364a703bf446306`.
- Final diff contains only Phase 20 plan/product/tests plus the real signed-offset pair regression test.
- Temporary Phase 20 workflows and patch scripts are absent.
- CASP remains in the CAS semantic family; raw encoding selects pair transport kind 12.
- `OpAtomic` remains 7 bytes and scalar kinds 0-11 are unchanged.
- `casp128` exact-r29 exemption and expectation are removed; `machine-outliner` remains the only compiler-derived intentional boundary.
- CASP pair lows are conservatively limited to even X0-X28; register-30 pair base remains fail-closed pending independent evidence.
- Real STP/LDP `WB=2` signed-offset encodings from production diagnostics are locked by regression tests.
- Merge is forbidden until current-head PR Verification and post-merge `main` Verification both succeed.
