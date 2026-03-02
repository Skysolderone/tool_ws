package api

import (
	"context"
	"fmt"
	"log"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/adshao/go-binance/v2/futures"
)

const exchangeInfoCacheTTL = 5 * time.Minute

var (
	exchangeInfoMu       sync.RWMutex
	exchangeInfoRefresh  sync.Mutex
	exchangeInfoCachedAt time.Time
	exchangeInfoData     *futures.ExchangeInfo
)

// TPLevel 阶梯止盈级别
type TPLevel struct {
	Percent    float64 `json:"percent"`    // 该级别平仓比例(%)，如 50 表示平 50% 仓位
	RiskReward float64 `json:"riskReward"` // 该级别盈亏比，如 2 表示 1:2
}

type PlaceOrderReq struct {
	Source       string                   `json:"source,omitempty"` // manual / strategy_xxx
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

	// 阶梯止盈：设置后替代单一止盈，支持多级止盈
	// 例：[{percent:50, riskReward:2}, {percent:50, riskReward:5}]
	// 表示 50% 仓位在 1:2 止盈，剩余 50% 在 1:5 止盈
	TPLevels []TPLevel `json:"tpLevels,omitempty"`
}

// ReversePositionReq 一键反手请求
type ReversePositionReq struct {
	Symbol        string `json:"symbol"`                  // 交易对，必填
	PositionSide  string `json:"positionSide,omitempty"`  // LONG / SHORT / BOTH
	QuoteQuantity string `json:"quoteQuantity,omitempty"` // 反向开仓金额(USDT)，不填则用原仓位等值保证金
	Leverage      int    `json:"leverage,omitempty"`      // 反向杠杆，不填则用原仓位杠杆
}

// PlaceOrderResult 下单结果，包含主单和可选的止盈止损单
type PlaceOrderResult struct {
	Order            *futures.CreateOrderResponse `json:"order"`                       // 主单
	TakeProfit       *AlgoOrderResponse           `json:"takeProfit,omitempty"`        // 止盈单 (单级，Algo模式兼容)
	TakeProfits      []*AlgoOrderResponse         `json:"takeProfits,omitempty"`       // 阶梯止盈单列表 (Algo模式兼容)
	StopLoss         *AlgoOrderResponse           `json:"stopLoss,omitempty"`          // 止损单 (Algo模式兼容)
	LocalTPSLGroupID string                       `json:"localTpslGroupId,omitempty"`  // 本地TPSL组ID
}

// ReversePositionResult 一键反手结果
type ReversePositionResult struct {
	CloseOrder *futures.CreateOrderResponse `json:"closeOrder"`          // 平仓单
	OpenOrder  *futures.CreateOrderResponse `json:"openOrder,omitempty"` // 反向开仓单
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
	info, err := getExchangeInfoCached(ctx)
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

// createReduceOrder 创建减仓/平仓限价单（用当前价格，reduceOnly=true）
func createReduceOrder(ctx context.Context, symbol string, side futures.SideType, positionSide futures.PositionSideType, quantity string, priceStr ...string) (*futures.CreateOrderResponse, error) {
	if positionSide == "" {
		positionSide = futures.PositionSideTypeBoth
	}

	// 确定限价
	var price string
	if len(priceStr) > 0 && priceStr[0] != "" {
		price = priceStr[0]
	} else {
		// 兜底获取当前价格
		p, err := getCurrentPrice(ctx, symbol, "")
		if err != nil {
			return nil, fmt.Errorf("get current price for reduce: %w", err)
		}
		pp, err := getSymbolPricePrecision(ctx, symbol)
		if err != nil {
			return nil, fmt.Errorf("get price precision: %w", err)
		}
		price = formatPrice(p, pp)
	}

	service := Client.NewCreateOrderService().
		Symbol(symbol).
		Side(side).
		Type(futures.OrderTypeLimit).
		TimeInForce(futures.TimeInForceTypeGTC).
		Quantity(quantity).
		Price(price).
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
	info, err := getExchangeInfoCached(ctx)
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

func getExchangeInfoCached(ctx context.Context) (*futures.ExchangeInfo, error) {
	now := time.Now()

	exchangeInfoMu.RLock()
	if exchangeInfoData != nil && now.Sub(exchangeInfoCachedAt) < exchangeInfoCacheTTL {
		data := exchangeInfoData
		exchangeInfoMu.RUnlock()
		return data, nil
	}
	exchangeInfoMu.RUnlock()

	// 仅允许一个请求刷新缓存，避免高并发下重复请求 Binance。
	exchangeInfoRefresh.Lock()
	defer exchangeInfoRefresh.Unlock()

	// double check
	now = time.Now()
	exchangeInfoMu.RLock()
	if exchangeInfoData != nil && now.Sub(exchangeInfoCachedAt) < exchangeInfoCacheTTL {
		data := exchangeInfoData
		exchangeInfoMu.RUnlock()
		return data, nil
	}
	exchangeInfoMu.RUnlock()

	info, err := Client.NewExchangeInfoService().Do(ctx)
	if err != nil {
		return nil, err
	}

	exchangeInfoMu.Lock()
	exchangeInfoData = info
	exchangeInfoCachedAt = time.Now()
	exchangeInfoMu.Unlock()

	return info, nil
}

// formatPrice 格式化价格为指定精度的字符串
func formatPrice(price float64, precision int) string {
	formatted := strconv.FormatFloat(price, 'f', precision, 64)
	formatted = strings.TrimRight(formatted, "0")
	formatted = strings.TrimRight(formatted, ".")
	return formatted
}

// calcStopLossPrice 根据请求参数计算止损价
func calcStopLossPrice(req PlaceOrderReq, entryPrice float64, quantity string) (float64, float64, error) {
	isBuy := req.Side == futures.SideTypeBuy
	var stopLossPrice, slDistance float64

	if req.StopLossPrice != "" {
		var err error
		stopLossPrice, err = strconv.ParseFloat(req.StopLossPrice, 64)
		if err != nil {
			return 0, 0, fmt.Errorf("invalid stopLossPrice: %w", err)
		}
		if isBuy {
			slDistance = entryPrice - stopLossPrice
		} else {
			slDistance = stopLossPrice - entryPrice
		}
	} else if req.StopLossAmount > 0 {
		qty, parseErr := strconv.ParseFloat(quantity, 64)
		if parseErr != nil || qty == 0 {
			return 0, 0, fmt.Errorf("invalid quantity for TPSL calculation: %s", quantity)
		}
		slDistance = req.StopLossAmount / qty
		if isBuy {
			stopLossPrice = entryPrice - slDistance
		} else {
			stopLossPrice = entryPrice + slDistance
		}
		log.Printf("[TPSL] stopLossAmount=%.2f USDT, quantity=%s, slDistance=%.4f, SL=%.4f",
			req.StopLossAmount, quantity, slDistance, stopLossPrice)
	} else {
		return 0, 0, fmt.Errorf("stopLossPrice or stopLossAmount is required")
	}

	return stopLossPrice, slDistance, nil
}

// PlaceTPSLOrders 在主单成交后挂止盈止损单
// 支持单级止盈和阶梯止盈两种模式：
//   - 单级：用 riskReward 计算单个 TP，使用本次下单数量（非 closePosition 全平）
//   - 阶梯：用 tpLevels 数组，每级指定 percent + riskReward，按比例拆分数量
func PlaceTPSLOrders(ctx context.Context, req PlaceOrderReq, entryPrice float64, quantity string) (tp *AlgoOrderResponse, sl *AlgoOrderResponse, err error) {
	isBuy := req.Side == futures.SideTypeBuy

	// 计算止损价
	stopLossPrice, slDistance, err := calcStopLossPrice(req, entryPrice, quantity)
	if err != nil {
		return nil, nil, err
	}

	// 验证止损价合理性
	if isBuy && stopLossPrice >= entryPrice {
		return nil, nil, fmt.Errorf("stopLossPrice (%.2f) must be below entryPrice (%.2f) for BUY", stopLossPrice, entryPrice)
	}
	if !isBuy && stopLossPrice <= entryPrice {
		return nil, nil, fmt.Errorf("stopLossPrice (%.2f) must be above entryPrice (%.2f) for SELL", stopLossPrice, entryPrice)
	}

	// 获取价格和数量精度
	pricePrecision, err := getSymbolPricePrecision(ctx, req.Symbol)
	if err != nil {
		return nil, nil, err
	}
	slPriceStr := formatPrice(stopLossPrice, pricePrecision)

	closeSide := futures.SideTypeSell
	if !isBuy {
		closeSide = futures.SideTypeBuy
	}

	positionSide := req.PositionSide
	if positionSide == "" {
		positionSide = futures.PositionSideTypeBoth
	}

	// 如果有阶梯止盈，走 combo 路径
	if len(req.TPLevels) > 0 {
		return placeComboTPSL(ctx, req, entryPrice, quantity, stopLossPrice, slDistance, pricePrecision, closeSide, positionSide)
	}

	// 单级止盈
	var takeProfitPrice float64
	if isBuy {
		takeProfitPrice = entryPrice + slDistance*req.RiskReward
	} else {
		takeProfitPrice = entryPrice - slDistance*req.RiskReward
	}

	// 验证止盈价
	if isBuy && takeProfitPrice <= entryPrice {
		return nil, nil, fmt.Errorf("calculated takeProfitPrice (%.2f) must be above entryPrice (%.2f)", takeProfitPrice, entryPrice)
	}
	if !isBuy && takeProfitPrice >= entryPrice {
		return nil, nil, fmt.Errorf("calculated takeProfitPrice (%.2f) must be below entryPrice (%.2f)", takeProfitPrice, entryPrice)
	}

	tpPriceStr := formatPrice(takeProfitPrice, pricePrecision)

	log.Printf("[TPSL] entryPrice=%.4f, stopLoss=%s, takeProfit=%s, riskReward=1:%.1f",
		entryPrice, slPriceStr, tpPriceStr, req.RiskReward)

	// 下止盈单（使用本次下单数量，而非 closePosition 全平）
	tpResult, err := PlaceAlgoOrder(ctx, AlgoOrderParams{
		Symbol:       req.Symbol,
		Side:         string(closeSide),
		OrderType:    "TAKE_PROFIT_MARKET",
		TriggerPrice: tpPriceStr,
		Quantity:     quantity,
		PositionSide: string(positionSide),
	})
	if err != nil {
		return nil, nil, fmt.Errorf("place take-profit order: %w", err)
	}

	// 下止损单（使用本次下单数量，而非 closePosition 全平）
	slResult, err := PlaceAlgoOrder(ctx, AlgoOrderParams{
		Symbol:       req.Symbol,
		Side:         string(closeSide),
		OrderType:    "STOP_MARKET",
		TriggerPrice: slPriceStr,
		Quantity:     quantity,
		PositionSide: string(positionSide),
	})
	if err != nil {
		log.Printf("[TPSL] stop-loss failed, cancelling take-profit algo order %d: %v", tpResult.AlgoID, err)
		_ = CancelAlgoOrder(ctx, req.Symbol, tpResult.AlgoID)
		return nil, nil, fmt.Errorf("place stop-loss order: %w", err)
	}

	return tpResult, slResult, nil
}

// placeComboTPSL 阶梯止盈止损：多个 TP 级别 + 一个 SL
// 每个 TP 级别按 percent 拆分数量，SL 使用 closePosition=true 平掉剩余
func placeComboTPSL(ctx context.Context, req PlaceOrderReq, entryPrice float64, quantity string,
	stopLossPrice, slDistance float64, pricePrecision int,
	closeSide futures.SideType, positionSide futures.PositionSideType,
) (tp *AlgoOrderResponse, sl *AlgoOrderResponse, err error) {
	isBuy := req.Side == futures.SideTypeBuy
	slPriceStr := formatPrice(stopLossPrice, pricePrecision)

	// 验证 tpLevels 总比例
	var totalPct float64
	for _, lv := range req.TPLevels {
		if lv.Percent <= 0 || lv.RiskReward <= 0 {
			return nil, nil, fmt.Errorf("tpLevels: each level must have positive percent and riskReward")
		}
		totalPct += lv.Percent
	}
	if math.Abs(totalPct-100) > 0.01 {
		return nil, nil, fmt.Errorf("tpLevels: total percent must equal 100, got %.2f", totalPct)
	}

	// 获取数量精度
	qtyPrecision, stepSize, err := getSymbolPrecision(ctx, req.Symbol)
	if err != nil {
		return nil, nil, err
	}
	totalQty, _ := strconv.ParseFloat(quantity, 64)

	var tpResults []*AlgoOrderResponse
	var placedAlgoIDs []int64

	// 逐级下止盈单
	for i, lv := range req.TPLevels {
		var tpPrice float64
		if isBuy {
			tpPrice = entryPrice + slDistance*lv.RiskReward
		} else {
			tpPrice = entryPrice - slDistance*lv.RiskReward
		}
		tpPriceStr := formatPrice(tpPrice, pricePrecision)

		// 计算该级别的数量
		levelQty := totalQty * lv.Percent / 100
		levelQty = roundToStepSize(levelQty, stepSize)
		levelQtyStr := formatQuantity(levelQty, qtyPrecision)

		log.Printf("[ComboTPSL] Level %d: %.0f%% qty=%s, TP=%.4f (rr=1:%.1f)",
			i+1, lv.Percent, levelQtyStr, tpPrice, lv.RiskReward)

		tpResult, tpErr := PlaceAlgoOrder(ctx, AlgoOrderParams{
			Symbol:       req.Symbol,
			Side:         string(closeSide),
			OrderType:    "TAKE_PROFIT_MARKET",
			TriggerPrice: tpPriceStr,
			Quantity:     levelQtyStr,
			PositionSide: string(positionSide),
		})
		if tpErr != nil {
			// 撤销已挂的止盈单
			for _, id := range placedAlgoIDs {
				_ = CancelAlgoOrder(ctx, req.Symbol, id)
			}
			return nil, nil, fmt.Errorf("place combo TP level %d: %w", i+1, tpErr)
		}
		tpResults = append(tpResults, tpResult)
		placedAlgoIDs = append(placedAlgoIDs, tpResult.AlgoID)
	}

	// 下止损单（使用本次下单全部数量，而非 closePosition 全平）
	slResult, slErr := PlaceAlgoOrder(ctx, AlgoOrderParams{
		Symbol:       req.Symbol,
		Side:         string(closeSide),
		OrderType:    "STOP_MARKET",
		TriggerPrice: slPriceStr,
		Quantity:     quantity,
		PositionSide: string(positionSide),
	})
	if slErr != nil {
		for _, id := range placedAlgoIDs {
			_ = CancelAlgoOrder(ctx, req.Symbol, id)
		}
		return nil, nil, fmt.Errorf("place combo SL: %w", slErr)
	}

	// 返回第一个 TP 作为兼容字段，完整列表在 TakeProfits
	var firstTP *AlgoOrderResponse
	if len(tpResults) > 0 {
		firstTP = tpResults[0]
	}

	// 注意：tpResults 存储在调用方的 PlaceOrderResult.TakeProfits 中
	// 这里通过闭包无法直接设置，需要调用方处理
	// 暂时返回 firstTP, slResult；调用方需检查 req.TPLevels 来获取完整列表
	// 为了解决这个问题，我们把 tpResults 存在全局临时变量中
	lastComboTPResults = tpResults

	return firstTP, slResult, nil
}

// lastComboTPResults 临时存储最近一次 combo TP 的所有结果
// 在 PlaceOrderViaWs 中读取后清空
var lastComboTPResults []*AlgoOrderResponse

// ReversePosition 一键反手：平掉当前仓位，然后反向开仓
func ReversePosition(ctx context.Context, req ReversePositionReq) (*ReversePositionResult, error) {
	if req.Symbol == "" {
		return nil, fmt.Errorf("symbol is required")
	}

	posSide := futures.PositionSideType(req.PositionSide)
	if posSide == "" {
		posSide = futures.PositionSideTypeBoth
	}

	// 查询当前仓位
	position, err := findPosition(ctx, req.Symbol, posSide)
	if err != nil {
		return nil, fmt.Errorf("find position: %w", err)
	}

	posAmt, _ := strconv.ParseFloat(position.PositionAmt, 64)
	if posAmt == 0 {
		return nil, fmt.Errorf("no open position for %s", req.Symbol)
	}

	isLong := posAmt > 0
	entryPrice, _ := strconv.ParseFloat(position.EntryPrice, 64)
	posLeverage, _ := strconv.Atoi(position.Leverage)

	// 平仓
	closeResp, err := ClosePositionViaWs(ctx, ClosePositionReq{
		Symbol:       req.Symbol,
		PositionSide: posSide,
	})
	if err != nil {
		return nil, fmt.Errorf("close position: %w", err)
	}

	result := &ReversePositionResult{CloseOrder: closeResp}

	// 计算反向开仓参数
	leverage := req.Leverage
	if leverage == 0 {
		leverage = posLeverage
	}

	quoteQty := req.QuoteQuantity
	if quoteQty == "" {
		// 用原仓位的保证金等值开仓：notional / leverage
		absAmt := math.Abs(posAmt)
		notional := absAmt * entryPrice
		margin := notional / float64(leverage)
		quoteQty = strconv.FormatFloat(margin, 'f', 2, 64)
	}

	// 反向：原多→开空，原空→开多
	var newSide futures.SideType
	var newPosSide futures.PositionSideType
	if isLong {
		newSide = futures.SideTypeSell
		newPosSide = futures.PositionSideTypeShort
	} else {
		newSide = futures.SideTypeBuy
		newPosSide = futures.PositionSideTypeLong
	}
	if posSide == futures.PositionSideTypeBoth {
		newPosSide = futures.PositionSideTypeBoth
	}

	// 反向开仓
	openReq := PlaceOrderReq{
		Source:        "reverse",
		Symbol:        req.Symbol,
		Side:          newSide,
		OrderType:     futures.OrderTypeMarket,
		PositionSide:  newPosSide,
		QuoteQuantity: quoteQty,
		Leverage:      leverage,
	}

	openResult, err := PlaceOrderViaWs(ctx, openReq)
	if err != nil {
		log.Printf("[Reverse] Close succeeded but open failed: %v", err)
		return result, fmt.Errorf("reverse open failed (position already closed): %w", err)
	}
	result.OpenOrder = openResult.Order

	log.Printf("[Reverse] %s %s → %s, closeOrderId=%d, openOrderId=%d",
		req.Symbol, map[bool]string{true: "LONG", false: "SHORT"}[isLong],
		map[bool]string{true: "SHORT", false: "LONG"}[isLong],
		closeResp.OrderID, openResult.Order.OrderID)

	return result, nil
}
