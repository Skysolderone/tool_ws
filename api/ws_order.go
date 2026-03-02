package api

import (
	"context"
	"fmt"
	"log"
	"math"
	"strconv"
	"time"

	ws "tools/websocket"

	"github.com/adshao/go-binance/v2/futures"
)

// PlaceOrderViaWs 通过 WebSocket 下单，失败时自动降级到 REST API
// 如果设置了 stopLossPrice + riskReward，主单成交后自动挂止盈止损单
// 返回 *PlaceOrderResult 包含主单和可选的止盈止损单
func PlaceOrderViaWs(ctx context.Context, req PlaceOrderReq) (*PlaceOrderResult, error) {
	orderStartTime := time.Now()

	recordFailure := func(action string, opErr error, relatedOrderID int64) {
		SaveFailedOperation(action, req.Source, req.Symbol, req, relatedOrderID, opErr)
		RecordOrderMetric(false, time.Since(orderStartTime).Milliseconds())
	}
	fail := func(action string, opErr error) (*PlaceOrderResult, error) {
		recordFailure(action, opErr, 0)
		return nil, opErr
	}

	// 验证必填字段
	if req.QuoteQuantity == "" {
		return fail("PLACE_ORDER", fmt.Errorf("quoteQuantity is required"))
	}
	if req.Leverage == 0 {
		return fail("PLACE_ORDER", fmt.Errorf("leverage is required"))
	}
	if req.Side == "" {
		return fail("PLACE_ORDER", fmt.Errorf("side is required"))
	}
	if req.OrderType == "" {
		return fail("PLACE_ORDER", fmt.Errorf("ordertype is required"))
	}

	// 验证止盈止损参数
	// 支持两种模式：stopLossPrice+riskReward 或 stopLossAmount+riskReward
	hasStopPrice := req.StopLossPrice != ""
	hasStopAmount := req.StopLossAmount > 0
	hasRatio := req.RiskReward > 0
	needTPSL := (hasStopPrice || hasStopAmount) && hasRatio

	if hasStopPrice && hasStopAmount {
		return fail("PLACE_ORDER", fmt.Errorf("stopLossPrice and stopLossAmount cannot be set at the same time, use one"))
	}
	if (hasStopPrice || hasStopAmount) && !hasRatio {
		return fail("PLACE_ORDER", fmt.Errorf("riskReward is required when stopLossPrice or stopLossAmount is set"))
	}
	if hasRatio && !hasStopPrice && !hasStopAmount {
		return fail("PLACE_ORDER", fmt.Errorf("stopLossPrice or stopLossAmount is required when riskReward is set"))
	}

	// 如果未指定 positionSide，默认使用 BOTH（单向持仓模式）
	if req.PositionSide == "" {
		req.PositionSide = futures.PositionSideTypeBoth
	}

	// DryRun 模拟交易模式：不实际下单，用虚拟资金撮合
	if IsDryRun() {
		qtyStr, qtyErr := calculateQuantityFromUSDT(ctx, req)
		if qtyErr != nil {
			return fail("PLACE_ORDER", fmt.Errorf("paper order calculate quantity: %w", qtyErr))
		}
		qtyFloat, _ := strconv.ParseFloat(qtyStr, 64)
		// 持仓方向映射：BUY→LONG，SELL→SHORT
		side := "LONG"
		if req.Side == futures.SideTypeSell {
			side = "SHORT"
		}
		if req.PositionSide == futures.PositionSideTypeLong {
			side = "LONG"
		} else if req.PositionSide == futures.PositionSideTypeShort {
			side = "SHORT"
		}
		source := req.Source
		if source == "" {
			source = "manual"
		}
		trade, paperErr := PaperPlaceOrder(req.Symbol, side, qtyFloat, 0, req.Leverage, source)
		if paperErr != nil {
			return fail("PLACE_ORDER", paperErr)
		}
		fakeOrderID := trade.ID
		log.Printf("[PaperTrading] PlaceOrder simulated: symbol=%s side=%s qty=%s tradeID=%d",
			req.Symbol, side, qtyStr, fakeOrderID)
		// 构造一个模拟的响应结构，price 使用模拟成交价
		priceStr := fmt.Sprintf("%.8f", trade.Price)
		fakeOrder := &futures.CreateOrderResponse{
			OrderID:          fakeOrderID,
			Symbol:           req.Symbol,
			Status:           futures.OrderStatusTypeFilled,
			OrigQuantity:     qtyStr,
			ExecutedQuantity: qtyStr,
			AvgPrice:         priceStr,
			Price:            priceStr,
			Side:             req.Side,
			PositionSide:     req.PositionSide,
			Type:             req.OrderType,
		}
		return &PlaceOrderResult{Order: fakeOrder}, nil
	}

	// 先调整该交易对的杠杆倍数
	_, err := ChangeLeverage(ctx, req.Symbol, req.Leverage)
	if err != nil {
		return fail("PLACE_ORDER", fmt.Errorf("change leverage: %w", err))
	}
	log.Printf("[WsOrder] Leverage set to %dx for %s", req.Leverage, req.Symbol)

	// 根据 USDT 金额和杠杆计算代币数量
	quantity, err := calculateQuantityFromUSDT(ctx, req)
	if err != nil {
		return fail("PLACE_ORDER", fmt.Errorf("calculate quantity: %w", err))
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
			return fail("PLACE_ORDER", err)
		}
	}

	result := &PlaceOrderResult{Order: mainOrder}

	// 异步记录执行质量（滑点 + 延迟 + 策略归因）
	go func() {
		avgPriceStr := mainOrder.AvgPrice
		if avgPriceStr == "" || avgPriceStr == "0" {
			return
		}
		executedPrice, parseErr := strconv.ParseFloat(avgPriceStr, 64)
		if parseErr != nil || executedPrice == 0 {
			return
		}
		// 下单时的市场价（标记价格缓存）
		arrivalPrice, priceErr := GetPriceCache().GetPrice(req.Symbol)
		if priceErr != nil || arrivalPrice == 0 {
			return
		}
		qty, _ := strconv.ParseFloat(mainOrder.OrigQuantity, 64)
		orderIDStr := strconv.FormatInt(mainOrder.OrderID, 10)
		source := req.Source
		if source == "" {
			source = "manual"
		}
		latencyMs := time.Since(orderStartTime).Milliseconds()
		RecordExecutionQuality(req.Symbol, orderIDStr, string(req.Side), source, arrivalPrice, executedPrice, qty, latencyMs)
		RecordOrderMetric(true, latencyMs)
	}()

	// 主单成交后注册本地止盈止损监控
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
				recordFailure("PLACE_TPSL", fmt.Errorf("cannot determine entry price: %w", parseErr), mainOrder.OrderID)
				return result, nil
			}
		}

		groupID, tpslErr := RegisterLocalTPSLFromOrder(req, entryPrice, quantity, mainOrder.OrderID)
		if tpslErr != nil {
			log.Printf("[TPSL] Warning: failed to register local TP/SL: %v", tpslErr)
			recordFailure("PLACE_TPSL", tpslErr, mainOrder.OrderID)
			return result, nil
		}
		result.LocalTPSLGroupID = groupID
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

// reduceOrderViaWs 通过 WebSocket 发送减仓/平仓限价单（用当前价格），失败降级到 REST API
func reduceOrderViaWs(ctx context.Context, symbol string, side futures.SideType, positionSide futures.PositionSideType, quantity string) (*futures.CreateOrderResponse, error) {
	// DryRun 模拟交易模式：不实际下单，模拟撮合
	if IsDryRun() {
		// 持仓方向：平多用 SELL 对应 LONG，平空用 BUY 对应 SHORT
		posSide := "LONG"
		if side == futures.SideTypeBuy {
			posSide = "SHORT"
		}
		if positionSide == futures.PositionSideTypeLong {
			posSide = "LONG"
		} else if positionSide == futures.PositionSideTypeShort {
			posSide = "SHORT"
		}
		qtyFloat, _ := strconv.ParseFloat(quantity, 64)
		trade, paperErr := PaperReducePosition(symbol, posSide, qtyFloat, 0, "reduce/close")
		if paperErr != nil {
			log.Printf("[PaperTrading] ReducePosition failed: %v", paperErr)
			return nil, paperErr
		}
		priceStr := fmt.Sprintf("%.8f", trade.Price)
		fakeOrder := &futures.CreateOrderResponse{
			OrderID:          trade.ID,
			Symbol:           symbol,
			Status:           futures.OrderStatusTypeFilled,
			OrigQuantity:     quantity,
			ExecutedQuantity: quantity,
			AvgPrice:         priceStr,
			Price:            priceStr,
			Side:             side,
			PositionSide:     positionSide,
			Type:             futures.OrderTypeMarket,
		}
		log.Printf("[PaperTrading] ReduceOrder simulated: symbol=%s side=%s qty=%s pnl=%.4f",
			symbol, posSide, quantity, trade.PnL)
		return fakeOrder, nil
	}

	// 获取当前价格，用于限价单
	price, priceErr := getCurrentPrice(ctx, symbol, "")
	if priceErr != nil {
		return nil, fmt.Errorf("get current price for reduce order: %w", priceErr)
	}

	// 获取价格精度
	pricePrecision, ppErr := getSymbolPricePrecision(ctx, symbol)
	if ppErr != nil {
		return nil, fmt.Errorf("get price precision: %w", ppErr)
	}
	priceStr := formatPrice(price, pricePrecision)

	wsClient := GetWsClient()
	if wsClient != nil {
		params := ws.PlaceOrderParams{
			Symbol:      symbol,
			Side:        string(side),
			Type:        "LIMIT",
			Quantity:    quantity,
			Price:       priceStr,
			TimeInForce: "GTC",
		}
		if positionSide != "" {
			params.PositionSide = string(positionSide)
		}

		result, err := wsClient.PlaceOrder(params)
		if err == nil {
			log.Printf("[WsOrder] ReduceOrder via WebSocket success: orderId=%d, price=%s", result.OrderId, priceStr)
			resp := convertWsOrderResult(result)
			ensureOrderFilled(ctx, symbol, resp.OrderID, side, positionSide, quantity, true, 3)
			return resp, nil
		}
		log.Printf("[WsOrder] ReduceOrder via WebSocket failed: %v, falling back to REST API", err)
		go ReconnectWsClient()
	} else {
		log.Println("[WsOrder] WebSocket client not available, using REST API for reduce/close")
	}

	// REST API 降级
	resp, err := createReduceOrder(ctx, symbol, side, positionSide, quantity, priceStr)
	if err != nil {
		return nil, err
	}
	ensureOrderFilled(ctx, symbol, resp.OrderID, side, positionSide, quantity, true, 3)
	return resp, nil
}

// ensureOrderFilled 异步确保限价单成交
// 每隔 5 秒检查一次订单状态，未成交则撤单并用最新价重挂，最多重试 maxRetries 次后转市价单
func ensureOrderFilled(ctx context.Context, symbol string, orderID int64, side futures.SideType, positionSide futures.PositionSideType, quantity string, isReduceOnly bool, maxRetries int) {
	go func() {
		for attempt := 0; attempt <= maxRetries; attempt++ {
			time.Sleep(5 * time.Second)

			// 查询订单状态
			order, err := Client.NewGetOrderService().Symbol(symbol).OrderID(orderID).Do(ctx)
			if err != nil {
				log.Printf("[OrderTimeout] Failed to query order %d: %v", orderID, err)
				return
			}

			// 已完全成交，退出
			if order.Status == futures.OrderStatusTypeFilled {
				return
			}

			// 挂单中（未成交或部分成交），执行撤单+重挂
			if order.Status == futures.OrderStatusTypeNew || order.Status == futures.OrderStatusTypePartiallyFilled {
				_, cancelErr := Client.NewCancelOrderService().Symbol(symbol).OrderID(orderID).Do(ctx)
				if cancelErr != nil {
					log.Printf("[OrderTimeout] Cancel order %d failed: %v", orderID, cancelErr)
					return
				}
				log.Printf("[OrderTimeout] Cancelled unfilled order %d (attempt %d/%d)", orderID, attempt+1, maxRetries)

				if attempt >= maxRetries {
					// 已达最大重试次数，改用市价单
					log.Printf("[OrderTimeout] Max retries reached, placing market order for %s %s qty=%s", symbol, side, quantity)
					svc := Client.NewCreateOrderService().
						Symbol(symbol).
						Side(side).
						Type(futures.OrderTypeMarket).
						Quantity(quantity)
					if positionSide != "" {
						svc = svc.PositionSide(positionSide)
					}
					if isReduceOnly {
						svc = svc.ReduceOnly(true)
					}
					result, mktErr := svc.Do(ctx)
					if mktErr != nil {
						log.Printf("[OrderTimeout] Market order fallback failed: %v", mktErr)
					} else {
						log.Printf("[OrderTimeout] Market order placed: orderId=%d", result.OrderID)
					}
					return
				}

				// 获取最新价，重挂限价单
				price, priceErr := getCurrentPrice(ctx, symbol, "")
				if priceErr != nil {
					log.Printf("[OrderTimeout] Get price failed: %v", priceErr)
					return
				}
				pricePrecision, ppErr := getSymbolPricePrecision(ctx, symbol)
				if ppErr != nil {
					log.Printf("[OrderTimeout] Get price precision failed: %v", ppErr)
					return
				}
				priceStr := formatPrice(price, pricePrecision)

				svc := Client.NewCreateOrderService().
					Symbol(symbol).
					Side(side).
					Type(futures.OrderTypeLimit).
					TimeInForce(futures.TimeInForceTypeGTC).
					Quantity(quantity).
					Price(priceStr)
				if positionSide != "" {
					svc = svc.PositionSide(positionSide)
				}
				if isReduceOnly {
					svc = svc.ReduceOnly(true)
				}
				result, replaceErr := svc.Do(ctx)
				if replaceErr != nil {
					log.Printf("[OrderTimeout] Re-place order failed: %v, giving up", replaceErr)
					return
				}
				orderID = result.OrderID
				log.Printf("[OrderTimeout] Re-placed limit order: orderId=%d, price=%s (attempt %d/%d)", orderID, priceStr, attempt+1, maxRetries)
			} else {
				// 已取消、已拒绝或其他终态，直接退出
				return
			}
		}
	}()
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
