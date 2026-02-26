package api

import (
	"encoding/json"
	"log"
	"math"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const (
	binanceLiquidationWSURL       = "wss://fstream.binance.com/ws/!forceOrder@arr"
	liquidationReconnectBaseDelay = 2 * time.Second
	liquidationBroadcastInterval  = 1 * time.Second
	liquidationRetentionDays      = 30
	liquidationHistoryDays        = 7
	liquidationHistoryH4          = 12
	liquidationHistoryH1          = 24
)

type binanceForceOrderEvent struct {
	EventType string `json:"e"`
	EventTime int64  `json:"E"`
	Order     struct {
		Symbol               string `json:"s"`
		Side                 string `json:"S"`
		Price                string `json:"p"`
		AvgPrice             string `json:"ap"`
		OrigQuantity         string `json:"q"`
		LastFilledQuantity   string `json:"l"`
		FilledAccumulatedQty string `json:"z"`
		TradeTime            int64  `json:"T"`
	} `json:"o"`
}

type liquidationBucketStat struct {
	StartTime     int64   `json:"startTime"`
	EndTime       int64   `json:"endTime"`
	TotalCount    int64   `json:"totalCount"`
	BuyCount      int64   `json:"buyCount"`
	SellCount     int64   `json:"sellCount"`
	TotalNotional float64 `json:"totalNotional"`
	BuyNotional   float64 `json:"buyNotional"`
	SellNotional  float64 `json:"sellNotional"`
}

type symbolNotional struct {
	Symbol   string  `json:"symbol"`
	Notional float64 `json:"notional"`
	Count    int64   `json:"count"`
}

type liquidationStatsPayload struct {
	Channel       string `json:"channel"`
	Time          int64  `json:"t"`
	Timezone      string `json:"timezone"`
	StartedAt     int64  `json:"startedAt"`
	LastEventTime int64  `json:"lastEventTime"`
	EventCount    uint64 `json:"eventCount"`
	Stats         struct {
		Daily []liquidationBucketStat `json:"daily"`
		H4    []liquidationBucketStat `json:"h4"`
		H1    []liquidationBucketStat `json:"h1"`
	} `json:"stats"`
	TopSymbols struct {
		H1  []symbolNotional `json:"h1"`
		H4  []symbolNotional `json:"h4"`
		Day []symbolNotional `json:"day"`
	} `json:"topSymbols"`
}

// symbolBucket 按币种追踪每个时间桶内的爆仓
type symbolBucket struct {
	data map[string]*symbolNotional // symbol -> notional+count
}

func newSymbolBucket() *symbolBucket {
	return &symbolBucket{data: make(map[string]*symbolNotional)}
}

func (sb *symbolBucket) add(symbol string, notional float64) {
	if sb.data == nil {
		sb.data = make(map[string]*symbolNotional)
	}
	if sn, ok := sb.data[symbol]; ok {
		sn.Notional += notional
		sn.Count++
	} else {
		sb.data[symbol] = &symbolNotional{Symbol: symbol, Notional: notional, Count: 1}
	}
}

func (sb *symbolBucket) topN(n int) []symbolNotional {
	if sb == nil || len(sb.data) == 0 {
		return nil
	}
	list := make([]symbolNotional, 0, len(sb.data))
	for _, v := range sb.data {
		list = append(list, *v)
	}
	// 按 notional 降序排序
	for i := 0; i < len(list); i++ {
		for j := i + 1; j < len(list); j++ {
			if list[j].Notional > list[i].Notional {
				list[i], list[j] = list[j], list[i]
			}
		}
	}
	if len(list) > n {
		list = list[:n]
	}
	return list
}

type liquidationStatsStore struct {
	mu            sync.RWMutex
	startedAt     int64
	lastEventTime int64
	version       uint64
	eventCount    uint64

	daily map[int64]*liquidationBucketStat
	h4    map[int64]*liquidationBucketStat
	h1    map[int64]*liquidationBucketStat

	// 按币种追踪（每个时间桶 -> symbolBucket）
	symbolH1  map[int64]*symbolBucket
	symbolH4  map[int64]*symbolBucket
	symbolDay map[int64]*symbolBucket
}

type liquidationStatsHub struct {
	mu      sync.RWMutex
	clients map[*wsClient]bool

	running bool
	stopC   chan struct{}
}

var (
	liquidationStore = &liquidationStatsStore{
		startedAt: time.Now().UTC().UnixMilli(),
		daily:     make(map[int64]*liquidationBucketStat),
		h4:        make(map[int64]*liquidationBucketStat),
		h1:        make(map[int64]*liquidationBucketStat),
		symbolH1:  make(map[int64]*symbolBucket),
		symbolH4:  make(map[int64]*symbolBucket),
		symbolDay: make(map[int64]*symbolBucket),
	}
	lHub = &liquidationStatsHub{
		clients: make(map[*wsClient]bool),
	}
)

func init() {
	// 暴露爆仓统计存储给 strategy_link 使用
	SetGlobalStatsStore(liquidationStore)
}

func handleWsLiquidationStats(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if Cfg.Auth.Token != "" && token != Cfg.Auth.Token {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	conn, err := wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("[WsLiq] Upgrade failed: %v", err)
		return
	}

	client := newWsClient(conn)
	lHub.subscribe(client)

	go client.writePump()
	go readPumpLiquidationStats(client)
}

func (h *liquidationStatsHub) subscribe(client *wsClient) {
	h.mu.Lock()
	h.clients[client] = true
	needStart := !h.running
	if needStart {
		h.running = true
		h.stopC = make(chan struct{})
	}
	total := len(h.clients)
	stopC := h.stopC
	h.mu.Unlock()

	h.sendSnapshot(client)
	if needStart {
		go h.run(stopC)
	}

	log.Printf("[WsLiq] Client subscribed (total: %d)", total)
}

func (h *liquidationStatsHub) unsubscribe(client *wsClient) {
	h.mu.Lock()
	delete(h.clients, client)
	remaining := len(h.clients)
	stopC := h.stopC
	h.mu.Unlock()

	log.Printf("[WsLiq] Client unsubscribed (remaining: %d)", remaining)
	if remaining != 0 || stopC == nil {
		return
	}

	go func() {
		time.Sleep(30 * time.Second)
		h.mu.Lock()
		defer h.mu.Unlock()
		if len(h.clients) != 0 || !h.running || h.stopC == nil {
			return
		}
		close(h.stopC)
		h.running = false
		h.stopC = nil
		log.Printf("[WsLiq] Background stream stopped")
	}()
}

func (h *liquidationStatsHub) run(stopC <-chan struct{}) {
	go runLiquidationCollector(stopC)

	ticker := time.NewTicker(liquidationBroadcastInterval)
	defer ticker.Stop()

	lastVersion := uint64(0)
	for {
		select {
		case <-stopC:
			return
		case <-ticker.C:
			version := liquidationStore.currentVersion()
			if version == 0 || version == lastVersion {
				continue
			}
			h.broadcastSnapshot()
			lastVersion = version
		}
	}
}

func (h *liquidationStatsHub) sendSnapshot(client *wsClient) {
	payload := liquidationStore.snapshot(time.Now().UTC())
	raw, err := json.Marshal(payload)
	if err != nil {
		return
	}
	go func(snapshot liquidationStatsPayload) {
		if err := SaveLiquidationSnapshot(snapshot); err != nil {
			log.Printf("[WsLiq] Save snapshot failed: %v", err)
		}
	}(payload)
	select {
	case client.sendCh <- raw:
	default:
	}
}

func (h *liquidationStatsHub) broadcastSnapshot() {
	payload := liquidationStore.snapshot(time.Now().UTC())
	raw, err := json.Marshal(payload)
	if err != nil {
		return
	}
	go func(snapshot liquidationStatsPayload) {
		if err := SaveLiquidationSnapshot(snapshot); err != nil {
			log.Printf("[WsLiq] Save snapshot failed: %v", err)
		}
	}(payload)

	h.mu.RLock()
	clients := make([]*wsClient, 0, len(h.clients))
	for c := range h.clients {
		clients = append(clients, c)
	}
	h.mu.RUnlock()

	for _, c := range clients {
		select {
		case c.sendCh <- raw:
		default:
		}
	}
}

func readPumpLiquidationStats(client *wsClient) {
	defer client.close()
	defer lHub.unsubscribe(client)

	client.conn.SetReadLimit(1024)
	client.conn.SetReadDeadline(time.Now().Add(90 * time.Second))
	client.conn.SetPongHandler(func(string) error {
		client.conn.SetReadDeadline(time.Now().Add(90 * time.Second))
		return nil
	})

	for {
		_, message, err := client.conn.ReadMessage()
		if err != nil {
			return
		}

		var req struct {
			Action string `json:"action"`
			Method string `json:"method"`
		}
		if err := json.Unmarshal(message, &req); err != nil {
			client.conn.SetReadDeadline(time.Now().Add(90 * time.Second))
			continue
		}

		action := strings.ToLower(strings.TrimSpace(chooseValue(req.Action, req.Method)))
		switch action {
		case "ping":
			enqueueJSON(client, map[string]any{"action": "pong"})
		case "refresh", "snapshot":
			lHub.sendSnapshot(client)
		}

		client.conn.SetReadDeadline(time.Now().Add(90 * time.Second))
	}
}

func runLiquidationCollector(stopC <-chan struct{}) {
	backoff := liquidationReconnectBaseDelay

	for {
		select {
		case <-stopC:
			return
		default:
		}

		conn, _, err := websocket.DefaultDialer.Dial(binanceLiquidationWSURL, nil)
		if err != nil {
			log.Printf("[WsLiq] Binance dial failed: %v", err)
			waitOrDone(stopC, backoff)
			backoff = min(backoff*2, 30*time.Second)
			continue
		}

		log.Printf("[WsLiq] Binance forceOrder stream connected")
		backoff = liquidationReconnectBaseDelay
		conn.SetReadLimit(1 << 20)
		conn.SetPongHandler(func(string) error {
			conn.SetReadDeadline(time.Now().Add(90 * time.Second))
			return nil
		})

		streamAlive := true
		for streamAlive {
			select {
			case <-stopC:
				streamAlive = false
				continue
			default:
			}

			conn.SetReadDeadline(time.Now().Add(90 * time.Second))
			_, message, err := conn.ReadMessage()
			if err != nil {
				log.Printf("[WsLiq] Binance stream disconnected: %v", err)
				streamAlive = false
				continue
			}
			handleLiquidationMessage(message)
		}

		conn.Close()
		waitOrDone(stopC, backoff)
	}
}

func handleLiquidationMessage(raw []byte) {
	payload := raw

	var combined struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(raw, &combined); err == nil && len(combined.Data) > 0 {
		payload = combined.Data
	}

	var event binanceForceOrderEvent
	if err := json.Unmarshal(payload, &event); err != nil {
		return
	}
	if !strings.EqualFold(event.EventType, "forceOrder") {
		return
	}

	ts := event.Order.TradeTime
	if ts <= 0 {
		ts = event.EventTime
	}
	if ts <= 0 {
		ts = time.Now().UTC().UnixMilli()
	}

	price := parseFirstPositive(event.Order.AvgPrice, event.Order.Price)
	qty := parseFirstPositive(event.Order.FilledAccumulatedQty, event.Order.LastFilledQuantity, event.Order.OrigQuantity)
	notional := price * qty

	liquidationStore.addEvent(ts, event.Order.Symbol, event.Order.Side, notional)
}

func (s *liquidationStatsStore) addEvent(ts int64, symbol string, side string, notional float64) {
	if ts <= 0 {
		ts = time.Now().UTC().UnixMilli()
	}
	if !isFinitePositive(notional) {
		notional = 0
	}

	eventTime := time.UnixMilli(ts).UTC()
	dayStart := utcDayStart(eventTime).UnixMilli()
	h4Start := utcHourStart(eventTime, 4).UnixMilli()
	h1Start := utcHourStart(eventTime, 1).UnixMilli()

	s.mu.Lock()
	defer s.mu.Unlock()

	updateBucket(getOrCreateBucket(s.daily, dayStart, 24*time.Hour), side, notional)
	updateBucket(getOrCreateBucket(s.h4, h4Start, 4*time.Hour), side, notional)
	updateBucket(getOrCreateBucket(s.h1, h1Start, 1*time.Hour), side, notional)

	// 按币种追踪
	if symbol != "" && notional > 0 {
		if s.symbolH1[h1Start] == nil {
			s.symbolH1[h1Start] = newSymbolBucket()
		}
		s.symbolH1[h1Start].add(symbol, notional)
		if s.symbolH4[h4Start] == nil {
			s.symbolH4[h4Start] = newSymbolBucket()
		}
		s.symbolH4[h4Start].add(symbol, notional)
		if s.symbolDay[dayStart] == nil {
			s.symbolDay[dayStart] = newSymbolBucket()
		}
		s.symbolDay[dayStart].add(symbol, notional)
	}

	s.lastEventTime = ts
	s.version++
	s.eventCount++
	if s.eventCount%128 == 0 {
		s.pruneLocked(time.Now().UTC())
	}
}

func getOrCreateBucket(target map[int64]*liquidationBucketStat, start int64, width time.Duration) *liquidationBucketStat {
	if bucket, ok := target[start]; ok {
		return bucket
	}
	bucket := &liquidationBucketStat{
		StartTime: start,
		EndTime:   start + width.Milliseconds(),
	}
	target[start] = bucket
	return bucket
}

func updateBucket(bucket *liquidationBucketStat, side string, notional float64) {
	bucket.TotalCount++
	bucket.TotalNotional += notional

	switch strings.ToUpper(strings.TrimSpace(side)) {
	case "BUY":
		bucket.BuyCount++
		bucket.BuyNotional += notional
	case "SELL":
		bucket.SellCount++
		bucket.SellNotional += notional
	}
}

func (s *liquidationStatsStore) pruneLocked(now time.Time) {
	dayCutoff := utcDayStart(now.Add(-liquidationRetentionDays * 24 * time.Hour)).UnixMilli()
	h4Cutoff := utcHourStart(now.Add(-liquidationRetentionDays*24*time.Hour), 4).UnixMilli()
	h1Cutoff := utcHourStart(now.Add(-liquidationRetentionDays*24*time.Hour), 1).UnixMilli()

	for start := range s.daily {
		if start < dayCutoff {
			delete(s.daily, start)
		}
	}
	for start := range s.h4 {
		if start < h4Cutoff {
			delete(s.h4, start)
		}
	}
	for start := range s.h1 {
		if start < h1Cutoff {
			delete(s.h1, start)
		}
	}
	for start := range s.symbolDay {
		if start < dayCutoff {
			delete(s.symbolDay, start)
		}
	}
	for start := range s.symbolH4 {
		if start < h4Cutoff {
			delete(s.symbolH4, start)
		}
	}
	for start := range s.symbolH1 {
		if start < h1Cutoff {
			delete(s.symbolH1, start)
		}
	}
}

func (s *liquidationStatsStore) currentVersion() uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.version
}

func (s *liquidationStatsStore) snapshotJSON(now time.Time) ([]byte, error) {
	payload := s.snapshot(now)
	return json.Marshal(payload)
}

func (s *liquidationStatsStore) snapshot(now time.Time) liquidationStatsPayload {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	now = now.UTC()

	s.mu.RLock()
	defer s.mu.RUnlock()

	payload := liquidationStatsPayload{
		Channel:       "liquidationStats",
		Time:          time.Now().UnixMilli(),
		Timezone:      "UTC",
		StartedAt:     s.startedAt,
		LastEventTime: s.lastEventTime,
		EventCount:    s.eventCount,
	}

	dayCurrent := utcDayStart(now).UnixMilli()
	h4Current := utcHourStart(now, 4).UnixMilli()
	h1Current := utcHourStart(now, 1).UnixMilli()

	payload.Stats.Daily = buildSeries(s.daily, dayCurrent, (24 * time.Hour).Milliseconds(), 24*time.Hour, liquidationHistoryDays)
	payload.Stats.H4 = buildSeries(s.h4, h4Current, (4 * time.Hour).Milliseconds(), 4*time.Hour, liquidationHistoryH4)
	payload.Stats.H1 = buildSeries(s.h1, h1Current, (1 * time.Hour).Milliseconds(), time.Hour, liquidationHistoryH1)

	// Top 5 爆仓币种
	if sb, ok := s.symbolH1[h1Current]; ok {
		payload.TopSymbols.H1 = sb.topN(5)
	}
	if sb, ok := s.symbolH4[h4Current]; ok {
		payload.TopSymbols.H4 = sb.topN(5)
	}
	if sb, ok := s.symbolDay[dayCurrent]; ok {
		payload.TopSymbols.Day = sb.topN(5)
	}

	return payload
}

func buildSeries(source map[int64]*liquidationBucketStat, currentStart int64, stepMS int64, width time.Duration, points int) []liquidationBucketStat {
	list := make([]liquidationBucketStat, 0, points)
	for i := 0; i < points; i++ {
		start := currentStart - int64(i)*stepMS
		if bucket, ok := source[start]; ok {
			list = append(list, *bucket)
			continue
		}
		list = append(list, liquidationBucketStat{
			StartTime: start,
			EndTime:   start + width.Milliseconds(),
		})
	}
	return list
}

func parseFirstPositive(values ...string) float64 {
	for _, raw := range values {
		v, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
		if err != nil || v <= 0 {
			continue
		}
		if !math.IsNaN(v) && !math.IsInf(v, 0) {
			return v
		}
	}
	return 0
}

func isFinitePositive(v float64) bool {
	return v >= 0 && !math.IsNaN(v) && !math.IsInf(v, 0)
}

func utcDayStart(t time.Time) time.Time {
	t = t.UTC()
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

func utcHourStart(t time.Time, hourStep int) time.Time {
	t = t.UTC()
	h := (t.Hour() / hourStep) * hourStep
	return time.Date(t.Year(), t.Month(), t.Day(), h, 0, 0, 0, time.UTC)
}
