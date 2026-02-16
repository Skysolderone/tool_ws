package api

import (
	"testing"
	"time"
)

// --- 价格缓存测试 ---

func TestPriceCache_Singleton(t *testing.T) {
	// 验证单例模式
	cache1 := GetPriceCache()
	cache2 := GetPriceCache()

	if cache1 != cache2 {
		t.Error("PriceCache should be singleton")
	}

	t.Log("PriceCache singleton verified")
}

func TestPriceData_Structure(t *testing.T) {
	// 验证 PriceData 结构
	data := &PriceData{
		Symbol:     "BTCUSDT",
		MarkPrice:  43000.0,
		LastUpdate: time.Now(),
	}

	if data.Symbol != "BTCUSDT" {
		t.Errorf("expected symbol BTCUSDT, got %s", data.Symbol)
	}
	if data.MarkPrice != 43000.0 {
		t.Errorf("expected price 43000.0, got %.2f", data.MarkPrice)
	}
	if time.Since(data.LastUpdate) > time.Second {
		t.Error("LastUpdate should be recent")
	}

	t.Logf("PriceData: Symbol=%s, Price=%.2f, Age=%v",
		data.Symbol, data.MarkPrice, time.Since(data.LastUpdate))
}

func TestPriceCache_GetAllPrices(t *testing.T) {
	cache := GetPriceCache()

	// 模拟添加价格数据
	cache.mu.Lock()
	cache.prices["BTCUSDT"] = &PriceData{
		Symbol:     "BTCUSDT",
		MarkPrice:  43000.0,
		LastUpdate: time.Now(),
	}
	cache.prices["ETHUSDT"] = &PriceData{
		Symbol:     "ETHUSDT",
		MarkPrice:  2300.0,
		LastUpdate: time.Now(),
	}
	cache.mu.Unlock()

	prices := cache.GetAllPrices()
	if len(prices) != 2 {
		t.Errorf("expected 2 prices, got %d", len(prices))
	}

	if prices["BTCUSDT"] != 43000.0 {
		t.Errorf("expected BTCUSDT price 43000.0, got %.2f", prices["BTCUSDT"])
	}
	if prices["ETHUSDT"] != 2300.0 {
		t.Errorf("expected ETHUSDT price 2300.0, got %.2f", prices["ETHUSDT"])
	}

	// 清理
	cache.mu.Lock()
	delete(cache.prices, "BTCUSDT")
	delete(cache.prices, "ETHUSDT")
	cache.mu.Unlock()

	t.Logf("GetAllPrices returned %d prices", len(prices))
}

func TestPriceCache_GetSubscribedSymbols(t *testing.T) {
	cache := GetPriceCache()

	// 模拟订阅
	cache.stopMu.Lock()
	cache.stopChannels["BTCUSDT"] = make(chan struct{})
	cache.stopChannels["ETHUSDT"] = make(chan struct{})
	cache.stopMu.Unlock()

	symbols := cache.GetSubscribedSymbols()
	if len(symbols) != 2 {
		t.Errorf("expected 2 subscribed symbols, got %d", len(symbols))
	}

	// 清理
	cache.stopMu.Lock()
	for _, ch := range cache.stopChannels {
		close(ch)
	}
	cache.stopChannels = make(map[string]chan struct{})
	cache.stopMu.Unlock()

	t.Logf("GetSubscribedSymbols returned %d symbols", len(symbols))
}

func TestPriceData_Freshness(t *testing.T) {
	// 测试价格新鲜度判断
	tests := []struct {
		name    string
		age     time.Duration
		isFresh bool
	}{
		{
			name:    "刚更新",
			age:     0,
			isFresh: true,
		},
		{
			name:    "5 秒前",
			age:     5 * time.Second,
			isFresh: true,
		},
		{
			name:    "9 秒前",
			age:     9 * time.Second,
			isFresh: true,
		},
		{
			name:    "11 秒前",
			age:     11 * time.Second,
			isFresh: false,
		},
		{
			name:    "1 分钟前",
			age:     time.Minute,
			isFresh: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := &PriceData{
				Symbol:     "BTCUSDT",
				MarkPrice:  43000.0,
				LastUpdate: time.Now().Add(-tt.age),
			}

			isFresh := time.Since(data.LastUpdate) < 10*time.Second
			if isFresh != tt.isFresh {
				t.Errorf("expected isFresh=%v for age %v, got %v", tt.isFresh, tt.age, isFresh)
			}

			t.Logf("Age: %v, Fresh: %v", tt.age, isFresh)
		})
	}
}

func TestPriceCache_ConcurrentAccess(t *testing.T) {
	cache := GetPriceCache()

	// 并发写入
	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func(id int) {
			cache.mu.Lock()
			cache.prices["TEST"+string(rune(id))] = &PriceData{
				Symbol:     "TEST",
				MarkPrice:  float64(id * 1000),
				LastUpdate: time.Now(),
			}
			cache.mu.Unlock()
			done <- true
		}(i)
	}

	// 等待所有写入完成
	for i := 0; i < 10; i++ {
		<-done
	}

	// 并发读取
	for i := 0; i < 10; i++ {
		go func() {
			prices := cache.GetAllPrices()
			t.Logf("Read %d prices", len(prices))
			done <- true
		}()
	}

	// 等待所有读取完成
	for i := 0; i < 10; i++ {
		<-done
	}

	// 清理
	cache.mu.Lock()
	for key := range cache.prices {
		if len(key) > 4 && key[:4] == "TEST" {
			delete(cache.prices, key)
		}
	}
	cache.mu.Unlock()

	t.Log("Concurrent access test completed")
}

func TestPriceCache_SubscribeLogic(t *testing.T) {
	// 测试订阅逻辑（不实际连接 WebSocket）
	cache := &PriceCache{
		prices:       make(map[string]*PriceData),
		stopChannels: make(map[string]chan struct{}),
	}

	// 模拟订阅
	symbol := "TESTUSDT"
	cache.stopMu.Lock()
	if _, exists := cache.stopChannels[symbol]; !exists {
		cache.stopChannels[symbol] = make(chan struct{})
	}
	cache.stopMu.Unlock()

	// 验证订阅成功
	symbols := cache.GetSubscribedSymbols()
	found := false
	for _, s := range symbols {
		if s == symbol {
			found = true
			break
		}
	}

	if !found {
		t.Errorf("expected %s in subscribed symbols", symbol)
	}

	// 清理
	cache.stopMu.Lock()
	if ch, exists := cache.stopChannels[symbol]; exists {
		close(ch)
		delete(cache.stopChannels, symbol)
	}
	cache.stopMu.Unlock()

	t.Logf("Subscribe logic test completed for %s", symbol)
}

func TestPriceCache_UnsubscribeLogic(t *testing.T) {
	cache := &PriceCache{
		prices:       make(map[string]*PriceData),
		stopChannels: make(map[string]chan struct{}),
	}

	// 模拟订阅
	symbol := "TESTUSDT"
	cache.stopMu.Lock()
	cache.stopChannels[symbol] = make(chan struct{})
	cache.stopMu.Unlock()

	cache.mu.Lock()
	cache.prices[symbol] = &PriceData{
		Symbol:     symbol,
		MarkPrice:  50000.0,
		LastUpdate: time.Now(),
	}
	cache.mu.Unlock()

	// 取消订阅
	cache.Unsubscribe(symbol)

	// 验证已移除
	symbols := cache.GetSubscribedSymbols()
	for _, s := range symbols {
		if s == symbol {
			t.Errorf("expected %s to be unsubscribed", symbol)
		}
	}

	cache.mu.RLock()
	_, exists := cache.prices[symbol]
	cache.mu.RUnlock()

	if exists {
		t.Errorf("expected %s price to be removed", symbol)
	}

	t.Log("Unsubscribe logic test completed")
}

func TestPriceCache_MultipleSymbols(t *testing.T) {
	cache := &PriceCache{
		prices:       make(map[string]*PriceData),
		stopChannels: make(map[string]chan struct{}),
	}

	symbols := []string{"BTCUSDT", "ETHUSDT", "BNBUSDT"}

	// 模拟订阅多个交易对
	for _, symbol := range symbols {
		cache.stopMu.Lock()
		cache.stopChannels[symbol] = make(chan struct{})
		cache.stopMu.Unlock()

		cache.mu.Lock()
		cache.prices[symbol] = &PriceData{
			Symbol:     symbol,
			MarkPrice:  float64(len(symbol) * 1000), // 模拟价格
			LastUpdate: time.Now(),
		}
		cache.mu.Unlock()
	}

	// 验证所有订阅
	subscribedSymbols := cache.GetSubscribedSymbols()
	if len(subscribedSymbols) != len(symbols) {
		t.Errorf("expected %d subscriptions, got %d", len(symbols), len(subscribedSymbols))
	}

	// 取消所有订阅
	cache.UnsubscribeAll()

	// 验证已清空
	if len(cache.GetSubscribedSymbols()) != 0 {
		t.Error("expected all subscriptions to be removed")
	}

	if len(cache.GetAllPrices()) != 0 {
		t.Error("expected all prices to be removed")
	}

	t.Log("Multiple symbols test completed")
}

func TestPriceData_TimestampValidation(t *testing.T) {
	// 测试时间戳相关的边界条件
	now := time.Now()

	tests := []struct {
		name       string
		updateTime time.Time
		wantFresh  bool
	}{
		{
			name:       "当前时间",
			updateTime: now,
			wantFresh:  true,
		},
		{
			name:       "1 秒前",
			updateTime: now.Add(-1 * time.Second),
			wantFresh:  true,
		},
		{
			name:       "临界点 9.9 秒",
			updateTime: now.Add(-9900 * time.Millisecond),
			wantFresh:  true,
		},
		{
			name:       "刚过期 10.1 秒",
			updateTime: now.Add(-10100 * time.Millisecond),
			wantFresh:  false,
		},
		{
			name:       "未来时间",
			updateTime: now.Add(1 * time.Second),
			wantFresh:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := &PriceData{
				Symbol:     "BTCUSDT",
				MarkPrice:  43000.0,
				LastUpdate: tt.updateTime,
			}

			isFresh := time.Since(data.LastUpdate) < 10*time.Second
			if isFresh != tt.wantFresh {
				t.Errorf("expected isFresh=%v, got %v (age: %v)",
					tt.wantFresh, isFresh, time.Since(data.LastUpdate))
			}
		})
	}
}
