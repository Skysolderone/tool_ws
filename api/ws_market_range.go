package api

import (
	"encoding/json"
	"fmt"
	"log"
	"math"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/adshao/go-binance/v2/futures"
)

const (
	marketRangeDefaultCooldownSec = 10
	marketRangeMinCooldownSec     = 5
	marketRangeMaxCooldownSec     = 600
	marketRangeRulesPersistFile   = "market_range_rules.json"

	marketRangeReconnectBase = time.Second
)

type marketRangeRule struct {
	ID            string  `json:"id"`
	Symbol        string  `json:"symbol"`
	Lower         float64 `json:"lower"`
	Upper         float64 `json:"upper"`
	CooldownSec   int     `json:"cooldownSec"`
	Enabled       bool    `json:"enabled"`
	LastPrice     float64 `json:"lastPrice"`
	LastInside    *bool   `json:"lastInside"`
	LastTriggerAt int64   `json:"lastTriggerAt"`
	CreatedAt     int64   `json:"createdAt"`
}

type marketRangeEvent struct {
	ID        string  `json:"id"`
	RuleID    string  `json:"ruleId"`
	Symbol    string  `json:"symbol"`
	Direction string  `json:"direction"` // 上破 / 下破
	Lower     float64 `json:"lower"`
	Upper     float64 `json:"upper"`
	Price     float64 `json:"price"`
	Time      int64   `json:"time"`
}

type marketRangePayload struct {
	Channel string            `json:"channel"`
	Type    string            `json:"type"` // snapshot / event / pong / ack / error
	Time    int64             `json:"t"`
	Action  string            `json:"action,omitempty"`
	Rules   []marketRangeRule `json:"rules,omitempty"`
	Event   *marketRangeEvent `json:"event,omitempty"`
	Error   string            `json:"error,omitempty"`
}

type marketRangeSession struct {
	client *wsClient

	mu    sync.Mutex
	rules map[string]*marketRangeRule
}

type marketRangeRoom struct {
	mu sync.RWMutex

	symbol   string
	sessions map[*marketRangeSession]bool
	stopC    chan struct{}
	running  bool
}

type marketRangeHub struct {
	mu sync.RWMutex

	sessions map[*wsClient]*marketRangeSession
	rooms    map[string]*marketRangeRoom

	persistMu sync.Mutex
}

var mRangeHub = &marketRangeHub{
	sessions: make(map[*wsClient]*marketRangeSession),
	rooms:    make(map[string]*marketRangeRoom),
}

func handleWsMarketRange(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if Cfg.Auth.Token != "" && token != Cfg.Auth.Token {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	conn, err := wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("[WsRange] Upgrade failed: %v", err)
		return
	}

	client := newWsClient(conn)
	session := mRangeHub.addSession(client)
	mRangeHub.sendSnapshot(session)

	go client.writePump()
	go readPumpMarketRange(session)
}

func (h *marketRangeHub) addSession(client *wsClient) *marketRangeSession {
	session := &marketRangeSession{
		client: client,
		rules:  make(map[string]*marketRangeRule),
	}
	if err := h.restoreSessionRules(session); err != nil {
		log.Printf("[WsRange] Restore rules failed: %v", err)
	}

	h.mu.Lock()
	h.sessions[client] = session
	h.mu.Unlock()

	symbols := session.enabledSymbols()
	for sym := range symbols {
		h.subscribeSymbol(sym, session)
	}

	log.Printf("[WsRange] Client connected")
	return session
}

func (h *marketRangeHub) removeSession(session *marketRangeSession) {
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

	log.Printf("[WsRange] Client disconnected")
}

func (h *marketRangeHub) getOrCreateRoom(symbol string) *marketRangeRoom {
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
	room = &marketRangeRoom{
		symbol:   sym,
		sessions: make(map[*marketRangeSession]bool),
		stopC:    make(chan struct{}),
	}
	h.rooms[sym] = room
	return room
}

func (h *marketRangeHub) subscribeSymbol(symbol string, session *marketRangeSession) {
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

	log.Printf("[WsRange] Session subscribed %s (total: %d)", room.symbol, total)
}

func (h *marketRangeHub) unsubscribeSymbol(symbol string, session *marketRangeSession) {
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

	log.Printf("[WsRange] Session unsubscribed %s (remaining: %d)", sym, remaining)
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
		log.Printf("[WsRange] Stream stopped for %s", sym)
	}()
}

func (h *marketRangeHub) startSymbolStream(room *marketRangeRoom) {
	backoff := marketRangeReconnectBase
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
			sessions := make([]*marketRangeSession, 0, len(room.sessions))
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
			log.Printf("[WsRange] Binance stream error %s: %v", room.symbol, err)
		})
		if err != nil {
			log.Printf("[WsRange] Binance connect failed %s: %v", room.symbol, err)
			select {
			case <-room.stopC:
				return
			case <-time.After(backoff):
			}
			backoff = min(backoff*2, 2*time.Minute)
			continue
		}

		backoff = marketRangeReconnectBase
		select {
		case <-room.stopC:
			close(stopC)
			return
		case <-doneC:
			log.Printf("[WsRange] Binance stream disconnected %s, reconnecting...", room.symbol)
		}

		select {
		case <-room.stopC:
			return
		case <-time.After(backoff):
		}
	}
}

func (s *marketRangeSession) enabledSymbols() map[string]bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.enabledSymbolsLocked()
}

func (s *marketRangeSession) enabledSymbolsLocked() map[string]bool {
	out := make(map[string]bool)
	for _, rule := range s.rules {
		if rule.Enabled {
			out[rule.Symbol] = true
		}
	}
	return out
}

func diffRangeSymbols(from, to map[string]bool) []string {
	list := make([]string, 0)
	for sym := range from {
		if !to[sym] {
			list = append(list, sym)
		}
	}
	return list
}

func cloneRangeRule(rule *marketRangeRule) marketRangeRule {
	if rule == nil {
		return marketRangeRule{}
	}
	out := marketRangeRule{
		ID:            rule.ID,
		Symbol:        rule.Symbol,
		Lower:         rule.Lower,
		Upper:         rule.Upper,
		CooldownSec:   rule.CooldownSec,
		Enabled:       rule.Enabled,
		LastPrice:     rule.LastPrice,
		LastTriggerAt: rule.LastTriggerAt,
		CreatedAt:     rule.CreatedAt,
	}
	if rule.LastInside != nil {
		b := *rule.LastInside
		out.LastInside = &b
	}
	return out
}

func (s *marketRangeSession) snapshotRules() []marketRangeRule {
	s.mu.Lock()
	defer s.mu.Unlock()

	list := make([]marketRangeRule, 0, len(s.rules))
	for _, rule := range s.rules {
		list = append(list, cloneRangeRule(rule))
	}
	for i := 0; i < len(list); i++ {
		for j := i + 1; j < len(list); j++ {
			if list[j].CreatedAt > list[i].CreatedAt {
				list[i], list[j] = list[j], list[i]
			}
		}
	}
	return list
}

func (h *marketRangeHub) restoreSessionRules(session *marketRangeSession) error {
	if session == nil {
		return nil
	}

	var stored []marketRangeRule
	if err := loadJSONFile(wsRulePersistPath(marketRangeRulesPersistFile), &stored); err != nil {
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
		if item.Lower <= 0 || item.Upper <= 0 || item.Lower >= item.Upper || math.IsNaN(item.Lower) || math.IsInf(item.Lower, 0) || math.IsNaN(item.Upper) || math.IsInf(item.Upper, 0) {
			continue
		}
		ruleID := strings.TrimSpace(item.ID)
		if ruleID == "" {
			ruleID = fmt.Sprintf("%s-restore-%d-%d", symbol, now, i)
		}
		rule := marketRangeRule{
			ID:            ruleID,
			Symbol:        symbol,
			Lower:         item.Lower,
			Upper:         item.Upper,
			CooldownSec:   sanitizeRangeCooldownSec(item.CooldownSec),
			Enabled:       item.Enabled,
			LastPrice:     item.LastPrice,
			LastTriggerAt: item.LastTriggerAt,
			CreatedAt:     item.CreatedAt,
		}
		if item.LastInside != nil {
			b := *item.LastInside
			rule.LastInside = &b
		}
		if rule.CreatedAt <= 0 {
			rule.CreatedAt = now - int64(len(stored)-i)
		}
		session.rules[rule.ID] = &rule
	}
	return nil
}

func (h *marketRangeHub) persistSessionRules(session *marketRangeSession) error {
	if session == nil {
		return nil
	}
	rules := session.snapshotRules()
	h.persistMu.Lock()
	defer h.persistMu.Unlock()
	return saveJSONFile(wsRulePersistPath(marketRangeRulesPersistFile), rules)
}

func sanitizeRangeCooldownSec(v int) int {
	if v <= 0 {
		return marketRangeDefaultCooldownSec
	}
	if v < marketRangeMinCooldownSec {
		return marketRangeMinCooldownSec
	}
	if v > marketRangeMaxCooldownSec {
		return marketRangeMaxCooldownSec
	}
	return v
}

func (s *marketRangeSession) upsertRule(rule marketRangeRule) (subscribe []string, unsubscribe []string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	before := s.enabledSymbolsLocked()

	if old, ok := s.rules[rule.ID]; ok {
		rule.LastPrice = old.LastPrice
		rule.LastTriggerAt = old.LastTriggerAt
		if old.LastInside != nil {
			b := *old.LastInside
			rule.LastInside = &b
		}
		if rule.CreatedAt <= 0 {
			rule.CreatedAt = old.CreatedAt
		}
	}
	if rule.CreatedAt <= 0 {
		rule.CreatedAt = time.Now().UnixMilli()
	}
	s.rules[rule.ID] = &rule

	after := s.enabledSymbolsLocked()
	subscribe = diffRangeSymbols(after, before)
	unsubscribe = diffRangeSymbols(before, after)
	return
}

func (s *marketRangeSession) toggleRule(id string, enabled *bool) (subscribe []string, unsubscribe []string, ok bool) {
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
	return diffRangeSymbols(after, before), diffRangeSymbols(before, after), true
}

func (s *marketRangeSession) removeRule(id string) (subscribe []string, unsubscribe []string, ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, exists := s.rules[id]
	if !exists {
		return nil, nil, false
	}
	before := s.enabledSymbolsLocked()
	delete(s.rules, id)
	after := s.enabledSymbolsLocked()
	return diffRangeSymbols(after, before), diffRangeSymbols(before, after), true
}

func (s *marketRangeSession) clearRules() (unsubscribe []string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	before := s.enabledSymbolsLocked()
	s.rules = make(map[string]*marketRangeRule)
	return diffRangeSymbols(before, map[string]bool{})
}

func (s *marketRangeSession) processPrice(symbol string, price float64, ts int64) []marketRangeEvent {
	if ts <= 0 {
		ts = time.Now().UnixMilli()
	}
	if price <= 0 || math.IsNaN(price) || math.IsInf(price, 0) {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	events := make([]marketRangeEvent, 0)
	for _, rule := range s.rules {
		if !rule.Enabled || rule.Symbol != symbol {
			continue
		}

		inside := price >= rule.Lower && price <= rule.Upper
		rule.LastPrice = price

		if rule.LastInside == nil {
			b := inside
			rule.LastInside = &b
			continue
		}

		cooldownMS := int64(sanitizeRangeCooldownSec(rule.CooldownSec)) * 1000
		if *rule.LastInside && !inside && ts-rule.LastTriggerAt >= cooldownMS {
			direction := "下破"
			if price > rule.Upper {
				direction = "上破"
			}
			rule.LastTriggerAt = ts
			events = append(events, marketRangeEvent{
				ID:        fmt.Sprintf("%s-%d", rule.ID, ts),
				RuleID:    rule.ID,
				Symbol:    symbol,
				Direction: direction,
				Lower:     rule.Lower,
				Upper:     rule.Upper,
				Price:     price,
				Time:      ts,
			})
		}

		b := inside
		rule.LastInside = &b
	}
	return events
}

func (h *marketRangeHub) sendSnapshot(session *marketRangeSession) {
	if session == nil {
		return
	}
	enqueueJSON(session.client, marketRangePayload{
		Channel: "marketRange",
		Type:    "snapshot",
		Time:    time.Now().UnixMilli(),
		Rules:   session.snapshotRules(),
	})
}

func (h *marketRangeHub) sendEvent(session *marketRangeSession, evt marketRangeEvent) {
	if session == nil {
		return
	}
	enqueueJSON(session.client, marketRangePayload{
		Channel: "marketRange",
		Type:    "event",
		Time:    time.Now().UnixMilli(),
		Event:   &evt,
	})
}

func (h *marketRangeHub) sendError(session *marketRangeSession, action, errMsg string) {
	if session == nil {
		return
	}
	enqueueJSON(session.client, marketRangePayload{
		Channel: "marketRange",
		Type:    "error",
		Action:  action,
		Time:    time.Now().UnixMilli(),
		Error:   errMsg,
	})
}

func (h *marketRangeHub) sendAck(session *marketRangeSession, action string) {
	if session == nil {
		return
	}
	enqueueJSON(session.client, marketRangePayload{
		Channel: "marketRange",
		Type:    "ack",
		Action:  action,
		Time:    time.Now().UnixMilli(),
	})
}

func readPumpMarketRange(session *marketRangeSession) {
	client := session.client
	defer client.close()
	defer mRangeHub.removeSession(session)

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
			Action      string  `json:"action"`
			Method      string  `json:"method"`
			ID          string  `json:"id"`
			Symbol      string  `json:"symbol"`
			Lower       float64 `json:"lower"`
			Upper       float64 `json:"upper"`
			CooldownSec int     `json:"cooldownSec"`
			Enabled     *bool   `json:"enabled"`
		}
		if err := json.Unmarshal(message, &req); err != nil {
			mRangeHub.sendError(session, "unknown", "invalid json payload")
			client.conn.SetReadDeadline(time.Now().Add(90 * time.Second))
			continue
		}

		action := strings.ToLower(strings.TrimSpace(chooseValue(req.Action, req.Method)))
		switch action {
		case "ping":
			enqueueJSON(client, marketRangePayload{
				Channel: "marketRange",
				Type:    "pong",
				Time:    time.Now().UnixMilli(),
			})

		case "snapshot", "refresh":
			mRangeHub.sendSnapshot(session)

		case "addrule", "create":
			symbol := strings.ToUpper(strings.TrimSpace(req.Symbol))
			if symbol == "" {
				mRangeHub.sendError(session, action, "symbol is required")
				break
			}
			if req.Lower <= 0 || req.Upper <= 0 || req.Lower >= req.Upper || math.IsNaN(req.Lower) || math.IsInf(req.Lower, 0) || math.IsNaN(req.Upper) || math.IsInf(req.Upper, 0) {
				mRangeHub.sendError(session, action, "invalid range")
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
			rule := marketRangeRule{
				ID:          ruleID,
				Symbol:      symbol,
				Lower:       req.Lower,
				Upper:       req.Upper,
				CooldownSec: sanitizeRangeCooldownSec(req.CooldownSec),
				Enabled:     enabled,
				CreatedAt:   time.Now().UnixMilli(),
			}
			subscribe, unsubscribe := session.upsertRule(rule)
			for _, sym := range unsubscribe {
				mRangeHub.unsubscribeSymbol(sym, session)
			}
			for _, sym := range subscribe {
				mRangeHub.subscribeSymbol(sym, session)
			}
			if err := mRangeHub.persistSessionRules(session); err != nil {
				log.Printf("[WsRange] Persist rules failed: %v", err)
			}
			mRangeHub.sendAck(session, action)
			mRangeHub.sendSnapshot(session)

		case "togglerule":
			ruleID := strings.TrimSpace(req.ID)
			if ruleID == "" {
				mRangeHub.sendError(session, action, "id is required")
				break
			}
			subscribe, unsubscribe, ok := session.toggleRule(ruleID, req.Enabled)
			if !ok {
				mRangeHub.sendError(session, action, "rule not found")
				break
			}
			for _, sym := range unsubscribe {
				mRangeHub.unsubscribeSymbol(sym, session)
			}
			for _, sym := range subscribe {
				mRangeHub.subscribeSymbol(sym, session)
			}
			if err := mRangeHub.persistSessionRules(session); err != nil {
				log.Printf("[WsRange] Persist rules failed: %v", err)
			}
			mRangeHub.sendAck(session, action)
			mRangeHub.sendSnapshot(session)

		case "removerule":
			ruleID := strings.TrimSpace(req.ID)
			if ruleID == "" {
				mRangeHub.sendError(session, action, "id is required")
				break
			}
			_, unsubscribe, ok := session.removeRule(ruleID)
			if !ok {
				mRangeHub.sendError(session, action, "rule not found")
				break
			}
			for _, sym := range unsubscribe {
				mRangeHub.unsubscribeSymbol(sym, session)
			}
			if err := mRangeHub.persistSessionRules(session); err != nil {
				log.Printf("[WsRange] Persist rules failed: %v", err)
			}
			mRangeHub.sendAck(session, action)
			mRangeHub.sendSnapshot(session)

		case "clearrules":
			unsubscribe := session.clearRules()
			for _, sym := range unsubscribe {
				mRangeHub.unsubscribeSymbol(sym, session)
			}
			if err := mRangeHub.persistSessionRules(session); err != nil {
				log.Printf("[WsRange] Persist rules failed: %v", err)
			}
			mRangeHub.sendAck(session, action)
			mRangeHub.sendSnapshot(session)

		default:
			mRangeHub.sendError(session, action, "unsupported action")
		}

		client.conn.SetReadDeadline(time.Now().Add(90 * time.Second))
	}
}
