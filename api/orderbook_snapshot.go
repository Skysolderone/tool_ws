package api

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/utils"
)

func normalizeOrderBookLimit(limit int) int {
	switch limit {
	case 5, 10, 20, 50, 100, 500, 1000:
		return limit
	default:
		return 100
	}
}

// GetOrderBookSnapshot 通过 REST 拉一次订单簿快照
func GetOrderBookSnapshot(ctx context.Context, symbol string, limit int) (*BookMsg, error) {
	if Client == nil {
		return nil, fmt.Errorf("client not initialized")
	}
	sym := strings.ToUpper(strings.TrimSpace(symbol))
	if sym == "" {
		return nil, fmt.Errorf("symbol is required")
	}

	depth, err := Client.NewDepthService().
		Symbol(sym).
		Limit(normalizeOrderBookLimit(limit)).
		Do(ctx)
	if err != nil {
		return nil, err
	}

	bids := make([]BookLevel, 0, len(depth.Bids))
	for _, b := range depth.Bids {
		bids = append(bids, BookLevel{Price: b.Price, Qty: b.Quantity})
	}
	asks := make([]BookLevel, 0, len(depth.Asks))
	for _, a := range depth.Asks {
		asks = append(asks, BookLevel{Price: a.Price, Qty: a.Quantity})
	}

	t := depth.Time
	if t == 0 {
		t = time.Now().UnixMilli()
	}

	return &BookMsg{
		Type:   "book",
		Symbol: sym,
		Time:   t,
		Bids:   bids,
		Asks:   asks,
	}, nil
}

// HandleGetOrderBook GET /tool/orderbook?symbol=ETHUSDT&limit=100
func HandleGetOrderBook(c context.Context, ctx *app.RequestContext) {
	symbol := strings.ToUpper(strings.TrimSpace(ctx.Query("symbol")))
	if symbol == "" {
		ctx.JSON(http.StatusBadRequest, utils.H{"error": "symbol is required"})
		return
	}

	limit := 100
	if raw := strings.TrimSpace(ctx.DefaultQuery("limit", "100")); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil {
			limit = v
		}
	}

	book, err := GetOrderBookSnapshot(c, symbol, limit)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, utils.H{"error": err.Error()})
		return
	}

	ctx.JSON(http.StatusOK, utils.H{"data": book})
}
