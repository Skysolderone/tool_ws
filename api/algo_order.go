package api

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// AlgoOrderResponse 币安 Algo Order API 响应
type AlgoOrderResponse struct {
	AlgoID        int64  `json:"algoId"`
	ClientAlgoID  string `json:"clientAlgoId"`
	AlgoType      string `json:"algoType"`
	OrderType     string `json:"orderType"`
	Symbol        string `json:"symbol"`
	Side          string `json:"side"`
	PositionSide  string `json:"positionSide"`
	Quantity      string `json:"quantity"`
	AlgoStatus    string `json:"algoStatus"`
	TriggerPrice  string `json:"triggerPrice"`
	Price         string `json:"price"`
	ClosePosition bool   `json:"closePosition"`
	PriceProtect  bool   `json:"priceProtect"`
	ReduceOnly    bool   `json:"reduceOnly"`
	WorkingType   string `json:"workingType"`
	CreateTime    int64  `json:"createTime"`
	UpdateTime    int64  `json:"updateTime"`
}

// AlgoOrderParams 下 Algo 条件单的参数
type AlgoOrderParams struct {
	Symbol        string
	Side          string // BUY / SELL
	OrderType     string // STOP_MARKET / TAKE_PROFIT_MARKET / STOP / TAKE_PROFIT
	TriggerPrice  string // 触发价格（即原来的 stopPrice）
	Quantity      string // 数量（与 closePosition 二选一）
	ClosePosition bool   // 是否触发后全部平仓
	PositionSide  string // BOTH / LONG / SHORT
	WorkingType   string // MARK_PRICE / CONTRACT_PRICE
	PriceProtect  bool   // 价格保护
}

// PlaceAlgoOrder 通过 POST /fapi/v1/algoOrder 下条件单
// 自 2025-12-09 起，币安要求 STOP_MARKET / TAKE_PROFIT_MARKET 等条件单必须使用此接口
func PlaceAlgoOrder(ctx context.Context, params AlgoOrderParams) (*AlgoOrderResponse, error) {
	// 构建请求参数
	values := url.Values{}
	values.Set("algoType", "CONDITIONAL")
	values.Set("symbol", params.Symbol)
	values.Set("side", params.Side)
	values.Set("type", params.OrderType)
	values.Set("triggerPrice", params.TriggerPrice)

	if params.ClosePosition {
		values.Set("closePosition", "true")
	} else if params.Quantity != "" {
		values.Set("quantity", params.Quantity)
	}

	if params.PositionSide != "" {
		values.Set("positionSide", params.PositionSide)
	}
	if params.WorkingType != "" {
		values.Set("workingType", params.WorkingType)
	}
	if params.PriceProtect {
		values.Set("priceProtect", "true")
	}

	// 添加时间戳
	values.Set("timestamp", strconv.FormatInt(time.Now().UnixMilli(), 10))

	// 签名
	signature := signQuery(values.Encode(), Cfg.REST.SecretKey)
	values.Set("signature", signature)

	// 构建请求 URL
	baseURL := "https://fapi.binance.com"
	if Cfg.Testnet {
		baseURL = "https://testnet.binancefuture.com"
	}
	reqURL := fmt.Sprintf("%s/fapi/v1/algoOrder?%s", baseURL, values.Encode())

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("X-MBX-APIKEY", Cfg.REST.APIKey)

	// 发送请求
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	// 检查是否有错误
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("algo order API error (status %d): %s", resp.StatusCode, string(body))
	}

	// 检查币安错误
	var errResp struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	}
	if err := json.Unmarshal(body, &errResp); err == nil && errResp.Code < 0 {
		return nil, fmt.Errorf("binance algo error %d: %s", errResp.Code, errResp.Msg)
	}

	var result AlgoOrderResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("parse response: %w (body: %s)", err, string(body))
	}

	log.Printf("[AlgoOrder] Placed %s order: algoId=%d, symbol=%s, side=%s, triggerPrice=%s, closePosition=%v",
		params.OrderType, result.AlgoID, result.Symbol, result.Side, result.TriggerPrice, result.ClosePosition)

	return &result, nil
}

// CancelAlgoOrder 撤销 Algo 条件单
func CancelAlgoOrder(ctx context.Context, symbol string, algoID int64) error {
	values := url.Values{}
	values.Set("symbol", symbol)
	values.Set("algoId", strconv.FormatInt(algoID, 10))
	values.Set("timestamp", strconv.FormatInt(time.Now().UnixMilli(), 10))

	signature := signQuery(values.Encode(), Cfg.REST.SecretKey)
	values.Set("signature", signature)

	baseURL := "https://fapi.binance.com"
	if Cfg.Testnet {
		baseURL = "https://testnet.binancefuture.com"
	}
	reqURL := fmt.Sprintf("%s/fapi/v1/algoOrder?%s", baseURL, values.Encode())

	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, reqURL, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("X-MBX-APIKEY", Cfg.REST.APIKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("cancel algo order API error (status %d): %s", resp.StatusCode, string(body))
	}

	log.Printf("[AlgoOrder] Cancelled algo order: algoId=%d, symbol=%s", algoID, symbol)
	return nil
}

// signQuery HMAC-SHA256 签名
func signQuery(queryString, secretKey string) string {
	h := hmac.New(sha256.New, []byte(secretKey))
	h.Write([]byte(queryString))
	return hex.EncodeToString(h.Sum(nil))
}
