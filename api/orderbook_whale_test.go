package api

import "testing"

func TestFindWhaleLevels_FilterSortAndLimit(t *testing.T) {
	levels := []BookLevel{
		{Price: "100", Qty: "100"}, // 10000
		{Price: "99", Qty: "30"},   // 2970
		{Price: "98", Qty: "60"},   // 5880
		{Price: "97", Qty: "10"},   // 970
	}

	got := findWhaleLevels(levels, 2500, 100, true, 2)
	if len(got) != 2 {
		t.Fatalf("expected 2 levels, got %d", len(got))
	}

	if got[0].Notional != 10000 || got[0].Price != 100 {
		t.Fatalf("unexpected first level: %+v", got[0])
	}
	if got[1].Notional != 5880 || got[1].Price != 98 {
		t.Fatalf("unexpected second level: %+v", got[1])
	}
}

func TestBuildWhaleOrderBookResponse_SideFilter(t *testing.T) {
	book := &BookMsg{
		Symbol: "BTCUSDT",
		Time:   123456789,
		Bids: []BookLevel{
			{Price: "100", Qty: "100"},
		},
		Asks: []BookLevel{
			{Price: "101", Qty: "100"},
		},
	}

	bidOnly := buildWhaleOrderBookResponse(book, 1000, 10, "BID", 100)
	if len(bidOnly.Bids) == 0 {
		t.Fatalf("expected bid whales")
	}
	if len(bidOnly.Asks) != 0 {
		t.Fatalf("expected no ask whales, got %d", len(bidOnly.Asks))
	}

	askOnly := buildWhaleOrderBookResponse(book, 1000, 10, "ASK", 100)
	if len(askOnly.Asks) == 0 {
		t.Fatalf("expected ask whales")
	}
	if len(askOnly.Bids) != 0 {
		t.Fatalf("expected no bid whales, got %d", len(askOnly.Bids))
	}
}
