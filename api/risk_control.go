package api

import (
	"fmt"
	"log"
	"strconv"
	"sync"
	"time"
)

// RiskConfig 风控配置
type RiskConfig struct {
	DailyMaxLosses int  `json:"dailyMaxLosses"` // 每日最大亏损次数，0=不限制
	Enabled        bool `json:"enabled"`         // 是否启用风控
}

// riskState 风控运行时状态
type riskState struct {
	mu           sync.RWMutex
	config       RiskConfig
	dailyPnl     float64   // 当天已实现盈亏
	dailyLosses  int       // 当天亏损次数
	locked       bool      // 是否已锁定下单
	lockReason   string    // 锁定原因
	lockedAt     time.Time // 锁定时间
	lastResetDay string    // 上次重置的日期 "2006-01-02"
}

var risk = &riskState{}

// InitRiskControl 初始化风控模块
func InitRiskControl(config RiskConfig) {
	risk.mu.Lock()
	defer risk.mu.Unlock()
	risk.config = config
	risk.lastResetDay = today()
	risk.dailyPnl = 0
	risk.dailyLosses = 0
	risk.locked = false
	risk.lockReason = ""

	if config.Enabled {
		log.Printf("[Risk] Enabled: max loss count = %d per day", config.DailyMaxLosses)
		// 从数据库恢复当天已有的盈亏
		go recoverDailyPnl()
	} else {
		log.Println("[Risk] Disabled")
	}
}

// CheckRisk 下单前检查风控
func CheckRisk() error {
	risk.mu.RLock()
	defer risk.mu.RUnlock()

	if !risk.config.Enabled {
		return nil
	}

	// 每日重置检查
	if today() != risk.lastResetDay {
		return nil
	}

	if risk.locked {
		return fmt.Errorf("风控锁定: %s，禁止下单至明日", risk.lockReason)
	}

	return nil
}

// AddDailyPnl 累加当日盈亏，检查是否触发锁定
func AddDailyPnl(pnl float64) {
	risk.mu.Lock()
	defer risk.mu.Unlock()

	if !risk.config.Enabled {
		return
	}

	// 跨日重置
	if today() != risk.lastResetDay {
		risk.dailyPnl = 0
		risk.dailyLosses = 0
		risk.locked = false
		risk.lockReason = ""
		risk.lastResetDay = today()
		log.Println("[Risk] Daily reset for new day")
	}

	risk.dailyPnl += pnl

	// 亏损次数计数
	if pnl < 0 {
		risk.dailyLosses++
		log.Printf("[Risk] Loss #%d today (%.2f USDT), daily PnL: %.2f", risk.dailyLosses, pnl, risk.dailyPnl)
	} else {
		log.Printf("[Risk] Profit +%.2f USDT, daily PnL: %.2f", pnl, risk.dailyPnl)
	}

	// 检查锁定条件: 亏损次数
	if risk.config.DailyMaxLosses > 0 && risk.dailyLosses >= risk.config.DailyMaxLosses {
		if !risk.locked {
			risk.locked = true
			risk.lockedAt = time.Now()
			risk.lockReason = fmt.Sprintf("今日已亏损 %d 次 (限额 %d 次)", risk.dailyLosses, risk.config.DailyMaxLosses)
			log.Printf("[Risk] LOCKED! %s", risk.lockReason)
		}
	}
}

// GetRiskStatus 获取当前风控状态
func GetRiskStatus() map[string]interface{} {
	risk.mu.RLock()
	defer risk.mu.RUnlock()

	return map[string]interface{}{
		"enabled":        risk.config.Enabled,
		"dailyMaxLosses": risk.config.DailyMaxLosses,
		"dailyPnl":       risk.dailyPnl,
		"dailyLosses":    risk.dailyLosses,
		"locked":         risk.locked,
		"lockReason":     risk.lockReason,
		"lockedAt":       risk.lockedAt,
	}
}

// UnlockRisk 手动解锁风控（紧急情况）
func UnlockRisk() {
	risk.mu.Lock()
	defer risk.mu.Unlock()
	risk.locked = false
	risk.lockReason = ""
	log.Println("[Risk] Manually unlocked")
}

// recoverDailyPnl 从数据库恢复当天的已实现盈亏和亏损次数
func recoverDailyPnl() {
	if DB == nil {
		return
	}

	todayStart := time.Now().Truncate(24 * time.Hour)
	var records []TradeRecord
	err := DB.Where("status = ? AND updated_at >= ?", "CLOSED", todayStart).Find(&records).Error
	if err != nil {
		log.Printf("[Risk] Failed to recover daily PnL: %v", err)
		return
	}

	var totalPnl float64
	var lossCount int
	for _, r := range records {
		pnl, _ := strconv.ParseFloat(r.RealizedPnl, 64)
		totalPnl += pnl
		if pnl < 0 {
			lossCount++
		}
	}

	risk.mu.Lock()
	risk.dailyPnl = totalPnl
	risk.dailyLosses = lossCount
	risk.mu.Unlock()

	log.Printf("[Risk] Recovered: PnL=%.2f USDT, losses=%d (%d closed trades today)", totalPnl, lossCount, len(records))

	// 触发锁定检查
	if lossCount > 0 || totalPnl < 0 {
		AddDailyPnl(0)
	}
}

func today() string {
	return time.Now().Format("2006-01-02")
}
