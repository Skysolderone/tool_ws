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
	Symbol         string    `json:"symbol"`
	CurrentPrice   float64   `json:"currentPrice"`
	Supports       []SRLevel `json:"supports"`
	Resistances    []SRLevel `json:"resistances"`
	PivotPoints    *PivotSet `json:"pivotPoints"`
	ClosestSupport *SRLevel  `json:"closestSupport"`
	ClosestResist  *SRLevel  `json:"closestResist"`
	CalculatedAt   string    `json:"calculatedAt"`
}

// swingPoint 内部用的 swing 检测结果
type swingPoint struct {
	price     float64
	volume    float64
	isHigh    bool // true = swing high (resistance), false = swing low (support)
	timeframe string
	index     int // 在 K 线数组中的位置
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

	// 4. 跨周期合并 + 强度评分
	supports, resistances := mergeAndRankLevels(allSwings, currentPrice, 0.15)

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

	return &SRResponse{
		Symbol:         symbol,
		CurrentPrice:   currentPrice,
		Supports:       supports,
		Resistances:    resistances,
		PivotPoints:    pivot,
		ClosestSupport: closestSupport,
		ClosestResist:  closestResist,
		CalculatedAt:   time.Now().Format("2006-01-02 15:04:05"),
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
			})
		}
		if isSwingLow {
			points = append(points, swingPoint{
				price:     low,
				volume:    vol,
				isHigh:    false,
				timeframe: timeframe,
				index:     i,
			})
		}
	}

	return points
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
	var supports, resistances []SRLevel
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
			Type:       "SUPPORT",
			Strength:   len(c.timeframes), // 汇合度 = 出现在几个周期
			Timeframes: tfs,
			Volume:     c.volumeSum,
			Distance:   math.Round(dist*100) / 100,
			TouchCount: c.count,
		}

		if avgPrice >= currentPrice {
			level.Type = "RESISTANCE"
			resistances = append(resistances, level)
		} else {
			supports = append(supports, level)
		}
	}

	// 支撑位：按距离近排序（距离为负，绝对值小的在前），强度大的优先
	sort.Slice(supports, func(i, j int) bool {
		if supports[i].Strength != supports[j].Strength {
			return supports[i].Strength > supports[j].Strength
		}
		return math.Abs(supports[i].Distance) < math.Abs(supports[j].Distance)
	})

	// 阻力位：按距离近排序（距离为正，值小的在前），强度大的优先
	sort.Slice(resistances, func(i, j int) bool {
		if resistances[i].Strength != resistances[j].Strength {
			return resistances[i].Strength > resistances[j].Strength
		}
		return resistances[i].Distance < resistances[j].Distance
	})

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
