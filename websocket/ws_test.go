package websocket

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// --- mock server ---

var upgrader = websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}

// mockServer 启动一个本地 WebSocket 服务，handler 收到请求后自定义响应
type mockHandler func(req WsRequest) WsResponse

func newMockServer(t *testing.T, h mockHandler) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Fatalf("upgrade: %v", err)
		}
		defer conn.Close()

		for {
			_, msg, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var req WsRequest
			if err := json.Unmarshal(msg, &req); err != nil {
				return
			}
			resp := h(req)
			resp.ID = req.ID
			data, _ := json.Marshal(resp)
			if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
				return
			}
		}
	}))
	return srv
}

// newTestClient 创建一个连接到 mock server 的 WsClient
func newTestClient(t *testing.T, srv *httptest.Server) *WsClient {
	t.Helper()
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	c := &WsClient{
		apiKey:    "testApiKey",
		secretKey: "testSecretKey",
		endpoint:  wsURL,
		pending:   make(map[string]chan *WsResponse),
		stopC:     make(chan struct{}),
		doneC:     make(chan struct{}),
	}
	if err := c.Connect(); err != nil {
		t.Fatalf("connect: %v", err)
	}
	return c
}

// --- 单元测试: structToMap ---

func TestStructToMap_Basic(t *testing.T) {
	p := PlaceOrderParams{
		Symbol:   "BTCUSDT",
		Side:     "BUY",
		Type:     "LIMIT",
		Quantity: "0.1",
		Price:    "43000",
	}
	m := structToMap(p)

	if m["symbol"] != "BTCUSDT" {
		t.Errorf("expected symbol=BTCUSDT, got %v", m["symbol"])
	}
	if m["side"] != "BUY" {
		t.Errorf("expected side=BUY, got %v", m["side"])
	}
	if m["quantity"] != "0.1" {
		t.Errorf("expected quantity=0.1, got %v", m["quantity"])
	}
}

func TestStructToMap_OmitsEmptyFields(t *testing.T) {
	p := PlaceOrderParams{
		Symbol:   "BTCUSDT",
		Side:     "BUY",
		Type:     "MARKET",
		Quantity: "1",
	}
	m := structToMap(p)

	if _, ok := m["price"]; ok {
		t.Error("expected price to be omitted for empty value")
	}
	if _, ok := m["stopPrice"]; ok {
		t.Error("expected stopPrice to be omitted for empty value")
	}
	if _, ok := m["timeInForce"]; ok {
		t.Error("expected timeInForce to be omitted for empty value")
	}
}

func TestStructToMap_OmitsZeroInt(t *testing.T) {
	p := CancelOrderParams{
		Symbol:  "BTCUSDT",
		OrderId: 0, // 零值应被过滤
	}
	m := structToMap(p)

	if _, ok := m["orderId"]; ok {
		t.Error("expected orderId=0 to be omitted")
	}
	if m["symbol"] != "BTCUSDT" {
		t.Errorf("expected symbol=BTCUSDT, got %v", m["symbol"])
	}
}

func TestStructToMap_KeepsNonZeroInt(t *testing.T) {
	p := CancelOrderParams{
		Symbol:  "BTCUSDT",
		OrderId: 12345,
	}
	m := structToMap(p)

	if m["orderId"] == nil {
		t.Error("expected orderId to be present")
	}
}

// --- 单元测试: sign ---

func TestSign_Deterministic(t *testing.T) {
	c := &WsClient{secretKey: "mySecret"}

	params := map[string]interface{}{
		"symbol":    "BTCUSDT",
		"side":      "BUY",
		"timestamp": int64(1700000000000),
	}

	sig1 := c.sign(params)
	sig2 := c.sign(params)

	if sig1 != sig2 {
		t.Errorf("sign should be deterministic, got %s vs %s", sig1, sig2)
	}
}

func TestSign_AlphabeticalOrder(t *testing.T) {
	c := &WsClient{secretKey: "secret123"}

	params := map[string]interface{}{
		"symbol":    "ETHUSDT",
		"side":      "SELL",
		"apiKey":    "key1",
		"timestamp": int64(1700000000000),
	}

	// 手动按字母序构造签名
	queryString := "apiKey=key1&side=SELL&symbol=ETHUSDT&timestamp=1700000000000"
	mac := hmac.New(sha256.New, []byte("secret123"))
	mac.Write([]byte(queryString))
	expected := hex.EncodeToString(mac.Sum(nil))

	got := c.sign(params)
	if got != expected {
		t.Errorf("sign mismatch:\n  expected: %s\n  got:      %s", expected, got)
	}
}

func TestSign_SkipsSignatureField(t *testing.T) {
	c := &WsClient{secretKey: "secret"}

	params := map[string]interface{}{
		"symbol":    "BTCUSDT",
		"signature": "should_be_ignored",
	}

	// 签名时应忽略 signature 字段
	queryString := "symbol=BTCUSDT"
	mac := hmac.New(sha256.New, []byte("secret"))
	mac.Write([]byte(queryString))
	expected := hex.EncodeToString(mac.Sum(nil))

	got := c.sign(params)
	if got != expected {
		t.Errorf("sign should skip signature field, expected %s, got %s", expected, got)
	}
}

// --- 单元测试: WsError ---

func TestWsError_Format(t *testing.T) {
	e := &WsError{Code: -1102, Msg: "Mandatory parameter 'symbol' was not sent"}
	s := e.Error()
	if !strings.Contains(s, "-1102") || !strings.Contains(s, "symbol") {
		t.Errorf("unexpected error string: %s", s)
	}
}

// --- 单元测试: NewWsClient ---

func TestNewWsClient_Production(t *testing.T) {
	c := NewWsClient("ak", "sk", false)
	if c.endpoint != WsEndpoint {
		t.Errorf("expected production endpoint, got %s", c.endpoint)
	}
	if c.apiKey != "ak" || c.secretKey != "sk" {
		t.Error("apiKey/secretKey not set correctly")
	}
}

func TestNewWsClient_Testnet(t *testing.T) {
	c := NewWsClient("ak", "sk", true)
	if c.endpoint != WsTestnetEndpoint {
		t.Errorf("expected testnet endpoint, got %s", c.endpoint)
	}
}

// --- 集成测试: mock server + 交易接口 ---

func TestSessionLogon(t *testing.T) {
	srv := newMockServer(t, func(req WsRequest) WsResponse {
		if req.Method != "session.logon" {
			t.Errorf("expected method session.logon, got %s", req.Method)
		}
		if req.Params["apiKey"] == nil {
			t.Error("expected apiKey in params")
		}
		if req.Params["signature"] == nil {
			t.Error("expected signature in params")
		}
		return WsResponse{Status: 200, Result: json.RawMessage(`{"apiKey":"testApiKey"}`)}
	})
	defer srv.Close()

	c := newTestClient(t, srv)
	defer c.Close()

	if err := c.SessionLogon(); err != nil {
		t.Fatalf("SessionLogon: %v", err)
	}
}

func TestPlaceOrder(t *testing.T) {
	srv := newMockServer(t, func(req WsRequest) WsResponse {
		if req.Method != "order.place" {
			t.Errorf("expected method order.place, got %s", req.Method)
		}
		if req.Params["symbol"] != "BTCUSDT" {
			t.Errorf("expected symbol BTCUSDT, got %v", req.Params["symbol"])
		}
		return WsResponse{
			Status: 200,
			Result: json.RawMessage(`{
				"orderId": 100001,
				"symbol": "BTCUSDT",
				"status": "NEW",
				"clientOrderId": "test123",
				"price": "43000",
				"avgPrice": "0",
				"origQty": "0.1",
				"executedQty": "0",
				"type": "LIMIT",
				"side": "BUY",
				"positionSide": "BOTH",
				"timeInForce": "GTC"
			}`),
		}
	})
	defer srv.Close()

	c := newTestClient(t, srv)
	defer c.Close()

	result, err := c.PlaceOrder(PlaceOrderParams{
		Symbol:      "BTCUSDT",
		Side:        "BUY",
		Type:        "LIMIT",
		Quantity:    "0.1",
		Price:       "43000",
		TimeInForce: "GTC",
	})
	if err != nil {
		t.Fatalf("PlaceOrder: %v", err)
	}
	if result.OrderId != 100001 {
		t.Errorf("expected orderId=100001, got %d", result.OrderId)
	}
	if result.Status != "NEW" {
		t.Errorf("expected status=NEW, got %s", result.Status)
	}
	if result.Symbol != "BTCUSDT" {
		t.Errorf("expected symbol=BTCUSDT, got %s", result.Symbol)
	}
}

func TestModifyOrder(t *testing.T) {
	srv := newMockServer(t, func(req WsRequest) WsResponse {
		if req.Method != "order.modify" {
			t.Errorf("expected method order.modify, got %s", req.Method)
		}
		return WsResponse{
			Status: 200,
			Result: json.RawMessage(`{
				"orderId": 200001,
				"symbol": "BTCUSDT",
				"status": "NEW",
				"price": "44000",
				"origQty": "0.2",
				"side": "SELL"
			}`),
		}
	})
	defer srv.Close()

	c := newTestClient(t, srv)
	defer c.Close()

	result, err := c.ModifyOrder(ModifyOrderParams{
		Symbol:   "BTCUSDT",
		OrderId:  200001,
		Side:     "SELL",
		Quantity: "0.2",
		Price:    "44000",
	})
	if err != nil {
		t.Fatalf("ModifyOrder: %v", err)
	}
	if result.Price != "44000" {
		t.Errorf("expected price=44000, got %s", result.Price)
	}
}

func TestCancelOrder(t *testing.T) {
	srv := newMockServer(t, func(req WsRequest) WsResponse {
		if req.Method != "order.cancel" {
			t.Errorf("expected method order.cancel, got %s", req.Method)
		}
		return WsResponse{
			Status: 200,
			Result: json.RawMessage(`{
				"orderId": 300001,
				"symbol": "BTCUSDT",
				"status": "CANCELED",
				"side": "BUY",
				"origQty": "1",
				"executedQty": "0"
			}`),
		}
	})
	defer srv.Close()

	c := newTestClient(t, srv)
	defer c.Close()

	result, err := c.CancelOrder(CancelOrderParams{
		Symbol:  "BTCUSDT",
		OrderId: 300001,
	})
	if err != nil {
		t.Fatalf("CancelOrder: %v", err)
	}
	if result.Status != "CANCELED" {
		t.Errorf("expected status=CANCELED, got %s", result.Status)
	}
}

func TestQueryOrder(t *testing.T) {
	srv := newMockServer(t, func(req WsRequest) WsResponse {
		if req.Method != "order.status" {
			t.Errorf("expected method order.status, got %s", req.Method)
		}
		return WsResponse{
			Status: 200,
			Result: json.RawMessage(`{
				"orderId": 400001,
				"symbol": "ETHUSDT",
				"status": "FILLED",
				"side": "BUY",
				"type": "MARKET",
				"executedQty": "10",
				"avgPrice": "2300.50"
			}`),
		}
	})
	defer srv.Close()

	c := newTestClient(t, srv)
	defer c.Close()

	result, err := c.QueryOrder(QueryOrderParams{
		Symbol:  "ETHUSDT",
		OrderId: 400001,
	})
	if err != nil {
		t.Fatalf("QueryOrder: %v", err)
	}
	if result.Status != "FILLED" {
		t.Errorf("expected status=FILLED, got %s", result.Status)
	}
	if result.AvgPrice != "2300.50" {
		t.Errorf("expected avgPrice=2300.50, got %s", result.AvgPrice)
	}
}

func TestGetPosition(t *testing.T) {
	srv := newMockServer(t, func(req WsRequest) WsResponse {
		if req.Method != "v2/account.position" {
			t.Errorf("expected method v2/account.position, got %s", req.Method)
		}
		return WsResponse{
			Status: 200,
			Result: json.RawMessage(`[
				{
					"symbol": "BTCUSDT",
					"positionSide": "LONG",
					"positionAmt": "0.5",
					"entryPrice": "42000",
					"markPrice": "43500",
					"unRealizedProfit": "750",
					"liquidationPrice": "35000",
					"leverage": "10",
					"initialMargin": "2100",
					"maintMargin": "210"
				}
			]`),
		}
	})
	defer srv.Close()

	c := newTestClient(t, srv)
	defer c.Close()

	results, err := c.GetPosition(PositionParams{Symbol: "BTCUSDT"})
	if err != nil {
		t.Fatalf("GetPosition: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 position, got %d", len(results))
	}
	if results[0].PositionAmt != "0.5" {
		t.Errorf("expected positionAmt=0.5, got %s", results[0].PositionAmt)
	}
	if results[0].Leverage != "10" {
		t.Errorf("expected leverage=10, got %s", results[0].Leverage)
	}
}

func TestPlaceAlgoOrder(t *testing.T) {
	srv := newMockServer(t, func(req WsRequest) WsResponse {
		if req.Method != "algoOrder.place" {
			t.Errorf("expected method algoOrder.place, got %s", req.Method)
		}
		return WsResponse{
			Status: 200,
			Result: json.RawMessage(`{
				"algoId": 500001,
				"clientAlgoId": "algo_test",
				"symbol": "BTCUSDT",
				"side": "SELL",
				"type": "STOP_MARKET",
				"status": "NEW"
			}`),
		}
	})
	defer srv.Close()

	c := newTestClient(t, srv)
	defer c.Close()

	result, err := c.PlaceAlgoOrder(AlgoOrderParams{
		Symbol:    "BTCUSDT",
		Side:      "SELL",
		Type:      "STOP_MARKET",
		Quantity:  "0.1",
		StopPrice: "40000",
	})
	if err != nil {
		t.Fatalf("PlaceAlgoOrder: %v", err)
	}
	if result.AlgoId != 500001 {
		t.Errorf("expected algoId=500001, got %d", result.AlgoId)
	}
	if result.Type != "STOP_MARKET" {
		t.Errorf("expected type=STOP_MARKET, got %s", result.Type)
	}
}

func TestCancelAlgoOrder(t *testing.T) {
	srv := newMockServer(t, func(req WsRequest) WsResponse {
		if req.Method != "algoOrder.cancel" {
			t.Errorf("expected method algoOrder.cancel, got %s", req.Method)
		}
		return WsResponse{
			Status: 200,
			Result: json.RawMessage(`{
				"algoId": 600001,
				"clientAlgoId": "algo_cancel",
				"status": "CANCELED"
			}`),
		}
	})
	defer srv.Close()

	c := newTestClient(t, srv)
	defer c.Close()

	result, err := c.CancelAlgoOrder(CancelAlgoOrderParams{AlgoId: 600001})
	if err != nil {
		t.Fatalf("CancelAlgoOrder: %v", err)
	}
	if result.Status != "CANCELED" {
		t.Errorf("expected status=CANCELED, got %s", result.Status)
	}
}

// --- 异常场景测试 ---

func TestPlaceOrder_ServerError(t *testing.T) {
	srv := newMockServer(t, func(req WsRequest) WsResponse {
		return WsResponse{
			Status: 400,
			Error:  &WsError{Code: -1102, Msg: "Mandatory parameter 'quantity' was not sent"},
		}
	})
	defer srv.Close()

	c := newTestClient(t, srv)
	defer c.Close()

	_, err := c.PlaceOrder(PlaceOrderParams{
		Symbol: "BTCUSDT",
		Side:   "BUY",
		Type:   "LIMIT",
	})
	if err == nil {
		t.Fatal("expected error for missing parameter")
	}
	if !strings.Contains(err.Error(), "-1102") {
		t.Errorf("expected error code -1102, got: %v", err)
	}
}

func TestSendTimeout(t *testing.T) {
	// mock server 不回复，触发超时
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		// 只读不回，制造超时
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}))
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	c := &WsClient{
		apiKey:    "ak",
		secretKey: "sk",
		endpoint:  wsURL,
		pending:   make(map[string]chan *WsResponse),
		stopC:     make(chan struct{}),
		doneC:     make(chan struct{}),
	}
	if err := c.Connect(); err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer c.Close()

	_, err := c.send("test.method", map[string]interface{}{}, 200*time.Millisecond)
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if !strings.Contains(err.Error(), "timeout") {
		t.Errorf("expected timeout in error, got: %v", err)
	}
}

func TestClose_Idempotent(t *testing.T) {
	srv := newMockServer(t, func(req WsRequest) WsResponse {
		return WsResponse{Status: 200, Result: json.RawMessage(`{}`)}
	})
	defer srv.Close()

	c := newTestClient(t, srv)

	// 多次 Close 不应 panic
	c.Close()
	c.Close()
	c.Close()
}

func TestSend_AfterClose(t *testing.T) {
	srv := newMockServer(t, func(req WsRequest) WsResponse {
		return WsResponse{Status: 200, Result: json.RawMessage(`{}`)}
	})
	defer srv.Close()

	c := newTestClient(t, srv)
	c.Close()

	_, err := c.send("test", map[string]interface{}{}, time.Second)
	if err == nil {
		t.Fatal("expected error after close")
	}
}

// --- 并发测试 ---

func TestConcurrentRequests(t *testing.T) {
	srv := newMockServer(t, func(req WsRequest) WsResponse {
		return WsResponse{
			Status: 200,
			Result: json.RawMessage(`{
				"orderId": 999,
				"symbol": "BTCUSDT",
				"status": "NEW",
				"side": "BUY"
			}`),
		}
	})
	defer srv.Close()

	c := newTestClient(t, srv)
	defer c.Close()

	var wg sync.WaitGroup
	errs := make(chan error, 20)

	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := c.PlaceOrder(PlaceOrderParams{
				Symbol:   "BTCUSDT",
				Side:     "BUY",
				Type:     "MARKET",
				Quantity: "0.01",
			})
			if err != nil {
				errs <- err
			}
		}()
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		t.Errorf("concurrent request failed: %v", err)
	}
}

// --- 验证请求参数传递 ---

func TestPlaceOrder_ParamsPassed(t *testing.T) {
	var receivedParams map[string]interface{}
	srv := newMockServer(t, func(req WsRequest) WsResponse {
		receivedParams = req.Params
		return WsResponse{
			Status: 200,
			Result: json.RawMessage(`{"orderId":1,"symbol":"BTCUSDT","status":"NEW","side":"BUY"}`),
		}
	})
	defer srv.Close()

	c := newTestClient(t, srv)
	defer c.Close()

	_, _ = c.PlaceOrder(PlaceOrderParams{
		Symbol:       "BTCUSDT",
		Side:         "BUY",
		Type:         "LIMIT",
		Quantity:     "0.5",
		Price:        "43000",
		PositionSide: "LONG",
		TimeInForce:  "GTC",
	})

	// 验证业务参数正确传递
	if receivedParams["symbol"] != "BTCUSDT" {
		t.Errorf("symbol: expected BTCUSDT, got %v", receivedParams["symbol"])
	}
	if receivedParams["side"] != "BUY" {
		t.Errorf("side: expected BUY, got %v", receivedParams["side"])
	}
	if receivedParams["positionSide"] != "LONG" {
		t.Errorf("positionSide: expected LONG, got %v", receivedParams["positionSide"])
	}
	// 验证签名相关字段存在
	if receivedParams["apiKey"] == nil {
		t.Error("expected apiKey in params")
	}
	if receivedParams["timestamp"] == nil {
		t.Error("expected timestamp in params")
	}
	if receivedParams["signature"] == nil {
		t.Error("expected signature in params")
	}
	// 验证零值字段未传递
	if receivedParams["stopPrice"] != nil {
		t.Error("expected stopPrice to be absent")
	}
	if receivedParams["reduceOnly"] != nil {
		t.Error("expected reduceOnly to be absent")
	}
}
