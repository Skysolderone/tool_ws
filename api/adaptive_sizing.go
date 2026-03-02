package api

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/utils"
)

// AdaptiveSizeConfig 自适应下单量配置
type AdaptiveSizeConfig struct {
	BaseUSDT       float64 `json:"baseUSDT"`       // 基础下单金额
	VolScaleFactor float64 `json:"volScaleFactor"`  // 波动率缩放系数（默认 1.0）
	DepthMinUSDT   float64 `json:"depthMinUSDT"`   // 深度不足时最小金额
	MaxUSDT        float64 `json:"maxUSDT"`         // 上限
}

// SizeDecision 下单量决策结果
type SizeDecision struct {
	OriginalUSDT float64 `json:"originalUSDT"`
	AdjustedUSDT float64 `json:"adjustedUSDT"`
	Volatility   float64 `json:"volatility"`   // 当前波动率
	DepthUSDT    float64 `json:"depthUSDT"`    // 盘口可用深度
	Reason       string  `json:"reason"`
}

// CalcAdaptiveSize 根据波动率和深度计算自适应下单量
func CalcAdaptiveSize(ctx context.Context, symbol string, baseUSDT float64, cfg AdaptiveSizeConfig) (*SizeDecision, error) {
	if baseUSDT <= 0 {
		baseUSDT = cfg.BaseUSDT
	}
	if baseUSDT <= 0 {
		return nil, fmt.Errorf("baseUSDT must be positive")
	}

	decision := &SizeDecision{
		OriginalUSDT: baseUSDT,
		AdjustedUSDT: baseUSDT,
		Reason:       "no adjustment",
	}

	volScale := cfg.VolScaleFactor
	if volScale <= 0 {
		volScale = 1.0
	}

	// 获取波动率（ATR%）
	vol := getCurrentVolatility(ctx, symbol)
	decision.Volatility = vol

	if vol > 0 {
		// 高波动时缩小仓位：adjusted = base * (normalVol / currentVol)
		normalVol := 0.03 // 3% 视为正常波动率
		if vol > normalVol {
			ratio := normalVol / vol * volScale
			decision.AdjustedUSDT = baseUSDT * math.Max(ratio, 0.3) // 最低 30%
			decision.Reason = fmt.Sprintf("high volatility (%.2f%%), reduced", vol*100)
		}
	}

	// 检查盘口深度
	flow, err := AnalyzeOrderFlow(symbol, 10)
	if err == nil && flow != nil {
		decision.DepthUSDT = flow.BidVolume + flow.AskVolume
		// 下单量不超过可用深度的 10%
		maxByDepth := decision.DepthUSDT * 0.1
		if maxByDepth > 0 && decision.AdjustedUSDT > maxByDepth {
			decision.AdjustedUSDT = maxByDepth
			decision.Reason = fmt.Sprintf("depth limited (depth=%.0f USDT)", decision.DepthUSDT)
		}
	}

	// 下限
	minUSDT := cfg.DepthMinUSDT
	if minUSDT <= 0 {
		minUSDT = 5
	}
	if decision.AdjustedUSDT < minUSDT {
		decision.AdjustedUSDT = minUSDT
	}

	// 上限
	if cfg.MaxUSDT > 0 && decision.AdjustedUSDT > cfg.MaxUSDT {
		decision.AdjustedUSDT = cfg.MaxUSDT
		decision.Reason = "capped at max"
	}

	decision.AdjustedUSDT = roundFloat(decision.AdjustedUSDT, 2)
	return decision, nil
}

func getCurrentVolatility(ctx context.Context, symbol string) float64 {
	// 拉取最近 14 根 1h K线计算 ATR%
	klines, err := Client.NewKlinesService().Symbol(symbol).Interval("1h").Limit(15).Do(ctx)
	if err != nil || len(klines) < 2 {
		return 0
	}

	var atrSum float64
	for i := 1; i < len(klines); i++ {
		h, _ := strconv.ParseFloat(klines[i].High, 64)
		l, _ := strconv.ParseFloat(klines[i].Low, 64)
		c, _ := strconv.ParseFloat(klines[i-1].Close, 64)
		tr := math.Max(h-l, math.Max(math.Abs(h-c), math.Abs(l-c)))
		atrSum += tr
	}
	atr := atrSum / float64(len(klines)-1)
	lastClose, _ := strconv.ParseFloat(klines[len(klines)-1].Close, 64)
	if lastClose > 0 {
		return atr / lastClose
	}
	return 0
}

// HandleGetAdaptiveSize GET /tool/adaptive-size?symbol=BTCUSDT&baseUSDT=100
func HandleGetAdaptiveSize(c context.Context, ctx *app.RequestContext) {
	symbol := strings.ToUpper(strings.TrimSpace(string(ctx.Query("symbol"))))
	if symbol == "" {
		symbol = "BTCUSDT"
	}
	baseUSDT, _ := strconv.ParseFloat(string(ctx.Query("baseUSDT")), 64)
	if baseUSDT <= 0 {
		baseUSDT = 100
	}

	decision, err := CalcAdaptiveSize(c, symbol, baseUSDT, AdaptiveSizeConfig{})
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, utils.H{"error": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, utils.H{"data": decision})
}
