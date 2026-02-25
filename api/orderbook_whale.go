package api

import (
	"context"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/utils"
)

const (
	orderBookWhaleDefaultMinNotional = 100000.0
	orderBookWhaleDefaultLimit       = 500
	orderBookWhaleDefaultMaxRows     = 20
	orderBookWhaleMinNotionalFloor   = 1000.0
)

// WhaleLevel 盘口大挂单档位
type WhaleLevel struct {
	Price    float64 `json:"price"`
	Qty      float64 `json:"qty"`
	Notional float64 `json:"notional"`
	Distance float64 `json:"distance"` // 相对中间价百分比
}

// WhaleOrderBookResponse 盘口大挂单检测结果
type WhaleOrderBookResponse struct {
	Symbol       string       `json:"symbol"`
	SnapshotTime int64        `json:"snapshotTime"`
	Limit        int          `json:"limit"`
	Side         string       `json:"side"` // BOTH / BID / ASK
	MinNotional  float64      `json:"minNotional"`
	MaxRows      int          `json:"maxRows"`
	BestBid      float64      `json:"bestBid"`
	BestAsk      float64      `json:"bestAsk"`
	MidPrice     float64      `json:"midPrice"`
	BidsScanned  int          `json:"bidsScanned"`
	AsksScanned  int          `json:"asksScanned"`
	Bids         []WhaleLevel `json:"bids"`
	Asks         []WhaleLevel `json:"asks"`
}

// HandleGetOrderBookWhale GET /tool/orderbook/whale?symbol=ETHUSDT&minNotional=100000&limit=500&maxRows=20&side=BOTH
func HandleGetOrderBookWhale(c context.Context, ctx *app.RequestContext) {
	symbol := strings.ToUpper(strings.TrimSpace(ctx.Query("symbol")))
	if symbol == "" {
		ctx.JSON(http.StatusBadRequest, utils.H{"error": "symbol is required"})
		return
	}

	limit := orderBookWhaleDefaultLimit
	if raw := strings.TrimSpace(ctx.DefaultQuery("limit", strconv.Itoa(orderBookWhaleDefaultLimit))); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil {
			limit = normalizeOrderBookLimit(v)
		}
	}

	maxRows := orderBookWhaleDefaultMaxRows
	if raw := strings.TrimSpace(ctx.DefaultQuery("maxRows", strconv.Itoa(orderBookWhaleDefaultMaxRows))); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil {
			maxRows = clampInt(v, 1, 100)
		}
	}

	minNotional := orderBookWhaleDefaultMinNotional
	if raw := strings.TrimSpace(ctx.DefaultQuery("minNotional", "100000")); raw != "" {
		if v, err := strconv.ParseFloat(raw, 64); err == nil && v >= orderBookWhaleMinNotionalFloor {
			minNotional = v
		}
	}

	side := strings.ToUpper(strings.TrimSpace(ctx.DefaultQuery("side", "BOTH")))
	if side != "BID" && side != "ASK" && side != "BOTH" {
		ctx.JSON(http.StatusBadRequest, utils.H{"error": "side must be BID, ASK or BOTH"})
		return
	}

	book, err := GetOrderBookSnapshot(c, symbol, limit)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, utils.H{"error": err.Error()})
		return
	}

	result := buildWhaleOrderBookResponse(book, minNotional, maxRows, side, limit)
	ctx.JSON(http.StatusOK, utils.H{"data": result})
}

func buildWhaleOrderBookResponse(book *BookMsg, minNotional float64, maxRows int, side string, limit int) *WhaleOrderBookResponse {
	resp := &WhaleOrderBookResponse{
		Symbol:       book.Symbol,
		SnapshotTime: book.Time,
		Limit:        limit,
		Side:         side,
		MinNotional:  minNotional,
		MaxRows:      maxRows,
		BidsScanned:  len(book.Bids),
		AsksScanned:  len(book.Asks),
		Bids:         make([]WhaleLevel, 0),
		Asks:         make([]WhaleLevel, 0),
	}

	resp.BestBid = firstLevelPrice(book.Bids)
	resp.BestAsk = firstLevelPrice(book.Asks)
	resp.MidPrice = calcMidPrice(resp.BestBid, resp.BestAsk)

	if side == "BID" || side == "BOTH" {
		resp.Bids = findWhaleLevels(book.Bids, minNotional, resp.MidPrice, true, maxRows)
	}
	if side == "ASK" || side == "BOTH" {
		resp.Asks = findWhaleLevels(book.Asks, minNotional, resp.MidPrice, false, maxRows)
	}
	return resp
}

func findWhaleLevels(levels []BookLevel, minNotional, midPrice float64, bidSide bool, maxRows int) []WhaleLevel {
	out := make([]WhaleLevel, 0, len(levels))
	for _, lv := range levels {
		price, okPrice := parseBookFloat(lv.Price)
		qty, okQty := parseBookFloat(lv.Qty)
		if !okPrice || !okQty {
			continue
		}
		notional := price * qty
		if notional < minNotional {
			continue
		}

		distance := 0.0
		if midPrice > 0 {
			distance = (price - midPrice) / midPrice * 100
		}
		out = append(out, WhaleLevel{
			Price:    round2(price),
			Qty:      round2(qty),
			Notional: round2(notional),
			Distance: round2(distance),
		})
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].Notional != out[j].Notional {
			return out[i].Notional > out[j].Notional
		}
		if bidSide {
			return out[i].Price > out[j].Price
		}
		return out[i].Price < out[j].Price
	})

	if len(out) > maxRows {
		out = out[:maxRows]
	}
	return out
}

func parseBookFloat(s string) (float64, bool) {
	v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil || v <= 0 {
		return 0, false
	}
	return v, true
}

func firstLevelPrice(levels []BookLevel) float64 {
	if len(levels) == 0 {
		return 0
	}
	price, ok := parseBookFloat(levels[0].Price)
	if !ok {
		return 0
	}
	return round2(price)
}

func calcMidPrice(bestBid, bestAsk float64) float64 {
	if bestBid > 0 && bestAsk > 0 {
		return round2((bestBid + bestAsk) / 2)
	}
	if bestBid > 0 {
		return bestBid
	}
	if bestAsk > 0 {
		return bestAsk
	}
	return 0
}

func clampInt(v, min, max int) int {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}
