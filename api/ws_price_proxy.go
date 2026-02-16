package api

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/adshao/go-binance/v2/futures"
	"github.com/gorilla/websocket"
)

// ========== 价格转发中心 ==========
// 后端订阅币安 aggTrade，转发给所有连接的 app 客户端
// app 不再直连币安，解决国内网络问题

// priceHub 管理所有 symbol 的价格订阅和客户端连接
type priceHub struct {
	mu      sync.RWMutex
	symbols map[string]*symbolRoom // key: symbol (大写)
}

// symbolRoom 单个交易对的房间
type symbolRoom struct {
	mu        sync.RWMutex
	symbol    string
	clients   map[*wsClient]bool
	stopC     chan struct{}
	lastPrice string
	running   bool
}

// wsClient 一个 WebSocket 客户端
type wsClient struct {
	conn          *websocket.Conn
	sendCh        chan []byte
	closeCh       chan struct{}
	once          sync.Once
	initialSymbol string // 通过 URL 参数初始订阅的 symbol
}

// PriceMsg 推给客户端的价格消息
type PriceMsg struct {
	Symbol string `json:"s"`
	Price  string `json:"p"`
	Time   int64  `json:"t"`
}

var hub = &priceHub{
	symbols: make(map[string]*symbolRoom),
}

// getOrCreateRoom 获取或创建 symbol 房间
func (h *priceHub) getOrCreateRoom(symbol string) *symbolRoom {
	sym := strings.ToUpper(symbol)

	h.mu.RLock()
	room, ok := h.symbols[sym]
	h.mu.RUnlock()
	if ok {
		return room
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	// double check
	if room, ok = h.symbols[sym]; ok {
		return room
	}

	room = &symbolRoom{
		symbol:  sym,
		clients: make(map[*wsClient]bool),
		stopC:   make(chan struct{}),
	}
	h.symbols[sym] = room
	return room
}

// subscribe 客户端订阅某 symbol
func (h *priceHub) subscribe(symbol string, client *wsClient) {
	room := h.getOrCreateRoom(symbol)

	room.mu.Lock()
	room.clients[client] = true
	needStart := !room.running
	room.running = true

	// 如果有最新价格，立即推送给新客户端
	lastPrice := room.lastPrice
	room.mu.Unlock()

	if lastPrice != "" {
		msg, _ := json.Marshal(PriceMsg{
			Symbol: room.symbol,
			Price:  lastPrice,
			Time:   time.Now().UnixMilli(),
		})
		select {
		case client.sendCh <- msg:
		default:
		}
	}

	// 首个客户端加入时启动币安订阅
	if needStart {
		go h.startBinanceStream(room)
	}

	log.Printf("[WsProxy] Client subscribed to %s (total: %d)", room.symbol, len(room.clients))
}

// unsubscribe 客户端取消订阅
func (h *priceHub) unsubscribe(symbol string, client *wsClient) {
	sym := strings.ToUpper(symbol)

	h.mu.RLock()
	room, ok := h.symbols[sym]
	h.mu.RUnlock()
	if !ok {
		return
	}

	room.mu.Lock()
	delete(room.clients, client)
	remaining := len(room.clients)
	room.mu.Unlock()

	log.Printf("[WsProxy] Client unsubscribed from %s (remaining: %d)", sym, remaining)

	// 没有客户端了，停止币安订阅（延迟 30 秒，避免频繁开关）
	if remaining == 0 {
		go func() {
			time.Sleep(30 * time.Second)
			room.mu.RLock()
			count := len(room.clients)
			room.mu.RUnlock()
			if count == 0 {
				h.stopRoom(sym)
			}
		}()
	}
}

// stopRoom 停止某 symbol 的币安订阅
func (h *priceHub) stopRoom(symbol string) {
	h.mu.Lock()
	room, ok := h.symbols[symbol]
	if ok {
		delete(h.symbols, symbol)
	}
	h.mu.Unlock()

	if ok && room.running {
		close(room.stopC)
		log.Printf("[WsProxy] Stopped Binance stream for %s", symbol)
	}
}

// startBinanceStream 连接币安 aggTrade 并广播给所有客户端
func (h *priceHub) startBinanceStream(room *symbolRoom) {
	sym := strings.ToLower(room.symbol)
	backoff := time.Second

	for {
		select {
		case <-room.stopC:
			return
		default:
		}

		log.Printf("[WsProxy] Connecting to Binance aggTrade for %s", room.symbol)

		doneC, stopC, err := futures.WsAggTradeServe(sym, func(event *futures.WsAggTradeEvent) {
			room.mu.Lock()
			room.lastPrice = event.Price
			clients := make([]*wsClient, 0, len(room.clients))
			for c := range room.clients {
				clients = append(clients, c)
			}
			room.mu.Unlock()

			msg, _ := json.Marshal(PriceMsg{
				Symbol: event.Symbol,
				Price:  event.Price,
				Time:   event.Time,
			})

			for _, c := range clients {
				select {
				case c.sendCh <- msg:
				default:
					// 发送缓冲满，跳过（避免阻塞）
				}
			}
		}, func(err error) {
			log.Printf("[WsProxy] Binance stream error for %s: %v", room.symbol, err)
		})

		if err != nil {
			log.Printf("[WsProxy] Failed to connect Binance for %s: %v, retry in %v", room.symbol, err, backoff)
			select {
			case <-room.stopC:
				return
			case <-time.After(backoff):
			}
			backoff = min(backoff*2, 2*time.Minute)
			continue
		}

		// 连接成功，重置 backoff
		backoff = time.Second

		select {
		case <-room.stopC:
			close(stopC)
			return
		case <-doneC:
			log.Printf("[WsProxy] Binance stream disconnected for %s, reconnecting...", room.symbol)
		}

		select {
		case <-room.stopC:
			return
		case <-time.After(backoff):
		}
	}
}

// ========== 客户端管理 ==========

func newWsClient(conn *websocket.Conn) *wsClient {
	return &wsClient{
		conn:    conn,
		sendCh:  make(chan []byte, 64),
		closeCh: make(chan struct{}),
	}
}

func (c *wsClient) close() {
	c.once.Do(func() {
		close(c.closeCh)
		c.conn.Close()
	})
}

// writePump 从 sendCh 读取消息写入 WebSocket
func (c *wsClient) writePump() {
	ticker := time.NewTicker(30 * time.Second)
	defer func() {
		ticker.Stop()
		c.close()
	}()

	for {
		select {
		case msg, ok := <-c.sendCh:
			if !ok {
				return
			}
			c.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
			if err := c.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				return
			}
		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		case <-c.closeCh:
			return
		}
	}
}

// readPump 读取客户端消息（处理 subscribe/unsubscribe + 心跳）
func (c *wsClient) readPump() {
	defer c.close()

	c.conn.SetReadLimit(512)
	c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	// 初始订阅（通过 URL 参数）
	currentSymbol := c.initialSymbol

	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			break
		}

		// 解析客户端消息: {"action": "subscribe", "symbol": "ETHUSDT"}
		var req struct {
			Action string `json:"action"`
			Symbol string `json:"symbol"`
		}
		if json.Unmarshal(message, &req) != nil {
			continue
		}

		switch req.Action {
		case "subscribe":
			if currentSymbol != "" {
				hub.unsubscribe(currentSymbol, c)
			}
			currentSymbol = strings.ToUpper(req.Symbol)
			hub.subscribe(currentSymbol, c)

		case "unsubscribe":
			if currentSymbol != "" {
				hub.unsubscribe(currentSymbol, c)
				currentSymbol = ""
			}

		case "ping":
			pong, _ := json.Marshal(map[string]string{"action": "pong"})
			select {
			case c.sendCh <- pong:
			default:
			}
		}

		c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	}

	// 断开时清理订阅
	if currentSymbol != "" {
		hub.unsubscribe(currentSymbol, c)
	}
}

// ========== Upgrader ==========

var wsUpgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin:     func(r *http.Request) bool { return true },
}

// handleWsPrice HTTP handler — 升级连接为 WebSocket
func handleWsPrice(w http.ResponseWriter, r *http.Request) {
	// Token 校验（从 query 参数获取）
	token := r.URL.Query().Get("token")
	if Cfg.Auth.Token != "" && token != Cfg.Auth.Token {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	conn, err := wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("[WsProxy] Upgrade failed: %v", err)
		return
	}

	client := newWsClient(conn)

	// 如果 URL 带了 symbol 参数，直接订阅
	symbol := strings.ToUpper(r.URL.Query().Get("symbol"))
	if symbol != "" {
		hub.subscribe(symbol, client)
		client.initialSymbol = symbol
	}

	go client.writePump()
	go client.readPump()
}

// StartWsPriceServer 启动 WebSocket 价格转发服务器
// 在 Hertz 同端口的 /ws/price 路径上监听
func StartWsPriceServer(port int) {
	mux := http.NewServeMux()
	mux.HandleFunc("/ws/price", handleWsPrice)

	addr := fmt.Sprintf("0.0.0.0:%d", port)
	log.Printf("[WsProxy] Price WebSocket server starting on %s", addr)

	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Printf("[WsProxy] Server error: %v", err)
	}
}

