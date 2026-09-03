#!/usr/bin/env python3
from pathlib import Path

p = Path("scripts/phase17_invoke_runtime_patch.py")
text = p.read_text()
old = '''replace_once(
    "internal/runtime/runtime.go",
    \'\'\'\\tExclusiveRegions   []vm.ExclusiveRegion
\\tFPSIMDInstructions []uint32
}
\'\'\',
    \'\'\'\\tExclusiveRegions   []vm.ExclusiveRegion
\\tFPSIMDInstructions []uint32
\\tExceptionInvokes   []ExceptionInvokeConfig
}
\'\'\',
)
replace_once(
    "internal/runtime/runtime.go",
    \'\'\'\\tExclusiveRegions   []vm.ExclusiveRegion
\\tFPSIMDInstructions []uint32
}
\'\'\',
    \'\'\'\\tExclusiveRegions   []vm.ExclusiveRegion
\\tFPSIMDInstructions []uint32
\\tExceptionInvokes   []ExceptionInvokeImage
}
\'\'\',
)
'''
new = '''replace_once(
    "internal/runtime/runtime.go",
    \'\'\'type BuildConfig struct {
\\tNDKDir             string
\\tOpcodes            vm.OpcodeMap
\\tSVCImmediates      []uint16
\\tExclusiveRegions   []vm.ExclusiveRegion
\\tFPSIMDInstructions []uint32
}
\'\'\',
    \'\'\'type BuildConfig struct {
\\tNDKDir             string
\\tOpcodes            vm.OpcodeMap
\\tSVCImmediates      []uint16
\\tExclusiveRegions   []vm.ExclusiveRegion
\\tFPSIMDInstructions []uint32
\\tExceptionInvokes   []ExceptionInvokeConfig
}
\'\'\',
)
replace_once(
    "internal/runtime/runtime.go",
    \'\'\'type Image struct {
\\tObject             []byte
\\tSections           []Section
\\tSymbols            []Symbol
\\tRelocations        []Relocation
\\tGNUPropertyNote    []byte
\\tEHFrame            []byte
\\tOpcodeMapDigest    string
\\tSVCImmediates      []uint16
\\tExclusiveRegions   []vm.ExclusiveRegion
\\tFPSIMDInstructions []uint32
}
\'\'\',
    \'\'\'type Image struct {
\\tObject             []byte
\\tSections           []Section
\\tSymbols            []Symbol
\\tRelocations        []Relocation
\\tGNUPropertyNote    []byte
\\tEHFrame            []byte
\\tOpcodeMapDigest    string
\\tSVCImmediates      []uint16
\\tExclusiveRegions   []vm.ExclusiveRegion
\\tFPSIMDInstructions []uint32
\\tExceptionInvokes   []ExceptionInvokeImage
}
\'\'\',
)
'''
if text.count(old) != 1:
    raise SystemExit(f"expected one ambiguous runtime block, found {text.count(old)}")
p.write_text(text.replace(old, new, 1))
print("phase17 patch anchors repaired")
