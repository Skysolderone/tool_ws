package api

import "testing"

func TestCalcRecommendSignalEvalMetrics(t *testing.T) {
	pnl, hit := calcRecommendSignalEvalMetrics("LONG", 100, 105)
	if pnl != 5 {
		t.Fatalf("unexpected long pnl: %.4f", pnl)
	}
	if !hit {
		t.Fatal("expected long signal hit")
	}

	pnl, hit = calcRecommendSignalEvalMetrics("LONG", 100, 96)
	if pnl != -4 {
		t.Fatalf("unexpected long pnl: %.4f", pnl)
	}
	if hit {
		t.Fatal("expected long signal miss")
	}

	pnl, hit = calcRecommendSignalEvalMetrics("SHORT", 100, 95)
	if pnl != 5 {
		t.Fatalf("unexpected short pnl: %.4f", pnl)
	}
	if !hit {
		t.Fatal("expected short signal hit")
	}

	pnl, hit = calcRecommendSignalEvalMetrics("SHORT", 100, 103)
	if pnl != -3 {
		t.Fatalf("unexpected short pnl: %.4f", pnl)
	}
	if hit {
		t.Fatal("expected short signal miss")
	}
}

func TestCalcRecommendSignalEvalMetricsInvalid(t *testing.T) {
	pnl, hit := calcRecommendSignalEvalMetrics("LONG", 0, 100)
	if pnl != 0 || hit {
		t.Fatalf("expected zero/false for invalid entry, got pnl=%.4f hit=%v", pnl, hit)
	}

	pnl, hit = calcRecommendSignalEvalMetrics("X", 100, 100)
	if pnl != 0 || hit {
		t.Fatalf("expected zero/false for invalid direction, got pnl=%.4f hit=%v", pnl, hit)
	}
}
