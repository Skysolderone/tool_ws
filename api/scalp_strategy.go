package api

import (
	"context"
	"fmt"
	"log"
	"math"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/adshao/go-binance/v2/futures"
	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/utils"
)

// ========== 1分钟 Scalp 策略 ==========
// 每分钟分析一次，综合 EMA交叉 + RSI + 成交量动量 自动判断买卖方向
// 有仓位时先平再反向，同一时间只持一个方向

// ScalpConfig 策略配置
type ScalpConfig struct {
	Symbol   string `json:"symbol"`
	Leverage int    `json:"leverage"`

	// 每次下单金额 (USDT)
	AmountPerOrder string `json:"amountPerOrder"`

	// 可选参数（都有合理默认值）
	EMAFast  int `json:"emaFast,omitempty"`  // 快线周期，默认 7
	EMASlow  int `json:"emaSlow,omitempty"`  // 慢线周期，默认 21
	EMATrend int `json:"emaTrend,omitempty"` // 趋势线周期，默认 50

	RSIPeriod     int     `json:"rsiPeriod,omitempty"`     // RSI 周期，默认 6
	RSIOverbought float64 `json:"rsiOverbought,omitempty"` // RSI 超买阈值，默认 75（不在超买时做多）
	RSIOversold   float64 `json:"rsiOversold,omitempty"`   // RSI 超卖阈值，默认 25（不在超卖时做空）

	VolumePeriod int     `json:"volumePeriod,omitempty"` // 成交量均线周期，默认 10
	VolumeMulti  float64 `json:"volumeMulti,omitempty"`  // 量比阈值，默认 1.2

	// 风控
	MaxLossPerTrade float64 `json:"maxLossPerTrade,omitempty"` // 单笔最大亏损(USDT)，默认不限
	MaxDailyLoss    float64 `json:"maxDailyLoss,omitempty"`    // 日最大亏损(USDT)，超过自动停
	MaxDailyTrades  int     `json:"maxDailyTrades,omitempty"`  // 日最大交易次数，默认 100
	CooldownSec     int     `json:"cooldownSec,omitempty"`     // 平仓后冷却秒数，默认 60

	// ATR 动态止损与仓位管理
	ATRPeriod     int     `json:"atrPeriod,omitempty"`     // ATR周期，默认14
	ATRMultiplier float64 `json:"atrMultiplier,omitempty"` // ATR止损倍数，默认1.5
	ATRSizing     bool    `json:"atrSizing,omitempty"`     // 是否启用ATR动态仓位
}

// ScalpStatus 策略状态
type ScalpStatus struct {
	Config     ScalpConfig `json:"config"`
	Active     bool        `json:"active"`
	Direction  string      `json:"direction"`  // LONG / SHORT / FLAT
	EMAFast    float64     `json:"emaFast"`    // 当前快线值
	EMASlow    float64     `json:"emaSlow"`    // 当前慢线值
	EMATrend   float64     `json:"emaTrend"`   // 当前趋势线值
	RSI        float64     `json:"rsi"`        // 当前 RSI
	VolRatio   float64     `json:"volRatio"`   // 量比
	MACD       float64     `json:"macd"`       // MACD 值
	MACDSignal float64     `json:"macdSignal"` // MACD 信号线
	MACDHist   float64     `json:"macdHist"`   // MACD 柱
	BBUpper    float64     `json:"bbUpper"`    // 布林带上轨
	BBMiddle   float64     `json:"bbMiddle"`   // 布林带中轨
	BBLower    float64     `json:"bbLower"`    // 布林带下轨
	ATR        float64     `json:"atr"`        // 当前 ATR 值
	Trend4H    string      `json:"trend4h"`    // 4H 趋势方向 BULL / BEAR / NEUTRAL
	Signal       string      `json:"signal"`       // 最近信号 BUY / SELL / HOLD
	SignalReason string      `json:"signalReason"` // 信号原因
	SignalTime   string      `json:"signalTime"`   // 信号时间
	OpenReason   string      `json:"openReason"`   // 开仓原因
	CloseReason  string      `json:"closeReason"`  // 平仓原因

	DailyTrades int     `json:"dailyTrades"` // 今日交易次数
	DailyPnl    float64 `json:"dailyPnl"`    // 今日盈亏
	TotalTrades int     `json:"totalTrades"` // 总交易次数
	TotalPnl    float64 `json:"totalPnl"`    // 总盈亏
	WinCount    int     `json:"winCount"`    // 盈利次数
	LossCount   int     `json:"lossCount"`   // 亏损次数

	LastError   string `json:"lastError"`
	LastCheckAt string `json:"lastCheckAt"`
}

type scalpState struct {
	Config ScalpConfig
	Active bool

	Direction string // LONG / SHORT / FLAT
	EMAFast   float64
	EMASlow   float64
	EMATrend  float64
	RSI        float64
	VolRatio   float64
	MACD       float64
	MACDSignal float64
	MACDHist   float64
	BBUpper    float64
	BBMiddle   float64
	BBLower    float64
	ATR          float64 // 当前 ATR 值
	CurrentPrice float64 // 当前价格（供 scalpOpenPosition 使用）
	Trend4H    string // 4H 趋势方向
	Signal       string
	SignalReason string
	SignalAt     time.Time
	OpenReason   string
	CloseReason  string

	DailyTrades int
	DailyPnl    float64
	DailyDate   string // "2006-01-02" 用于日切重置
	TotalTrades int
	TotalPnl    float64
	WinCount    int
	LossCount   int

	LastError   string
	LastCheckAt time.Time
	LastTradeAt time.Time // 冷却计时

	stopC chan struct{}
}

var (
	scalpTasks = make(map[string]*scalpState)
	scalpMu    sync.Mutex
)

// StartScalp 启动 Scalp 策略
func StartScalp(config ScalpConfig) error {
	if config.Symbol == "" {
		return fmt.Errorf("symbol is required")
	}
	if config.Leverage <= 0 {
		return fmt.Errorf("leverage must be > 0")
	}
	if config.AmountPerOrder == "" {
		return fmt.Errorf("amountPerOrder is required")
	}

	// 默认值
	if config.EMAFast <= 0 {
		config.EMAFast = 7
	}
	if config.EMASlow <= 0 {
		config.EMASlow = 21
	}
	if config.EMATrend <= 0 {
		config.EMATrend = 50
	}
	if config.RSIPeriod <= 0 {
		config.RSIPeriod = 6
	}
	if config.RSIOverbought <= 0 {
		config.RSIOverbought = 75
	}
	if config.RSIOversold <= 0 {
		config.RSIOversold = 25
	}
	if config.VolumePeriod <= 0 {
		config.VolumePeriod = 10
	}
	if config.VolumeMulti <= 0 {
		config.VolumeMulti = 1.2
	}
	if config.MaxDailyTrades <= 0 {
		config.MaxDailyTrades = 100
	}
	if config.CooldownSec <= 0 {
		config.CooldownSec = 60
	}
	if config.ATRPeriod <= 0 {
		config.ATRPeriod = 14
	}
	if config.ATRMultiplier <= 0 {
		config.ATRMultiplier = 1.5
	}

	scalpMu.Lock()
	defer scalpMu.Unlock()

	if existing, ok := scalpTasks[config.Symbol]; ok && existing.Active {
		return fmt.Errorf("scalp already running for %s", config.Symbol)
	}

	state := &scalpState{
		Config:    config,
		Active:    true,
		Direction: "FLAT",
		DailyDate: time.Now().Format("2006-01-02"),
		stopC:     make(chan struct{}),
	}
	scalpTasks[config.Symbol] = state

	go scalpLoop(state)

	log.Printf("[Scalp] Started for %s: EMA(%d/%d/%d), RSI(%d), amount=%s, lev=%dx",
		config.Symbol, config.EMAFast, config.EMASlow, config.EMATrend,
		config.RSIPeriod, config.AmountPerOrder, config.Leverage)

	SaveStrategyState("scalp", config.Symbol, config)
	return nil
}

// StopScalp 停止 Scalp 策略
func StopScalp(symbol string) error {
	scalpMu.Lock()
	defer scalpMu.Unlock()

	state, ok := scalpTasks[symbol]
	if !ok || !state.Active {
		return fmt.Errorf("no active scalp for %s", symbol)
	}

	close(state.stopC)
	state.Active = false
	log.Printf("[Scalp] Stopped for %s: trades=%d, PnL=%.4f, win=%d, loss=%d",
		symbol, state.TotalTrades, state.TotalPnl, state.WinCount, state.LossCount)
	MarkStrategyStopped("scalp", symbol)
	return nil
}

// GetScalpStatus 获取策略状态
func GetScalpStatus(symbol string) *ScalpStatus {
	scalpMu.Lock()
	defer scalpMu.Unlock()

	state, ok := scalpTasks[symbol]
	if !ok {
		return nil
	}

	signalTime := ""
	if !state.SignalAt.IsZero() {
		signalTime = state.SignalAt.Format("15:04:05")
	}
	lastCheck := ""
	if !state.LastCheckAt.IsZero() {
		lastCheck = state.LastCheckAt.Format("15:04:05")
	}

	return &ScalpStatus{
		Config:      state.Config,
		Active:      state.Active,
		Direction:   state.Direction,
		EMAFast:     math.Round(state.EMAFast*100) / 100,
		EMASlow:     math.Round(state.EMASlow*100) / 100,
		EMATrend:    math.Round(state.EMATrend*100) / 100,
		RSI:         math.Round(state.RSI*100) / 100,
		VolRatio:    math.Round(state.VolRatio*100) / 100,
		MACD:        math.Round(state.MACD*10000) / 10000,
		MACDSignal:  math.Round(state.MACDSignal*10000) / 10000,
		MACDHist:    math.Round(state.MACDHist*10000) / 10000,
		BBUpper:     math.Round(state.BBUpper*100) / 100,
		BBMiddle:    math.Round(state.BBMiddle*100) / 100,
		BBLower:     math.Round(state.BBLower*100) / 100,
		ATR:         math.Round(state.ATR*10000) / 10000,
		Trend4H:     state.Trend4H,
		Signal:       state.Signal,
		SignalReason: state.SignalReason,
		SignalTime:   signalTime,
		OpenReason:   state.OpenReason,
		CloseReason:  state.CloseReason,
		DailyTrades: state.DailyTrades,
		DailyPnl:    math.Round(state.DailyPnl*10000) / 10000,
		TotalTrades: state.TotalTrades,
		TotalPnl:    math.Round(state.TotalPnl*10000) / 10000,
		WinCount:    state.WinCount,
		LossCount:   state.LossCount,
		LastError:   state.LastError,
		LastCheckAt: lastCheck,
	}
}

// ========== 策略主循环 ==========

func scalpLoop(state *scalpState) {
	cfg := state.Config
	ctx := context.Background()

	// 设置杠杆
	if _, err := ChangeLeverage(ctx, cfg.Symbol, cfg.Leverage); err != nil {
		log.Printf("[Scalp] Warning: set leverage failed: %v", err)
	}

	// 确保价格订阅
	_ = GetPriceCache().Subscribe(cfg.Symbol)

	// 每分钟检查
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	// 首次立即执行
	scalpCheck(ctx, state)

	for {
		select {
		case <-state.stopC:
			log.Printf("[Scalp] Loop stopped for %s", cfg.Symbol)
			return
		case <-ticker.C:
			scalpCheck(ctx, state)
		}
	}
}

func scalpCheck(ctx context.Context, state *scalpState) {
	cfg := state.Config

	scalpMu.Lock()
	state.LastCheckAt = time.Now()

	// 日切重置
	today := time.Now().Format("2006-01-02")
	if today != state.DailyDate {
		state.DailyTrades = 0
		state.DailyPnl = 0
		state.DailyDate = today
	}

	// 日最大亏损检查：优先用手动设置的 MaxDailyLoss，否则自动按 amountPerOrder * 10%
	dailyLossLimit := cfg.MaxDailyLoss
	if dailyLossLimit <= 0 {
		amt, _ := strconv.ParseFloat(cfg.AmountPerOrder, 64)
		if amt > 0 {
			dailyLossLimit = amt * 0.1 // 默认每次下单金额的 10%
		}
	}
	if dailyLossLimit > 0 && state.DailyPnl <= -dailyLossLimit {
		state.LastError = fmt.Sprintf("日亏损 %.2f 达到限额 %.2f，已暂停（次日自动恢复）", state.DailyPnl, dailyLossLimit)
		scalpMu.Unlock()
		log.Printf("[Scalp] %s: %s", cfg.Symbol, state.LastError)
		return
	}

	// 日最大交易次数
	if state.DailyTrades >= cfg.MaxDailyTrades {
		state.LastError = fmt.Sprintf("日交易次数 %d 达到限额 %d", state.DailyTrades, cfg.MaxDailyTrades)
		scalpMu.Unlock()
		return
	}

	// 冷却期
	if !state.LastTradeAt.IsZero() && time.Since(state.LastTradeAt) < time.Duration(cfg.CooldownSec)*time.Second {
		scalpMu.Unlock()
		return
	}
	scalpMu.Unlock()

	// 风控
	if err := CheckRisk(); err != nil {
		scalpMu.Lock()
		state.LastError = fmt.Sprintf("risk: %v", err)
		scalpMu.Unlock()
		return
	}

	// 资金费率过滤：极端资金费率时不顺方向开仓
	fundingRate, fundingErr := fetchScalpFundingRate(cfg.Symbol)
	if fundingErr != nil {
		log.Printf("[Scalp] Warning: fetch funding rate failed: %v", fundingErr)
	}

	// 爆仓联动过滤：最近 1 分钟爆仓金额超过阈值则暂停
	const liqSuspendThreshold = 10_000_000.0 // 1000 万 U
	if isRecentLiquidationHigh(liqSuspendThreshold) {
		scalpMu.Lock()
		state.LastError = fmt.Sprintf("最近1分钟爆仓金额超过 %.0f USDT，暂停本次决策", liqSuspendThreshold)
		scalpMu.Unlock()
		log.Printf("[Scalp] %s: %s", cfg.Symbol, state.LastError)
		return
	}

	// 拉取1分钟K线
	needKlines := cfg.EMATrend + 10
	if needKlines < 60 {
		needKlines = 60
	}
	klines, err := Client.NewKlinesService().
		Symbol(cfg.Symbol).
		Interval("1m").
		Limit(needKlines).
		Do(ctx)
	if err != nil {
		scalpMu.Lock()
		state.LastError = fmt.Sprintf("fetch klines: %v", err)
		scalpMu.Unlock()
		return
	}

	if len(klines) < cfg.EMATrend+2 {
		scalpMu.Lock()
		state.LastError = fmt.Sprintf("K线不足: %d < %d", len(klines), cfg.EMATrend+2)
		scalpMu.Unlock()
		return
	}

	// 提取数据
	closes := make([]float64, len(klines))
	volumes := make([]float64, len(klines))
	for i, k := range klines {
		closes[i], _ = strconv.ParseFloat(k.Close, 64)
		volumes[i], _ = strconv.ParseFloat(k.Volume, 64)
	}

	// 计算指标
	emaFastArr := calcEMA(closes, cfg.EMAFast)
	emaSlowArr := calcEMA(closes, cfg.EMASlow)
	emaTrendArr := calcEMA(closes, cfg.EMATrend)
	rsiArr := calcRSI(closes, cfg.RSIPeriod)

	// 计算 ATR
	atr := calcATR(klines, cfg.ATRPeriod)

	// 取最新值
	emaFast := emaFastArr[len(emaFastArr)-1]
	emaSlow := emaSlowArr[len(emaSlowArr)-1]
	emaTrend := emaTrendArr[len(emaTrendArr)-1]
	prevEmaFast := emaFastArr[len(emaFastArr)-2]
	prevEmaSlow := emaSlowArr[len(emaSlowArr)-2]

	rsi := rsiArr[len(rsiArr)-1]

	// 成交量
	currentVol := volumes[len(volumes)-1]
	avgVol := calcAvgVolume(volumes, cfg.VolumePeriod)
	volRatio := 0.0
	if avgVol > 0 {
		volRatio = currentVol / avgVol
	}

	currentPrice := closes[len(closes)-1]

	// MACD(12,26,9)
	macdVal, macdSig, macdHist := calcMACD(closes, 12, 26, 9)

	// 布林带(20,2)
	bbUpper, bbMiddle, bbLower := calcBollingerBands(closes, 20, 2.0)

	// 4H 趋势（每5分钟更新一次，用缓存避免频繁请求）
	scalpMu.Lock()
	trend4h := state.Trend4H
	scalpMu.Unlock()
	if trend4h == "" || time.Since(state.LastCheckAt) > 5*time.Minute {
		trend4h = fetch4HTrend(ctx, cfg.Symbol)
	}

	// 更新指标状态
	scalpMu.Lock()
	state.EMAFast = emaFast
	state.EMASlow = emaSlow
	state.EMATrend = emaTrend
	state.RSI = rsi
	state.VolRatio = volRatio
	state.MACD = macdVal
	state.MACDSignal = macdSig
	state.MACDHist = macdHist
	state.BBUpper = bbUpper
	state.BBMiddle = bbMiddle
	state.BBLower = bbLower
	state.ATR = atr
	state.CurrentPrice = currentPrice
	state.Trend4H = trend4h
	state.LastError = ""
	scalpMu.Unlock()

	// ========== 综合信号判断 ==========
	signal, reason := scalpDecide(cfg, currentPrice, emaFast, emaSlow, emaTrend, prevEmaFast, prevEmaSlow, rsi, volRatio, macdHist, bbUpper, bbLower, trend4h, fundingRate)

	scalpMu.Lock()
	state.Signal = signal
	state.SignalReason = reason
	if signal != "HOLD" {
		state.SignalAt = time.Now()
	}
	currentDir := state.Direction
	scalpMu.Unlock()

	log.Printf("[Scalp] %s price=%.2f EMA(%.2f/%.2f/%.2f) RSI=%.1f Vol=%.1fx → %s (pos=%s)",
		cfg.Symbol, currentPrice, emaFast, emaSlow, emaTrend, rsi, volRatio, signal, currentDir)

	// ========== 执行交易 ==========
	switch signal {
	case "BUY":
		if currentDir == "LONG" {
			return // 已持多，不加仓
		}
		if currentDir == "SHORT" {
			scalpClosePosition(ctx, state, "SHORT", "反手平空: "+reason)
		}
		scalpOpenPosition(ctx, state, "BUY", reason)

	case "SELL":
		if currentDir == "SHORT" {
			return // 已持空，不加仓
		}
		if currentDir == "LONG" {
			scalpClosePosition(ctx, state, "LONG", "反手平多: "+reason)
		}
		scalpOpenPosition(ctx, state, "SELL", reason)

	case "CLOSE":
		if currentDir != "FLAT" {
			scalpClosePosition(ctx, state, currentDir, reason)
		}
	}
}

// scalpDecide 综合信号判断（EMA + RSI + Volume + MACD + Bollinger + 4H趋势 + 资金费率）
// 返回: (signal, reason) — signal: BUY / SELL / CLOSE / HOLD
func scalpDecide(cfg ScalpConfig, price, emaFast, emaSlow, emaTrend, prevFast, prevSlow, rsi, volRatio, macdHist, bbUpper, bbLower float64, trend4h string, fundingRate float64) (string, string) {
	// 条件1: EMA 快慢线交叉
	fastAboveSlow := emaFast > emaSlow
	prevFastAboveSlow := prevFast > prevSlow
	goldenCross := fastAboveSlow && !prevFastAboveSlow
	deathCross := !fastAboveSlow && prevFastAboveSlow

	// 条件2: 趋势方向
	aboveTrend := price > emaTrend
	belowTrend := price < emaTrend

	// 条件3: RSI 过滤
	rsiAllowBuy := rsi < cfg.RSIOverbought
	rsiAllowSell := rsi > cfg.RSIOversold
	rsiStrongBuy := rsi < 40
	rsiStrongSell := rsi > 60

	// 条件4: 成交量确认
	volConfirm := volRatio >= cfg.VolumeMulti

	// 条件5: MACD 动量确认
	macdBullish := macdHist > 0
	macdBearish := macdHist < 0

	// 条件6: 布林带位置
	nearBBLower := bbLower > 0 && price <= bbLower*1.005 // 在下轨附近
	nearBBUpper := bbUpper > 0 && price >= bbUpper*0.995 // 在上轨附近

	// 条件7: 4H 趋势过滤（不逆大趋势开仓）
	trend4hAllowBuy := trend4h != "BEAR"
	trend4hAllowSell := trend4h != "BULL"

	// 条件8: 资金费率过滤
	// 极端看多（多头付空头费率）: fundingRate > 0.0003 (0.03%)，不做多
	// 极端看空（空头付多头费率）: fundingRate < -0.0003 (-0.03%)，不做空
	const fundingThreshold = 0.0003
	fundingAllowBuy := fundingRate <= fundingThreshold
	fundingAllowSell := fundingRate >= -fundingThreshold

	// === 做多信号 ===
	// 金叉 + 趋势上方 + RSI允许 + 有量 + MACD柱为正 + 4H不空 + 资金费率不极端看多
	if goldenCross && aboveTrend && rsiAllowBuy && volConfirm && macdBullish && trend4hAllowBuy && fundingAllowBuy {
		return "BUY", fmt.Sprintf("EMA金叉+趋势上方+RSI=%.1f+量比=%.1fx+MACD柱=+%.4f+4H=%s+FR=%.4f%%",
			rsi, volRatio, macdHist, trend4h, fundingRate*100)
	}
	// 多头排列 + RSI低 + 有量 + 布林带下轨反弹 + 4H不空 + 资金费率不极端看多
	if fastAboveSlow && aboveTrend && rsiStrongBuy && volConfirm && nearBBLower && trend4hAllowBuy && fundingAllowBuy {
		return "BUY", fmt.Sprintf("多头排列+RSI=%.1f+布林下轨反弹(%.2f)+量比=%.1fx+4H=%s+FR=%.4f%%",
			rsi, bbLower, volRatio, trend4h, fundingRate*100)
	}
	// 多头排列 + RSI低 + 有量 + MACD确认 + 4H不空 + 资金费率不极端看多
	if fastAboveSlow && aboveTrend && rsiStrongBuy && volConfirm && macdBullish && trend4hAllowBuy && fundingAllowBuy {
		return "BUY", fmt.Sprintf("多头排列+RSI=%.1f+MACD柱=+%.4f+量比=%.1fx+4H=%s+FR=%.4f%%",
			rsi, macdHist, volRatio, trend4h, fundingRate*100)
	}

	// === 做空信号 ===
	if deathCross && belowTrend && rsiAllowSell && volConfirm && macdBearish && trend4hAllowSell && fundingAllowSell {
		return "SELL", fmt.Sprintf("EMA死叉+趋势下方+RSI=%.1f+量比=%.1fx+MACD柱=%.4f+4H=%s+FR=%.4f%%",
			rsi, volRatio, macdHist, trend4h, fundingRate*100)
	}
	if !fastAboveSlow && belowTrend && rsiStrongSell && volConfirm && nearBBUpper && trend4hAllowSell && fundingAllowSell {
		return "SELL", fmt.Sprintf("空头排列+RSI=%.1f+布林上轨压力(%.2f)+量比=%.1fx+4H=%s+FR=%.4f%%",
			rsi, bbUpper, volRatio, trend4h, fundingRate*100)
	}
	if !fastAboveSlow && belowTrend && rsiStrongSell && volConfirm && macdBearish && trend4hAllowSell && fundingAllowSell {
		return "SELL", fmt.Sprintf("空头排列+RSI=%.1f+MACD柱=%.4f+量比=%.1fx+4H=%s+FR=%.4f%%",
			rsi, macdHist, volRatio, trend4h, fundingRate*100)
	}

	// === 平仓信号 ===
	if goldenCross {
		return "CLOSE", fmt.Sprintf("EMA金叉反转(%.2f↑%.2f)，趋势转变平仓", emaFast, emaSlow)
	}
	if deathCross {
		return "CLOSE", fmt.Sprintf("EMA死叉反转(%.2f↓%.2f)，趋势转变平仓", emaFast, emaSlow)
	}
	if rsi >= cfg.RSIOverbought+5 {
		return "CLOSE", fmt.Sprintf("RSI极度超买(%.1f)，防回撤平仓", rsi)
	}
	if rsi <= cfg.RSIOversold-5 {
		return "CLOSE", fmt.Sprintf("RSI极度超卖(%.1f)，防回撤平仓", rsi)
	}
	// 布林带极端 + MACD 反转
	if nearBBUpper && macdBearish {
		return "CLOSE", fmt.Sprintf("布林上轨(%.2f)+MACD转负(%.4f)，获利了结", bbUpper, macdHist)
	}
	if nearBBLower && macdBullish {
		return "CLOSE", fmt.Sprintf("布林下轨(%.2f)+MACD转正(%.4f)，止损反转", bbLower, macdHist)
	}

	return "HOLD", ""
}

// scalpOpenPosition 开仓
func scalpOpenPosition(ctx context.Context, state *scalpState, signal string, reason string) {
	cfg := state.Config

	var side futures.SideType
	var posSide futures.PositionSideType
	var direction string
	if signal == "BUY" {
		side = futures.SideTypeBuy
		posSide = futures.PositionSideTypeLong
		direction = "LONG"
	} else {
		side = futures.SideTypeSell
		posSide = futures.PositionSideTypeShort
		direction = "SHORT"
	}

	// 读取 ATR 和当前价格（由 scalpCheck 在锁内写入 state）
	scalpMu.Lock()
	atr := state.ATR
	currentPrice := state.CurrentPrice
	scalpMu.Unlock()

	// ATR 动态仓位计算
	amount := cfg.AmountPerOrder
	if cfg.ATRSizing && atr > 0 && currentPrice > 0 {
		baseAmt, _ := strconv.ParseFloat(cfg.AmountPerOrder, 64)
		// 基准 ATR：用价格的 0.5% 作为参考波动（近似1分钟正常波动幅度）
		refATR := currentPrice * 0.005
		if refATR > 0 {
			ratio := refATR / atr
			if ratio > 2.0 {
				ratio = 2.0 // 最多翻倍
			}
			if ratio < 0.3 {
				ratio = 0.3 // 最少保留30%
			}
			adjustedAmt := baseAmt * ratio
			amount = fmt.Sprintf("%.2f", adjustedAmt)
			log.Printf("[Scalp] ATR sizing: base=%.2f, ATR=%.4f, refATR=%.4f, ratio=%.2f, adjusted=%s USDT",
				baseAmt, atr, refATR, ratio, amount)
		}
	}

	log.Printf("[Scalp] Opening %s for %s: %s USDT × %dx",
		direction, cfg.Symbol, amount, cfg.Leverage)

	req := PlaceOrderReq{
		Source:        "strategy_scalp",
		Symbol:        cfg.Symbol,
		Side:          side,
		OrderType:     futures.OrderTypeLimit,
		PositionSide:  posSide,
		QuoteQuantity: amount,
		Leverage:      cfg.Leverage,
	}

	// 止损逻辑：优先用 MaxLossPerTrade，否则用 ATR 倍数换算为 USDT 止损额
	if cfg.MaxLossPerTrade > 0 {
		req.StopLossAmount = cfg.MaxLossPerTrade
		req.RiskReward = 2 // 默认 1:2 盈亏比
	} else if atr > 0 && currentPrice > 0 {
		// ATR 止损：止损距离 = ATR * ATRMultiplier
		slDistance := atr * cfg.ATRMultiplier
		// 按下单金额和杠杆估算名义持仓量，换算为实际账户 USDT 亏损额
		// 名义持仓量 = amount * leverage / currentPrice（币数）
		// 当价格移动 slDistance 时，账户亏损 = 币数 * slDistance / leverage（无杠杆放大的实际权益变化）
		baseAmt, _ := strconv.ParseFloat(amount, 64)
		if baseAmt > 0 {
			qty := baseAmt * float64(cfg.Leverage) / currentPrice
			slAmountUSDT := qty * slDistance / float64(cfg.Leverage)
			req.StopLossAmount = slAmountUSDT
			req.RiskReward = 2
			log.Printf("[Scalp] ATR stop-loss: ATR=%.4f, multiplier=%.1f, slDist=%.4f, slAmt=%.4f USDT",
				atr, cfg.ATRMultiplier, slDistance, slAmountUSDT)
		}
	}

	result, err := PlaceOrderViaWs(ctx, req)
	if err != nil {
		scalpMu.Lock()
		state.LastError = fmt.Sprintf("open %s failed: %v", direction, err)
		scalpMu.Unlock()
		log.Printf("[Scalp] Open failed: %v", err)
		return
	}

	scalpMu.Lock()
	state.Direction = direction
	state.OpenReason = reason
	state.CloseReason = ""
	state.DailyTrades++
	state.TotalTrades++
	state.LastTradeAt = time.Now()
	state.LastError = ""
	scalpMu.Unlock()

	log.Printf("[Scalp] Opened %s for %s: orderId=%d, price=%s, reason=%s",
		direction, cfg.Symbol, result.Order.OrderID, result.Order.AvgPrice, reason)

	NotifyTradeOpen(cfg.Symbol, direction, amount, cfg.Leverage, reason)
}

// scalpClosePosition 平仓
func scalpClosePosition(ctx context.Context, state *scalpState, direction string, reason string) {
	cfg := state.Config

	var posSide futures.PositionSideType
	if direction == "LONG" {
		posSide = futures.PositionSideTypeLong
	} else {
		posSide = futures.PositionSideTypeShort
	}

	// 获取当前持仓盈亏
	var pnl float64
	positions, err := Client.NewGetPositionRiskService().Symbol(cfg.Symbol).Do(ctx)
	if err == nil {
		for _, pos := range positions {
			if futures.PositionSideType(pos.PositionSide) == posSide {
				amt, _ := strconv.ParseFloat(pos.PositionAmt, 64)
				if amt != 0 {
					pnl, _ = strconv.ParseFloat(pos.UnRealizedProfit, 64)
				}
			}
		}
	}

	log.Printf("[Scalp] Closing %s for %s: PnL=%.4f", direction, cfg.Symbol, pnl)

	_, err = ClosePositionViaWs(ctx, ClosePositionReq{
		Symbol:       cfg.Symbol,
		PositionSide: posSide,
	})
	if err != nil {
		log.Printf("[Scalp] Close failed: %v", err)
		scalpMu.Lock()
		state.LastError = fmt.Sprintf("close %s failed: %v", direction, err)
		scalpMu.Unlock()
		return
	}

	// 取消该 symbol 的所有本地 TPSL
	if tpslMonitor != nil {
		tpslMonitor.mu.RLock()
		var groupIDs []string
		for _, cond := range tpslMonitor.conditions[cfg.Symbol] {
			found := false
			for _, gid := range groupIDs {
				if gid == cond.GroupID {
					found = true
					break
				}
			}
			if !found {
				groupIDs = append(groupIDs, cond.GroupID)
			}
		}
		tpslMonitor.mu.RUnlock()

		for _, gid := range groupIDs {
			_ = CancelTPSLByGroup(gid)
		}
	}

	tpslReason := ""
	if pnl >= 0 {
		tpslReason = fmt.Sprintf("止盈: PnL=+%.4f, %s", pnl, reason)
	} else {
		tpslReason = fmt.Sprintf("止损: PnL=%.4f, %s", pnl, reason)
	}

	scalpMu.Lock()
	state.Direction = "FLAT"
	state.CloseReason = tpslReason
	state.DailyPnl += pnl
	state.TotalPnl += pnl
	state.LastTradeAt = time.Now()
	if pnl >= 0 {
		state.WinCount++
	} else {
		state.LossCount++
	}
	scalpMu.Unlock()

	log.Printf("[Scalp] Closed %s for %s: PnL=%.4f, dailyPnl=%.4f, totalPnl=%.4f, reason=%s",
		direction, cfg.Symbol, pnl, state.DailyPnl, state.TotalPnl, tpslReason)

	NotifyTradeClose(cfg.Symbol, direction, pnl, tpslReason)
}

// ========== ATR 计算 ==========

// calcATR 使用K线的 High/Low/Close 计算简单平均真实波幅（SMA-ATR）
func calcATR(klines []*futures.Kline, period int) float64 {
	if period <= 0 {
		period = 14
	}
	if len(klines) < period+1 {
		return 0
	}
	var trSum float64
	for i := len(klines) - period; i < len(klines); i++ {
		high, _ := strconv.ParseFloat(klines[i].High, 64)
		low, _ := strconv.ParseFloat(klines[i].Low, 64)
		prevClose, _ := strconv.ParseFloat(klines[i-1].Close, 64)

		tr1 := high - low
		tr2 := math.Abs(high - prevClose)
		tr3 := math.Abs(low - prevClose)

		tr := tr1
		if tr2 > tr {
			tr = tr2
		}
		if tr3 > tr {
			tr = tr3
		}
		trSum += tr
	}
	return trSum / float64(period)
}

// ========== EMA 计算 ==========

// calcEMA 计算 EMA 序列
func calcEMA(data []float64, period int) []float64 {
	if len(data) < period {
		return data
	}

	multiplier := 2.0 / float64(period+1)
	ema := make([]float64, len(data))

	// SMA 作为初始值
	sum := 0.0
	for i := 0; i < period; i++ {
		sum += data[i]
	}
	ema[period-1] = sum / float64(period)

	// EMA 递推
	for i := period; i < len(data); i++ {
		ema[i] = (data[i]-ema[i-1])*multiplier + ema[i-1]
	}

	// 返回有效部分（从 period-1 开始）
	return ema[period-1:]
}

// calcMACD 计算 MACD(fast, slow, signal) 返回最新值
func calcMACD(closes []float64, fastPeriod, slowPeriod, signalPeriod int) (macdVal, signalVal, histVal float64) {
	if len(closes) < slowPeriod+signalPeriod {
		return 0, 0, 0
	}
	emaFast := calcEMA(closes, fastPeriod)
	emaSlow := calcEMA(closes, slowPeriod)

	diff := len(emaFast) - len(emaSlow)
	if diff > 0 {
		emaFast = emaFast[diff:]
	}

	macdLine := make([]float64, len(emaSlow))
	for i := range emaSlow {
		macdLine[i] = emaFast[i] - emaSlow[i]
	}

	if len(macdLine) < signalPeriod {
		return 0, 0, 0
	}
	signalLine := calcEMA(macdLine, signalPeriod)
	macdVal = macdLine[len(macdLine)-1]
	signalVal = signalLine[len(signalLine)-1]
	histVal = macdVal - signalVal
	return
}

// calcBollingerBands 计算布林带(period, multiplier) 返回最新 upper/middle/lower
func calcBollingerBands(closes []float64, period int, mult float64) (upper, middle, lower float64) {
	if len(closes) < period {
		return 0, 0, 0
	}
	sum := 0.0
	for i := len(closes) - period; i < len(closes); i++ {
		sum += closes[i]
	}
	middle = sum / float64(period)

	variance := 0.0
	for i := len(closes) - period; i < len(closes); i++ {
		d := closes[i] - middle
		variance += d * d
	}
	stdDev := math.Sqrt(variance / float64(period))
	upper = middle + mult*stdDev
	lower = middle - mult*stdDev
	return
}

// fetch4HTrend 获取 4H EMA 趋势方向
func fetch4HTrend(ctx context.Context, symbol string) string {
	klines, err := Client.NewKlinesService().
		Symbol(symbol).Interval("4h").Limit(30).Do(ctx)
	if err != nil || len(klines) < 22 {
		return "NEUTRAL"
	}
	closes := make([]float64, len(klines))
	for i, k := range klines {
		closes[i], _ = strconv.ParseFloat(k.Close, 64)
	}
	ema10 := calcEMA(closes, 10)
	ema20 := calcEMA(closes, 20)
	e10 := ema10[len(ema10)-1]
	e20 := ema20[len(ema20)-1]
	price := closes[len(closes)-1]

	if price > e10 && e10 > e20 {
		return "BULL"
	}
	if price < e10 && e10 < e20 {
		return "BEAR"
	}
	return "NEUTRAL"
}

// ========== Handlers ==========

// HandleStartScalp POST /tool/scalp/start
func HandleStartScalp(c context.Context, ctx *app.RequestContext) {
	var config ScalpConfig
	if err := ctx.BindAndValidate(&config); err != nil {
		ctx.JSON(http.StatusBadRequest, utils.H{"error": err.Error()})
		return
	}
	if err := StartScalp(config); err != nil {
		ctx.JSON(http.StatusBadRequest, utils.H{"error": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, utils.H{"message": "scalp strategy started", "symbol": config.Symbol})
}

// HandleStopScalp POST /tool/scalp/stop
func HandleStopScalp(c context.Context, ctx *app.RequestContext) {
	var req struct {
		Symbol string `json:"symbol"`
	}
	if err := ctx.BindAndValidate(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, utils.H{"error": err.Error()})
		return
	}
	if err := StopScalp(req.Symbol); err != nil {
		ctx.JSON(http.StatusBadRequest, utils.H{"error": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, utils.H{"message": "scalp strategy stopped", "symbol": req.Symbol})
}

// HandleScalpStatus GET /tool/scalp/status?symbol=BTCUSDT
func HandleScalpStatus(c context.Context, ctx *app.RequestContext) {
	symbol := ctx.Query("symbol")
	if symbol == "" {
		ctx.JSON(http.StatusBadRequest, utils.H{"error": "symbol is required"})
		return
	}
	status := GetScalpStatus(symbol)
	if status == nil {
		ctx.JSON(http.StatusOK, utils.H{"data": nil, "message": "no scalp strategy for " + symbol})
		return
	}
	ctx.JSON(http.StatusOK, utils.H{"data": status})
}

// ========== 资金费率 + 爆仓联动辅助函数 ==========

// fetchScalpFundingRate 获取指定 symbol 的当前资金费率（scalp 策略专用，避免与 analytics.go 中的 fetchFundingRate 冲突）
// 直接复用 analytics.go 中的 fetchFundingRate(symbol string) 函数
func fetchScalpFundingRate(symbol string) (float64, error) {
	return fetchFundingRate(symbol)
}

// isRecentLiquidationHigh 检查最近 1 分钟爆仓金额是否超过阈值
// 使用内存中的爆仓统计存储（liquidationStore），不走 DB 避免延迟
func isRecentLiquidationHigh(thresholdUSDT float64) bool {
	store := getLiqStatsStore()
	if store == nil {
		return false
	}

	now := time.Now().UTC()
	h1Start := utcHourStart(now, 1).UnixMilli()

	store.mu.RLock()
	bucket, ok := store.h1[h1Start]
	store.mu.RUnlock()

	if !ok || bucket == nil {
		return false
	}

	// 简化：用当前小时桶的总爆仓量与 60 分钟时间比较，推算 1 分钟近似量
	// 更精准做法需要分钟级桶，此处按小时桶平均估算：totalNotional / elapsed_minutes
	elapsedMinutes := now.Sub(time.UnixMilli(h1Start)).Minutes()
	if elapsedMinutes < 1 {
		elapsedMinutes = 1
	}
	minuteAvgNotional := bucket.TotalNotional / elapsedMinutes

	return minuteAvgNotional >= thresholdUSDT
}
