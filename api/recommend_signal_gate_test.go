package api

import (
	"testing"
	"time"
)

func resetRecSignalStatesForTest() {
	recSignalStatesMu.Lock()
	defer recSignalStatesMu.Unlock()
	recSignalStates = map[string]*recSignalState{}
}

func TestUpdateSignalStateConfirmationAndCooldown(t *testing.T) {
	resetRecSignalStatesForTest()

	now := time.Unix(1700000000, 0)

	if !updateSignalState("BTCUSDT", "LONG", now) {
		t.Fatal("aggressive mode should publish on first round")
	}
	if updateSignalState("BTCUSDT", "LONG", now.Add(1*time.Minute)) {
		t.Fatal("same direction inside cooldown should be blocked")
	}
	if updateSignalState("BTCUSDT", "LONG", now.Add(2*time.Minute)) {
		t.Fatal("same direction inside cooldown should be blocked")
	}
	if !updateSignalState("BTCUSDT", "LONG", now.Add(recSignalCooldown+1*time.Minute)) {
		t.Fatal("same direction after cooldown should pass again")
	}
}

func TestUpdateSignalStateResetAndDirectionSwitch(t *testing.T) {
	resetRecSignalStatesForTest()

	now := time.Unix(1700001000, 0)

	if !updateSignalState("ETHUSDT", "LONG", now) {
		t.Fatal("aggressive mode should publish on first round")
	}
	if updateSignalState("ETHUSDT", "", now.Add(1*time.Minute)) {
		t.Fatal("empty direction should reset and never publish")
	}
	if updateSignalState("ETHUSDT", "LONG", now.Add(2*time.Minute)) {
		t.Fatal("same direction should still be blocked by cooldown after reset")
	}

	if !updateSignalState("ETHUSDT", "SHORT", now.Add(3*time.Minute)) {
		t.Fatal("direction switch should pass immediately in aggressive mode")
	}
	if updateSignalState("ETHUSDT", "SHORT", now.Add(4*time.Minute)) {
		t.Fatal("same new direction inside cooldown should be blocked")
	}
}
