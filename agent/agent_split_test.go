package agent

import (
	"reflect"
	"testing"
)

func TestNormalizeAndDedupeSymbols(t *testing.T) {
	got := normalizeAndDedupeSymbols([]string{" btcusdt ", "ETHUSDT", "BTCUSDT", "", "ethusdt"})
	want := []string{"BTCUSDT", "ETHUSDT"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("normalizeAndDedupeSymbols mismatch, got=%v want=%v", got, want)
	}
}

func TestShouldSplitAnalyzeBySymbol(t *testing.T) {
	tests := []struct {
		name string
		req  AnalysisRequest
		want bool
	}{
		{
			name: "positions with multiple symbols should split",
			req: AnalysisRequest{
				Mode:    "positions",
				Symbols: []string{"BTCUSDT", "ETHUSDT"},
			},
			want: true,
		},
		{
			name: "positions with single symbol should not split",
			req: AnalysisRequest{
				Mode:    "positions",
				Symbols: []string{"BTCUSDT"},
			},
			want: false,
		},
		{
			name: "full with multiple symbols should not split",
			req: AnalysisRequest{
				Mode:    "full",
				Symbols: []string{"BTCUSDT", "ETHUSDT"},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shouldSplitAnalyzeBySymbol(tt.req)
			if got != tt.want {
				t.Fatalf("shouldSplitAnalyzeBySymbol() = %v, want %v", got, tt.want)
			}
		})
	}
}
