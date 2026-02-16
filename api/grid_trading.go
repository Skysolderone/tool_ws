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

// GridConfig 网格交易配置
type GridConfig struct {
	Symbol       string                   `json:"symbol"`
	PositionSide futures.PositionSideType `json:"positionSide,omitempty"` // LONG / SHORT / BOTH
	Leverage     int                      `json:"leverage"`

	UpperPrice float64 `json:"upperPrice"` // 价格上界
	LowerPrice float64 `json:"lowerPrice"` // 价格下界
	GridCount  int     `json:"gridCount"`  // 网格数量
	AmountPerGrid string `json:"amountPerGrid"` // 每格投入金额(USDT)

	StopLossPrice  float64 `json:"stopLossPrice,omitempty"`  // 整体止损价，可选
	TakeProfitPrice float64 `json:"takeProfitPrice,omitempty"` // 整体止盈价，可选
}

// GridStatus 网格交易状态
type GridStatus struct {
	Config       GridConfig `json:"config"`
	Active       bool       `json:"active"`
	GridLevels   []GridLevel `json:"gridLevels"`
	FilledBuys   int        `json:"filledBuys"`   // 已成交买单数
	FilledSells  int        `json:"filledSells"`  // 已成交卖单数
	TotalProfit  float64    `json:"totalProfit"`  // 网格总利润
	CurrentPrice float64   `json:"currentPrice"` // 当前价格
}

// GridLevel 单个网格层级
type GridLevel struct {
	Price     float64 `json:"price"`
	HasBuy    bool    `json:"hasBuy"`    // 是否在此价位有挂单/已买入
	Filled    bool    `json:"filled"`    // 该层是否已持有
}

type gridState struct {
	Config      GridConfig
	Active      bool
	Levels      []GridLevel
	FilledBuys  int
	FilledSells int
	TotalProfit float64
	stopC       chan struct{}
}

var (
	gridTasks = make(map[string]*gridState)
	gridMu    sync.Mutex
)

// StartGrid 启动网格交易
func StartGrid(config GridConfig) error {
	if config.Symbol == "" {
		return fmt.Errorf("symbol is required")
	}
	if config.UpperPrice <= config.LowerPrice {
		return fmt.Errorf("upperPrice must be greater than lowerPrice")
	}
	if config.GridCount < 2 || config.GridCount > 100 {
		return fmt.Errorf("gridCount must be between 2 and 100")
	}
	if config.AmountPerGrid == "" {
		return fmt.Errorf("amountPerGrid is required")
	}
	if config.Leverage <= 0 {
		return fmt.Errorf("leverage must be > 0")
	}

	// 双向持仓模式下默认用 LONG（网格做多为主）
	if config.PositionSide == "" {
		config.PositionSide = futures.PositionSideTypeLong
	}

	gridMu.Lock()
	defer gridMu.Unlock()

	if existing, ok := gridTasks[config.Symbol]; ok && existing.Active {
		return fmt.Errorf("grid already running for %s, stop it first", config.Symbol)
	}

	// 计算网格层级价格
	levels := make([]GridLevel, config.GridCount)
	step := (config.UpperPrice - config.LowerPrice) / float64(config.GridCount-1)
	for i := 0; i < config.GridCount; i++ {
		levels[i] = GridLevel{
			Price: config.LowerPrice + step*float64(i),
		}
	}

	state := &gridState{
		Config: config,
		Active: true,
		Levels: levels,
		stopC:  make(chan struct{}),
	}
	gridTasks[config.Symbol] = state

	go gridMonitorLoop(state)

	log.Printf("[Grid] Started for %s: range=[%.2f, %.2f], grids=%d, perGrid=%s USDT",
		config.Symbol, config.LowerPrice, config.UpperPrice, config.GridCount, config.AmountPerGrid)

	return nil
}

// StopGrid 停止网格交易
func StopGrid(symbol string) error {
	gridMu.Lock()
	defer gridMu.Unlock()

	state, ok := gridTasks[symbol]
	if !ok || !state.Active {
		return fmt.Errorf("no active grid task for %s", symbol)
	}

	close(state.stopC)
	state.Active = false
	log.Printf("[Grid] Stopped for %s: buys=%d, sells=%d, profit=%.4f",
		symbol, state.FilledBuys, state.FilledSells, state.TotalProfit)

	return nil
}

// GetGridStatus 获取网格交易状态
func GetGridStatus(symbol string) *GridStatus {
	gridMu.Lock()
	defer gridMu.Unlock()

	state, ok := gridTasks[symbol]
	if !ok {
		return nil
	}

	// 获取当前价格
	var currentPrice float64
	cache := GetPriceCache()
	price, err := cache.GetPrice(symbol)
	if err == nil {
		currentPrice = price
	}

	return &GridStatus{
		Config:       state.Config,
		Active:       state.Active,
		GridLevels:   state.Levels,
		FilledBuys:   state.FilledBuys,
		FilledSells:  state.FilledSells,
		TotalProfit:  state.TotalProfit,
		CurrentPrice: currentPrice,
	}
}

// gridMonitorLoop 网格交易监控循环
func gridMonitorLoop(state *gridState) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	cfg := state.Config
	ctx := context.Background()

	// 设置杠杆
	if _, err := ChangeLeverage(ctx, cfg.Symbol, cfg.Leverage); err != nil {
		log.Printf("[Grid] Warning: set leverage failed: %v", err)
	}

	log.Printf("[Grid] Monitor started for %s", cfg.Symbol)

	for {
		select {
		case <-state.stopC:
			log.Printf("[Grid] Monitor stopped for %s", cfg.Symbol)
			return
		case <-ticker.C:
			gridTick(ctx, state)
		}
	}
}

// gridTick 每个 tick 检查价格并决定买卖
func gridTick(ctx context.Context, state *gridState) {
	cfg := state.Config

	// 获取当前价格
	cache := GetPriceCache()
	currentPrice, err := cache.GetPrice(cfg.Symbol)
	if err != nil {
		return
	}

	// 止损/止盈检查
	if cfg.StopLossPrice > 0 && currentPrice <= cfg.StopLossPrice {
		log.Printf("[Grid] Stop loss triggered for %s at %.4f", cfg.Symbol, currentPrice)
		gridCloseAll(ctx, state)
		return
	}
	if cfg.TakeProfitPrice > 0 && currentPrice >= cfg.TakeProfitPrice {
		log.Printf("[Grid] Take profit triggered for %s at %.4f", cfg.Symbol, currentPrice)
		gridCloseAll(ctx, state)
		return
	}

	// 找到当前价格所在的层级
	for i := range state.Levels {
		level := &state.Levels[i]

		if level.Filled {
			// 已持有：如果价格涨到上一格 → 卖出
			if i < len(state.Levels)-1 {
				nextPrice := state.Levels[i+1].Price
				if currentPrice >= nextPrice {
					// 卖出（平多 / 开空）
					err := gridSellAtLevel(ctx, state, i)
					if err != nil {
						log.Printf("[Grid] Sell at level %d (%.2f) failed: %v", i, level.Price, err)
					}
				}
			}
		} else {
			// 未持有：如果价格跌到该层 → 买入
			if currentPrice <= level.Price && currentPrice >= cfg.LowerPrice {
				// 不重复买：检查上方没有未持有层级先于当前层买入
				err := gridBuyAtLevel(ctx, state, i)
				if err != nil {
					log.Printf("[Grid] Buy at level %d (%.2f) failed: %v", i, level.Price, err)
				}
			}
		}
	}
}

// gridBuyAtLevel 在指定层级买入
func gridBuyAtLevel(ctx context.Context, state *gridState, levelIdx int) error {
	cfg := state.Config
	level := &state.Levels[levelIdx]

	// 风控检查
	if err := CheckRisk(); err != nil {
		return err
	}

	positionSide := cfg.PositionSide
	if positionSide == "" {
		positionSide = futures.PositionSideTypeBoth
	}

	req := PlaceOrderReq{
		Symbol:        cfg.Symbol,
		Side:          futures.SideTypeBuy,
		OrderType:     futures.OrderTypeMarket,
		PositionSide:  positionSide,
		QuoteQuantity: cfg.AmountPerGrid,
		Leverage:      cfg.Leverage,
	}

	result, err := PlaceOrderViaWs(ctx, req)
	if err != nil {
		return err
	}

	gridMu.Lock()
	level.Filled = true
	level.HasBuy = true
	state.FilledBuys++
	gridMu.Unlock()

	log.Printf("[Grid] BUY at level %d (%.2f): orderId=%d, %s USDT",
		levelIdx, level.Price, result.Order.OrderID, cfg.AmountPerGrid)

	return nil
}

// gridSellAtLevel 在指定层级卖出
func gridSellAtLevel(ctx context.Context, state *gridState, levelIdx int) error {
	cfg := state.Config
	level := &state.Levels[levelIdx]

	positionSide := cfg.PositionSide
	if positionSide == "" {
		positionSide = futures.PositionSideTypeBoth
	}

	// 卖出（平仓）同样金额
	req := PlaceOrderReq{
		Symbol:        cfg.Symbol,
		Side:          futures.SideTypeSell,
		OrderType:     futures.OrderTypeMarket,
		PositionSide:  positionSide,
		QuoteQuantity: cfg.AmountPerGrid,
		Leverage:      cfg.Leverage,
	}

	result, err := PlaceOrderViaWs(ctx, req)
	if err != nil {
		return err
	}

	// 计算单格利润
	nextPrice := state.Levels[levelIdx+1].Price
	gridStep := nextPrice - level.Price
	amtPerGrid, _ := strconv.ParseFloat(cfg.AmountPerGrid, 64)
	profitEstimate := amtPerGrid * float64(cfg.Leverage) * gridStep / level.Price

	gridMu.Lock()
	level.Filled = false
	level.HasBuy = false
	state.FilledSells++
	state.TotalProfit += profitEstimate
	gridMu.Unlock()

	log.Printf("[Grid] SELL at level %d (%.2f→%.2f): orderId=%d, profit≈%.4f USDT",
		levelIdx, level.Price, nextPrice, result.Order.OrderID, profitEstimate)

	return nil
}

// gridCloseAll 网格止损/止盈 → 平掉所有仓位并停止
func gridCloseAll(ctx context.Context, state *gridState) {
	cfg := state.Config
	positionSide := cfg.PositionSide
	if positionSide == "" {
		positionSide = futures.PositionSideTypeBoth
	}

	_, err := ClosePositionViaWs(ctx, ClosePositionReq{
		Symbol:       cfg.Symbol,
		PositionSide: positionSide,
	})
	if err != nil {
		log.Printf("[Grid] Close all position failed: %v", err)
	}

	gridMu.Lock()
	state.Active = false
	gridMu.Unlock()

	// 关闭 channel
	select {
	case <-state.stopC:
	default:
		close(state.stopC)
	}
}

// calculateGridLevels 辅助：计算等差网格价格
func calculateGridLevels(lower, upper float64, count int) []float64 {
	levels := make([]float64, count)
	step := (upper - lower) / float64(count-1)
	for i := 0; i < count; i++ {
		levels[i] = math.Round((lower+step*float64(i))*100) / 100
	}
	return levels
}
