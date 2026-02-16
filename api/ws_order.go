package api

import (
	"context"
	"fmt"
	"log"
	"math"
	"strconv"

	ws "tools/websocket"

	"github.com/adshao/go-binance/v2/futures"
)

// PlaceOrderViaWs 通过 WebSocket 下单，失败时自动降级到 REST API
// 如果设置了 stopLossPrice + riskReward，主单成交后自动挂止盈止损单
// 返回 *PlaceOrderResult 包含主单和可选的止盈止损单
func PlaceOrderViaWs(ctx context.Context, req PlaceOrderReq) (*PlaceOrderResult, error) {
	// 验证必填字段
	if req.QuoteQuantity == "" {
		return nil, fmt.Errorf("quoteQuantity is required")
	}
	if req.Leverage == 0 {
		return nil, fmt.Errorf("leverage is required")
	}
	if req.Side == "" {
		return nil, fmt.Errorf("side is required")
	}
	if req.OrderType == "" {
		return nil, fmt.Errorf("ordertype is required")
	}

	// 验证止盈止损参数
	// 支持两种模式：stopLossPrice+riskReward 或 stopLossAmount+riskReward
	hasStopPrice := req.StopLossPrice != ""
	hasStopAmount := req.StopLossAmount > 0
	hasRatio := req.RiskReward > 0
	needTPSL := (hasStopPrice || hasStopAmount) && hasRatio

	if hasStopPrice && hasStopAmount {
		return nil, fmt.Errorf("stopLossPrice and stopLossAmount cannot be set at the same time, use one")
	}
	if (hasStopPrice || hasStopAmount) && !hasRatio {
		return nil, fmt.Errorf("riskReward is required when stopLossPrice or stopLossAmount is set")
	}
	if hasRatio && !hasStopPrice && !hasStopAmount {
		return nil, fmt.Errorf("stopLossPrice or stopLossAmount is required when riskReward is set")
	}

	// 如果未指定 positionSide，默认使用 BOTH（单向持仓模式）
	if req.PositionSide == "" {
		req.PositionSide = futures.PositionSideTypeBoth
	}

	// 先调整该交易对的杠杆倍数
	_, err := ChangeLeverage(ctx, req.Symbol, req.Leverage)
	if err != nil {
		return nil, fmt.Errorf("change leverage: %w", err)
	}
	log.Printf("[WsOrder] Leverage set to %dx for %s", req.Leverage, req.Symbol)

	// 根据 USDT 金额和杠杆计算代币数量
	quantity, err := calculateQuantityFromUSDT(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("calculate quantity: %w", err)
	}

	// 尝试 WebSocket 下单
	var mainOrder *futures.CreateOrderResponse
	wsClient := GetWsClient()
	if wsClient != nil {
		result, err := wsPlaceOrder(wsClient, req, quantity)
		if err == nil {
			log.Printf("[WsOrder] PlaceOrder via WebSocket success: orderId=%d", result.OrderID)
			mainOrder = result
		} else {
			log.Printf("[WsOrder] PlaceOrder via WebSocket failed: %v, falling back to REST API", err)
			go ReconnectWsClient()
		}
	} else {
		log.Println("[WsOrder] WebSocket client not available, using REST API")
	}

	// REST API 降级
	if mainOrder == nil {
		mainOrder, err = restPlaceOrder(ctx, req, quantity)
		if err != nil {
			return nil, err
		}
	}

	result := &PlaceOrderResult{Order: mainOrder}

	// 主单成交后挂止盈止损
	if needTPSL {
		// 确定入场价：市价单用 avgPrice，限价单用 price
		entryPriceStr := mainOrder.AvgPrice
		if entryPriceStr == "" || entryPriceStr == "0" {
			entryPriceStr = mainOrder.Price
		}
		entryPrice, parseErr := strconv.ParseFloat(entryPriceStr, 64)
		if parseErr != nil || entryPrice == 0 {
			// 市价单可能 avgPrice 尚未返回，回退到当前市场价
			entryPrice, parseErr = getCurrentPrice(ctx, req.Symbol, "")
			if parseErr != nil {
				log.Printf("[TPSL] Warning: cannot determine entry price, skip TP/SL: %v", parseErr)
				return result, nil
			}
		}

		tp, sl, tpslErr := PlaceTPSLOrders(ctx, req, entryPrice, quantity)
		if tpslErr != nil {
			log.Printf("[TPSL] Warning: failed to place TP/SL orders: %v", tpslErr)
			// 主单已成功，止盈止损失败不算整体失败，返回主单结果 + 错误信息
			return result, nil
		}
		result.TakeProfit = tp
		result.StopLoss = sl
	}

	return result, nil
}

// CancelOrderViaWs 通过 WebSocket 撤单，失败时降级到 REST API
func CancelOrderViaWs(ctx context.Context, symbol string, orderID int64) (*futures.CancelOrderResponse, error) {
	// 尝试 WebSocket 撤单
	wsClient := GetWsClient()
	if wsClient != nil {
		result, err := wsClient.CancelOrder(ws.CancelOrderParams{
			Symbol:  symbol,
			OrderId: orderID,
		})
		if err == nil {
			log.Printf("[WsOrder] CancelOrder via WebSocket success: orderId=%d", result.OrderId)
			return convertWsCancelResult(result), nil
		}
		log.Printf("[WsOrder] CancelOrder via WebSocket failed: %v, falling back to REST API", err)
		go ReconnectWsClient()
	} else {
		log.Println("[WsOrder] WebSocket client not available, using REST API for cancel")
	}

	// REST API 降级
	return Client.NewCancelOrderService().
		Symbol(symbol).
		OrderID(orderID).
		Do(ctx)
}

// GetOrderListViaWs 查询订单 - 注意：WS API 只能查单个订单状态，批量查询仍使用 REST API
// WebSocket API 没有 openOrders 接口，所以查询订单列表直接使用 REST API
func GetOrderListViaWs(ctx context.Context, symbol string) ([]*futures.Order, error) {
	// WS API 不支持批量查询未成交订单，直接使用 REST API
	service := Client.NewListOpenOrdersService()
	if symbol != "" {
		service.Symbol(symbol)
	}
	return service.Do(ctx)
}

// QuerySingleOrderViaWs 通过 WebSocket 查询单个订单状态，失败时降级到 REST API
func QuerySingleOrderViaWs(ctx context.Context, symbol string, orderID int64) (*ws.OrderResult, error) {
	wsClient := GetWsClient()
	if wsClient != nil {
		result, err := wsClient.QueryOrder(ws.QueryOrderParams{
			Symbol:  symbol,
			OrderId: orderID,
		})
		if err == nil {
			log.Printf("[WsOrder] QueryOrder via WebSocket success: orderId=%d status=%s", result.OrderId, result.Status)
			return result, nil
		}
		log.Printf("[WsOrder] QueryOrder via WebSocket failed: %v, falling back to REST API", err)
		go ReconnectWsClient()
	}

	// REST API 降级：通过 REST 查询后转换为 ws.OrderResult
	order, err := Client.NewGetOrderService().
		Symbol(symbol).
		OrderID(orderID).
		Do(ctx)
	if err != nil {
		return nil, err
	}
	return &ws.OrderResult{
		OrderId:       order.OrderID,
		Symbol:        order.Symbol,
		Status:        string(order.Status),
		ClientOrderId: order.ClientOrderID,
		Price:         order.Price,
		AvgPrice:      order.AvgPrice,
		OrigQty:       order.OrigQuantity,
		ExecutedQty:   order.ExecutedQuantity,
		Type:          string(order.Type),
		Side:          string(order.Side),
		PositionSide:  string(order.PositionSide),
		TimeInForce:   string(order.TimeInForce),
		StopPrice:     order.StopPrice,
		UpdateTime:    order.UpdateTime,
	}, nil
}

// --- 内部函数 ---

// wsPlaceOrder 通过 WebSocket 下单
func wsPlaceOrder(wsClient *ws.WsClient, req PlaceOrderReq, quantity string) (*futures.CreateOrderResponse, error) {
	params := ws.PlaceOrderParams{
		Symbol:   req.Symbol,
		Side:     string(req.Side),
		Type:     string(req.OrderType),
		Quantity: quantity,
	}

	if req.Price != "" {
		params.Price = req.Price
	}
	if req.StopPrice != "" {
		params.StopPrice = req.StopPrice
	}
	if req.PositionSide != "" {
		params.PositionSide = string(req.PositionSide)
	}
	// timeInForce 只在限价单时设置，市价单不需要
	if req.OrderType == futures.OrderTypeLimit {
		if req.TimeInForce != "" {
			params.TimeInForce = string(req.TimeInForce)
		} else {
			params.TimeInForce = "GTC"
		}
	}
	if req.ReduceOnly {
		params.ReduceOnly = "true"
	}

	result, err := wsClient.PlaceOrder(params)
	if err != nil {
		return nil, err
	}

	return convertWsOrderResult(result), nil
}

// restPlaceOrder 通过 REST API 下单（降级路径）
func restPlaceOrder(ctx context.Context, req PlaceOrderReq, quantity string) (*futures.CreateOrderResponse, error) {
	service := Client.NewCreateOrderService().
		Symbol(req.Symbol).
		Side(req.Side).
		Type(req.OrderType).
		Quantity(quantity)

	if req.Price != "" {
		service.Price(req.Price)
	}
	if req.StopPrice != "" {
		service.StopPrice(req.StopPrice)
	}
	if req.PositionSide != "" {
		service.PositionSide(req.PositionSide)
	}
	// timeInForce 只在限价单时设置，市价单不需要
	if req.OrderType == futures.OrderTypeLimit {
		if req.TimeInForce != "" {
			service.TimeInForce(req.TimeInForce)
		} else {
			service.TimeInForce(futures.TimeInForceTypeGTC)
		}
	}
	if req.ReduceOnly {
		service.ReduceOnly(req.ReduceOnly)
	}

	return service.Do(ctx)
}

// convertWsOrderResult 将 WebSocket 订单结果转为 REST API 兼容的响应结构
func convertWsOrderResult(r *ws.OrderResult) *futures.CreateOrderResponse {
	return &futures.CreateOrderResponse{
		OrderID:          r.OrderId,
		Symbol:           r.Symbol,
		Status:           futures.OrderStatusType(r.Status),
		ClientOrderID:    r.ClientOrderId,
		Price:            r.Price,
		AvgPrice:         r.AvgPrice,
		OrigQuantity:     r.OrigQty,
		ExecutedQuantity: r.ExecutedQty,
		Type:             futures.OrderType(r.Type),
		Side:             futures.SideType(r.Side),
		PositionSide:     futures.PositionSideType(r.PositionSide),
		TimeInForce:      futures.TimeInForceType(r.TimeInForce),
		StopPrice:        r.StopPrice,
		UpdateTime:       r.UpdateTime,
	}
}

// convertWsCancelResult 将 WebSocket 撤单结果转为 REST API 兼容的响应结构
func convertWsCancelResult(r *ws.OrderResult) *futures.CancelOrderResponse {
	orderID := r.OrderId
	return &futures.CancelOrderResponse{
		OrderID:          orderID,
		Symbol:           r.Symbol,
		Status:           futures.OrderStatusType(r.Status),
		ClientOrderID:    r.ClientOrderId,
		Price:            r.Price,
		OrigQuantity:     r.OrigQty,
		ExecutedQuantity: r.ExecutedQty,
		Type:             futures.OrderType(r.Type),
		Side:             futures.SideType(r.Side),
		PositionSide:     futures.PositionSideType(r.PositionSide),
		TimeInForce:      futures.TimeInForceType(r.TimeInForce),
		StopPrice:        r.StopPrice,
	}
}

// GetPositionsViaWs 通过 WebSocket 查询仓位，失败时降级到 REST API
func GetPositionsViaWs(ctx context.Context) ([]*futures.PositionRisk, error) {
	// wsClient := GetWsClient()
	// if wsClient != nil {
	// 	results, err := wsClient.GetPosition(ws.PositionParams{})
	// 	log.Printf("%#v", results)
	// 	if err == nil {
	// 		log.Printf("[WsOrder] GetPositions via WebSocket success: %d positions", len(results))
	// 		return convertWsPositionResults(results), nil
	// 	}
	// 	log.Printf("[WsOrder] GetPositions via WebSocket failed: %v, falling back to REST API", err)
	// 	go ReconnectWsClient()
	// }

	// REST API 降级
	return GetPositions(ctx)
}

// ReducePositionViaWs 通过 WebSocket 减仓，失败时降级到 REST API
func ReducePositionViaWs(ctx context.Context, req ReducePositionReq) (*futures.CreateOrderResponse, error) {
	if req.Symbol == "" {
		return nil, fmt.Errorf("symbol is required")
	}
	if req.PositionSide == "" {
		req.PositionSide = futures.PositionSideTypeBoth
	}

	// 查询当前仓位
	position, err := findPosition(ctx, req.Symbol, req.PositionSide)
	if err != nil {
		return nil, err
	}

	posAmt, _ := strconv.ParseFloat(position.PositionAmt, 64)
	if posAmt == 0 {
		return nil, fmt.Errorf("no open position for %s", req.Symbol)
	}

	// 计算减仓数量
	var reduceQty float64
	if req.Quantity != "" {
		reduceQty, err = strconv.ParseFloat(req.Quantity, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid quantity: %w", err)
		}
	} else if req.Percent > 0 {
		reduceQty = math.Abs(posAmt) * req.Percent / 100
	} else {
		return nil, fmt.Errorf("quantity or percent is required")
	}

	precision, stepSize, err := getSymbolPrecision(ctx, req.Symbol)
	if err != nil {
		return nil, fmt.Errorf("get symbol precision: %w", err)
	}
	reduceQty = roundToStepSize(reduceQty, stepSize)
	quantity := formatQuantity(reduceQty, precision)

	side := futures.SideTypeSell
	if posAmt < 0 {
		side = futures.SideTypeBuy
	}

	return reduceOrderViaWs(ctx, req.Symbol, side, req.PositionSide, quantity)
}

// ClosePositionViaWs 通过 WebSocket 全部平仓，失败时降级到 REST API
func ClosePositionViaWs(ctx context.Context, req ClosePositionReq) (*futures.CreateOrderResponse, error) {
	if req.Symbol == "" {
		return nil, fmt.Errorf("symbol is required")
	}
	if req.PositionSide == "" {
		req.PositionSide = futures.PositionSideTypeBoth
	}

	position, err := findPosition(ctx, req.Symbol, req.PositionSide)
	if err != nil {
		return nil, err
	}

	posAmt, _ := strconv.ParseFloat(position.PositionAmt, 64)
	if posAmt == 0 {
		return nil, fmt.Errorf("no open position for %s", req.Symbol)
	}

	absAmt := math.Abs(posAmt)
	precision, stepSize, err := getSymbolPrecision(ctx, req.Symbol)
	if err != nil {
		return nil, fmt.Errorf("get symbol precision: %w", err)
	}
	absAmt = roundToStepSize(absAmt, stepSize)
	quantity := formatQuantity(absAmt, precision)

	side := futures.SideTypeSell
	if posAmt < 0 {
		side = futures.SideTypeBuy
	}

	return reduceOrderViaWs(ctx, req.Symbol, side, req.PositionSide, quantity)
}

// reduceOrderViaWs 通过 WebSocket 发送减仓/平仓市价单，失败降级到 REST API
func reduceOrderViaWs(ctx context.Context, symbol string, side futures.SideType, positionSide futures.PositionSideType, quantity string) (*futures.CreateOrderResponse, error) {
	wsClient := GetWsClient()
	if wsClient != nil {
		params := ws.PlaceOrderParams{
			Symbol:   symbol,
			Side:     string(side),
			Type:     "MARKET",
			Quantity: quantity,
			// ReduceOnly: "true",
		}
		if positionSide != "" {
			params.PositionSide = string(positionSide)
		}

		result, err := wsClient.PlaceOrder(params)
		if err == nil {
			log.Printf("[WsOrder] ReduceOrder via WebSocket success: orderId=%d", result.OrderId)
			return convertWsOrderResult(result), nil
		}
		log.Printf("[WsOrder] ReduceOrder via WebSocket failed: %v, falling back to REST API", err)
		go ReconnectWsClient()
	} else {
		log.Println("[WsOrder] WebSocket client not available, using REST API for reduce/close")
	}

	// REST API 降级
	return createReduceOrder(ctx, symbol, side, positionSide, quantity)
}

// convertWsPositionResults 将 WebSocket 仓位结果转为 REST API 兼容的响应结构
func convertWsPositionResults(results []ws.PositionResult) []*futures.PositionRisk {
	var positions []*futures.PositionRisk
	for _, r := range results {

		amt, _ := strconv.ParseFloat(r.PositionAmt, 64)
		if amt == 0 {
			continue // 跳过空仓位
		}
		positions = append(positions, &futures.PositionRisk{
			Symbol:           r.Symbol,
			PositionSide:     r.PositionSide,
			PositionAmt:      r.PositionAmt,
			EntryPrice:       r.EntryPrice,
			BreakEvenPrice:   r.BreakEvenPrice,
			MarkPrice:        r.MarkPrice,
			UnRealizedProfit: r.UnRealizedProfit,
			LiquidationPrice: r.LiquidationPrice,
			Leverage:         r.Leverage,
		})
	}
	return positions
}
