package api

import (
	"context"
	"math"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/utils"
)

// OpsMetrics 运营链路指标
type OpsMetrics struct {
	OrderTotal     int64   `json:"orderTotal"`
	OrderSuccess   int64   `json:"orderSuccess"`
	OrderFailed    int64   `json:"orderFailed"`
	SuccessRate    float64 `json:"successRate"`

	AvgLatencyMs float64 `json:"avgLatencyMs"`
	P50LatencyMs float64 `json:"p50LatencyMs"`
	P95LatencyMs float64 `json:"p95LatencyMs"`
	P99LatencyMs float64 `json:"p99LatencyMs"`

	RiskTriggerCount int64 `json:"riskTriggerCount"`
	KillSwitchCount  int64 `json:"killSwitchCount"`

	WindowStart time.Time `json:"windowStart"`
}

type opsMetricsState struct {
	mu sync.Mutex

	orderTotal   int64
	orderSuccess int64
	orderFailed  int64

	riskTriggers    int64
	killSwitchFires int64

	latencies   []float64 // 滑动窗口，最近 1000 条
	windowStart time.Time
}

const opsMaxLatencies = 1000

var ops *opsMetricsState

// InitOpsMetrics 初始化运营指标收集器
func InitOpsMetrics() {
	ops = &opsMetricsState{
		latencies:   make([]float64, 0, opsMaxLatencies),
		windowStart: time.Now(),
	}
}

// RecordOrderMetric 记录一笔下单结果
func RecordOrderMetric(success bool, latencyMs int64) {
	if ops == nil {
		return
	}
	ops.mu.Lock()
	defer ops.mu.Unlock()

	ops.orderTotal++
	if success {
		ops.orderSuccess++
	} else {
		ops.orderFailed++
	}

	if latencyMs > 0 {
		if len(ops.latencies) >= opsMaxLatencies {
			ops.latencies = ops.latencies[1:]
		}
		ops.latencies = append(ops.latencies, float64(latencyMs))
	}
}

// RecordRiskTrigger 记录一次风控/熔断触发
func RecordRiskTrigger(triggerType string) {
	if ops == nil {
		return
	}
	ops.mu.Lock()
	defer ops.mu.Unlock()

	switch triggerType {
	case "kill_switch":
		ops.killSwitchFires++
	default:
		ops.riskTriggers++
	}
}

// GetOpsMetrics 获取当前运营指标快照
func GetOpsMetrics() *OpsMetrics {
	if ops == nil {
		return &OpsMetrics{}
	}
	ops.mu.Lock()
	defer ops.mu.Unlock()

	m := &OpsMetrics{
		OrderTotal:       ops.orderTotal,
		OrderSuccess:     ops.orderSuccess,
		OrderFailed:      ops.orderFailed,
		RiskTriggerCount: ops.riskTriggers,
		KillSwitchCount:  ops.killSwitchFires,
		WindowStart:      ops.windowStart,
	}

	if m.OrderTotal > 0 {
		m.SuccessRate = math.Round(float64(m.OrderSuccess)/float64(m.OrderTotal)*10000) / 100
	}

	if len(ops.latencies) > 0 {
		sorted := make([]float64, len(ops.latencies))
		copy(sorted, ops.latencies)
		sort.Float64s(sorted)

		var sum float64
		for _, l := range sorted {
			sum += l
		}
		m.AvgLatencyMs = math.Round(sum/float64(len(sorted))*100) / 100
		m.P50LatencyMs = percentile(sorted, 50)
		m.P95LatencyMs = percentile(sorted, 95)
		m.P99LatencyMs = percentile(sorted, 99)
	}

	return m
}

// HandleGetOpsMetrics GET /tool/ops/metrics
func HandleGetOpsMetrics(c context.Context, ctx *app.RequestContext) {
	ctx.JSON(http.StatusOK, utils.H{"data": GetOpsMetrics()})
}
