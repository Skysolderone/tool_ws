package api

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/utils"
)

// ExecutionQualityReport 按策略来源的执行质量统计
type ExecutionQualityReport struct {
	Source          string  `json:"source"`
	TotalOrders     int     `json:"totalOrders"`
	AvgSlippageBps  float64 `json:"avgSlippageBps"`
	P95SlippageBps  float64 `json:"p95SlippageBps"`
	AvgLatencyMs    float64 `json:"avgLatencyMs"`
	P95LatencyMs    float64 `json:"p95LatencyMs"`
	TotalSlippageCost float64 `json:"totalSlippageCost"` // 估算滑点成本 USDT
}

// RecordExecutionQuality 记录一笔下单的完整执行质量
func RecordExecutionQuality(symbol, orderID, side, source string, arrivalPrice, fillPrice, qty float64, latencyMs int64) {
	if DB == nil || symbol == "" || orderID == "" {
		return
	}
	if arrivalPrice <= 0 || fillPrice <= 0 {
		return
	}

	slippageBps := math.Abs(fillPrice-arrivalPrice) / arrivalPrice * 10000

	record := &SlippageRecord{
		Symbol:         symbol,
		OrderID:        orderID,
		IntendedPrice:  arrivalPrice,
		ExecutedPrice:  fillPrice,
		SlippageBps:    roundFloat(slippageBps, 4),
		Side:           side,
		Quantity:        qty,
		Source:         source,
		ArrivalPrice:   arrivalPrice,
		LatencyMs:      latencyMs,
		StrategySource: source,
	}

	if err := DB.Create(record).Error; err != nil {
		// 静默失败，不影响主流程
	}
}

// GetExecutionQualityReport 按策略来源查询执行质量统计
func GetExecutionQualityReport(source string, days int) ([]ExecutionQualityReport, error) {
	if DB == nil {
		return nil, fmt.Errorf("database not initialized")
	}
	if days <= 0 {
		days = 30
	}

	since := time.Now().AddDate(0, 0, -days)
	var records []SlippageRecord
	q := DB.Where("created_at >= ?", since).Order("created_at DESC").Limit(5000)
	if source != "" {
		q = q.Where("strategy_source = ? OR source = ?", source, source)
	}
	if err := q.Find(&records).Error; err != nil {
		return nil, err
	}

	// 按 source 分组
	grouped := map[string][]SlippageRecord{}
	for _, r := range records {
		src := r.StrategySource
		if src == "" {
			src = r.Source
		}
		if src == "" {
			src = "unknown"
		}
		grouped[src] = append(grouped[src], r)
	}

	var reports []ExecutionQualityReport
	for src, recs := range grouped {
		report := buildQualityReport(src, recs)
		reports = append(reports, report)
	}

	sort.Slice(reports, func(i, j int) bool {
		return reports[i].TotalOrders > reports[j].TotalOrders
	})
	return reports, nil
}

func buildQualityReport(source string, records []SlippageRecord) ExecutionQualityReport {
	n := len(records)
	report := ExecutionQualityReport{Source: source, TotalOrders: n}
	if n == 0 {
		return report
	}

	bps := make([]float64, 0, n)
	lats := make([]float64, 0, n)
	var sumBps, sumLat, totalCost float64

	for _, r := range records {
		bps = append(bps, r.SlippageBps)
		sumBps += r.SlippageBps
		if r.LatencyMs > 0 {
			lats = append(lats, float64(r.LatencyMs))
			sumLat += float64(r.LatencyMs)
		}
		if r.IntendedPrice > 0 {
			totalCost += math.Abs(r.ExecutedPrice-r.IntendedPrice) * r.Quantity
		}
	}

	report.AvgSlippageBps = roundFloat(sumBps/float64(n), 4)
	sort.Float64s(bps)
	report.P95SlippageBps = roundFloat(percentile(bps, 95), 4)
	report.TotalSlippageCost = roundFloat(totalCost, 4)

	if len(lats) > 0 {
		report.AvgLatencyMs = roundFloat(sumLat/float64(len(lats)), 2)
		sort.Float64s(lats)
		report.P95LatencyMs = roundFloat(percentile(lats, 95), 2)
	}

	return report
}

// HandleGetExecutionQuality GET /tool/execution/quality?source=scalp&days=7
func HandleGetExecutionQuality(c context.Context, ctx *app.RequestContext) {
	source := strings.TrimSpace(string(ctx.Query("source")))
	days, _ := strconv.Atoi(strings.TrimSpace(string(ctx.Query("days"))))

	reports, err := GetExecutionQualityReport(source, days)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, utils.H{"error": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, utils.H{"data": reports})
}
