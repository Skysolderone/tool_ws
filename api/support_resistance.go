package api

import (
	"context"
	"fmt"
	"log"
	"math"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/adshao/go-binance/v2/futures"
)

// ========== 支撑/阻力位计算 ==========
// 基于多时间框架 K 线的 Swing High/Low 检测 + 跨周期汇合 + 经典 Pivot Points

// SRLevel 单个支撑/阻力级别
type SRLevel struct {
	Price      float64  `json:"price"`
	Type       string   `json:"type"`       // "SUPPORT" / "RESISTANCE"
	Strength   int      `json:"strength"`   // 1-4 汇合的周期数量
	Timeframes []string `json:"timeframes"` // 出现在哪些周期
	Volume     float64  `json:"volume"`     // 该价位累计成交量
	Distance   float64  `json:"distance"`   // 距当前价百分比（正=上方, 负=下方）
	TouchCount int      `json:"touchCount"` // 被触及次数
	ZoneLow    float64  `json:"zoneLow"`
	ZoneHigh   float64  `json:"zoneHigh"`
}

// SRZone 强支撑/阻力区间
type SRZone struct {
	Lower      float64  `json:"lower"`
	Upper      float64  `json:"upper"`
	Mid        float64  `json:"mid"`
	Type       string   `json:"type"`       // "SUPPORT" / "RESISTANCE"
	Strength   int      `json:"strength"`   // 1-4 汇合的周期数量
	Timeframes []string `json:"timeframes"` // 出现在哪些周期
	TouchCount int      `json:"touchCount"` // 被触及次数
	Distance   float64  `json:"distance"`   // 区间中点距当前价百分比
}

// PivotSet 经典 Pivot Points
type PivotSet struct {
	PP float64 `json:"pp"`
	R1 float64 `json:"r1"`
	R2 float64 `json:"r2"`
	R3 float64 `json:"r3"`
	S1 float64 `json:"s1"`
	S2 float64 `json:"s2"`
	S3 float64 `json:"s3"`
}

// SRResponse 完整返回
type SRResponse struct {
	Symbol                string    `json:"symbol"`
	CurrentPrice          float64   `json:"currentPrice"`
	Supports              []SRLevel `json:"supports"`
	Resistances           []SRLevel `json:"resistances"`
	StrongSupportZones    []SRZone  `json:"strongSupportZones"`
	StrongResistanceZones []SRZone  `json:"strongResistanceZones"`
	PivotPoints           *PivotSet `json:"pivotPoints"`
	ClosestSupport        *SRLevel  `json:"closestSupport"`
	ClosestResist         *SRLevel  `json:"closestResist"`
	ClosestSupportZone    *SRZone   `json:"closestSupportZone"`
	ClosestResistZone     *SRZone   `json:"closestResistZone"`
	CalculatedAt          string    `json:"calculatedAt"`
}

// swingPoint 内部用的 swing 检测结果
type swingPoint struct {
	price     float64
	volume    float64
	isHigh    bool // true = swing high (resistance), false = swing low (support)
	timeframe string
	index     int // 在 K 线数组中的位置
	recency   float64
}

// tfConfig 时间框架配置
type tfConfig struct {
	interval string // "1h", "4h", "1d", "1w"
	limit    int    // 拉取根数
	period   int    // swing 检测窗口
	label    string // 显示标签
}

var srTimeframes = []tfConfig{
	{interval: "1h", limit: 50, period: 5, label: "1h"},
	{interval: "4h", limit: 48, period: 5, label: "4h"},
	{interval: "1d", limit: 60, period: 10, label: "1d"},
	{interval: "1w", limit: 52, period: 5, label: "1w"},
}

// GetSRLevels 计算支撑/阻力位（主入口）
func GetSRLevels(ctx context.Context, symbol string) (*SRResponse, error) {
	if symbol == "" {
		return nil, fmt.Errorf("symbol is required")
	}

	// 1. 获取当前价格
	cache := GetPriceCache()
	currentPrice, err := cache.GetPrice(symbol)
	if err != nil {
		// 回退到 REST API
		currentPrice, err = getCurrentPrice(ctx, symbol, "")
		if err != nil {
			return nil, fmt.Errorf("get current price: %w", err)
		}
	}

	// 2. 并发获取多时间框架 K 线
	allKlines, err := fetchMultiTimeframeKlines(ctx, symbol)
	if err != nil {
		return nil, fmt.Errorf("fetch klines: %w", err)
	}

	// 3. 每个时间框架检测 swing points
	var allSwings []swingPoint
	for _, tf := range srTimeframes {
		klines, ok := allKlines[tf.interval]
		if !ok || len(klines) < tf.period*2+1 {
			continue
		}
		swings := detectSwingLevels(klines, tf.period, tf.label)
		allSwings = append(allSwings, swings...)
	}

	// 4. 跨周期合并 + 强度评分（容差按波动率自适应）
	tolerancePercent := calcAdaptiveTolerancePercent(allKlines, 0.15)
	log.Printf("[SR] %s adaptive tolerance: %.3f%%", symbol, tolerancePercent)
	supports, resistances := mergeAndRankLevels(allSwings, currentPrice, tolerancePercent)
	supportZones := buildStrongZones(supports, currentPrice, "SUPPORT")
	resistanceZones := buildStrongZones(resistances, currentPrice, "RESISTANCE")

	// 5. 计算经典 Pivot Points（用日线）
	var pivot *PivotSet
	if dailyKlines, ok := allKlines["1d"]; ok && len(dailyKlines) >= 2 {
		pivot = calculatePivotPoints(dailyKlines)
	}

	// 6. 找最近的支撑/阻力
	var closestSupport, closestResist *SRLevel
	if len(supports) > 0 {
		s := supports[0]
		closestSupport = &s
	}
	if len(resistances) > 0 {
		r := resistances[0]
		closestResist = &r
	}
	var closestSupportZone, closestResistZone *SRZone
	if len(supportZones) > 0 {
		z := supportZones[0]
		closestSupportZone = &z
	}
	if len(resistanceZones) > 0 {
		z := resistanceZones[0]
		closestResistZone = &z
	}

	return &SRResponse{
		Symbol:                symbol,
		CurrentPrice:          currentPrice,
		Supports:              supports,
		Resistances:           resistances,
		StrongSupportZones:    supportZones,
		StrongResistanceZones: resistanceZones,
		PivotPoints:           pivot,
		ClosestSupport:        closestSupport,
		ClosestResist:         closestResist,
		ClosestSupportZone:    closestSupportZone,
		ClosestResistZone:     closestResistZone,
		CalculatedAt:          time.Now().Format("2006-01-02 15:04:05"),
	}, nil
}

// fetchMultiTimeframeKlines 并发拉取多个时间框架的 K 线
func fetchMultiTimeframeKlines(ctx context.Context, symbol string) (map[string][]*futures.Kline, error) {
	result := make(map[string][]*futures.Kline)
	var mu sync.Mutex
	var wg sync.WaitGroup
	var firstErr error

	for _, tf := range srTimeframes {
		wg.Add(1)
		go func(tf tfConfig) {
			defer wg.Done()
			klines, err := Client.NewKlinesService().
				Symbol(symbol).
				Interval(tf.interval).
				Limit(tf.limit).
				Do(ctx)
			if err != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = fmt.Errorf("%s klines: %w", tf.interval, err)
				}
				mu.Unlock()
				return
			}
			mu.Lock()
			result[tf.interval] = klines
			mu.Unlock()
		}(tf)
	}
	wg.Wait()

	// 至少需要一个时间框架成功
	if len(result) == 0 {
		if firstErr != nil {
			return nil, firstErr
		}
		return nil, fmt.Errorf("no kline data available")
	}

	log.Printf("[SR] Fetched klines: %s %v timeframes", symbol, func() []string {
		var tfs []string
		for k := range result {
			tfs = append(tfs, k)
		}
		return tfs
	}())

	return result, nil
}

// detectSwingLevels 在一组 K 线中检测 swing high/low
// period = 前后看 N 根 K 线作为窗口
func detectSwingLevels(klines []*futures.Kline, period int, timeframe string) []swingPoint {
	n := len(klines)
	var points []swingPoint

	for i := period; i < n-period; i++ {
		high, _ := strconv.ParseFloat(klines[i].High, 64)
		low, _ := strconv.ParseFloat(klines[i].Low, 64)
		vol, _ := strconv.ParseFloat(klines[i].Volume, 64)

		isSwingHigh := true
		isSwingLow := true

		for j := i - period; j <= i+period; j++ {
			if j == i {
				continue
			}
			jHigh, _ := strconv.ParseFloat(klines[j].High, 64)
			jLow, _ := strconv.ParseFloat(klines[j].Low, 64)

			if jHigh >= high {
				isSwingHigh = false
			}
			if jLow <= low {
				isSwingLow = false
			}
		}

		if isSwingHigh {
			points = append(points, swingPoint{
				price:     high,
				volume:    vol,
				isHigh:    true,
				timeframe: timeframe,
				index:     i,
				recency:   calcSwingRecency(i, n),
			})
		}
		if isSwingLow {
			points = append(points, swingPoint{
				price:     low,
				volume:    vol,
				isHigh:    false,
				timeframe: timeframe,
				index:     i,
				recency:   calcSwingRecency(i, n),
			})
		}
	}

	return points
}

// calcSwingRecency 返回 0~1 的新鲜度权重，越靠近最近 K 线越高
func calcSwingRecency(index, total int) float64 {
	if total <= 1 {
		return 1
	}
	ratio := float64(index) / float64(total-1)
	if ratio < 0 {
		return 0
	}
	if ratio > 1 {
		return 1
	}
	return ratio
}

// calcAdaptiveTolerancePercent 根据多周期 ATR 估算动态聚类容差百分比
func calcAdaptiveTolerancePercent(allKlines map[string][]*futures.Kline, fallback float64) float64 {
	type tfWeight struct {
		weight float64
	}
	weights := map[string]tfWeight{
		"1h": {weight: 0.9},
		"4h": {weight: 1.0},
		"1d": {weight: 1.3},
		"1w": {weight: 0.8},
	}

	var weightedSum float64
	var totalWeight float64
	for tf, cfg := range weights {
		klines := allKlines[tf]
		atrPercent := calcATRPercent(klines, 14)
		if atrPercent <= 0 || math.IsNaN(atrPercent) || math.IsInf(atrPercent, 0) {
			continue
		}
		weightedSum += atrPercent * cfg.weight
		totalWeight += cfg.weight
	}

	if totalWeight == 0 {
		return fallback
	}

	avgATRPercent := weightedSum / totalWeight
	// 容差约为 ATR 的 35%，并做上下限约束。
	tolerancePercent := avgATRPercent * 0.35
	if tolerancePercent < 0.12 {
		return 0.12
	}
	if tolerancePercent > 0.8 {
		return 0.8
	}
	return tolerancePercent
}

// calcATRPercent 计算最近 period 根已完成 K 线的 ATR 百分比
func calcATRPercent(klines []*futures.Kline, period int) float64 {
	n := len(klines)
	// 至少需要 period+1 根用于 prev close，且使用倒数第二根作为最后一根已完成K线。
	if n < period+2 {
		return 0
	}

	lastCompleted := n - 2
	start := lastCompleted - period + 1
	if start < 1 {
		start = 1
	}

	var trSum float64
	var count int
	for i := start; i <= lastCompleted; i++ {
		high, okHigh := parsePositiveFloat(klines[i].High)
		low, okLow := parsePositiveFloat(klines[i].Low)
		prevClose, okPrevClose := parsePositiveFloat(klines[i-1].Close)
		if !okHigh || !okLow || !okPrevClose {
			continue
		}
		tr := math.Max(high-low, math.Max(math.Abs(high-prevClose), math.Abs(low-prevClose)))
		trSum += tr
		count++
	}
	if count == 0 {
		return 0
	}

	closePrice, okClose := parsePositiveFloat(klines[lastCompleted].Close)
	if !okClose {
		return 0
	}
	atr := trSum / float64(count)
	return atr / closePrice * 100
}

func parsePositiveFloat(s string) (float64, bool) {
	v, err := strconv.ParseFloat(s, 64)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) || v <= 0 {
		return 0, false
	}
	return v, true
}

// mergeAndRankLevels 跨周期合并去重 + 强度评分
// tolerancePercent: 价格容差百分比（0.15 = 0.15%）
func mergeAndRankLevels(allSwings []swingPoint, currentPrice, tolerancePercent float64) ([]SRLevel, []SRLevel) {
	if len(allSwings) == 0 {
		return nil, nil
	}

	tolerance := currentPrice * tolerancePercent / 100

	// 按价格排序方便聚类
	sort.Slice(allSwings, func(i, j int) bool {
		return allSwings[i].price < allSwings[j].price
	})

	// 聚类：相邻 swing points 在容差内的归为同一级别
	type cluster struct {
		priceSum   float64
		volumeSum  float64
		count      int
		timeframes map[string]bool
		isHigh     bool // 多数投票
		highCount  int
		lowCount   int
		recencySum float64
		minPrice   float64
		maxPrice   float64
	}

	type rankedLevel struct {
		level SRLevel
		score float64
	}

	var clusters []cluster
	visited := make([]bool, len(allSwings))

	for i := 0; i < len(allSwings); i++ {
		if visited[i] {
			continue
		}

		c := cluster{
			priceSum:   allSwings[i].price,
			volumeSum:  allSwings[i].volume,
			count:      1,
			timeframes: map[string]bool{allSwings[i].timeframe: true},
			recencySum: allSwings[i].recency,
			minPrice:   allSwings[i].price,
			maxPrice:   allSwings[i].price,
		}
		if allSwings[i].isHigh {
			c.highCount = 1
		} else {
			c.lowCount = 1
		}
		visited[i] = true

		for j := i + 1; j < len(allSwings); j++ {
			if visited[j] {
				continue
			}
			if allSwings[j].price-allSwings[i].price > tolerance*2 {
				break // 超过容差范围，后面的都不会在范围内
			}
			// 检查与簇中心的距离
			center := c.priceSum / float64(c.count)
			if math.Abs(allSwings[j].price-center) <= tolerance {
				c.priceSum += allSwings[j].price
				c.volumeSum += allSwings[j].volume
				c.count++
				c.timeframes[allSwings[j].timeframe] = true
				c.recencySum += allSwings[j].recency
				c.minPrice = math.Min(c.minPrice, allSwings[j].price)
				c.maxPrice = math.Max(c.maxPrice, allSwings[j].price)
				if allSwings[j].isHigh {
					c.highCount++
				} else {
					c.lowCount++
				}
				visited[j] = true
			}
		}

		c.isHigh = c.highCount >= c.lowCount
		clusters = append(clusters, c)
	}

	// 转换为 SRLevel
	var supportRanked, resistanceRanked []rankedLevel
	sideSlack := tolerance
	for _, c := range clusters {
		avgPrice := c.priceSum / float64(c.count)
		dist := (avgPrice - currentPrice) / currentPrice * 100

		tfs := make([]string, 0, len(c.timeframes))
		for tf := range c.timeframes {
			tfs = append(tfs, tf)
		}
		// 按周期顺序排序
		sort.Slice(tfs, func(i, j int) bool {
			return tfOrder(tfs[i]) < tfOrder(tfs[j])
		})

		level := SRLevel{
			Price:      math.Round(avgPrice*100) / 100, // 保留2位小数
			Strength:   len(c.timeframes),              // 汇合度 = 出现在几个周期
			Timeframes: tfs,
			Volume:     c.volumeSum,
			Distance:   math.Round(dist*100) / 100,
			TouchCount: c.count,
		}
		zonePad := tolerance * (0.4 + 0.1*float64(minInt(level.Strength, 4)-1))
		zoneLow := math.Max(0, c.minPrice-zonePad)
		zoneHigh := c.maxPrice + zonePad
		if zoneHigh < zoneLow {
			zoneLow, zoneHigh = zoneHigh, zoneLow
		}
		level.ZoneLow = round2(zoneLow)
		level.ZoneHigh = round2(zoneHigh)

		avgRecency := c.recencySum / float64(c.count)
		dominance := math.Abs(float64(c.highCount-c.lowCount)) / float64(c.count)
		touchBonus := math.Min(float64(c.count), 6) / 6.0
		score := float64(level.Strength)*2.0 + avgRecency + dominance + touchBonus

		if c.isHigh {
			// 已明显跌破的旧阻力（远离当前价）直接过滤，避免误导。
			if avgPrice < currentPrice-sideSlack {
				continue
			}
			level.Type = "RESISTANCE"
			resistanceRanked = append(resistanceRanked, rankedLevel{level: level, score: score})
		} else {
			// 已明显突破的旧支撑（远离当前价）直接过滤，避免误导。
			if avgPrice > currentPrice+sideSlack {
				continue
			}
			level.Type = "SUPPORT"
			supportRanked = append(supportRanked, rankedLevel{level: level, score: score})
		}
	}

	// 支撑位：先按综合分数，再按距离近
	sort.Slice(supportRanked, func(i, j int) bool {
		if supportRanked[i].score != supportRanked[j].score {
			return supportRanked[i].score > supportRanked[j].score
		}
		return math.Abs(supportRanked[i].level.Distance) < math.Abs(supportRanked[j].level.Distance)
	})

	// 阻力位：先按综合分数，再按距离近
	sort.Slice(resistanceRanked, func(i, j int) bool {
		if resistanceRanked[i].score != resistanceRanked[j].score {
			return resistanceRanked[i].score > resistanceRanked[j].score
		}
		return math.Abs(resistanceRanked[i].level.Distance) < math.Abs(resistanceRanked[j].level.Distance)
	})

	supports := make([]SRLevel, 0, len(supportRanked))
	for _, r := range supportRanked {
		supports = append(supports, r.level)
	}
	resistances := make([]SRLevel, 0, len(resistanceRanked))
	for _, r := range resistanceRanked {
		resistances = append(resistances, r.level)
	}

	// 限制返回数量
	maxLevels := 8
	if len(supports) > maxLevels {
		supports = supports[:maxLevels]
	}
	if len(resistances) > maxLevels {
		resistances = resistances[:maxLevels]
	}

	return supports, resistances
}

func buildStrongZones(levels []SRLevel, currentPrice float64, zoneType string) []SRZone {
	if len(levels) == 0 {
		return nil
	}

	// 强区间优先规则：至少具备跨周期汇合或多次触达。
	var zones []SRZone
	for _, lv := range levels {
		if lv.Strength < 2 && lv.TouchCount < 2 {
			continue
		}
		zones = appendOrMergeZone(zones, levelToZone(lv, currentPrice, zoneType), currentPrice)
	}

	// 若没有满足“强”规则，退化为最近的前两个区间，避免空数据。
	if len(zones) == 0 {
		for i := 0; i < len(levels) && i < 2; i++ {
			zones = appendOrMergeZone(zones, levelToZone(levels[i], currentPrice, zoneType), currentPrice)
		}
	}

	sort.Slice(zones, func(i, j int) bool {
		if zones[i].Strength != zones[j].Strength {
			return zones[i].Strength > zones[j].Strength
		}
		return math.Abs(zones[i].Distance) < math.Abs(zones[j].Distance)
	})

	maxZones := 4
	if len(zones) > maxZones {
		zones = zones[:maxZones]
	}
	return zones
}

func levelToZone(level SRLevel, currentPrice float64, zoneType string) SRZone {
	lower := level.ZoneLow
	upper := level.ZoneHigh
	if lower <= 0 || upper <= 0 || upper < lower {
		halfWidth := math.Max(level.Price*0.0008, 0.01)
		lower = math.Max(0, level.Price-halfWidth)
		upper = level.Price + halfWidth
	}
	mid := (lower + upper) / 2
	distance := 0.0
	if currentPrice > 0 {
		distance = (mid - currentPrice) / currentPrice * 100
	}
	return SRZone{
		Lower:      round2(lower),
		Upper:      round2(upper),
		Mid:        round2(mid),
		Type:       zoneType,
		Strength:   level.Strength,
		Timeframes: append([]string(nil), level.Timeframes...),
		TouchCount: level.TouchCount,
		Distance:   round2(distance),
	}
}

func appendOrMergeZone(zones []SRZone, candidate SRZone, currentPrice float64) []SRZone {
	nearGap := currentPrice * 0.05 / 100 // 0.05%
	if nearGap <= 0 {
		nearGap = candidate.Mid * 0.0005
	}

	for i := range zones {
		if zones[i].Type != candidate.Type {
			continue
		}
		if candidate.Lower <= zones[i].Upper+nearGap && candidate.Upper >= zones[i].Lower-nearGap {
			zones[i].Lower = round2(math.Min(zones[i].Lower, candidate.Lower))
			zones[i].Upper = round2(math.Max(zones[i].Upper, candidate.Upper))
			zones[i].Mid = round2((zones[i].Lower + zones[i].Upper) / 2)
			zones[i].Strength = maxInt(zones[i].Strength, candidate.Strength)
			zones[i].TouchCount += candidate.TouchCount
			zones[i].Timeframes = unionTimeframes(zones[i].Timeframes, candidate.Timeframes)
			if currentPrice > 0 {
				zones[i].Distance = round2((zones[i].Mid - currentPrice) / currentPrice * 100)
			}
			return zones
		}
	}
	return append(zones, candidate)
}

func unionTimeframes(a, b []string) []string {
	set := make(map[string]bool, len(a)+len(b))
	for _, tf := range a {
		if tf != "" {
			set[tf] = true
		}
	}
	for _, tf := range b {
		if tf != "" {
			set[tf] = true
		}
	}
	out := make([]string, 0, len(set))
	for tf := range set {
		out = append(out, tf)
	}
	sort.Slice(out, func(i, j int) bool {
		return tfOrder(out[i]) < tfOrder(out[j])
	})
	return out
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// calculatePivotPoints 根据日线计算经典 Pivot Points
func calculatePivotPoints(dailyKlines []*futures.Kline) *PivotSet {
	// 使用前一根日线（已完成的）
	prev := dailyKlines[len(dailyKlines)-2]
	high, _ := strconv.ParseFloat(prev.High, 64)
	low, _ := strconv.ParseFloat(prev.Low, 64)
	close_, _ := strconv.ParseFloat(prev.Close, 64)

	pp := (high + low + close_) / 3

	return &PivotSet{
		PP: round2(pp),
		R1: round2(2*pp - low),
		S1: round2(2*pp - high),
		R2: round2(pp + (high - low)),
		S2: round2(pp - (high - low)),
		R3: round2(high + 2*(pp-low)),
		S3: round2(low - 2*(high-pp)),
	}
}

// round2 保留 2 位小数
func round2(v float64) float64 {
	return math.Round(v*100) / 100
}

// tfOrder 时间框架排序权重
func tfOrder(tf string) int {
	switch tf {
	case "1h":
		return 1
	case "4h":
		return 2
	case "1d":
		return 3
	case "1w":
		return 4
	default:
		return 9
	}
}
