package api

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/utils"
)

// KillSwitchLevel 熔断级别
type KillSwitchLevel string

const (
	KSStrategy KillSwitchLevel = "strategy"
	KSSymbol   KillSwitchLevel = "symbol"
	KSAccount  KillSwitchLevel = "account"
)

// KillSwitchRequest 熔断请求
type KillSwitchRequest struct {
	Level  KillSwitchLevel `json:"level"`            // strategy / symbol / account
	Target string          `json:"target,omitempty"` // 策略名或币种
	Action string          `json:"action"`           // activate / deactivate
	Reason string          `json:"reason,omitempty"`
}

type killSwitchState struct {
	mu          sync.RWMutex
	account     bool
	symbols     map[string]bool
	strategies  map[string]bool
	reasons     map[string]string
	triggeredAt map[string]time.Time
}

var ks = &killSwitchState{
	symbols:     make(map[string]bool),
	strategies:  make(map[string]bool),
	reasons:     make(map[string]string),
	triggeredAt: make(map[string]time.Time),
}

// InitKillSwitch 初始化 kill switch
func InitKillSwitch() {
	log.Println("[KillSwitch] Initialized")
}

// ActivateKillSwitch 激活熔断
func ActivateKillSwitch(level KillSwitchLevel, target, reason string) {
	ks.mu.Lock()
	defer ks.mu.Unlock()

	key := makeKSKey(level, target)
	now := time.Now()

	switch level {
	case KSAccount:
		ks.account = true
	case KSSymbol:
		ks.symbols[strings.ToUpper(target)] = true
	case KSStrategy:
		ks.strategies[strings.ToLower(target)] = true
	}

	ks.reasons[key] = reason
	ks.triggeredAt[key] = now

	log.Printf("[KillSwitch] ACTIVATED level=%s target=%s reason=%s", level, target, reason)
	NotifyKillSwitch(string(level), target, reason, true)
}

// DeactivateKillSwitch 解除熔断
func DeactivateKillSwitch(level KillSwitchLevel, target string) {
	ks.mu.Lock()
	defer ks.mu.Unlock()

	key := makeKSKey(level, target)

	switch level {
	case KSAccount:
		ks.account = false
	case KSSymbol:
		delete(ks.symbols, strings.ToUpper(target))
	case KSStrategy:
		delete(ks.strategies, strings.ToLower(target))
	}

	delete(ks.reasons, key)
	delete(ks.triggeredAt, key)

	log.Printf("[KillSwitch] DEACTIVATED level=%s target=%s", level, target)
	NotifyKillSwitch(string(level), target, "", false)
}

// CheckKillSwitch 检查是否被熔断，返回 nil 表示通过
func CheckKillSwitch(source, symbol string) error {
	ks.mu.RLock()
	defer ks.mu.RUnlock()

	// 账户级
	if ks.account {
		return fmt.Errorf("kill-switch: 账户级熔断已激活 (%s)", ks.reasons[makeKSKey(KSAccount, "")])
	}

	// 币种级
	if symbol != "" {
		if ks.symbols[strings.ToUpper(symbol)] {
			return fmt.Errorf("kill-switch: 币种 %s 已熔断 (%s)", symbol, ks.reasons[makeKSKey(KSSymbol, symbol)])
		}
	}

	// 策略级
	if source != "" {
		if ks.strategies[strings.ToLower(source)] {
			return fmt.Errorf("kill-switch: 策略 %s 已熔断 (%s)", source, ks.reasons[makeKSKey(KSStrategy, source)])
		}
	}

	return nil
}

// GetKillSwitchStatus 获取当前所有熔断状态
func GetKillSwitchStatus() map[string]interface{} {
	ks.mu.RLock()
	defer ks.mu.RUnlock()

	activeItems := []map[string]interface{}{}

	if ks.account {
		key := makeKSKey(KSAccount, "")
		activeItems = append(activeItems, map[string]interface{}{
			"level": "account", "target": "*",
			"reason": ks.reasons[key], "triggeredAt": ks.triggeredAt[key],
		})
	}
	for sym := range ks.symbols {
		key := makeKSKey(KSSymbol, sym)
		activeItems = append(activeItems, map[string]interface{}{
			"level": "symbol", "target": sym,
			"reason": ks.reasons[key], "triggeredAt": ks.triggeredAt[key],
		})
	}
	for strat := range ks.strategies {
		key := makeKSKey(KSStrategy, strat)
		activeItems = append(activeItems, map[string]interface{}{
			"level": "strategy", "target": strat,
			"reason": ks.reasons[key], "triggeredAt": ks.triggeredAt[key],
		})
	}

	return map[string]interface{}{
		"accountLocked": ks.account,
		"activeCount":   len(activeItems),
		"items":         activeItems,
	}
}

func makeKSKey(level KillSwitchLevel, target string) string {
	return string(level) + ":" + target
}

// NotifyKillSwitch 发送熔断通知
func NotifyKillSwitch(level, target, reason string, activated bool) {
	action := "解除"
	if activated {
		action = "激活"
	}
	msg := fmt.Sprintf("🚨 Kill-Switch %s\n级别: %s\n目标: %s", action, level, target)
	if reason != "" {
		msg += "\n原因: " + reason
	}
	SendNotify(msg)
}

// HandleKillSwitch POST /tool/risk/kill-switch
func HandleKillSwitch(c context.Context, ctx *app.RequestContext) {
	var req KillSwitchRequest
	if err := ctx.BindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, utils.H{"error": "invalid request"})
		return
	}

	switch strings.ToLower(req.Action) {
	case "activate":
		ActivateKillSwitch(req.Level, req.Target, req.Reason)
		ctx.JSON(http.StatusOK, utils.H{"message": "kill-switch activated", "level": req.Level, "target": req.Target})
	case "deactivate":
		DeactivateKillSwitch(req.Level, req.Target)
		ctx.JSON(http.StatusOK, utils.H{"message": "kill-switch deactivated", "level": req.Level, "target": req.Target})
	default:
		ctx.JSON(http.StatusBadRequest, utils.H{"error": "action must be 'activate' or 'deactivate'"})
	}
}

// HandleGetKillSwitchStatus GET /tool/risk/kill-switch
func HandleGetKillSwitchStatus(c context.Context, ctx *app.RequestContext) {
	ctx.JSON(http.StatusOK, utils.H{"data": GetKillSwitchStatus()})
}
