package api

import (
	"fmt"
	"log"
	"math"
	"sort"
	"time"

	"gorm.io/gorm"
)

// SlippageRecord 滑点记录（GORM 模型，对应 slippage_records 表）
type SlippageRecord struct {
	gorm.Model
	Symbol        string  `gorm:"type:varchar(20);index" json:"symbol"`
	OrderID       string  `gorm:"type:varchar(40);index" json:"orderId"`
	IntendedPrice float64 `gorm:"type:numeric(36,18)" json:"intendedPrice"` // 下单时市场价
	ExecutedPrice float64 `gorm:"type:numeric(36,18)" json:"executedPrice"` // 实际成交均价
	SlippageBps   float64 `gorm:"type:numeric(18,6)" json:"slippageBps"`    // 滑点 basis points
	Side          string  `gorm:"type:varchar(10)" json:"side"`             // BUY / SELL
	Quantity      float64 `gorm:"type:numeric(36,18)" json:"quantity"`
	Source         string  `gorm:"type:varchar(40);index" json:"source"` // scalp / manual / tpsl 等
	ArrivalPrice   float64 `gorm:"type:numeric(36,18)" json:"arrivalPrice"`   // 下单瞬间标记价
	LatencyMs      int64   `json:"latencyMs"`                                 // 下单到成交毫秒数
	StrategySource string  `gorm:"type:varchar(40);index" json:"strategySource"` // 策略来源归因
}

// SlippageStats 滑点统计摘要
type SlippageStats struct {
	Symbol         string  `json:"symbol"`
	TotalTrades    int     `json:"totalTrades"`
	AvgSlippageBps float64 `json:"avgSlippageBps"`
	MaxSlippageBps float64 `json:"maxSlippageBps"`
	P50SlippageBps float64 `json:"p50SlippageBps"`
	P95SlippageBps float64 `json:"p95SlippageBps"`
	UpdatedAt      string  `json:"updatedAt"`
}

// RecordSlippage 记录一笔成交的滑点到数据库
// intendedPrice: 下单时的市场标记价格
// executedPrice: 实际成交均价（AvgPrice）
// slippageBps = |executedPrice - intendedPrice| / intendedPrice * 10000
func RecordSlippage(symbol, orderID, side, source string, intendedPrice, executedPrice, quantity float64) {
	if DB == nil {
		return
	}
	if symbol == "" || orderID == "" {
		return
	}
	if intendedPrice <= 0 || executedPrice <= 0 {
		return
	}

	slippageBps := math.Abs(executedPrice-intendedPrice) / intendedPrice * 10000

	record := &SlippageRecord{
		Symbol:        symbol,
		OrderID:       orderID,
		IntendedPrice: intendedPrice,
		ExecutedPrice: executedPrice,
		SlippageBps:   roundFloat(slippageBps, 4),
		Side:          side,
		Quantity:      quantity,
		Source:        source,
	}

	if err := DB.Create(record).Error; err != nil {
		log.Printf("[Slippage] Failed to save slippage record for order %s: %v", orderID, err)
	}
}

// GetSlippageStats 查询指定交易对的滑点统计
// 统计最近 1000 条记录
func GetSlippageStats(symbol string) (*SlippageStats, error) {
	if DB == nil {
		return nil, fmt.Errorf("database not initialized")
	}
	if symbol == "" {
		return nil, fmt.Errorf("symbol is required")
	}

	var records []SlippageRecord
	q := DB.Where("symbol = ?", symbol).
		Order("created_at DESC").
		Limit(1000)
	if err := q.Find(&records).Error; err != nil {
		return nil, fmt.Errorf("query slippage records: %w", err)
	}

	stats := &SlippageStats{
		Symbol:    symbol,
		UpdatedAt: time.Now().UTC().Format(time.RFC3339),
	}
	if len(records) == 0 {
		return stats, nil
	}

	stats.TotalTrades = len(records)

	// 收集所有滑点值用于统计
	bps := make([]float64, 0, len(records))
	var sumBps float64
	for _, r := range records {
		bps = append(bps, r.SlippageBps)
		sumBps += r.SlippageBps
		if r.SlippageBps > stats.MaxSlippageBps {
			stats.MaxSlippageBps = r.SlippageBps
		}
	}

	stats.AvgSlippageBps = roundFloat(sumBps/float64(len(records)), 4)
	stats.MaxSlippageBps = roundFloat(stats.MaxSlippageBps, 4)

	// 计算百分位数
	sort.Float64s(bps)
	stats.P50SlippageBps = roundFloat(percentile(bps, 50), 4)
	stats.P95SlippageBps = roundFloat(percentile(bps, 95), 4)

	return stats, nil
}

// percentile 计算有序切片的第 p 百分位数（线性插值）
func percentile(sorted []float64, p float64) float64 {
	n := len(sorted)
	if n == 0 {
		return 0
	}
	if n == 1 {
		return sorted[0]
	}
	// 线性插值
	index := p / 100.0 * float64(n-1)
	lower := int(math.Floor(index))
	upper := int(math.Ceil(index))
	if lower == upper {
		return sorted[lower]
	}
	frac := index - float64(lower)
	return sorted[lower]*(1-frac) + sorted[upper]*frac
}
