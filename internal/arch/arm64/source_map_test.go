package arm64

import (
	"testing"

	"github.com/geg971509-wq/VMPackerGO/internal/vm"
)

func TestTranslateResultSourceMapCoversMergedOffsetsAndFunctionEnd(t *testing.T) {
	decoder := NewDecoder()
	raws := []uint32{
		0xc85ffc20, // ldaxr x0, [x1]
		0x91000400, // add x0, x0, #1
		0xc802fc20, // stlxr w2, x0, [x1]
	}
	instructions := make([]vm.Instruction, len(raws))
	for i, raw := range raws {
		instructions[i] = decoder.Decode(raw, i*4)
	}
	translator, err := NewTranslator(0x1000, len(raws)*4, vm.IdentityOpcodeMap())
	if err != nil {
		t.Fatal(err)
	}
	result, err := translator.Translate(instructions)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Unsupported) != 0 {
		t.Fatalf("unsupported=%v", result.Unsupported)
	}
	wantOffsets := []int{0, 4, 8, 12}
	if len(result.SourceMap) != len(wantOffsets) {
		t.Fatalf("source map=%v", result.SourceMap)
	}
	for i, want := range wantOffsets {
		if result.SourceMap[i].ARM64Offset != want {
			t.Fatalf("source map[%d]=%+v want offset=%d", i, result.SourceMap[i], want)
		}
		if i > 0 && result.SourceMap[i-1].ARM64Offset >= result.SourceMap[i].ARM64Offset {
			t.Fatalf("source map is not strictly sorted: %v", result.SourceMap)
		}
	}
	if result.SourceMap[1].VMOffset != result.SourceMap[2].VMOffset {
		t.Fatalf("merged exclusive offsets did not share VM continuation: %v", result.SourceMap)
	}
	if result.SourceMap[len(result.SourceMap)-1].VMOffset != result.SourceMap[2].VMOffset {
		t.Fatalf("function-end mapping=%v", result.SourceMap)
	}
}

func TestNativeCallSiteUsesSameSourceMapVMOffset(t *testing.T) {
	instructions := []vm.Instruction{
		{Op: int(BL), Offset: 0, Imm: 0x100},
		{Op: int(NOP), Offset: 4},
		{Op: int(NOP), Offset: 8},
	}
	translator, err := NewTranslator(0x2000, 12, vm.IdentityOpcodeMap())
	if err != nil {
		t.Fatal(err)
	}
	result, err := translator.Translate(instructions)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Unsupported) != 0 || len(result.NativeCallSites) != 1 {
		t.Fatalf("unsupported=%v calls=%v", result.Unsupported, result.NativeCallSites)
	}
	call := result.NativeCallSites[0]
	found := false
	for _, entry := range result.SourceMap {
		if entry.ARM64Offset == call.ARM64Offset {
			found = true
			if entry.VMOffset != call.VMOffset {
				t.Fatalf("call=%+v source=%+v", call, entry)
			}
		}
	}
	if !found {
		t.Fatalf("native call offset %d missing from source map %v", call.ARM64Offset, result.SourceMap)
	}
}
