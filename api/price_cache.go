package api

import (
	"fmt"
	"log"
	"strconv"
	"sync"
	"time"

	"github.com/adshao/go-binance/v2/futures"
)

// PriceCache 价格缓存，通过 WebSocket 实时更新
type PriceCache struct {
	prices map[string]*PriceData // symbol -> price data
	mu     sync.RWMutex

	stopChannels map[string]chan struct{} // symbol -> stop channel
	stopMu       sync.Mutex
}

// PriceData 价格数据
type PriceData struct {
	Symbol      string
	MarkPrice   float64   // 标记价格
	LastUpdate  time.Time // 最后更新时间
}

var priceCache *PriceCache
var priceCacheOnce sync.Once

// GetPriceCache 获取全局价格缓存实例（单例）
func GetPriceCache() *PriceCache {
	priceCacheOnce.Do(func() {
		priceCache = &PriceCache{
			prices:       make(map[string]*PriceData),
			stopChannels: make(map[string]chan struct{}),
		}
	})
	return priceCache
}

// Subscribe 订阅交易对价格（如果尚未订阅）
func (pc *PriceCache) Subscribe(symbol string) error {
	pc.stopMu.Lock()
	defer pc.stopMu.Unlock()

	// 如果已经订阅，直接返回
	if _, exists := pc.stopChannels[symbol]; exists {
		return nil
	}

	// 启动 WebSocket 订阅
	stopC := make(chan struct{})
	pc.stopChannels[symbol] = stopC

	go pc.subscribePrice(symbol, stopC)

	log.Printf("[PriceCache] Subscribed to %s price feed", symbol)
	return nil
}

// subscribePrice 订阅单个交易对的价格
func (pc *PriceCache) subscribePrice(symbol string, stopC chan struct{}) {
	handler := func(event *futures.WsMarkPriceEvent) {
		price, err := strconv.ParseFloat(event.MarkPrice, 64)
		if err != nil {
			log.Printf("[PriceCache] Failed to parse price for %s: %v", symbol, err)
			return
		}

		pc.mu.Lock()
		pc.prices[symbol] = &PriceData{
			Symbol:     symbol,
			MarkPrice:  price,
			LastUpdate: time.Now(),
		}
		pc.mu.Unlock()
	}

	errHandler := func(err error) {
		log.Printf("[PriceCache] WebSocket error for %s: %v", symbol, err)
	}

	doneC, _, err := futures.WsMarkPriceServe(symbol, handler, errHandler)
	if err != nil {
		log.Printf("[PriceCache] Failed to start WebSocket for %s: %v", symbol, err)
		return
	}

	// 等待停止信号或 WebSocket 断开
	select {
	case <-stopC:
		log.Printf("[PriceCache] Stopped subscription for %s", symbol)
	case <-doneC:
		log.Printf("[PriceCache] WebSocket closed for %s", symbol)
		// WebSocket 断开，从停止通道中移除
		pc.stopMu.Lock()
		delete(pc.stopChannels, symbol)
		pc.stopMu.Unlock()
	}
}

// GetPrice 获取交易对的当前价格
// 如果价格不存在或过期（超过 10 秒未更新），会自动订阅
func (pc *PriceCache) GetPrice(symbol string) (float64, error) {
	pc.mu.RLock()
	data, exists := pc.prices[symbol]
	pc.mu.RUnlock()

	// 如果价格存在且新鲜（10 秒内更新）
	if exists && time.Since(data.LastUpdate) < 10*time.Second {
		return data.MarkPrice, nil
	}

	// 价格不存在或过期，触发订阅
	if !exists {
		if err := pc.Subscribe(symbol); err != nil {
			return 0, fmt.Errorf("subscribe to %s: %w", symbol, err)
		}
	}

	// 等待价格更新（最多等待 5 秒）
	timeout := time.After(5 * time.Second)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-timeout:
			return 0, fmt.Errorf("timeout waiting for %s price", symbol)
		case <-ticker.C:
			pc.mu.RLock()
			data, exists = pc.prices[symbol]
			pc.mu.RUnlock()

			if exists && time.Since(data.LastUpdate) < 10*time.Second {
				return data.MarkPrice, nil
			}
		}
	}
}

// Unsubscribe 取消订阅交易对价格
func (pc *PriceCache) Unsubscribe(symbol string) {
	pc.stopMu.Lock()
	defer pc.stopMu.Unlock()

	if stopC, exists := pc.stopChannels[symbol]; exists {
		close(stopC)
		delete(pc.stopChannels, symbol)

		pc.mu.Lock()
		delete(pc.prices, symbol)
		pc.mu.Unlock()

		log.Printf("[PriceCache] Unsubscribed from %s", symbol)
	}
}

// UnsubscribeAll 取消所有订阅
func (pc *PriceCache) UnsubscribeAll() {
	pc.stopMu.Lock()
	defer pc.stopMu.Unlock()

	for symbol, stopC := range pc.stopChannels {
		close(stopC)
		log.Printf("[PriceCache] Unsubscribed from %s", symbol)
	}

	pc.stopChannels = make(map[string]chan struct{})

	pc.mu.Lock()
	pc.prices = make(map[string]*PriceData)
	pc.mu.Unlock()
}

// GetAllPrices 获取所有缓存的价格
func (pc *PriceCache) GetAllPrices() map[string]float64 {
	pc.mu.RLock()
	defer pc.mu.RUnlock()

	result := make(map[string]float64)
	for symbol, data := range pc.prices {
		result[symbol] = data.MarkPrice
	}
	return result
}

// GetSubscribedSymbols 获取已订阅的交易对列表
func (pc *PriceCache) GetSubscribedSymbols() []string {
	pc.stopMu.Lock()
	defer pc.stopMu.Unlock()

	symbols := make([]string, 0, len(pc.stopChannels))
	for symbol := range pc.stopChannels {
		symbols = append(symbols, symbol)
	}
	return symbols
}
