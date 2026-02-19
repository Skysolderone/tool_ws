package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"log"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const (
	newsRefreshInterval      = 5 * time.Second
	hyperInfoURL             = "https://api.hyperliquid.xyz/info"
	hyperWSURL               = "wss://api.hyperliquid.xyz/ws"
	hyperPingInterval        = 30 * time.Second
	hyperReconnectInterval   = 3 * time.Second
	hyperSnapshotInterval    = 30 * time.Second
	hyperHTTPTimeout         = 12 * time.Second
	newsHTTPTimeout          = 10 * time.Second
	proxyHTTPResponseMaxSize = 2 << 20
)

type newsFeedSource struct {
	Key     string
	Name    string
	URL     string
	Headers map[string]string
}

type newsItem struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Summary string `json:"summary"`
	Link    string `json:"link"`
	PubDate string `json:"pubDate"`
	Source  string `json:"source"`
}

type newsPayload struct {
	Channel  string                `json:"channel"`
	Data     map[string][]newsItem `json:"data,omitempty"`
	Failures []string              `json:"failures,omitempty"`
	Error    string                `json:"error,omitempty"`
	Time     int64                 `json:"t"`
}

type newsHub struct {
	mu      sync.RWMutex
	clients map[*wsClient]bool

	running bool
	stopC   chan struct{}
	kickC   chan struct{}

	lastMsg []byte
}

var (
	newsSources = []newsFeedSource{
		{
			Key:  "blockbeats",
			Name: "BlockBeats",
			URL:  "https://api.theblockbeats.news/v2/rss/newsflash",
			Headers: map[string]string{
				"language": "cn",
			},
		},
		{
			Key:  "0xzx",
			Name: "0xzx",
			URL:  "https://0xzx.com/feed/",
		},
	}

	newsClient = &http.Client{Timeout: newsHTTPTimeout}
	hyperHTTP  = &http.Client{Timeout: hyperHTTPTimeout}

	reTagStrip = regexp.MustCompile(`(?s)<[^>]+>`)
	reItem     = regexp.MustCompile(`(?is)<item[\s\S]*?</item>`)
	reEntry    = regexp.MustCompile(`(?is)<entry[\s\S]*?</entry>`)
	reAtomLink = regexp.MustCompile(`(?is)<link\b[^>]*href=["']([^"']+)["'][^>]*/?>`)
	reAddress  = regexp.MustCompile(`^0x[a-fA-F0-9]{40}$`)

	nHub = &newsHub{
		clients: make(map[*wsClient]bool),
	}
)

func handleWsNews(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if Cfg.Auth.Token != "" && token != Cfg.Auth.Token {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	conn, err := wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("[WsNews] Upgrade failed: %v", err)
		return
	}

	client := newWsClient(conn)
	nHub.subscribe(client)

	go client.writePump()
	go readPumpNews(client)
}

func (h *newsHub) subscribe(client *wsClient) {
	h.mu.Lock()
	h.clients[client] = true
	needStart := !h.running
	last := append([]byte(nil), h.lastMsg...)
	if needStart {
		h.running = true
		h.stopC = make(chan struct{})
		h.kickC = make(chan struct{}, 1)
	}
	total := len(h.clients)
	h.mu.Unlock()

	if len(last) > 0 {
		select {
		case client.sendCh <- last:
		default:
		}
	}

	if needStart {
		go h.run()
	}

	log.Printf("[WsNews] Client subscribed (total: %d)", total)
}

func (h *newsHub) unsubscribe(client *wsClient) {
	h.mu.Lock()
	delete(h.clients, client)
	remaining := len(h.clients)
	h.mu.Unlock()

	log.Printf("[WsNews] Client unsubscribed (remaining: %d)", remaining)

	if remaining == 0 {
		go func() {
			time.Sleep(30 * time.Second)
			h.mu.Lock()
			defer h.mu.Unlock()
			if len(h.clients) != 0 || !h.running {
				return
			}
			close(h.stopC)
			h.running = false
			h.stopC = nil
			h.kickC = nil
			log.Printf("[WsNews] Background fetch loop stopped")
		}()
	}
}

func (h *newsHub) triggerRefresh() {
	h.mu.RLock()
	kickC := h.kickC
	h.mu.RUnlock()
	if kickC == nil {
		return
	}

	select {
	case kickC <- struct{}{}:
	default:
	}
}

func (h *newsHub) run() {
	h.fetchAndBroadcast()

	ticker := time.NewTicker(newsRefreshInterval)
	defer ticker.Stop()

	for {
		h.mu.RLock()
		stopC := h.stopC
		kickC := h.kickC
		h.mu.RUnlock()
		if stopC == nil {
			return
		}

		select {
		case <-stopC:
			return
		case <-ticker.C:
			h.fetchAndBroadcast()
		case <-kickC:
			h.fetchAndBroadcast()
		}
	}
}

func (h *newsHub) fetchAndBroadcast() {
	data, failures, err := fetchNewsSnapshot()
	payload := newsPayload{
		Channel:  "news",
		Data:     data,
		Failures: failures,
		Time:     time.Now().UnixMilli(),
	}
	if err != nil {
		payload.Error = err.Error()
	}

	raw, marshalErr := json.Marshal(payload)
	if marshalErr != nil {
		log.Printf("[WsNews] Marshal payload failed: %v", marshalErr)
		return
	}

	h.mu.Lock()
	h.lastMsg = raw
	clients := make([]*wsClient, 0, len(h.clients))
	for c := range h.clients {
		clients = append(clients, c)
	}
	h.mu.Unlock()

	for _, c := range clients {
		select {
		case c.sendCh <- raw:
		default:
		}
	}
}

func fetchNewsSnapshot() (map[string][]newsItem, []string, error) {
	type fetchResult struct {
		key  string
		list []newsItem
		err  error
	}

	results := make(chan fetchResult, len(newsSources))
	var wg sync.WaitGroup

	for _, source := range newsSources {
		s := source
		wg.Add(1)
		go func() {
			defer wg.Done()

			list, err := fetchNewsFeed(s)
			results <- fetchResult{key: s.Key, list: list, err: err}
		}()
	}

	wg.Wait()
	close(results)

	data := make(map[string][]newsItem, len(newsSources))
	failures := make([]string, 0, len(newsSources))
	success := 0

	for res := range results {
		if res.err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", res.key, res.err))
			continue
		}
		success++
		data[res.key] = normalizeNewsList(res.list)
	}

	for _, source := range newsSources {
		if _, ok := data[source.Key]; !ok {
			data[source.Key] = []newsItem{}
		}
	}

	if success == 0 {
		return data, failures, fmt.Errorf("all news feeds failed")
	}
	return data, failures, nil
}

func fetchNewsFeed(source newsFeedSource) ([]newsItem, error) {
	req, err := http.NewRequest(http.MethodGet, source.URL, nil)
	if err != nil {
		return nil, err
	}
	for k, v := range source.Headers {
		req.Header.Set(k, v)
	}

	resp, err := newsClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, proxyHTTPResponseMaxSize))
	if err != nil {
		return nil, err
	}

	list := parseRSSContent(string(body), source.Name)
	return list, nil
}

func parseRSSContent(xmlText string, defaultSource string) []newsItem {
	items := make([]newsItem, 0, 32)
	blocks := reItem.FindAllString(xmlText, -1)
	for idx, block := range blocks {
		link := pickTag(block, "link")
		guid := pickTag(block, "guid")
		src := pickTag(block, "source")
		author := pickTag(block, "author")
		if author == "" {
			author = pickTag(block, "dc:creator")
		}
		description := pickTag(block, "description")
		if description == "" {
			description = pickTag(block, "content:encoded")
		}

		items = append(items, newsItem{
			ID:      chooseValue(guid, link, fmt.Sprintf("%d", idx)),
			Title:   pickTag(block, "title"),
			Summary: description,
			Link:    chooseValue(link, guid),
			PubDate: pickTag(block, "pubDate"),
			Source:  chooseValue(src, author, defaultSource),
		})
	}

	if len(items) > 0 {
		return items
	}

	// fallback: atom feed
	entries := reEntry.FindAllString(xmlText, -1)
	for idx, block := range entries {
		link := extractAtomLink(block)
		title := pickTag(block, "title")
		summary := chooseValue(pickTag(block, "summary"), pickTag(block, "content"))
		author := pickTag(block, "name")
		if author == "" {
			author = pickTag(block, "author")
		}
		pubDate := chooseValue(pickTag(block, "updated"), pickTag(block, "published"))
		items = append(items, newsItem{
			ID:      chooseValue(link, fmt.Sprintf("%d", idx)),
			Title:   title,
			Summary: summary,
			Link:    link,
			PubDate: pubDate,
			Source:  chooseValue(author, defaultSource),
		})
	}

	return items
}

func extractAtomLink(block string) string {
	match := reAtomLink.FindStringSubmatch(block)
	if len(match) >= 2 {
		return cleanXMLText(match[1])
	}
	return ""
}

func pickTag(block string, tag string) string {
	pattern := regexp.MustCompile(`(?is)<` + regexp.QuoteMeta(tag) + `(?:\\s[^>]*)?>(.*?)</` + regexp.QuoteMeta(tag) + `>`)
	match := pattern.FindStringSubmatch(block)
	if len(match) < 2 {
		return ""
	}
	return cleanXMLText(match[1])
}

func cleanXMLText(raw string) string {
	text := strings.TrimSpace(raw)
	text = strings.TrimPrefix(text, "<![CDATA[")
	text = strings.TrimSuffix(text, "]]>")
	text = reTagStrip.ReplaceAllString(text, " ")
	text = html.UnescapeString(text)
	text = strings.Join(strings.Fields(text), " ")
	return text
}

func chooseValue(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func normalizeNewsList(list []newsItem) []newsItem {
	seen := make(map[string]struct{}, len(list))
	deduped := make([]newsItem, 0, len(list))

	for _, item := range list {
		key := chooseValue(item.Link, item.Title, item.ID)
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		deduped = append(deduped, item)
	}

	sort.SliceStable(deduped, func(i, j int) bool {
		return parseNewsTime(deduped[i].PubDate) > parseNewsTime(deduped[j].PubDate)
	})

	if len(deduped) > 20 {
		return deduped[:20]
	}
	return deduped
}

func parseNewsTime(pubDate string) int64 {
	if pubDate == "" {
		return 0
	}
	layouts := []string{
		time.RFC3339,
		time.RFC3339Nano,
		time.RFC1123Z,
		time.RFC1123,
		time.RFC822Z,
		time.RFC822,
		"Mon, 2 Jan 2006 15:04:05 -0700",
		"2006-01-02 15:04:05",
	}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, pubDate); err == nil {
			return t.UnixMilli()
		}
	}
	return 0
}

func readPumpNews(client *wsClient) {
	defer client.close()
	defer nHub.unsubscribe(client)

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
			nHub.triggerRefresh()
		}

		client.conn.SetReadDeadline(time.Now().Add(90 * time.Second))
	}
}

func handleWsHyperMonitor(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if Cfg.Auth.Token != "" && token != Cfg.Auth.Token {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	address := strings.TrimSpace(r.URL.Query().Get("address"))
	if !reAddress.MatchString(address) {
		http.Error(w, "address is required", http.StatusBadRequest)
		return
	}

	conn, err := wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("[WsHyper] Upgrade failed: %v", err)
		return
	}

	client := newWsClient(conn)
	go client.writePump()
	go runHyperMonitorSession(client, address)
}

func runHyperMonitorSession(client *wsClient, address string) {
	defer client.close()

	snapshotReqC := make(chan struct{}, 1)
	go readPumpHyperClient(client, snapshotReqC)
	go runHyperSnapshotLoop(client, address, snapshotReqC)

	select {
	case snapshotReqC <- struct{}{}:
	default:
	}

	runHyperForwardLoop(client, address)
}

func readPumpHyperClient(client *wsClient, snapshotReqC chan<- struct{}) {
	defer client.close()

	client.conn.SetReadLimit(2048)
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
			select {
			case snapshotReqC <- struct{}{}:
			default:
			}
		}

		client.conn.SetReadDeadline(time.Now().Add(90 * time.Second))
	}
}

func runHyperSnapshotLoop(client *wsClient, address string, snapshotReqC <-chan struct{}) {
	ticker := time.NewTicker(hyperSnapshotInterval)
	defer ticker.Stop()

	for {
		select {
		case <-client.closeCh:
			return
		case <-ticker.C:
			pushHyperSnapshot(client, address)
		case <-snapshotReqC:
			pushHyperSnapshot(client, address)
		}
	}
}

func pushHyperSnapshot(client *wsClient, address string) {
	openOrders, errOpen := fetchHyperInfo(map[string]any{
		"type": "frontendOpenOrders",
		"user": address,
	})
	historyOrders, errHistory := fetchHyperInfo(map[string]any{
		"type": "historicalOrders",
		"user": address,
	})
	fills, errFills := fetchHyperInfo(map[string]any{
		"type":            "userFills",
		"user":            address,
		"aggregateByTime": true,
	})

	hasSuccess := false
	if errOpen == nil {
		hasSuccess = true
		enqueueJSON(client, map[string]any{
			"channel":    "openOrders",
			"isSnapshot": true,
			"data": map[string]any{
				"orders": openOrders,
			},
		})
	}
	if errHistory == nil {
		hasSuccess = true
		enqueueJSON(client, map[string]any{
			"channel":    "orderUpdates",
			"isSnapshot": true,
			"data":       historyOrders,
		})
	}
	if errFills == nil {
		hasSuccess = true
		enqueueJSON(client, map[string]any{
			"channel": "userFills",
			"data": map[string]any{
				"isSnapshot": true,
				"fills":      fills,
			},
		})
	}

	if !hasSuccess {
		enqueueJSON(client, map[string]any{
			"channel": "snapshotError",
			"error": fmt.Sprintf(
				"openOrders=%v, historicalOrders=%v, userFills=%v",
				errOpen,
				errHistory,
				errFills,
			),
		})
	}
}

func fetchHyperInfo(body map[string]any) (any, error) {
	rawBody, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest(http.MethodPost, hyperInfoURL, bytes.NewReader(rawBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := hyperHTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	bodyRaw, err := io.ReadAll(io.LimitReader(resp.Body, proxyHTTPResponseMaxSize))
	if err != nil {
		return nil, err
	}

	var data any
	if err := json.Unmarshal(bodyRaw, &data); err != nil {
		return nil, err
	}
	return data, nil
}

func runHyperForwardLoop(client *wsClient, address string) {
	for {
		select {
		case <-client.closeCh:
			return
		default:
		}

		upstream, _, err := websocket.DefaultDialer.Dial(hyperWSURL, nil)
		if err != nil {
			log.Printf("[WsHyper] Upstream dial failed: %v", err)
			waitOrDone(client.closeCh, hyperReconnectInterval)
			continue
		}

		if err := subscribeHyperChannels(upstream, address); err != nil {
			log.Printf("[WsHyper] Upstream subscribe failed: %v", err)
			upstream.Close()
			waitOrDone(client.closeCh, hyperReconnectInterval)
			continue
		}

		log.Printf("[WsHyper] Upstream connected for %s", address)
		stopPing := make(chan struct{})
		stopCloseWatch := make(chan struct{})

		go func() {
			ticker := time.NewTicker(hyperPingInterval)
			defer ticker.Stop()
			for {
				select {
				case <-stopPing:
					return
				case <-client.closeCh:
					return
				case <-ticker.C:
					upstream.SetWriteDeadline(time.Now().Add(5 * time.Second))
					if err := upstream.WriteJSON(map[string]any{"method": "ping"}); err != nil {
						upstream.Close()
						return
					}
				}
			}
		}()

		go func() {
			select {
			case <-client.closeCh:
				upstream.Close()
			case <-stopCloseWatch:
			}
		}()

		for {
			_, msg, err := upstream.ReadMessage()
			if err != nil {
				break
			}
			select {
			case client.sendCh <- msg:
			default:
			}
		}

		close(stopPing)
		close(stopCloseWatch)
		upstream.Close()
		waitOrDone(client.closeCh, hyperReconnectInterval)
	}
}

func subscribeHyperChannels(conn *websocket.Conn, address string) error {
	subs := []map[string]any{
		{"method": "subscribe", "subscription": map[string]any{"type": "openOrders", "user": address}},
		{"method": "subscribe", "subscription": map[string]any{"type": "orderUpdates", "user": address}},
		{"method": "subscribe", "subscription": map[string]any{"type": "userEvents", "user": address}},
		{"method": "subscribe", "subscription": map[string]any{"type": "userFills", "user": address, "aggregateByTime": true}},
	}
	for _, sub := range subs {
		conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
		if err := conn.WriteJSON(sub); err != nil {
			return err
		}
	}
	return nil
}

func waitOrDone(done <-chan struct{}, d time.Duration) {
	select {
	case <-done:
	case <-time.After(d):
	}
}

func enqueueJSON(client *wsClient, payload any) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return
	}
	select {
	case client.sendCh <- raw:
	default:
	}
}
