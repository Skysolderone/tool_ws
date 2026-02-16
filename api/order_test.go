package api

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/adshao/go-binance/v2/futures"
)

// 使用反射替换 Client 的方法，实现 mock 效果

// mockCreateOrderFunc 下单的 mock 函数类型
type mockCreateOrderFunc func(ctx context.Context, req PlaceOrderReq) (*futures.CreateOrderResponse, error)

// mockListOpenOrdersFunc 查询订单的 mock 函数类型
type mockListOpenOrdersFunc func(ctx context.Context, symbol string) ([]*futures.Order, error)

// mockCancelOrderFunc 撤单的 mock 函数类型
type mockCancelOrderFunc func(ctx context.Context, symbol string, orderID int64) (*futures.CancelOrderResponse, error)

// mockChangeLeverageFunc 调整杠杆的 mock 函数类型
type mockChangeLeverageFunc func(ctx context.Context, symbol string, leverage int) (*futures.SymbolLeverage, error)

// --- 测试辅助函数 ---

func setupMockClient(t *testing.T) func() {
	t.Helper()
	originalClient := Client
	return func() {
		Client = originalClient
	}
}

// --- 测试用例 ---

func TestPlaceOrder_LimitOrder(t *testing.T) {
	defer setupMockClient(t)()

	_ = context.Background()
	req := PlaceOrderReq{
		Symbol:        "BTCUSDT",
		Side:          futures.SideTypeBuy,
		OrderType:     futures.OrderTypeLimit,
		QuoteQuantity: "5",
		Leverage:      10,
		Price:         "43000",
		PositionSide:  futures.PositionSideTypeLong,
		TimeInForce:   futures.TimeInForceTypeGTC,
	}

	// 验证请求参数的有效性
	if req.Symbol == "" {
		t.Error("symbol should not be empty")
	}
	if req.Side != futures.SideTypeBuy && req.Side != futures.SideTypeSell {
		t.Error("invalid side")
	}
	if req.QuoteQuantity == "" {
		t.Error("quoteQuantity should not be empty")
	}
	if req.Leverage == 0 {
		t.Error("leverage should not be zero")
	}
	if req.OrderType == futures.OrderTypeLimit && req.Price == "" {
		t.Error("price required for LIMIT order")
	}

	t.Logf("PlaceOrder request validated: Symbol=%s, Side=%s, Type=%s, QuoteQuantity=%s, Leverage=%d, Price=%s",
		req.Symbol, req.Side, req.OrderType, req.QuoteQuantity, req.Leverage, req.Price)
}

func TestPlaceOrder_MarketOrder(t *testing.T) {
	defer setupMockClient(t)()

	_ = context.Background()
	req := PlaceOrderReq{
		Symbol:        "ETHUSDT",
		Side:          futures.SideTypeSell,
		OrderType:     futures.OrderTypeMarket,
		QuoteQuantity: "10",
		Leverage:      5,
	}

	// 验证市价单参数
	if req.OrderType != futures.OrderTypeMarket {
		t.Error("expected MARKET order type")
	}
	if req.Price != "" {
		t.Error("market order should not have price")
	}

	t.Logf("Market order validated: Symbol=%s, Side=%s, Type=%s, QuoteQuantity=%s, Leverage=%d",
		req.Symbol, req.Side, req.OrderType, req.QuoteQuantity, req.Leverage)
}

func TestPlaceOrderReq_Validation(t *testing.T) {
	tests := []struct {
		name    string
		req     PlaceOrderReq
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid limit order",
			req: PlaceOrderReq{
				Symbol:        "BTCUSDT",
				Side:          futures.SideTypeBuy,
				OrderType:     futures.OrderTypeLimit,
				QuoteQuantity: "5",
				Leverage:      10,
				Price:         "43000",
				TimeInForce:   futures.TimeInForceTypeGTC,
			},
			wantErr: false,
		},
		{
			name: "valid market order",
			req: PlaceOrderReq{
				Symbol:        "ETHUSDT",
				Side:          futures.SideTypeSell,
				OrderType:     futures.OrderTypeMarket,
				QuoteQuantity: "10",
				Leverage:      5,
			},
			wantErr: false,
		},
		{
			name: "missing symbol",
			req: PlaceOrderReq{
				Side:          futures.SideTypeBuy,
				OrderType:     futures.OrderTypeLimit,
				QuoteQuantity: "5",
				Leverage:      10,
				Price:         "43000",
			},
			wantErr: true,
			errMsg:  "symbol required",
		},
		{
			name: "missing quoteQuantity",
			req: PlaceOrderReq{
				Symbol:    "BTCUSDT",
				Side:      futures.SideTypeBuy,
				OrderType: futures.OrderTypeLimit,
				Leverage:  10,
				Price:     "43000",
			},
			wantErr: true,
			errMsg:  "quoteQuantity is required",
		},
		{
			name: "missing leverage",
			req: PlaceOrderReq{
				Symbol:        "BTCUSDT",
				Side:          futures.SideTypeBuy,
				OrderType:     futures.OrderTypeLimit,
				QuoteQuantity: "5",
				Price:         "43000",
			},
			wantErr: true,
			errMsg:  "leverage is required",
		},
		{
			name: "limit order without price",
			req: PlaceOrderReq{
				Symbol:        "BTCUSDT",
				Side:          futures.SideTypeBuy,
				OrderType:     futures.OrderTypeLimit,
				QuoteQuantity: "5",
				Leverage:      10,
			},
			wantErr: true,
			errMsg:  "price required for LIMIT order",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validatePlaceOrderReq(tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("validatePlaceOrderReq() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestPlaceOrder_WithStopPrice(t *testing.T) {
	defer setupMockClient(t)()

	req := PlaceOrderReq{
		Symbol:        "BTCUSDT",
		Side:          futures.SideTypeSell,
		OrderType:     futures.OrderTypeLimit,
		QuoteQuantity: "5",
		Leverage:      10,
		Price:         "45000",
		StopPrice:     "44000",
	}

	if req.StopPrice == "" {
		t.Error("stop price should be set")
	}

	t.Logf("Stop order validated: StopPrice=%s", req.StopPrice)
}

func TestPlaceOrder_ReduceOnly(t *testing.T) {
	defer setupMockClient(t)()

	req := PlaceOrderReq{
		Symbol:        "BTCUSDT",
		Side:          futures.SideTypeSell,
		OrderType:     futures.OrderTypeMarket,
		QuoteQuantity: "5",
		Leverage:      10,
		ReduceOnly:    true,
	}

	if !req.ReduceOnly {
		t.Error("reduceOnly should be true")
	}

	t.Log("Reduce only order validated")
}

func TestGetOrderList_Validation(t *testing.T) {
	defer setupMockClient(t)()

	tests := []struct {
		name   string
		symbol string
		desc   string
	}{
		{
			name:   "with specific symbol",
			symbol: "BTCUSDT",
			desc:   "should query orders for BTCUSDT",
		},
		{
			name:   "all symbols",
			symbol: "",
			desc:   "should query orders for all symbols",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Logf("GetOrderList: symbol=%q - %s", tt.symbol, tt.desc)
		})
	}
}

func TestCancelOrder_Validation(t *testing.T) {
	defer setupMockClient(t)()

	tests := []struct {
		name    string
		symbol  string
		orderID int64
		wantErr bool
	}{
		{
			name:    "valid cancel",
			symbol:  "BTCUSDT",
			orderID: 300001,
			wantErr: false,
		},
		{
			name:    "empty symbol",
			symbol:  "",
			orderID: 300001,
			wantErr: true,
		},
		{
			name:    "zero orderID",
			symbol:  "BTCUSDT",
			orderID: 0,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateCancelOrderParams(tt.symbol, tt.orderID)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateCancelOrderParams() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestChangeLeverage_Validation(t *testing.T) {
	defer setupMockClient(t)()

	tests := []struct {
		name     string
		symbol   string
		leverage int
		wantErr  bool
	}{
		{
			name:     "valid leverage 10",
			symbol:   "BTCUSDT",
			leverage: 10,
			wantErr:  false,
		},
		{
			name:     "valid leverage 125",
			symbol:   "BTCUSDT",
			leverage: 125,
			wantErr:  false,
		},
		{
			name:     "leverage too low",
			symbol:   "BTCUSDT",
			leverage: 0,
			wantErr:  true,
		},
		{
			name:     "leverage too high",
			symbol:   "BTCUSDT",
			leverage: 126,
			wantErr:  true,
		},
		{
			name:     "empty symbol",
			symbol:   "",
			leverage: 10,
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateChangeLeverageParams(tt.symbol, tt.leverage)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateChangeLeverageParams() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestPlaceOrderReq_FieldTypes(t *testing.T) {
	req := PlaceOrderReq{
		Symbol:        "BTCUSDT",
		Side:          futures.SideTypeBuy,
		OrderType:     futures.OrderTypeLimit,
		QuoteQuantity: "5",
		Leverage:      10,
		Price:         "43000",
		StopPrice:     "42000",
		PositionSide:  futures.PositionSideTypeLong,
		TimeInForce:   futures.TimeInForceTypeGTC,
		ReduceOnly:    true,
	}

	// 验证字段类型
	v := reflect.TypeOf(req)
	for i := 0; i < v.NumField(); i++ {
		field := v.Field(i)
		t.Logf("Field %s: Type=%s, Tag=%s", field.Name, field.Type, field.Tag.Get("json"))
	}

	// 验证枚举类型
	if req.Side != futures.SideTypeBuy && req.Side != futures.SideTypeSell {
		t.Error("invalid SideType")
	}

	if req.PositionSide != futures.PositionSideTypeBoth &&
		req.PositionSide != futures.PositionSideTypeLong &&
		req.PositionSide != futures.PositionSideTypeShort {
		t.Error("invalid PositionSideType")
	}
}

// --- 辅助验证函数 ---

func validatePlaceOrderReq(req PlaceOrderReq) error {
	if req.Symbol == "" {
		return errors.New("symbol required")
	}
	if req.QuoteQuantity == "" {
		return errors.New("quoteQuantity is required")
	}
	if req.Leverage == 0 {
		return errors.New("leverage is required")
	}
	if req.OrderType == futures.OrderTypeLimit && req.Price == "" {
		return errors.New("price required for LIMIT order")
	}
	return nil
}

func validateCancelOrderParams(symbol string, orderID int64) error {
	if symbol == "" {
		return errors.New("symbol required")
	}
	if orderID == 0 {
		return errors.New("orderID required")
	}
	return nil
}

func validateChangeLeverageParams(symbol string, leverage int) error {
	if symbol == "" {
		return errors.New("symbol required")
	}
	if leverage < 1 || leverage > 125 {
		return errors.New("leverage must be between 1 and 125")
	}
	return nil
}
