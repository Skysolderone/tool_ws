package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strconv"
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

// BookLevel 订单簿档位
type BookLevel struct {
	Price string `json:"p"`
	Qty   string `json:"q"`
}

// BookMsg 推给客户端的订单簿快照消息
type BookMsg struct {
	Type   string      `json:"type"` // 固定 "book"
	Symbol string      `json:"s"`
	Time   int64       `json:"t"`
	Bids   []BookLevel `json:"b"`
	Asks   []BookLevel `json:"a"`
}

var hub = &priceHub{
	symbols: make(map[string]*symbolRoom),
}

// ========== 订单簿转发中心 ==========

type bookHub struct {
	mu      sync.RWMutex
	symbols map[string]*bookRoom
}

type bookRoom struct {
	mu       sync.RWMutex
	key      string
	symbol   string
	levels   int
	clients  map[*wsClient]bool
	stopC    chan struct{}
	running  bool
	lastBook *BookMsg
}

var obHub = &bookHub{
	symbols: make(map[string]*bookRoom),
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
	total := len(room.clients)

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

	log.Printf("[WsProxy] Client subscribed to %s (total: %d)", room.symbol, total)
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

// readPumpBook 读取客户端消息（仅心跳），并在断开时清理订单簿订阅
func (c *wsClient) readPumpBook(roomKey string) {
	defer c.close()
	defer obHub.unsubscribe(roomKey, c)

	c.conn.SetReadLimit(512)
	c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			break
		}

		var req struct {
			Action string `json:"action"`
		}
		if json.Unmarshal(message, &req) != nil {
			c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
			continue
		}

		if req.Action == "ping" {
			pong, _ := json.Marshal(map[string]string{"action": "pong"})
			select {
			case c.sendCh <- pong:
			default:
			}
		}

		c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	}
}

type localOrderBook struct {
	lastUpdateID int64
	bids         map[string]string // price -> qty
	asks         map[string]string // price -> qty
}

func newLocalOrderBook(snapshot *futures.DepthResponse) *localOrderBook {
	ob := &localOrderBook{
		lastUpdateID: snapshot.LastUpdateID,
		bids:         make(map[string]string, len(snapshot.Bids)),
		asks:         make(map[string]string, len(snapshot.Asks)),
	}
	for _, b := range snapshot.Bids {
		if b.Price == "" {
			continue
		}
		if b.Quantity == "0" || b.Quantity == "0.0" || b.Quantity == "" {
			continue
		}
		ob.bids[b.Price] = b.Quantity
	}
	for _, a := range snapshot.Asks {
		if a.Price == "" {
			continue
		}
		if a.Quantity == "0" || a.Quantity == "0.0" || a.Quantity == "" {
			continue
		}
		ob.asks[a.Price] = a.Quantity
	}
	return ob
}

func applySideUpdates(side map[string]string, updates []futures.Bid) {
	for _, lv := range updates {
		price := lv.Price
		qty := lv.Quantity
		if price == "" {
			continue
		}
		if qty == "" || qty == "0" || qty == "0.0" {
			delete(side, price)
			continue
		}
		side[price] = qty
	}
}

func (ob *localOrderBook) applyEvent(event *futures.WsDepthEvent) {
	applySideUpdates(ob.bids, event.Bids)
	applySideUpdates(ob.asks, event.Asks)
	ob.lastUpdateID = event.LastUpdateID
}

type pricedLevel struct {
	price float64
	qty   string
	raw   string
}

func topLevels(side map[string]string, levels int, desc bool) []BookLevel {
	if levels <= 0 {
		return nil
	}
	items := make([]pricedLevel, 0, len(side))
	for rawPrice, qty := range side {
		p, err := strconv.ParseFloat(rawPrice, 64)
		if err != nil || p <= 0 {
			continue
		}
		q, err := strconv.ParseFloat(qty, 64)
		if err != nil || q <= 0 {
			continue
		}
		items = append(items, pricedLevel{
			price: p,
			qty:   qty,
			raw:   rawPrice,
		})
	}

	sort.Slice(items, func(i, j int) bool {
		if desc {
			return items[i].price > items[j].price
		}
		return items[i].price < items[j].price
	})

	if len(items) > levels {
		items = items[:levels]
	}

	out := make([]BookLevel, 0, len(items))
	for _, it := range items {
		out = append(out, BookLevel{
			Price: it.raw,
			Qty:   it.qty,
		})
	}
	return out
}

func (ob *localOrderBook) toBookMsg(symbol string, levels int, ts int64) *BookMsg {
	if ts == 0 {
		ts = time.Now().UnixMilli()
	}
	return &BookMsg{
		Type:   "book",
		Symbol: symbol,
		Time:   ts,
		Bids:   topLevels(ob.bids, levels, true),
		Asks:   topLevels(ob.asks, levels, false),
	}
}

func cloneDepthEvent(event *futures.WsDepthEvent) *futures.WsDepthEvent {
	if event == nil {
		return nil
	}
	cp := *event
	if len(event.Bids) > 0 {
		cp.Bids = append([]futures.Bid(nil), event.Bids...)
	}
	if len(event.Asks) > 0 {
		cp.Asks = append([]futures.Ask(nil), event.Asks...)
	}
	return &cp
}

func fetchDepthSnapshot(symbol string, limit int) (*futures.DepthResponse, error) {
	if Client == nil {
		return nil, fmt.Errorf("client not initialized")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	return Client.NewDepthService().
		Symbol(symbol).
		Limit(limit).
		Do(ctx)
}

func (h *bookHub) broadcastBook(room *bookRoom, msg *BookMsg) {
	room.mu.Lock()
	room.lastBook = msg
	clients := make([]*wsClient, 0, len(room.clients))
	for c := range room.clients {
		clients = append(clients, c)
	}
	room.mu.Unlock()

	raw, _ := json.Marshal(msg)
	for _, c := range clients {
		select {
		case c.sendCh <- raw:
		default:
		}
	}
}

func normalizeBookLevels(levels int) int {
	if levels == 5 || levels == 10 || levels == 20 || levels == 50 || levels == 100 || levels == 500 || levels == 1000 {
		return levels
	}
	return 20
}

func bookRoomKey(symbol string, levels int) string {
	return fmt.Sprintf("%s:%d", strings.ToUpper(symbol), normalizeBookLevels(levels))
}

// getOrCreateRoom 获取或创建订单簿房间
func (h *bookHub) getOrCreateRoom(symbol string, levels int) *bookRoom {
	sym := strings.ToUpper(symbol)
	lv := normalizeBookLevels(levels)
	key := bookRoomKey(sym, lv)

	h.mu.RLock()
	room, ok := h.symbols[key]
	h.mu.RUnlock()
	if ok {
		return room
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	if room, ok = h.symbols[key]; ok {
		return room
	}

	room = &bookRoom{
		key:     key,
		symbol:  sym,
		levels:  lv,
		clients: make(map[*wsClient]bool),
		stopC:   make(chan struct{}),
	}
	h.symbols[key] = room
	return room
}

// subscribe 客户端订阅某 symbol 的订单簿
func (h *bookHub) subscribe(symbol string, levels int, client *wsClient) string {
	room := h.getOrCreateRoom(symbol, levels)

	room.mu.Lock()
	room.clients[client] = true
	needStart := !room.running
	room.running = true
	total := len(room.clients)
	lastBook := room.lastBook
	room.mu.Unlock()

	if lastBook != nil {
		msg, _ := json.Marshal(lastBook)
		select {
		case client.sendCh <- msg:
		default:
		}
	}

	if needStart {
		go h.startBookStream(room)
	}

	log.Printf("[WsBook] Client subscribed to %s (%d levels, total: %d)", room.symbol, room.levels, total)
	return room.key
}

// unsubscribe 客户端取消订单簿订阅
func (h *bookHub) unsubscribe(roomKey string, client *wsClient) {
	h.mu.RLock()
	room, ok := h.symbols[roomKey]
	h.mu.RUnlock()
	if !ok {
		return
	}

	room.mu.Lock()
	delete(room.clients, client)
	remaining := len(room.clients)
	room.mu.Unlock()

	log.Printf("[WsBook] Client unsubscribed from %s (%d levels, remaining: %d)", room.symbol, room.levels, remaining)

	if remaining == 0 {
		go func() {
			time.Sleep(30 * time.Second)
			room.mu.RLock()
			count := len(room.clients)
			room.mu.RUnlock()
			if count == 0 {
				h.stopRoom(roomKey)
			}
		}()
	}
}

// stopRoom 停止某 symbol 的订单簿流
func (h *bookHub) stopRoom(roomKey string) {
	h.mu.Lock()
	room, ok := h.symbols[roomKey]
	if ok {
		delete(h.symbols, roomKey)
	}
	h.mu.Unlock()

	if ok && room.running {
		close(room.stopC)
		log.Printf("[WsBook] Stopped orderbook stream for %s (%d levels)", room.symbol, room.levels)
	}
}

// startBookStream 连接币安 diff depth，并按官方步骤维护本地订单簿后广播
func (h *bookHub) startBookStream(room *bookRoom) {
	sym := strings.ToLower(room.symbol)
	backoff := time.Second

	for {
		select {
		case <-room.stopC:
			return
		default:
		}

		log.Printf("[WsBook] Connecting to Binance diff depth for %s (%d levels)", room.symbol, room.levels)

		eventCh := make(chan *futures.WsDepthEvent, 4096)
		droppedCh := make(chan struct{}, 1)

		doneC, stopC, err := futures.WsDiffDepthServe(sym, func(event *futures.WsDepthEvent) {
			cp := cloneDepthEvent(event)
			select {
			case eventCh <- cp:
			default:
				// 队列溢出，触发重同步
				select {
				case droppedCh <- struct{}{}:
				default:
				}
			}
		}, func(err error) {
			log.Printf("[WsBook] Binance stream error for %s (%d levels): %v", room.symbol, room.levels, err)
		})

		if err != nil {
			log.Printf("[WsBook] Failed to connect Binance for %s (%d levels): %v, retry in %v", room.symbol, room.levels, err, backoff)
			select {
			case <-room.stopC:
				return
			case <-time.After(backoff):
			}
			backoff = min(backoff*2, 2*time.Minute)
			continue
		}

		stopped := false
		stopStream := func() {
			if !stopped {
				close(stopC)
				stopped = true
			}
		}

		// 先拉一次快照，后续用增量做精确同步
		snapshot, err := fetchDepthSnapshot(room.symbol, 1000)
		if err != nil {
			log.Printf("[WsBook] Fetch depth snapshot failed for %s: %v", room.symbol, err)
			stopStream()
			select {
			case <-room.stopC:
				return
			case <-time.After(backoff):
			}
			backoff = min(backoff*2, 2*time.Minute)
			continue
		}

		ob := newLocalOrderBook(snapshot)
		synced := false
		needResync := false

		// 等待第一条可桥接事件：U <= lastUpdateId <= u
		for !synced && !needResync {
			select {
			case <-room.stopC:
				stopStream()
				return
			case <-doneC:
				needResync = true
			case <-droppedCh:
				log.Printf("[WsBook] Event queue overflow before sync for %s, resyncing", room.symbol)
				needResync = true
			case event := <-eventCh:
				if event == nil {
					continue
				}

				// 丢弃过期事件
				if event.LastUpdateID < ob.lastUpdateID {
					continue
				}

				// 快照太旧，无法桥接
				if event.FirstUpdateID > ob.lastUpdateID {
					log.Printf("[WsBook] Snapshot too old for %s: first U=%d > lastUpdateId=%d, resyncing",
						room.symbol, event.FirstUpdateID, ob.lastUpdateID)
					needResync = true
					continue
				}

				if event.FirstUpdateID <= ob.lastUpdateID && ob.lastUpdateID <= event.LastUpdateID {
					ob.applyEvent(event)
					synced = true
					h.broadcastBook(room, ob.toBookMsg(room.symbol, room.levels, event.Time))
				}
			case <-time.After(10 * time.Second):
				log.Printf("[WsBook] Wait first bridge event timeout for %s, resyncing", room.symbol)
				needResync = true
			}
		}

		if !synced {
			stopStream()
			select {
			case <-room.stopC:
				return
			case <-time.After(backoff):
			}
			backoff = min(backoff*2, 2*time.Minute)
			continue
		}

		backoff = time.Second
		streamAlive := true

		for streamAlive {
			select {
			case <-room.stopC:
				stopStream()
				return
			case <-doneC:
				log.Printf("[WsBook] Binance stream disconnected for %s (%d levels), reconnecting...", room.symbol, room.levels)
				streamAlive = false
			case <-droppedCh:
				log.Printf("[WsBook] Event queue overflow for %s, resyncing local orderbook", room.symbol)
				streamAlive = false
			case event := <-eventCh:
				if event == nil {
					continue
				}

				// 丢弃过期事件
				if event.LastUpdateID < ob.lastUpdateID {
					continue
				}

				// 关键连续性校验：pu 必须等于上一条 u
				if event.PrevLastUpdateID != ob.lastUpdateID {
					log.Printf("[WsBook] Sequence gap for %s: pu=%d, expected=%d, resyncing",
						room.symbol, event.PrevLastUpdateID, ob.lastUpdateID)
					streamAlive = false
					continue
				}

				ob.applyEvent(event)
				h.broadcastBook(room, ob.toBookMsg(room.symbol, room.levels, event.Time))
			}
		}

		stopStream()
		select {
		case <-room.stopC:
			return
		case <-time.After(backoff):
		}
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

// handleWsBook HTTP handler — 实时订单簿 WebSocket
func handleWsBook(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if Cfg.Auth.Token != "" && token != Cfg.Auth.Token {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	symbol := strings.ToUpper(r.URL.Query().Get("symbol"))
	if symbol == "" {
		http.Error(w, "symbol is required", http.StatusBadRequest)
		return
	}

	levels := 20
	levelsStr := r.URL.Query().Get("levels")
	if levelsStr != "" {
		v, err := strconv.Atoi(levelsStr)
		if err != nil || normalizeBookLevels(v) != v {
			http.Error(w, "levels must be one of 5,10,20,50,100,500,1000", http.StatusBadRequest)
			return
		}
		levels = v
	}

	conn, err := wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("[WsBook] Upgrade failed: %v", err)
		return
	}

	client := newWsClient(conn)
	roomKey := obHub.subscribe(symbol, levels, client)

	go client.writePump()
	go client.readPumpBook(roomKey)
}

// StartWsPriceServer 启动 WebSocket 价格转发服务器
// 在 Hertz 同端口的 /ws/price 路径上监听
func StartWsPriceServer(port int) {
	mux := http.NewServeMux()
	mux.HandleFunc("/ws/price", handleWsPrice)
	mux.HandleFunc("/ws/book", handleWsBook)
	mux.HandleFunc("/ws/news", handleWsNews)
	mux.HandleFunc("/ws/hyper-monitor", handleWsHyperMonitor)

	addr := fmt.Sprintf("0.0.0.0:%d", port)
	log.Printf("[WsProxy] Price WebSocket server starting on %s", addr)

	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Printf("[WsProxy] Server error: %v", err)
	}
}
