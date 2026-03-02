package api

import (
	"context"
	"math"
	"net/http"
	"sync"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/utils"
)

// DataQualityMetrics 单个交易对的数据质量指标
type DataQualityMetrics struct {
	Symbol         string    `json:"symbol"`
	MissedTicks    int64     `json:"missedTicks"`    // 超时未收到数据次数
	JumpCount      int64     `json:"jumpCount"`      // 价格跳点次数
	AvgLatencyMs   float64   `json:"avgLatencyMs"`   // 平均接收延迟
	MaxLatencyMs   float64   `json:"maxLatencyMs"`   // 最大延迟
	ClockDriftMs   float64   `json:"clockDriftMs"`   // 最近一次时钟偏差
	HealthScore    float64   `json:"healthScore"`    // 健康评分 0-100
	LastUpdateTime time.Time `json:"lastUpdateTime"`
	TotalTicks     int64     `json:"totalTicks"`
}

type dataQualityTracker struct {
	mu        sync.RWMutex
	metrics   map[string]*DataQualityMetrics
	prevPrice map[string]float64
	prevTime  map[string]time.Time
	latencies map[string][]float64 // 最近 200 个延迟样本
}

const (
	dqJumpThresholdPct = 0.03  // 3% 价格跳变视为异常
	dqTickTimeoutSec   = 10    // 10秒未收到数据视为缺失
	dqMaxLatencySamples = 200
)

var dqTracker *dataQualityTracker

// InitDataQualityTracker 初始化数据质量追踪器
func InitDataQualityTracker() {
	dqTracker = &dataQualityTracker{
		metrics:   make(map[string]*DataQualityMetrics),
		prevPrice: make(map[string]float64),
		prevTime:  make(map[string]time.Time),
		latencies: make(map[string][]float64),
	}

	// 后台检查 missed ticks
	go dqTracker.missedTickChecker()
}

// RecordTick 记录一次价格数据到达
func RecordTick(symbol string, price float64, exchangeTimeMs int64) {
	if dqTracker == nil || symbol == "" || price <= 0 {
		return
	}

	now := time.Now()
	latencyMs := float64(now.UnixMilli() - exchangeTimeMs)

	dqTracker.mu.Lock()
	defer dqTracker.mu.Unlock()

	m, ok := dqTracker.metrics[symbol]
	if !ok {
		m = &DataQualityMetrics{Symbol: symbol, HealthScore: 100}
		dqTracker.metrics[symbol] = m
	}

	m.TotalTicks++
	m.LastUpdateTime = now
	m.ClockDriftMs = latencyMs

	// 延迟统计
	if latencyMs >= 0 {
		lats := dqTracker.latencies[symbol]
		if len(lats) >= dqMaxLatencySamples {
			lats = lats[1:]
		}
		lats = append(lats, latencyMs)
		dqTracker.latencies[symbol] = lats

		// 计算平均和最大
		var sum float64
		m.MaxLatencyMs = 0
		for _, l := range lats {
			sum += l
			if l > m.MaxLatencyMs {
				m.MaxLatencyMs = l
			}
		}
		m.AvgLatencyMs = sum / float64(len(lats))
	}

	// 价格跳点检测
	prev, hasPrev := dqTracker.prevPrice[symbol]
	if hasPrev && prev > 0 {
		changePct := math.Abs(price-prev) / prev
		if changePct >= dqJumpThresholdPct {
			m.JumpCount++
		}
	}
	dqTracker.prevPrice[symbol] = price
	dqTracker.prevTime[symbol] = now

	// 计算健康评分
	m.HealthScore = calcHealthScore(m)
}

func calcHealthScore(m *DataQualityMetrics) float64 {
	score := 100.0

	// 延迟惩罚：>500ms 扣分
	if m.AvgLatencyMs > 500 {
		score -= math.Min((m.AvgLatencyMs-500)/100, 30)
	}

	// 跳点惩罚
	if m.TotalTicks > 0 {
		jumpRate := float64(m.JumpCount) / float64(m.TotalTicks)
		score -= jumpRate * 1000 // 1% jump rate = -10 分
	}

	// 缺失惩罚
	if m.TotalTicks > 0 {
		missRate := float64(m.MissedTicks) / float64(m.TotalTicks+m.MissedTicks)
		score -= missRate * 500
	}

	if score < 0 {
		score = 0
	}
	return math.Round(score*100) / 100
}

func (t *dataQualityTracker) missedTickChecker() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		t.mu.Lock()
		now := time.Now()
		for symbol, prevT := range t.prevTime {
			if now.Sub(prevT) > time.Duration(dqTickTimeoutSec)*time.Second {
				if m, ok := t.metrics[symbol]; ok {
					m.MissedTicks++
					m.HealthScore = calcHealthScore(m)
				}
			}
		}
		t.mu.Unlock()
	}
}

// GetDataQualityMetrics 获取所有交易对的数据质量指标
func GetDataQualityMetrics() map[string]*DataQualityMetrics {
	if dqTracker == nil {
		return nil
	}
	dqTracker.mu.RLock()
	defer dqTracker.mu.RUnlock()

	result := make(map[string]*DataQualityMetrics, len(dqTracker.metrics))
	for k, v := range dqTracker.metrics {
		clone := *v
		result[k] = &clone
	}
	return result
}

// HandleGetDataQuality GET /tool/data-quality
func HandleGetDataQuality(c context.Context, ctx *app.RequestContext) {
	metrics := GetDataQualityMetrics()
	ctx.JSON(http.StatusOK, utils.H{"data": metrics})
}
