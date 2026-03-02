package api

import (
	"context"
	"log"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/utils"
)

// FallbackConfig 数据降级配置
type FallbackConfig struct {
	TimeoutSec       int `json:"timeoutSec"`       // WS 无数据超时秒数，默认 15
	RestIntervalMs   int `json:"restIntervalMs"`   // REST 轮询间隔，默认 2000
	RecoveryDelaySec int `json:"recoveryDelaySec"` // 恢复后等待秒数，默认 5
}

type fallbackState struct {
	mu          sync.RWMutex
	degraded    map[string]bool        // symbol → 是否降级中
	lastWsTime  map[string]time.Time   // symbol → 最后 WS 数据时间
	stopPolling map[string]chan struct{}
	cfg         FallbackConfig
}

var fb *fallbackState

// InitDataFallback 初始化数据降级机制
func InitDataFallback(cfg FallbackConfig) {
	if cfg.TimeoutSec <= 0 {
		cfg.TimeoutSec = 15
	}
	if cfg.RestIntervalMs <= 0 {
		cfg.RestIntervalMs = 2000
	}
	if cfg.RecoveryDelaySec <= 0 {
		cfg.RecoveryDelaySec = 5
	}

	fb = &fallbackState{
		degraded:    make(map[string]bool),
		lastWsTime:  make(map[string]time.Time),
		stopPolling: make(map[string]chan struct{}),
		cfg:         cfg,
	}

	go fb.monitorLoop()
	log.Printf("[DataFallback] Initialized: timeout=%ds, restInterval=%dms", cfg.TimeoutSec, cfg.RestIntervalMs)
}

// RecordWsData 记录 WS 数据到达时间（在 priceHub 中调用）
func RecordWsData(symbol string) {
	if fb == nil {
		return
	}
	fb.mu.Lock()
	fb.lastWsTime[symbol] = time.Now()
	wasDegraded := fb.degraded[symbol]
	fb.mu.Unlock()

	// 如果之前是降级状态，恢复
	if wasDegraded {
		go fb.recoverSymbol(symbol)
	}
}

func (f *fallbackState) monitorLoop() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		f.mu.RLock()
		timeout := time.Duration(f.cfg.TimeoutSec) * time.Second
		now := time.Now()
		var toDegrade []string
		for symbol, lastT := range f.lastWsTime {
			if now.Sub(lastT) > timeout && !f.degraded[symbol] {
				toDegrade = append(toDegrade, symbol)
			}
		}
		f.mu.RUnlock()

		for _, symbol := range toDegrade {
			f.degradeSymbol(symbol)
		}
	}
}

func (f *fallbackState) degradeSymbol(symbol string) {
	f.mu.Lock()
	if f.degraded[symbol] {
		f.mu.Unlock()
		return
	}
	f.degraded[symbol] = true
	stopCh := make(chan struct{})
	f.stopPolling[symbol] = stopCh
	f.mu.Unlock()

	log.Printf("[DataFallback] WS timeout for %s, falling back to REST polling", symbol)

	go f.restPollLoop(symbol, stopCh)
}

func (f *fallbackState) recoverSymbol(symbol string) {
	time.Sleep(time.Duration(f.cfg.RecoveryDelaySec) * time.Second)

	f.mu.Lock()
	if !f.degraded[symbol] {
		f.mu.Unlock()
		return
	}
	f.degraded[symbol] = false
	if ch, ok := f.stopPolling[symbol]; ok {
		close(ch)
		delete(f.stopPolling, symbol)
	}
	f.mu.Unlock()

	log.Printf("[DataFallback] WS recovered for %s, stopped REST polling", symbol)
}

func (f *fallbackState) restPollLoop(symbol string, stopCh chan struct{}) {
	interval := time.Duration(f.cfg.RestIntervalMs) * time.Millisecond
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-stopCh:
			return
		case <-ticker.C:
			f.fetchRestPrice(symbol)
		}
	}
}

func (f *fallbackState) fetchRestPrice(symbol string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	prices, err := Client.NewListPricesService().Symbol(symbol).Do(ctx)
	if err != nil || len(prices) == 0 {
		return
	}

	price, _ := strconv.ParseFloat(prices[0].Price, 64)
	if price > 0 {
		// 更新价格缓存
		GetPriceCache().UpdatePrice(symbol, price)
	}
}

// GetFallbackStatus 获取降级状态
func GetFallbackStatus() map[string]interface{} {
	if fb == nil {
		return map[string]interface{}{"enabled": false}
	}

	fb.mu.RLock()
	defer fb.mu.RUnlock()

	degradedList := []string{}
	for sym, active := range fb.degraded {
		if active {
			degradedList = append(degradedList, sym)
		}
	}

	return map[string]interface{}{
		"enabled":       true,
		"degradedCount": len(degradedList),
		"degraded":      degradedList,
		"config": map[string]int{
			"timeoutSec":     fb.cfg.TimeoutSec,
			"restIntervalMs": fb.cfg.RestIntervalMs,
		},
	}
}

// HandleGetFallbackStatus GET /tool/data-fallback/status
func HandleGetFallbackStatus(c context.Context, ctx *app.RequestContext) {
	ctx.JSON(http.StatusOK, utils.H{"data": GetFallbackStatus()})
}
