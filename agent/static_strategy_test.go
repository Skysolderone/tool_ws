package agent

import (
	"testing"

	"tools/api"
)

func TestFilterPositionAnalysisBySymbols(t *testing.T) {
	items := []api.PositionAnalysis{
		{Symbol: "BTCUSDT"},
		{Symbol: "ETHUSDT"},
		{Symbol: "SOLUSDT"},
	}

	got := filterPositionAnalysisBySymbols(items, []string{"ethusdt", " btcusdt "})
	if len(got) != 2 {
		t.Fatalf("filterPositionAnalysisBySymbols len=%d, want 2", len(got))
	}
	if got[0].Symbol != "BTCUSDT" || got[1].Symbol != "ETHUSDT" {
		t.Fatalf("unexpected filtered symbols: %+v", got)
	}
}

func TestStaticActionItemMapping(t *testing.T) {
	tests := []struct {
		name        string
		item        api.PositionAnalysis
		wantAction  string
		wantEnabled bool
	}{
		{
			name:        "close advice maps to close action",
			item:        api.PositionAnalysis{Symbol: "BTCUSDT", Advice: "close"},
			wantAction:  "close",
			wantEnabled: true,
		},
		{
			name:        "stop loss advice maps to set_sl action",
			item:        api.PositionAnalysis{Symbol: "ETHUSDT", Advice: "stop_loss", StopLoss: 2200},
			wantAction:  "set_sl",
			wantEnabled: true,
		},
		{
			name:        "hold advice should not emit action",
			item:        api.PositionAnalysis{Symbol: "SOLUSDT", Advice: "hold"},
			wantAction:  "",
			wantEnabled: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := staticActionItem(tt.item, "medium")
			if ok != tt.wantEnabled {
				t.Fatalf("staticActionItem ok=%v, want %v", ok, tt.wantEnabled)
			}
			if !tt.wantEnabled {
				return
			}
			if got.Action != tt.wantAction {
				t.Fatalf("staticActionItem action=%s, want %s", got.Action, tt.wantAction)
			}
		})
	}
}
