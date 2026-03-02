package api

import (
	"fmt"
	"log"
	"sync"
	"time"
)

// VolatilityGuardConfig 异常波动守卫配置（嵌入到 Config 中）
type VolatilityGuardConfig struct {
	Enabled      bool     `json:"enabled"`
	ThresholdPct float64  `json:"thresholdPct"`  // 波动阈值百分比，默认 3
	WatchSymbols []string `json:"watchSymbols"`  // 监控的 symbol，默认 ["BTCUSDT","ETHUSDT"]
	CooldownMin  int      `json:"cooldownMin"`   // 冷却分钟数，默认 5
}

// volatilityGuardState 运行时状态
type volatilityGuardState struct {
	mu sync.Mutex

	// 上一次采样的价格（每 10 秒更新一次）
	prevPrices map[string]float64

	// 冷却期结束时间，在此时间之前不会再次触发
	cooldownUntil time.Time

	// 是否当前处于被守卫暂停状态
	suspended bool

	stopCh chan struct{}
}

var (
	vgState *volatilityGuardState
	vgMu    sync.Mutex
)

// StartVolatilityGuard 启动异常波动守卫
func StartVolatilityGuard(cfg VolatilityGuardConfig) {
	if !cfg.Enabled {
		return
	}

	// 设置默认值
	if cfg.ThresholdPct <= 0 {
		cfg.ThresholdPct = 3.0
	}
	if len(cfg.WatchSymbols) == 0 {
		cfg.WatchSymbols = []string{"BTCUSDT", "ETHUSDT"}
	}
	if cfg.CooldownMin <= 0 {
		cfg.CooldownMin = 5
	}

	vgMu.Lock()
	defer vgMu.Unlock()

	// 如果已在运行，先停止
	if vgState != nil {
		close(vgState.stopCh)
	}

	vgState = &volatilityGuardState{
		prevPrices: make(map[string]float64),
		stopCh:     make(chan struct{}),
	}

	go runVolatilityGuard(cfg, vgState)
	log.Printf("[VolGuard] Started: threshold=%.1f%%, symbols=%v, cooldown=%dmin",
		cfg.ThresholdPct, cfg.WatchSymbols, cfg.CooldownMin)
}

// StopVolatilityGuard 停止异常波动守卫
func StopVolatilityGuard() {
	vgMu.Lock()
	defer vgMu.Unlock()
	if vgState != nil {
		close(vgState.stopCh)
		vgState = nil
		log.Println("[VolGuard] Stopped")
	}
}

// GetVolatilityGuardStatus 获取守卫状态
func GetVolatilityGuardStatus() map[string]interface{} {
	vgMu.Lock()
	s := vgState
	vgMu.Unlock()

	if s == nil {
		return map[string]interface{}{"running": false}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	cooldownRemaining := ""
	inCooldown := time.Now().Before(s.cooldownUntil)
	if inCooldown {
		remaining := time.Until(s.cooldownUntil).Round(time.Second)
		cooldownRemaining = remaining.String()
	}

	return map[string]interface{}{
		"running":           true,
		"suspended":         s.suspended,
		"inCooldown":        inCooldown,
		"cooldownUntil":     s.cooldownUntil,
		"cooldownRemaining": cooldownRemaining,
	}
}

func runVolatilityGuard(cfg VolatilityGuardConfig, state *volatilityGuardState) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	cooldownDur := time.Duration(cfg.CooldownMin) * time.Minute

	for {
		select {
		case <-state.stopCh:
			return
		case <-ticker.C:
			checkVolatility(cfg, state, cooldownDur)
		}
	}
}

func checkVolatility(cfg VolatilityGuardConfig, state *volatilityGuardState, cooldownDur time.Duration) {
	cache := GetPriceCache()

	state.mu.Lock()
	defer state.mu.Unlock()

	now := time.Now()
	inCooldown := now.Before(state.cooldownUntil)

	// 冷却期结束后自动恢复（如果之前是因为本守卫导致的锁定，则解锁）
	if state.suspended && !inCooldown {
		state.suspended = false
		UnlockRisk()
		log.Printf("[VolGuard] Cooldown ended, risk control unlocked")
	}

	for _, symbol := range cfg.WatchSymbols {
		currentPrice, err := cache.GetPrice(symbol)
		if err != nil || currentPrice <= 0 {
			continue
		}

		prevPrice, hasPrev := state.prevPrices[symbol]
		state.prevPrices[symbol] = currentPrice // 更新采样价格

		if !hasPrev || prevPrice <= 0 {
			continue
		}

		// 已在冷却期，不重复触发
		if inCooldown {
			continue
		}

		// 计算 10 秒涨跌幅
		changePct := (currentPrice - prevPrice) / prevPrice * 100
		if changePct < 0 {
			changePct = -changePct
		}

		if changePct >= cfg.ThresholdPct {
			reason := fmt.Sprintf("异常波动: %s 10秒内波动 %.2f%% (阈值 %.1f%%)", symbol, changePct, cfg.ThresholdPct)
			log.Printf("[VolGuard] TRIGGERED! %s", reason)

			// 锁定风控
			risk.mu.Lock()
			if !risk.locked {
				risk.locked = true
				risk.lockedAt = now
				risk.lockReason = reason
				log.Printf("[Risk] LOCKED by VolGuard! %s", reason)
				NotifyRiskLocked(reason)
			}
			risk.mu.Unlock()

			// 设置冷却期
			state.cooldownUntil = now.Add(cooldownDur)
			state.suspended = true
			inCooldown = true // 防止同一次检查多个 symbol 重复触发

			log.Printf("[VolGuard] Cooldown set until %s", state.cooldownUntil.Format("15:04:05"))
		}
	}
}
