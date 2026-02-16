package api

import (
	"context"
	"fmt"
	"log"
	"math"
	"strconv"
	"sync"
	"time"

	"github.com/adshao/go-binance/v2/futures"
)

// AutoScaleConfig 浮盈加仓配置
type AutoScaleConfig struct {
	Symbol       string                   `json:"symbol"`
	Side         futures.SideType         `json:"side"`                   // BUY / SELL（原始开仓方向）
	PositionSide futures.PositionSideType `json:"positionSide,omitempty"` // BOTH / LONG / SHORT
	Leverage     int                      `json:"leverage"`               // 杠杆倍数

	// 触发条件（二选一）
	TriggerAmount  float64 `json:"triggerAmount,omitempty"`  // 浮盈达到 X USDT 时触发加仓
	TriggerPercent float64 `json:"triggerPercent,omitempty"` // 浮盈达到持仓成本 X% 时触发加仓

	// 加仓参数
	AddQuantity   string `json:"addQuantity"`   // 每次加仓的 USDT 金额
	MaxScaleCount int    `json:"maxScaleCount"` // 最大加仓次数

	// 止盈止损（可选）
	UpdateTPSL     bool    `json:"updateTPSL,omitempty"`     // 加仓后是否重新计算 TP/SL
	StopLossAmount float64 `json:"stopLossAmount,omitempty"` // 止损金额(USDT)，updateTPSL=true 时使用
	RiskReward     float64 `json:"riskReward,omitempty"`     // 盈亏比，updateTPSL=true 时使用
}

// AutoScaleStatus 返回给用户的加仓任务状态（不含内部 channel）
type AutoScaleStatus struct {
	Config     AutoScaleConfig `json:"config"`
	ScaleCount int             `json:"scaleCount"` // 已加仓次数
	TotalAdded float64         `json:"totalAdded"` // 已加仓总 USDT 金额
	Active     bool            `json:"active"`     // 是否正在运行
	LastAlgoTP int64           `json:"lastAlgoTP"` // 最新止盈 AlgoID
	LastAlgoSL int64           `json:"lastAlgoSL"` // 最新止损 AlgoID
}

// autoScaleState 运行中的加仓任务内部状态
type autoScaleState struct {
	Config     AutoScaleConfig
	ScaleCount int
	TotalAdded float64
	Active     bool
	LastAlgoTP int64
	LastAlgoSL int64
	stopC      chan struct{}
}

var (
	autoScaleTasks = make(map[string]*autoScaleState)
	autoScaleMu    sync.Mutex
)

// StartAutoScale 启动浮盈加仓监控
func StartAutoScale(config AutoScaleConfig) error {
	// 参数校验
	if config.Symbol == "" {
		return fmt.Errorf("symbol is required")
	}
	if config.Side == "" {
		return fmt.Errorf("side is required")
	}
	if config.AddQuantity == "" {
		return fmt.Errorf("addQuantity is required")
	}
	if config.MaxScaleCount <= 0 {
		return fmt.Errorf("maxScaleCount must be > 0")
	}
	if config.Leverage <= 0 {
		return fmt.Errorf("leverage must be > 0")
	}
	if config.TriggerAmount <= 0 && config.TriggerPercent <= 0 {
		return fmt.Errorf("triggerAmount or triggerPercent is required")
	}
	if config.TriggerAmount > 0 && config.TriggerPercent > 0 {
		return fmt.Errorf("triggerAmount and triggerPercent cannot be set at the same time")
	}
	if config.UpdateTPSL {
		if config.StopLossAmount <= 0 {
			return fmt.Errorf("stopLossAmount is required when updateTPSL is true")
		}
		if config.RiskReward <= 0 {
			return fmt.Errorf("riskReward is required when updateTPSL is true")
		}
	}

	autoScaleMu.Lock()
	defer autoScaleMu.Unlock()

	// 检查是否已有该交易对的监控任务
	if existing, ok := autoScaleTasks[config.Symbol]; ok && existing.Active {
		return fmt.Errorf("auto scale already running for %s, stop it first", config.Symbol)
	}

	state := &autoScaleState{
		Config: config,
		Active: true,
		stopC:  make(chan struct{}),
	}
	autoScaleTasks[config.Symbol] = state

	go monitorAndScale(state)

	log.Printf("[AutoScale] Started for %s: side=%s, addQty=%s USDT, maxCount=%d, trigger(amount=%.2f, percent=%.2f%%)",
		config.Symbol, config.Side, config.AddQuantity, config.MaxScaleCount,
		config.TriggerAmount, config.TriggerPercent)

	return nil
}

// StopAutoScale 停止浮盈加仓监控
func StopAutoScale(symbol string) error {
	autoScaleMu.Lock()
	defer autoScaleMu.Unlock()

	state, ok := autoScaleTasks[symbol]
	if !ok || !state.Active {
		return fmt.Errorf("no active auto scale task for %s", symbol)
	}

	close(state.stopC)
	state.Active = false
	log.Printf("[AutoScale] Stopped for %s (scaled %d times, total %.2f USDT added)",
		symbol, state.ScaleCount, state.TotalAdded)

	return nil
}

// GetAutoScaleStatus 获取浮盈加仓任务状态
func GetAutoScaleStatus(symbol string) *AutoScaleStatus {
	autoScaleMu.Lock()
	defer autoScaleMu.Unlock()

	state, ok := autoScaleTasks[symbol]
	if !ok {
		return nil
	}

	return &AutoScaleStatus{
		Config:     state.Config,
		ScaleCount: state.ScaleCount,
		TotalAdded: state.TotalAdded,
		Active:     state.Active,
		LastAlgoTP: state.LastAlgoTP,
		LastAlgoSL: state.LastAlgoSL,
	}
}

// monitorAndScale 后台监控协程：定时检查浮盈，达到阈值时自动加仓
func monitorAndScale(state *autoScaleState) {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	cfg := state.Config
	ctx := context.Background()

	log.Printf("[AutoScale] Monitor started for %s", cfg.Symbol)

	for {
		select {
		case <-state.stopC:
			log.Printf("[AutoScale] Monitor stopped for %s", cfg.Symbol)
			return
		case <-ticker.C:
			// 检查是否已达到最大加仓次数
			if state.ScaleCount >= cfg.MaxScaleCount {
				log.Printf("[AutoScale] Max scale count (%d) reached for %s, stopping monitor",
					cfg.MaxScaleCount, cfg.Symbol)
				autoScaleMu.Lock()
				state.Active = false
				autoScaleMu.Unlock()
				return
			}

			// 查询当前持仓
			position, err := findPosition(ctx, cfg.Symbol, cfg.PositionSide)
			if err != nil {
				log.Printf("[AutoScale] Error finding position for %s: %v", cfg.Symbol, err)
				continue
			}

			posAmt, _ := strconv.ParseFloat(position.PositionAmt, 64)
			if posAmt == 0 {
				// 仓位已经平了，停止监控
				log.Printf("[AutoScale] Position closed for %s, stopping monitor", cfg.Symbol)
				autoScaleMu.Lock()
				state.Active = false
				autoScaleMu.Unlock()
				return
			}

			// 获取浮盈
			unrealizedProfit, _ := strconv.ParseFloat(position.UnRealizedProfit, 64)

			// 判断是否触发加仓
			shouldScale := false
			if cfg.TriggerAmount > 0 {
				// 金额模式：浮盈 >= triggerAmount × (已加仓次数+1)
				threshold := cfg.TriggerAmount * float64(state.ScaleCount+1)
				if unrealizedProfit >= threshold {
					shouldScale = true
					log.Printf("[AutoScale] %s trigger: profit=%.4f USDT >= threshold=%.4f (amount mode, count=%d)",
						cfg.Symbol, unrealizedProfit, threshold, state.ScaleCount)
				}
			} else if cfg.TriggerPercent > 0 {
				// 百分比模式：浮盈百分比 >= triggerPercent × (已加仓次数+1)
				entryPrice, _ := strconv.ParseFloat(position.EntryPrice, 64)
				if entryPrice > 0 {
					// 持仓成本 = |posAmt| × entryPrice
					positionCost := math.Abs(posAmt) * entryPrice
					if positionCost > 0 {
						profitPercent := (unrealizedProfit / positionCost) * 100
						threshold := cfg.TriggerPercent * float64(state.ScaleCount+1)
						if profitPercent >= threshold {
							shouldScale = true
							log.Printf("[AutoScale] %s trigger: profitPct=%.2f%% >= threshold=%.2f%% (percent mode, count=%d)",
								cfg.Symbol, profitPercent, threshold, state.ScaleCount)
						}
					}
				}
			}

			if !shouldScale {
				continue
			}

			// 执行加仓
			err = executeScaleIn(ctx, state)
			if err != nil {
				log.Printf("[AutoScale] Error scaling in for %s: %v", cfg.Symbol, err)
				continue
			}
		}
	}
}

// executeScaleIn 执行一次加仓操作
func executeScaleIn(ctx context.Context, state *autoScaleState) error {
	cfg := state.Config

	log.Printf("[AutoScale] Executing scale-in #%d for %s: %s USDT",
		state.ScaleCount+1, cfg.Symbol, cfg.AddQuantity)

	// 构建加仓请求（不带止盈止损，TP/SL单独处理）
	scaleReq := PlaceOrderReq{
		Symbol:        cfg.Symbol,
		Side:          cfg.Side,
		OrderType:     futures.OrderTypeMarket,
		PositionSide:  cfg.PositionSide,
		QuoteQuantity: cfg.AddQuantity,
		Leverage:      cfg.Leverage,
	}

	result, err := PlaceOrderViaWs(ctx, scaleReq)
	if err != nil {
		return fmt.Errorf("scale-in order failed: %w", err)
	}

	// 更新状态
	autoScaleMu.Lock()
	addQty, _ := strconv.ParseFloat(cfg.AddQuantity, 64)
	state.ScaleCount++
	state.TotalAdded += addQty
	autoScaleMu.Unlock()

	log.Printf("[AutoScale] Scale-in #%d success for %s: orderId=%d, total scaled=%d/%d",
		state.ScaleCount, cfg.Symbol, result.Order.OrderID, state.ScaleCount, cfg.MaxScaleCount)

	// 如果需要更新止盈止损
	if cfg.UpdateTPSL {
		err = updateTPSLAfterScale(ctx, state)
		if err != nil {
			log.Printf("[AutoScale] Warning: failed to update TP/SL after scale-in: %v", err)
			// 加仓已成功，TP/SL更新失败不影响
		}
	}

	return nil
}

// updateTPSLAfterScale 加仓后更新止盈止损
func updateTPSLAfterScale(ctx context.Context, state *autoScaleState) error {
	cfg := state.Config

	// 1. 撤销旧的止盈止损 algo 单
	if state.LastAlgoTP > 0 {
		log.Printf("[AutoScale] Cancelling old TP algo order %d", state.LastAlgoTP)
		if err := CancelAlgoOrder(ctx, cfg.Symbol, state.LastAlgoTP); err != nil {
			log.Printf("[AutoScale] Warning: cancel old TP failed: %v", err)
		}
	}
	if state.LastAlgoSL > 0 {
		log.Printf("[AutoScale] Cancelling old SL algo order %d", state.LastAlgoSL)
		if err := CancelAlgoOrder(ctx, cfg.Symbol, state.LastAlgoSL); err != nil {
			log.Printf("[AutoScale] Warning: cancel old SL failed: %v", err)
		}
	}

	// 2. 查询最新持仓，获取新均价和总数量
	position, err := findPosition(ctx, cfg.Symbol, cfg.PositionSide)
	if err != nil {
		return fmt.Errorf("find position: %w", err)
	}

	entryPrice, _ := strconv.ParseFloat(position.EntryPrice, 64)
	posAmt := math.Abs(mustParseFloat(position.PositionAmt))
	if entryPrice == 0 || posAmt == 0 {
		return fmt.Errorf("invalid position data: entry=%.4f, amt=%.4f", entryPrice, posAmt)
	}

	// 获取精度
	precision, stepSize, err := getSymbolPrecision(ctx, cfg.Symbol)
	if err != nil {
		return fmt.Errorf("get symbol precision: %w", err)
	}
	posAmt = roundToStepSize(posAmt, stepSize)
	quantity := formatQuantity(posAmt, precision)

	// 3. 使用新的均价和总仓位重新挂止盈止损
	tpslReq := PlaceOrderReq{
		Symbol:         cfg.Symbol,
		Side:           cfg.Side,
		PositionSide:   cfg.PositionSide,
		StopLossAmount: cfg.StopLossAmount,
		RiskReward:     cfg.RiskReward,
	}

	tp, sl, err := PlaceTPSLOrders(ctx, tpslReq, entryPrice, quantity)
	if err != nil {
		return fmt.Errorf("place new TP/SL: %w", err)
	}

	// 4. 保存新的 AlgoID
	autoScaleMu.Lock()
	state.LastAlgoTP = tp.AlgoID
	state.LastAlgoSL = sl.AlgoID
	autoScaleMu.Unlock()

	log.Printf("[AutoScale] Updated TP/SL for %s: TP algoId=%d (trigger=%s), SL algoId=%d (trigger=%s)",
		cfg.Symbol, tp.AlgoID, tp.TriggerPrice, sl.AlgoID, sl.TriggerPrice)

	return nil
}

func mustParseFloat(s string) float64 {
	f, _ := strconv.ParseFloat(s, 64)
	return f
}
