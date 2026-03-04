package api

import (
	"strings"
	"testing"
	"time"
)

func TestValidateRecommendSignalLongInvalidRelation(t *testing.T) {
	now := time.Unix(1700000000, 0)

	ok, reason := validateRecommendSignal("LONG", 100, 100.1, 104, 100, 0, now, now)
	if ok {
		t.Fatal("expected invalid LONG signal when stopLoss >= entry")
	}
	if reason != "invalid_long_sl_entry_tp_relation" {
		t.Fatalf("unexpected reason: %s", reason)
	}
}

func TestValidateRecommendSignalShortInvalidRelation(t *testing.T) {
	now := time.Unix(1700000100, 0)

	ok, reason := validateRecommendSignal("SHORT", 100, 99.5, 97, 100, 0, now, now)
	if ok {
		t.Fatal("expected invalid SHORT signal when stopLoss <= entry")
	}
	if reason != "invalid_short_tp_entry_sl_relation" {
		t.Fatalf("unexpected reason: %s", reason)
	}
}

func TestValidateRecommendSignalTTLExpired(t *testing.T) {
	base := time.Unix(1700000200, 0)

	ok, reason := validateRecommendSignal("LONG", 100, 99, 103, 100, 0, base, base.Add(recSignalTTL+time.Second))
	if ok {
		t.Fatal("expected expired signal to be filtered")
	}
	if !strings.HasPrefix(reason, "signal_ttl_exceeded") {
		t.Fatalf("unexpected reason: %s", reason)
	}
}

func TestValidateRecommendSignalEntryDrift(t *testing.T) {
	now := time.Unix(1700000300, 0)

	ok, reason := validateRecommendSignal("LONG", 100, 99, 104, 100.6, 0, now, now)
	if ok {
		t.Fatal("expected LONG drift signal to be filtered")
	}
	if reason != "long_entry_drift_too_far" {
		t.Fatalf("unexpected reason: %s", reason)
	}

	ok, reason = validateRecommendSignal("SHORT", 100, 102, 96, 99.2, 0, now, now)
	if ok {
		t.Fatal("expected SHORT drift signal to be filtered")
	}
	if reason != "short_entry_drift_too_far" {
		t.Fatalf("unexpected reason: %s", reason)
	}
}

func TestValidateRecommendSignalLiveRRGuard(t *testing.T) {
	now := time.Unix(1700000400, 0)

	ok, reason := validateRecommendSignal("LONG", 100, 99, 101, 100, 0, now, now)
	if ok {
		t.Fatal("expected long low RR signal to be filtered")
	}
	if reason != "long_live_rr_too_low" {
		t.Fatalf("unexpected reason: %s", reason)
	}
}

func TestValidateRecommendSignalNearStopLoss(t *testing.T) {
	now := time.Unix(1700000500, 0)

	ok, reason := validateRecommendSignal("LONG", 100, 99.90, 102, 99.91, 0.01, now, now)
	if ok {
		t.Fatal("expected near-SL LONG signal to be filtered")
	}
	if reason != "long_near_stop_loss" {
		t.Fatalf("unexpected reason: %s", reason)
	}

	ok, reason = validateRecommendSignal("SHORT", 100, 100.10, 98, 100.09, 0.01, now, now)
	if ok {
		t.Fatal("expected near-SL SHORT signal to be filtered")
	}
	if reason != "short_near_stop_loss" {
		t.Fatalf("unexpected reason: %s", reason)
	}
}

func TestValidateRecommendSignalValid(t *testing.T) {
	base := time.Unix(1700000600, 0)

	if ok, reason := validateRecommendSignal("LONG", 100, 99, 103, 100.1, 0, base, base.Add(time.Minute)); !ok {
		t.Fatalf("expected valid LONG signal, reason=%s", reason)
	}
	if ok, reason := validateRecommendSignal("SHORT", 100, 101, 97, 100.2, 0, base, base.Add(time.Minute)); !ok {
		t.Fatalf("expected valid SHORT signal, reason=%s", reason)
	}
}
