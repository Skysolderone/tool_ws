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

// ========== K线形态（十字星）独立策略 ==========
// 监听十字星、锤子线、射击之星等反转 K 线形态
// 结合历史 K 线趋势方向判断做多/做空
// 可选叠加 RSI 过滤，减少假信号

// PatternType K线形态类型
type PatternType string

const (
	PatternNone         PatternType = "NONE"
	PatternDoji         PatternType = "DOJI"         // 十字星：实体极小
	PatternHammer       PatternType = "HAMMER"        // 锤子线：下影线长，实体在上部（看涨）
	PatternShootingStar PatternType = "SHOOTING_STAR" // 射击之星：上影线长，实体在下部（看跌）
	PatternEngulfBull   PatternType = "ENGULF_BULL"   // 看涨吞没：阳线吞没前一阴线
	PatternEngulfBear   PatternType = "ENGULF_BEAR"   // 看跌吞没：阴线吞没前一阳线
)

// DojiConfig K线形态策略配置
type DojiConfig struct {
	Symbol   string `json:"symbol"`
	Leverage int    `json:"leverage"`

	// K线周期
	Interval string `json:"interval"` // 1m, 5m, 15m, 30m, 1h, 4h

	// 形态参数
	BodyRatio     float64 `json:"bodyRatio"`     // 十字星: 实体/全长 <= 此值视为十字星，默认 0.1 (10%)
	ShadowRatio   float64 `json:"shadowRatio"`   // 锤子/射击之星: 影线/实体 >= 此值，默认 2.0
	EnableDoji    bool    `json:"enableDoji"`    // 启用十字星，默认 true
	EnableHammer  bool    `json:"enableHammer"`  // 启用锤子线/射击之星，默认 true
	EnableEngulf  bool    `json:"enableEngulf"`  // 启用吞没形态，默认 true

	// 趋势确认
	TrendBars     int     `json:"trendBars"`     // 用前 N 根 K 线判断趋势，默认 5
	TrendStrength float64 `json:"trendStrength"` // 趋势最小涨跌幅(%)，默认 0.3

	// 可选 RSI 过滤
	EnableRSI      bool    `json:"enableRsi"`      // 是否启用 RSI 辅助过滤，默认 false
	RSIPeriod      int     `json:"rsiPeriod"`      // RSI 周期，默认 14
	RSIOverbought  float64 `json:"rsiOverbought"`  // 空信号需 RSI >= 此值，默认 65
	RSIOversold    float64 `json:"rsiOversold"`    // 多信号需 RSI <= 此值，默认 35

	// 成交量过滤
	EnableVolume  bool    `json:"enableVolume"`  // 是否启用成交量过滤，默认 false
	VolumePeriod  int     `json:"volumePeriod"`  // 均量周期，默认 20
	VolumeMulti   float64 `json:"volumeMulti"`   // 量比阈值，默认 1.2

	// 下单参数
	AmountPerOrder string `json:"amountPerOrder"` // 每次投入(USDT)
	MaxPositions   int    `json:"maxPositions"`   // 最大同时持仓数，默认 1

	// 止盈止损
	StopLossPercent   float64 `json:"stopLossPercent,omitempty"`   // 止损百分比
	TakeProfitPercent float64 `json:"takeProfitPercent,omitempty"` // 止盈百分比
}

// DojiStatus 策略状态（返回前端）
type DojiStatus struct {
	Config       DojiConfig  `json:"config"`
	Active       bool        `json:"active"`
	LastPattern  string      `json:"lastPattern"`  // 最近识别的形态
	TrendDir     string      `json:"trendDir"`     // UP / DOWN / FLAT
	LastSignal   string      `json:"lastSignal"`   // BUY / SELL / NONE
	SignalTime   string      `json:"signalTime"`
	CurrentRSI   float64     `json:"currentRsi,omitempty"`
	VolRatio     float64     `json:"volRatio,omitempty"`
	OpenTrades   int         `json:"openTrades"`
	TotalTrades  int         `json:"totalTrades"`
	TotalPnl     float64     `json:"totalPnl"`
	LastError    string      `json:"lastError"`
	LastCheckAt  string      `json:"lastCheckAt"`
}

type dojiState struct {
	Config      DojiConfig
	Active      bool
	LastPattern PatternType
	TrendDir    string // UP / DOWN / FLAT
	LastSignal  string
	SignalTime  time.Time
	CurrentRSI  float64
	VolRatio    float64
	OpenTrades  int
	TotalTrades int
	TotalPnl    float64
	LastError   string
	LastCheckAt time.Time
	stopC       chan struct{}
}

var (
	dojiTasks = make(map[string]*dojiState)
	dojiMu    sync.Mutex
)

// StartDojiStrategy 启动 K 线形态策略
func StartDojiStrategy(config DojiConfig) error {
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
	if config.Interval == "" {
		config.Interval = "15m"
	}
	if config.BodyRatio <= 0 {
		config.BodyRatio = 0.1
	}
	if config.ShadowRatio <= 0 {
		config.ShadowRatio = 2.0
	}
	if config.TrendBars <= 0 {
		config.TrendBars = 5
	}
	if config.TrendStrength <= 0 {
		config.TrendStrength = 0.3
	}
	if config.MaxPositions <= 0 {
		config.MaxPositions = 1
	}
	// 形态默认全部启用
	if !config.EnableDoji && !config.EnableHammer && !config.EnableEngulf {
		config.EnableDoji = true
		config.EnableHammer = true
		config.EnableEngulf = true
	}
	// RSI 默认值
	if config.RSIPeriod <= 0 {
		config.RSIPeriod = 14
	}
	if config.RSIOverbought <= 0 {
		config.RSIOverbought = 65
	}
	if config.RSIOversold <= 0 {
		config.RSIOversold = 35
	}
	// 成交量默认值
	if config.VolumePeriod <= 0 {
		config.VolumePeriod = 20
	}
	if config.VolumeMulti <= 0 {
		config.VolumeMulti = 1.2
	}

	dojiMu.Lock()
	defer dojiMu.Unlock()

	if existing, ok := dojiTasks[config.Symbol]; ok && existing.Active {
		return fmt.Errorf("doji strategy already running for %s, stop it first", config.Symbol)
	}

	state := &dojiState{
		Config: config,
		Active: true,
		stopC:  make(chan struct{}),
	}
	dojiTasks[config.Symbol] = state

	go dojiLoop(state)

	log.Printf("[Doji] Started for %s: interval=%s, bodyRatio=%.2f, trendBars=%d, RSI=%v, Vol=%v",
		config.Symbol, config.Interval, config.BodyRatio, config.TrendBars,
		config.EnableRSI, config.EnableVolume)

	return nil
}

// StopDojiStrategy 停止策略
func StopDojiStrategy(symbol string) error {
	dojiMu.Lock()
	defer dojiMu.Unlock()

	state, ok := dojiTasks[symbol]
	if !ok || !state.Active {
		return fmt.Errorf("no active doji strategy for %s", symbol)
	}

	close(state.stopC)
	state.Active = false
	log.Printf("[Doji] Stopped for %s: trades=%d, PnL=%.4f",
		symbol, state.TotalTrades, state.TotalPnl)

	return nil
}

// GetDojiStatus 获取策略状态
func GetDojiStatus(symbol string) *DojiStatus {
	dojiMu.Lock()
	defer dojiMu.Unlock()

	state, ok := dojiTasks[symbol]
	if !ok {
		return nil
	}

	signalTime := ""
	if !state.SignalTime.IsZero() {
		signalTime = state.SignalTime.Format("15:04:05")
	}
	lastCheck := ""
	if !state.LastCheckAt.IsZero() {
		lastCheck = state.LastCheckAt.Format("15:04:05")
	}

	return &DojiStatus{
		Config:      state.Config,
		Active:      state.Active,
		LastPattern: string(state.LastPattern),
		TrendDir:    state.TrendDir,
		LastSignal:  state.LastSignal,
		SignalTime:  signalTime,
		CurrentRSI:  math.Round(state.CurrentRSI*100) / 100,
		VolRatio:    math.Round(state.VolRatio*100) / 100,
		OpenTrades:  state.OpenTrades,
		TotalTrades: state.TotalTrades,
		TotalPnl:    math.Round(state.TotalPnl*10000) / 10000,
		LastError:   state.LastError,
		LastCheckAt: lastCheck,
	}
}

// ========== 策略循环 ==========

func dojiLoop(state *dojiState) {
	cfg := state.Config
	ctx := context.Background()

	log.Printf("[Doji] Loop starting for %s", cfg.Symbol)

	// 设置杠杆
	if _, err := ChangeLeverage(ctx, cfg.Symbol, cfg.Leverage); err != nil {
		log.Printf("[Doji] Warning: set leverage failed: %v", err)
	}

	checkInterval := klineToCheckInterval(cfg.Interval)
	ticker := time.NewTicker(checkInterval)
	defer ticker.Stop()

	// 首次立即检查
	dojiCheck(ctx, state)

	for {
		select {
		case <-state.stopC:
			log.Printf("[Doji] Loop stopped for %s", cfg.Symbol)
			return
		case <-ticker.C:
			dojiCheck(ctx, state)
		}
	}
}

// dojiCheck 一次完整的形态检查
func dojiCheck(ctx context.Context, state *dojiState) {
	cfg := state.Config

	dojiMu.Lock()
	state.LastCheckAt = time.Now()
	dojiMu.Unlock()

	// 1. 拉取 K 线（需要足够的历史数据）
	needKlines := cfg.TrendBars + 5
	if cfg.EnableRSI && cfg.RSIPeriod+5 > needKlines {
		needKlines = cfg.RSIPeriod + 5
	}
	if cfg.EnableVolume && cfg.VolumePeriod+5 > needKlines {
		needKlines = cfg.VolumePeriod + 5
	}
	if needKlines < 30 {
		needKlines = 30
	}

	klines, err := Client.NewKlinesService().
		Symbol(cfg.Symbol).
		Interval(cfg.Interval).
		Limit(needKlines).
		Do(ctx)
	if err != nil {
		dojiMu.Lock()
		state.LastError = fmt.Sprintf("fetch klines: %v", err)
		dojiMu.Unlock()
		log.Printf("[Doji] Fetch klines failed for %s: %v", cfg.Symbol, err)
		return
	}

	if len(klines) < cfg.TrendBars+2 {
		dojiMu.Lock()
		state.LastError = fmt.Sprintf("not enough klines: got %d", len(klines))
		dojiMu.Unlock()
		return
	}

	// 2. 提取 OHLCV 数据
	n := len(klines)
	opens := make([]float64, n)
	highs := make([]float64, n)
	lows := make([]float64, n)
	closes := make([]float64, n)
	volumes := make([]float64, n)
	for i, k := range klines {
		opens[i], _ = strconv.ParseFloat(k.Open, 64)
		highs[i], _ = strconv.ParseFloat(k.High, 64)
		lows[i], _ = strconv.ParseFloat(k.Low, 64)
		closes[i], _ = strconv.ParseFloat(k.Close, 64)
		volumes[i], _ = strconv.ParseFloat(k.Volume, 64)
	}

	// 使用倒数第2根已完成K线做形态分析（最新一根可能未收盘）
	idx := n - 2
	if idx < 1 {
		return
	}

	// 3. 识别K线形态
	pattern := detectPattern(cfg, opens, highs, lows, closes, idx)

	// 4. 判断趋势方向
	trendDir := detectTrend(closes, idx, cfg.TrendBars, cfg.TrendStrength)

	// 5. 可选 RSI 计算
	var currentRSI float64
	if cfg.EnableRSI {
		rsiValues := calcRSI(closes[:idx+1], cfg.RSIPeriod)
		if len(rsiValues) > 0 {
			currentRSI = rsiValues[len(rsiValues)-1]
		}
	}

	// 6. 可选成交量计算
	var volRatio float64
	if cfg.EnableVolume {
		avgVol := calcAvgVolume(volumes[:idx+1], cfg.VolumePeriod)
		if avgVol > 0 {
			volRatio = volumes[idx] / avgVol
		}
	}

	// 更新状态
	dojiMu.Lock()
	state.LastPattern = pattern
	state.TrendDir = trendDir
	state.CurrentRSI = currentRSI
	state.VolRatio = volRatio
	state.LastError = ""
	dojiMu.Unlock()

	log.Printf("[Doji] %s [%s] pattern=%s, trend=%s, RSI=%.2f, volRatio=%.2f",
		cfg.Symbol, cfg.Interval, pattern, trendDir, currentRSI, volRatio)

	// 7. 无形态则跳过
	if pattern == PatternNone {
		return
	}

	// 8. 根据形态+趋势判断信号
	signal := dojiSignalFromPattern(pattern, trendDir)
	if signal == "NONE" {
		log.Printf("[Doji] Pattern %s but trend %s, no confirmed signal", pattern, trendDir)
		return
	}

	// 9. RSI 过滤
	if cfg.EnableRSI {
		if signal == "BUY" && currentRSI > cfg.RSIOversold {
			log.Printf("[Doji] BUY signal filtered: RSI=%.2f > %.0f", currentRSI, cfg.RSIOversold)
			return
		}
		if signal == "SELL" && currentRSI < cfg.RSIOverbought {
			log.Printf("[Doji] SELL signal filtered: RSI=%.2f < %.0f", currentRSI, cfg.RSIOverbought)
			return
		}
	}

	// 10. 成交量过滤
	if cfg.EnableVolume && volRatio < cfg.VolumeMulti {
		log.Printf("[Doji] Signal filtered: volRatio=%.2f < %.2f", volRatio, cfg.VolumeMulti)
		return
	}

	dojiMu.Lock()
	state.LastSignal = signal
	state.SignalTime = time.Now()
	dojiMu.Unlock()

	// 11. 持仓限制
	dojiMu.Lock()
	openTrades := state.OpenTrades
	dojiMu.Unlock()

	if openTrades >= cfg.MaxPositions {
		log.Printf("[Doji] Signal %s ignored: max positions reached (%d/%d)",
			signal, openTrades, cfg.MaxPositions)
		return
	}

	// 12. 风控检查
	if err := CheckRisk(); err != nil {
		dojiMu.Lock()
		state.LastError = fmt.Sprintf("risk blocked: %v", err)
		dojiMu.Unlock()
		log.Printf("[Doji] Risk blocked: %v", err)
		return
	}

	// 13. 执行开仓
	dojiOpenPosition(ctx, state, signal)
}

// ========== 形态识别 ==========

// detectPattern 检测最新K线的形态
func detectPattern(cfg DojiConfig, opens, highs, lows, closes []float64, idx int) PatternType {
	o := opens[idx]
	h := highs[idx]
	l := lows[idx]
	c := closes[idx]

	body := math.Abs(c - o)
	fullRange := h - l

	if fullRange <= 0 {
		return PatternNone
	}

	bodyRatio := body / fullRange

	// === 十字星 ===
	if cfg.EnableDoji && bodyRatio <= cfg.BodyRatio {
		return PatternDoji
	}

	// === 锤子线 / 射击之星 ===
	if cfg.EnableHammer && body > 0 {
		realBody := body
		upperShadow := h - math.Max(o, c)
		lowerShadow := math.Min(o, c) - l

		// 锤子线：下影线长，上影线短，实体在上部
		if lowerShadow/realBody >= cfg.ShadowRatio && upperShadow < realBody {
			return PatternHammer
		}

		// 射击之星：上影线长，下影线短，实体在下部
		if upperShadow/realBody >= cfg.ShadowRatio && lowerShadow < realBody {
			return PatternShootingStar
		}
	}

	// === 吞没形态 ===
	if cfg.EnableEngulf && idx >= 1 {
		prevO := opens[idx-1]
		prevC := closes[idx-1]
		prevBody := math.Abs(prevC - prevO)

		if prevBody > 0 && body > prevBody {
			// 看涨吞没：前一根阴线，当前阳线完全包裹
			if prevC < prevO && c > o && c > prevO && o <= prevC {
				return PatternEngulfBull
			}
			// 看跌吞没：前一根阳线，当前阴线完全包裹
			if prevC > prevO && c < o && o > prevC && c <= prevO {
				return PatternEngulfBear
			}
		}
	}

	return PatternNone
}

// detectTrend 用前 N 根 K 线的收盘价判断趋势方向
// 返回 "UP" / "DOWN" / "FLAT"
func detectTrend(closes []float64, currentIdx int, bars int, strengthPct float64) string {
	startIdx := currentIdx - bars
	if startIdx < 0 {
		startIdx = 0
	}

	startPrice := closes[startIdx]
	endPrice := closes[currentIdx]

	if startPrice <= 0 {
		return "FLAT"
	}

	changePct := (endPrice - startPrice) / startPrice * 100

	if changePct >= strengthPct {
		return "UP"
	}
	if changePct <= -strengthPct {
		return "DOWN"
	}
	return "FLAT"
}

// dojiSignalFromPattern 根据形态+趋势推导信号
// 核心逻辑：反转形态出现在趋势末端
func dojiSignalFromPattern(pattern PatternType, trend string) string {
	switch pattern {
	case PatternDoji:
		// 十字星：犹豫形态，在上涨末端做空，在下跌末端做多
		if trend == "UP" {
			return "SELL"
		}
		if trend == "DOWN" {
			return "BUY"
		}
		return "NONE" // FLAT 不确认

	case PatternHammer:
		// 锤子线：经典底部反转信号，下跌趋势中做多
		if trend == "DOWN" {
			return "BUY"
		}
		return "NONE"

	case PatternShootingStar:
		// 射击之星：经典顶部反转信号，上涨趋势中做空
		if trend == "UP" {
			return "SELL"
		}
		return "NONE"

	case PatternEngulfBull:
		// 看涨吞没：下跌趋势中做多
		if trend == "DOWN" {
			return "BUY"
		}
		return "NONE"

	case PatternEngulfBear:
		// 看跌吞没：上涨趋势中做空
		if trend == "UP" {
			return "SELL"
		}
		return "NONE"
	}

	return "NONE"
}

// ========== 开仓执行 ==========

func dojiOpenPosition(ctx context.Context, state *dojiState, signal string) {
	cfg := state.Config

	var side futures.SideType
	var posSide futures.PositionSideType
	if signal == "BUY" {
		side = futures.SideTypeBuy
		posSide = futures.PositionSideTypeLong
	} else {
		side = futures.SideTypeSell
		posSide = futures.PositionSideTypeShort
	}

	log.Printf("[Doji] Opening %s position for %s: pattern=%s, trend=%s, amount=%s USDT",
		signal, cfg.Symbol, state.LastPattern, state.TrendDir, cfg.AmountPerOrder)

	req := PlaceOrderReq{
		Symbol:        cfg.Symbol,
		Side:          side,
		OrderType:     futures.OrderTypeMarket,
		PositionSide:  posSide,
		QuoteQuantity: cfg.AmountPerOrder,
		Leverage:      cfg.Leverage,
	}

	// 止盈止损
	if cfg.StopLossPercent > 0 && cfg.TakeProfitPercent > 0 {
		amtFloat, _ := strconv.ParseFloat(cfg.AmountPerOrder, 64)
		slAmount := amtFloat * cfg.StopLossPercent / 100
		rr := cfg.TakeProfitPercent / cfg.StopLossPercent
		req.StopLossAmount = slAmount
		req.RiskReward = rr
	}

	result, err := PlaceOrderViaWs(ctx, req)
	if err != nil {
		dojiMu.Lock()
		state.LastError = fmt.Sprintf("open failed: %v", err)
		dojiMu.Unlock()
		log.Printf("[Doji] Open position failed: %v", err)
		return
	}

	dojiMu.Lock()
	state.OpenTrades++
	state.TotalTrades++
	state.LastError = ""
	dojiMu.Unlock()

	log.Printf("[Doji] Opened %s for %s: orderId=%d, price=%s",
		signal, cfg.Symbol, result.Order.OrderID, result.Order.AvgPrice)

	// 异步保存交易记录
	go func() {
		if result.Order == nil {
			return
		}
		record := &TradeRecord{
			Symbol:        cfg.Symbol,
			Side:          string(side),
			PositionSide:  string(posSide),
			OrderType:     "MARKET",
			OrderID:       result.Order.OrderID,
			Quantity:      result.Order.OrigQuantity,
			Price:         result.Order.AvgPrice,
			QuoteQuantity: cfg.AmountPerOrder,
			Leverage:      cfg.Leverage,
			Status:        "OPEN",
		}
		if result.TakeProfit != nil {
			record.TakeProfitPrice = result.TakeProfit.TriggerPrice
			record.TakeProfitAlgoID = result.TakeProfit.AlgoID
		}
		if result.StopLoss != nil {
			record.StopLossPrice = result.StopLoss.TriggerPrice
			record.StopLossAlgoID = result.StopLoss.AlgoID
		}
		if err := SaveTradeRecord(record); err != nil {
			log.Printf("[Doji] Save trade record failed: %v", err)
		}
	}()
}
