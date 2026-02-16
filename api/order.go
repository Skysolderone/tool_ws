package api

import (
	"context"
	"fmt"
	"log"
	"math"
	"strconv"
	"strings"

	"github.com/adshao/go-binance/v2/futures"
)

type PlaceOrderReq struct {
	Symbol       string                   `json:"symbol"`
	Side         futures.SideType         `json:"side"`      // BUY / SELL
	OrderType    futures.OrderType        `json:"orderType"` // LIMIT / MARKET
	Price        string                   `json:"price,omitempty"`
	StopPrice    string                   `json:"stopPrice,omitempty"`
	PositionSide futures.PositionSideType `json:"positionSide,omitempty"` // BOTH / LONG / SHORT
	TimeInForce  futures.TimeInForceType  `json:"timeInForce,omitempty"`  // GTC / IOC / FOK
	ReduceOnly   bool                     `json:"reduceOnly,omitempty"`

	// 必填字段：用 USDT 金额下单
	QuoteQuantity string `json:"quoteQuantity"` // USDT 保证金金额，必填，如 "5" 表示 5 USDT
	Leverage      int    `json:"leverage"`      // 杠杆倍数，必填，如 10 表示 10x

	// 止盈止损：设置后主单成交后自动挂止盈止损单
	// 方式1：指定止损价 + 盈亏比 → 自动计算止盈价
	StopLossPrice string `json:"stopLossPrice,omitempty"` // 止损价，与 riskReward 配合使用
	// 方式2：指定亏损金额(USDT) + 盈亏比 → 自动计算止损价和止盈价
	// 例：stopLossAmount=1, riskReward=3 表示最多亏1U，盈利目标3U
	StopLossAmount float64 `json:"stopLossAmount,omitempty"` // 最大亏损金额(USDT)
	RiskReward     float64 `json:"riskReward,omitempty"`     // 盈亏比，如 3 表示 1:3
}

// PlaceOrderResult 下单结果，包含主单和可选的止盈止损单
type PlaceOrderResult struct {
	Order      *futures.CreateOrderResponse `json:"order"`                // 主单
	TakeProfit *AlgoOrderResponse           `json:"takeProfit,omitempty"` // 止盈单 (Algo Order)
	StopLoss   *AlgoOrderResponse           `json:"stopLoss,omitempty"`   // 止损单 (Algo Order)
}

// PlaceOrder 下单，支持市价单/限价单/止损止盈
// QuoteQuantity（USDT 保证金金额）和 Leverage（杠杆倍数）为必填字段
func PlaceOrder(ctx context.Context, req PlaceOrderReq) (*futures.CreateOrderResponse, error) {
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

	// 如果未指定 positionSide，默认使用 BOTH（单向持仓模式）
	if req.PositionSide == "" {
		req.PositionSide = futures.PositionSideTypeBoth
	}

	// 根据 USDT 金额和杠杆计算代币数量
	quantity, err := calculateQuantityFromUSDT(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("calculate quantity: %w", err)
	}

	service := Client.NewCreateOrderService().
		Symbol(req.Symbol).
		Side(req.Side).
		Type(req.OrderType).
		Quantity(quantity).
		PositionSide(req.PositionSide)

	if req.Price != "" {
		service.Price(req.Price)
	}
	if req.StopPrice != "" {
		service.StopPrice(req.StopPrice)
	}
	if req.TimeInForce != "" {
		service.TimeInForce(req.TimeInForce)
	} else {
		service.TimeInForce(futures.TimeInForceTypeGTC)
	}
	if req.ReduceOnly {
		service.ReduceOnly(req.ReduceOnly)
	}

	return service.Do(ctx)
}

// calculateQuantityFromUSDT 根据 USDT 金额、杠杆和当前价格计算代币数量
// 公式：代币数量 = (USDT 金额 × 杠杆) / 当前价格
func calculateQuantityFromUSDT(ctx context.Context, req PlaceOrderReq) (string, error) {
	// 解析 USDT 金额
	usdtAmount, err := strconv.ParseFloat(req.QuoteQuantity, 64)
	if err != nil {
		return "", fmt.Errorf("invalid quoteQuantity: %w", err)
	}

	// 获取杠杆倍数（默认 1x）
	leverage := float64(req.Leverage)
	if leverage == 0 {
		leverage = 1
	}

	// 获取当前价格
	price, err := getCurrentPrice(ctx, req.Symbol, req.Price)
	if err != nil {
		return "", fmt.Errorf("get current price: %w", err)
	}

	// 计算代币数量 = (保证金 × 杠杆) / 价格
	notionalValue := usdtAmount * leverage // 总持仓价值
	quantity := notionalValue / price

	// 获取交易对精度信息
	precision, stepSize, err := getSymbolPrecision(ctx, req.Symbol)
	if err != nil {
		return "", fmt.Errorf("get symbol precision: %w", err)
	}

	// 根据 stepSize 调整数量
	quantity = roundToStepSize(quantity, stepSize)

	// 格式化为指定精度的字符串
	return formatQuantity(quantity, precision), nil
}

// getCurrentPrice 获取当前市场价格，如果已提供限价则使用限价
// 优先使用 WebSocket 订阅的实时价格缓存，避免频繁的 REST API 调用
func getCurrentPrice(ctx context.Context, symbol, limitPrice string) (float64, error) {
	// 如果是限价单，使用用户提供的价格
	if limitPrice != "" {
		return strconv.ParseFloat(limitPrice, 64)
	}

	// 从 WebSocket 价格缓存获取实时价格
	cache := GetPriceCache()
	price, err := cache.GetPrice(symbol)
	if err == nil {
		return price, nil
	}

	// 如果 WebSocket 价格获取失败，降级使用 REST API
	log.Printf("[Order] WebSocket price failed for %s, falling back to REST API: %v", symbol, err)
	prices, err := Client.NewListPricesService().Symbol(symbol).Do(ctx)
	if err != nil {
		return 0, fmt.Errorf("fetch market price: %w", err)
	}
	if len(prices) == 0 {
		return 0, fmt.Errorf("no price data for symbol %s", symbol)
	}

	return strconv.ParseFloat(prices[0].Price, 64)
}

// getSymbolPrecision 获取交易对的精度和步长信息
func getSymbolPrecision(ctx context.Context, symbol string) (precision int, stepSize float64, err error) {
	info, err := Client.NewExchangeInfoService().Do(ctx)
	if err != nil {
		return 0, 0, fmt.Errorf("fetch exchange info: %w", err)
	}

	for _, s := range info.Symbols {
		if s.Symbol == symbol {
			// 从 LOT_SIZE 过滤器获取 stepSize
			for _, filter := range s.Filters {
				if filterType, ok := filter["filterType"].(string); ok && filterType == "LOT_SIZE" {
					if stepSizeStr, ok := filter["stepSize"].(string); ok {
						stepSize, _ = strconv.ParseFloat(stepSizeStr, 64)
						break
					}
				}
			}
			return s.QuantityPrecision, stepSize, nil
		}
	}

	return 0, 0, fmt.Errorf("symbol %s not found in exchange info", symbol)
}

// roundToStepSize 将数量调整为 stepSize 的整数倍
func roundToStepSize(quantity, stepSize float64) float64 {
	if stepSize == 0 {
		return quantity
	}
	return math.Floor(quantity/stepSize) * stepSize
}

// formatQuantity 格式化数量为指定精度的字符串，去除尾部零
func formatQuantity(quantity float64, precision int) string {
	formatted := strconv.FormatFloat(quantity, 'f', precision, 64)
	// 去除尾部的零和小数点
	formatted = strings.TrimRight(formatted, "0")
	formatted = strings.TrimRight(formatted, ".")
	return formatted
}

// GetOrderList 获取当前未成交订单
func GetOrderList(ctx context.Context, symbol string) ([]*futures.Order, error) {
	service := Client.NewListOpenOrdersService()
	if symbol != "" {
		service.Symbol(symbol)
	}
	return service.Do(ctx)
}

// CancelOrder 取消订单
func CancelOrder(ctx context.Context, symbol string, orderID int64) (*futures.CancelOrderResponse, error) {
	return Client.NewCancelOrderService().
		Symbol(symbol).
		OrderID(orderID).
		Do(ctx)
}

// ChangeLeverage 调整杠杆倍数
func ChangeLeverage(ctx context.Context, symbol string, leverage int) (*futures.SymbolLeverage, error) {
	return Client.NewChangeLeverageService().
		Symbol(symbol).
		Leverage(leverage).
		Do(ctx)
}

// ReducePositionReq 减仓请求
type ReducePositionReq struct {
	Symbol       string                   `json:"symbol"`                 // 交易对，必填
	PositionSide futures.PositionSideType `json:"positionSide,omitempty"` // LONG / SHORT / BOTH
	Quantity     string                   `json:"quantity,omitempty"`     // 减仓数量（代币），与 percent 二选一
	Percent      float64                  `json:"percent,omitempty"`      // 减仓比例 0-100，如 50 表示减仓 50%
}

// ClosePositionReq 平仓请求
type ClosePositionReq struct {
	Symbol       string                   `json:"symbol"`                 // 交易对，必填
	PositionSide futures.PositionSideType `json:"positionSide,omitempty"` // LONG / SHORT / BOTH
}

// ReducePosition 减仓：市价卖出指定数量或比例的持仓
// 支持两种模式：
//  1. 指定 Quantity — 直接按代币数量减仓
//  2. 指定 Percent — 按当前持仓的百分比减仓（如 50 = 50%）
func ReducePosition(ctx context.Context, req ReducePositionReq) (*futures.CreateOrderResponse, error) {
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
		absAmt := math.Abs(posAmt)
		reduceQty = absAmt * req.Percent / 100
	} else {
		return nil, fmt.Errorf("quantity or percent is required")
	}

	// 精度调整
	precision, stepSize, err := getSymbolPrecision(ctx, req.Symbol)
	if err != nil {
		return nil, fmt.Errorf("get symbol precision: %w", err)
	}
	reduceQty = roundToStepSize(reduceQty, stepSize)
	quantity := formatQuantity(reduceQty, precision)

	// 确定平仓方向：多仓用 SELL，空仓用 BUY
	side := futures.SideTypeSell
	if posAmt < 0 {
		side = futures.SideTypeBuy
	}

	return createReduceOrder(ctx, req.Symbol, side, req.PositionSide, quantity)
}

// ClosePosition 全部平仓：市价平掉指定交易对的全部持仓
func ClosePosition(ctx context.Context, req ClosePositionReq) (*futures.CreateOrderResponse, error) {
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

	// 精度调整
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

	return createReduceOrder(ctx, req.Symbol, side, req.PositionSide, quantity)
}

// findPosition 查找指定交易对和持仓方向的仓位
func findPosition(ctx context.Context, symbol string, positionSide futures.PositionSideType) (*futures.PositionRisk, error) {
	service := Client.NewGetPositionRiskService().Symbol(symbol)
	positions, err := service.Do(ctx)
	if err != nil {
		return nil, fmt.Errorf("query positions: %w", err)
	}

	for _, pos := range positions {
		amt, _ := strconv.ParseFloat(pos.PositionAmt, 64)
		if amt == 0 {
			continue
		}
		// 如果指定了 positionSide，精确匹配
		if positionSide != "" && futures.PositionSideType(pos.PositionSide) != positionSide {
			continue
		}
		return pos, nil
	}

	return nil, fmt.Errorf("no open position for %s (side=%s)", symbol, positionSide)
}

// createReduceOrder 创建减仓/平仓市价单（reduceOnly=true）
func createReduceOrder(ctx context.Context, symbol string, side futures.SideType, positionSide futures.PositionSideType, quantity string) (*futures.CreateOrderResponse, error) {
	if positionSide == "" {
		positionSide = futures.PositionSideTypeBoth
	}
	service := Client.NewCreateOrderService().
		Symbol(symbol).
		Side(side).
		Type(futures.OrderTypeMarket).
		Quantity(quantity).
		PositionSide(positionSide).
		ReduceOnly(true)

	return service.Do(ctx)
}

// --- 止盈止损 ---

// calcTPSLPrices 根据入场价、止损价和盈亏比计算止盈价
// 做多: 止盈价 = entryPrice + (entryPrice - stopLoss) * riskReward
// 做空: 止盈价 = entryPrice - (stopLoss - entryPrice) * riskReward
func calcTPSLPrices(entryPrice, stopLossPrice, riskReward float64, isBuy bool) (takeProfit float64) {
	if isBuy {
		risk := entryPrice - stopLossPrice
		takeProfit = entryPrice + risk*riskReward
	} else {
		risk := stopLossPrice - entryPrice
		takeProfit = entryPrice - risk*riskReward
	}
	return
}

// getSymbolPricePrecision 获取交易对的价格精度
func getSymbolPricePrecision(ctx context.Context, symbol string) (int, error) {
	info, err := Client.NewExchangeInfoService().Do(ctx)
	if err != nil {
		return 0, fmt.Errorf("fetch exchange info: %w", err)
	}

	for _, s := range info.Symbols {
		if s.Symbol == symbol {
			return s.PricePrecision, nil
		}
	}
	return 0, fmt.Errorf("symbol %s not found", symbol)
}

// formatPrice 格式化价格为指定精度的字符串
func formatPrice(price float64, precision int) string {
	formatted := strconv.FormatFloat(price, 'f', precision, 64)
	formatted = strings.TrimRight(formatted, "0")
	formatted = strings.TrimRight(formatted, ".")
	return formatted
}

// PlaceTPSLOrders 在主单成交后挂止盈止损单
// entryPrice: 入场价（市价单用 avgPrice，限价单用 price）
// quantity: 与主单相同的数量（代币数量字符串）
//
// 支持两种模式：
//  1. stopLossPrice + riskReward → 直接用止损价，计算止盈价
//  2. stopLossAmount + riskReward → 根据 USDT 亏损金额计算止损价和止盈价
//     公式：止损价距 = stopLossAmount / quantity, SL = entry ± 价距, TP = entry ± 价距×riskReward
func PlaceTPSLOrders(ctx context.Context, req PlaceOrderReq, entryPrice float64, quantity string) (tp *AlgoOrderResponse, sl *AlgoOrderResponse, err error) {
	isBuy := req.Side == futures.SideTypeBuy

	var stopLossPrice, takeProfitPrice float64

	if req.StopLossPrice != "" {
		// 方式1：用户直接指定止损价
		stopLossPrice, err = strconv.ParseFloat(req.StopLossPrice, 64)
		if err != nil {
			return nil, nil, fmt.Errorf("invalid stopLossPrice: %w", err)
		}
		takeProfitPrice = calcTPSLPrices(entryPrice, stopLossPrice, req.RiskReward, isBuy)
	} else if req.StopLossAmount > 0 {
		// 方式2：根据 USDT 亏损金额计算
		qty, parseErr := strconv.ParseFloat(quantity, 64)
		if parseErr != nil || qty == 0 {
			return nil, nil, fmt.Errorf("invalid quantity for TPSL calculation: %s", quantity)
		}
		// 止损价距（每个代币承受的价格波动）= USDT亏损额 / 代币数量
		slDistance := req.StopLossAmount / qty
		if isBuy {
			stopLossPrice = entryPrice - slDistance
			takeProfitPrice = entryPrice + slDistance*req.RiskReward
		} else {
			stopLossPrice = entryPrice + slDistance
			takeProfitPrice = entryPrice - slDistance*req.RiskReward
		}
		log.Printf("[TPSL] stopLossAmount=%.2f USDT, quantity=%s, slDistance=%.4f, SL=%.4f, TP=%.4f",
			req.StopLossAmount, quantity, slDistance, stopLossPrice, takeProfitPrice)
	} else {
		return nil, nil, fmt.Errorf("stopLossPrice or stopLossAmount is required")
	}

	// 验证价格合理性
	if isBuy {
		if stopLossPrice >= entryPrice {
			return nil, nil, fmt.Errorf("stopLossPrice (%.2f) must be below entryPrice (%.2f) for BUY", stopLossPrice, entryPrice)
		}
		if takeProfitPrice <= entryPrice {
			return nil, nil, fmt.Errorf("calculated takeProfitPrice (%.2f) must be above entryPrice (%.2f)", takeProfitPrice, entryPrice)
		}
	} else {
		if stopLossPrice <= entryPrice {
			return nil, nil, fmt.Errorf("stopLossPrice (%.2f) must be above entryPrice (%.2f) for SELL", stopLossPrice, entryPrice)
		}
		if takeProfitPrice >= entryPrice {
			return nil, nil, fmt.Errorf("calculated takeProfitPrice (%.2f) must be below entryPrice (%.2f)", takeProfitPrice, entryPrice)
		}
	}

	// 获取价格精度
	pricePrecision, err := getSymbolPricePrecision(ctx, req.Symbol)
	if err != nil {
		return nil, nil, err
	}
	tpPriceStr := formatPrice(takeProfitPrice, pricePrecision)
	slPriceStr := formatPrice(stopLossPrice, pricePrecision)

	// 止盈止损的平仓方向：做多用 SELL 平，做空用 BUY 平
	closeSide := futures.SideTypeSell
	if !isBuy {
		closeSide = futures.SideTypeBuy
	}

	log.Printf("[TPSL] entryPrice=%.4f, stopLoss=%s, takeProfit=%s, riskReward=1:%.1f",
		entryPrice, slPriceStr, tpPriceStr, req.RiskReward)

	// 确保 positionSide 有值
	positionSide := req.PositionSide
	if positionSide == "" {
		positionSide = futures.PositionSideTypeBoth
	}

	// 下止盈单 (TAKE_PROFIT_MARKET: 触发后市价平仓) — 使用 Algo Order API
	tpResult, err := PlaceAlgoOrder(ctx, AlgoOrderParams{
		Symbol:        req.Symbol,
		Side:          string(closeSide),
		OrderType:     "TAKE_PROFIT_MARKET",
		TriggerPrice:  tpPriceStr,
		ClosePosition: true,
		PositionSide:  string(positionSide),
	})
	if err != nil {
		return nil, nil, fmt.Errorf("place take-profit order: %w", err)
	}

	// 下止损单 (STOP_MARKET: 触发后市价平仓) — 使用 Algo Order API
	slResult, err := PlaceAlgoOrder(ctx, AlgoOrderParams{
		Symbol:        req.Symbol,
		Side:          string(closeSide),
		OrderType:     "STOP_MARKET",
		TriggerPrice:  slPriceStr,
		ClosePosition: true,
		PositionSide:  string(positionSide),
	})
	if err != nil {
		// 止损挂单失败，尝试撤销已挂的止盈单
		log.Printf("[TPSL] stop-loss failed, cancelling take-profit algo order %d: %v", tpResult.AlgoID, err)
		_ = CancelAlgoOrder(ctx, req.Symbol, tpResult.AlgoID)
		return nil, nil, fmt.Errorf("place stop-loss order: %w", err)
	}

	return tpResult, slResult, nil
}
