#!/usr/bin/env python3
from pathlib import Path


def main():
    path = Path("internal/elf/rewrite_plan.go")
    text = path.read_text()
    start = text.index("func buildPlannedTokenTrampoline(")
    end = text.index("\nfunc planProgramHeaders(", start)
    replacement = '''func buildPlannedTokenTrampoline(input []byte, selection Selection, translation *arm64.TranslateResult, targetVA uint64, token uint32) ([]byte, error) {
\tif translation == nil {
\t\treturn nil, fmt.Errorf("translation is missing")
\t}
\tcode, err := selectedCode(input, selection)
\tif err != nil {
\t\treturn nil, err
\t}
\tpatchOffset := 0
\tif translation.HasEntryBTI {
\t\tpatchOffset = 4
\t\tif selection.Size() < 4 {
\t\t\treturn nil, fmt.Errorf("BTI entry is truncated")
\t\t}
\t\tdecoded := arm64.NewDecoder().Decode(binary.LittleEndian.Uint32(code[:4]), 0)
\t\tif arm64.Op(decoded.Op) != translation.EntryBTI {
\t\t\treturn nil, fmt.Errorf("BTI entry metadata does not match input encoding")
\t\t}
\t}

\tbranchVA, ok := checkedAdd(selection.Address, uint64(patchOffset+8))
\tif !ok {
\t\treturn nil, fmt.Errorf("entry branch address overflows")
\t}
\ttransfer, err := buildEntryTransfer(branchVA, targetVA)
\tif err != nil {
\t\treturn nil, err
\t}
\tpatchSize := patchOffset + 8 + 4*len(transfer)
\tif selection.Size() < uint64(patchSize) {
\t\tkind := "entry"
\t\tif translation.HasEntryBTI {
\t\t\tkind = "BTI entry"
\t\t}
\t\tif len(transfer) > 1 {
\t\t\tkind += " long transfer"
\t\t}
\t\treturn nil, fmt.Errorf("%s requires at least %d bytes, got %d", kind, patchSize, selection.Size())
\t}

\tpatch := make([]byte, patchSize)
\tif patchOffset != 0 {
\t\tcopy(patch[:patchOffset], code[:patchOffset])
\t}
\tlo16 := token & 0xffff
\thi16 := token >> 16
\tbinary.LittleEndian.PutUint32(patch[patchOffset:patchOffset+4], 0x52800010|lo16<<5)
\tbinary.LittleEndian.PutUint32(patch[patchOffset+4:patchOffset+8], 0x72A00010|hi16<<5)
\tcursor := patchOffset + 8
\tfor _, word := range transfer {
\t\tbinary.LittleEndian.PutUint32(patch[cursor:cursor+4], word)
\t\tcursor += 4
\t}
\treturn patch, nil
}
'''
    path.write_text(text[:start] + replacement + text[end:])

    path = Path("internal/runtime/templates/android/arm64/vm_entry.S")
    text = path.read_text()
    if text.count("\tbti c\n") != 1:
        raise SystemExit("vm_entry.S BTI landing marker changed")
    path.write_text(text.replace("\tbti c\n", "\tbti jc\n", 1))

    path = Path("internal/elf/selection.go")
    text = path.read_text()
    old = '\t"the entry patch requires at least 12 contiguous bytes, or 16 bytes when preserving an entry BTI",\n'
    new = '\t"near entry patches require 12 contiguous bytes, or 16 bytes with entry BTI; far inline veneers require 20 bytes, or 24 bytes with entry BTI, and reject beyond ADRP range",\n'
    if text.count(old) != 1:
        raise SystemExit("entry patch limitation text changed")
    path.write_text(text.replace(old, new, 1))


if __name__ == "__main__":
    main()
