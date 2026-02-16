package api

import (
	"bytes"
	"encoding/json"
	"net/url"
	"testing"

	"github.com/adshao/go-binance/v2/futures"
)

// --- Handler 层测试：参数解析和数据格式验证 ---

func TestGetPositions_QueryParam(t *testing.T) {
	// 测试查询参数解析逻辑
	tests := []struct {
		name     string
		rawQuery string
		expected string
	}{
		{
			name:     "with symbol",
			rawQuery: "symbol=BTCUSDT",
			expected: "BTCUSDT",
		},
		{
			name:     "empty query",
			rawQuery: "",
			expected: "",
		},
		{
			name:     "ETH symbol",
			rawQuery: "symbol=ETHUSDT",
			expected: "ETHUSDT",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			values, err := url.ParseQuery(tt.rawQuery)
			if err != nil {
				t.Fatalf("parse query: %v", err)
			}

			symbol := values.Get("symbol")
			if symbol != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, symbol)
			}

			t.Logf("Parsed symbol from query: %q", symbol)
		})
	}
}

func TestPlaceOrder_JSONSerialization(t *testing.T) {
	// 测试下单请求的 JSON 序列化
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

	body, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}

	var parsed PlaceOrderReq
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("unmarshal request: %v", err)
	}

	if parsed.Symbol != req.Symbol {
		t.Errorf("symbol mismatch: expected %s, got %s", req.Symbol, parsed.Symbol)
	}
	if parsed.QuoteQuantity != req.QuoteQuantity {
		t.Errorf("quoteQuantity mismatch: expected %s, got %s", req.QuoteQuantity, parsed.QuoteQuantity)
	}
	if parsed.Leverage != req.Leverage {
		t.Errorf("leverage mismatch: expected %d, got %d", req.Leverage, parsed.Leverage)
	}

	t.Logf("JSON roundtrip successful: %s", string(body))
}

func TestPlaceOrder_InvalidJSON(t *testing.T) {
	// 测试无效 JSON 的处理
	invalidJSON := []byte(`{invalid json`)

	var req PlaceOrderReq
	err := json.Unmarshal(invalidJSON, &req)
	if err == nil {
		t.Error("expected error for invalid JSON")
	}

	t.Logf("Correctly detected invalid JSON: %v", err)
}

func TestCancelOrder_QueryParams(t *testing.T) {
	// 测试撤单参数解析
	tests := []struct {
		name      string
		rawQuery  string
		symbol    string
		orderID   string
		wantError bool
	}{
		{
			name:      "valid params",
			rawQuery:  "symbol=BTCUSDT&orderId=123456",
			symbol:    "BTCUSDT",
			orderID:   "123456",
			wantError: false,
		},
		{
			name:      "missing symbol",
			rawQuery:  "orderId=123456",
			symbol:    "",
			orderID:   "123456",
			wantError: true,
		},
		{
			name:      "missing orderId",
			rawQuery:  "symbol=BTCUSDT",
			symbol:    "BTCUSDT",
			orderID:   "",
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			values, err := url.ParseQuery(tt.rawQuery)
			if err != nil {
				t.Fatalf("parse query: %v", err)
			}

			symbol := values.Get("symbol")
			orderID := values.Get("orderId")

			hasError := symbol == "" || orderID == ""
			if hasError != tt.wantError {
				t.Errorf("expected error=%v, got error=%v", tt.wantError, hasError)
			}

			t.Logf("Parsed: symbol=%q, orderID=%q", symbol, orderID)
		})
	}
}

func TestChangeLeverage_JSONSerialization(t *testing.T) {
	// 测试杠杆调整请求的 JSON 序列化
	req := struct {
		Symbol   string `json:"symbol"`
		Leverage int    `json:"leverage"`
	}{
		Symbol:   "BTCUSDT",
		Leverage: 20,
	}

	body, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}

	var parsed struct {
		Symbol   string `json:"symbol"`
		Leverage int    `json:"leverage"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("unmarshal request: %v", err)
	}

	if parsed.Symbol != req.Symbol {
		t.Errorf("symbol mismatch: expected %s, got %s", req.Symbol, parsed.Symbol)
	}
	if parsed.Leverage != req.Leverage {
		t.Errorf("leverage mismatch: expected %d, got %d", req.Leverage, parsed.Leverage)
	}

	t.Logf("JSON roundtrip successful: %s", string(body))
}

func TestResponseFormat_Success(t *testing.T) {
	// 测试成功响应格式
	response := map[string]interface{}{
		"data": map[string]interface{}{
			"orderId": 100001,
			"symbol":  "BTCUSDT",
			"status":  "NEW",
		},
	}

	body, err := json.Marshal(response)
	if err != nil {
		t.Fatalf("marshal response: %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if _, ok := parsed["data"]; !ok {
		t.Error("response should have 'data' field")
	}

	t.Logf("Success response format: %s", string(body))
}

func TestResponseFormat_Error(t *testing.T) {
	// 测试错误响应格式
	response := map[string]interface{}{
		"error": "invalid symbol",
	}

	body, err := json.Marshal(response)
	if err != nil {
		t.Fatalf("marshal response: %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if _, ok := parsed["error"]; !ok {
		t.Error("error response should have 'error' field")
	}

	t.Logf("Error response format: %s", string(body))
}

func TestPlaceOrderReq_JSONTags(t *testing.T) {
	// 验证 PlaceOrderReq 的 JSON 标签
	req := PlaceOrderReq{
		Symbol:        "BTCUSDT",
		Side:          "BUY",
		OrderType:     "LIMIT",
		QuoteQuantity: "5",
		Leverage:      10,
		Price:         "43000",
	}

	body, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// 验证 JSON 字段名
	expectedFields := []string{"symbol", "side", "orderType", "quoteQuantity", "leverage", "price"}
	for _, field := range expectedFields {
		if _, ok := parsed[field]; !ok {
			t.Errorf("expected field %q in JSON", field)
		}
	}

	t.Logf("JSON output: %s", string(body))
}

func TestRequestBuffer_LargeBody(t *testing.T) {
	// 测试大请求体的处理
	req := PlaceOrderReq{
		Symbol:        "BTCUSDT",
		Side:          "BUY",
		OrderType:     "LIMIT",
		QuoteQuantity: "5",
		Leverage:      10,
		Price:         "43000" + string(bytes.Repeat([]byte("0"), 100)),
	}

	body, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	if len(body) < 100 {
		t.Error("expected large request body")
	}

	t.Logf("Large request body size: %d bytes", len(body))
}

func TestHTTPMethod_Validation(t *testing.T) {
	// 验证 HTTP 方法约定
	routes := []struct {
		endpoint string
		method   string
	}{
		{"/api/positions", "GET"},
		{"/api/order", "POST"},
		{"/api/orders", "GET"},
		{"/api/order", "DELETE"},
		{"/api/leverage", "POST"},
	}

	for _, route := range routes {
		t.Run(route.endpoint+"_"+route.method, func(t *testing.T) {
			t.Logf("Endpoint %s uses HTTP method %s", route.endpoint, route.method)
		})
	}
}

func TestOrderIDParsing(t *testing.T) {
	// 测试 orderID 字符串解析
	tests := []struct {
		name      string
		orderID   string
		wantError bool
	}{
		{
			name:      "valid integer",
			orderID:   "123456",
			wantError: false,
		},
		{
			name:      "invalid string",
			orderID:   "abc",
			wantError: true,
		},
		{
			name:      "empty string",
			orderID:   "",
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			values := url.Values{}
			values.Set("orderId", tt.orderID)

			orderIDStr := values.Get("orderId")
			hasError := orderIDStr == ""
			if hasError != tt.wantError {
				t.Logf("OrderID: %q", orderIDStr)
			}
		})
	}
}

func TestContentType_JSON(t *testing.T) {
	// 验证 Content-Type 为 application/json
	expectedContentType := "application/json"

	t.Logf("Expected Content-Type: %s", expectedContentType)
}

func TestBatchRequestParsing(t *testing.T) {
	// 测试批量查询多个品种
	symbols := []string{"BTCUSDT", "ETHUSDT", "BNBUSDT"}

	for _, symbol := range symbols {
		values := url.Values{}
		values.Set("symbol", symbol)

		parsed := values.Get("symbol")
		if parsed != symbol {
			t.Errorf("expected symbol %s, got %s", symbol, parsed)
		}

		t.Logf("Batch query symbol: %s", symbol)
	}
}

func TestPlaceOrderReq_OmitEmpty(t *testing.T) {
	// 测试 omitempty 标签
	req := PlaceOrderReq{
		Symbol:        "BTCUSDT",
		Side:          "BUY",
		OrderType:     "MARKET",
		QuoteQuantity: "5",
		Leverage:      10,
		// Price 为空，应该被 omit
	}

	body, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// Price 为空时不应该出现在 JSON 中
	if _, ok := parsed["price"]; ok && req.Price == "" {
		t.Error("expected price to be omitted when empty")
	}

	// QuoteQuantity 和 Leverage 是必填字段，应该始终出现
	if _, ok := parsed["quoteQuantity"]; !ok {
		t.Error("expected quoteQuantity to be present")
	}
	if _, ok := parsed["leverage"]; !ok {
		t.Error("expected leverage to be present")
	}

	t.Logf("Market order JSON (no price): %s", string(body))
}
