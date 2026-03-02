package api

import (
	"context"
	"math"
	"net/http"
	"strconv"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/utils"
)

// StressScenario 冲击测试场景
type StressScenario struct {
	Name                string             `json:"name"`                          // "corr_spike" / "vol_double" / "custom"
	CorrelationOverride float64            `json:"correlationOverride,omitempty"` // 强制相关系数
	VolMultiplier       float64            `json:"volMultiplier,omitempty"`       // 波动率乘数
	PriceShocks         map[string]float64 `json:"priceShocks,omitempty"`        // symbol → 价格变动百分比
}

// StressTestResult 冲击测试结果
type StressTestResult struct {
	Scenario         StressScenario     `json:"scenario"`
	BaseVar95        float64            `json:"baseVar95"`
	StressedVar95    float64            `json:"stressedVar95"`
	MaxDrawdownUSDT  float64            `json:"maxDrawdownUSDT"`
	ExposureBySymbol map[string]float64 `json:"exposureBySymbol"`
	SameDirectionPct float64            `json:"sameDirectionPct"`
}

// RunStressTest 执行冲击测试
func RunStressTest(ctx context.Context, scenarios []StressScenario) ([]StressTestResult, error) {
	positions, err := GetPositions(ctx)
	if err != nil {
		return nil, err
	}

	var posForVar []positionForVar
	var symbols []string
	symbolSet := map[string]bool{}
	exposure := map[string]float64{}
	var longN, shortN float64

	for _, p := range positions {
		amt, _ := strconv.ParseFloat(p.PositionAmt, 64)
		if amt == 0 {
			continue
		}
		mark, _ := strconv.ParseFloat(p.MarkPrice, 64)
		notional := math.Abs(amt) * mark
		side := "LONG"
		if amt < 0 {
			side = "SHORT"
			shortN += notional
		} else {
			longN += notional
		}
		posForVar = append(posForVar, positionForVar{
			Symbol: p.Symbol, Notional: notional, Side: side,
		})
		exposure[p.Symbol] += notional
		if !symbolSet[p.Symbol] {
			symbols = append(symbols, p.Symbol)
			symbolSet[p.Symbol] = true
		}
	}

	totalExposure := longN + shortN
	sameDirPct := 0.0
	if totalExposure > 0 {
		sameDirPct = math.Max(longN, shortN) / totalExposure * 100
	}

	// 基准 VaR
	baseSnap := calcPortfolioVar(posForVar, symbols)

	var results []StressTestResult
	for _, scenario := range scenarios {
		result := runSingleStress(scenario, posForVar, symbols, baseSnap.TotalVar95, exposure, sameDirPct)
		results = append(results, result)
	}

	return results, nil
}

func runSingleStress(scenario StressScenario, positions []positionForVar, symbols []string, baseVar95 float64, exposure map[string]float64, sameDirPct float64) StressTestResult {
	z95 := 1.645
	dailyVol := 0.04

	volMult := scenario.VolMultiplier
	if volMult <= 0 {
		volMult = 1
	}

	var totalVarSq float64
	var maxDrawdown float64

	for _, p := range positions {
		vol := dailyVol * volMult
		singleVar := p.Notional * vol * z95
		totalVarSq += singleVar * singleVar

		// 价格冲击导致的直接损失
		if shock, ok := scenario.PriceShocks[p.Symbol]; ok {
			loss := p.Notional * math.Abs(shock) / 100
			if (p.Side == "LONG" && shock < 0) || (p.Side == "SHORT" && shock > 0) {
				maxDrawdown += loss
			}
		}
	}

	// 相关性覆盖
	corrOverride := scenario.CorrelationOverride
	if corrOverride > 0 && len(positions) > 1 {
		for i := 0; i < len(positions); i++ {
			for j := i + 1; j < len(positions); j++ {
				vol := dailyVol * volMult
				vi := positions[i].Notional * vol * z95
				vj := positions[j].Notional * vol * z95
				totalVarSq += 2 * corrOverride * vi * vj
			}
		}
	}

	stressedVar := math.Sqrt(math.Max(totalVarSq, 0))

	return StressTestResult{
		Scenario:         scenario,
		BaseVar95:        roundFloat(baseVar95, 2),
		StressedVar95:    roundFloat(stressedVar, 2),
		MaxDrawdownUSDT:  roundFloat(maxDrawdown, 2),
		ExposureBySymbol: exposure,
		SameDirectionPct: roundFloat(sameDirPct, 2),
	}
}

// HandleRunStressTest POST /tool/risk/stress-test
func HandleRunStressTest(c context.Context, ctx *app.RequestContext) {
	var req struct {
		Scenarios []StressScenario `json:"scenarios"`
	}
	if err := ctx.BindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, utils.H{"error": "invalid request"})
		return
	}

	if len(req.Scenarios) == 0 {
		// 默认场景
		req.Scenarios = []StressScenario{
			{Name: "corr_spike", CorrelationOverride: 0.9, VolMultiplier: 1},
			{Name: "vol_double", VolMultiplier: 2},
			{Name: "btc_crash", PriceShocks: map[string]float64{"BTCUSDT": -10, "ETHUSDT": -15}},
		}
	}

	results, err := RunStressTest(c, req.Scenarios)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, utils.H{"error": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, utils.H{"data": results})
}
