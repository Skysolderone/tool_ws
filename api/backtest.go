package api

import (
	"context"
	"fmt"
	"log"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/adshao/go-binance/v2/futures"
	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/utils"
)

// ========== 回测系统 ==========
// 拉取历史 1m K 线，滑动窗口回放 scalpDecide 逻辑，统计胜率/盈亏比/最大回撤

// BacktestConfig 回测参数
type BacktestConfig struct {
	Symbol     string  `json:"symbol"`
	Days       int     `json:"days"`       // 回测天数，默认 7
	Leverage   int     `json:"leverage"`   // 杠杆，默认 10
	Amount     float64 `json:"amount"`     // 每笔名义金额（USDT），默认 100
	EMAFast    int     `json:"emaFast"`    // EMA 快线周期，默认 7
	EMASlow    int     `json:"emaSlow"`    // EMA 慢线周期，默认 21
	EMATrend   int     `json:"emaTrend"`   // EMA 趋势线周期，默认 50
	RSIPeriod  int     `json:"rsiPeriod"`  // RSI 周期，默认 6
	RSIOverbought float64 `json:"rsiOverbought"` // RSI 超买，默认 75
	RSIOversold   float64 `json:"rsiOversold"`   // RSI 超卖，默认 25
	VolumePeriod  int     `json:"volumePeriod"`  // 量均线周期，默认 10
	VolumeMulti   float64 `json:"volumeMulti"`   // 量比阈值，默认 1.2
	ATRPeriod     int     `json:"atrPeriod"`     // ATR 周期，默认 14
	ATRMultiplier float64 `json:"atrMultiplier"` // ATR 止损倍数，默认 1.5
}

// BacktestTrade 单笔回测交易记录
type BacktestTrade struct {
	OpenTime   string  `json:"openTime"`
	CloseTime  string  `json:"closeTime"`
	Side       string  `json:"side"`       // LONG / SHORT
	EntryPrice float64 `json:"entryPrice"`
	ExitPrice  float64 `json:"exitPrice"`
	Quantity   float64 `json:"quantity"`   // 合约数量（按杠杆计算）
	PnL        float64 `json:"pnl"`        // 盈亏 USDT
	OpenReason string  `json:"openReason"`
	CloseReason string `json:"closeReason"`
}

// BacktestResult 回测结果汇总
type BacktestResult struct {
	Symbol       string          `json:"symbol"`
	Period       string          `json:"period"`
	TotalKlines  int             `json:"totalKlines"`
	TotalTrades  int             `json:"totalTrades"`
	WinCount     int             `json:"winCount"`
	LossCount    int             `json:"lossCount"`
	WinRate      float64         `json:"winRate"`      // 胜率 0~1
	TotalPnL     float64         `json:"totalPnl"`     // 总盈亏 USDT
	MaxDrawdown  float64         `json:"maxDrawdown"`  // 最大回撤 USDT
	ProfitFactor float64         `json:"profitFactor"` // 盈利因子 = 总盈利 / 总亏损
	AvgWin       float64         `json:"avgWin"`       // 平均盈利 USDT
	AvgLoss      float64         `json:"avgLoss"`      // 平均亏损 USDT（正数）
	RiskReward   float64         `json:"riskReward"`   // 盈亏比 avgWin / avgLoss
	Trades       []BacktestTrade `json:"trades"`
}

// fetchHistoricalKlines 分批拉取历史 1m K 线，最多每次 1500 根
// 币安 API: startTime / endTime 用毫秒时间戳
func fetchHistoricalKlines(ctx context.Context, symbol string, days int) ([]*futures.Kline, error) {
	const batchSize = 1500          // 每次最多 1500 根
	const intervalMs = 60_000       // 1 分钟 = 60000ms

	endTime := time.Now().UnixMilli()
	startTime := time.Now().AddDate(0, 0, -days).UnixMilli()

	var allKlines []*futures.Kline
	batchStart := startTime

	for batchStart < endTime {
		batchEnd := batchStart + int64(batchSize)*intervalMs
		if batchEnd > endTime {
			batchEnd = endTime
		}

		log.Printf("[Backtest] 拉取 K 线 %s: %s ~ %s",
			symbol,
			time.UnixMilli(batchStart).Format("2006-01-02 15:04"),
			time.UnixMilli(batchEnd).Format("2006-01-02 15:04"),
		)

		klines, err := Client.NewKlinesService().
			Symbol(symbol).
			Interval("1m").
			StartTime(batchStart).
			EndTime(batchEnd).
			Limit(batchSize).
			Do(ctx)
		if err != nil {
			return nil, fmt.Errorf("拉取 K 线失败 (%s ~ %s): %w",
				time.UnixMilli(batchStart).Format("2006-01-02 15:04"),
				time.UnixMilli(batchEnd).Format("2006-01-02 15:04"),
				err,
			)
		}

		if len(klines) == 0 {
			break
		}

		allKlines = append(allKlines, klines...)

		// 下一批从最后一根的 CloseTime + 1ms 开始
		lastClose := klines[len(klines)-1].CloseTime
		batchStart = lastClose + 1

		// 防止 API 速率限制，短暂等待
		time.Sleep(200 * time.Millisecond)
	}

	log.Printf("[Backtest] 共拉取 %d 根 1m K 线（%s, %d 天）", len(allKlines), symbol, days)
	return allKlines, nil
}

// calcAvgVolumeSlice 计算 volumes[0:n] 末尾 period 根的平均成交量
func calcAvgVolumeSlice(volumes []float64, period int) float64 {
	n := len(volumes)
	if n == 0 || period <= 0 {
		return 0
	}
	start := n - period
	if start < 0 {
		start = 0
	}
	sum := 0.0
	cnt := 0
	for i := start; i < n; i++ {
		sum += volumes[i]
		cnt++
	}
	if cnt == 0 {
		return 0
	}
	return sum / float64(cnt)
}

// calcRSI 已在 signal_strategy.go 中定义，此处复用

// calcATRSlice 使用 klines[:n+1] 的最后 period 根计算 ATR
func calcATRSlice(highs, lows, closes []float64, period int) float64 {
	n := len(closes)
	if period <= 0 {
		period = 14
	}
	if n < period+1 {
		return 0
	}

	var trSum float64
	for i := n - period; i < n; i++ {
		h := highs[i]
		l := lows[i]
		prevC := closes[i-1]

		tr1 := h - l
		tr2 := math.Abs(h - prevC)
		tr3 := math.Abs(l - prevC)

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

// RunBacktest 执行回测，返回统计结果
func RunBacktest(cfg BacktestConfig) (*BacktestResult, error) {
	// 填充默认值
	if cfg.Symbol == "" {
		return nil, fmt.Errorf("symbol is required")
	}
	if cfg.Days <= 0 {
		cfg.Days = 7
	}
	if cfg.Leverage <= 0 {
		cfg.Leverage = 10
	}
	if cfg.Amount <= 0 {
		cfg.Amount = 100
	}
	if cfg.EMAFast <= 0 {
		cfg.EMAFast = 7
	}
	if cfg.EMASlow <= 0 {
		cfg.EMASlow = 21
	}
	if cfg.EMATrend <= 0 {
		cfg.EMATrend = 50
	}
	if cfg.RSIPeriod <= 0 {
		cfg.RSIPeriod = 6
	}
	if cfg.RSIOverbought <= 0 {
		cfg.RSIOverbought = 75
	}
	if cfg.RSIOversold <= 0 {
		cfg.RSIOversold = 25
	}
	if cfg.VolumePeriod <= 0 {
		cfg.VolumePeriod = 10
	}
	if cfg.VolumeMulti <= 0 {
		cfg.VolumeMulti = 1.2
	}
	if cfg.ATRPeriod <= 0 {
		cfg.ATRPeriod = 14
	}
	if cfg.ATRMultiplier <= 0 {
		cfg.ATRMultiplier = 1.5
	}

	ctx := context.Background()

	// 拉取历史K线
	klines, err := fetchHistoricalKlines(ctx, cfg.Symbol, cfg.Days)
	if err != nil {
		return nil, err
	}

	if len(klines) < cfg.EMATrend+50 {
		return nil, fmt.Errorf("K线数量不足 %d，无法回测（需至少 %d 根）", len(klines), cfg.EMATrend+50)
	}

	// 预处理：提取 close/high/low/volume/openTime 数组
	n := len(klines)
	closes := make([]float64, n)
	highs := make([]float64, n)
	lows := make([]float64, n)
	volumes := make([]float64, n)
	openTimes := make([]int64, n)

	for i, k := range klines {
		closes[i], _ = strconv.ParseFloat(k.Close, 64)
		highs[i], _ = strconv.ParseFloat(k.High, 64)
		lows[i], _ = strconv.ParseFloat(k.Low, 64)
		volumes[i], _ = strconv.ParseFloat(k.Volume, 64)
		openTimes[i] = k.OpenTime
	}

	// 构造内联 ScalpConfig 参数（供 scalpDecide 调用）
	scalpCfg := ScalpConfig{
		EMAFast:       cfg.EMAFast,
		EMASlow:       cfg.EMASlow,
		EMATrend:      cfg.EMATrend,
		RSIPeriod:     cfg.RSIPeriod,
		RSIOverbought: cfg.RSIOverbought,
		RSIOversold:   cfg.RSIOversold,
		VolumePeriod:  cfg.VolumePeriod,
		VolumeMulti:   cfg.VolumeMulti,
		ATRPeriod:     cfg.ATRPeriod,
		ATRMultiplier: cfg.ATRMultiplier,
	}

	// 滑动窗口回放
	// 从第 warmup 根开始，保证各指标计算有足够数据
	warmup := cfg.EMATrend + 30 // 至少给趋势线留足数据
	if warmup < 60 {
		warmup = 60
	}

	// 虚拟持仓状态
	type virtualPos struct {
		open      bool
		side      string  // LONG / SHORT
		entry     float64 // 开仓价
		qty       float64 // 合约数量
		openTime  int64
		openReason string
		stopLoss  float64 // 止损价
		takeProfit float64 // 止盈价
	}

	var pos virtualPos
	var trades []BacktestTrade
	var equityCurve []float64 // 权益曲线（用于回撤计算）
	cumulativePnL := 0.0

	// 4H 趋势：简化处理，用当前窗口末尾的4H走势近似（每 240 根1m更新一次）
	trend4hCache := "NEUTRAL"
	trend4hUpdateAt := 0

	log.Printf("[Backtest] 开始回测 %s: %d 根K线，warmup=%d，预计 %d 次迭代",
		cfg.Symbol, n, warmup, n-warmup)

	for i := warmup; i < n; i++ {
		// 每 240 根（约4小时）更新一次4H趋势
		if i-trend4hUpdateAt >= 240 || i == warmup {
			trend4hUpdateAt = i
			// 使用当前窗口末尾 closes 计算1m近似4H趋势（取最近 240 根的 EMA）
			end := i + 1
			start := end - 240
			if start < 0 {
				start = 0
			}
			slice4h := closes[start:end]
			if len(slice4h) >= 22 {
				ema10_4h := calcEMA(slice4h, 10)
				ema20_4h := calcEMA(slice4h, 20)
				if len(ema10_4h) > 0 && len(ema20_4h) > 0 {
					e10 := ema10_4h[len(ema10_4h)-1]
					e20 := ema20_4h[len(ema20_4h)-1]
					price4h := slice4h[len(slice4h)-1]
					if price4h > e10 && e10 > e20 {
						trend4hCache = "BULL"
					} else if price4h < e10 && e10 < e20 {
						trend4hCache = "BEAR"
					} else {
						trend4hCache = "NEUTRAL"
					}
				}
			}
		}

		// 当前窗口数据（0..i 包含 i）
		end := i + 1
		closeSlice := closes[:end]
		volSlice := volumes[:end]

		// 计算指标
		emaFastArr := calcEMA(closeSlice, cfg.EMAFast)
		emaSlowArr := calcEMA(closeSlice, cfg.EMASlow)
		emaTrendArr := calcEMA(closeSlice, cfg.EMATrend)
		rsiArr := calcRSI(closeSlice, cfg.RSIPeriod)

		if len(emaFastArr) < 2 || len(emaSlowArr) < 2 || len(emaTrendArr) < 1 || len(rsiArr) < 1 {
			continue
		}

		emaFast := emaFastArr[len(emaFastArr)-1]
		emaSlow := emaSlowArr[len(emaSlowArr)-1]
		emaTrend := emaTrendArr[len(emaTrendArr)-1]
		prevEmaFast := emaFastArr[len(emaFastArr)-2]
		prevEmaSlow := emaSlowArr[len(emaSlowArr)-2]

		rsi := 0.0
		for ri := len(rsiArr) - 1; ri >= 0; ri-- {
			if rsiArr[ri] > 0 {
				rsi = rsiArr[ri]
				break
			}
		}

		// 成交量
		currentVol := volSlice[len(volSlice)-1]
		avgVol := calcAvgVolumeSlice(volSlice, cfg.VolumePeriod)
		volRatio := 0.0
		if avgVol > 0 {
			volRatio = currentVol / avgVol
		}

		// MACD
		_, _, macdHist := calcMACD(closeSlice, 12, 26, 9)

		// 布林带
		bbUpper, _, bbLower := calcBollingerBands(closeSlice, 20, 2.0)

		// ATR
		atr := calcATRSlice(highs[:end], lows[:end], closeSlice, cfg.ATRPeriod)

		currentPrice := closeSlice[len(closeSlice)-1]
		currentTs := openTimes[i]

		// ========== 检查止损止盈（优先于开新仓） ==========
		if pos.open {
			hit := false
			exitPrice := currentPrice
			closeReason := ""

			if pos.side == "LONG" {
				if currentPrice <= pos.stopLoss {
					hit = true
					exitPrice = pos.stopLoss // 假设以止损价成交
					closeReason = fmt.Sprintf("触发止损 SL=%.4f", pos.stopLoss)
				} else if currentPrice >= pos.takeProfit {
					hit = true
					exitPrice = pos.takeProfit
					closeReason = fmt.Sprintf("触发止盈 TP=%.4f", pos.takeProfit)
				}
			} else { // SHORT
				if currentPrice >= pos.stopLoss {
					hit = true
					exitPrice = pos.stopLoss
					closeReason = fmt.Sprintf("触发止损 SL=%.4f", pos.stopLoss)
				} else if currentPrice <= pos.takeProfit {
					hit = true
					exitPrice = pos.takeProfit
					closeReason = fmt.Sprintf("触发止盈 TP=%.4f", pos.takeProfit)
				}
			}

			if hit {
				pnl := 0.0
				if pos.side == "LONG" {
					pnl = (exitPrice - pos.entry) * pos.qty
				} else {
					pnl = (pos.entry - exitPrice) * pos.qty
				}
				cumulativePnL += pnl
				equityCurve = append(equityCurve, cumulativePnL)

				trades = append(trades, BacktestTrade{
					OpenTime:    time.UnixMilli(pos.openTime).Format("2006-01-02 15:04"),
					CloseTime:   time.UnixMilli(currentTs).Format("2006-01-02 15:04"),
					Side:        pos.side,
					EntryPrice:  pos.entry,
					ExitPrice:   exitPrice,
					Quantity:    pos.qty,
					PnL:         math.Round(pnl*10000) / 10000,
					OpenReason:  pos.openReason,
					CloseReason: closeReason,
				})

				pos = virtualPos{}
			}
		}

		// ========== 信号判断 ==========
		signal, reason := scalpDecide(
			scalpCfg, currentPrice,
			emaFast, emaSlow, emaTrend,
			prevEmaFast, prevEmaSlow,
			rsi, volRatio, macdHist,
			bbUpper, bbLower,
			trend4hCache,
			0, // 回测不做资金费率过滤
		)

		// ========== 处理信号 ==========
		switch signal {
		case "BUY":
			if pos.open && pos.side == "LONG" {
				// 已持多，不加仓
				continue
			}
			if pos.open && pos.side == "SHORT" {
				// 平空反手：先平当前空
				exitPrice := currentPrice
				pnl := (pos.entry - exitPrice) * pos.qty
				cumulativePnL += pnl
				equityCurve = append(equityCurve, cumulativePnL)
				trades = append(trades, BacktestTrade{
					OpenTime:    time.UnixMilli(pos.openTime).Format("2006-01-02 15:04"),
					CloseTime:   time.UnixMilli(currentTs).Format("2006-01-02 15:04"),
					Side:        pos.side,
					EntryPrice:  pos.entry,
					ExitPrice:   exitPrice,
					Quantity:    pos.qty,
					PnL:         math.Round(pnl*10000) / 10000,
					OpenReason:  pos.openReason,
					CloseReason: "反手平空: " + reason,
				})
				pos = virtualPos{}
			}
			// 开多
			if atr > 0 && currentPrice > 0 {
				qty := cfg.Amount * float64(cfg.Leverage) / currentPrice
				slDist := atr * cfg.ATRMultiplier
				slPrice := currentPrice - slDist
				tpPrice := currentPrice + slDist*2 // 1:2 盈亏比
				pos = virtualPos{
					open:        true,
					side:        "LONG",
					entry:       currentPrice,
					qty:         qty,
					openTime:    currentTs,
					openReason:  reason,
					stopLoss:    slPrice,
					takeProfit:  tpPrice,
				}
			}

		case "SELL":
			if pos.open && pos.side == "SHORT" {
				// 已持空，不加仓
				continue
			}
			if pos.open && pos.side == "LONG" {
				// 平多反手
				exitPrice := currentPrice
				pnl := (exitPrice - pos.entry) * pos.qty
				cumulativePnL += pnl
				equityCurve = append(equityCurve, cumulativePnL)
				trades = append(trades, BacktestTrade{
					OpenTime:    time.UnixMilli(pos.openTime).Format("2006-01-02 15:04"),
					CloseTime:   time.UnixMilli(currentTs).Format("2006-01-02 15:04"),
					Side:        pos.side,
					EntryPrice:  pos.entry,
					ExitPrice:   exitPrice,
					Quantity:    pos.qty,
					PnL:         math.Round(pnl*10000) / 10000,
					OpenReason:  pos.openReason,
					CloseReason: "反手平多: " + reason,
				})
				pos = virtualPos{}
			}
			// 开空
			if atr > 0 && currentPrice > 0 {
				qty := cfg.Amount * float64(cfg.Leverage) / currentPrice
				slDist := atr * cfg.ATRMultiplier
				slPrice := currentPrice + slDist
				tpPrice := currentPrice - slDist*2
				pos = virtualPos{
					open:        true,
					side:        "SHORT",
					entry:       currentPrice,
					qty:         qty,
					openTime:    currentTs,
					openReason:  reason,
					stopLoss:    slPrice,
					takeProfit:  tpPrice,
				}
			}

		case "CLOSE":
			if pos.open {
				exitPrice := currentPrice
				pnl := 0.0
				if pos.side == "LONG" {
					pnl = (exitPrice - pos.entry) * pos.qty
				} else {
					pnl = (pos.entry - exitPrice) * pos.qty
				}
				cumulativePnL += pnl
				equityCurve = append(equityCurve, cumulativePnL)
				trades = append(trades, BacktestTrade{
					OpenTime:    time.UnixMilli(pos.openTime).Format("2006-01-02 15:04"),
					CloseTime:   time.UnixMilli(currentTs).Format("2006-01-02 15:04"),
					Side:        pos.side,
					EntryPrice:  pos.entry,
					ExitPrice:   exitPrice,
					Quantity:    pos.qty,
					PnL:         math.Round(pnl*10000) / 10000,
					OpenReason:  pos.openReason,
					CloseReason: reason,
				})
				pos = virtualPos{}
			}
		}

		// 每 1000 根打印一次进度
		if (i-warmup)%1000 == 0 && i > warmup {
			log.Printf("[Backtest] 进度 %d/%d (%.1f%%)，已完成 %d 笔交易，累计 PnL=%.4f USDT",
				i-warmup, n-warmup,
				float64(i-warmup)/float64(n-warmup)*100,
				len(trades), cumulativePnL,
			)
		}
	}

	// 强制平掉未结仓位（用最后一根收盘价）
	if pos.open && n > 0 {
		exitPrice := closes[n-1]
		pnl := 0.0
		if pos.side == "LONG" {
			pnl = (exitPrice - pos.entry) * pos.qty
		} else {
			pnl = (pos.entry - exitPrice) * pos.qty
		}
		cumulativePnL += pnl
		equityCurve = append(equityCurve, cumulativePnL)
		trades = append(trades, BacktestTrade{
			OpenTime:    time.UnixMilli(pos.openTime).Format("2006-01-02 15:04"),
			CloseTime:   time.UnixMilli(openTimes[n-1]).Format("2006-01-02 15:04"),
			Side:        pos.side,
			EntryPrice:  pos.entry,
			ExitPrice:   exitPrice,
			Quantity:    pos.qty,
			PnL:         math.Round(pnl*10000) / 10000,
			OpenReason:  pos.openReason,
			CloseReason: "回测结束强制平仓",
		})
	}

	// ========== 统计 ==========
	totalTrades := len(trades)
	winCount := 0
	lossCount := 0
	totalWin := 0.0
	totalLoss := 0.0

	for _, t := range trades {
		if t.PnL >= 0 {
			winCount++
			totalWin += t.PnL
		} else {
			lossCount++
			totalLoss += -t.PnL // 转为正数
		}
	}

	winRate := 0.0
	if totalTrades > 0 {
		winRate = float64(winCount) / float64(totalTrades)
	}

	profitFactor := 0.0
	if totalLoss > 0 {
		profitFactor = totalWin / totalLoss
	} else if totalWin > 0 {
		profitFactor = 999 // 无亏损
	}

	avgWin := 0.0
	if winCount > 0 {
		avgWin = totalWin / float64(winCount)
	}
	avgLoss := 0.0
	if lossCount > 0 {
		avgLoss = totalLoss / float64(lossCount)
	}

	riskReward := 0.0
	if avgLoss > 0 {
		riskReward = avgWin / avgLoss
	}

	// 最大回撤：权益曲线最高点到最低点
	maxDrawdown := 0.0
	peak := 0.0
	for _, eq := range equityCurve {
		if eq > peak {
			peak = eq
		}
		dd := peak - eq
		if dd > maxDrawdown {
			maxDrawdown = dd
		}
	}

	startDate := time.UnixMilli(klines[0].OpenTime).Format("2006-01-02")
	endDate := time.UnixMilli(klines[len(klines)-1].CloseTime).Format("2006-01-02")

	log.Printf("[Backtest] 完成 %s: 共 %d 笔，胜率=%.1f%%，盈利因子=%.2f，总PnL=%.4f，最大回撤=%.4f",
		cfg.Symbol, totalTrades, winRate*100, profitFactor, cumulativePnL, maxDrawdown)

	return &BacktestResult{
		Symbol:       cfg.Symbol,
		Period:       fmt.Sprintf("%s ~ %s (%d 天)", startDate, endDate, cfg.Days),
		TotalKlines:  n,
		TotalTrades:  totalTrades,
		WinCount:     winCount,
		LossCount:    lossCount,
		WinRate:      math.Round(winRate*10000) / 10000,
		TotalPnL:     math.Round(cumulativePnL*10000) / 10000,
		MaxDrawdown:  math.Round(maxDrawdown*10000) / 10000,
		ProfitFactor: math.Round(profitFactor*100) / 100,
		AvgWin:       math.Round(avgWin*10000) / 10000,
		AvgLoss:      math.Round(avgLoss*10000) / 10000,
		RiskReward:   math.Round(riskReward*100) / 100,
		Trades:       trades,
	}, nil
}

// HandleRunBacktest POST /tool/backtest/run
func HandleRunBacktest(c context.Context, ctx *app.RequestContext) {
	var cfg BacktestConfig
	if err := ctx.BindJSON(&cfg); err != nil {
		ctx.JSON(http.StatusBadRequest, utils.H{"error": err.Error()})
		return
	}

	if cfg.Symbol == "" {
		ctx.JSON(http.StatusBadRequest, utils.H{"error": "symbol is required"})
		return
	}

	log.Printf("[Backtest] 收到回测请求: symbol=%s, days=%d, leverage=%d, amount=%.2f",
		cfg.Symbol, cfg.Days, cfg.Leverage, cfg.Amount)

	result, err := RunBacktest(cfg)
	if err != nil {
		log.Printf("[Backtest] 回测失败: %v", err)
		ctx.JSON(http.StatusInternalServerError, utils.H{"error": err.Error()})
		return
	}

	ctx.JSON(http.StatusOK, utils.H{"data": result})
}
