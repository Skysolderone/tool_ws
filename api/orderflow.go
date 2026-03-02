package api

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strconv"
	"time"
)

// PriceLevel 价格档位
type PriceLevel struct {
	Price    float64 `json:"price"`
	Quantity float64 `json:"quantity"`
	Notional float64 `json:"notional"` // USD 名义价值
}

// OrderFlowSnapshot 订单流快照
type OrderFlowSnapshot struct {
	Symbol        string       `json:"symbol"`
	BidVolume     float64      `json:"bidVolume"`     // 买方总量
	AskVolume     float64      `json:"askVolume"`     // 卖方总量
	Imbalance     float64      `json:"imbalance"`     // 不平衡度 (bid-ask)/(bid+ask)
	BidTopN       float64      `json:"bidTopN"`       // 前 N 档买方量
	AskTopN       float64      `json:"askTopN"`       // 前 N 档卖方量
	LargeBidWalls []PriceLevel `json:"largeBidWalls"` // 大买单墙
	LargeAskWalls []PriceLevel `json:"largeAskWalls"` // 大卖单墙
	Pressure      string       `json:"pressure"`      // BUY_HEAVY / SELL_HEAVY / NEUTRAL
	Time          string       `json:"time"`
}

const (
	// largWallNotionalThreshold 大单墙名义价值阈值（USDT）
	largeWallNotionalThreshold = 50000.0
	// topNDepth 前 N 档统计深度
	topNDepth = 5
	// imbalanceBuyHeavy 买方压力阈值
	imbalanceBuyHeavy = 0.3
	// imbalanceSellHeavy 卖方压力阈值
	imbalanceSellHeavy = -0.3
)

// AnalyzeOrderFlow 获取订单簿深度数据并分析买卖压力
// depth: 订单簿深度档数，建议 100~500
func AnalyzeOrderFlow(symbol string, depth int) (*OrderFlowSnapshot, error) {
	if symbol == "" {
		return nil, fmt.Errorf("symbol is required")
	}
	if depth <= 0 {
		depth = 500
	}
	if depth > 1000 {
		depth = 1000
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	book, err := Client.NewDepthService().Symbol(symbol).Limit(depth).Do(ctx)
	if err != nil {
		return nil, fmt.Errorf("fetch depth for %s: %w", symbol, err)
	}

	// 获取当前标记价格用于计算名义价值
	markPrice, _ := GetPriceCache().GetPrice(symbol)
	if markPrice == 0 {
		// 如果缓存没有，用 mid-price 近似
		if len(book.Bids) > 0 && len(book.Asks) > 0 {
			bid0, _ := strconv.ParseFloat(book.Bids[0].Price, 64)
			ask0, _ := strconv.ParseFloat(book.Asks[0].Price, 64)
			markPrice = (bid0 + ask0) / 2
		}
	}

	snap := &OrderFlowSnapshot{
		Symbol: symbol,
		Time:   time.Now().UTC().Format(time.RFC3339),
	}

	// --- 解析 Bids（买方，价格从高到低） ---
	var bidLevels []PriceLevel
	for _, b := range book.Bids {
		price, err := strconv.ParseFloat(b.Price, 64)
		if err != nil || price <= 0 {
			continue
		}
		qty, err := strconv.ParseFloat(b.Quantity, 64)
		if err != nil || qty <= 0 {
			continue
		}
		notional := price * qty
		bidLevels = append(bidLevels, PriceLevel{
			Price:    price,
			Quantity: qty,
			Notional: notional,
		})
		snap.BidVolume += qty
	}

	// --- 解析 Asks（卖方，价格从低到高） ---
	var askLevels []PriceLevel
	for _, a := range book.Asks {
		price, err := strconv.ParseFloat(a.Price, 64)
		if err != nil || price <= 0 {
			continue
		}
		qty, err := strconv.ParseFloat(a.Quantity, 64)
		if err != nil || qty <= 0 {
			continue
		}
		notional := price * qty
		askLevels = append(askLevels, PriceLevel{
			Price:    price,
			Quantity: qty,
			Notional: notional,
		})
		snap.AskVolume += qty
	}

	// --- 前 N 档统计 ---
	topN := topNDepth
	for i, lv := range bidLevels {
		if i >= topN {
			break
		}
		snap.BidTopN += lv.Quantity
	}
	for i, lv := range askLevels {
		if i >= topN {
			break
		}
		snap.AskTopN += lv.Quantity
	}

	// --- 不平衡度计算 ---
	total := snap.BidVolume + snap.AskVolume
	if total > 0 {
		snap.Imbalance = (snap.BidVolume - snap.AskVolume) / total
		snap.Imbalance = roundFloat(snap.Imbalance, 4)
	}

	// --- 大单墙检测 ---
	// 使用名义价值（price * qty）判断，阈值 50000 USDT
	// 如果 markPrice 存在，用 markPrice * qty 得到更准确的名义价值
	wallThreshold := largeWallNotionalThreshold
	for _, lv := range bidLevels {
		notional := lv.Notional
		if markPrice > 0 {
			notional = markPrice * lv.Quantity
		}
		if notional >= wallThreshold {
			snap.LargeBidWalls = append(snap.LargeBidWalls, PriceLevel{
				Price:    lv.Price,
				Quantity: lv.Quantity,
				Notional: notional,
			})
		}
	}
	for _, lv := range askLevels {
		notional := lv.Notional
		if markPrice > 0 {
			notional = markPrice * lv.Quantity
		}
		if notional >= wallThreshold {
			snap.LargeAskWalls = append(snap.LargeAskWalls, PriceLevel{
				Price:    lv.Price,
				Quantity: lv.Quantity,
				Notional: notional,
			})
		}
	}

	// 大单墙按名义价值降序排列，取前 20 个
	sort.Slice(snap.LargeBidWalls, func(i, j int) bool {
		return snap.LargeBidWalls[i].Notional > snap.LargeBidWalls[j].Notional
	})
	sort.Slice(snap.LargeAskWalls, func(i, j int) bool {
		return snap.LargeAskWalls[i].Notional > snap.LargeAskWalls[j].Notional
	})
	if len(snap.LargeBidWalls) > 20 {
		snap.LargeBidWalls = snap.LargeBidWalls[:20]
	}
	if len(snap.LargeAskWalls) > 20 {
		snap.LargeAskWalls = snap.LargeAskWalls[:20]
	}

	// --- 压力方向判断 ---
	switch {
	case snap.Imbalance > imbalanceBuyHeavy:
		snap.Pressure = "BUY_HEAVY"
	case snap.Imbalance < imbalanceSellHeavy:
		snap.Pressure = "SELL_HEAVY"
	default:
		snap.Pressure = "NEUTRAL"
	}

	// 四舍五入数值
	snap.BidVolume = roundFloat(snap.BidVolume, 4)
	snap.AskVolume = roundFloat(snap.AskVolume, 4)
	snap.BidTopN = roundFloat(snap.BidTopN, 4)
	snap.AskTopN = roundFloat(snap.AskTopN, 4)

	return snap, nil
}

// roundFloat 保留指定小数位
func roundFloat(v float64, decimals int) float64 {
	pow := math.Pow(10, float64(decimals))
	return math.Round(v*pow) / pow
}
