package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"sort"
	"strconv"
	"sync"
	"time"
)

// ========== 资金费率监控 ==========
// 定时拉取所有 USDT 永续合约的 funding rate，发现异常时触发提醒或自动开仓

// FundingRateConfig 资金费率监控配置
type FundingRateConfig struct {
	// 触发阈值（年化百分比），如 100 表示年化 100%
	// funding rate 每 8h 一次，年化 = rate × 3 × 365 × 100
	ThresholdAnnualized float64 `json:"thresholdAnnualized"`
	// 刷新间隔（秒），默认 300（5 分钟）
	IntervalSec int `json:"intervalSec,omitempty"`
	// 是否自动开仓（反向套利）
	AutoTrade bool `json:"autoTrade,omitempty"`
	// 自动开仓金额(USDT)
	AmountPerOrder string `json:"amountPerOrder,omitempty"`
	// 自动开仓杠杆
	Leverage int `json:"leverage,omitempty"`
	// 只监控前 N 个异常币种
	TopN int `json:"topN,omitempty"`
}

// FundingRateItem 单个币种费率信息
type FundingRateItem struct {
	Symbol          string  `json:"symbol"`
	FundingRate     float64 `json:"fundingRate"`     // 原始费率（如 0.001 = 0.1%）
	FundingRatePct  float64 `json:"fundingRatePct"`  // 百分比（如 0.1）
	AnnualizedPct   float64 `json:"annualizedPct"`   // 年化百分比
	NextFundingTime int64   `json:"nextFundingTime"` // 下次结算时间戳
	MarkPrice       float64 `json:"markPrice"`
	Direction       string  `json:"direction"` // "positive" 多付空 / "negative" 空付多
}

// FundingRateSnapshot 监控快照
type FundingRateSnapshot struct {
	UpdateTime    string             `json:"updateTime"`
	TopPositive   []FundingRateItem  `json:"topPositive"`   // 正费率最高（多头付费）
	TopNegative   []FundingRateItem  `json:"topNegative"`   // 负费率最高（空头付费）
	AlertSymbols  []FundingRateItem  `json:"alertSymbols"`  // 超阈值的币种
	Config        FundingRateConfig  `json:"config"`
	Active        bool               `json:"active"`
}

// fundingMonitor 全局资金费率监控实例
type fundingMonitor struct {
	mu       sync.RWMutex
	config   FundingRateConfig
	active   bool
	cancel   context.CancelFunc
	snapshot FundingRateSnapshot
}

var fundingMon = &fundingMonitor{}

// StartFundingMonitor 启动资金费率监控
func StartFundingMonitor(config FundingRateConfig) error {
	fundingMon.mu.Lock()
	defer fundingMon.mu.Unlock()

	if fundingMon.active {
		// 先停止旧的
		if fundingMon.cancel != nil {
			fundingMon.cancel()
		}
	}

	if config.ThresholdAnnualized <= 0 {
		config.ThresholdAnnualized = 100 // 默认年化 100%
	}
	if config.IntervalSec <= 0 {
		config.IntervalSec = 300
	}
	if config.TopN <= 0 {
		config.TopN = 10
	}
	if config.Leverage <= 0 {
		config.Leverage = 5
	}

	ctx, cancel := context.WithCancel(context.Background())
	fundingMon.config = config
	fundingMon.active = true
	fundingMon.cancel = cancel

	go fundingMonitorLoop(ctx, config)

	log.Printf("[FundingRate] Monitor started: threshold=%.0f%% annualized, interval=%ds, autoTrade=%v",
		config.ThresholdAnnualized, config.IntervalSec, config.AutoTrade)

	SaveStrategyState("funding", "*", config)
	return nil
}

// StopFundingMonitor 停止资金费率监控
func StopFundingMonitor() error {
	fundingMon.mu.Lock()
	defer fundingMon.mu.Unlock()

	if !fundingMon.active {
		return fmt.Errorf("funding monitor not active")
	}
	if fundingMon.cancel != nil {
		fundingMon.cancel()
	}
	fundingMon.active = false
	log.Println("[FundingRate] Monitor stopped")
	MarkStrategyStopped("funding", "*")
	return nil
}

// GetFundingStatus 获取资金费率监控状态
func GetFundingStatus() *FundingRateSnapshot {
	fundingMon.mu.RLock()
	defer fundingMon.mu.RUnlock()
	snap := fundingMon.snapshot
	snap.Active = fundingMon.active
	snap.Config = fundingMon.config
	return &snap
}

// fundingMonitorLoop 监控主循环
func fundingMonitorLoop(ctx context.Context, config FundingRateConfig) {
	ticker := time.NewTicker(time.Duration(config.IntervalSec) * time.Second)
	defer ticker.Stop()

	// 首次立即执行
	doFundingCheck(ctx, config)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			doFundingCheck(ctx, config)
		}
	}
}

// premiumIndexResp 币安 premiumIndex 返回
type premiumIndexResp struct {
	Symbol          string `json:"symbol"`
	MarkPrice       string `json:"markPrice"`
	LastFundingRate string `json:"lastFundingRate"`
	NextFundingTime int64  `json:"nextFundingTime"`
}

// doFundingCheck 执行一次资金费率检查
func doFundingCheck(ctx context.Context, config FundingRateConfig) {
	items, err := fetchAllFundingRates(ctx)
	if err != nil {
		log.Printf("[FundingRate] Fetch failed: %v", err)
		return
	}

	// 过滤只要 USDT 结尾
	var usdtItems []FundingRateItem
	for _, item := range items {
		if len(item.Symbol) > 4 && item.Symbol[len(item.Symbol)-4:] == "USDT" {
			usdtItems = append(usdtItems, item)
		}
	}

	// 按年化排序
	sort.Slice(usdtItems, func(i, j int) bool {
		return usdtItems[i].AnnualizedPct > usdtItems[j].AnnualizedPct
	})

	topN := config.TopN
	if topN > len(usdtItems) {
		topN = len(usdtItems)
	}

	// 正费率 top
	topPositive := make([]FundingRateItem, 0, topN)
	for _, item := range usdtItems {
		if item.FundingRate > 0 && len(topPositive) < topN {
			topPositive = append(topPositive, item)
		}
	}

	// 负费率 top（从末尾取）
	topNegative := make([]FundingRateItem, 0, topN)
	for i := len(usdtItems) - 1; i >= 0 && len(topNegative) < topN; i-- {
		if usdtItems[i].FundingRate < 0 {
			topNegative = append(topNegative, usdtItems[i])
		}
	}

	// 超阈值告警
	var alertSymbols []FundingRateItem
	for _, item := range usdtItems {
		if math.Abs(item.AnnualizedPct) >= config.ThresholdAnnualized {
			alertSymbols = append(alertSymbols, item)
		}
	}

	// 更新快照
	fundingMon.mu.Lock()
	fundingMon.snapshot = FundingRateSnapshot{
		UpdateTime:   time.Now().Format("2006-01-02 15:04:05"),
		TopPositive:  topPositive,
		TopNegative:  topNegative,
		AlertSymbols: alertSymbols,
	}
	fundingMon.mu.Unlock()

	log.Printf("[FundingRate] Check done: %d USDT pairs, %d alerts", len(usdtItems), len(alertSymbols))

	// 自动开仓
	if config.AutoTrade && len(alertSymbols) > 0 {
		for _, item := range alertSymbols {
			go autoFundingTrade(ctx, config, item)
		}
	}
}

// fetchAllFundingRates 获取所有合约的资金费率
func fetchAllFundingRates(ctx context.Context) ([]FundingRateItem, error) {
	baseURL := "https://fapi.binance.com"
	if Cfg.Testnet {
		baseURL = "https://testnet.binancefuture.com"
	}
	reqURL := baseURL + "/fapi/v1/premiumIndex"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var rawItems []premiumIndexResp
	if err := json.Unmarshal(body, &rawItems); err != nil {
		return nil, fmt.Errorf("parse premiumIndex: %w", err)
	}

	result := make([]FundingRateItem, 0, len(rawItems))
	for _, raw := range rawItems {
		rate, _ := strconv.ParseFloat(raw.LastFundingRate, 64)
		mark, _ := strconv.ParseFloat(raw.MarkPrice, 64)
		ratePct := rate * 100
		annualized := ratePct * 3 * 365 // 每8h一次, 一天3次, 一年365天

		direction := "positive"
		if rate < 0 {
			direction = "negative"
		}

		result = append(result, FundingRateItem{
			Symbol:          raw.Symbol,
			FundingRate:     rate,
			FundingRatePct:  ratePct,
			AnnualizedPct:   annualized,
			NextFundingTime: raw.NextFundingTime,
			MarkPrice:       mark,
			Direction:       direction,
		})
	}

	return result, nil
}

// autoFundingTrade 自动资金费率套利：费率为正做空吃费率，费率为负做多吃费率
func autoFundingTrade(ctx context.Context, config FundingRateConfig, item FundingRateItem) {
	// 正费率 → 空头收费率 → 做空; 负费率 → 多头收费率 → 做多
	side := "SELL"
	if item.FundingRate < 0 {
		side = "BUY"
	}

	req := PlaceOrderReq{
		Source:        "funding_arb",
		Symbol:        item.Symbol,
		Side:          "BUY",
		OrderType:     "MARKET",
		QuoteQuantity: config.AmountPerOrder,
		Leverage:      config.Leverage,
	}
	if side == "SELL" {
		req.Side = "SELL"
	}
	if req.Side == "BUY" {
		req.PositionSide = "LONG"
	} else {
		req.PositionSide = "SHORT"
	}

	result, err := PlaceOrderViaWs(ctx, req)
	if err != nil {
		log.Printf("[FundingArb] Auto trade failed for %s: %v", item.Symbol, err)
		SaveFailedOperation("FUNDING_ARB", "funding_arb", item.Symbol, req, 0, err)
		return
	}

	log.Printf("[FundingArb] Opened %s %s (rate=%.4f%%, annualized=%.0f%%), orderId=%d",
		side, item.Symbol, item.FundingRatePct, item.AnnualizedPct, result.Order.OrderID)
}
