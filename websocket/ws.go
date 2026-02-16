package websocket

import (
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

// SignType 签名类型
type SignType int

const (
	SignTypeHMAC    SignType = iota // HMAC SHA256 签名
	SignTypeEd25519                // Ed25519 签名（ws-fapi session.logon 必需）
)

const (
	WsEndpoint        = "wss://ws-fapi.binance.com/ws-fapi/v1"
	WsTestnetEndpoint = "wss://testnet.binancefuture.com/ws-fapi/v1"

	writeWait  = 10 * time.Second
	pongWait   = 10 * time.Minute
	pingPeriod = 3 * time.Minute
)

// WsRequest WebSocket 请求结构
type WsRequest struct {
	ID     string                 `json:"id"`
	Method string                 `json:"method"`
	Params map[string]interface{} `json:"params"`
}

// WsResponse WebSocket 响应结构
type WsResponse struct {
	ID        string          `json:"id"`
	Status    int             `json:"status"`
	Result    json.RawMessage `json:"result,omitempty"`
	Error     *WsError        `json:"error,omitempty"`
	RateLimits json.RawMessage `json:"rateLimits,omitempty"`
}

type WsError struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
}

func (e *WsError) Error() string {
	return fmt.Sprintf("binance ws error %d: %s", e.Code, e.Msg)
}

// --- 请求/响应结构体 ---

type PlaceOrderParams struct {
	Symbol                  string `json:"symbol"`
	Side                    string `json:"side"`
	Type                    string `json:"type"`
	Quantity                string `json:"quantity,omitempty"`
	Price                   string `json:"price,omitempty"`
	PositionSide            string `json:"positionSide,omitempty"`
	TimeInForce             string `json:"timeInForce,omitempty"`
	StopPrice               string `json:"stopPrice,omitempty"`
	ReduceOnly              string `json:"reduceOnly,omitempty"`
	ClosePosition           string `json:"closePosition,omitempty"`
	NewClientOrderId        string `json:"newClientOrderId,omitempty"`
	WorkingType             string `json:"workingType,omitempty"`
	PriceProtect            string `json:"priceProtect,omitempty"`
	CallbackRate            string `json:"callbackRate,omitempty"`
	ActivationPrice         string `json:"activationPrice,omitempty"`
	SelfTradePreventionMode string `json:"selfTradePreventionMode,omitempty"`
}

type ModifyOrderParams struct {
	Symbol            string `json:"symbol"`
	OrderId           int64  `json:"orderId,omitempty"`
	OrigClientOrderId string `json:"origClientOrderId,omitempty"`
	Side              string `json:"side"`
	Quantity          string `json:"quantity"`
	Price             string `json:"price"`
	PriceMatch        string `json:"priceMatch,omitempty"`
}

type CancelOrderParams struct {
	Symbol            string `json:"symbol"`
	OrderId           int64  `json:"orderId,omitempty"`
	OrigClientOrderId string `json:"origClientOrderId,omitempty"`
}

type QueryOrderParams struct {
	Symbol            string `json:"symbol"`
	OrderId           int64  `json:"orderId,omitempty"`
	OrigClientOrderId string `json:"origClientOrderId,omitempty"`
}

type PositionParams struct {
	Symbol string `json:"symbol,omitempty"`
}

type AlgoOrderParams struct {
	Symbol                  string `json:"symbol"`
	Side                    string `json:"side"`
	Type                    string `json:"type"`
	Quantity                string `json:"quantity,omitempty"`
	Price                   string `json:"price,omitempty"`
	StopPrice               string `json:"stopPrice,omitempty"`
	PositionSide            string `json:"positionSide,omitempty"`
	TimeInForce             string `json:"timeInForce,omitempty"`
	ReduceOnly              string `json:"reduceOnly,omitempty"`
	ClosePosition           string `json:"closePosition,omitempty"`
	WorkingType             string `json:"workingType,omitempty"`
	PriceProtect            string `json:"priceProtect,omitempty"`
	CallbackRate            string `json:"callbackRate,omitempty"`
	ActivationPrice         string `json:"activationPrice,omitempty"`
	SelfTradePreventionMode string `json:"selfTradePreventionMode,omitempty"`
}

type CancelAlgoOrderParams struct {
	AlgoId      int64  `json:"algoId,omitempty"`
	ClientAlgoId string `json:"clientAlgoId,omitempty"`
}

type OrderResult struct {
	OrderId       int64  `json:"orderId"`
	Symbol        string `json:"symbol"`
	Status        string `json:"status"`
	ClientOrderId string `json:"clientOrderId"`
	Price         string `json:"price"`
	AvgPrice      string `json:"avgPrice"`
	OrigQty       string `json:"origQty"`
	ExecutedQty   string `json:"executedQty"`
	Type          string `json:"type"`
	Side          string `json:"side"`
	PositionSide  string `json:"positionSide"`
	TimeInForce   string `json:"timeInForce"`
	StopPrice     string `json:"stopPrice"`
	UpdateTime    int64  `json:"updateTime"`
}

type PositionResult struct {
	Symbol           string `json:"symbol"`
	PositionSide     string `json:"positionSide"`
	PositionAmt      string `json:"positionAmt"`
	EntryPrice       string `json:"entryPrice"`
	BreakEvenPrice   string `json:"breakEvenPrice"`
	MarkPrice        string `json:"markPrice"`
	UnRealizedProfit string `json:"unRealizedProfit"`
	LiquidationPrice string `json:"liquidationPrice"`
	Leverage         string `json:"leverage"`
	InitialMargin    string `json:"initialMargin"`
	MaintMargin      string `json:"maintMargin"`
}

type AlgoOrderResult struct {
	AlgoId       int64  `json:"algoId"`
	ClientAlgoId string `json:"clientAlgoId"`
	Symbol       string `json:"symbol"`
	Side         string `json:"side"`
	Type         string `json:"type"`
	Status       string `json:"status"`
}

// --- Client ---

type WsClient struct {
	apiKey    string
	secretKey string
	endpoint  string
	signType  SignType

	// Ed25519 私钥（仅 SignTypeEd25519 时使用）
	ed25519Key ed25519.PrivateKey

	conn    *websocket.Conn
	mu      sync.Mutex // 保护 conn 写操作
	closed  atomic.Bool

	// 请求-响应关联
	pending   map[string]chan *WsResponse
	pendingMu sync.Mutex

	stopC chan struct{}
	doneC chan struct{}
}

// NewWsClient 创建使用 HMAC SHA256 签名的 WebSocket 客户端（REST API 兼容密钥）
func NewWsClient(apiKey, secretKey string, testnet bool) *WsClient {
	endpoint := WsEndpoint
	if testnet {
		endpoint = WsTestnetEndpoint
	}
	return &WsClient{
		apiKey:    apiKey,
		secretKey: secretKey,
		endpoint:  endpoint,
		signType:  SignTypeHMAC,
		pending:   make(map[string]chan *WsResponse),
		stopC:     make(chan struct{}),
		doneC:     make(chan struct{}),
	}
}

// NewWsClientEd25519 创建使用 Ed25519 签名的 WebSocket 客户端
// apiKey: Ed25519 API Key（在币安创建时选择 Ed25519 类型）
// ed25519PrivKeyPEM: Ed25519 私钥 PEM 格式字符串
func NewWsClientEd25519(apiKey, ed25519PrivKeyPEM string, testnet bool) (*WsClient, error) {
	endpoint := WsEndpoint
	if testnet {
		endpoint = WsTestnetEndpoint
	}

	privKey, err := parseEd25519PrivateKey(ed25519PrivKeyPEM)
	if err != nil {
		return nil, fmt.Errorf("parse ed25519 private key: %w", err)
	}

	return &WsClient{
		apiKey:     apiKey,
		endpoint:   endpoint,
		signType:   SignTypeEd25519,
		ed25519Key: privKey,
		pending:    make(map[string]chan *WsResponse),
		stopC:      make(chan struct{}),
		doneC:      make(chan struct{}),
	}, nil
}

// parseEd25519PrivateKey 从 PEM 格式解析 Ed25519 私钥
func parseEd25519PrivateKey(pemStr string) (ed25519.PrivateKey, error) {
	block, _ := pem.Decode([]byte(pemStr))
	if block == nil {
		return nil, fmt.Errorf("failed to decode PEM block")
	}

	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse PKCS8 key: %w", err)
	}

	ed25519Key, ok := key.(ed25519.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("key is not Ed25519, got %T", key)
	}
	return ed25519Key, nil
}

// Connect 建立 WebSocket 连接并启动读取协程
func (c *WsClient) Connect() error {
	conn, _, err := websocket.DefaultDialer.Dial(c.endpoint, nil)
	if err != nil {
		return fmt.Errorf("ws dial: %w", err)
	}
	c.conn = conn

	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(pongWait))
	})

	go c.readLoop()
	go c.pingLoop()

	return nil
}

// ConnectAndLogon 连接并执行会话认证，后续请求无需逐个签名
func (c *WsClient) ConnectAndLogon() error {
	if err := c.Connect(); err != nil {
		return err
	}
	return c.SessionLogon()
}

// Close 关闭连接
func (c *WsClient) Close() {
	if c.closed.CompareAndSwap(false, true) {
		close(c.stopC)
		c.mu.Lock()
		if c.conn != nil {
			c.conn.WriteMessage(websocket.CloseMessage,
				websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
			c.conn.Close()
		}
		c.mu.Unlock()
		<-c.doneC
	}
}

// --- 核心通信 ---

func (c *WsClient) readLoop() {
	defer close(c.doneC)
	for {
		_, msg, err := c.conn.ReadMessage()
		if err != nil {
			if !c.closed.Load() {
				log.Printf("[ws] read error: %v", err)
			}
			return
		}

		var resp WsResponse
		if err := json.Unmarshal(msg, &resp); err != nil {
			log.Printf("[ws] unmarshal error: %v", err)
			continue
		}

		c.pendingMu.Lock()
		ch, ok := c.pending[resp.ID]
		if ok {
			delete(c.pending, resp.ID)
		}
		c.pendingMu.Unlock()

		if ok {
			ch <- &resp
		}
	}
}

func (c *WsClient) pingLoop() {
	ticker := time.NewTicker(pingPeriod)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			c.mu.Lock()
			err := c.conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(writeWait))
			c.mu.Unlock()
			if err != nil {
				log.Printf("[ws] ping error: %v", err)
				return
			}
		case <-c.stopC:
			return
		}
	}
}

// send 发送请求并等待响应
func (c *WsClient) send(method string, params map[string]interface{}, timeout time.Duration) (*WsResponse, error) {
	id := uuid.New().String()
	req := WsRequest{
		ID:     id,
		Method: method,
		Params: params,
	}

	data, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	ch := make(chan *WsResponse, 1)
	c.pendingMu.Lock()
	c.pending[id] = ch
	c.pendingMu.Unlock()

	c.mu.Lock()
	err = c.conn.WriteMessage(websocket.TextMessage, data)
	c.mu.Unlock()
	if err != nil {
		c.pendingMu.Lock()
		delete(c.pending, id)
		c.pendingMu.Unlock()
		return nil, fmt.Errorf("ws write: %w", err)
	}

	select {
	case resp := <-ch:
		if resp.Error != nil {
			return resp, resp.Error
		}
		return resp, nil
	case <-time.After(timeout):
		c.pendingMu.Lock()
		delete(c.pending, id)
		c.pendingMu.Unlock()
		return nil, fmt.Errorf("request %s timeout after %v", method, timeout)
	case <-c.stopC:
		return nil, fmt.Errorf("client closed")
	}
}

// sendSigned 发送带签名的请求
func (c *WsClient) sendSigned(method string, params map[string]interface{}, timeout time.Duration) (*WsResponse, error) {
	params["apiKey"] = c.apiKey
	params["timestamp"] = time.Now().UnixMilli()
	params["signature"] = c.sign(params)
	return c.send(method, params, timeout)
}

// sign 对参数进行签名（参数按字母序排列）
// 根据 signType 选择 HMAC SHA256 或 Ed25519 签名
func (c *WsClient) sign(params map[string]interface{}) string {
	keys := make([]string, 0, len(params))
	for k := range params {
		if k == "signature" {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var parts []string
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%v", k, params[k]))
	}
	queryString := strings.Join(parts, "&")

	switch c.signType {
	case SignTypeEd25519:
		sig := ed25519.Sign(c.ed25519Key, []byte(queryString))
		return base64.StdEncoding.EncodeToString(sig)
	default: // SignTypeHMAC
		mac := hmac.New(sha256.New, []byte(c.secretKey))
		mac.Write([]byte(queryString))
		return hex.EncodeToString(mac.Sum(nil))
	}
}

// --- 会话认证 ---

// SessionLogon 会话级别认证，认证后后续请求无需逐个签名
func (c *WsClient) SessionLogon() error {
	params := map[string]interface{}{}
	_, err := c.sendSigned("session.logon", params, 10*time.Second)
	return err
}

// SessionLogout 注销会话认证
func (c *WsClient) SessionLogout() error {
	_, err := c.send("session.logout", map[string]interface{}{}, 10*time.Second)
	return err
}

// --- 交易接口 ---

// PlaceOrder 下单 (order.place) 权重: 0
func (c *WsClient) PlaceOrder(p PlaceOrderParams) (*OrderResult, error) {
	params := structToMap(p)
	resp, err := c.sendSigned("order.place", params, 10*time.Second)
	if err != nil {
		return nil, err
	}
	var result OrderResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return nil, fmt.Errorf("unmarshal order result: %w", err)
	}
	return &result, nil
}

// ModifyOrder 修改订单 (order.modify) 权重: 1，仅支持 LIMIT 订单
func (c *WsClient) ModifyOrder(p ModifyOrderParams) (*OrderResult, error) {
	params := structToMap(p)
	resp, err := c.sendSigned("order.modify", params, 10*time.Second)
	if err != nil {
		return nil, err
	}
	var result OrderResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return nil, fmt.Errorf("unmarshal modify result: %w", err)
	}
	return &result, nil
}

// CancelOrder 撤单 (order.cancel) 权重: 1
func (c *WsClient) CancelOrder(p CancelOrderParams) (*OrderResult, error) {
	params := structToMap(p)
	resp, err := c.sendSigned("order.cancel", params, 10*time.Second)
	if err != nil {
		return nil, err
	}
	var result OrderResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return nil, fmt.Errorf("unmarshal cancel result: %w", err)
	}
	return &result, nil
}

// QueryOrder 查询订单 (order.status) 权重: 1
func (c *WsClient) QueryOrder(p QueryOrderParams) (*OrderResult, error) {
	params := structToMap(p)
	resp, err := c.sendSigned("order.status", params, 10*time.Second)
	if err != nil {
		return nil, err
	}
	var result OrderResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return nil, fmt.Errorf("unmarshal query result: %w", err)
	}
	return &result, nil
}

// GetPosition 查询持仓 (v2/account.position) 权重: 5
func (c *WsClient) GetPosition(p PositionParams) ([]PositionResult, error) {
	params := structToMap(p)
	resp, err := c.sendSigned("v2/account.position", params, 10*time.Second)
	if err != nil {
		return nil, err
	}
	var result []PositionResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return nil, fmt.Errorf("unmarshal position result: %w", err)
	}
	return result, nil
}

// PlaceAlgoOrder 条件单下单 (algoOrder.place) 权重: 0
func (c *WsClient) PlaceAlgoOrder(p AlgoOrderParams) (*AlgoOrderResult, error) {
	params := structToMap(p)
	resp, err := c.sendSigned("algoOrder.place", params, 10*time.Second)
	if err != nil {
		return nil, err
	}
	var result AlgoOrderResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return nil, fmt.Errorf("unmarshal algo order result: %w", err)
	}
	return &result, nil
}

// CancelAlgoOrder 条件单撤销 (algoOrder.cancel) 权重: 1
func (c *WsClient) CancelAlgoOrder(p CancelAlgoOrderParams) (*AlgoOrderResult, error) {
	params := structToMap(p)
	resp, err := c.sendSigned("algoOrder.cancel", params, 10*time.Second)
	if err != nil {
		return nil, err
	}
	var result AlgoOrderResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return nil, fmt.Errorf("unmarshal cancel algo result: %w", err)
	}
	return &result, nil
}

// --- 工具函数 ---

// structToMap 将结构体转为 map，跳过零值字段
func structToMap(v interface{}) map[string]interface{} {
	data, _ := json.Marshal(v)
	m := make(map[string]interface{})
	json.Unmarshal(data, &m)

	// 清理零值：删除空字符串和值为 0 的字段
	for k, val := range m {
		switch v := val.(type) {
		case string:
			if v == "" {
				delete(m, k)
			}
		case float64:
			if v == 0 {
				delete(m, k)
			}
		}
	}
	return m
}
