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

// ========== RSI + 成交量 信号策略 ==========
// 双重确认: RSI 超买超卖 + 成交量放大
// 适合中长线，用较大周期 K 线（15m/1h/4h）

// SignalConfig 信号策略配置
type SignalConfig struct {
	Symbol       string                   `json:"symbol"`
	PositionSide futures.PositionSideType `json:"positionSide,omitempty"` // 自动推断
	Leverage     int                      `json:"leverage"`

	// K 线周期: 1m, 3m, 5m, 15m, 30m, 1h, 2h, 4h, 1d
	Interval string `json:"interval"`

	// RSI 参数
	RSIPeriod      int     `json:"rsiPeriod"`      // RSI 周期，默认 14
	RSIOverbought  float64 `json:"rsiOverbought"`  // 超买阈值，默认 70
	RSIOversold    float64 `json:"rsiOversold"`     // 超卖阈值，默认 30

	// 成交量参数
	VolumePeriod int     `json:"volumePeriod"` // 成交量均线周期，默认 20
	VolumeMulti  float64 `json:"volumeMulti"`  // 成交量 > 均量 × 倍数 才确认信号，默认 1.5

	// 下单参数
	AmountPerOrder string  `json:"amountPerOrder"` // 每次投入(USDT)
	MaxPositions   int     `json:"maxPositions"`   // 最大同时持仓数，默认 1

	// 止盈止损
	StopLossPercent   float64 `json:"stopLossPercent,omitempty"`   // 止损百分比，如 2 = 2%
	TakeProfitPercent float64 `json:"takeProfitPercent,omitempty"` // 止盈百分比，如 6 = 6%

	// RSI 平仓条件（可选，不设则只按止盈止损平仓）
	RSIExitOverbought float64 `json:"rsiExitOverbought,omitempty"` // 多单 RSI 超过此值平仓，如 65
	RSIExitOversold   float64 `json:"rsiExitOversold,omitempty"`   // 空单 RSI 低于此值平仓，如 35
}

// SignalStatus 策略状态
type SignalStatus struct {
	Config       SignalConfig `json:"config"`
	Active       bool         `json:"active"`
	CurrentRSI   float64      `json:"currentRsi"`
	CurrentVol   float64      `json:"currentVol"`   // 当前成交量
	AvgVol       float64      `json:"avgVol"`        // 平均成交量
	VolRatio     float64      `json:"volRatio"`      // 当前量/均量
	LastSignal   string       `json:"lastSignal"`    // BUY / SELL / NONE
	SignalTime   string       `json:"signalTime"`    // 最近信号时间
	OpenTrades   int          `json:"openTrades"`    // 当前持仓数
	TotalTrades  int          `json:"totalTrades"`   // 总交易次数
	TotalPnl     float64      `json:"totalPnl"`      // 总盈亏
	LastError    string       `json:"lastError"`
	LastCheckAt  string       `json:"lastCheckAt"`
}

type signalState struct {
	Config      SignalConfig
	Active      bool
	CurrentRSI  float64
	CurrentVol  float64
	AvgVol      float64
	VolRatio    float64
	LastSignal  string
	SignalTime  time.Time
	OpenTrades  int
	TotalTrades int
	TotalPnl    float64
	LastError   string
	LastCheckAt time.Time
	stopC       chan struct{}
}

var (
	signalTasks = make(map[string]*signalState)
	signalMu    sync.Mutex
)

// StartSignalStrategy 启动 RSI+成交量 信号策略
func StartSignalStrategy(config SignalConfig) error {
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
	if config.RSIPeriod <= 0 {
		config.RSIPeriod = 14
	}
	if config.RSIOverbought <= 0 {
		config.RSIOverbought = 70
	}
	if config.RSIOversold <= 0 {
		config.RSIOversold = 30
	}
	if config.VolumePeriod <= 0 {
		config.VolumePeriod = 20
	}
	if config.VolumeMulti <= 0 {
		config.VolumeMulti = 1.5
	}
	if config.MaxPositions <= 0 {
		config.MaxPositions = 1
	}

	signalMu.Lock()
	defer signalMu.Unlock()

	if existing, ok := signalTasks[config.Symbol]; ok && existing.Active {
		return fmt.Errorf("signal strategy already running for %s, stop it first", config.Symbol)
	}

	state := &signalState{
		Config: config,
		Active: true,
		stopC:  make(chan struct{}),
	}
	signalTasks[config.Symbol] = state

	go signalLoop(state)

	log.Printf("[Signal] Started for %s: interval=%s, RSI(%d) ob=%.0f/os=%.0f, vol(%d) multi=%.1f",
		config.Symbol, config.Interval, config.RSIPeriod,
		config.RSIOverbought, config.RSIOversold,
		config.VolumePeriod, config.VolumeMulti)

	return nil
}

// StopSignalStrategy 停止策略
func StopSignalStrategy(symbol string) error {
	signalMu.Lock()
	defer signalMu.Unlock()

	state, ok := signalTasks[symbol]
	if !ok || !state.Active {
		return fmt.Errorf("no active signal strategy for %s", symbol)
	}

	close(state.stopC)
	state.Active = false
	log.Printf("[Signal] Stopped for %s: trades=%d, PnL=%.4f",
		symbol, state.TotalTrades, state.TotalPnl)

	return nil
}

// GetSignalStatus 获取策略状态
func GetSignalStatus(symbol string) *SignalStatus {
	signalMu.Lock()
	defer signalMu.Unlock()

	state, ok := signalTasks[symbol]
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

	return &SignalStatus{
		Config:      state.Config,
		Active:      state.Active,
		CurrentRSI:  math.Round(state.CurrentRSI*100) / 100,
		CurrentVol:  state.CurrentVol,
		AvgVol:      state.AvgVol,
		VolRatio:    math.Round(state.VolRatio*100) / 100,
		LastSignal:  state.LastSignal,
		SignalTime:  signalTime,
		OpenTrades:  state.OpenTrades,
		TotalTrades: state.TotalTrades,
		TotalPnl:    math.Round(state.TotalPnl*10000) / 10000,
		LastError:   state.LastError,
		LastCheckAt: lastCheck,
	}
}

// ========== 策略循环 ==========

func signalLoop(state *signalState) {
	cfg := state.Config
	ctx := context.Background()

	log.Printf("[Signal] Loop starting for %s", cfg.Symbol)

	// 设置杠杆
	if _, err := ChangeLeverage(ctx, cfg.Symbol, cfg.Leverage); err != nil {
		log.Printf("[Signal] Warning: set leverage failed: %v", err)
	}

	// 根据 K 线周期决定检查间隔
	checkInterval := klineToCheckInterval(cfg.Interval)
	ticker := time.NewTicker(checkInterval)
	defer ticker.Stop()

	// 首次立即检查
	signalCheck(ctx, state)

	for {
		select {
		case <-state.stopC:
			log.Printf("[Signal] Loop stopped for %s", cfg.Symbol)
			return
		case <-ticker.C:
			signalCheck(ctx, state)
		}
	}
}

// signalCheck 一次完整的信号检查
func signalCheck(ctx context.Context, state *signalState) {
	cfg := state.Config

	signalMu.Lock()
	state.LastCheckAt = time.Now()
	signalMu.Unlock()

	// 1. 拉取 K 线数据（需要 RSI 周期 + 成交量周期 + 额外几根）
	needKlines := cfg.RSIPeriod + cfg.VolumePeriod + 5
	if needKlines < 50 {
		needKlines = 50
	}

	klines, err := Client.NewKlinesService().
		Symbol(cfg.Symbol).
		Interval(cfg.Interval).
		Limit(needKlines).
		Do(ctx)
	if err != nil {
		signalMu.Lock()
		state.LastError = fmt.Sprintf("fetch klines: %v", err)
		signalMu.Unlock()
		log.Printf("[Signal] Fetch klines failed for %s: %v", cfg.Symbol, err)
		return
	}

	if len(klines) < cfg.RSIPeriod+2 {
		signalMu.Lock()
		state.LastError = fmt.Sprintf("not enough klines: got %d, need %d", len(klines), cfg.RSIPeriod+2)
		signalMu.Unlock()
		return
	}

	// 2. 提取收盘价和成交量
	closes := make([]float64, len(klines))
	volumes := make([]float64, len(klines))
	for i, k := range klines {
		closes[i], _ = strconv.ParseFloat(k.Close, 64)
		volumes[i], _ = strconv.ParseFloat(k.Volume, 64)
	}

	// 3. 计算 RSI
	rsi := calcRSI(closes, cfg.RSIPeriod)
	currentRSI := rsi[len(rsi)-1]
	prevRSI := rsi[len(rsi)-2]

	// 4. 计算成交量均值和比率
	currentVol := volumes[len(volumes)-1]
	avgVol := calcAvgVolume(volumes, cfg.VolumePeriod)
	volRatio := 0.0
	if avgVol > 0 {
		volRatio = currentVol / avgVol
	}

	// 更新状态
	signalMu.Lock()
	state.CurrentRSI = currentRSI
	state.CurrentVol = currentVol
	state.AvgVol = avgVol
	state.VolRatio = volRatio
	state.LastError = ""
	signalMu.Unlock()

	log.Printf("[Signal] %s [%s] RSI=%.2f (prev=%.2f), Vol=%.0f, AvgVol=%.0f, Ratio=%.2f",
		cfg.Symbol, cfg.Interval, currentRSI, prevRSI, currentVol, avgVol, volRatio)

	// 5. 检查是否需要平仓（RSI 反转平仓）
	signalCheckExit(ctx, state, currentRSI)

	// 6. 判断开仓信号
	volumeConfirmed := volRatio >= cfg.VolumeMulti

	signal := "NONE"

	// 做多信号: RSI 从超卖区回升 + 放量
	if prevRSI <= cfg.RSIOversold && currentRSI > cfg.RSIOversold && volumeConfirmed {
		signal = "BUY"
	}

	// 做空信号: RSI 从超买区回落 + 放量
	if prevRSI >= cfg.RSIOverbought && currentRSI < cfg.RSIOverbought && volumeConfirmed {
		signal = "SELL"
	}

	signalMu.Lock()
	state.LastSignal = signal
	if signal != "NONE" {
		state.SignalTime = time.Now()
	}
	signalMu.Unlock()

	if signal == "NONE" {
		return
	}

	// 7. 检查持仓数限制
	signalMu.Lock()
	openTrades := state.OpenTrades
	signalMu.Unlock()

	if openTrades >= cfg.MaxPositions {
		log.Printf("[Signal] Signal %s ignored: max positions reached (%d/%d)",
			signal, openTrades, cfg.MaxPositions)
		return
	}

	// 8. 风控检查
	if err := CheckRisk(); err != nil {
		signalMu.Lock()
		state.LastError = fmt.Sprintf("risk blocked: %v", err)
		signalMu.Unlock()
		log.Printf("[Signal] Risk blocked: %v", err)
		return
	}

	// 9. 执行开仓
	signalOpenPosition(ctx, state, signal)
}

// signalOpenPosition 根据信号开仓
func signalOpenPosition(ctx context.Context, state *signalState, signal string) {
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

	log.Printf("[Signal] Opening %s position for %s: amount=%s USDT, leverage=%dx",
		signal, cfg.Symbol, cfg.AmountPerOrder, cfg.Leverage)

	req := PlaceOrderReq{
		Symbol:        cfg.Symbol,
		Side:          side,
		OrderType:     futures.OrderTypeMarket,
		PositionSide:  posSide,
		QuoteQuantity: cfg.AmountPerOrder,
		Leverage:      cfg.Leverage,
	}

	// 如果设置了止盈止损百分比，用金额方式换算
	if cfg.StopLossPercent > 0 && cfg.TakeProfitPercent > 0 {
		amtFloat, _ := strconv.ParseFloat(cfg.AmountPerOrder, 64)
		slAmount := amtFloat * cfg.StopLossPercent / 100
		rr := cfg.TakeProfitPercent / cfg.StopLossPercent
		req.StopLossAmount = slAmount
		req.RiskReward = rr
	}

	result, err := PlaceOrderViaWs(ctx, req)
	if err != nil {
		signalMu.Lock()
		state.LastError = fmt.Sprintf("open failed: %v", err)
		signalMu.Unlock()
		log.Printf("[Signal] Open position failed: %v", err)
		return
	}

	signalMu.Lock()
	state.OpenTrades++
	state.TotalTrades++
	state.LastError = ""
	signalMu.Unlock()

	log.Printf("[Signal] Opened %s for %s: orderId=%d, price=%s",
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
			log.Printf("[Signal] Save trade record failed: %v", err)
		}
	}()
}

// signalCheckExit 检查 RSI 平仓条件
func signalCheckExit(ctx context.Context, state *signalState, currentRSI float64) {
	cfg := state.Config

	// 没有设置 RSI 平仓条件
	if cfg.RSIExitOverbought <= 0 && cfg.RSIExitOversold <= 0 {
		return
	}

	// 查询当前持仓
	positions, err := Client.NewGetPositionRiskService().Symbol(cfg.Symbol).Do(ctx)
	if err != nil {
		return
	}

	for _, pos := range positions {
		posAmt, _ := strconv.ParseFloat(pos.PositionAmt, 64)
		if posAmt == 0 {
			continue
		}

		posSide := futures.PositionSideType(pos.PositionSide)
		pnl, _ := strconv.ParseFloat(pos.UnRealizedProfit, 64)

		shouldClose := false
		reason := ""

		// 多仓: RSI 超买区平仓
		if posAmt > 0 && cfg.RSIExitOverbought > 0 && currentRSI >= cfg.RSIExitOverbought {
			shouldClose = true
			reason = fmt.Sprintf("RSI=%.2f >= %.0f (overbought exit)", currentRSI, cfg.RSIExitOverbought)
		}

		// 空仓: RSI 超卖区平仓
		if posAmt < 0 && cfg.RSIExitOversold > 0 && currentRSI <= cfg.RSIExitOversold {
			shouldClose = true
			reason = fmt.Sprintf("RSI=%.2f <= %.0f (oversold exit)", currentRSI, cfg.RSIExitOversold)
		}

		if !shouldClose {
			continue
		}

		log.Printf("[Signal] Closing %s position for %s: %s, PnL=%.4f",
			posSide, cfg.Symbol, reason, pnl)

		_, err := ClosePositionViaWs(ctx, ClosePositionReq{
			Symbol:       cfg.Symbol,
			PositionSide: posSide,
		})
		if err != nil {
			log.Printf("[Signal] Close position failed: %v", err)
			continue
		}

		signalMu.Lock()
		state.OpenTrades--
		if state.OpenTrades < 0 {
			state.OpenTrades = 0
		}
		state.TotalPnl += pnl
		signalMu.Unlock()

		log.Printf("[Signal] Closed %s for %s: PnL=%.4f, totalPnl=%.4f",
			posSide, cfg.Symbol, pnl, state.TotalPnl)
	}
}

// ========== 技术指标计算 ==========

// calcRSI 计算 RSI 序列
// 返回长度 = len(closes) - period 的 RSI 数组
func calcRSI(closes []float64, period int) []float64 {
	if len(closes) < period+1 {
		return []float64{50} // 数据不足，返回中性值
	}

	// 计算价格变动
	changes := make([]float64, len(closes)-1)
	for i := 1; i < len(closes); i++ {
		changes[i-1] = closes[i] - closes[i-1]
	}

	// 初始平均涨幅/跌幅（SMA）
	var avgGain, avgLoss float64
	for i := 0; i < period; i++ {
		if changes[i] > 0 {
			avgGain += changes[i]
		} else {
			avgLoss += math.Abs(changes[i])
		}
	}
	avgGain /= float64(period)
	avgLoss /= float64(period)

	// 计算 RSI 序列（Wilder's Smoothing / EMA）
	rsiValues := make([]float64, 0, len(changes)-period+1)

	if avgLoss == 0 {
		rsiValues = append(rsiValues, 100)
	} else {
		rs := avgGain / avgLoss
		rsiValues = append(rsiValues, 100-100/(1+rs))
	}

	// 后续使用 EMA 平滑
	for i := period; i < len(changes); i++ {
		gain := 0.0
		loss := 0.0
		if changes[i] > 0 {
			gain = changes[i]
		} else {
			loss = math.Abs(changes[i])
		}

		avgGain = (avgGain*float64(period-1) + gain) / float64(period)
		avgLoss = (avgLoss*float64(period-1) + loss) / float64(period)

		if avgLoss == 0 {
			rsiValues = append(rsiValues, 100)
		} else {
			rs := avgGain / avgLoss
			rsiValues = append(rsiValues, 100-100/(1+rs))
		}
	}

	return rsiValues
}

// calcAvgVolume 计算最近 period 根 K 线的平均成交量（不含最新一根）
func calcAvgVolume(volumes []float64, period int) float64 {
	n := len(volumes)
	if n < period+1 {
		period = n - 1
	}
	if period <= 0 {
		return 0
	}

	sum := 0.0
	// 从倒数第2根往前取 period 根
	for i := n - 2; i >= n-1-period && i >= 0; i-- {
		sum += volumes[i]
	}
	return sum / float64(period)
}

// klineToCheckInterval K 线周期 → 检查间隔
// 每根 K 线结束时检查一次，加 5 秒等 K 线完全关闭
func klineToCheckInterval(interval string) time.Duration {
	switch interval {
	case "1m":
		return 1 * time.Minute
	case "3m":
		return 3 * time.Minute
	case "5m":
		return 5 * time.Minute
	case "15m":
		return 15 * time.Minute
	case "30m":
		return 30 * time.Minute
	case "1h":
		return 1 * time.Hour
	case "2h":
		return 2 * time.Hour
	case "4h":
		return 4 * time.Hour
	case "1d":
		return 24 * time.Hour
	default:
		return 15 * time.Minute
	}
}
