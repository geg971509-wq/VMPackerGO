#!/usr/bin/env python3
from pathlib import Path


def replace_once(path: str, old: str, new: str) -> None:
    p = Path(path)
    text = p.read_text()
    count = text.count(old)
    if count != 1:
        raise SystemExit(f"{path}: expected exactly one match, found {count}\n--- expected ---\n{old}")
    p.write_text(text.replace(old, new, 1))


# CIE personality references model the encoded reference target. For indirect
# encodings this is the indirection slot, not a pack-time dereference.
replace_once(
    "internal/unwind/frame.go",
    '''\tPersonalityEncoding byte
\tPersonality         *uint64
\tInstructions        []byte
''',
    '''\tPersonalityEncoding byte
\t// Personality is the resolved encoded-reference target. When
\t// PersonalityEncoding contains PEIndirect, this is the address of the
\t// indirection slot; the external personality function is intentionally not
\t// dereferenced during packing.
\tPersonality  *uint64
\tInstructions []byte
''',
)
replace_once(
    "internal/unwind/frame.go",
    '''\t\t\tcase 'P':
\t\t\t\tif offset >= augmentationEnd {
\t\t\t\t\treturn nil, fmt.Errorf("missing personality encoding")
\t\t\t\t}
\t\t\t\tcie.PersonalityEncoding = data[offset]
\t\t\t\toffset++
\t\t\t\tvalue, err := DecodePointer(data[:augmentationEnd], &offset, cie.PersonalityEncoding, order, pointerSize, Bases{Field: fieldVA})
\t\t\t\tif err != nil {
\t\t\t\t\treturn nil, err
\t\t\t\t}
\t\t\t\tcie.Personality = &value
''',
    '''\t\t\tcase 'P':
\t\t\t\tif offset >= augmentationEnd {
\t\t\t\t\treturn nil, fmt.Errorf("missing personality encoding")
\t\t\t\t}
\t\t\t\tcie.PersonalityEncoding = data[offset]
\t\t\t\toffset++
\t\t\t\t// Preserve an indirect reference as its slot address. This mirrors
\t\t\t\t// LSDA type-info handling and lets the final writer rebuild the same
\t\t\t\t// relocatable reference without target-memory access.
\t\t\t\tvalue, err := DecodePointer(data[:augmentationEnd], &offset, cie.PersonalityEncoding&^PEIndirect, order, pointerSize, Bases{Field: fieldVA})
\t\t\t\tif err != nil {
\t\t\t\t\treturn nil, err
\t\t\t\t}
\t\t\t\tcie.Personality = &value
''',
)

# Persist the original landing PC as the stable runtime routing identity.
replace_once(
    "internal/unwind/bridge.go",
    '''type InvokeThunk struct {
\tID           uint32
\tOriginalPC   uint64
\tVMCallOffset uint32
\tVMLandingPad uint32
''',
    '''type InvokeThunk struct {
\tID                 uint32
\tOriginalPC         uint64
\tOriginalLandingPad uint64
\tVMCallOffset       uint32
\tVMLandingPad       uint32
''',
)
replace_once(
    "internal/unwind/bridge.go",
    '''\t\tthunk := InvokeThunk{
\t\t\tOriginalPC: call.OriginalPC, VMCallOffset: call.VMOffset,
\t\t\tVMLandingPad: mapped[siteIndex].VMLandingPad, Action: site.Action,
''',
    '''\t\tthunk := InvokeThunk{
\t\t\tOriginalPC: call.OriginalPC, OriginalLandingPad: site.LandingPad,
\t\t\tVMCallOffset: call.VMOffset, VMLandingPad: mapped[siteIndex].VMLandingPad, Action: site.Action,
''',
)
replace_once(
    "internal/unwind/bridge.go",
    '''\tfor _, value := range []uint64{personality, fdeOffset, thunk.OriginalPC, uint64(thunk.VMCallOffset), uint64(thunk.VMLandingPad), thunk.Action} {
''',
    '''\tfor _, value := range []uint64{personality, fdeOffset, thunk.OriginalPC, thunk.OriginalLandingPad, uint64(thunk.VMCallOffset), uint64(thunk.VMLandingPad), thunk.Action} {
''',
)

# Focused regression coverage.
replace_once(
    "internal/unwind/unwind_test.go",
    '''func TestParseEHFrameCIEAndFDE(t *testing.T) {
''',
    r'''func TestParseEHFramePreservesIndirectPersonalityReferenceSlot(t *testing.T) {
	const sectionVA = uint64(0x1000)
	const slotVA = uint64(0x3000)
	// zPR: personality=indirect|pcrel|sdata4, FDE encoding=pcrel|sdata4.
	cieContent := []byte{1, 'z', 'P', 'R', 0, 1, 0x78, 30, 6, PEIndirect | PEPcrel | PESdata4, 0, 0, 0, 0, PEPcrel | PESdata4, 0x0c}
	// CIE content starts at sectionVA+8; the encoded personality sdata4 starts
	// after version/string/alignment/register/augmentation-length/encoding.
	fieldVA := sectionVA + 8 + 10
	delta := int32(slotVA - fieldVA)
	binary.LittleEndian.PutUint32(cieContent[10:14], uint32(delta))
	cieBody := append([]byte{0, 0, 0, 0}, cieContent...)
	data := appendLength(nil, cieBody)
	data = append(data, 0, 0, 0, 0)

	frame, err := ParseEHFrame(data, sectionVA, binary.LittleEndian, 8)
	if err != nil {
		t.Fatal(err)
	}
	cie := frame.CIEs[0]
	if cie == nil || cie.Personality == nil || *cie.Personality != slotVA || cie.PersonalityEncoding != PEIndirect|PEPcrel|PESdata4 {
		t.Fatalf("CIE personality=%+v", cie)
	}
	// Generic DecodePointer still refuses to claim a dereference without a
	// target-memory resolver.
	if _, err := DecodePointer([]byte{0, 0, 0, 0}, new(int), PEIndirect|PESdata4, binary.LittleEndian, 8, Bases{}); err == nil {
		t.Fatal("generic indirect pointer dereference was unexpectedly accepted")
	}
}

func TestParseEHFrameCIEAndFDE(t *testing.T) {
''',
)
replace_once(
    "internal/unwind/unwind_test.go",
    '''\tif len(plan.Thunks) != 1 || plan.Thunks[0].ID == 0 || plan.Personality != personality || plan.Thunks[0].VMLandingPad != 0x80 {
\t\tt.Fatalf("plan=%+v", plan)
\t}
''',
    '''\tif len(plan.Thunks) != 1 || plan.Thunks[0].ID == 0 || plan.Personality != personality || plan.Thunks[0].VMLandingPad != 0x80 || plan.Thunks[0].OriginalLandingPad != 0x1008 {
\t\tt.Fatalf("plan=%+v", plan)
\t}
\tchanged := plan.Thunks[0]
\tchanged.OriginalLandingPad++
\tif invokeThunkID(plan.Personality, fde.Offset, changed) == plan.Thunks[0].ID {
\t\tt.Fatal("invoke thunk ID did not include original landing identity")
\t}
''',
)

# Phase-13 unit fixture should assert the newly retained identity too.
replace_once(
    "internal/elf/exception_preflight_test.go",
    '''\tif thunk.OriginalPC != 0x1004 || thunk.VMCallOffset != 11 || thunk.VMLandingPad != 36 {
''',
    '''\tif thunk.OriginalPC != 0x1004 || thunk.OriginalLandingPad != 0x1018 || thunk.VMCallOffset != 11 || thunk.VMLandingPad != 36 {
''',
)

print("phase15 personality reference patch applied")
