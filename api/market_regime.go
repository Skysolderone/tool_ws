package api

import (
	"context"
	"log"
	"math"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/utils"
)

// RegimeType 市场状态类型
type RegimeType string

const (
	RegimeTrend   RegimeType = "trend"
	RegimeRange   RegimeType = "range"
	RegimeHighVol RegimeType = "high_volatility"
)

// RegimeIndicators 状态判断指标
type RegimeIndicators struct {
	ADX14          float64 `json:"adx14"`
	BollingerWidth float64 `json:"bollingerWidth"`
	ATRPct         float64 `json:"atrPct"`
	TrendStrength  float64 `json:"trendStrength"` // +1 强上行, -1 强下行, 0 无趋势
}

// RegimeChange 状态切换记录
type RegimeChange struct {
	Time       time.Time  `json:"time"`
	From       RegimeType `json:"from"`
	To         RegimeType `json:"to"`
	Confidence float64    `json:"confidence"`
}

type regimeState struct {
	mu         sync.RWMutex
	current    RegimeType
	confidence float64
	indicators RegimeIndicators
	updatedAt  time.Time
	history    []RegimeChange
	stopCh     chan struct{}
	active     bool
}

var regime = &regimeState{current: RegimeRange}

// StartRegimeDetector 启动市场状态检测器
func StartRegimeDetector(symbol string, intervalSec int) {
	regime.mu.Lock()
	if regime.active {
		regime.mu.Unlock()
		return
	}
	if intervalSec <= 0 {
		intervalSec = 60
	}
	regime.active = true
	regime.stopCh = make(chan struct{})
	regime.mu.Unlock()

	go regimeDetectLoop(symbol, intervalSec)
	log.Printf("[Regime] Detector started for %s, interval=%ds", symbol, intervalSec)
}

// StopRegimeDetector 停止检测器
func StopRegimeDetector() {
	regime.mu.Lock()
	defer regime.mu.Unlock()
	if !regime.active {
		return
	}
	regime.active = false
	close(regime.stopCh)
}

func regimeDetectLoop(symbol string, intervalSec int) {
	time.Sleep(10 * time.Second)
	detectAndUpdate(symbol)

	ticker := time.NewTicker(time.Duration(intervalSec) * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-regime.stopCh:
			return
		case <-ticker.C:
			detectAndUpdate(symbol)
		}
	}
}

func detectAndUpdate(symbol string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// 拉取 4h K线
	klines, err := Client.NewKlinesService().Symbol(symbol).Interval("4h").Limit(30).Do(ctx)
	if err != nil || len(klines) < 20 {
		return
	}

	closes := make([]float64, len(klines))
	highs := make([]float64, len(klines))
	lows := make([]float64, len(klines))
	for i, k := range klines {
		c, _ := parseKlineFloat(k.Close)
		h, _ := parseKlineFloat(k.High)
		l, _ := parseKlineFloat(k.Low)
		closes[i] = c
		highs[i] = h
		lows[i] = l
	}

	// 计算指标
	atrPct := calcATRPct(highs, lows, closes, 14)
	bbWidth := calcBBWidth(closes, 20)
	trendStrength := calcTrendStrength(closes)

	// ADX 简化：用 trend strength 幅度代替
	adx := math.Abs(trendStrength) * 50

	indicators := RegimeIndicators{
		ADX14:          roundFloat(adx, 2),
		BollingerWidth: roundFloat(bbWidth, 4),
		ATRPct:         roundFloat(atrPct, 4),
		TrendStrength:  roundFloat(trendStrength, 4),
	}

	// 判断 regime
	var newRegime RegimeType
	var confidence float64

	if atrPct > 0.06 { // 6% 以上为高波动
		newRegime = RegimeHighVol
		confidence = math.Min(atrPct/0.1, 1)
	} else if adx > 25 {
		newRegime = RegimeTrend
		confidence = math.Min(adx/50, 1)
	} else {
		newRegime = RegimeRange
		confidence = math.Min((50-adx)/50, 1)
	}

	regime.mu.Lock()
	prev := regime.current
	regime.current = newRegime
	regime.confidence = confidence
	regime.indicators = indicators
	regime.updatedAt = time.Now()
	if prev != newRegime {
		change := RegimeChange{Time: time.Now(), From: prev, To: newRegime, Confidence: confidence}
		regime.history = append(regime.history, change)
		if len(regime.history) > 50 {
			regime.history = regime.history[len(regime.history)-50:]
		}
		log.Printf("[Regime] Changed: %s → %s (confidence=%.2f)", prev, newRegime, confidence)
	}
	regime.mu.Unlock()
}

func calcATRPct(highs, lows, closes []float64, period int) float64 {
	n := len(closes)
	if n < period+1 {
		return 0
	}
	var sum float64
	for i := n - period; i < n; i++ {
		tr := math.Max(highs[i]-lows[i], math.Max(math.Abs(highs[i]-closes[i-1]), math.Abs(lows[i]-closes[i-1])))
		sum += tr
	}
	atr := sum / float64(period)
	if closes[n-1] > 0 {
		return atr / closes[n-1]
	}
	return 0
}

func calcBBWidth(closes []float64, period int) float64 {
	n := len(closes)
	if n < period {
		return 0
	}
	slice := closes[n-period:]
	mean, std := meanStd(slice)
	if mean == 0 {
		return 0
	}
	return (2 * std * 2) / mean // BB width = (upper-lower)/middle
}

func calcTrendStrength(closes []float64) float64 {
	n := len(closes)
	if n < 10 {
		return 0
	}
	// 简化：用最近 10 根 K 线的斜率
	first := closes[n-10]
	last := closes[n-1]
	if first == 0 {
		return 0
	}
	return (last - first) / first * 10 // 归一化
}

func parseKlineFloat(s string) (float64, error) {
	return strconv.ParseFloat(s, 64)
}

// GetCurrentRegime 获取当前市场状态
func GetCurrentRegime() (RegimeType, float64, *RegimeIndicators) {
	regime.mu.RLock()
	defer regime.mu.RUnlock()
	ind := regime.indicators
	return regime.current, regime.confidence, &ind
}

// HandleGetRegime GET /tool/regime/status
func HandleGetRegime(c context.Context, ctx *app.RequestContext) {
	regime.mu.RLock()
	data := map[string]interface{}{
		"regime":     regime.current,
		"confidence": regime.confidence,
		"indicators": regime.indicators,
		"updatedAt":  regime.updatedAt,
		"history":    regime.history,
	}
	regime.mu.RUnlock()
	ctx.JSON(http.StatusOK, utils.H{"data": data})
}
