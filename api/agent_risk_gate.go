package api

import (
	"context"
	"fmt"
	"net/http"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/utils"
)

// AgentRiskGateResult Agent 风控预检结果
type AgentRiskGateResult struct {
	Allowed    bool     `json:"allowed"`
	Reasons    []string `json:"reasons"`
	VarImpact  float64  `json:"varImpact"`  // 新增 VaR 影响
	CurrentVar float64  `json:"currentVar"` // 当前 VaR
	BudgetLeft float64  `json:"budgetLeft"` // 剩余风险预算
}

// AgentRiskGateReq 风控预检请求
type AgentRiskGateReq struct {
	Symbol   string  `json:"symbol"`
	Side     string  `json:"side"`     // BUY / SELL
	SizeUSDT float64 `json:"sizeUSDT"` // 下单金额
	Leverage int     `json:"leverage"`
}

// AgentRiskGate 执行 Agent 风控预检
func AgentRiskGate(symbol, side string, sizeUSDT float64, leverage int) *AgentRiskGateResult {
	result := &AgentRiskGateResult{Allowed: true}

	// 1. Kill-Switch 检查
	if err := CheckKillSwitch("agent", symbol); err != nil {
		result.Allowed = false
		result.Reasons = append(result.Reasons, err.Error())
	}

	// 2. 日度风控检查
	if err := CheckRisk(); err != nil {
		result.Allowed = false
		result.Reasons = append(result.Reasons, err.Error())
	}

	// 3. 组合风控检查
	notional := sizeUSDT * float64(leverage)
	isLong := side == "BUY" || side == "LONG"
	if err := CheckPortfolioRisk(symbol, notional, isLong); err != nil {
		result.Allowed = false
		result.Reasons = append(result.Reasons, err.Error())
	}

	// 4. VaR 预算检查
	varSnap := GetVarStatus()
	if varSnap != nil {
		result.CurrentVar = varSnap.TotalVar95
		// 估算新增 VaR
		dailyVol := 0.04
		z95 := 1.645
		addVar := notional * dailyVol * z95
		result.VarImpact = roundFloat(addVar, 2)

		varMu.RLock()
		budget := varCfg.RiskBudgetUSDT
		varMu.RUnlock()

		if budget > 0 {
			result.BudgetLeft = roundFloat(budget-varSnap.TotalVar95, 2)
			if varSnap.TotalVar95+addVar > budget {
				result.Allowed = false
				result.Reasons = append(result.Reasons,
					fmt.Sprintf("VaR 预算不足: 当前 %.0f + 新增 %.0f > 预算 %.0f",
						varSnap.TotalVar95, addVar, budget))
			}
		}
	}

	return result
}

// HandleAgentRiskCheck POST /tool/agent/risk-check
func HandleAgentRiskCheck(c context.Context, ctx *app.RequestContext) {
	var req AgentRiskGateReq
	if err := ctx.BindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, utils.H{"error": "invalid request"})
		return
	}
	result := AgentRiskGate(req.Symbol, req.Side, req.SizeUSDT, req.Leverage)
	ctx.JSON(http.StatusOK, utils.H{"data": result})
}
