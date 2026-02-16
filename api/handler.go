package api

import (
	"context"
	"log"
	"net/http"
	"strconv"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/utils"
)

// HandleGetBalance GET /api/balance
func HandleGetBalance(c context.Context, ctx *app.RequestContext) {
	balance, err := GetBalance(c)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, utils.H{"error": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, utils.H{"data": balance})
}

// HandleGetPositions GET /api/positions?symbol=BTCUSDT
func HandleGetPositions(c context.Context, ctx *app.RequestContext) {
	// symbol := ctx.DefaultQuery("symbol", "")
	positions, err := GetPositionsViaWs(c)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, utils.H{"error": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, utils.H{"data": positions})
}

// HandlePlaceOrder POST /api/order
func HandlePlaceOrder(c context.Context, ctx *app.RequestContext) {
	// 风控检查
	if err := CheckRisk(); err != nil {
		ctx.JSON(http.StatusForbidden, utils.H{"error": err.Error()})
		return
	}

	var req PlaceOrderReq
	if err := ctx.BindAndValidate(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, utils.H{"error": err.Error()})
		return
	}
	resp, err := PlaceOrderViaWs(c, req)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, utils.H{"error": err.Error()})
		return
	}

	// 异步保存交易记录到数据库
	go func() {
		if resp == nil || resp.Order == nil {
			return
		}
		record := &TradeRecord{
			Symbol:        req.Symbol,
			Side:          string(req.Side),
			PositionSide:  string(req.PositionSide),
			OrderType:     string(req.OrderType),
			OrderID:       resp.Order.OrderID,
			Quantity:      resp.Order.OrigQuantity,
			Price:         resp.Order.AvgPrice,
			QuoteQuantity: req.QuoteQuantity,
			Leverage:      req.Leverage,
			Status:        "OPEN",
		}
		if resp.TakeProfit != nil {
			record.TakeProfitPrice = resp.TakeProfit.TriggerPrice
			record.TakeProfitAlgoID = resp.TakeProfit.AlgoID
		}
		if resp.StopLoss != nil {
			record.StopLossPrice = resp.StopLoss.TriggerPrice
			record.StopLossAlgoID = resp.StopLoss.AlgoID
		}
		if err := SaveTradeRecord(record); err != nil {
			log.Printf("[DB] Failed to save trade record: %v", err)
		}
	}()

	ctx.JSON(http.StatusOK, utils.H{"data": resp})
}

// HandleGetTrades GET /api/trades?symbol=ETHUSDT&limit=50
func HandleGetTrades(c context.Context, ctx *app.RequestContext) {
	symbol := ctx.DefaultQuery("symbol", "")
	limitStr := ctx.DefaultQuery("limit", "50")
	limit, _ := strconv.Atoi(limitStr)
	if limit <= 0 {
		limit = 50
	}
	records, err := GetTradeRecords(symbol, limit)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, utils.H{"error": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, utils.H{"data": records})
}

// HandleGetOrders GET /api/orders?symbol=BTCUSDT
func HandleGetOrders(c context.Context, ctx *app.RequestContext) {
	symbol := ctx.DefaultQuery("symbol", "")
	orders, err := GetOrderListViaWs(c, symbol)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, utils.H{"error": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, utils.H{"data": orders})
}

// HandleCancelOrder DELETE /api/order?symbol=BTCUSDT&orderId=123
func HandleCancelOrder(c context.Context, ctx *app.RequestContext) {
	symbol := ctx.Query("symbol")
	orderIDStr := ctx.Query("orderId")
	if symbol == "" || orderIDStr == "" {
		ctx.JSON(http.StatusBadRequest, utils.H{"error": "symbol and orderId are required"})
		return
	}
	orderID, err := strconv.ParseInt(orderIDStr, 10, 64)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, utils.H{"error": "invalid orderId"})
		return
	}
	resp, err := CancelOrderViaWs(c, symbol, orderID)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, utils.H{"error": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, utils.H{"data": resp})
}

// HandleChangeLeverage POST /api/leverage
func HandleChangeLeverage(c context.Context, ctx *app.RequestContext) {
	var req struct {
		Symbol   string `json:"symbol"`
		Leverage int    `json:"leverage"`
	}
	if err := ctx.BindAndValidate(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, utils.H{"error": err.Error()})
		return
	}
	resp, err := ChangeLeverage(c, req.Symbol, req.Leverage)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, utils.H{"error": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, utils.H{"data": resp})
}

// HandleReducePosition POST /api/reduce
// Body: {"symbol": "BTCUSDT", "positionSide": "LONG", "quantity": "0.001"}
// 或:   {"symbol": "BTCUSDT", "positionSide": "LONG", "percent": 50}
func HandleReducePosition(c context.Context, ctx *app.RequestContext) {
	var req ReducePositionReq
	if err := ctx.BindAndValidate(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, utils.H{"error": err.Error()})
		return
	}
	resp, err := ReducePositionViaWs(c, req)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, utils.H{"error": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, utils.H{"data": resp})
}

// HandleClosePosition POST /api/close
// Body: {"symbol": "BTCUSDT", "positionSide": "LONG"}
func HandleClosePosition(c context.Context, ctx *app.RequestContext) {
	var req ClosePositionReq
	if err := ctx.BindAndValidate(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, utils.H{"error": err.Error()})
		return
	}
	resp, err := ClosePositionViaWs(c, req)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, utils.H{"error": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, utils.H{"data": resp})
}

// HandleStartAutoScale POST /api/autoscale/start
// 开启浮盈加仓监控
func HandleStartAutoScale(c context.Context, ctx *app.RequestContext) {
	var config AutoScaleConfig
	if err := ctx.BindAndValidate(&config); err != nil {
		ctx.JSON(http.StatusBadRequest, utils.H{"error": err.Error()})
		return
	}
	if err := StartAutoScale(config); err != nil {
		ctx.JSON(http.StatusBadRequest, utils.H{"error": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, utils.H{"message": "auto scale started", "symbol": config.Symbol})
}

// HandleStopAutoScale POST /api/autoscale/stop
// 关闭浮盈加仓监控
func HandleStopAutoScale(c context.Context, ctx *app.RequestContext) {
	var req struct {
		Symbol string `json:"symbol"`
	}
	if err := ctx.BindAndValidate(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, utils.H{"error": err.Error()})
		return
	}
	if err := StopAutoScale(req.Symbol); err != nil {
		ctx.JSON(http.StatusBadRequest, utils.H{"error": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, utils.H{"message": "auto scale stopped", "symbol": req.Symbol})
}

// HandleAutoScaleStatus GET /api/autoscale/status?symbol=ETHUSDT
// 查询浮盈加仓状态
func HandleAutoScaleStatus(c context.Context, ctx *app.RequestContext) {
	symbol := ctx.Query("symbol")
	if symbol == "" {
		ctx.JSON(http.StatusBadRequest, utils.H{"error": "symbol is required"})
		return
	}
	status := GetAutoScaleStatus(symbol)
	if status == nil {
		ctx.JSON(http.StatusOK, utils.H{"data": nil, "message": "no auto scale task for " + symbol})
		return
	}
	ctx.JSON(http.StatusOK, utils.H{"data": status})
}

// HandleGetRiskStatus GET /api/risk/status
func HandleGetRiskStatus(c context.Context, ctx *app.RequestContext) {
	ctx.JSON(http.StatusOK, utils.H{"data": GetRiskStatus()})
}

// HandleUnlockRisk POST /api/risk/unlock
func HandleUnlockRisk(c context.Context, ctx *app.RequestContext) {
	UnlockRisk()
	ctx.JSON(http.StatusOK, utils.H{"message": "risk control unlocked"})
}

// ========== 网格交易 ==========

// HandleStartGrid POST /api/grid/start
func HandleStartGrid(c context.Context, ctx *app.RequestContext) {
	var config GridConfig
	if err := ctx.BindAndValidate(&config); err != nil {
		ctx.JSON(http.StatusBadRequest, utils.H{"error": err.Error()})
		return
	}
	if err := StartGrid(config); err != nil {
		ctx.JSON(http.StatusBadRequest, utils.H{"error": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, utils.H{"message": "grid started", "symbol": config.Symbol})
}

// HandleStopGrid POST /api/grid/stop
func HandleStopGrid(c context.Context, ctx *app.RequestContext) {
	var req struct {
		Symbol string `json:"symbol"`
	}
	if err := ctx.BindAndValidate(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, utils.H{"error": err.Error()})
		return
	}
	if err := StopGrid(req.Symbol); err != nil {
		ctx.JSON(http.StatusBadRequest, utils.H{"error": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, utils.H{"message": "grid stopped", "symbol": req.Symbol})
}

// HandleGridStatus GET /api/grid/status?symbol=ETHUSDT
func HandleGridStatus(c context.Context, ctx *app.RequestContext) {
	symbol := ctx.Query("symbol")
	if symbol == "" {
		ctx.JSON(http.StatusBadRequest, utils.H{"error": "symbol is required"})
		return
	}
	status := GetGridStatus(symbol)
	if status == nil {
		ctx.JSON(http.StatusOK, utils.H{"data": nil, "message": "no grid task for " + symbol})
		return
	}
	ctx.JSON(http.StatusOK, utils.H{"data": status})
}

// ========== DCA 定投 ==========

// HandleStartDCA POST /api/dca/start
func HandleStartDCA(c context.Context, ctx *app.RequestContext) {
	var config DCAConfig
	if err := ctx.BindAndValidate(&config); err != nil {
		ctx.JSON(http.StatusBadRequest, utils.H{"error": err.Error()})
		return
	}
	if err := StartDCA(config); err != nil {
		ctx.JSON(http.StatusBadRequest, utils.H{"error": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, utils.H{"message": "DCA started", "symbol": config.Symbol})
}

// HandleStopDCA POST /api/dca/stop
func HandleStopDCA(c context.Context, ctx *app.RequestContext) {
	var req struct {
		Symbol string `json:"symbol"`
	}
	if err := ctx.BindAndValidate(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, utils.H{"error": err.Error()})
		return
	}
	if err := StopDCA(req.Symbol); err != nil {
		ctx.JSON(http.StatusBadRequest, utils.H{"error": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, utils.H{"message": "DCA stopped", "symbol": req.Symbol})
}

// HandleDCAStatus GET /api/dca/status?symbol=ETHUSDT
func HandleDCAStatus(c context.Context, ctx *app.RequestContext) {
	symbol := ctx.Query("symbol")
	if symbol == "" {
		ctx.JSON(http.StatusBadRequest, utils.H{"error": "symbol is required"})
		return
	}
	status := GetDCAStatus(symbol)
	if status == nil {
		ctx.JSON(http.StatusOK, utils.H{"data": nil, "message": "no DCA task for " + symbol})
		return
	}
	ctx.JSON(http.StatusOK, utils.H{"data": status})
}

// ========== 信号策略 (RSI + 成交量) ==========

// HandleStartSignal POST /api/signal/start
func HandleStartSignal(c context.Context, ctx *app.RequestContext) {
	var config SignalConfig
	if err := ctx.BindAndValidate(&config); err != nil {
		ctx.JSON(http.StatusBadRequest, utils.H{"error": err.Error()})
		return
	}
	if err := StartSignalStrategy(config); err != nil {
		ctx.JSON(http.StatusBadRequest, utils.H{"error": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, utils.H{"message": "signal strategy started", "symbol": config.Symbol})
}

// HandleStopSignal POST /api/signal/stop
func HandleStopSignal(c context.Context, ctx *app.RequestContext) {
	var req struct {
		Symbol string `json:"symbol"`
	}
	if err := ctx.BindAndValidate(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, utils.H{"error": err.Error()})
		return
	}
	if err := StopSignalStrategy(req.Symbol); err != nil {
		ctx.JSON(http.StatusBadRequest, utils.H{"error": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, utils.H{"message": "signal strategy stopped", "symbol": req.Symbol})
}

// HandleSignalStatus GET /api/signal/status?symbol=ETHUSDT
func HandleSignalStatus(c context.Context, ctx *app.RequestContext) {
	symbol := ctx.Query("symbol")
	if symbol == "" {
		ctx.JSON(http.StatusBadRequest, utils.H{"error": "symbol is required"})
		return
	}
	status := GetSignalStatus(symbol)
	if status == nil {
		ctx.JSON(http.StatusOK, utils.H{"data": nil, "message": "no signal strategy for " + symbol})
		return
	}
	ctx.JSON(http.StatusOK, utils.H{"data": status})
}

// ========== K线形态（十字星）策略 ==========

// HandleStartDoji POST /api/doji/start
func HandleStartDoji(c context.Context, ctx *app.RequestContext) {
	var config DojiConfig
	if err := ctx.BindAndValidate(&config); err != nil {
		ctx.JSON(http.StatusBadRequest, utils.H{"error": err.Error()})
		return
	}
	if err := StartDojiStrategy(config); err != nil {
		ctx.JSON(http.StatusBadRequest, utils.H{"error": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, utils.H{"message": "doji strategy started", "symbol": config.Symbol})
}

// HandleStopDoji POST /api/doji/stop
func HandleStopDoji(c context.Context, ctx *app.RequestContext) {
	var req struct {
		Symbol string `json:"symbol"`
	}
	if err := ctx.BindAndValidate(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, utils.H{"error": err.Error()})
		return
	}
	if err := StopDojiStrategy(req.Symbol); err != nil {
		ctx.JSON(http.StatusBadRequest, utils.H{"error": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, utils.H{"message": "doji strategy stopped", "symbol": req.Symbol})
}

// HandleDojiStatus GET /api/doji/status?symbol=ETHUSDT
func HandleDojiStatus(c context.Context, ctx *app.RequestContext) {
	symbol := ctx.Query("symbol")
	if symbol == "" {
		ctx.JSON(http.StatusBadRequest, utils.H{"error": "symbol is required"})
		return
	}
	status := GetDojiStatus(symbol)
	if status == nil {
		ctx.JSON(http.StatusOK, utils.H{"data": nil, "message": "no doji strategy for " + symbol})
		return
	}
	ctx.JSON(http.StatusOK, utils.H{"data": status})
}
