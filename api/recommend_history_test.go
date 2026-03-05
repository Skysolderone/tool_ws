package api

import (
	"testing"
	"time"
)

func TestRecommendHistoryRedisKeyByDay(t *testing.T) {
	day := time.Date(2026, 3, 5, 15, 30, 0, 0, time.FixedZone("CST", 8*3600))
	key := recommendHistoryRedisKey(day, "tool:")
	if key != "tool:recommend:signal:history:v1:20260305" {
		t.Fatalf("unexpected redis key: %s", key)
	}
}

func TestParseRecommendHistoryItemsFromDB(t *testing.T) {
	records := []RecommendSignalRecord{
		{
			ID:         10,
			Symbol:     "BTCUSDT",
			Direction:  "LONG",
			Confidence: 78,
			Entry:      62500.12,
			StopLoss:   61200.01,
			TakeProfit: 64200.88,
			Reasons:    `["RSI回升","量能放大"]`,
			Signals:    `[{"timeframe":"1h","direction":"LONG","score":2.1}]`,
			Source:     "engine",
			ScannedAt:  time.Unix(1700000000, 0),
			CreatedAt:  time.Unix(1700000100, 0),
		},
	}

	items := parseRecommendHistoryItemsFromDB(records)
	if len(items) != 1 {
		t.Fatalf("unexpected items len: %d", len(items))
	}
	item := items[0]
	if item.Symbol != "BTCUSDT" || item.Direction != "LONG" {
		t.Fatalf("unexpected item basic fields: %+v", item)
	}
	if len(item.Reasons) != 2 {
		t.Fatalf("unexpected reasons len: %d", len(item.Reasons))
	}
	if len(item.Signals) != 1 || item.Signals[0].Timeframe != "1h" {
		t.Fatalf("unexpected signals: %+v", item.Signals)
	}
}
