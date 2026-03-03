package agent

import (
	"encoding/json"
	"testing"
)

func TestBuildMockAnalysisOutput(t *testing.T) {
	out := buildMockAnalysisOutput(AnalysisRequest{
		Mode:    "positions",
		Symbols: []string{"ethusdt"},
		Mock:    true,
	})
	if out == nil {
		t.Fatal("mock output is nil")
	}
	if out.PositionAnalysis == nil || len(out.PositionAnalysis) == 0 {
		t.Fatal("position_analysis should not be empty")
	}
	if out.PositionAnalysis[0].Symbol != "ETHUSDT" {
		t.Fatalf("unexpected symbol: %s", out.PositionAnalysis[0].Symbol)
	}
	if len(out.ActionItems) == 0 {
		t.Fatal("action_items should not be empty")
	}

	b, err := json.Marshal(out)
	if err != nil {
		t.Fatalf("marshal mock output failed: %v", err)
	}
	t.Logf("mock output: %s", string(b))
}
