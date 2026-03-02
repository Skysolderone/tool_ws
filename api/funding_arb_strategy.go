package api

import (
	"context"
	"fmt"
	"log"
	"math"
	"sync"
	"time"

	"github.com/adshao/go-binance/v2/futures"
)

// ========== 资金费率极端套利策略 ==========
// 当资金费率极端时开反向仓位收取费率
// 下次结算前 30 分钟停止开仓，避免结算风险

// FundingArbConfig 资金费率套利策略配置
type FundingArbConfig struct {
	Enabled           bool     `json:"enabled"`
	Symbols           []string `json:"symbols"`           // 监控的币种列表，空则监控所有USDT合约
	Leverage          int      `json:"leverage"`          // 默认 3
	AmountPerOrder    string   `json:"amountPerOrder"`    // 默认 50
	HighRateThreshold float64  `json:"highRateThreshold"` // 费率阈值，默认 0.05 (0.05%)
	CooldownHours     int      `json:"cooldownHours"`     // 冷却小时，默认 8
}

// FundingArbStatus 策略状态
type FundingArbStatus struct {
	Config        FundingArbConfig           `json:"config"`
	Active        bool                       `json:"active"`
	LastCheckAt   string                     `json:"lastCheckAt"`
	ActiveTrades  map[string]fundingArbTrade `json:"activeTrades"` // symbol -> 持仓信息
	TotalTrades   int                        `json:"totalTrades"`
	LastError     string                     `json:"lastError"`
}

// fundingArbTrade 单个套利持仓记录
type fundingArbTrade struct {
	Symbol      string    `json:"symbol"`
	Direction   string    `json:"direction"` // LONG / SHORT
	OpenedAt    time.Time `json:"openedAt"`
	FundingRate float64   `json:"fundingRate"` // 开仓时的费率
}

type fundingArbState struct {
	mu           sync.RWMutex
	config       FundingArbConfig
	active       bool
	stopC        chan struct{}
	lastCheckAt  time.Time
	activeTrades map[string]fundingArbTrade // symbol -> trade
	cooldownMap  map[string]time.Time        // symbol -> 上次触发时间
	totalTrades  int
	lastError    string
}

var (
	fundingArb     = &fundingArbState{activeTrades: make(map[string]fundingArbTrade), cooldownMap: make(map[string]time.Time)}
	fundingArbOnce sync.Mutex
)

// StartFundingArb 启动资金费率套利策略
func StartFundingArb(cfg FundingArbConfig) error {
	fundingArbOnce.Lock()
	defer fundingArbOnce.Unlock()

	fundingArb.mu.Lock()
	if fundingArb.active {
		fundingArb.mu.Unlock()
		return fmt.Errorf("funding arb strategy already running")
	}
	fundingArb.mu.Unlock()

	// 参数默认值
	if cfg.Leverage <= 0 {
		cfg.Leverage = 3
	}
	if cfg.AmountPerOrder == "" {
		cfg.AmountPerOrder = "50"
	}
	if cfg.HighRateThreshold <= 0 {
		cfg.HighRateThreshold = 0.05
	}
	if cfg.CooldownHours <= 0 {
		cfg.CooldownHours = 8
	}

	stopC := make(chan struct{})

	fundingArb.mu.Lock()
	fundingArb.config = cfg
	fundingArb.active = true
	fundingArb.stopC = stopC
	fundingArb.activeTrades = make(map[string]fundingArbTrade)
	fundingArb.cooldownMap = make(map[string]time.Time)
	fundingArb.mu.Unlock()

	go runFundingArbLoop(stopC, cfg)

	SaveStrategyState("funding_arb", "*", cfg)
	log.Printf("[FundingArb] Started threshold=%.4f%% cooldown=%dh leverage=%dx",
		cfg.HighRateThreshold, cfg.CooldownHours, cfg.Leverage)
	return nil
}

// StopFundingArb 停止资金费率套利策略
func StopFundingArb() error {
	fundingArb.mu.Lock()
	defer fundingArb.mu.Unlock()

	if !fundingArb.active {
		return fmt.Errorf("funding arb strategy not running")
	}
	close(fundingArb.stopC)
	fundingArb.active = false
	fundingArb.stopC = nil

	MarkStrategyStopped("funding_arb", "*")
	log.Printf("[FundingArb] Stopped")
	return nil
}

// GetFundingArbStatus 查询策略状态
func GetFundingArbStatus() *FundingArbStatus {
	fundingArb.mu.RLock()
	defer fundingArb.mu.RUnlock()

	// 深拷贝 activeTrades
	trades := make(map[string]fundingArbTrade, len(fundingArb.activeTrades))
	for k, v := range fundingArb.activeTrades {
		trades[k] = v
	}

	s := &FundingArbStatus{
		Config:       fundingArb.config,
		Active:       fundingArb.active,
		ActiveTrades: trades,
		TotalTrades:  fundingArb.totalTrades,
		LastError:    fundingArb.lastError,
	}
	if !fundingArb.lastCheckAt.IsZero() {
		s.LastCheckAt = fundingArb.lastCheckAt.Format(time.RFC3339)
	}
	return s
}

func runFundingArbLoop(stopC chan struct{}, cfg FundingArbConfig) {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	// 启动立即执行一次
	checkFundingArb(cfg)

	for {
		select {
		case <-stopC:
			return
		case <-ticker.C:
			checkFundingArb(cfg)
		}
	}
}

func checkFundingArb(cfg FundingArbConfig) {
	fundingArb.mu.Lock()
	fundingArb.lastCheckAt = time.Now()
	fundingArb.mu.Unlock()

	ctx := context.Background()
	items, err := fetchAllFundingRates(ctx)
	if err != nil {
		fundingArb.mu.Lock()
		fundingArb.lastError = err.Error()
		fundingArb.mu.Unlock()
		log.Printf("[FundingArb] Fetch rates failed: %v", err)
		return
	}

	cooldownDur := time.Duration(cfg.CooldownHours) * time.Hour

	// 构建监控币种集合
	watchSet := make(map[string]bool)
	for _, s := range cfg.Symbols {
		watchSet[s] = true
	}
	watchAll := len(cfg.Symbols) == 0

	fundingArb.mu.RLock()
	cooldownMap := make(map[string]time.Time, len(fundingArb.cooldownMap))
	for k, v := range fundingArb.cooldownMap {
		cooldownMap[k] = v
	}
	fundingArb.mu.RUnlock()

	for _, item := range items {
		// 只处理 USDT 结尾的合约
		if len(item.Symbol) <= 4 || item.Symbol[len(item.Symbol)-4:] != "USDT" {
			continue
		}

		// 过滤监控币种
		if !watchAll && !watchSet[item.Symbol] {
			continue
		}

		// 冷却检查
		if lastTrigger, ok := cooldownMap[item.Symbol]; ok {
			if time.Since(lastTrigger) < cooldownDur {
				continue
			}
		}

		// 检查费率是否超过阈值
		rate := item.FundingRate
		absRate := math.Abs(rate)
		if absRate < cfg.HighRateThreshold/100 {
			// 注意：HighRateThreshold 单位是 0.05 表示 0.05%，即 0.0005 原始值
			// 需要统一单位：配置的是百分比数值如 0.05，原始rate也是小数如 0.0005
			// 这里比较原始rate和阈值（均视为小数形式）
			continue
		}

		// 下次结算时间前 30 分钟不开仓
		nextFunding := time.UnixMilli(item.NextFundingTime)
		if time.Until(nextFunding) < 30*time.Minute {
			log.Printf("[FundingArb] %s: next funding in %v, skip", item.Symbol, time.Until(nextFunding).Round(time.Minute))
			continue
		}

		// 确定方向：正费率 → 做空收费（多头付费给空头），负费率 → 做多收费
		var side futures.SideType
		var positionSide futures.PositionSideType
		var direction string

		if rate > 0 {
			// 正费率：多头付费给空头，做空可收取费率
			side = futures.SideTypeSell
			positionSide = futures.PositionSideTypeShort
			direction = "SHORT"
		} else {
			// 负费率：空头付费给多头，做多可收取费率
			side = futures.SideTypeBuy
			positionSide = futures.PositionSideTypeLong
			direction = "LONG"
		}

		log.Printf("[FundingArb] Signal %s %s rate=%.6f%% nextFunding=%v",
			item.Symbol, direction, rate*100, nextFunding.Format("15:04"))

		if err := CheckRisk(); err != nil {
			log.Printf("[FundingArb] Risk check failed: %v", err)
			continue
		}

		req := PlaceOrderReq{
			Source:        "funding_arb",
			Symbol:        item.Symbol,
			Side:          side,
			OrderType:     futures.OrderTypeMarket,
			PositionSide:  positionSide,
			QuoteQuantity: cfg.AmountPerOrder,
			Leverage:      cfg.Leverage,
		}

		resp, err := PlaceOrderViaWs(ctx, req)
		if err != nil {
			log.Printf("[FundingArb] Place order failed %s: %v", item.Symbol, err)
			SendNotify(fmt.Sprintf("*资金费率套利* 下单失败 %s: %v", item.Symbol, err))
			fundingArb.mu.Lock()
			fundingArb.lastError = err.Error()
			fundingArb.mu.Unlock()
			continue
		}

		orderID := int64(0)
		if resp != nil && resp.Order != nil {
			orderID = resp.Order.OrderID
		}
		log.Printf("[FundingArb] Order placed %s %s orderID=%d", item.Symbol, direction, orderID)

		fundingArb.mu.Lock()
		fundingArb.activeTrades[item.Symbol] = fundingArbTrade{
			Symbol:      item.Symbol,
			Direction:   direction,
			OpenedAt:    time.Now(),
			FundingRate: rate,
		}
		fundingArb.cooldownMap[item.Symbol] = time.Now()
		fundingArb.totalTrades++
		fundingArb.lastError = ""
		fundingArb.mu.Unlock()

		msg := fmt.Sprintf("*资金费率套利开仓*\n交易对: %s\n方向: %s\n费率: %.4f%%\n金额: %s USDT x %dx\n下次结算: %s",
			item.Symbol, direction, rate*100, cfg.AmountPerOrder, cfg.Leverage,
			nextFunding.Format("15:04:05"))
		SendNotify(msg)
	}

	fundingArb.mu.Lock()
	if fundingArb.lastError == "" {
		// 清理过期冷却记录
		for sym, t := range fundingArb.cooldownMap {
			if time.Since(t) > cooldownDur*2 {
				delete(fundingArb.cooldownMap, sym)
			}
		}
	}
	fundingArb.mu.Unlock()
}
