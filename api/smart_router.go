package api

import (
	"context"
	"fmt"
	"log"
	"math"
	"strconv"
	"time"
)

// SmartRouterConfig 智能下单路由配置
type SmartRouterConfig struct {
	EnablePostOnly   bool    `json:"enablePostOnly"`
	FallbackToIOC    bool    `json:"fallbackToIOC"`
	MaxSliceSizeUSDT float64 `json:"maxSliceSizeUSDT"` // 单片最大金额
	MaxImpactBps     float64 `json:"maxImpactBps"`     // 盘口冲击上限 bps
	SliceIntervalMs  int     `json:"sliceIntervalMs"`  // 分片间隔
}

// SliceResult 分片下单结果
type SliceResult struct {
	SliceIndex  int     `json:"sliceIndex"`
	OrderID     int64   `json:"orderId"`
	FilledQty   float64 `json:"filledQty"`
	AvgPrice    float64 `json:"avgPrice"`
	SlippageBps float64 `json:"slippageBps"`
}

// SmartRoute 智能路由下单：分片 + 盘口冲击约束
func SmartRoute(ctx context.Context, req PlaceOrderReq, cfg SmartRouterConfig) (*PlaceOrderResult, []SliceResult, error) {
	totalUSDT, err := strconv.ParseFloat(req.QuoteQuantity, 64)
	if err != nil || totalUSDT <= 0 {
		return nil, nil, fmt.Errorf("invalid quoteQuantity")
	}

	// 检查盘口冲击
	if cfg.MaxImpactBps > 0 {
		impact, impactErr := estimateImpact(req.Symbol, totalUSDT)
		if impactErr == nil && impact > cfg.MaxImpactBps {
			log.Printf("[SmartRouter] Impact %.1f bps exceeds limit %.1f bps for %s", impact, cfg.MaxImpactBps, req.Symbol)
			// 降低下单量到安全水平
			safeUSDT := totalUSDT * (cfg.MaxImpactBps / impact) * 0.8
			if safeUSDT < 5 {
				safeUSDT = 5
			}
			totalUSDT = safeUSDT
			req.QuoteQuantity = strconv.FormatFloat(totalUSDT, 'f', 2, 64)
		}
	}

	// 判断是否需要分片
	sliceSize := cfg.MaxSliceSizeUSDT
	if sliceSize <= 0 || totalUSDT <= sliceSize {
		// 单次下单
		result, placeErr := PlaceOrderViaWs(ctx, req)
		if placeErr != nil {
			return nil, nil, placeErr
		}
		return result, nil, nil
	}

	// 分片下单
	slices := splitOrderSlices(totalUSDT, sliceSize)
	var allResults []SliceResult
	var lastResult *PlaceOrderResult
	intervalMs := cfg.SliceIntervalMs
	if intervalMs <= 0 {
		intervalMs = 200
	}

	for i, sliceUSDT := range slices {
		sliceReq := req
		sliceReq.QuoteQuantity = strconv.FormatFloat(sliceUSDT, 'f', 2, 64)

		result, placeErr := PlaceOrderViaWs(ctx, sliceReq)
		if placeErr != nil {
			log.Printf("[SmartRouter] Slice %d/%d failed: %v", i+1, len(slices), placeErr)
			allResults = append(allResults, SliceResult{SliceIndex: i, OrderID: 0})
			continue
		}

		lastResult = result
		sr := SliceResult{SliceIndex: i}
		if result.Order != nil {
			sr.OrderID = result.Order.OrderID
			sr.AvgPrice, _ = strconv.ParseFloat(result.Order.AvgPrice, 64)
			sr.FilledQty, _ = strconv.ParseFloat(result.Order.ExecutedQuantity, 64)
		}
		allResults = append(allResults, sr)

		if i < len(slices)-1 {
			time.Sleep(time.Duration(intervalMs) * time.Millisecond)
		}
	}

	return lastResult, allResults, nil
}

func estimateImpact(symbol string, sizeUSDT float64) (float64, error) {
	flow, err := AnalyzeOrderFlow(symbol, 20)
	if err != nil {
		return 0, err
	}

	totalDepthUSDT := flow.BidVolume + flow.AskVolume
	if totalDepthUSDT <= 0 {
		return 0, fmt.Errorf("no depth data")
	}

	// 简化冲击估算：订单量 / 盘口深度 * 10000 bps
	impactBps := (sizeUSDT / totalDepthUSDT) * 10000
	return math.Max(impactBps, 0), nil
}

func splitOrderSlices(totalUSDT, sliceSize float64) []float64 {
	var slices []float64
	remaining := totalUSDT
	for remaining > 0 {
		s := math.Min(remaining, sliceSize)
		if s < 5 {
			// 最后一片太小，合并到前一片
			if len(slices) > 0 {
				slices[len(slices)-1] += s
			} else {
				slices = append(slices, s)
			}
			break
		}
		slices = append(slices, s)
		remaining -= s
	}
	return slices
}
