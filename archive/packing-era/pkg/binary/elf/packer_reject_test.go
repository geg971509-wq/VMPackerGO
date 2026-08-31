package elf

import "testing"

func TestClosestNames_SuggestsTypo(t *testing.T) {
	names := []string{"picsmemo_sha1", "picsmemo_xcalloc", "main"}
	got := closestNames("picsmemo_sha", names, 3)
	if len(got) == 0 || got[0] != "picsmemo_sha1" {
		t.Fatalf("closestNames=%v", got)
	}
}

func TestMissingFunctionError_IncludesSuggestion(t *testing.T) {
	err := missingFunctionError("picsmemo_sha", []string{"picsmemo_sha1", "main"})
	if err == nil || err.Error() != "function 'picsmemo_sha' not found; did you mean: picsmemo_sha1" {
		t.Fatalf("err=%v", err)
	}
}

func TestSummarizeUnsupported_Kinds(t *testing.T) {
	items := []string{
		"SIMD/FP 偏移 0x001C: UNSUPPORTED (raw=0x6F00E400) - 不支持的指令类型",
		"SIMD/FP 偏移 0x01B0: UNSUPPORTED (raw=0x3C8583E0) - 不支持的指令类型",
		"out-of-range branch 偏移 0x0008: B (raw=0x14038BF6) - 分支目标超出函数范围",
	}
	got := summarizeUnsupported(items)
	want := "2 SIMD/FP instruction(s); 1 out-of-range branch(es)"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}
