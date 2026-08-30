# Archived packing-era Android ARM64 runtime

This directory is an unsupported historical snapshot of the former fixed-blob packer:

- `pkg/` ELF injector, APK workflow, and ARM64 translator
- `stub/linux/arm64` interpreter compiled into `vm_interp.bin`
- `scripts/make_stub_blob.py`

It is outside the active VMPacker product and isolated from root Go module traversal. The sources are preserved for history and are not maintained.
