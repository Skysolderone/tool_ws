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

// CorrelationResult 相关性分析结果
type CorrelationResult struct {
	Symbols  []string    `json:"symbols"`
	Matrix   [][]float64 `json:"matrix"`  // 相关系数矩阵，Matrix[i][j] = symbols[i] 与 symbols[j] 的 Pearson 相关系数
	Period   string      `json:"period"`  // 时间周期，如 "1h"
	Limit    int         `json:"limit"`   // K 线数量
}

// CalcCorrelation 计算多个币种之间的 Pearson 相关系数矩阵
// symbols: 币种列表（至少 2 个），interval: K 线周期（如 "1h"），limit: K 线数量
func CalcCorrelation(symbols []string, interval string, limit int) (*CorrelationResult, error) {
	if len(symbols) < 2 {
		return nil, fmt.Errorf("at least 2 symbols required")
	}
	if interval == "" {
		interval = "1h"
	}
	if limit <= 0 {
		limit = 100
	}
	if limit > 500 {
		limit = 500
	}

	ctx := context.Background()

	// 拉取所有币种的收盘价变化率序列
	returns := make([][]float64, len(symbols))
	for i, sym := range symbols {
		klines, err := Client.NewKlinesService().
			Symbol(sym).
			Interval(interval).
			Limit(limit + 1). // 多取一根用于计算变化率
			Do(ctx)
		if err != nil {
			return nil, fmt.Errorf("fetch klines for %s: %w", sym, err)
		}
		if len(klines) < 2 {
			return nil, fmt.Errorf("insufficient klines for %s: got %d", sym, len(klines))
		}

		// 提取收盘价
		closes := make([]float64, len(klines))
		for j, k := range klines {
			closes[j], _ = strconv.ParseFloat(k.Close, 64)
		}

		// 计算收益率序列（log return 更稳健，此处用简单变化率）
		rets := make([]float64, len(closes)-1)
		for j := 1; j < len(closes); j++ {
			if closes[j-1] > 0 {
				rets[j-1] = (closes[j] - closes[j-1]) / closes[j-1]
			}
		}
		returns[i] = rets
	}

	// 截断到最短长度，确保所有序列等长
	minLen := len(returns[0])
	for _, r := range returns {
		if len(r) < minLen {
			minLen = len(r)
		}
	}
	for i := range returns {
		returns[i] = returns[i][len(returns[i])-minLen:]
	}

	n := len(symbols)
	matrix := make([][]float64, n)
	for i := range matrix {
		matrix[i] = make([]float64, n)
		matrix[i][i] = 1.0 // 自身相关系数为 1
	}

	for i := 0; i < n; i++ {
		for j := i + 1; j < n; j++ {
			r := pearson(returns[i], returns[j])
			matrix[i][j] = r
			matrix[j][i] = r
		}
	}

	return &CorrelationResult{
		Symbols: symbols,
		Matrix:  matrix,
		Period:  interval,
		Limit:   minLen,
	}, nil
}

// pearson 计算两个等长序列的 Pearson 相关系数
func pearson(x, y []float64) float64 {
	n := len(x)
	if n == 0 || len(y) != n {
		return 0
	}

	// 计算均值
	var sumX, sumY float64
	for i := 0; i < n; i++ {
		sumX += x[i]
		sumY += y[i]
	}
	meanX := sumX / float64(n)
	meanY := sumY / float64(n)

	// 计算 Pearson: r = Σ(xi-x̄)(yi-ȳ) / sqrt(Σ(xi-x̄)² * Σ(yi-ȳ)²)
	var num, denomX, denomY float64
	for i := 0; i < n; i++ {
		dx := x[i] - meanX
		dy := y[i] - meanY
		num += dx * dy
		denomX += dx * dx
		denomY += dy * dy
	}

	denom := math.Sqrt(denomX * denomY)
	if denom == 0 {
		return 0
	}

	r := num / denom
	// 钳制到 [-1, 1] 区间（避免浮点误差越界）
	if r > 1 {
		r = 1
	}
	if r < -1 {
		r = -1
	}
	// 保留 4 位小数
	return math.Round(r*10000) / 10000
}

// HandleGetCorrelation GET /tool/analytics/correlation?symbols=BTCUSDT,ETHUSDT,SOLUSDT&interval=1h&limit=100
func HandleGetCorrelation(c context.Context, ctx *app.RequestContext) {
	symbolsRaw := ctx.DefaultQuery("symbols", "")
	if symbolsRaw == "" {
		ctx.JSON(http.StatusBadRequest, utils.H{"error": "symbols is required, e.g. symbols=BTCUSDT,ETHUSDT"})
		return
	}

	parts := strings.Split(symbolsRaw, ",")
	var symbols []string
	for _, p := range parts {
		s := strings.TrimSpace(strings.ToUpper(p))
		if s != "" {
			symbols = append(symbols, s)
		}
	}
	if len(symbols) < 2 {
		ctx.JSON(http.StatusBadRequest, utils.H{"error": "at least 2 symbols required"})
		return
	}

	interval := ctx.DefaultQuery("interval", "1h")
	limitStr := ctx.DefaultQuery("limit", "100")
	limit, _ := strconv.Atoi(limitStr)
	if limit <= 0 {
		limit = 100
	}

	result, err := CalcCorrelation(symbols, interval, limit)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, utils.H{"error": err.Error()})
		return
	}

	ctx.JSON(http.StatusOK, utils.H{"data": result})
}
