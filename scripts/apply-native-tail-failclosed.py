#!/usr/bin/env python3
from pathlib import Path


def remove_between(text, start_marker, end_marker, label):
    start = text.find(start_marker)
    if start < 0:
        raise SystemExit(f"{label}: start marker missing")
    end = text.find(end_marker, start)
    if end < 0:
        raise SystemExit(f"{label}: end marker missing")
    return text[:start] + text[end:]


def main():
    path = Path("internal/arch/arm64/translator.go")
    text = path.read_text()
    for old in (
        "\tnativeTailTransfers map[int]struct{}\n",
        "\t\tnativeTailTransfers: make(map[int]struct{}),\n",
    ):
        if text.count(old) != 1:
            raise SystemExit(f"translator.go marker changed: {old!r}")
        text = text.replace(old, "", 1)
    path.write_text(text)

    path = Path("internal/arch/arm64/tr_branch.go")
    text = path.read_text()
    text = remove_between(
        text,
        "// SetNativeTailTransfer marks a terminal direct B to an executable target",
        "func (t *Translator) trBranchOrOutlined",
        "SetNativeTailTransfer",
    )
    native_check = '''\tif _, configured := t.nativeTailTransfers[inst.Offset]; configured {
\t\ttarget := int64(inst.Offset) + inst.Imm
\t\tif target >= 0 && target < int64(t.funcSize) {
\t\t\treturn fmt.Errorf("native tail handling at 0x%x is configured for an in-function branch target 0x%x", inst.Offset, target)
\t\t}
\t\treturn t.trNativeTail(inst)
\t}
'''
    if text.count(native_check) != 1:
        raise SystemExit("trBranchOrOutlined native-tail dispatch changed")
    text = text.replace(native_check, "", 1)
    text = remove_between(
        text,
        "// trNativeTail de-optimizes an external A64 tail branch",
        "func (t *Translator) trBranch(inst vm.Instruction) error {",
        "trNativeTail",
    )
    native_guard = '''\tif _, exists := t.nativeTailTransfers[branchOffset]; exists {
\t\treturn fmt.Errorf("tail branch offset 0x%x is configured more than once", branchOffset)
\t}
'''
    count = text.count(native_guard)
    if count != 2:
        raise SystemExit(f"expected two obsolete native-tail duplicate guards, found {count}")
    text = text.replace(native_guard, "")
    path.write_text(text)

    path = Path("internal/elf/outliner.go")
    text = path.read_text()
    func_start = text.index("func configureExternalTailTransfers(")
    start = text.index("\t\tvar names []string", func_start)
    end = text.index("\n\t}\n\treturn nil\n}", start)
    replacement = '''\t\tif symbols == nil {
\t\t\treturn fmt.Errorf("external unconditional branch at 0x%X to 0x%X is neither a selected packed tail target nor a validated compiler outlined helper", branchSite, target)
\t\t}
\t\tnames, err := symbols.directTransferNames(branchSite, target)
\t\tif err != nil {
\t\t\treturn err
\t\t}
\t\thelper, matched, err := resolveOutlinedTailHelper(input, meta, symbols, selection, branchSite, target, names)
\t\tif err != nil {
\t\t\treturn err
\t\t}
\t\tif !matched {
\t\t\treturn fmt.Errorf("external unconditional branch at 0x%X to 0x%X is neither a selected packed tail target nor a validated compiler outlined helper", branchSite, target)
\t\t}
\t\tif err := translator.SetOutlinedTailInline(inst.Offset, helper.raws); err != nil {
\t\t\treturn fmt.Errorf("outlined helper %q: %w", helper.name, err)
\t\t}
'''
    path.write_text(text[:start] + replacement + text[end:])

    path = Path("internal/elf/packed_tail_test.go")
    text = path.read_text()
    start = text.index("func TestExecutableUnselectedExternalTailBecomesNativeCallReturn")
    end = text.index("\nfunc TestUnmappedExternalTailRemainsFailClosed", start)
    replacement = '''func TestExecutableUnselectedExternalTailRemainsFailClosed(t *testing.T) {
\topcodes := vm.IdentityOpcodeMap()
\ttranslator, err := arm64.NewTranslator(0x1000, 4, opcodes)
\tif err != nil {
\t\tt.Fatal(err)
\t}
\tselection := Selection{Name: "caller", Address: 0x1000, End: 0x1004}
\tinstructions := []vm.Instruction{{Op: int(arm64.B), Offset: 0, Imm: 0x1000}}
\tmeta := &elfMetadata{loads: []loadMapping{{
\t\tvaddr: 0x2000, filesz: 0x100, memsz: 0x100,
\t\tflags: elf.PF_R | elf.PF_X,
\t}}}
\tif err := configureExternalTailTransfers(nil, meta, nil, selection, instructions, translator, nil); err == nil {
\t\tt.Fatal("executable but unselected native tail unexpectedly passed preparation")
\t}
}
'''
    path.write_text(text[:start] + replacement + text[end:])


if __name__ == "__main__":
    main()
