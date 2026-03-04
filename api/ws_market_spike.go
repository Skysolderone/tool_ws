package api

import (
	"encoding/json"
	"fmt"
	"log"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/adshao/go-binance/v2/futures"
)

const (
	marketSpikeDefaultThresholdPct = 1.5
	marketSpikeDefaultWindowSec    = 30
	marketSpikeDefaultCooldownSec  = 15
	marketSpikeDefaultSuppressSec  = 60
	marketSpikeDefaultMinSamples   = 40
	marketSpikeDefaultWarnQ        = 95.0
	marketSpikeDefaultCriticalQ    = 99.0
	marketSpikeRulesPersistFile    = "market_spike_rules.json"

	marketSpikeMinThresholdPct = 0.1
	marketSpikeMaxThresholdPct = 30.0
	marketSpikeMinWindowSec    = 5
	marketSpikeMaxWindowSec    = 3600
	marketSpikeMinCooldownSec  = 5
	marketSpikeMaxCooldownSec  = 600
	marketSpikeMinSuppressSec  = 10
	marketSpikeMaxSuppressSec  = 1800
	marketSpikeMinQuantile     = 50.0
	marketSpikeMaxQuantile     = 99.9
	marketSpikeMinSamplesMin   = 10
	marketSpikeMinSamplesMax   = 500

	marketSpikeSampleCap       = 3000
	marketSpikeMoveHistoryCap  = 1200
	marketSpikeReconnectBase   = time.Second
	marketSpikeSnapshotEveryMS = 30000
)

type marketSpikeSample struct {
	T int64
	P float64
}

type marketSpikeRule struct {
	ID            string  `json:"id"`
	Symbol        string  `json:"symbol"`
	ThresholdPct  float64 `json:"thresholdPct"`
	Dynamic       bool    `json:"dynamic"`
	WarnQuantile  float64 `json:"warnQuantile"`
	CriticalQ     float64 `json:"criticalQuantile"`
	MinSamples    int     `json:"minSamples"`
	WindowSec     int     `json:"windowSec"`
	CooldownSec   int     `json:"cooldownSec"`
	SuppressSec   int     `json:"suppressSec"`
	Enabled       bool    `json:"enabled"`
	LastPrice     float64 `json:"lastPrice"`
	LastMovePct   float64 `json:"lastMovePct"`
	LastTriggerAt int64   `json:"lastTriggerAt"`
	CreatedAt     int64   `json:"createdAt"`

	Samples     []marketSpikeSample `json:"-"`
	MoveHistory []float64           `json:"-"`
}

type marketSpikeEvent struct {
	ID           string  `json:"id"`
	RuleID       string  `json:"ruleId"`
	Symbol       string  `json:"symbol"`
	Direction    string  `json:"direction"` // 拉升 / 下跌
	ThresholdPct float64 `json:"thresholdPct"`
	TriggerPct   float64 `json:"triggerPct"`
	Dynamic      bool    `json:"dynamic"`
	Severity     string  `json:"severity"`
	SuppressSec  int     `json:"suppressSec"`
	WindowSec    int     `json:"windowSec"`
	MovePct      float64 `json:"movePct"`
	BasePrice    float64 `json:"basePrice"`
	Price        float64 `json:"price"`
	Time         int64   `json:"time"`
}

type marketSpikePayload struct {
	Channel string            `json:"channel"`
	Type    string            `json:"type"` // snapshot / event / pong / ack / error
	Time    int64             `json:"t"`
	Action  string            `json:"action,omitempty"`
	Rules   []marketSpikeRule `json:"rules,omitempty"`
	Event   *marketSpikeEvent `json:"event,omitempty"`
	Error   string            `json:"error,omitempty"`
}

type marketSpikeSession struct {
	client *wsClient

	mu    sync.Mutex
	rules map[string]*marketSpikeRule

	lastSuppressed map[string]int64
}

type marketSpikeRoom struct {
	mu sync.RWMutex

	symbol   string
	sessions map[*marketSpikeSession]bool
	stopC    chan struct{}
	running  bool
}

type marketSpikeHub struct {
	mu sync.RWMutex

	sessions map[*wsClient]*marketSpikeSession
	rooms    map[string]*marketSpikeRoom

	persistMu sync.Mutex
}

var mSpikeHub = &marketSpikeHub{
	sessions: make(map[*wsClient]*marketSpikeSession),
	rooms:    make(map[string]*marketSpikeRoom),
}

func handleWsMarketSpike(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if Cfg.Auth.Token != "" && token != Cfg.Auth.Token {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	conn, err := wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("[WsSpike] Upgrade failed: %v", err)
		return
	}

	client := newWsClient(conn)
	session := mSpikeHub.addSession(client)
	mSpikeHub.sendSnapshot(session)

	go client.writePump()
	go readPumpMarketSpike(session)
}

func (h *marketSpikeHub) addSession(client *wsClient) *marketSpikeSession {
	session := &marketSpikeSession{
		client:         client,
		rules:          make(map[string]*marketSpikeRule),
		lastSuppressed: make(map[string]int64),
	}
	if err := h.restoreSessionRules(session); err != nil {
		log.Printf("[WsSpike] Restore rules failed: %v", err)
	}

	h.mu.Lock()
	h.sessions[client] = session
	h.mu.Unlock()

	symbols := session.enabledSymbols()
	for sym := range symbols {
		h.subscribeSymbol(sym, session)
	}

	log.Printf("[WsSpike] Client connected")
	return session
}

func (h *marketSpikeHub) removeSession(session *marketSpikeSession) {
	if session == nil || session.client == nil {
		return
	}

	symbols := session.enabledSymbols()
	for sym := range symbols {
		h.unsubscribeSymbol(sym, session)
	}

	h.mu.Lock()
	delete(h.sessions, session.client)
	h.mu.Unlock()

	log.Printf("[WsSpike] Client disconnected")
}

func (h *marketSpikeHub) getOrCreateRoom(symbol string) *marketSpikeRoom {
	sym := strings.ToUpper(strings.TrimSpace(symbol))
	h.mu.RLock()
	room, ok := h.rooms[sym]
	h.mu.RUnlock()
	if ok {
		return room
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	if room, ok = h.rooms[sym]; ok {
		return room
	}
	room = &marketSpikeRoom{
		symbol:   sym,
		sessions: make(map[*marketSpikeSession]bool),
		stopC:    make(chan struct{}),
	}
	h.rooms[sym] = room
	return room
}

func (h *marketSpikeHub) subscribeSymbol(symbol string, session *marketSpikeSession) {
	room := h.getOrCreateRoom(symbol)

	room.mu.Lock()
	room.sessions[session] = true
	needStart := !room.running
	if needStart {
		room.running = true
	}
	total := len(room.sessions)
	room.mu.Unlock()

	if needStart {
		go h.startSymbolStream(room)
	}

	log.Printf("[WsSpike] Session subscribed %s (total: %d)", room.symbol, total)
}

func (h *marketSpikeHub) unsubscribeSymbol(symbol string, session *marketSpikeSession) {
	sym := strings.ToUpper(strings.TrimSpace(symbol))
	h.mu.RLock()
	room, ok := h.rooms[sym]
	h.mu.RUnlock()
	if !ok {
		return
	}

	room.mu.Lock()
	delete(room.sessions, session)
	remaining := len(room.sessions)
	stopC := room.stopC
	room.mu.Unlock()

	log.Printf("[WsSpike] Session unsubscribed %s (remaining: %d)", sym, remaining)
	if remaining != 0 || stopC == nil {
		return
	}

	go func() {
		time.Sleep(20 * time.Second)
		room.mu.Lock()
		defer room.mu.Unlock()
		if len(room.sessions) != 0 || !room.running || room.stopC == nil {
			return
		}
		close(room.stopC)
		room.running = false

		h.mu.Lock()
		delete(h.rooms, sym)
		h.mu.Unlock()
		log.Printf("[WsSpike] Stream stopped for %s", sym)
	}()
}

func (h *marketSpikeHub) startSymbolStream(room *marketSpikeRoom) {
	backoff := marketSpikeReconnectBase
	symbolLower := strings.ToLower(room.symbol)

	for {
		select {
		case <-room.stopC:
			return
		default:
		}

		doneC, stopC, err := futures.WsAggTradeServe(symbolLower, func(event *futures.WsAggTradeEvent) {
			price, err := strconv.ParseFloat(event.Price, 64)
			if err != nil || price <= 0 || math.IsNaN(price) || math.IsInf(price, 0) {
				return
			}

			ts := event.Time
			if ts <= 0 {
				ts = time.Now().UnixMilli()
			}

			room.mu.RLock()
			sessions := make([]*marketSpikeSession, 0, len(room.sessions))
			for s := range room.sessions {
				sessions = append(sessions, s)
			}
			room.mu.RUnlock()

			for _, session := range sessions {
				events := session.processPrice(room.symbol, price, ts)
				for _, evt := range events {
					h.sendEvent(session, evt)
				}
			}
		}, func(err error) {
			log.Printf("[WsSpike] Binance stream error %s: %v", room.symbol, err)
		})
		if err != nil {
			log.Printf("[WsSpike] Binance connect failed %s: %v", room.symbol, err)
			select {
			case <-room.stopC:
				return
			case <-time.After(backoff):
			}
			backoff = min(backoff*2, 2*time.Minute)
			continue
		}

		backoff = marketSpikeReconnectBase
		select {
		case <-room.stopC:
			close(stopC)
			return
		case <-doneC:
			log.Printf("[WsSpike] Binance stream disconnected %s, reconnecting...", room.symbol)
		}

		select {
		case <-room.stopC:
			return
		case <-time.After(backoff):
		}
	}
}

func (s *marketSpikeSession) enabledSymbols() map[string]bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.enabledSymbolsLocked()
}

func (s *marketSpikeSession) enabledSymbolsLocked() map[string]bool {
	out := make(map[string]bool)
	for _, rule := range s.rules {
		if rule.Enabled {
			out[rule.Symbol] = true
		}
	}
	return out
}

func diffSymbols(from, to map[string]bool) []string {
	list := make([]string, 0)
	for sym := range from {
		if !to[sym] {
			list = append(list, sym)
		}
	}
	return list
}

func cloneRule(rule *marketSpikeRule) marketSpikeRule {
	if rule == nil {
		return marketSpikeRule{}
	}
	return marketSpikeRule{
		ID:            rule.ID,
		Symbol:        rule.Symbol,
		ThresholdPct:  rule.ThresholdPct,
		Dynamic:       rule.Dynamic,
		WarnQuantile:  rule.WarnQuantile,
		CriticalQ:     rule.CriticalQ,
		MinSamples:    rule.MinSamples,
		WindowSec:     rule.WindowSec,
		CooldownSec:   rule.CooldownSec,
		SuppressSec:   rule.SuppressSec,
		Enabled:       rule.Enabled,
		LastPrice:     rule.LastPrice,
		LastMovePct:   rule.LastMovePct,
		LastTriggerAt: rule.LastTriggerAt,
		CreatedAt:     rule.CreatedAt,
	}
}

func (s *marketSpikeSession) snapshotRules() []marketSpikeRule {
	s.mu.Lock()
	defer s.mu.Unlock()

	list := make([]marketSpikeRule, 0, len(s.rules))
	for _, rule := range s.rules {
		list = append(list, cloneRule(rule))
	}
	// createdAt 倒序
	for i := 0; i < len(list); i++ {
		for j := i + 1; j < len(list); j++ {
			if list[j].CreatedAt > list[i].CreatedAt {
				list[i], list[j] = list[j], list[i]
			}
		}
	}
	return list
}

func (h *marketSpikeHub) restoreSessionRules(session *marketSpikeSession) error {
	if session == nil {
		return nil
	}

	var stored []marketSpikeRule
	if err := loadJSONFile(wsRulePersistPath(marketSpikeRulesPersistFile), &stored); err != nil {
		return err
	}
	if len(stored) == 0 {
		return nil
	}

	now := time.Now().UnixMilli()
	session.mu.Lock()
	defer session.mu.Unlock()

	for i, item := range stored {
		symbol := strings.ToUpper(strings.TrimSpace(item.Symbol))
		if symbol == "" {
			continue
		}
		ruleID := strings.TrimSpace(item.ID)
		if ruleID == "" {
			ruleID = fmt.Sprintf("%s-restore-%d-%d", symbol, now, i)
		}
		dynamic := item.Dynamic
		if !dynamic && item.WarnQuantile == 0 && item.CriticalQ == 0 && item.MinSamples == 0 && item.SuppressSec == 0 {
			dynamic = true
		}
		rule := marketSpikeRule{
			ID:            ruleID,
			Symbol:        symbol,
			ThresholdPct:  sanitizeThresholdPct(item.ThresholdPct),
			Dynamic:       dynamic,
			WarnQuantile:  sanitizeQuantile(item.WarnQuantile, marketSpikeDefaultWarnQ),
			CriticalQ:     sanitizeQuantile(item.CriticalQ, marketSpikeDefaultCriticalQ),
			MinSamples:    sanitizeMinSamples(item.MinSamples),
			WindowSec:     sanitizeWindowSec(item.WindowSec),
			CooldownSec:   sanitizeCooldownSec(item.CooldownSec),
			SuppressSec:   sanitizeSuppressSec(item.SuppressSec),
			Enabled:       item.Enabled,
			LastPrice:     item.LastPrice,
			LastMovePct:   item.LastMovePct,
			LastTriggerAt: item.LastTriggerAt,
			CreatedAt:     item.CreatedAt,
		}
		if rule.CreatedAt <= 0 {
			rule.CreatedAt = now - int64(len(stored)-i)
		}
		if rule.CriticalQ < rule.WarnQuantile {
			rule.CriticalQ = rule.WarnQuantile
		}
		session.rules[rule.ID] = &rule
	}
	return nil
}

func (h *marketSpikeHub) persistSessionRules(session *marketSpikeSession) error {
	if session == nil {
		return nil
	}
	rules := session.snapshotRules()
	h.persistMu.Lock()
	defer h.persistMu.Unlock()
	return saveJSONFile(wsRulePersistPath(marketSpikeRulesPersistFile), rules)
}

func sanitizeThresholdPct(v float64) float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) || v <= 0 {
		return marketSpikeDefaultThresholdPct
	}
	if v < marketSpikeMinThresholdPct {
		return marketSpikeMinThresholdPct
	}
	if v > marketSpikeMaxThresholdPct {
		return marketSpikeMaxThresholdPct
	}
	return math.Round(v*100) / 100
}

func sanitizeWindowSec(v int) int {
	if v <= 0 {
		return marketSpikeDefaultWindowSec
	}
	if v < marketSpikeMinWindowSec {
		return marketSpikeMinWindowSec
	}
	if v > marketSpikeMaxWindowSec {
		return marketSpikeMaxWindowSec
	}
	return v
}

func sanitizeCooldownSec(v int) int {
	if v <= 0 {
		return marketSpikeDefaultCooldownSec
	}
	if v < marketSpikeMinCooldownSec {
		return marketSpikeMinCooldownSec
	}
	if v > marketSpikeMaxCooldownSec {
		return marketSpikeMaxCooldownSec
	}
	return v
}

func sanitizeSuppressSec(v int) int {
	if v <= 0 {
		return marketSpikeDefaultSuppressSec
	}
	if v < marketSpikeMinSuppressSec {
		return marketSpikeMinSuppressSec
	}
	if v > marketSpikeMaxSuppressSec {
		return marketSpikeMaxSuppressSec
	}
	return v
}

func sanitizeQuantile(v float64, fallback float64) float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) || v <= 0 {
		return fallback
	}
	if v < marketSpikeMinQuantile {
		return marketSpikeMinQuantile
	}
	if v > marketSpikeMaxQuantile {
		return marketSpikeMaxQuantile
	}
	return math.Round(v*10) / 10
}

func sanitizeMinSamples(v int) int {
	if v <= 0 {
		return marketSpikeDefaultMinSamples
	}
	if v < marketSpikeMinSamplesMin {
		return marketSpikeMinSamplesMin
	}
	if v > marketSpikeMinSamplesMax {
		return marketSpikeMinSamplesMax
	}
	return v
}

func calcPctl(values []float64, q float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sorted := make([]float64, 0, len(values))
	for _, v := range values {
		if v > 0 && !math.IsNaN(v) && !math.IsInf(v, 0) {
			sorted = append(sorted, v)
		}
	}
	if len(sorted) == 0 {
		return 0
	}
	sort.Float64s(sorted)
	return percentile(sorted, q)
}

func (s *marketSpikeSession) upsertRule(rule marketSpikeRule) (subscribe []string, unsubscribe []string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	before := s.enabledSymbolsLocked()

	rule.ThresholdPct = sanitizeThresholdPct(rule.ThresholdPct)
	rule.WindowSec = sanitizeWindowSec(rule.WindowSec)
	rule.CooldownSec = sanitizeCooldownSec(rule.CooldownSec)
	rule.SuppressSec = sanitizeSuppressSec(rule.SuppressSec)
	rule.WarnQuantile = sanitizeQuantile(rule.WarnQuantile, marketSpikeDefaultWarnQ)
	rule.CriticalQ = sanitizeQuantile(rule.CriticalQ, marketSpikeDefaultCriticalQ)
	if rule.CriticalQ < rule.WarnQuantile {
		rule.CriticalQ = rule.WarnQuantile
	}
	rule.MinSamples = sanitizeMinSamples(rule.MinSamples)

	if old, ok := s.rules[rule.ID]; ok {
		// 更新规则时继承历史样本与上次状态
		rule.Samples = old.Samples
		rule.MoveHistory = old.MoveHistory
		rule.LastPrice = old.LastPrice
		rule.LastMovePct = old.LastMovePct
		rule.LastTriggerAt = old.LastTriggerAt
		if rule.CreatedAt <= 0 {
			rule.CreatedAt = old.CreatedAt
		}
	}
	if rule.CreatedAt <= 0 {
		rule.CreatedAt = time.Now().UnixMilli()
	}
	s.rules[rule.ID] = &rule

	after := s.enabledSymbolsLocked()
	subscribe = diffSymbols(after, before)
	unsubscribe = diffSymbols(before, after)
	return
}

func (s *marketSpikeSession) toggleRule(id string, enabled *bool) (subscribe []string, unsubscribe []string, ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	rule, exists := s.rules[id]
	if !exists {
		return nil, nil, false
	}
	before := s.enabledSymbolsLocked()
	if enabled == nil {
		rule.Enabled = !rule.Enabled
	} else {
		rule.Enabled = *enabled
	}
	after := s.enabledSymbolsLocked()
	return diffSymbols(after, before), diffSymbols(before, after), true
}

func (s *marketSpikeSession) removeRule(id string) (subscribe []string, unsubscribe []string, ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, exists := s.rules[id]
	if !exists {
		return nil, nil, false
	}
	before := s.enabledSymbolsLocked()
	delete(s.rules, id)
	after := s.enabledSymbolsLocked()
	return diffSymbols(after, before), diffSymbols(before, after), true
}

func (s *marketSpikeSession) clearRules() (unsubscribe []string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	before := s.enabledSymbolsLocked()
	s.rules = make(map[string]*marketSpikeRule)
	return diffSymbols(before, map[string]bool{})
}

func (s *marketSpikeSession) processPrice(symbol string, price float64, ts int64) []marketSpikeEvent {
	if ts <= 0 {
		ts = time.Now().UnixMilli()
	}
	if price <= 0 || math.IsNaN(price) || math.IsInf(price, 0) {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	events := make([]marketSpikeEvent, 0)
	for _, rule := range s.rules {
		if !rule.Enabled || rule.Symbol != symbol {
			continue
		}

		windowSec := sanitizeWindowSec(rule.WindowSec)
		windowMS := int64(windowSec) * 1000
		cooldownMS := int64(sanitizeCooldownSec(rule.CooldownSec)) * 1000
		suppressSec := sanitizeSuppressSec(rule.SuppressSec)
		suppressMS := int64(suppressSec) * 1000
		thresholdPct := sanitizeThresholdPct(rule.ThresholdPct)
		warnQ := sanitizeQuantile(rule.WarnQuantile, marketSpikeDefaultWarnQ)
		criticalQ := sanitizeQuantile(rule.CriticalQ, marketSpikeDefaultCriticalQ)
		if criticalQ < warnQ {
			criticalQ = warnQ
		}
		minSamples := sanitizeMinSamples(rule.MinSamples)
		dynamic := rule.Dynamic

		samples := make([]marketSpikeSample, 0, len(rule.Samples)+1)
		for _, sample := range rule.Samples {
			if sample.P <= 0 || ts-sample.T > windowMS {
				continue
			}
			samples = append(samples, sample)
		}
		samples = append(samples, marketSpikeSample{T: ts, P: price})
		if len(samples) > marketSpikeSampleCap {
			samples = samples[len(samples)-marketSpikeSampleCap:]
		}
		rule.Samples = samples

		base := samples[0].P
		movePct := 0.0
		if base > 0 {
			movePct = ((price - base) / base) * 100
		}
		absMove := math.Abs(movePct)
		rule.LastPrice = price
		rule.LastMovePct = movePct

		if base <= 0 {
			continue
		}

		if absMove > 0 && !math.IsNaN(absMove) && !math.IsInf(absMove, 0) {
			hist := append(rule.MoveHistory, absMove)
			if len(hist) > marketSpikeMoveHistoryCap {
				hist = hist[len(hist)-marketSpikeMoveHistoryCap:]
			}
			rule.MoveHistory = hist
		}

		triggerPct := thresholdPct
		severity := "warn"
		dynamicUsed := false
		if dynamic && len(rule.MoveHistory) >= minSamples {
			warnTrigger := calcPctl(rule.MoveHistory, warnQ)
			criticalTrigger := calcPctl(rule.MoveHistory, criticalQ)
			if warnTrigger <= 0 {
				warnTrigger = thresholdPct
			}
			if criticalTrigger < warnTrigger {
				criticalTrigger = warnTrigger
			}
			dynamicUsed = true
			triggerPct = warnTrigger
			switch {
			case absMove >= criticalTrigger:
				severity = "critical"
				triggerPct = criticalTrigger
			case absMove >= warnTrigger:
				severity = "warn"
			default:
				continue
			}
		} else {
			if absMove < thresholdPct {
				continue
			}
			if absMove >= thresholdPct*2 {
				severity = "critical"
			}
		}
		if ts-rule.LastTriggerAt < cooldownMS {
			continue
		}

		direction := "拉升"
		if movePct < 0 {
			direction = "下跌"
		}
		if s.lastSuppressed == nil {
			s.lastSuppressed = make(map[string]int64)
		}
		suppressKey := fmt.Sprintf("%s|%s|%s", symbol, direction, severity)
		if lastTS, ok := s.lastSuppressed[suppressKey]; ok && lastTS > 0 && ts-lastTS < suppressMS {
			continue
		}
		s.lastSuppressed[suppressKey] = ts
		rule.LastTriggerAt = ts

		events = append(events, marketSpikeEvent{
			ID:           fmt.Sprintf("%s-%d", rule.ID, ts),
			RuleID:       rule.ID,
			Symbol:       symbol,
			Direction:    direction,
			ThresholdPct: thresholdPct,
			TriggerPct:   roundFloat(triggerPct, 4),
			Dynamic:      dynamicUsed,
			Severity:     severity,
			SuppressSec:  suppressSec,
			WindowSec:    windowSec,
			MovePct:      movePct,
			BasePrice:    base,
			Price:        price,
			Time:         ts,
		})
	}
	return events
}

func (h *marketSpikeHub) sendSnapshot(session *marketSpikeSession) {
	if session == nil {
		return
	}
	enqueueJSON(session.client, marketSpikePayload{
		Channel: "marketSpike",
		Type:    "snapshot",
		Time:    time.Now().UnixMilli(),
		Rules:   session.snapshotRules(),
	})
}

func (h *marketSpikeHub) sendEvent(session *marketSpikeSession, evt marketSpikeEvent) {
	if session == nil {
		return
	}
	enqueueJSON(session.client, marketSpikePayload{
		Channel: "marketSpike",
		Type:    "event",
		Time:    time.Now().UnixMilli(),
		Event:   &evt,
	})
}

func (h *marketSpikeHub) sendError(session *marketSpikeSession, action, errMsg string) {
	if session == nil {
		return
	}
	enqueueJSON(session.client, marketSpikePayload{
		Channel: "marketSpike",
		Type:    "error",
		Action:  action,
		Time:    time.Now().UnixMilli(),
		Error:   errMsg,
	})
}

func (h *marketSpikeHub) sendAck(session *marketSpikeSession, action string) {
	if session == nil {
		return
	}
	enqueueJSON(session.client, marketSpikePayload{
		Channel: "marketSpike",
		Type:    "ack",
		Action:  action,
		Time:    time.Now().UnixMilli(),
	})
}

func readPumpMarketSpike(session *marketSpikeSession) {
	client := session.client
	defer client.close()
	defer mSpikeHub.removeSession(session)

	client.conn.SetReadLimit(4096)
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
			Action       string  `json:"action"`
			Method       string  `json:"method"`
			ID           string  `json:"id"`
			Symbol       string  `json:"symbol"`
			ThresholdPct float64 `json:"thresholdPct"`
			Dynamic      *bool   `json:"dynamic"`
			WarnQuantile float64 `json:"warnQuantile"`
			CriticalQ    float64 `json:"criticalQuantile"`
			MinSamples   int     `json:"minSamples"`
			WindowSec    int     `json:"windowSec"`
			CooldownSec  int     `json:"cooldownSec"`
			SuppressSec  int     `json:"suppressSec"`
			Enabled      *bool   `json:"enabled"`
		}
		if err := json.Unmarshal(message, &req); err != nil {
			mSpikeHub.sendError(session, "unknown", "invalid json payload")
			client.conn.SetReadDeadline(time.Now().Add(90 * time.Second))
			continue
		}

		action := strings.ToLower(strings.TrimSpace(chooseValue(req.Action, req.Method)))
		switch action {
		case "ping":
			enqueueJSON(client, marketSpikePayload{
				Channel: "marketSpike",
				Type:    "pong",
				Time:    time.Now().UnixMilli(),
			})

		case "snapshot", "refresh":
			mSpikeHub.sendSnapshot(session)

		case "addrule", "create":
			symbol := strings.ToUpper(strings.TrimSpace(req.Symbol))
			if symbol == "" {
				mSpikeHub.sendError(session, action, "symbol is required")
				break
			}
			ruleID := strings.TrimSpace(req.ID)
			if ruleID == "" {
				ruleID = fmt.Sprintf("%s-%d", symbol, time.Now().UnixNano())
			}
			enabled := true
			if req.Enabled != nil {
				enabled = *req.Enabled
			}
			dynamic := true
			if req.Dynamic != nil {
				dynamic = *req.Dynamic
			}
			rule := marketSpikeRule{
				ID:           ruleID,
				Symbol:       symbol,
				ThresholdPct: sanitizeThresholdPct(req.ThresholdPct),
				Dynamic:      dynamic,
				WarnQuantile: sanitizeQuantile(req.WarnQuantile, marketSpikeDefaultWarnQ),
				CriticalQ:    sanitizeQuantile(req.CriticalQ, marketSpikeDefaultCriticalQ),
				MinSamples:   sanitizeMinSamples(req.MinSamples),
				WindowSec:    sanitizeWindowSec(req.WindowSec),
				CooldownSec:  sanitizeCooldownSec(req.CooldownSec),
				SuppressSec:  sanitizeSuppressSec(req.SuppressSec),
				Enabled:      enabled,
				CreatedAt:    time.Now().UnixMilli(),
			}
			subscribe, unsubscribe := session.upsertRule(rule)
			for _, sym := range unsubscribe {
				mSpikeHub.unsubscribeSymbol(sym, session)
			}
			for _, sym := range subscribe {
				mSpikeHub.subscribeSymbol(sym, session)
			}
			if err := mSpikeHub.persistSessionRules(session); err != nil {
				log.Printf("[WsSpike] Persist rules failed: %v", err)
			}
			mSpikeHub.sendAck(session, action)
			mSpikeHub.sendSnapshot(session)

		case "togglerule":
			ruleID := strings.TrimSpace(req.ID)
			if ruleID == "" {
				mSpikeHub.sendError(session, action, "id is required")
				break
			}
			subscribe, unsubscribe, ok := session.toggleRule(ruleID, req.Enabled)
			if !ok {
				mSpikeHub.sendError(session, action, "rule not found")
				break
			}
			for _, sym := range unsubscribe {
				mSpikeHub.unsubscribeSymbol(sym, session)
			}
			for _, sym := range subscribe {
				mSpikeHub.subscribeSymbol(sym, session)
			}
			if err := mSpikeHub.persistSessionRules(session); err != nil {
				log.Printf("[WsSpike] Persist rules failed: %v", err)
			}
			mSpikeHub.sendAck(session, action)
			mSpikeHub.sendSnapshot(session)

		case "removerule":
			ruleID := strings.TrimSpace(req.ID)
			if ruleID == "" {
				mSpikeHub.sendError(session, action, "id is required")
				break
			}
			_, unsubscribe, ok := session.removeRule(ruleID)
			if !ok {
				mSpikeHub.sendError(session, action, "rule not found")
				break
			}
			for _, sym := range unsubscribe {
				mSpikeHub.unsubscribeSymbol(sym, session)
			}
			if err := mSpikeHub.persistSessionRules(session); err != nil {
				log.Printf("[WsSpike] Persist rules failed: %v", err)
			}
			mSpikeHub.sendAck(session, action)
			mSpikeHub.sendSnapshot(session)

		case "clearrules":
			unsubscribe := session.clearRules()
			for _, sym := range unsubscribe {
				mSpikeHub.unsubscribeSymbol(sym, session)
			}
			if err := mSpikeHub.persistSessionRules(session); err != nil {
				log.Printf("[WsSpike] Persist rules failed: %v", err)
			}
			mSpikeHub.sendAck(session, action)
			mSpikeHub.sendSnapshot(session)

		default:
			mSpikeHub.sendError(session, action, "unsupported action")
		}

		client.conn.SetReadDeadline(time.Now().Add(90 * time.Second))
	}
}
