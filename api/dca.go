package api

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"sync"
	"time"

	"github.com/adshao/go-binance/v2/futures"
)

// DCAConfig 定投(DCA)配置
type DCAConfig struct {
	Symbol       string                   `json:"symbol"`
	Side         futures.SideType         `json:"side"`                   // BUY / SELL
	PositionSide futures.PositionSideType `json:"positionSide,omitempty"` // LONG / SHORT
	Leverage     int                      `json:"leverage"`

	AmountPerOrder string `json:"amountPerOrder"` // 每次投入金额(USDT)
	TotalOrders    int    `json:"totalOrders"`    // 总投入次数
	IntervalSec    int    `json:"intervalSec"`    // 投入间隔(秒)

	// 价格条件（可选）
	PriceDropPercent float64 `json:"priceDropPercent,omitempty"` // 每次需价格下跌X%才触发（逢跌加仓）

	// 止盈止损（可选）
	StopLossAmount   float64 `json:"stopLossAmount,omitempty"`   // 总止损(USDT)
	TakeProfitAmount float64 `json:"takeProfitAmount,omitempty"` // 总止盈(USDT)
}

// DCAStatus 定投状态
type DCAStatus struct {
	Config      DCAConfig `json:"config"`
	Active      bool      `json:"active"`
	OrderCount  int       `json:"orderCount"`  // 已投入次数
	TotalAmount float64   `json:"totalAmount"` // 已投入总金额
	AvgEntry    float64   `json:"avgEntry"`    // 平均入场价
	CurrentPnl  float64   `json:"currentPnl"`  // 当前浮盈亏
	LastOrderAt string    `json:"lastOrderAt"` // 上次下单时间
	LastError   string    `json:"lastError"`   // 最近一次错误
	FailCount   int       `json:"failCount"`   // 连续失败次数
}

type dcaState struct {
	Config      DCAConfig
	Active      bool
	OrderCount  int
	TotalAmount float64
	AvgEntry    float64
	LastOrderAt time.Time
	LastPrice   float64 // 上次下单时的价格（用于 priceDropPercent）
	LastError   string  // 最近一次错误
	FailCount   int     // 连续失败次数
	stopC       chan struct{}
}

var (
	dcaTasks = make(map[string]*dcaState)
	dcaMu    sync.Mutex
)

// StartDCA 启动定投策略
func StartDCA(config DCAConfig) error {
	if config.Symbol == "" {
		return fmt.Errorf("symbol is required")
	}
	if config.Side == "" {
		return fmt.Errorf("side is required")
	}
	if config.AmountPerOrder == "" {
		return fmt.Errorf("amountPerOrder is required")
	}
	if config.TotalOrders <= 0 {
		return fmt.Errorf("totalOrders must be > 0")
	}
	if config.IntervalSec <= 0 {
		return fmt.Errorf("intervalSec must be > 0")
	}
	if config.Leverage <= 0 {
		return fmt.Errorf("leverage must be > 0")
	}

	// 自动推断 positionSide：双向持仓模式下必须是 LONG/SHORT
	if config.PositionSide == "" {
		if config.Side == futures.SideTypeBuy {
			config.PositionSide = futures.PositionSideTypeLong
		} else {
			config.PositionSide = futures.PositionSideTypeShort
		}
	}

	dcaMu.Lock()
	defer dcaMu.Unlock()

	if existing, ok := dcaTasks[config.Symbol]; ok && existing.Active {
		return fmt.Errorf("DCA already running for %s, stop it first", config.Symbol)
	}

	state := &dcaState{
		Config: config,
		Active: true,
		stopC:  make(chan struct{}),
	}
	dcaTasks[config.Symbol] = state

	go dcaLoop(state)

	log.Printf("[DCA] Started for %s: side=%s, positionSide=%s, amount=%s USDT, total=%d, interval=%ds",
		config.Symbol, config.Side, config.PositionSide, config.AmountPerOrder, config.TotalOrders, config.IntervalSec)

	return nil
}

// StopDCA 停止定投
func StopDCA(symbol string) error {
	dcaMu.Lock()
	defer dcaMu.Unlock()

	state, ok := dcaTasks[symbol]
	if !ok || !state.Active {
		return fmt.Errorf("no active DCA task for %s", symbol)
	}

	close(state.stopC)
	state.Active = false
	log.Printf("[DCA] Stopped for %s: orders=%d/%d, total=%.2f USDT, avgEntry=%.4f",
		symbol, state.OrderCount, state.Config.TotalOrders, state.TotalAmount, state.AvgEntry)

	return nil
}

// GetDCAStatus 获取定投状态
func GetDCAStatus(symbol string) *DCAStatus {
	dcaMu.Lock()
	defer dcaMu.Unlock()

	state, ok := dcaTasks[symbol]
	if !ok {
		return nil
	}

	// 获取当前浮盈
	var currentPnl float64
	ctx := context.Background()
	positions, err := Client.NewGetPositionRiskService().Symbol(symbol).Do(ctx)
	if err == nil {
		for _, pos := range positions {
			if futures.PositionSideType(pos.PositionSide) == state.Config.PositionSide {
				amt, _ := strconv.ParseFloat(pos.PositionAmt, 64)
				if amt != 0 {
					currentPnl, _ = strconv.ParseFloat(pos.UnRealizedProfit, 64)
					break
				}
			}
		}
	}

	lastOrderStr := ""
	if !state.LastOrderAt.IsZero() {
		lastOrderStr = state.LastOrderAt.Format("15:04:05")
	}

	return &DCAStatus{
		Config:      state.Config,
		Active:      state.Active,
		OrderCount:  state.OrderCount,
		TotalAmount: state.TotalAmount,
		AvgEntry:    state.AvgEntry,
		CurrentPnl:  currentPnl,
		LastOrderAt: lastOrderStr,
		LastError:   state.LastError,
		FailCount:   state.FailCount,
	}
}

// dcaLoop 定投主循环
func dcaLoop(state *dcaState) {
	cfg := state.Config
	ctx := context.Background()

	log.Printf("[DCA] Loop starting for %s (side=%s, positionSide=%s)", cfg.Symbol, cfg.Side, cfg.PositionSide)

	// 设置杠杆
	if _, err := ChangeLeverage(ctx, cfg.Symbol, cfg.Leverage); err != nil {
		log.Printf("[DCA] Warning: set leverage failed: %v", err)
	}

	// 立即执行第一次（带重试）
	if err := dcaExecuteWithRetry(ctx, state, 3); err != nil {
		log.Printf("[DCA] First order failed after retries: %v", err)
		// 不退出，继续等待下次 ticker
	}

	ticker := time.NewTicker(time.Duration(cfg.IntervalSec) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-state.stopC:
			log.Printf("[DCA] Loop stopped for %s", cfg.Symbol)
			return
		case <-ticker.C:
			if state.OrderCount >= cfg.TotalOrders {
				log.Printf("[DCA] All %d orders completed for %s", cfg.TotalOrders, cfg.Symbol)
				dcaMu.Lock()
				state.Active = false
				dcaMu.Unlock()
				return
			}

			// 止盈/止损检查
			if dcaCheckTPSL(ctx, state) {
				return
			}

			// 价格条件检查
			if cfg.PriceDropPercent > 0 && state.LastPrice > 0 {
				cache := GetPriceCache()
				currentPrice, err := cache.GetPrice(cfg.Symbol)
				if err != nil {
					log.Printf("[DCA] GetPrice failed for %s: %v", cfg.Symbol, err)
					continue
				}

				var dropPct float64
				if cfg.Side == futures.SideTypeBuy {
					dropPct = (state.LastPrice - currentPrice) / state.LastPrice * 100
				} else {
					dropPct = (currentPrice - state.LastPrice) / state.LastPrice * 100
				}

				if dropPct < cfg.PriceDropPercent {
					continue
				}
				log.Printf("[DCA] Price condition met: drop=%.2f%% >= threshold=%.2f%%", dropPct, cfg.PriceDropPercent)
			}

			if err := dcaExecuteWithRetry(ctx, state, 2); err != nil {
				log.Printf("[DCA] Order failed after retries: %v", err)
			}
		}
	}
}

// dcaExecuteWithRetry 带重试的定投执行
func dcaExecuteWithRetry(ctx context.Context, state *dcaState, maxRetries int) error {
	var lastErr error
	for i := 0; i < maxRetries; i++ {
		if i > 0 {
			log.Printf("[DCA] Retry #%d for %s", i, state.Config.Symbol)
			time.Sleep(time.Duration(i*2) * time.Second)
		}

		err := dcaExecute(ctx, state)
		if err == nil {
			// 成功，清除错误状态
			dcaMu.Lock()
			state.LastError = ""
			state.FailCount = 0
			dcaMu.Unlock()
			return nil
		}
		lastErr = err
	}

	// 全部重试失败
	dcaMu.Lock()
	state.LastError = lastErr.Error()
	state.FailCount++
	dcaMu.Unlock()

	// 连续失败 10 次，自动停止
	if state.FailCount >= 10 {
		log.Printf("[DCA] Too many failures (%d) for %s, auto stopping", state.FailCount, state.Config.Symbol)
		dcaMu.Lock()
		state.Active = false
		dcaMu.Unlock()
		select {
		case <-state.stopC:
		default:
			close(state.stopC)
		}
	}

	return lastErr
}

// dcaExecute 执行一次定投
func dcaExecute(ctx context.Context, state *dcaState) error {
	cfg := state.Config

	// 风控检查
	if err := CheckRisk(); err != nil {
		return fmt.Errorf("risk blocked: %w", err)
	}

	log.Printf("[DCA] Executing order #%d for %s: side=%s, positionSide=%s, amount=%s USDT",
		state.OrderCount+1, cfg.Symbol, cfg.Side, cfg.PositionSide, cfg.AmountPerOrder)

	req := PlaceOrderReq{
		Symbol:        cfg.Symbol,
		Side:          cfg.Side,
		OrderType:     futures.OrderTypeMarket,
		PositionSide:  cfg.PositionSide,
		QuoteQuantity: cfg.AmountPerOrder,
		Leverage:      cfg.Leverage,
	}

	result, err := PlaceOrderViaWs(ctx, req)
	if err != nil {
		return fmt.Errorf("order failed: %w", err)
	}

	// 获取成交价
	filledPrice, _ := strconv.ParseFloat(result.Order.AvgPrice, 64)
	amtPerOrder, _ := strconv.ParseFloat(cfg.AmountPerOrder, 64)

	dcaMu.Lock()
	state.OrderCount++
	state.TotalAmount += amtPerOrder
	state.LastOrderAt = time.Now()
	state.LastPrice = filledPrice

	if state.OrderCount == 1 {
		state.AvgEntry = filledPrice
	} else {
		state.AvgEntry = (state.AvgEntry*float64(state.OrderCount-1) + filledPrice) / float64(state.OrderCount)
	}
	dcaMu.Unlock()

	log.Printf("[DCA] Order #%d/%d for %s: price=%.4f, amount=%s USDT, avgEntry=%.4f",
		state.OrderCount, cfg.TotalOrders, cfg.Symbol, filledPrice, cfg.AmountPerOrder, state.AvgEntry)

	return nil
}

// dcaCheckTPSL 定投的止盈止损检查
func dcaCheckTPSL(ctx context.Context, state *dcaState) bool {
	cfg := state.Config

	if cfg.StopLossAmount <= 0 && cfg.TakeProfitAmount <= 0 {
		return false
	}

	positions, err := Client.NewGetPositionRiskService().Symbol(cfg.Symbol).Do(ctx)
	if err != nil {
		return false
	}

	for _, pos := range positions {
		amt, _ := strconv.ParseFloat(pos.PositionAmt, 64)
		if amt == 0 {
			continue
		}

		pnl, _ := strconv.ParseFloat(pos.UnRealizedProfit, 64)

		if cfg.StopLossAmount > 0 && pnl <= -cfg.StopLossAmount {
			log.Printf("[DCA] Stop loss triggered: PnL=%.2f <= -%.2f", pnl, cfg.StopLossAmount)
			dcaCloseAndStop(ctx, state)
			return true
		}

		if cfg.TakeProfitAmount > 0 && pnl >= cfg.TakeProfitAmount {
			log.Printf("[DCA] Take profit triggered: PnL=%.2f >= %.2f", pnl, cfg.TakeProfitAmount)
			dcaCloseAndStop(ctx, state)
			return true
		}
	}

	return false
}

// dcaCloseAndStop 平仓并停止DCA
func dcaCloseAndStop(ctx context.Context, state *dcaState) {
	cfg := state.Config

	_, err := ClosePositionViaWs(ctx, ClosePositionReq{
		Symbol:       cfg.Symbol,
		PositionSide: cfg.PositionSide,
	})
	if err != nil {
		log.Printf("[DCA] Close position failed: %v", err)
	}

	dcaMu.Lock()
	state.Active = false
	dcaMu.Unlock()

	select {
	case <-state.stopC:
	default:
		close(state.stopC)
	}
}
