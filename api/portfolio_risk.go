package api

import (
	"context"
	"fmt"
	"log"
	"math"
	"strconv"
	"sync"
	"time"
)

// PortfolioRiskConfig 组合风控配置
type PortfolioRiskConfig struct {
	MaxTotalNotional   float64 `json:"maxTotalNotional"`   // 总持仓名义价值上限(USDT)，0=不限
	MaxSymbolNotional  float64 `json:"maxSymbolNotional"`  // 单币种名义价值上限(USDT)，0=不限
	MaxSameDirection   int     `json:"maxSameDirection"`   // 同方向最大持仓数量，0=不限
	MaxSymbolPositions int     `json:"maxSymbolPositions"` // 单币种最大持仓笔数，0=不限
	RefreshIntervalSec int     `json:"refreshIntervalSec"` // 刷新间隔秒数，默认10
}

type portfolioSnapshot struct {
	TotalNotional       float64            // 总名义价值
	TotalLongNotional   float64            // 多头总名义
	TotalShortNotional  float64            // 空头总名义
	SymbolNotional      map[string]float64 // 各币种名义价值
	SymbolPositionCount map[string]int     // 各币种持仓笔数
	LongCount           int                // 多头持仓数
	ShortCount          int                // 空头持仓数
	UpdatedAt           time.Time
}

var (
	portfolioMu     sync.RWMutex
	portfolioCfg    PortfolioRiskConfig
	portfolioSnap   portfolioSnapshot
	portfolioStopCh chan struct{}
)

// InitPortfolioRisk 初始化组合风控
func InitPortfolioRisk(cfg PortfolioRiskConfig) {
	portfolioMu.Lock()
	portfolioCfg = cfg
	portfolioSnap.SymbolNotional = make(map[string]float64)
	portfolioSnap.SymbolPositionCount = make(map[string]int)
	portfolioMu.Unlock()

	if cfg.RefreshIntervalSec <= 0 {
		cfg.RefreshIntervalSec = 10
	}

	portfolioStopCh = make(chan struct{})
	go portfolioRefreshLoop(cfg.RefreshIntervalSec)
	log.Printf("[PortfolioRisk] Initialized: maxTotal=%.0f, maxSymbol=%.0f, maxSameDir=%d",
		cfg.MaxTotalNotional, cfg.MaxSymbolNotional, cfg.MaxSameDirection)
}

func portfolioRefreshLoop(intervalSec int) {
	ticker := time.NewTicker(time.Duration(intervalSec) * time.Second)
	defer ticker.Stop()
	// 立即执行一次
	refreshPortfolio()
	for {
		select {
		case <-portfolioStopCh:
			return
		case <-ticker.C:
			refreshPortfolio()
		}
	}
}

func refreshPortfolio() {
	ctx := context.Background()
	positions, err := Client.NewGetPositionRiskService().Do(ctx)
	if err != nil {
		return
	}

	var snap portfolioSnapshot
	snap.SymbolNotional = make(map[string]float64)
	snap.SymbolPositionCount = make(map[string]int)

	for _, pos := range positions {
		amt, _ := strconv.ParseFloat(pos.PositionAmt, 64)
		if amt == 0 {
			continue
		}
		markPrice, _ := strconv.ParseFloat(pos.MarkPrice, 64)
		notional := math.Abs(amt) * markPrice
		snap.TotalNotional += notional
		snap.SymbolNotional[pos.Symbol] += notional
		snap.SymbolPositionCount[pos.Symbol]++

		if amt > 0 {
			snap.TotalLongNotional += notional
			snap.LongCount++
		} else {
			snap.TotalShortNotional += notional
			snap.ShortCount++
		}
	}
	snap.UpdatedAt = time.Now()

	portfolioMu.Lock()
	portfolioSnap = snap
	portfolioMu.Unlock()
}

// CheckPortfolioRisk 下单前检查组合风控
// symbol: 要下单的币种, additionalNotional: 本次下单的名义价值, isLong: 是否做多
func CheckPortfolioRisk(symbol string, additionalNotional float64, isLong bool) error {
	portfolioMu.RLock()
	cfg := portfolioCfg
	snap := portfolioSnap
	portfolioMu.RUnlock()

	// 总持仓限制
	if cfg.MaxTotalNotional > 0 {
		if snap.TotalNotional+additionalNotional > cfg.MaxTotalNotional {
			return fmt.Errorf("组合风控: 总持仓名义%.0f+新单%.0f > 限额%.0f",
				snap.TotalNotional, additionalNotional, cfg.MaxTotalNotional)
		}
	}

	// 单币种限制
	if cfg.MaxSymbolNotional > 0 {
		current := snap.SymbolNotional[symbol]
		if current+additionalNotional > cfg.MaxSymbolNotional {
			return fmt.Errorf("组合风控: %s持仓%.0f+新单%.0f > 限额%.0f",
				symbol, current, additionalNotional, cfg.MaxSymbolNotional)
		}
	}

	// 同方向最大数量
	if cfg.MaxSameDirection > 0 {
		if isLong && snap.LongCount >= cfg.MaxSameDirection {
			return fmt.Errorf("组合风控: 多头持仓数%d已达限额%d", snap.LongCount, cfg.MaxSameDirection)
		}
		if !isLong && snap.ShortCount >= cfg.MaxSameDirection {
			return fmt.Errorf("组合风控: 空头持仓数%d已达限额%d", snap.ShortCount, cfg.MaxSameDirection)
		}
	}

	// 单币种最大持仓笔数
	if cfg.MaxSymbolPositions > 0 {
		currentCount := snap.SymbolPositionCount[symbol]
		if currentCount >= cfg.MaxSymbolPositions {
			return fmt.Errorf("组合风控: %s持仓笔数%d已达限额%d", symbol, currentCount, cfg.MaxSymbolPositions)
		}
	}

	return nil
}

// GetPortfolioStatus 获取当前组合状态
func GetPortfolioStatus() map[string]interface{} {
	portfolioMu.RLock()
	defer portfolioMu.RUnlock()

	return map[string]interface{}{
		"config":              portfolioCfg,
		"totalNotional":       math.Round(portfolioSnap.TotalNotional*100) / 100,
		"totalLongNotional":   math.Round(portfolioSnap.TotalLongNotional*100) / 100,
		"totalShortNotional":  math.Round(portfolioSnap.TotalShortNotional*100) / 100,
		"longCount":           portfolioSnap.LongCount,
		"shortCount":          portfolioSnap.ShortCount,
		"symbolNotional":      portfolioSnap.SymbolNotional,
		"symbolPositionCount": portfolioSnap.SymbolPositionCount,
		"updatedAt":           portfolioSnap.UpdatedAt,
	}
}
