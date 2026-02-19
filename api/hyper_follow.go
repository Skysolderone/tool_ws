package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/adshao/go-binance/v2/futures"
	"github.com/gorilla/websocket"
)

const (
	hyperFollowDefaultSymbol = "BTCUSDT"
	hyperFollowSource        = "hyper_follow"
)

// HyperFollowConfig 服务端跟单配置
type HyperFollowConfig struct {
	Address       string `json:"address"`
	Symbol        string `json:"symbol"`
	QuoteQuantity string `json:"quoteQuantity"`
	Leverage      int    `json:"leverage"`
}

// HyperFollowStatus 服务端跟单状态
type HyperFollowStatus struct {
	Address       string `json:"address"`
	Symbol        string `json:"symbol"`
	QuoteQuantity string `json:"quoteQuantity"`
	Leverage      int    `json:"leverage"`
	Enabled       bool   `json:"enabled"`
	Connected     bool   `json:"connected"`
	ExecutedCount int64  `json:"executedCount"`
	FailedCount   int64  `json:"failedCount"`
	LastError     string `json:"lastError,omitempty"`
	UpdatedAt     int64  `json:"updatedAt"`
}

type hyperFollowManager struct {
	mu    sync.RWMutex
	tasks map[string]*hyperFollowTask // key: address(lower)
}

type hyperFollowTask struct {
	mu sync.RWMutex

	cfg          HyperFollowConfig
	connected    bool
	lastError    string
	executed     int64
	failed       int64
	updatedAt    time.Time
	stopC        chan struct{}
	stopOnce     sync.Once
	seenFillKeys map[string]int64
}

var hyperFollowMgr = &hyperFollowManager{
	tasks: make(map[string]*hyperFollowTask),
}

func normalizeHyperFollowConfig(cfg HyperFollowConfig) (HyperFollowConfig, error) {
	cfg.Address = strings.TrimSpace(cfg.Address)
	if !reAddress.MatchString(cfg.Address) {
		return cfg, fmt.Errorf("address is invalid")
	}
	cfg.Address = strings.ToLower(cfg.Address)

	cfg.Symbol = strings.ToUpper(strings.TrimSpace(cfg.Symbol))
	if cfg.Symbol == "" {
		cfg.Symbol = hyperFollowDefaultSymbol
	}

	cfg.QuoteQuantity = strings.TrimSpace(cfg.QuoteQuantity)
	if cfg.QuoteQuantity == "" {
		return cfg, fmt.Errorf("quoteQuantity is required")
	}
	quoteQty, err := strconv.ParseFloat(cfg.QuoteQuantity, 64)
	if err != nil || quoteQty <= 0 {
		return cfg, fmt.Errorf("quoteQuantity must be > 0")
	}

	if cfg.Leverage <= 0 {
		return cfg, fmt.Errorf("leverage must be > 0")
	}

	return cfg, nil
}

// StartHyperFollow 启动或更新服务端跟单
func StartHyperFollow(cfg HyperFollowConfig) (*HyperFollowStatus, error) {
	normalized, err := normalizeHyperFollowConfig(cfg)
	if err != nil {
		return nil, err
	}

	key := normalized.Address
	hyperFollowMgr.mu.Lock()
	task, ok := hyperFollowMgr.tasks[key]
	if ok {
		task.updateConfig(normalized)
		hyperFollowMgr.mu.Unlock()
		status := task.snapshot()
		return &status, nil
	}

	task = newHyperFollowTask(normalized)
	hyperFollowMgr.tasks[key] = task
	hyperFollowMgr.mu.Unlock()

	go task.run()
	status := task.snapshot()
	return &status, nil
}

// StopHyperFollow 停止服务端跟单
func StopHyperFollow(address string) error {
	addr := strings.ToLower(strings.TrimSpace(address))
	if !reAddress.MatchString(addr) {
		return fmt.Errorf("address is invalid")
	}

	hyperFollowMgr.mu.Lock()
	task, ok := hyperFollowMgr.tasks[addr]
	if ok {
		delete(hyperFollowMgr.tasks, addr)
	}
	hyperFollowMgr.mu.Unlock()

	if !ok {
		return nil
	}

	task.stop()
	return nil
}

// GetHyperFollowStatus 查询服务端跟单状态（address 为空时返回全部）
func GetHyperFollowStatus(address string) any {
	address = strings.ToLower(strings.TrimSpace(address))
	if address != "" {
		hyperFollowMgr.mu.RLock()
		task := hyperFollowMgr.tasks[address]
		hyperFollowMgr.mu.RUnlock()
		if task == nil {
			return nil
		}
		status := task.snapshot()
		return status
	}

	hyperFollowMgr.mu.RLock()
	tasks := make([]*hyperFollowTask, 0, len(hyperFollowMgr.tasks))
	for _, task := range hyperFollowMgr.tasks {
		tasks = append(tasks, task)
	}
	hyperFollowMgr.mu.RUnlock()

	statuses := make([]HyperFollowStatus, 0, len(tasks))
	for _, task := range tasks {
		statuses = append(statuses, task.snapshot())
	}
	return statuses
}

func newHyperFollowTask(cfg HyperFollowConfig) *hyperFollowTask {
	now := time.Now()
	return &hyperFollowTask{
		cfg:          cfg,
		updatedAt:    now,
		stopC:        make(chan struct{}),
		seenFillKeys: make(map[string]int64, 2048),
	}
}

func (t *hyperFollowTask) updateConfig(cfg HyperFollowConfig) {
	t.mu.Lock()
	t.cfg = cfg
	t.updatedAt = time.Now()
	t.mu.Unlock()
	log.Printf("[HyperFollow] Updated config for %s: symbol=%s qty=%s lev=%d", cfg.Address, cfg.Symbol, cfg.QuoteQuantity, cfg.Leverage)
}

func (t *hyperFollowTask) stop() {
	t.stopOnce.Do(func() {
		close(t.stopC)
	})
}

func (t *hyperFollowTask) snapshot() HyperFollowStatus {
	t.mu.RLock()
	defer t.mu.RUnlock()

	return HyperFollowStatus{
		Address:       t.cfg.Address,
		Symbol:        t.cfg.Symbol,
		QuoteQuantity: t.cfg.QuoteQuantity,
		Leverage:      t.cfg.Leverage,
		Enabled:       true,
		Connected:     t.connected,
		ExecutedCount: t.executed,
		FailedCount:   t.failed,
		LastError:     t.lastError,
		UpdatedAt:     t.updatedAt.UnixMilli(),
	}
}

func (t *hyperFollowTask) setConnected(v bool) {
	t.mu.Lock()
	t.connected = v
	t.updatedAt = time.Now()
	t.mu.Unlock()
}

func (t *hyperFollowTask) markError(err error) {
	if err == nil {
		return
	}
	t.mu.Lock()
	t.lastError = err.Error()
	t.updatedAt = time.Now()
	t.mu.Unlock()
}

func (t *hyperFollowTask) markExecuted() {
	t.mu.Lock()
	t.executed++
	t.lastError = ""
	t.updatedAt = time.Now()
	t.mu.Unlock()
}

func (t *hyperFollowTask) markFailed(err error) {
	t.mu.Lock()
	t.failed++
	if err != nil {
		t.lastError = err.Error()
	}
	t.updatedAt = time.Now()
	t.mu.Unlock()
}

func (t *hyperFollowTask) run() {
	cfg := t.snapshot()
	log.Printf("[HyperFollow] Started for %s (symbol=%s)", cfg.Address, cfg.Symbol)
	defer log.Printf("[HyperFollow] Stopped for %s", cfg.Address)

	for {
		select {
		case <-t.stopC:
			t.setConnected(false)
			return
		default:
		}

		t.runOnce()
		select {
		case <-t.stopC:
			t.setConnected(false)
			return
		case <-time.After(hyperReconnectInterval):
		}
	}
}

func (t *hyperFollowTask) runOnce() {
	cfg := t.getConfig()
	upstream, _, err := websocket.DefaultDialer.Dial(hyperWSURL, nil)
	if err != nil {
		t.setConnected(false)
		t.markError(err)
		log.Printf("[HyperFollow] Dial upstream failed for %s: %v", cfg.Address, err)
		return
	}
	defer upstream.Close()

	sub := map[string]any{
		"method": "subscribe",
		"subscription": map[string]any{
			"type":            "userFills",
			"user":            cfg.Address,
			"aggregateByTime": true,
		},
	}
	upstream.SetWriteDeadline(time.Now().Add(5 * time.Second))
	if err := upstream.WriteJSON(sub); err != nil {
		t.setConnected(false)
		t.markError(err)
		log.Printf("[HyperFollow] Subscribe failed for %s: %v", cfg.Address, err)
		return
	}

	t.setConnected(true)

	stopPing := make(chan struct{})
	defer close(stopPing)
	go t.pingLoop(upstream, stopPing)

	for {
		select {
		case <-t.stopC:
			return
		default:
		}

		_, msg, err := upstream.ReadMessage()
		if err != nil {
			t.setConnected(false)
			t.markError(err)
			return
		}
		t.handleUpstreamMessage(msg)
	}
}

func (t *hyperFollowTask) pingLoop(conn *websocket.Conn, stop <-chan struct{}) {
	ticker := time.NewTicker(hyperPingInterval)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.stopC:
			return
		case <-ticker.C:
			conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
			if err := conn.WriteJSON(map[string]any{"method": "ping"}); err != nil {
				conn.Close()
				return
			}
		}
	}
}

func (t *hyperFollowTask) handleUpstreamMessage(raw []byte) {
	var envelope struct {
		Channel string          `json:"channel"`
		Data    json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return
	}
	if envelope.Channel != "userFills" {
		return
	}

	var payload struct {
		IsSnapshot bool                     `json:"isSnapshot"`
		Fills      []map[string]interface{} `json:"fills"`
	}
	if err := json.Unmarshal(envelope.Data, &payload); err == nil && (payload.IsSnapshot || len(payload.Fills) > 0) {
		if payload.IsSnapshot {
			return
		}
		t.handleFills(payload.Fills)
		return
	}

	// 兼容 data 直接是数组
	var fills []map[string]interface{}
	if err := json.Unmarshal(envelope.Data, &fills); err == nil {
		t.handleFills(fills)
	}
}

func (t *hyperFollowTask) handleFills(fills []map[string]interface{}) {
	for _, fill := range fills {
		if !isBTCFill(fill) {
			continue
		}

		fillKey := makeHyperFillKey(fill)
		timeMs := parseAnyInt64(fill["time"])
		if t.markFillSeen(fillKey, timeMs) {
			continue
		}

		action := fillAction(fill)
		side := orderSideFromFill(fill)
		positionSide := positionSideFromFill(fill, action, side)

		switch action {
		case "open":
			if side == "" {
				continue
			}
			t.executeOpen(fill, side, positionSide)
		case "close":
			t.executeClose(fill, positionSide)
		}
	}
}

func (t *hyperFollowTask) executeOpen(fill map[string]interface{}, side, positionSide string) {
	if err := CheckRisk(); err != nil {
		t.markFailed(err)
		SaveFailedOperation("HYPER_FOLLOW_OPEN", hyperFollowSource, t.getConfig().Symbol, fill, 0, err)
		return
	}

	cfg := t.getConfig()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	req := PlaceOrderReq{
		Source:        hyperFollowSource,
		Symbol:        cfg.Symbol,
		Side:          futures.SideType(side),
		OrderType:     futures.OrderTypeMarket,
		QuoteQuantity: cfg.QuoteQuantity,
		Leverage:      cfg.Leverage,
		PositionSide:  futures.PositionSideType(positionSide),
	}

	result, err := PlaceOrderViaWs(ctx, req)
	if err != nil {
		t.markFailed(err)
		log.Printf("[HyperFollow] Open failed for %s: %v", cfg.Address, err)
		return
	}

	t.markExecuted()
	orderID := int64(0)
	if result != nil && result.Order != nil {
		orderID = result.Order.OrderID
	}
	log.Printf("[HyperFollow] Open executed for %s: %s %s %s (%sU %dx), orderId=%d",
		cfg.Address,
		cfg.Symbol,
		positionSide,
		side,
		cfg.QuoteQuantity,
		cfg.Leverage,
		orderID,
	)
}

func (t *hyperFollowTask) executeClose(fill map[string]interface{}, positionSide string) {
	cfg := t.getConfig()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	_, err := ClosePositionViaWs(ctx, ClosePositionReq{
		Symbol:       cfg.Symbol,
		PositionSide: futures.PositionSideType(positionSide),
	})
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "no open position") {
			return
		}
		t.markFailed(err)
		SaveFailedOperation("HYPER_FOLLOW_CLOSE", hyperFollowSource, cfg.Symbol, fill, 0, err)
		log.Printf("[HyperFollow] Close failed for %s: %v", cfg.Address, err)
		return
	}

	t.markExecuted()
	log.Printf("[HyperFollow] Close executed for %s: %s %s", cfg.Address, cfg.Symbol, positionSide)
}

func (t *hyperFollowTask) getConfig() HyperFollowConfig {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.cfg
}

func (t *hyperFollowTask) markFillSeen(fillKey string, ts int64) bool {
	if fillKey == "" {
		return false
	}
	if ts <= 0 {
		ts = time.Now().UnixMilli()
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	if _, exists := t.seenFillKeys[fillKey]; exists {
		return true
	}
	t.seenFillKeys[fillKey] = ts

	// 控制内存占用：超过阈值时清理 24h 之前的 key
	if len(t.seenFillKeys) > 3000 {
		cutoff := time.Now().Add(-24 * time.Hour).UnixMilli()
		for k, tms := range t.seenFillKeys {
			if tms < cutoff {
				delete(t.seenFillKeys, k)
			}
		}
		if len(t.seenFillKeys) > 6000 {
			// 再做一次保底清理：随机删除最旧的一批
			for k := range t.seenFillKeys {
				delete(t.seenFillKeys, k)
				if len(t.seenFillKeys) <= 3000 {
					break
				}
			}
		}
	}

	return false
}

func isBTCFill(fill map[string]interface{}) bool {
	coin := strings.ToUpper(strings.TrimSpace(parseAnyString(fill["coin"])))
	return strings.HasPrefix(coin, "BTC")
}

func fillAction(fill map[string]interface{}) string {
	dir := strings.ToLower(parseAnyString(fill["dir"]))
	if strings.Contains(dir, "open") {
		return "open"
	}
	if strings.Contains(dir, "close") {
		return "close"
	}
	if parseAnyFloat(fill["closedPnl"]) != 0 {
		return "close"
	}
	return ""
}

func orderSideFromFill(fill map[string]interface{}) string {
	side := strings.ToUpper(strings.TrimSpace(parseAnyString(fill["side"])))
	switch side {
	case "B", "BUY":
		return "BUY"
	case "A", "SELL":
		return "SELL"
	default:
		return ""
	}
}

func positionSideFromFill(fill map[string]interface{}, action, side string) string {
	dir := strings.ToLower(parseAnyString(fill["dir"]))
	if strings.Contains(dir, "long") {
		return "LONG"
	}
	if strings.Contains(dir, "short") {
		return "SHORT"
	}

	switch action {
	case "open":
		if side == "BUY" {
			return "LONG"
		}
		if side == "SELL" {
			return "SHORT"
		}
	case "close":
		if side == "BUY" {
			return "SHORT"
		}
		if side == "SELL" {
			return "LONG"
		}
	}
	return "BOTH"
}

func makeHyperFillKey(fill map[string]interface{}) string {
	tid := parseAnyString(fill["tid"])
	hash := parseAnyString(fill["hash"])
	timeVal := parseAnyString(fill["time"])
	coin := parseAnyString(fill["coin"])
	side := parseAnyString(fill["side"])

	if tid == "" && hash == "" && timeVal == "" {
		return ""
	}
	return fmt.Sprintf("%s::%s::%s::%s::%s", tid, hash, timeVal, coin, side)
}

func parseAnyString(v interface{}) string {
	switch val := v.(type) {
	case string:
		return val
	case json.Number:
		return val.String()
	case float64:
		return strconv.FormatInt(int64(val), 10)
	case float32:
		return strconv.FormatInt(int64(val), 10)
	case int:
		return strconv.Itoa(val)
	case int64:
		return strconv.FormatInt(val, 10)
	case int32:
		return strconv.FormatInt(int64(val), 10)
	default:
		if val == nil {
			return ""
		}
		return fmt.Sprintf("%v", val)
	}
}

func parseAnyFloat(v interface{}) float64 {
	s := parseAnyString(v)
	if s == "" {
		return 0
	}
	f, _ := strconv.ParseFloat(s, 64)
	return f
}

func parseAnyInt64(v interface{}) int64 {
	s := parseAnyString(v)
	if s == "" {
		return 0
	}
	i, _ := strconv.ParseInt(s, 10, 64)
	return i
}
