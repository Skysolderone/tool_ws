package api

import (
	"math"
	"testing"
)

func TestSelectFurtherTPFromSRLong(t *testing.T) {
	sr := &SRResponse{
		Resistances: []SRLevel{
			{Price: 100.20},
			{Price: 100.75},
			{Price: 101.40},
		},
	}
	got, ok := selectFurtherTPFromSR("LONG", 100, sr, 0.8)
	if !ok {
		t.Fatal("expected to find a farther resistance")
	}
	if math.Abs(got-101.40) > 1e-9 {
		t.Fatalf("unexpected price: got=%.4f want=101.40", got)
	}
}

func TestApplyTakeProfitGuardFallback(t *testing.T) {
	tp, reasons := applyTakeProfitGuard("LONG", 100, 99, 100.10, nil, nil)
	if math.Abs(tp-102.0) > 1e-9 {
		t.Fatalf("unexpected fallback tp: got=%.4f want=102.0", tp)
	}
	if len(reasons) == 0 {
		t.Fatal("expected guard reason to be appended")
	}
}

func TestApplyTakeProfitGuardRiskReward(t *testing.T) {
	// rr = 0.9 / 1.0 = 0.9 < 1.2，应该被提升到 1.2
	tp, reasons := applyTakeProfitGuard("LONG", 100, 99, 100.90, nil, []string{"base"})
	if math.Abs(tp-101.20) > 1e-9 {
		t.Fatalf("unexpected rr-adjusted tp: got=%.4f want=101.20", tp)
	}
	if len(reasons) < 2 {
		t.Fatalf("expected guard reason, got reasons=%v", reasons)
	}
}
