package elf

import (
	"strings"
	"testing"
)

func TestExceptionBridgeRequiresGNUUnwindDiscovery(t *testing.T) {
	fixture := buildELFFixture(fixtureOptions{dynamic: true})
	planner := rewritePlanner{
		req: Request{Input: fixture.data},
		preparation: &TranslationPreparation{ExceptionBridges: []PreparedExceptionBridge{{
			Selection: Selection{Name: "throws", Address: 0x1000, End: 0x1010},
		}}},
	}
	if err := planner.reserveGNUUnwindIndex(); err == nil || !strings.Contains(err.Error(), "PT_GNU_EH_FRAME") {
		t.Fatalf("missing unwind discovery err=%v", err)
	}
}

func TestMissingGNUUnwindDiscoveryRemainsAllowedWithoutExceptionBridge(t *testing.T) {
	fixture := buildELFFixture(fixtureOptions{dynamic: true})
	planner := rewritePlanner{
		req:         Request{Input: fixture.data},
		preparation: &TranslationPreparation{},
	}
	if err := planner.reserveGNUUnwindIndex(); err != nil {
		t.Fatal(err)
	}
	if planner.plan.gnuEHFrame != nil {
		t.Fatalf("unexpected GNU unwind plan=%+v", planner.plan.gnuEHFrame)
	}
}
