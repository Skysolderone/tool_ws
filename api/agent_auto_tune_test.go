package api

import (
	"reflect"
	"testing"
)

func TestNormalizeProfile(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{in: "conservative", want: "conservative"},
		{in: " Aggressive ", want: "aggressive"},
		{in: "custom", want: "custom"},
		{in: "", want: "custom"},
		{in: "unknown", want: "custom"},
	}

	for _, tc := range cases {
		got := normalizeProfile(tc.in)
		if got != tc.want {
			t.Fatalf("normalizeProfile(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestReconcileBlockedSymbols_NonRecovery(t *testing.T) {
	after, added, removed := reconcileBlockedSymbols(
		[]string{"btcusdt", "ETHUSDT"},
		[]string{"solusdt", "ETHUSDT"},
		false,
	)

	if !reflect.DeepEqual(after, []string{"BTCUSDT", "ETHUSDT", "SOLUSDT"}) {
		t.Fatalf("after = %v", after)
	}
	if !reflect.DeepEqual(added, []string{"SOLUSDT"}) {
		t.Fatalf("added = %v", added)
	}
	if len(removed) != 0 {
		t.Fatalf("removed = %v, want empty", removed)
	}
}

func TestReconcileBlockedSymbols_Recovery(t *testing.T) {
	after, added, removed := reconcileBlockedSymbols(
		[]string{"BTCUSDT", "ETHUSDT", "XRPUSDT"},
		[]string{"ethusdt", "solusdt"},
		true,
	)

	if !reflect.DeepEqual(after, []string{"ETHUSDT", "SOLUSDT"}) {
		t.Fatalf("after = %v", after)
	}
	if !reflect.DeepEqual(added, []string{"SOLUSDT"}) {
		t.Fatalf("added = %v", added)
	}
	if !reflect.DeepEqual(removed, []string{"BTCUSDT", "XRPUSDT"}) {
		t.Fatalf("removed = %v", removed)
	}
}

func TestStreakHelpers_WithNilDB(t *testing.T) {
	originDB := DB
	DB = nil
	t.Cleanup(func() {
		DB = originDB
	})

	if has24HHitRateStreak(3, func(rate float64) bool { return rate < 40 }) {
		t.Fatalf("has24HHitRateStreak should be false when DB is nil")
	}
	if got := findPoorSymbolsStreak(7, 40); got != nil {
		t.Fatalf("findPoorSymbolsStreak = %v, want nil when DB is nil", got)
	}
}
