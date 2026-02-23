package api

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/adshao/go-binance/v2/futures"
)

const (
	bigTradeDefaultMinNotional = 100_000.0
	bigTradeReconnectBaseDelay = time.Second
)

type bigTradeMsg struct {
	Channel  string  `json:"channel"`
	Symbol   string  `json:"s"`
	Price    string  `json:"p"`
	Qty      string  `json:"q"`
	Notional float64 `json:"n"`
	Side     string  `json:"side"` // BUY / SELL (主动方向)
	IsMaker  bool    `json:"m"`    // Binance aggTrade m 字段
	TradeID  int64   `json:"tradeId"`
	Time     int64   `json:"t"`
}

type bigTradeHub struct {
	mu    sync.RWMutex
	rooms map[string]*bigTradeRoom
}

type bigTradeRoom struct {
	mu          sync.RWMutex
	key         string
	symbol      string
	minNotional float64
	clients     map[*wsClient]bool
	stopC       chan struct{}
	running     bool
}

var btHub = &bigTradeHub{
	rooms: make(map[string]*bigTradeRoom),
}

func bigTradeRoomKey(symbol string, minNotional float64) string {
	return strings.ToUpper(symbol) + ":" + strconv.FormatFloat(minNotional, 'f', -1, 64)
}

func (h *bigTradeHub) getOrCreateRoom(symbol string, minNotional float64) *bigTradeRoom {
	sym := strings.ToUpper(strings.TrimSpace(symbol))
	key := bigTradeRoomKey(sym, minNotional)

	h.mu.RLock()
	room, ok := h.rooms[key]
	h.mu.RUnlock()
	if ok {
		return room
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	if room, ok = h.rooms[key]; ok {
		return room
	}

	room = &bigTradeRoom{
		key:         key,
		symbol:      sym,
		minNotional: minNotional,
		clients:     make(map[*wsClient]bool),
		stopC:       make(chan struct{}),
	}
	h.rooms[key] = room
	return room
}

func (h *bigTradeHub) subscribe(symbol string, minNotional float64, client *wsClient) string {
	room := h.getOrCreateRoom(symbol, minNotional)

	room.mu.Lock()
	room.clients[client] = true
	needStart := !room.running
	room.running = true
	total := len(room.clients)
	room.mu.Unlock()

	if needStart {
		go h.startStream(room)
	}

	log.Printf("[WsBigTrade] Client subscribed to %s >= %.0f (total: %d)", room.symbol, room.minNotional, total)
	return room.key
}

func (h *bigTradeHub) unsubscribe(roomKey string, client *wsClient) {
	h.mu.RLock()
	room, ok := h.rooms[roomKey]
	h.mu.RUnlock()
	if !ok {
		return
	}

	room.mu.Lock()
	delete(room.clients, client)
	remaining := len(room.clients)
	room.mu.Unlock()

	log.Printf("[WsBigTrade] Client unsubscribed from %s >= %.0f (remaining: %d)", room.symbol, room.minNotional, remaining)

	if remaining == 0 {
		go func() {
			time.Sleep(20 * time.Second)
			room.mu.RLock()
			count := len(room.clients)
			room.mu.RUnlock()
			if count == 0 {
				h.stopRoom(roomKey)
			}
		}()
	}
}

func (h *bigTradeHub) stopRoom(roomKey string) {
	h.mu.Lock()
	room, ok := h.rooms[roomKey]
	if ok {
		delete(h.rooms, roomKey)
	}
	h.mu.Unlock()

	if ok && room.running {
		close(room.stopC)
		log.Printf("[WsBigTrade] Stopped stream for %s >= %.0f", room.symbol, room.minNotional)
	}
}

func (h *bigTradeHub) startStream(room *bigTradeRoom) {
	sym := strings.ToLower(room.symbol)
	backoff := bigTradeReconnectBaseDelay

	for {
		select {
		case <-room.stopC:
			return
		default:
		}

		log.Printf("[WsBigTrade] Connecting to Binance aggTrade for %s >= %.0f", room.symbol, room.minNotional)
		doneC, stopC, err := futures.WsAggTradeServe(sym, func(event *futures.WsAggTradeEvent) {
			price, _ := strconv.ParseFloat(event.Price, 64)
			qty, _ := strconv.ParseFloat(event.Quantity, 64)
			notional := price * qty
			if notional < room.minNotional {
				return
			}

			side := "BUY"
			if event.Maker {
				// buyer is maker => taker side is sell
				side = "SELL"
			}

			payload := bigTradeMsg{
				Channel:  "bigTrade",
				Symbol:   event.Symbol,
				Price:    event.Price,
				Qty:      event.Quantity,
				Notional: notional,
				Side:     side,
				IsMaker:  event.Maker,
				TradeID:  event.AggregateTradeID,
				Time:     event.TradeTime,
			}
			raw, _ := json.Marshal(payload)

			room.mu.RLock()
			clients := make([]*wsClient, 0, len(room.clients))
			for c := range room.clients {
				clients = append(clients, c)
			}
			room.mu.RUnlock()

			for _, c := range clients {
				select {
				case c.sendCh <- raw:
				default:
				}
			}
		}, func(err error) {
			log.Printf("[WsBigTrade] Binance stream error for %s: %v", room.symbol, err)
		})
		if err != nil {
			log.Printf("[WsBigTrade] Connect failed for %s: %v", room.symbol, err)
			select {
			case <-room.stopC:
				return
			case <-time.After(backoff):
			}
			backoff = min(backoff*2, 2*time.Minute)
			continue
		}

		backoff = bigTradeReconnectBaseDelay
		select {
		case <-room.stopC:
			close(stopC)
			return
		case <-doneC:
			log.Printf("[WsBigTrade] Stream disconnected for %s, reconnecting...", room.symbol)
		}

		select {
		case <-room.stopC:
			return
		case <-time.After(backoff):
		}
	}
}

func readPumpBigTrade(client *wsClient, roomKey string) {
	defer client.close()
	defer btHub.unsubscribe(roomKey, client)

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
		if action == "ping" {
			enqueueJSON(client, map[string]any{"action": "pong"})
		}
		client.conn.SetReadDeadline(time.Now().Add(90 * time.Second))
	}
}

func handleWsBigTrade(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if Cfg.Auth.Token != "" && token != Cfg.Auth.Token {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	symbol := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("symbol")))
	if symbol == "" {
		symbol = "BTCUSDT"
	}

	minNotional := bigTradeDefaultMinNotional
	if raw := strings.TrimSpace(r.URL.Query().Get("minNotional")); raw != "" {
		if v, err := strconv.ParseFloat(raw, 64); err == nil && v >= 1000 {
			minNotional = v
		}
	}

	conn, err := wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("[WsBigTrade] Upgrade failed: %v", err)
		return
	}

	client := newWsClient(conn)
	roomKey := btHub.subscribe(symbol, minNotional, client)

	go client.writePump()
	go readPumpBigTrade(client, roomKey)
}
