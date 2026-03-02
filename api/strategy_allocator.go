package api

import (
	"context"
	"log"
	"math"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/utils"
	"gorm.io/gorm"
)

// AllocatorConfig 策略资金分配器配置
type AllocatorConfig struct {
	TotalBudgetUSDT  float64 `json:"totalBudgetUSDT"`
	MaxDrawdownPct   float64 `json:"maxDrawdownPct"`    // 单策略回撤上限
	TurnoverPenaltyK float64 `json:"turnoverPenaltyK"`  // 换手惩罚系数
	MinWeight        float64 `json:"minWeight"`          // 最低权重
	RebalanceIntervalMin int `json:"rebalanceIntervalMin"`
}

// StrategyAllocation 策略资金分配记录
type StrategyAllocation struct {
	gorm.Model
	StrategyType    string  `gorm:"type:varchar(40);index" json:"strategyType"`
	Weight          float64 `gorm:"type:numeric(8,4)" json:"weight"`
	AllocatedUSDT   float64 `gorm:"type:numeric(18,4)" json:"allocatedUSDT"`
	Sharpe          float64 `gorm:"type:numeric(8,4)" json:"sharpe"`
	MaxDrawdownPct  float64 `gorm:"type:numeric(8,4)" json:"maxDrawdownPct"`
	TurnoverPenalty float64 `gorm:"type:numeric(8,4)" json:"turnoverPenalty"`
}

var (
	allocMu     sync.RWMutex
	allocCfg    AllocatorConfig
	allocLatest []StrategyAllocation
	allocStopCh chan struct{}
	allocActive bool
)

// StartAllocator 启动策略资金分配器
func StartAllocator(cfg AllocatorConfig) error {
	allocMu.Lock()
	defer allocMu.Unlock()

	if allocActive {
		return nil
	}
	if cfg.TotalBudgetUSDT <= 0 {
		cfg.TotalBudgetUSDT = 100
	}
	if cfg.RebalanceIntervalMin <= 0 {
		cfg.RebalanceIntervalMin = 60
	}
	if cfg.MinWeight <= 0 {
		cfg.MinWeight = 0.05
	}

	allocCfg = cfg
	allocActive = true
	allocStopCh = make(chan struct{})

	go allocRebalanceLoop()
	log.Printf("[Allocator] Started: budget=%.0f, interval=%dm", cfg.TotalBudgetUSDT, cfg.RebalanceIntervalMin)
	return nil
}

// StopAllocator 停止分配器
func StopAllocator() {
	allocMu.Lock()
	defer allocMu.Unlock()
	if !allocActive {
		return
	}
	allocActive = false
	close(allocStopCh)
	log.Println("[Allocator] Stopped")
}

func allocRebalanceLoop() {
	allocMu.RLock()
	interval := time.Duration(allocCfg.RebalanceIntervalMin) * time.Minute
	allocMu.RUnlock()

	// 立即执行一次
	rebalanceWeights()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-allocStopCh:
			return
		case <-ticker.C:
			rebalanceWeights()
		}
	}
}

func rebalanceWeights() {
	allocMu.RLock()
	cfg := allocCfg
	allocMu.RUnlock()

	strategies := []string{"scalp", "signal", "doji", "grid", "dca", "autoscale", "funding"}

	type stratScore struct {
		name   string
		sharpe float64
		mdd    float64
		score  float64
	}

	var scores []stratScore
	for _, st := range strategies {
		sharpe := calcStrategySharpe(st, 30)
		mdd := calcStrategyMaxDD(st, 30)
		// 惩罚高回撤
		score := sharpe
		if cfg.MaxDrawdownPct > 0 && mdd > cfg.MaxDrawdownPct {
			score *= 0.5
		}
		scores = append(scores, stratScore{name: st, sharpe: sharpe, mdd: mdd, score: score})
	}

	// Regime 调整
	regime, _, _ := GetCurrentRegime()
	for i := range scores {
		switch regime {
		case RegimeTrend:
			if scores[i].name == "grid" || scores[i].name == "dca" {
				scores[i].score *= 0.7 // 趋势市降低震荡策略
			}
		case RegimeRange:
			if scores[i].name == "scalp" || scores[i].name == "signal" {
				scores[i].score *= 0.7 // 震荡市降低趋势策略
			}
		case RegimeHighVol:
			scores[i].score *= 0.8 // 高波动全局降权
		}
	}

	// 归一化
	var totalScore float64
	for _, s := range scores {
		if s.score > 0 {
			totalScore += s.score
		}
	}

	var allocs []StrategyAllocation
	for _, s := range scores {
		weight := cfg.MinWeight
		if totalScore > 0 && s.score > 0 {
			weight = math.Max(s.score/totalScore, cfg.MinWeight)
		}
		allocs = append(allocs, StrategyAllocation{
			StrategyType:   s.name,
			Weight:         roundFloat(weight, 4),
			AllocatedUSDT:  roundFloat(weight*cfg.TotalBudgetUSDT, 2),
			Sharpe:         roundFloat(s.sharpe, 4),
			MaxDrawdownPct: roundFloat(s.mdd, 4),
		})
	}

	sort.Slice(allocs, func(i, j int) bool { return allocs[i].Weight > allocs[j].Weight })

	allocMu.Lock()
	allocLatest = allocs
	allocMu.Unlock()

	// 持久化
	if DB != nil {
		for _, a := range allocs {
			DB.Create(&a)
		}
	}
}

func calcStrategySharpe(strategyType string, days int) float64 {
	if DB == nil {
		return 0
	}
	since := time.Now().AddDate(0, 0, -days)
	var records []TradeRecord
	DB.Where("source = ? AND status = 'CLOSED' AND closed_at >= ?", "strategy_"+strategyType, since).
		Find(&records)

	if len(records) < 5 {
		return 0
	}

	var pnls []float64
	for _, r := range records {
		pnls = append(pnls, r.RealizedPnl)
	}

	mean, std := meanStd(pnls)
	if std == 0 {
		return 0
	}
	return mean / std * math.Sqrt(252) // 年化
}

func calcStrategyMaxDD(strategyType string, days int) float64 {
	if DB == nil {
		return 0
	}
	since := time.Now().AddDate(0, 0, -days)
	var records []TradeRecord
	DB.Where("source = ? AND status = 'CLOSED' AND closed_at >= ?", "strategy_"+strategyType, since).
		Order("closed_at ASC").Find(&records)

	if len(records) == 0 {
		return 0
	}

	var cumPnl, peak, maxDD float64
	for _, r := range records {
		cumPnl += r.RealizedPnl
		if cumPnl > peak {
			peak = cumPnl
		}
		dd := peak - cumPnl
		if dd > maxDD {
			maxDD = dd
		}
	}

	if peak == 0 {
		return 0
	}
	return maxDD / math.Max(math.Abs(peak), 1) * 100
}

func meanStd(data []float64) (float64, float64) {
	if len(data) == 0 {
		return 0, 0
	}
	var sum float64
	for _, v := range data {
		sum += v
	}
	mean := sum / float64(len(data))
	var variance float64
	for _, v := range data {
		variance += (v - mean) * (v - mean)
	}
	variance /= float64(len(data))
	return mean, math.Sqrt(variance)
}

// GetAllocationStatus 获取当前分配状态
func GetAllocationStatus() []StrategyAllocation {
	allocMu.RLock()
	defer allocMu.RUnlock()
	result := make([]StrategyAllocation, len(allocLatest))
	copy(result, allocLatest)
	return result
}

// HandleStartAllocator POST /tool/allocator/start
func HandleStartAllocator(c context.Context, ctx *app.RequestContext) {
	var cfg AllocatorConfig
	if err := ctx.BindJSON(&cfg); err != nil {
		ctx.JSON(http.StatusBadRequest, utils.H{"error": err.Error()})
		return
	}
	if err := StartAllocator(cfg); err != nil {
		ctx.JSON(http.StatusInternalServerError, utils.H{"error": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, utils.H{"message": "allocator started"})
}

// HandleStopAllocator POST /tool/allocator/stop
func HandleStopAllocator(c context.Context, ctx *app.RequestContext) {
	StopAllocator()
	ctx.JSON(http.StatusOK, utils.H{"message": "allocator stopped"})
}

// HandleGetAllocation GET /tool/allocator/status
func HandleGetAllocation(c context.Context, ctx *app.RequestContext) {
	allocMu.RLock()
	active := allocActive
	allocMu.RUnlock()
	ctx.JSON(http.StatusOK, utils.H{"data": map[string]interface{}{
		"active":      active,
		"allocations": GetAllocationStatus(),
	}})
}
