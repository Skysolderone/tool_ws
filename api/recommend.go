package api

import (
	"context"
	"fmt"
	"log"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/utils"
)

// ========== 推荐交易扫描（多时间框架 + 后台预计算） ==========

// 默认扫描币种
var defaultScanSymbols = []string{
	"BTCUSDT", "ETHUSDT", "BNBUSDT", "SOLUSDT",
	"XRPUSDT", "DOGEUSDT", "ADAUSDT", "AVAXUSDT",
	"DOTUSDT", "LINKUSDT", "MATICUSDT", "LTCUSDT",
	"ATOMUSDT", "NEARUSDT", "APTUSDT", "ARBUSDT",
	"OPUSDT", "SUIUSDT", "FILUSDT", "INJUSDT",
	"WIFUSDT", "PEPEUSDT", "SEIUSDT", "TIAUSDT",
}

// 多时间框架配置：周期 → 权重 + 刷新间隔 + K线拉取数
type recTimeframe struct {
	interval     string        // "1d", "4h", "1h"
	label        string        // 中文标签
	weight       float64       // 汇总权重（大周期更大）
	refreshEvery time.Duration // 后台刷新间隔
	klineLimit   int           // 拉取根数
}

var recTimeframes = []recTimeframe{
	{interval: "1d", label: "日线", weight: 3.0, refreshEvery: 4 * time.Hour, klineLimit: 60},
	{interval: "4h", label: "4H", weight: 2.0, refreshEvery: 1 * time.Hour, klineLimit: 60},
	{interval: "1h", label: "1H", weight: 1.5, refreshEvery: 15 * time.Minute, klineLimit: 80},
}

const (
	recMinTakeProfitDistancePct = 1.2
	recFallbackTakeProfitPct    = 3.0
	recMinRiskReward            = 1.6
	recMinConfidence            = 24
	recStrongConfidence         = 65
	recMediumConfidence         = 38
	recConfirmRounds            = 1
	recVolStrongRatio           = 2.0
	recVolMediumRatio           = 1.5
	recTrendStrength            = 1.0
	recShortTrendStrength       = 0.6
	recNearSRDistance           = 0.8
	recStrongSRDistance         = 0.4
	recFundingStrongThreshold   = 0.0003
	recSignalTTL                = 3 * time.Minute
	recSignalDriftPct           = 0.35
	recLiveMinRiskReward        = 1.3
	recPriceFreshTTL            = 10 * time.Second
	recPriceFetchTimeout        = 2 * time.Second
	recPriceRuleFetchTimeout    = 2 * time.Second
)

const recSignalCooldown = 30 * time.Minute

// ========== 数据结构 ==========

// tfSignal 单个时间框架对单个币种的分析结果
type tfSignal struct {
	Timeframe string  `json:"timeframe"`
	Direction string  `json:"direction"` // LONG / SHORT / ""
	Score     float64 `json:"score"`     // 0-10
	RSI       float64 `json:"rsi"`
	Trend     string  `json:"trend"`    // UP / DOWN / FLAT
	Pattern   string  `json:"pattern"`  // 形态名称
	VolRatio  float64 `json:"volRatio"` // 量比
	Reason    string  `json:"reason"`   // 该周期信号摘要
}

// RecommendItem 单条推荐
type RecommendItem struct {
	Symbol     string      `json:"symbol"`
	Direction  string      `json:"direction"`  // LONG / SHORT
	Confidence int         `json:"confidence"` // 0-100
	Entry      float64     `json:"entry"`
	StopLoss   float64     `json:"stopLoss"`
	TakeProfit float64     `json:"takeProfit"`
	Reasons    []string    `json:"reasons"`
	Signals    []tfSignal  `json:"signals"` // 各时间框架信号
	Scores     ScoreDetail `json:"scores"`
}

// ScoreDetail 各维度评分明细
type ScoreDetail struct {
	RSI     float64 `json:"rsi"`
	Volume  float64 `json:"volume"`
	Pattern float64 `json:"pattern"`
	Trend   float64 `json:"trend"`
	SR      float64 `json:"sr"`
	Funding float64 `json:"funding"`
	Total   float64 `json:"total"`
}

// MarketSentiment 全局市场情绪
type MarketSentiment struct {
	Bias        string  `json:"bias"`        // bullish / neutral / bearish
	Score       float64 `json:"score"`       // -100 ~ +100
	FundingRate float64 `json:"fundingRate"` // BTC 资金费率
	LongShort   float64 `json:"longShort"`   // 多空比
	LiqTotal    float64 `json:"liqTotal"`    // 1h 爆仓总额
}

// RecommendResponse 扫描返回
type RecommendResponse struct {
	Items     []RecommendItem `json:"items"`
	Sentiment MarketSentiment `json:"sentiment"`
	ScannedAt string          `json:"scannedAt"`
	Count     int             `json:"count"`
}

// ========== 后台预计算缓存 ==========

// 缓存：每个时间框架独立缓存
type recCache struct {
	mu        sync.RWMutex
	items     map[string]*tfSignal // symbol → signal（每个时间框架）
	updatedAt time.Time
}

type recSignalState struct {
	candidateDirection string
	candidateCount     int
	publishedDirection string
	publishedAt        time.Time
}

var (
	// recCaches[interval] → cache
	recCaches      = map[string]*recCache{}
	recCachesMu    sync.RWMutex
	sentimentCache struct {
		sync.RWMutex
		data      MarketSentiment
		updatedAt time.Time
	}
	// 综合结果缓存
	finalCache struct {
		sync.RWMutex
		resp      *RecommendResponse
		updatedAt time.Time
	}
	recSignalStates   = map[string]*recSignalState{}
	recSignalStatesMu sync.Mutex
)

func init() {
	for _, tf := range recTimeframes {
		recCaches[tf.interval] = &recCache{
			items: make(map[string]*tfSignal),
		}
	}
}

// StartRecommendEngine 启动后台推荐预计算引擎（在 main.go 中调用）
func StartRecommendEngine() {
	log.Printf("[Recommend] Starting background engine for %d symbols × %d timeframes",
		len(defaultScanSymbols), len(recTimeframes))

	// 启动后立即全量计算一轮
	go func() {
		time.Sleep(5 * time.Second) // 等 Client 初始化完成
		refreshAllTimeframes()
		rebuildFinalCache()
	}()

	// 每个时间框架独立定时刷新
	for _, tf := range recTimeframes {
		go recRefreshLoop(tf)
	}

	// 情绪定时刷新（5分钟）
	go func() {
		for {
			refreshSentiment()
			time.Sleep(5 * time.Minute)
		}
	}()
}

// recRefreshLoop 单个时间框架的刷新循环
func recRefreshLoop(tf recTimeframe) {
	ticker := time.NewTicker(tf.refreshEvery)
	defer ticker.Stop()
	for range ticker.C {
		refreshTimeframe(tf)
		rebuildFinalCache()
	}
}

// refreshAllTimeframes 全量刷新所有时间框架
func refreshAllTimeframes() {
	refreshSentiment()
	for _, tf := range recTimeframes {
		refreshTimeframe(tf)
	}
}

// refreshSentiment 刷新全局情绪
func refreshSentiment() {
	s := calcMarketSentiment()
	sentimentCache.Lock()
	sentimentCache.data = s
	sentimentCache.updatedAt = time.Now()
	sentimentCache.Unlock()
	log.Printf("[Recommend] Sentiment refreshed: %s (score=%.0f)", s.Bias, s.Score)
}

// refreshTimeframe 刷新单个时间框架的所有币种
func refreshTimeframe(tf recTimeframe) {
	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	cache := recCaches[tf.interval]
	sem := make(chan struct{}, 6) // 并发限 6
	var wg sync.WaitGroup

	for _, sym := range defaultScanSymbols {
		wg.Add(1)
		go func(symbol string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			symCtx, symCancel := context.WithTimeout(ctx, 10*time.Second)
			defer symCancel()

			signal := analyzeTF(symCtx, symbol, tf)
			cache.mu.Lock()
			cache.items[symbol] = signal
			cache.mu.Unlock()
		}(sym)
	}
	wg.Wait()

	cache.mu.Lock()
	cache.updatedAt = time.Now()
	cache.mu.Unlock()

	log.Printf("[Recommend] %s refreshed %d symbols in %v", tf.interval, len(defaultScanSymbols), time.Since(start).Round(time.Millisecond))
}

func updateSignalState(symbol string, direction string, now time.Time) bool {
	recSignalStatesMu.Lock()
	defer recSignalStatesMu.Unlock()

	state, ok := recSignalStates[symbol]
	if !ok {
		state = &recSignalState{}
		recSignalStates[symbol] = state
	}

	if direction == "" {
		state.candidateDirection = ""
		state.candidateCount = 0
		return false
	}

	if state.candidateDirection == direction {
		state.candidateCount++
	} else {
		state.candidateDirection = direction
		state.candidateCount = 1
	}

	if state.candidateCount < recConfirmRounds {
		return false
	}

	if state.publishedDirection == direction && !state.publishedAt.IsZero() &&
		now.Sub(state.publishedAt) < recSignalCooldown {
		return false
	}

	state.publishedDirection = direction
	state.publishedAt = now
	return true
}

// rebuildFinalCache 汇总所有时间框架，生成最终推荐列表
func rebuildFinalCache() {
	sentimentCache.RLock()
	sentiment := sentimentCache.data
	sentimentCache.RUnlock()

	now := time.Now()
	var items []RecommendItem
	for _, symbol := range defaultScanSymbols {
		item := mergeTimeframes(symbol)
		if item == nil {
			updateSignalState(symbol, "", now)
			continue
		}
		if updateSignalState(symbol, item.Direction, now) {
			items = append(items, *item)
		}
	}
	items = filterValidRecommendItems(context.Background(), items, now, false)

	sort.Slice(items, func(i, j int) bool {
		return items[i].Confidence > items[j].Confidence
	})
	if len(items) > 15 {
		items = items[:15]
	}
	persistRecommendSignalHistoryBatch(items, now, "engine")

	resp := &RecommendResponse{
		Items:     items,
		Sentiment: sentiment,
		ScannedAt: now.Format(time.RFC3339),
		Count:     len(items),
	}

	finalCache.Lock()
	finalCache.resp = resp
	finalCache.updatedAt = now
	finalCache.Unlock()

	log.Printf("[Recommend] Final cache rebuilt: %d recommendations", len(items))
}

// ========== API Handler ==========

// HandleRecommendScan GET /tool/recommend/scan?symbols=...&force=1
func HandleRecommendScan(c context.Context, ctx *app.RequestContext) {
	// force=1 时实时刷新
	if string(ctx.Query("force")) == "1" {
		go func() {
			refreshAllTimeframes()
			rebuildFinalCache()
		}()
	}

	// 如果指定了 symbols，做实时计算（不走缓存）
	symbolsParam := string(ctx.Query("symbols"))
	if symbolsParam != "" {
		var symbols []string
		for _, s := range strings.Split(symbolsParam, ",") {
			s = strings.TrimSpace(strings.ToUpper(s))
			if s != "" {
				symbols = append(symbols, s)
			}
		}
		if len(symbols) > 0 {
			handleRealtimeScan(c, ctx, symbols)
			return
		}
	}

	// 从缓存返回
	finalCache.RLock()
	resp := finalCache.resp
	cachedAt := finalCache.updatedAt
	finalCache.RUnlock()

	if resp == nil {
		// 缓存还没有准备好，触发实时计算
		handleRealtimeScan(c, ctx, defaultScanSymbols)
		return
	}

	scannedAt := parseRecommendScannedAt(resp.ScannedAt, cachedAt)
	filteredItems := filterValidRecommendItems(c, resp.Items, scannedAt, true)
	if len(filteredItems) == 0 && time.Since(scannedAt) > recSignalTTL {
		log.Printf("[Recommend] Cache stale at %s, fallback to realtime scan", scannedAt.Format(time.RFC3339))
		handleRealtimeScan(c, ctx, defaultScanSymbols)
		return
	}

	filteredResp := *resp
	filteredResp.Items = filteredItems
	filteredResp.Count = len(filteredItems)
	ctx.JSON(http.StatusOK, utils.H{"data": filteredResp})
}

// handleRealtimeScan 实时扫描（指定 symbols 时或缓存未就绪时使用）
func handleRealtimeScan(c context.Context, ctx *app.RequestContext, symbols []string) {
	scanCtx, cancel := context.WithTimeout(c, 30*time.Second)
	defer cancel()

	sentiment := calcMarketSentiment()

	type scanResult struct {
		symbol string
		item   *RecommendItem
	}
	resultCh := make(chan scanResult, len(symbols))
	sem := make(chan struct{}, 6)
	var wg sync.WaitGroup

	for _, sym := range symbols {
		wg.Add(1)
		go func(symbol string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			symCtx, symCancel := context.WithTimeout(scanCtx, 12*time.Second)
			defer symCancel()

			item := realtimeScanSymbol(symCtx, symbol)
			resultCh <- scanResult{symbol: symbol, item: item}
		}(sym)
	}
	go func() { wg.Wait(); close(resultCh) }()

	now := time.Now()
	var items []RecommendItem
	for res := range resultCh {
		if res.item == nil {
			updateSignalState(res.symbol, "", now)
			continue
		}
		if updateSignalState(res.symbol, res.item.Direction, now) {
			items = append(items, *res.item)
		}
	}
	items = filterValidRecommendItems(scanCtx, items, now, false)

	sort.Slice(items, func(i, j int) bool {
		return items[i].Confidence > items[j].Confidence
	})
	if len(items) > 15 {
		items = items[:15]
	}
	persistRecommendSignalHistoryBatch(items, now, "realtime")

	ctx.JSON(http.StatusOK, utils.H{
		"data": RecommendResponse{
			Items:     items,
			Sentiment: sentiment,
			ScannedAt: now.Format(time.RFC3339),
			Count:     len(items),
		},
	})
}

// realtimeScanSymbol 实时扫描一个币种（拉取所有时间框架）
func realtimeScanSymbol(ctx context.Context, symbol string) *RecommendItem {
	for _, tf := range recTimeframes {
		signal := analyzeTF(ctx, symbol, tf)
		cache := recCaches[tf.interval]
		cache.mu.Lock()
		cache.items[symbol] = signal
		cache.mu.Unlock()
	}
	return mergeTimeframes(symbol)
}

// ========== 单时间框架分析 ==========

// analyzeTF 分析单个币种在单个时间框架下的信号
func analyzeTF(ctx context.Context, symbol string, tf recTimeframe) *tfSignal {
	sig := &tfSignal{Timeframe: tf.interval}

	klines, err := Client.NewKlinesService().Symbol(symbol).
		Interval(tf.interval).Limit(tf.klineLimit).Do(ctx)
	if err != nil || len(klines) < 20 {
		return sig
	}

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
	lastIdx := n - 1

	// --- RSI ---
	rsiValues := calcRSI(closes, 14)
	rsiCurrent := 50.0
	rsiDir := ""
	rsiScore := 0.0
	reasonParts := []string{}

	if len(rsiValues) >= 2 {
		rsiCurrent = rsiValues[len(rsiValues)-1]
		rsiPrev := rsiValues[len(rsiValues)-2]

		if rsiPrev < 30 && rsiCurrent >= 30 {
			rsiDir = "LONG"
			rsiScore = 2.5
			reasonParts = append(reasonParts, fmt.Sprintf("RSI超卖回升%.0f→%.0f", rsiPrev, rsiCurrent))
		} else if rsiPrev > 70 && rsiCurrent <= 70 {
			rsiDir = "SHORT"
			rsiScore = 2.5
			reasonParts = append(reasonParts, fmt.Sprintf("RSI超买回落%.0f→%.0f", rsiPrev, rsiCurrent))
		} else if rsiCurrent < 30 {
			rsiDir = "LONG"
			rsiScore = 2.0
			reasonParts = append(reasonParts, fmt.Sprintf("RSI超卖%.0f", rsiCurrent))
		} else if rsiCurrent > 70 {
			rsiDir = "SHORT"
			rsiScore = 2.0
			reasonParts = append(reasonParts, fmt.Sprintf("RSI超买%.0f", rsiCurrent))
		} else if rsiCurrent < 40 {
			rsiDir = "LONG"
			rsiScore = 1.2
			reasonParts = append(reasonParts, fmt.Sprintf("RSI偏低%.0f", rsiCurrent))
		} else if rsiCurrent > 60 {
			rsiDir = "SHORT"
			rsiScore = 1.2
			reasonParts = append(reasonParts, fmt.Sprintf("RSI偏高%.0f", rsiCurrent))
		} else if rsiCurrent < 45 {
			rsiDir = "LONG"
			rsiScore = 0.5
		} else if rsiCurrent > 55 {
			rsiDir = "SHORT"
			rsiScore = 0.5
		}
	}
	sig.RSI = rsiCurrent

	// --- 成交量 ---
	volScore := 0.0
	avgVol := calcAvgVolume(volumes, 20)
	volRatio := 0.0
	if avgVol > 0 {
		volRatio = volumes[lastIdx] / avgVol
	}
	sig.VolRatio = volRatio
	if volRatio >= recVolStrongRatio {
		volScore = 1.5
		reasonParts = append(reasonParts, fmt.Sprintf("放量%.1fx", volRatio))
	} else if volRatio >= recVolMediumRatio {
		volScore = 0.8
	}

	// --- K线形态 ---
	patternScore := 0.0
	dojiCfg := DojiConfig{
		BodyRatio: 0.1, ShadowRatio: 2.0,
		EnableDoji: true, EnableHammer: true, EnableEngulf: true,
	}
	pattern := detectPattern(dojiCfg, opens, highs, lows, closes, lastIdx)
	patDir := ""
	if pattern != PatternNone {
		sig.Pattern = string(pattern)
		patDir = patternDirection(pattern)
		if patDir != "" {
			patternScore = 2.0
			reasonParts = append(reasonParts, patternLabel(pattern))
		} else {
			patternScore = 0.8
		}
	}

	// --- 趋势 ---
	trendScore := 0.0
	trend := detectTrend(closes, lastIdx, 10, recTrendStrength)
	shortTrend := detectTrend(closes, lastIdx, 5, recShortTrendStrength)
	sig.Trend = trend

	trendDir := ""
	if shortTrend == "UP" {
		trendDir = "LONG"
		trendScore = 1.2
		reasonParts = append(reasonParts, "短期升势")
	} else if shortTrend == "DOWN" {
		trendDir = "SHORT"
		trendScore = 1.2
		reasonParts = append(reasonParts, "短期降势")
	} else if trend == "UP" {
		trendDir = "LONG"
		trendScore = 0.8
	} else if trend == "DOWN" {
		trendDir = "SHORT"
		trendScore = 0.8
	} else {
		trendScore = 0.3
	}

	// --- 投票汇总 ---
	longV := 0.0
	shortV := 0.0
	if rsiDir == "LONG" {
		longV += 2.5
	} else if rsiDir == "SHORT" {
		shortV += 2.5
	}
	if patDir == "LONG" {
		longV += 2.0
	} else if patDir == "SHORT" {
		shortV += 2.0
	}
	if trendDir == "LONG" {
		longV += 1.5
	} else if trendDir == "SHORT" {
		shortV += 1.5
	}

	if longV > shortV {
		sig.Direction = "LONG"
	} else if shortV > longV {
		sig.Direction = "SHORT"
	}

	// 反向惩罚
	if rsiDir != "" && rsiDir != sig.Direction {
		rsiScore *= 0.3
	}
	if patDir != "" && patDir != sig.Direction {
		patternScore *= 0.2
	}

	sig.Score = rsiScore + volScore + patternScore + trendScore
	sig.Reason = fmt.Sprintf("[%s] %s", tf.label, strings.Join(reasonParts, " | "))

	return sig
}

// ========== 多时间框架汇总 ==========

// mergeTimeframes 汇总 1d/4h/1h 三个时间框架的信号，生成最终推荐
func mergeTimeframes(symbol string) *RecommendItem {
	var signals []tfSignal
	longVotes := 0.0
	shortVotes := 0.0
	weightedScore := 0.0
	totalWeight := 0.0
	var reasons []string

	for _, tf := range recTimeframes {
		cache := recCaches[tf.interval]
		cache.mu.RLock()
		sig := cache.items[symbol]
		cache.mu.RUnlock()

		if sig == nil {
			continue
		}
		signals = append(signals, *sig)

		// 加权投票
		if sig.Direction == "LONG" {
			longVotes += tf.weight
		} else if sig.Direction == "SHORT" {
			shortVotes += tf.weight
		}

		// 加权评分
		weightedScore += sig.Score * tf.weight
		totalWeight += tf.weight

		if sig.Reason != "" && sig.Direction != "" {
			reasons = append(reasons, sig.Reason)
		}
	}

	if totalWeight == 0 {
		return nil
	}

	// 方向由加权投票决定
	direction := ""
	if longVotes > shortVotes {
		direction = "LONG"
	} else if shortVotes > longVotes {
		direction = "SHORT"
	}
	if direction == "" {
		return nil
	}

	// 多周期一致性加成
	alignCount := 0
	for _, sig := range signals {
		if sig.Direction == direction {
			alignCount++
		}
	}
	alignBonus := 0.0
	if alignCount == len(recTimeframes) {
		alignBonus = 1.5 // 三周期完全一致
		reasons = append(reasons, fmt.Sprintf("★ %d周期方向一致", alignCount))
	} else if alignCount >= 2 {
		alignBonus = 0.8
		reasons = append(reasons, fmt.Sprintf("%d/%d周期方向一致", alignCount, len(recTimeframes)))
	}

	// 归一化评分
	avgScore := weightedScore / totalWeight
	// 叠加一致性奖励和 SR + funding
	entry := 0.0
	var stopLoss, takeProfit float64

	// 获取当前价格
	cache := GetPriceCache()
	if p, err := cache.GetPrice(symbol); err == nil {
		entry = p
	} else {
		// 用 1h 最新收盘
		c1h := recCaches["1h"]
		c1h.mu.RLock()
		s1h := c1h.items[symbol]
		c1h.mu.RUnlock()
		if s1h != nil {
			// 从 K 线拿不到，用 REST
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			entry, _ = getCurrentPrice(ctx, symbol, "")
			cancel()
		}
	}

	// SR + funding（较轻量，可直接取）
	srScore := 0.0
	fundingScore := 0.0

	// 支撑阻力
	var srData *SRResponse
	srCtx, srCancel := context.WithTimeout(context.Background(), 5*time.Second)
	srData, srErr := GetSRLevels(srCtx, symbol)
	srCancel()
	if srErr == nil && srData != nil {
		supportDist := 999.0
		resistDist := 999.0
		if srData.ClosestSupport != nil {
			supportDist = math.Abs(srData.ClosestSupport.Distance)
		}
		if srData.ClosestResist != nil {
			resistDist = math.Abs(srData.ClosestResist.Distance)
		}

		if direction == "LONG" && supportDist < recNearSRDistance {
			if supportDist < recStrongSRDistance && srData.ClosestSupport.Strength >= 2 {
				srScore = 1.5
				reasons = append(reasons, fmt.Sprintf("靠近强支撑 $%.2f", srData.ClosestSupport.Price))
			} else {
				srScore = 0.7
			}
		} else if direction == "SHORT" && resistDist < recNearSRDistance {
			if resistDist < recStrongSRDistance && srData.ClosestResist.Strength >= 2 {
				srScore = 1.5
				reasons = append(reasons, fmt.Sprintf("靠近强阻力 $%.2f", srData.ClosestResist.Price))
			} else {
				srScore = 0.7
			}
		}

		// 止损止盈
		if direction == "LONG" {
			if srData.ClosestSupport != nil {
				stopLoss = srData.ClosestSupport.Price * 0.997
			}
			if srData.ClosestResist != nil {
				takeProfit = srData.ClosestResist.Price
			}
		} else {
			if srData.ClosestResist != nil {
				stopLoss = srData.ClosestResist.Price * 1.003
			}
			if srData.ClosestSupport != nil {
				takeProfit = srData.ClosestSupport.Price
			}
		}
	}

	// 资金费率
	if fr, err := fetchFundingRate(symbol); err == nil {
		if (direction == "LONG" && fr < -recFundingStrongThreshold) || (direction == "SHORT" && fr > recFundingStrongThreshold) {
			fundingScore = 1.0
			reasons = append(reasons, fmt.Sprintf("资金费率支持 (%.4f%%)", fr*100))
		} else if (direction == "LONG" && fr < 0) || (direction == "SHORT" && fr > 0) {
			fundingScore = 0.5
		}
	}

	if entry == 0 {
		return nil
	}

	// 退化止损止盈
	if stopLoss == 0 {
		if direction == "LONG" {
			stopLoss = entry * 0.98
		} else {
			stopLoss = entry * 1.02
		}
	}
	if takeProfit == 0 {
		if direction == "LONG" {
			takeProfit = entry * 1.04
		} else {
			takeProfit = entry * 0.96
		}
	}
	takeProfit, reasons = applyTakeProfitGuard(direction, entry, stopLoss, takeProfit, srData, reasons)
	now := time.Now()
	if ok, reason := validateRecommendSignal(direction, entry, stopLoss, takeProfit, entry, 0, now, now); !ok {
		log.Printf("[Recommend] Drop invalid signal %s %s: reason=%s entry=%.4f sl=%.4f tp=%.4f",
			symbol, direction, reason, entry, stopLoss, takeProfit)
		return nil
	}

	// 最终得分：多TF加权平均 + 一致性 + SR + funding，满分约 13
	total := avgScore + alignBonus + srScore + fundingScore
	confidence := int(total / 13.0 * 100)
	if confidence > 100 {
		confidence = 100
	}

	// 投票倾斜度加成
	dominant := math.Max(longVotes, shortVotes)
	minor := math.Min(longVotes, shortVotes)
	if dominant > 0 && minor == 0 {
		confidence = int(math.Min(100, float64(confidence)*1.10))
	} else if dominant >= minor*2 {
		confidence = int(math.Min(100, float64(confidence)*1.05))
	}

	if confidence < recMinConfidence || len(reasons) == 0 {
		return nil
	}

	// 汇总各维度分数（取最大 TF 的）
	var bestRSI, bestVol, bestPat, bestTrend float64
	for _, sig := range signals {
		// 简单取最好的
		rsiPart := sig.Score * 0.4 // 粗略拆
		if rsiPart > bestRSI {
			bestRSI = rsiPart
		}
	}
	_ = bestVol
	_ = bestPat
	_ = bestTrend

	return &RecommendItem{
		Symbol:     symbol,
		Direction:  direction,
		Confidence: confidence,
		Entry:      entry,
		StopLoss:   stopLoss,
		TakeProfit: takeProfit,
		Reasons:    reasons,
		Signals:    signals,
		Scores: ScoreDetail{
			RSI:     avgScore * 0.3,
			Volume:  avgScore * 0.15,
			Pattern: avgScore * 0.25,
			Trend:   avgScore * 0.15,
			SR:      srScore,
			Funding: fundingScore,
			Total:   total,
		},
	}
}

func selectFurtherTPFromSR(direction string, entry float64, srData *SRResponse, minDistPct float64) (float64, bool) {
	if srData == nil || entry <= 0 {
		return 0, false
	}

	bestPrice := 0.0
	bestDist := math.MaxFloat64

	switch direction {
	case "LONG":
		for _, level := range srData.Resistances {
			if level.Price <= entry {
				continue
			}
			distPct := (level.Price - entry) / entry * 100
			if distPct < minDistPct || distPct >= bestDist {
				continue
			}
			bestDist = distPct
			bestPrice = level.Price
		}
	case "SHORT":
		for _, level := range srData.Supports {
			if level.Price >= entry {
				continue
			}
			distPct := (entry - level.Price) / entry * 100
			if distPct < minDistPct || distPct >= bestDist {
				continue
			}
			bestDist = distPct
			bestPrice = level.Price
		}
	}

	if bestPrice <= 0 {
		return 0, false
	}
	return bestPrice, true
}

func applyTakeProfitGuard(direction string, entry, stopLoss, takeProfit float64, srData *SRResponse, reasons []string) (float64, []string) {
	if entry <= 0 || takeProfit <= 0 {
		return takeProfit, reasons
	}

	tpDistPct := math.Abs(takeProfit-entry) / entry * 100
	if tpDistPct < recMinTakeProfitDistancePct {
		oldDist := tpDistPct
		if srTP, ok := selectFurtherTPFromSR(direction, entry, srData, recMinTakeProfitDistancePct); ok {
			takeProfit = srTP
		} else if direction == "LONG" {
			takeProfit = entry * (1 + recFallbackTakeProfitPct/100)
		} else if direction == "SHORT" {
			takeProfit = entry * (1 - recFallbackTakeProfitPct/100)
		}

		if takeProfit > 0 {
			newDist := math.Abs(takeProfit-entry) / entry * 100
			reasons = append(reasons, fmt.Sprintf("防护策略：止盈目标过近(%.2f%%)，已调整为 %.2f%%", oldDist, newDist))
		}
	}

	if stopLoss <= 0 || takeProfit <= 0 {
		return takeProfit, reasons
	}

	slDist := math.Abs(entry - stopLoss)
	tpDist := math.Abs(takeProfit - entry)
	if slDist <= 0 {
		return takeProfit, reasons
	}

	rr := tpDist / slDist
	if rr >= recMinRiskReward {
		return takeProfit, reasons
	}

	targetTPDist := slDist * recMinRiskReward
	updated := takeProfit
	if direction == "LONG" {
		updated = entry + targetTPDist
		if updated > takeProfit {
			takeProfit = updated
		}
	} else if direction == "SHORT" {
		updated = entry - targetTPDist
		if updated < takeProfit {
			takeProfit = updated
		}
	}
	if takeProfit != updated {
		return takeProfit, reasons
	}
	reasons = append(reasons, fmt.Sprintf("防护策略：盈亏比 %.2f 偏低，已提升到至少 1:%.1f", rr, recMinRiskReward))
	return takeProfit, reasons
}

func parseRecommendScannedAt(scannedAt string, fallback time.Time) time.Time {
	if t, err := time.Parse(time.RFC3339, scannedAt); err == nil && !t.IsZero() {
		return t
	}
	if !fallback.IsZero() {
		return fallback
	}
	return time.Now()
}

func filterValidRecommendItems(ctx context.Context, items []RecommendItem, generatedAt time.Time, allowREST bool) []RecommendItem {
	if len(items) == 0 {
		return items
	}

	now := time.Now()
	if generatedAt.IsZero() {
		generatedAt = now
	}

	valid := make([]RecommendItem, 0, len(items))
	for _, item := range items {
		mark := resolveRecommendMarkPrice(ctx, item.Symbol, item.Entry, allowREST)
		tickSize := loadRecommendTickSize(ctx, item.Symbol, allowREST)
		ok, reason := validateRecommendSignal(
			item.Direction,
			item.Entry,
			item.StopLoss,
			item.TakeProfit,
			mark,
			tickSize,
			generatedAt,
			now,
		)
		if !ok {
			log.Printf("[Recommend] Filtered signal %s %s reason=%s age=%s entry=%.4f mark=%.4f sl=%.4f tp=%.4f",
				item.Symbol,
				item.Direction,
				reason,
				now.Sub(generatedAt).Round(time.Second),
				item.Entry,
				mark,
				item.StopLoss,
				item.TakeProfit,
			)
			continue
		}
		valid = append(valid, item)
	}
	return valid
}

func resolveRecommendMarkPrice(ctx context.Context, symbol string, fallback float64, allowREST bool) float64 {
	if price, ok := loadFreshCachedPrice(symbol); ok {
		return price
	}
	if !allowREST || Client == nil {
		return fallback
	}

	parent := ctx
	if parent == nil {
		parent = context.Background()
	}
	reqCtx, cancel := context.WithTimeout(parent, recPriceFetchTimeout)
	defer cancel()

	prices, err := Client.NewListPricesService().Symbol(symbol).Do(reqCtx)
	if err != nil || len(prices) == 0 {
		return fallback
	}
	price, err := strconv.ParseFloat(prices[0].Price, 64)
	if err != nil || price <= 0 {
		return fallback
	}
	GetPriceCache().UpdatePrice(symbol, price)
	return price
}

func loadFreshCachedPrice(symbol string) (float64, bool) {
	cache := GetPriceCache()
	cache.mu.RLock()
	data, ok := cache.prices[symbol]
	cache.mu.RUnlock()
	if !ok || data == nil || data.MarkPrice <= 0 {
		return 0, false
	}
	if time.Since(data.LastUpdate) > recPriceFreshTTL {
		return 0, false
	}
	return data.MarkPrice, true
}

func loadRecommendTickSize(ctx context.Context, symbol string, allowNetwork bool) float64 {
	if !allowNetwork || Client == nil {
		return 0
	}
	parent := ctx
	if parent == nil {
		parent = context.Background()
	}
	ruleCtx, cancel := context.WithTimeout(parent, recPriceRuleFetchTimeout)
	defer cancel()

	_, tickSize, err := getSymbolPriceRules(ruleCtx, symbol)
	if err != nil || tickSize <= 0 {
		return 0
	}
	return tickSize
}

func normalizeSignalPrice(price, tickSize float64) float64 {
	if price <= 0 {
		return price
	}
	if tickSize <= 0 {
		return price
	}
	return roundToStepSize(price, tickSize)
}

func validateRecommendSignal(direction string, entry, stopLoss, takeProfit, markPrice, tickSize float64, generatedAt, now time.Time) (bool, string) {
	if direction != "LONG" && direction != "SHORT" {
		return false, "invalid_direction"
	}
	if entry <= 0 || stopLoss <= 0 || takeProfit <= 0 {
		return false, "invalid_price_values"
	}

	if !generatedAt.IsZero() {
		if age := now.Sub(generatedAt); age > recSignalTTL {
			return false, fmt.Sprintf("signal_ttl_exceeded(%s)", age.Round(time.Second))
		}
	}

	entry = normalizeSignalPrice(entry, tickSize)
	stopLoss = normalizeSignalPrice(stopLoss, tickSize)
	takeProfit = normalizeSignalPrice(takeProfit, tickSize)
	if markPrice <= 0 {
		markPrice = entry
	}
	markPrice = normalizeSignalPrice(markPrice, tickSize)

	if entry <= 0 || stopLoss <= 0 || takeProfit <= 0 || markPrice <= 0 {
		return false, "invalid_normalized_prices"
	}

	driftRatio := recSignalDriftPct / 100

	switch direction {
	case "LONG":
		if !(stopLoss < entry && entry < takeProfit) {
			return false, "invalid_long_sl_entry_tp_relation"
		}
		if markPrice > entry*(1+driftRatio) {
			return false, "long_entry_drift_too_far"
		}
		risk := markPrice - stopLoss
		reward := takeProfit - markPrice
		if risk <= 0 {
			return false, "long_mark_at_or_below_stop_loss"
		}
		if reward <= 0 {
			return false, "long_take_profit_already_passed"
		}
		if reward/risk < recLiveMinRiskReward {
			return false, "long_live_rr_too_low"
		}
		stopBuffer := tickSize
		if markPrice <= stopLoss+stopBuffer {
			return false, "long_near_stop_loss"
		}
	case "SHORT":
		if !(takeProfit < entry && entry < stopLoss) {
			return false, "invalid_short_tp_entry_sl_relation"
		}
		if markPrice < entry*(1-driftRatio) {
			return false, "short_entry_drift_too_far"
		}
		risk := stopLoss - markPrice
		reward := markPrice - takeProfit
		if risk <= 0 {
			return false, "short_mark_at_or_above_stop_loss"
		}
		if reward <= 0 {
			return false, "short_take_profit_already_passed"
		}
		if reward/risk < recLiveMinRiskReward {
			return false, "short_live_rr_too_low"
		}
		stopBuffer := tickSize
		if markPrice >= stopLoss-stopBuffer {
			return false, "short_near_stop_loss"
		}
	}

	return true, ""
}

// ========== 市场情绪 ==========

func calcMarketSentiment() MarketSentiment {
	var (
		fundingRate float64
		longShort   float64
		liqTotal    float64
		score       float64
	)

	if fr, err := fetchFundingRate("BTCUSDT"); err == nil {
		fundingRate = fr
	}
	if ls, err := fetchGlobalLongShortAccount("BTCUSDT", "5m"); err == nil {
		longShort = ls.LongAccount / math.Max(ls.ShortAccount, 0.001)
	}
	liq := loadLatestLiquidationStat()
	liqTotal = liq.TotalNotional

	if fundingRate > 0 {
		score += 20
	} else if fundingRate < 0 {
		score -= 20
	}
	if longShort > 1.1 {
		score += 15
	} else if longShort < 0.9 {
		score -= 15
	}
	if liqTotal > 0 && liq.SellNotional > liq.BuyNotional*1.5 {
		score += 15
	} else if liqTotal > 0 && liq.BuyNotional > liq.SellNotional*1.5 {
		score -= 15
	}

	bias := "neutral"
	if score >= 20 {
		bias = "bullish"
	} else if score <= -20 {
		bias = "bearish"
	}

	return MarketSentiment{
		Bias: bias, Score: score,
		FundingRate: fundingRate, LongShort: longShort, LiqTotal: liqTotal,
	}
}

// ========== 持仓分析 ==========

// PositionAnalysis 单条持仓分析结果
type PositionAnalysis struct {
	Symbol        string     `json:"symbol"`
	Side          string     `json:"side"` // LONG / SHORT
	EntryPrice    float64    `json:"entryPrice"`
	MarkPrice     float64    `json:"markPrice"`
	Amount        float64    `json:"amount"`
	Leverage      int        `json:"leverage"`
	UnrealizedPnl float64    `json:"unrealizedPnl"`
	PnlPercent    float64    `json:"pnlPercent"` // 盈亏百分比
	Direction     string     `json:"direction"`  // AI 建议方向 LONG/SHORT
	Confidence    int        `json:"confidence"`
	Advice        string     `json:"advice"`      // hold / take_profit / stop_loss / add / reduce / close
	AdviceLabel   string     `json:"adviceLabel"` // 中文建议
	Reasons       []string   `json:"reasons"`
	Signals       []tfSignal `json:"signals"`
	StopLoss      float64    `json:"stopLoss"`
	TakeProfit    float64    `json:"takeProfit"`
}

// AnalyzeResponse 分析返回
type AnalyzeResponse struct {
	Items      []PositionAnalysis `json:"items"`
	Sentiment  MarketSentiment    `json:"sentiment"`
	AnalyzedAt string             `json:"analyzedAt"`
	Count      int                `json:"count"`
}

// HandleRecommendAnalyze GET /tool/recommend/analyze
func HandleRecommendAnalyze(c context.Context, ctx *app.RequestContext) {
	start := time.Now()
	reqID := strconv.FormatInt(time.Now().UnixNano(), 36)
	log.Printf("[RecommendAnalyze][%s] start", reqID)

	fetchStart := time.Now()
	positions, err := GetPositionsViaWs(c)
	if err != nil {
		log.Printf("[RecommendAnalyze][%s] get_positions failed after=%v err=%v", reqID, time.Since(fetchStart).Round(time.Millisecond), err)
		ctx.JSON(http.StatusInternalServerError, utils.H{"error": "获取持仓失败: " + err.Error()})
		return
	}
	log.Printf("[RecommendAnalyze][%s] get_positions done after=%v count=%d", reqID, time.Since(fetchStart).Round(time.Millisecond), len(positions))

	sentimentCache.RLock()
	sentiment := sentimentCache.data
	sentimentCache.RUnlock()

	sem := make(chan struct{}, 6)
	var wg sync.WaitGroup
	type result struct {
		pa *PositionAnalysis
	}
	resultCh := make(chan result, len(positions))
	analyzeStart := time.Now()
	activePositions := 0

	for _, pos := range positions {
		amt, _ := strconv.ParseFloat(pos.PositionAmt, 64)
		if amt == 0 {
			continue
		}
		activePositions++
		wg.Add(1)
		go func(p posInfo) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			pa := analyzePosition(c, p)
			resultCh <- result{pa: pa}
		}(posInfo{
			symbol:     pos.Symbol,
			side:       pos.PositionSide,
			entryPrice: parseF(pos.EntryPrice),
			markPrice:  parseF(pos.MarkPrice),
			amount:     amt,
			leverage:   parseInt(pos.Leverage),
			pnl:        parseF(pos.UnRealizedProfit),
		})
	}
	go func() { wg.Wait(); close(resultCh) }()

	var items []PositionAnalysis
	for res := range resultCh {
		if res.pa != nil {
			items = append(items, *res.pa)
		}
	}
	log.Printf("[RecommendAnalyze][%s] analyze_positions done after=%v active=%d output=%d", reqID, time.Since(analyzeStart).Round(time.Millisecond), activePositions, len(items))

	// 按盈亏百分比排序（亏最多的排前面，需要关注）
	sort.Slice(items, func(i, j int) bool {
		return items[i].PnlPercent < items[j].PnlPercent
	})

	ctx.JSON(http.StatusOK, utils.H{
		"data": AnalyzeResponse{
			Items:      items,
			Sentiment:  sentiment,
			AnalyzedAt: time.Now().Format(time.RFC3339),
			Count:      len(items),
		},
	})
	log.Printf("[RecommendAnalyze][%s] done after=%v", reqID, time.Since(start).Round(time.Millisecond))
}

type posInfo struct {
	symbol     string
	side       string
	entryPrice float64
	markPrice  float64
	amount     float64
	leverage   int
	pnl        float64
}

func parseF(s string) float64 {
	v, _ := strconv.ParseFloat(s, 64)
	return v
}

func parseInt(s string) int {
	v, _ := strconv.Atoi(s)
	return v
}

func analyzePosition(ctx context.Context, p posInfo) *PositionAnalysis {
	// 判断持仓方向
	side := "LONG"
	if p.amount < 0 || p.side == "SHORT" {
		side = "SHORT"
	}

	absAmt := math.Abs(p.amount)
	_ = absAmt

	// 盈亏百分比
	pnlPct := 0.0
	if p.entryPrice > 0 && p.markPrice > 0 {
		if side == "LONG" {
			pnlPct = (p.markPrice - p.entryPrice) / p.entryPrice * 100
		} else {
			pnlPct = (p.entryPrice - p.markPrice) / p.entryPrice * 100
		}
		if p.leverage > 0 {
			pnlPct *= float64(p.leverage)
		}
	}

	// 获取 AI 多时间框架信号
	var rec *RecommendItem
	// 先从缓存取
	finalCache.RLock()
	if finalCache.resp != nil {
		for i := range finalCache.resp.Items {
			if finalCache.resp.Items[i].Symbol == p.symbol {
				item := finalCache.resp.Items[i]
				rec = &item
				break
			}
		}
	}
	finalCache.RUnlock()

	// 缓存没有则实时计算
	if rec == nil {
		symCtx, cancel := context.WithTimeout(ctx, 12*time.Second)
		rec = realtimeScanSymbol(symCtx, p.symbol)
		cancel()
	}

	// 默认值
	pa := &PositionAnalysis{
		Symbol:        p.symbol,
		Side:          side,
		EntryPrice:    p.entryPrice,
		MarkPrice:     p.markPrice,
		Amount:        p.amount,
		Leverage:      p.leverage,
		UnrealizedPnl: p.pnl,
		PnlPercent:    math.Round(pnlPct*100) / 100,
	}

	if rec == nil {
		// 无信号也给基本建议
		pa.Advice = "hold"
		pa.AdviceLabel = "数据不足，建议观望"
		pa.Reasons = []string{"暂无足够的技术信号"}
		return pa
	}

	pa.Direction = rec.Direction
	pa.Confidence = rec.Confidence
	pa.Signals = rec.Signals
	pa.StopLoss = rec.StopLoss
	pa.TakeProfit = rec.TakeProfit
	pa.Reasons = rec.Reasons

	// 根据 AI 方向 vs 持仓方向 + 盈亏状态 生成建议
	sameDir := (side == rec.Direction)

	switch {
	// AI 方向与持仓一致
	case sameDir && rec.Confidence >= recStrongConfidence:
		if pnlPct > 10 {
			pa.Advice = "take_profit"
			pa.AdviceLabel = "强信号一致但浮盈较大，考虑部分止盈"
			pa.Reasons = append(pa.Reasons, fmt.Sprintf("浮盈 %.1f%%，建议锁定部分利润", pnlPct))
		} else if pnlPct > 0 {
			pa.Advice = "add"
			pa.AdviceLabel = "强信号方向一致，可考虑加仓"
			pa.Reasons = append(pa.Reasons, fmt.Sprintf("AI %s 信号强度 %d%%，与持仓方向一致", rec.Direction, rec.Confidence))
		} else {
			pa.Advice = "hold"
			pa.AdviceLabel = "信号一致，继续持有等待回本"
			pa.Reasons = append(pa.Reasons, "AI方向与持仓一致，耐心等待")
		}
	case sameDir && rec.Confidence >= recMediumConfidence:
		if pnlPct > 15 {
			pa.Advice = "take_profit"
			pa.AdviceLabel = "浮盈丰厚，建议止盈"
		} else {
			pa.Advice = "hold"
			pa.AdviceLabel = "方向一致，继续持有"
			pa.Reasons = append(pa.Reasons, fmt.Sprintf("AI %s 信号中等 %d%%", rec.Direction, rec.Confidence))
		}
	case sameDir:
		pa.Advice = "hold"
		pa.AdviceLabel = "弱信号一致，谨慎持有"

	// AI 方向与持仓相反
	case !sameDir && rec.Confidence >= recStrongConfidence:
		if pnlPct < -5 {
			pa.Advice = "close"
			pa.AdviceLabel = "强反向信号 + 亏损，建议平仓"
			pa.Reasons = append(pa.Reasons, fmt.Sprintf("⚠ AI看%s与持仓反向，置信度%d%%，浮亏%.1f%%", rec.Direction, rec.Confidence, pnlPct))
		} else if pnlPct < 0 {
			pa.Advice = "reduce"
			pa.AdviceLabel = "强反向信号，建议减仓"
			pa.Reasons = append(pa.Reasons, fmt.Sprintf("⚠ AI看%s，建议减仓控险", rec.Direction))
		} else {
			pa.Advice = "take_profit"
			pa.AdviceLabel = "反向信号出现，建议止盈离场"
			pa.Reasons = append(pa.Reasons, fmt.Sprintf("AI转向%s(置信度%d%%)，建议锁定利润", rec.Direction, rec.Confidence))
		}
	case !sameDir && rec.Confidence >= recMediumConfidence:
		if pnlPct < -8 {
			pa.Advice = "stop_loss"
			pa.AdviceLabel = "反向信号 + 较大亏损，建议止损"
			pa.Reasons = append(pa.Reasons, fmt.Sprintf("⚠ 浮亏%.1f%% + AI看%s，建议止损", pnlPct, rec.Direction))
		} else {
			pa.Advice = "reduce"
			pa.AdviceLabel = "中等反向信号，建议减仓"
		}
	case !sameDir:
		pa.Advice = "hold"
		pa.AdviceLabel = "弱反向信号，暂时观望"
		pa.Reasons = append(pa.Reasons, "反向信号较弱，暂不操作")
	}

	return pa
}

// ========== 辅助 ==========

func patternDirection(p PatternType) string {
	switch p {
	case PatternHammer, PatternEngulfBull:
		return "LONG"
	case PatternShootingStar, PatternEngulfBear:
		return "SHORT"
	default:
		return ""
	}
}

func patternLabel(p PatternType) string {
	switch p {
	case PatternDoji:
		return "十字星"
	case PatternHammer:
		return "锤子线"
	case PatternShootingStar:
		return "射击之星"
	case PatternEngulfBull:
		return "看涨吞没"
	case PatternEngulfBear:
		return "看跌吞没"
	default:
		return string(p)
	}
}
