package api

import (
	"context"
	"fmt"
	"log"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/adshao/go-binance/v2/futures"
)

// ========== 多策略联动 ==========
// 当一个策略产生信号时，自动触发另一个策略的动作
// 支持的触发源：RSI信号、爆仓突增、资金费率异常
// 支持的动作：启动网格、平仓/减仓、降杠杆、开仓

// LinkTriggerType 触发源类型
type LinkTriggerType string

const (
	TriggerRSIBuy      LinkTriggerType = "rsi_buy"      // RSI 超卖信号
	TriggerRSISell     LinkTriggerType = "rsi_sell"     // RSI 超买信号
	TriggerLiqSpike    LinkTriggerType = "liq_spike"    // 爆仓突增
	TriggerFundingHigh LinkTriggerType = "funding_high" // 资金费率超高
	TriggerNewsKeyword LinkTriggerType = "news_keyword" // 新闻关键词命中
)

// LinkActionType 动作类型
type LinkActionType string

const (
	ActionStartGrid      LinkActionType = "start_grid"      // 启动网格
	ActionClosePosition  LinkActionType = "close_position"  // 全部平仓
	ActionReducePosition LinkActionType = "reduce_position" // 减仓
	ActionReduceLeverage LinkActionType = "reduce_leverage" // 降杠杆
	ActionPlaceOrder     LinkActionType = "place_order"     // 开仓
)

// StrategyLinkRule 联动规则
type StrategyLinkRule struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`

	// 触发条件
	Trigger       LinkTriggerType `json:"trigger"`
	TriggerSymbol string          `json:"triggerSymbol,omitempty"` // 触发源币种（RSI 用）

	// 动作
	Action       LinkActionType `json:"action"`
	ActionSymbol string         `json:"actionSymbol,omitempty"` // 动作目标币种

	// 动作参数（根据 action 不同使用不同字段）
	ActionParams map[string]string `json:"actionParams,omitempty"`

	// 冷却期（秒），同一规则不重复触发
	CooldownSec int `json:"cooldownSec,omitempty"`
}

// StrategyLinkStatus 联动状态
type StrategyLinkStatus struct {
	Rules      []StrategyLinkRule `json:"rules"`
	Active     bool               `json:"active"`
	LastChecks []LinkCheckLog     `json:"lastChecks"` // 最近触发日志
}

// LinkCheckLog 联动检查日志
type LinkCheckLog struct {
	Time      string `json:"time"`
	RuleID    string `json:"ruleId"`
	RuleName  string `json:"ruleName"`
	Triggered bool   `json:"triggered"`
	Detail    string `json:"detail"`
}

// strategyLinker 全局联动管理器
type strategyLinker struct {
	mu        sync.RWMutex
	rules     []StrategyLinkRule
	active    bool
	cancel    context.CancelFunc
	checkLogs []LinkCheckLog
	lastFired map[string]time.Time // ruleID → 上次触发时间
}

var linker = &strategyLinker{
	lastFired: make(map[string]time.Time),
}

// StartStrategyLink 启动策略联动
func StartStrategyLink(rules []StrategyLinkRule) error {
	linker.mu.Lock()
	defer linker.mu.Unlock()

	if linker.active {
		if linker.cancel != nil {
			linker.cancel()
		}
	}

	for i := range rules {
		if rules[i].ID == "" {
			rules[i].ID = fmt.Sprintf("rule_%d", i+1)
		}
		if rules[i].CooldownSec <= 0 {
			rules[i].CooldownSec = 300 // 默认 5 分钟冷却
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	linker.rules = rules
	linker.active = true
	linker.cancel = cancel
	linker.checkLogs = nil

	go strategyLinkLoop(ctx)

	log.Printf("[StrategyLink] Started with %d rules", len(rules))
	return nil
}

// StopStrategyLink 停止策略联动
func StopStrategyLink() error {
	linker.mu.Lock()
	defer linker.mu.Unlock()

	if !linker.active {
		return fmt.Errorf("strategy link not active")
	}
	if linker.cancel != nil {
		linker.cancel()
	}
	linker.active = false
	log.Println("[StrategyLink] Stopped")
	return nil
}

// GetStrategyLinkStatus 获取联动状态
func GetStrategyLinkStatus() *StrategyLinkStatus {
	linker.mu.RLock()
	defer linker.mu.RUnlock()
	return &StrategyLinkStatus{
		Rules:      linker.rules,
		Active:     linker.active,
		LastChecks: linker.checkLogs,
	}
}

// UpdateStrategyLinkRules 更新规则（不中断监控循环）
func UpdateStrategyLinkRules(rules []StrategyLinkRule) {
	linker.mu.Lock()
	defer linker.mu.Unlock()
	linker.rules = rules
}

// strategyLinkLoop 联动主循环
func strategyLinkLoop(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			checkAllRules(ctx)
		}
	}
}

// checkAllRules 检查所有联动规则
func checkAllRules(ctx context.Context) {
	linker.mu.RLock()
	rules := make([]StrategyLinkRule, len(linker.rules))
	copy(rules, linker.rules)
	linker.mu.RUnlock()

	for _, rule := range rules {
		if !rule.Enabled {
			continue
		}

		// 冷却期检查
		linker.mu.RLock()
		lastTime, exists := linker.lastFired[rule.ID]
		linker.mu.RUnlock()
		if exists && time.Since(lastTime) < time.Duration(rule.CooldownSec)*time.Second {
			continue
		}

		triggered, detail := checkTrigger(ctx, rule)
		if triggered {
			logEntry := LinkCheckLog{
				Time:      time.Now().Format("15:04:05"),
				RuleID:    rule.ID,
				RuleName:  rule.Name,
				Triggered: true,
				Detail:    detail,
			}

			err := executeAction(ctx, rule)
			if err != nil {
				logEntry.Detail += fmt.Sprintf(" → 执行失败: %v", err)
				log.Printf("[StrategyLink] Rule %s triggered but action failed: %v", rule.Name, err)
			} else {
				logEntry.Detail += " → 执行成功"
				log.Printf("[StrategyLink] Rule %s triggered and executed: %s", rule.Name, detail)
			}

			linker.mu.Lock()
			linker.lastFired[rule.ID] = time.Now()
			linker.checkLogs = append(linker.checkLogs, logEntry)
			if len(linker.checkLogs) > 50 {
				linker.checkLogs = linker.checkLogs[len(linker.checkLogs)-50:]
			}
			linker.mu.Unlock()
		}
	}
}

// checkTrigger 检查触发条件是否满足
func checkTrigger(ctx context.Context, rule StrategyLinkRule) (bool, string) {
	switch rule.Trigger {
	case TriggerRSIBuy:
		return checkRSITrigger(rule, true)
	case TriggerRSISell:
		return checkRSITrigger(rule, false)
	case TriggerLiqSpike:
		return checkLiqSpikeTrigger(rule)
	case TriggerFundingHigh:
		return checkFundingTrigger(rule)
	case TriggerNewsKeyword:
		return checkNewsKeywordTrigger(rule)
	default:
		return false, "unknown trigger type"
	}
}

// checkRSITrigger 检查 RSI 信号
func checkRSITrigger(rule StrategyLinkRule, isBuy bool) (bool, string) {
	sym := rule.TriggerSymbol
	if sym == "" {
		return false, ""
	}

	status := GetSignalStatus(sym)
	if status == nil || !status.Active {
		return false, ""
	}

	if isBuy && status.LastSignal == "BUY" {
		return true, fmt.Sprintf("RSI=%.1f 超卖信号 (%s)", status.CurrentRSI, sym)
	}
	if !isBuy && status.LastSignal == "SELL" {
		return true, fmt.Sprintf("RSI=%.1f 超买信号 (%s)", status.CurrentRSI, sym)
	}
	return false, ""
}

// checkLiqSpikeTrigger 检查爆仓突增
func checkLiqSpikeTrigger(rule StrategyLinkRule) (bool, string) {
	store := getLiqStatsStore()
	if store == nil {
		return false, ""
	}

	snap := store.snapshot(time.Now().UTC())

	thresholdStr := rule.ActionParams["liqThresholdM"]
	threshold := 50.0 // 默认 50M USD
	if thresholdStr != "" {
		if v, err := strconv.ParseFloat(thresholdStr, 64); err == nil {
			threshold = v
		}
	}

	// 累加最近 1h 所有桶的总额
	var total1h float64
	for _, bucket := range snap.Stats.H1 {
		total1h += bucket.BuyNotional + bucket.SellNotional
	}
	totalM := total1h / 1e6

	if totalM >= threshold {
		return true, fmt.Sprintf("1h爆仓总额 %.1fM USD (阈值 %.0fM)", totalM, threshold)
	}
	return false, ""
}

// checkFundingTrigger 检查资金费率异常
func checkFundingTrigger(rule StrategyLinkRule) (bool, string) {
	snap := GetFundingStatus()
	if snap == nil || !snap.Active {
		return false, ""
	}

	sym := rule.TriggerSymbol
	if sym == "" {
		// 检查任意超阈值的
		if len(snap.AlertSymbols) > 0 {
			top := snap.AlertSymbols[0]
			return true, fmt.Sprintf("%s 费率异常 (年化 %.0f%%)", top.Symbol, top.AnnualizedPct)
		}
		return false, ""
	}

	// 检查指定币种
	for _, item := range snap.AlertSymbols {
		if item.Symbol == sym {
			return true, fmt.Sprintf("%s 费率 %.4f%% (年化 %.0f%%)", sym, item.FundingRatePct, item.AnnualizedPct)
		}
	}
	return false, ""
}

// executeAction 执行联动动作
func executeAction(ctx context.Context, rule StrategyLinkRule) error {
	switch rule.Action {
	case ActionStartGrid:
		return executeStartGrid(ctx, rule)
	case ActionClosePosition:
		return executeClosePosition(ctx, rule)
	case ActionReducePosition:
		return executeReducePosition(ctx, rule)
	case ActionReduceLeverage:
		return executeReduceLeverage(ctx, rule)
	case ActionPlaceOrder:
		return executePlaceOrder(ctx, rule)
	default:
		return fmt.Errorf("unknown action: %s", rule.Action)
	}
}

func executeStartGrid(ctx context.Context, rule StrategyLinkRule) error {
	sym := rule.ActionSymbol
	if sym == "" {
		sym = rule.TriggerSymbol
	}
	// 从 actionParams 构建 GridConfig
	gridCount := 5
	if v, ok := rule.ActionParams["gridCount"]; ok {
		if n, err := strconv.Atoi(v); err == nil {
			gridCount = n
		}
	}
	upperStr := rule.ActionParams["upperPrice"]
	lowerStr := rule.ActionParams["lowerPrice"]
	amountStr := rule.ActionParams["amountPerGrid"]
	leverageStr := rule.ActionParams["leverage"]

	upper, _ := strconv.ParseFloat(upperStr, 64)
	lower, _ := strconv.ParseFloat(lowerStr, 64)

	if upper == 0 || lower == 0 {
		// 自动计算：当前价格 ±5%
		price, err := getCurrentPrice(ctx, sym, "")
		if err != nil {
			return fmt.Errorf("get price for grid: %w", err)
		}
		upper = price * 1.05
		lower = price * 0.95
	}

	leverage := 5
	if v, err := strconv.Atoi(leverageStr); err == nil && v > 0 {
		leverage = v
	}

	config := GridConfig{
		Symbol:        sym,
		UpperPrice:    upper,
		LowerPrice:    lower,
		GridCount:     gridCount,
		AmountPerGrid: amountStr,
		Leverage:      leverage,
	}

	return StartGrid(config)
}

func executeClosePosition(ctx context.Context, rule StrategyLinkRule) error {
	sym := rule.ActionSymbol
	if sym == "" {
		return fmt.Errorf("actionSymbol is required for close_position")
	}
	_, err := ClosePositionViaWs(ctx, ClosePositionReq{Symbol: sym})
	return err
}

func executeReducePosition(ctx context.Context, rule StrategyLinkRule) error {
	sym := rule.ActionSymbol
	if sym == "" {
		return fmt.Errorf("actionSymbol is required for reduce_position")
	}
	pct := 50.0
	if v, ok := rule.ActionParams["reducePercent"]; ok {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			pct = f
		}
	}
	_, err := ReducePositionViaWs(ctx, ReducePositionReq{
		Symbol:  sym,
		Percent: pct,
	})
	return err
}

func executeReduceLeverage(ctx context.Context, rule StrategyLinkRule) error {
	sym := rule.ActionSymbol
	if sym == "" {
		return fmt.Errorf("actionSymbol is required for reduce_leverage")
	}
	targetLev := 3
	if v, ok := rule.ActionParams["targetLeverage"]; ok {
		if n, err := strconv.Atoi(v); err == nil {
			targetLev = n
		}
	}
	_, err := ChangeLeverage(ctx, sym, targetLev)
	return err
}

func executePlaceOrder(ctx context.Context, rule StrategyLinkRule) error {
	sym := rule.ActionSymbol
	if sym == "" {
		sym = rule.TriggerSymbol
	}

	side := rule.ActionParams["side"]
	if side == "" {
		// RSI buy → BUY, RSI sell → SELL
		if rule.Trigger == TriggerRSIBuy {
			side = "BUY"
		} else if rule.Trigger == TriggerRSISell {
			side = "SELL"
		} else {
			return fmt.Errorf("side is required for place_order")
		}
	}

	amount := rule.ActionParams["amountPerOrder"]
	if amount == "" {
		amount = "5"
	}
	leverage := 10
	if v, ok := rule.ActionParams["leverage"]; ok {
		if n, err := strconv.Atoi(v); err == nil {
			leverage = n
		}
	}

	posSide := "LONG"
	if side == "SELL" {
		posSide = "SHORT"
	}

	req := PlaceOrderReq{
		Source:        "strategy_link",
		Symbol:        sym,
		Side:          futures.SideType(side),
		OrderType:     futures.OrderType("MARKET"),
		PositionSide:  futures.PositionSideType(posSide),
		QuoteQuantity: amount,
		Leverage:      leverage,
	}

	_, err := PlaceOrderViaWs(ctx, req)
	return err
}

// checkNewsKeywordTrigger 检查新闻关键词命中
func checkNewsKeywordTrigger(rule StrategyLinkRule) (bool, string) {
	kwStr := rule.ActionParams["keywords"]
	if kwStr == "" {
		return false, ""
	}
	keywords := make([]string, 0)
	for _, kw := range splitAndTrim(kwStr, ",") {
		if kw != "" {
			keywords = append(keywords, kw)
		}
	}
	if len(keywords) == 0 {
		return false, ""
	}

	items := GetRecentNews(30)
	cutoff := time.Now().Add(-10 * time.Minute)
	for _, item := range items {
		if item.Timestamp.IsZero() || item.Timestamp.Before(cutoff) {
			continue
		}
		title := strings.ToLower(item.Title + " " + item.Summary)
		for _, kw := range keywords {
			if strings.Contains(title, strings.ToLower(kw)) {
				return true, fmt.Sprintf("新闻关键词命中: %s → %s", kw, item.Title)
			}
		}
	}
	return false, ""
}

func splitAndTrim(s, sep string) []string {
	parts := strings.Split(s, sep)
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// getLiqStatsStore 获取爆仓统计存储的引用（跨包访问）
func getLiqStatsStore() *liquidationStatsStore {
	statsStoreMu.RLock()
	defer statsStoreMu.RUnlock()
	return globalStatsStore
}

// 为了让 strategy_link 能访问爆仓统计，需要暴露全局变量
var (
	globalStatsStore *liquidationStatsStore
	statsStoreMu     sync.RWMutex
)

// SetGlobalStatsStore 在 ws_liquidation_stats.go 初始化时调用
func SetGlobalStatsStore(store *liquidationStatsStore) {
	statsStoreMu.Lock()
	globalStatsStore = store
	statsStoreMu.Unlock()
}

// 为了让 executeAction 能正确编译，确保需要的类型被引用
var _ = math.Abs
