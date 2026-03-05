package api

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/utils"
)

// StrategyAdminReq 策略管理请求
type StrategyAdminReq struct {
	StrategyType string  `json:"strategyType"` // scalp/grid/dca...
	Symbol       string  `json:"symbol"`
	Action       string  `json:"action"`           // "demote" / "retire" / "restore"
	Weight       float64 `json:"weight,omitempty"` // demote 时的目标权重
}

// DemoteStrategy 降权策略
func DemoteStrategy(strategyType, symbol string, weight float64) error {
	if DB == nil {
		return fmt.Errorf("database not initialized")
	}

	result := DB.Model(&StrategyState{}).
		Where("strategy_type = ? AND symbol = ? AND status = 'ACTIVE'", strategyType, symbol).
		Update("demoted", true)
	if result.RowsAffected == 0 {
		return fmt.Errorf("no active strategy found: %s/%s", strategyType, symbol)
	}

	log.Printf("[StrategyAdmin] Demoted %s/%s weight=%.2f", strategyType, symbol, weight)
	syncStrategyStateFromDB(strategyType, symbol)
	SendNotify(fmt.Sprintf("📉 策略降权: %s/%s → 权重 %.2f", strategyType, symbol, weight))
	return nil
}

// RetireStrategy 退役策略（停止 + 权重归零）
func RetireStrategy(strategyType, symbol string) error {
	// 停止策略
	stopErr := stopStrategyByType(strategyType, symbol)
	if stopErr != nil {
		log.Printf("[StrategyAdmin] Stop %s/%s error: %v", strategyType, symbol, stopErr)
	}

	// 标记为 STOPPED
	MarkStrategyStopped(strategyType, symbol)

	log.Printf("[StrategyAdmin] Retired %s/%s", strategyType, symbol)
	SendNotify(fmt.Sprintf("🛑 策略退役: %s/%s", strategyType, symbol))
	return nil
}

// RestoreStrategy 恢复策略
func RestoreStrategy(strategyType, symbol string) error {
	if DB == nil {
		return fmt.Errorf("database not initialized")
	}

	DB.Model(&StrategyState{}).
		Where("strategy_type = ? AND symbol = ?", strategyType, symbol).
		Update("demoted", false)

	syncStrategyStateFromDB(strategyType, symbol)
	log.Printf("[StrategyAdmin] Restored %s/%s", strategyType, symbol)
	return nil
}

func stopStrategyByType(strategyType, symbol string) error {
	switch strings.ToLower(strategyType) {
	case "scalp":
		return StopScalp(symbol)
	case "grid":
		return StopGrid(symbol)
	case "signal":
		return StopSignalStrategy(symbol)
	case "doji":
		return StopDojiStrategy(symbol)
	case "dca":
		return StopDCA(symbol)
	case "autoscale":
		return StopAutoScale(symbol)
	default:
		return fmt.Errorf("unsupported strategy type: %s", strategyType)
	}
}

// HandleStrategyAdmin POST /tool/strategy/admin
func HandleStrategyAdmin(c context.Context, ctx *app.RequestContext) {
	var req StrategyAdminReq
	if err := ctx.BindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, utils.H{"error": "invalid request"})
		return
	}

	var err error
	switch strings.ToLower(req.Action) {
	case "demote":
		err = DemoteStrategy(req.StrategyType, req.Symbol, req.Weight)
	case "retire":
		err = RetireStrategy(req.StrategyType, req.Symbol)
	case "restore":
		err = RestoreStrategy(req.StrategyType, req.Symbol)
	default:
		ctx.JSON(http.StatusBadRequest, utils.H{"error": "action must be demote/retire/restore"})
		return
	}

	if err != nil {
		ctx.JSON(http.StatusInternalServerError, utils.H{"error": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, utils.H{"message": req.Action + " success", "strategyType": req.StrategyType, "symbol": req.Symbol})
}
