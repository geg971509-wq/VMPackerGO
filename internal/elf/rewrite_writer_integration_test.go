package elf

import (
	"strings"
	"testing"
)

func TestProcessAnalyzedProducesRewriteArtifact(t *testing.T) {
	fixture := buildELFFixture(fixtureOptions{dynamic: true})
	request, analysis, preparation := rewritePlanPreparation(t, fixture, false)
	request.RuntimeImage = rewritePlanRuntimeImage(t, request.Opcodes)
	request.Preparation = preparation

	result, err := ProcessAnalyzed(request, analysis)
	if err != nil || result.DevelopmentStrategy != "rewrite-artifact-ready" || len(result.Artifact) == 0 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	assertRewrittenArtifactParses(t, result.Artifact, analysis.TargetKind)
}

func TestProcessAnalyzedPreservesPlanReadyFactsWhenWriterRejectsPlan(t *testing.T) {
	fixture := buildELFFixture(fixtureOptions{dynamic: true})
	request, analysis, preparation := rewritePlanPreparation(t, fixture, false)
	request.RuntimeImage = rewritePlanRuntimeImage(t, request.Opcodes)
	request.Preparation = preparation

	selection := analysis.Selections[0]
	size := selection.Size()
	selection.Address = 0x1000 + uint64(fixture.phoff)
	selection.End = selection.Address + size
	selection.Offset = uint64(fixture.phoff)
	analysis.Selections[0] = selection
	preparation.Functions[0].Selection = selection

	result, err := ProcessAnalyzed(request, analysis)
	if err == nil || !strings.Contains(err.Error(), "overlaps ELF metadata") {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if result.DevelopmentStrategy != "rewrite-plan-ready" || len(result.Artifact) != 0 || len(result.Functions) != 1 || result.Functions[0].Translated == 0 {
		t.Fatalf("writer failure lost the completed plan boundary: %+v", result)
	}
}
