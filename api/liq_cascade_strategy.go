package api

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/adshao/go-binance/v2/futures"
)

// ========== 爆仓级联交易策略 ==========
// 当检测到大规模同方向爆仓时，反向开仓做均值回归

// LiqCascadeConfig 爆仓级联策略配置
type LiqCascadeConfig struct {
	Enabled        bool    `json:"enabled"`
	Symbol         string  `json:"symbol"`         // 默认 BTCUSDT
	Leverage       int     `json:"leverage"`        // 默认 5
	AmountPerOrder string  `json:"amountPerOrder"`  // 默认 10
	ThresholdUSDT  float64 `json:"thresholdUSDT"`   // 爆仓金额阈值，默认 5000000 (500万U)
	WindowMin      int     `json:"windowMin"`       // 时间窗口分钟，默认 5
	CooldownMin    int     `json:"cooldownMin"`     // 冷却期分钟，默认 30
}

// LiqCascadeStatus 策略状态
type LiqCascadeStatus struct {
	Config        LiqCascadeConfig `json:"config"`
	Active        bool             `json:"active"`
	LastCheckAt   string           `json:"lastCheckAt"`
	LastSignal    string           `json:"lastSignal"`    // LONG / SHORT / NONE
	LastTriggerAt string           `json:"lastTriggerAt"`
	BuyNotional   float64          `json:"buyNotional"`   // 最近窗口内多头爆仓金额
	SellNotional  float64          `json:"sellNotional"`  // 最近窗口内空头爆仓金额
	TotalTrades   int              `json:"totalTrades"`
	LastError     string           `json:"lastError"`
}

type liqCascadeState struct {
	mu            sync.RWMutex
	config        LiqCascadeConfig
	active        bool
	stopC         chan struct{}
	lastCheckAt   time.Time
	lastSignal    string
	lastTriggerAt time.Time
	buyNotional   float64
	sellNotional  float64
	totalTrades   int
	lastError     string
}

var (
	liqCascade     = &liqCascadeState{}
	liqCascadeOnce sync.Mutex
)

// StartLiqCascade 启动爆仓级联策略
func StartLiqCascade(cfg LiqCascadeConfig) error {
	liqCascadeOnce.Lock()
	defer liqCascadeOnce.Unlock()

	liqCascade.mu.Lock()
	if liqCascade.active {
		liqCascade.mu.Unlock()
		return fmt.Errorf("liq cascade strategy already running")
	}
	liqCascade.mu.Unlock()

	// 参数默认值
	if cfg.Symbol == "" {
		cfg.Symbol = "BTCUSDT"
	}
	if cfg.Leverage <= 0 {
		cfg.Leverage = 5
	}
	if cfg.AmountPerOrder == "" {
		cfg.AmountPerOrder = "10"
	}
	if cfg.ThresholdUSDT <= 0 {
		cfg.ThresholdUSDT = 5_000_000
	}
	if cfg.WindowMin <= 0 {
		cfg.WindowMin = 5
	}
	if cfg.CooldownMin <= 0 {
		cfg.CooldownMin = 30
	}

	stopC := make(chan struct{})

	liqCascade.mu.Lock()
	liqCascade.config = cfg
	liqCascade.active = true
	liqCascade.stopC = stopC
	liqCascade.lastSignal = "NONE"
	liqCascade.mu.Unlock()

	go runLiqCascadeLoop(stopC, cfg)

	SaveStrategyState("liq_cascade", cfg.Symbol, cfg)
	log.Printf("[LiqCascade] Started symbol=%s threshold=%.0f windowMin=%d",
		cfg.Symbol, cfg.ThresholdUSDT, cfg.WindowMin)
	return nil
}

// StopLiqCascade 停止爆仓级联策略
func StopLiqCascade() error {
	liqCascade.mu.Lock()
	defer liqCascade.mu.Unlock()

	if !liqCascade.active {
		return fmt.Errorf("liq cascade strategy not running")
	}
	close(liqCascade.stopC)
	liqCascade.active = false
	liqCascade.stopC = nil

	MarkStrategyStopped("liq_cascade", liqCascade.config.Symbol)
	log.Printf("[LiqCascade] Stopped")
	return nil
}

// GetLiqCascadeStatus 查询策略状态
func GetLiqCascadeStatus() *LiqCascadeStatus {
	liqCascade.mu.RLock()
	defer liqCascade.mu.RUnlock()

	s := &LiqCascadeStatus{
		Config:       liqCascade.config,
		Active:       liqCascade.active,
		LastSignal:   liqCascade.lastSignal,
		BuyNotional:  liqCascade.buyNotional,
		SellNotional: liqCascade.sellNotional,
		TotalTrades:  liqCascade.totalTrades,
		LastError:    liqCascade.lastError,
	}
	if !liqCascade.lastCheckAt.IsZero() {
		s.LastCheckAt = liqCascade.lastCheckAt.Format(time.RFC3339)
	}
	if !liqCascade.lastTriggerAt.IsZero() {
		s.LastTriggerAt = liqCascade.lastTriggerAt.Format(time.RFC3339)
	}
	return s
}

func runLiqCascadeLoop(stopC chan struct{}, cfg LiqCascadeConfig) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-stopC:
			return
		case <-ticker.C:
			checkLiqCascade(cfg)
		}
	}
}

func checkLiqCascade(cfg LiqCascadeConfig) {
	if DB == nil {
		return
	}

	liqCascade.mu.Lock()
	liqCascade.lastCheckAt = time.Now()
	liqCascade.mu.Unlock()

	// 计算时间窗口
	windowStart := time.Now().Add(-time.Duration(cfg.WindowMin) * time.Minute)
	windowStartMs := windowStart.UnixMilli()

	// 查询 DB 最近 WindowMin 分钟内的爆仓统计
	// 使用 1h 粒度数据（最细粒度），按 start_time >= windowStartMs 过滤
	var records []LiquidationStatRecord
	if err := DB.
		Where("bucket_interval = ? AND start_time >= ?", liquidationIntervalH1, windowStartMs).
		Order("start_time DESC").
		Find(&records).Error; err != nil {
		liqCascade.mu.Lock()
		liqCascade.lastError = err.Error()
		liqCascade.mu.Unlock()
		log.Printf("[LiqCascade] DB query failed: %v", err)
		return
	}

	// 汇总买方（多头）和卖方（空头）爆仓金额
	// BuyNotional = 多头爆仓（买入平仓），SellNotional = 空头爆仓（卖出平仓）
	var totalBuy, totalSell float64
	for _, r := range records {
		totalBuy += r.BuyNotional
		totalSell += r.SellNotional
	}

	liqCascade.mu.Lock()
	liqCascade.buyNotional = totalBuy
	liqCascade.sellNotional = totalSell
	lastTrigger := liqCascade.lastTriggerAt
	liqCascade.mu.Unlock()

	// 冷却检查
	cooldownDur := time.Duration(cfg.CooldownMin) * time.Minute
	if time.Since(lastTrigger) < cooldownDur {
		return
	}

	// 判断信号
	// 多头大规模爆仓 → 反向做多（均值回归，超卖反弹）
	// 空头大规模爆仓 → 反向做空（均值回归，超买回调）
	var signal string
	var side futures.SideType
	var positionSide futures.PositionSideType

	if totalBuy > cfg.ThresholdUSDT {
		// 多头大量爆仓 → 做多反弹
		signal = "LONG"
		side = futures.SideTypeBuy
		positionSide = futures.PositionSideTypeLong
	} else if totalSell > cfg.ThresholdUSDT {
		// 空头大量爆仓 → 做空反弹
		signal = "SHORT"
		side = futures.SideTypeSell
		positionSide = futures.PositionSideTypeShort
	}

	if signal == "" {
		return
	}

	log.Printf("[LiqCascade] Signal=%s buyLiq=%.0f sellLiq=%.0f threshold=%.0f",
		signal, totalBuy, totalSell, cfg.ThresholdUSDT)

	liqCascade.mu.Lock()
	liqCascade.lastSignal = signal
	liqCascade.lastTriggerAt = time.Now()
	liqCascade.mu.Unlock()

	if err := CheckRisk(); err != nil {
		log.Printf("[LiqCascade] Risk check failed: %v", err)
		return
	}

	req := PlaceOrderReq{
		Source:        "liq_cascade",
		Symbol:        cfg.Symbol,
		Side:          side,
		OrderType:     futures.OrderTypeMarket,
		PositionSide:  positionSide,
		QuoteQuantity: cfg.AmountPerOrder,
		Leverage:      cfg.Leverage,
	}

	resp, err := PlaceOrderViaWs(context.Background(), req)
	if err != nil {
		log.Printf("[LiqCascade] Place order failed: %v", err)
		SendNotify(fmt.Sprintf("*爆仓级联* 下单失败 %s %s: %v", cfg.Symbol, signal, err))
		liqCascade.mu.Lock()
		liqCascade.lastError = err.Error()
		liqCascade.mu.Unlock()
		return
	}

	liqCascade.mu.Lock()
	liqCascade.totalTrades++
	liqCascade.lastError = ""
	liqCascade.mu.Unlock()

	orderID := int64(0)
	if resp != nil && resp.Order != nil {
		orderID = resp.Order.OrderID
	}
	log.Printf("[LiqCascade] Order placed symbol=%s signal=%s orderID=%d", cfg.Symbol, signal, orderID)

	direction := "多头"
	if signal == "SHORT" {
		direction = "空头"
	}
	msg := fmt.Sprintf("*爆仓级联开仓* %s\n方向: %s\n多头爆仓: %.0f U\n空头爆仓: %.0f U\n金额: %s USDT x %dx",
		cfg.Symbol, direction, totalBuy, totalSell, cfg.AmountPerOrder, cfg.Leverage)
	SendNotify(msg)
}
