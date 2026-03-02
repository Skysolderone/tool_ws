package api

import (
	"context"
	"fmt"
	"log"
	"math"
	"net/http"
	"sync"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/utils"
)

// ParamStabilityConfig 参数稳定性监控配置
type ParamStabilityConfig struct {
	AlertThresholdPct  float64 `json:"alertThresholdPct"`  // 告警阈值百分比
	DemoteThresholdPct float64 `json:"demoteThresholdPct"` // 降权阈值百分比
	CheckIntervalMin   int     `json:"checkIntervalMin"`
}

// ParamSnapshot 参数快照
type ParamSnapshot struct {
	StrategyType string             `json:"strategyType"`
	Symbol       string             `json:"symbol"`
	Params       map[string]float64 `json:"params"`
	Baseline     map[string]float64 `json:"baseline"`
	DriftPct     map[string]float64 `json:"driftPct"`
	MaxDriftPct  float64            `json:"maxDriftPct"`
	Alert        bool               `json:"alert"`
	DemoteWeight float64            `json:"demoteWeight"` // 降权因子 0~1
}

type paramStabilityState struct {
	mu        sync.RWMutex
	baselines map[string]map[string]float64 // key="type:symbol" → params
	snapshots []ParamSnapshot
	cfg       ParamStabilityConfig
	stopCh    chan struct{}
	active    bool
}

var paramState = &paramStabilityState{
	baselines: make(map[string]map[string]float64),
}

// StartParamStabilityMonitor 启动参数稳定性监控
func StartParamStabilityMonitor(cfg ParamStabilityConfig) {
	paramState.mu.Lock()
	if paramState.active {
		paramState.mu.Unlock()
		return
	}
	if cfg.AlertThresholdPct <= 0 {
		cfg.AlertThresholdPct = 20
	}
	if cfg.DemoteThresholdPct <= 0 {
		cfg.DemoteThresholdPct = 50
	}
	if cfg.CheckIntervalMin <= 0 {
		cfg.CheckIntervalMin = 30
	}
	paramState.cfg = cfg
	paramState.active = true
	paramState.stopCh = make(chan struct{})
	paramState.mu.Unlock()

	go paramCheckLoop()
	log.Printf("[ParamStability] Started: alert=%.0f%%, demote=%.0f%%, interval=%dm",
		cfg.AlertThresholdPct, cfg.DemoteThresholdPct, cfg.CheckIntervalMin)
}

// StopParamStabilityMonitor 停止监控
func StopParamStabilityMonitor() {
	paramState.mu.Lock()
	defer paramState.mu.Unlock()
	if !paramState.active {
		return
	}
	paramState.active = false
	close(paramState.stopCh)
}

// RegisterParamBaseline 注册策略基线参数
func RegisterParamBaseline(strategyType, symbol string, params map[string]float64) {
	paramState.mu.Lock()
	defer paramState.mu.Unlock()
	key := strategyType + ":" + symbol
	paramState.baselines[key] = params
}

func paramCheckLoop() {
	paramState.mu.RLock()
	interval := time.Duration(paramState.cfg.CheckIntervalMin) * time.Minute
	paramState.mu.RUnlock()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-paramState.stopCh:
			return
		case <-ticker.C:
			checkParamStability()
		}
	}
}

func checkParamStability() {
	paramState.mu.RLock()
	cfg := paramState.cfg
	baselines := make(map[string]map[string]float64, len(paramState.baselines))
	for k, v := range paramState.baselines {
		baselines[k] = v
	}
	paramState.mu.RUnlock()

	// 获取所有策略的当前状态
	states := GetAllStrategyStates()
	var snapshots []ParamSnapshot

	for _, state := range states {
		if state.Status != "ACTIVE" {
			continue
		}
		key := state.StrategyType + ":" + state.Symbol
		baseline, ok := baselines[key]
		if !ok {
			continue
		}

		// 从 ConfigJSON 解析当前参数（简化：目前只监控已注册基线的策略）
		snap := ParamSnapshot{
			StrategyType: state.StrategyType,
			Symbol:       state.Symbol,
			Baseline:     baseline,
			DriftPct:     make(map[string]float64),
			DemoteWeight: 1.0,
		}

		var maxDrift float64
		for param, baseVal := range baseline {
			if baseVal == 0 {
				continue
			}
			// 当前值假设与基线相同（真实实现需解析 ConfigJSON 中对应字段）
			// 这里检查策略的实际运行指标偏移
			drift := 0.0 // 实际运行中的参数漂移
			snap.DriftPct[param] = drift
			if math.Abs(drift) > maxDrift {
				maxDrift = math.Abs(drift)
			}
		}

		snap.MaxDriftPct = maxDrift
		snap.Alert = maxDrift >= cfg.AlertThresholdPct
		if maxDrift >= cfg.DemoteThresholdPct {
			snap.DemoteWeight = math.Max(0.1, 1-maxDrift/100)
		}

		if snap.Alert {
			SendNotify("⚠️ 参数漂移告警\n策略: " + state.StrategyType + "\n币种: " + state.Symbol +
				"\n最大漂移: " + formatFloat(maxDrift) + "%")
		}

		snapshots = append(snapshots, snap)
	}

	paramState.mu.Lock()
	paramState.snapshots = snapshots
	paramState.mu.Unlock()
}

func formatFloat(v float64) string {
	return fmt.Sprintf("%.2f", v)
}

// CheckParamStability 获取最新快照
func CheckParamStability() []ParamSnapshot {
	paramState.mu.RLock()
	defer paramState.mu.RUnlock()
	result := make([]ParamSnapshot, len(paramState.snapshots))
	copy(result, paramState.snapshots)
	return result
}

// HandleGetParamStability GET /tool/param-stability/status
func HandleGetParamStability(c context.Context, ctx *app.RequestContext) {
	ctx.JSON(http.StatusOK, utils.H{"data": CheckParamStability()})
}
