package api

import (
	"context"
	"fmt"
	"log"
	"math"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/adshao/go-binance/v2/futures"
	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/utils"
	"github.com/google/uuid"
)

// LocalTPSLCondition 本地止盈止损条件（持久化到数据库）
type LocalTPSLCondition struct {
	ID            uint       `gorm:"primaryKey" json:"id"`
	GroupID       string     `gorm:"type:varchar(40);index" json:"groupId"`  // 同一笔主单的 TP+SL 共享，用于联动取消
	Symbol        string     `gorm:"type:varchar(20);index" json:"symbol"`   // 交易对
	ConditionType string     `gorm:"type:varchar(20)" json:"conditionType"`  // TAKE_PROFIT / STOP_LOSS / TRAILING_STOP
	Side          string     `gorm:"type:varchar(10)" json:"side"`           // 平仓方向: BUY / SELL
	PositionSide  string     `gorm:"type:varchar(10)" json:"positionSide"`   // BOTH / LONG / SHORT
	TriggerPrice  float64    `gorm:"type:numeric(36,8)" json:"triggerPrice"` // 触发价格
	Quantity      string     `gorm:"type:varchar(40)" json:"quantity"`       // 本次下单量（非全仓）
	EntryPrice    float64    `gorm:"type:numeric(36,8)" json:"entryPrice"`   // 入场价
	LevelIndex    int        `json:"levelIndex"`                             // 阶梯TP层级索引，-1 表示非阶梯
	TotalLevels   int        `json:"totalLevels"`                            // 阶梯总层级数
	Status        string     `gorm:"type:varchar(20);index" json:"status"`   // ACTIVE / TRIGGERED / CANCELLED
	OrderID       int64      `gorm:"index" json:"orderId"`                   // 关联的主单 OrderID
	TriggeredAt   *time.Time `json:"triggeredAt,omitempty"`                  // 触发时间
	Source        string     `gorm:"type:varchar(40)" json:"source"`         // manual / strategy_xxx
	CreatedAt     time.Time  `gorm:"autoCreateTime" json:"createdAt"`
	UpdatedAt     time.Time  `gorm:"autoUpdateTime" json:"updatedAt"`

	// 移动止损专用字段（ConditionType = TRAILING_STOP 时有效）
	TrailingCallbackRate    float64 `gorm:"type:numeric(10,4);default:0" json:"trailingCallbackRate"`    // 回调比例，如 1.0 表示 1%，0=非移动止损
	TrailingActivationPrice float64 `gorm:"type:numeric(36,8);default:0" json:"trailingActivationPrice"` // 激活价格，0=立即激活
	TrailingHighestPrice    float64 `gorm:"type:numeric(36,8);default:0" json:"trailingHighestPrice"`    // 追踪极值（多头用最高价，空头用最低价），仅存内存，触发时才持久化
	TrailingActivated       bool    `gorm:"default:false" json:"trailingActivated"`                      // 是否已激活追踪
}

// localTPSLMonitor 本地止盈止损监控器
type localTPSLMonitor struct {
	mu         sync.RWMutex
	conditions map[string][]*LocalTPSLCondition // symbol -> active conditions
	stopCh     chan struct{}
}

var tpslMonitor *localTPSLMonitor

// StartLocalTPSLMonitor 从DB加载ACTIVE条件 + 启动监控goroutine
func StartLocalTPSLMonitor() {
	tpslMonitor = &localTPSLMonitor{
		conditions: make(map[string][]*LocalTPSLCondition),
		stopCh:     make(chan struct{}),
	}

	// 启动恢复顺序：Redis 优先，Redis 不可用或为空再回退 DB。
	loadedFromRedis := false
	if redisConds, err := loadActiveTPSLFromRedis(); err != nil {
		log.Printf("[LocalTPSL] Redis load failed, fallback to DB: %v", err)
	} else if len(redisConds) > 0 {
		tpslMonitor.mu.Lock()
		for _, cond := range redisConds {
			if cond == nil {
				continue
			}
			key := strings.ToUpper(strings.TrimSpace(cond.Symbol))
			if key == "" {
				key = cond.Symbol
			} else {
				cond.Symbol = key
			}
			tpslMonitor.conditions[key] = append(tpslMonitor.conditions[key], cond)
		}
		tpslMonitor.mu.Unlock()
		loadedFromRedis = true
		log.Printf("[LocalTPSL] Loaded %d active conditions from Redis", len(redisConds))
	}

	if !loadedFromRedis {
		dbConds, err := loadActiveTPSLFromDB("")
		if err != nil {
			log.Printf("[LocalTPSL] Failed to load active conditions from DB: %v", err)
		} else {
			tpslMonitor.mu.Lock()
			for _, cond := range dbConds {
				if cond == nil {
					continue
				}
				key := strings.ToUpper(strings.TrimSpace(cond.Symbol))
				if key == "" {
					key = cond.Symbol
				} else {
					cond.Symbol = key
				}
				tpslMonitor.conditions[key] = append(tpslMonitor.conditions[key], cond)
			}
			tpslMonitor.mu.Unlock()
			if len(dbConds) > 0 {
				log.Printf("[LocalTPSL] Loaded %d active conditions from DB", len(dbConds))
			}
			// DB 回填 Redis，清理可能遗留的脏 key。
			replaceActiveTPSLInRedis(dbConds)
		}
	}

	go tpslMonitor.run()
	log.Println("[LocalTPSL] Monitor started")
}

func loadActiveTPSLFromDB(symbol string) ([]*LocalTPSLCondition, error) {
	if DB == nil {
		return nil, nil
	}

	var rows []LocalTPSLCondition
	q := DB.Where("status = ?", "ACTIVE")
	if symbol != "" {
		q = q.Where("symbol = ?", symbol)
	}
	if err := q.Find(&rows).Error; err != nil {
		return nil, err
	}

	out := make([]*LocalTPSLCondition, 0, len(rows))
	for i := range rows {
		c := rows[i]
		out = append(out, &c)
	}
	return out, nil
}

// run 1秒 ticker 循环检查价格触发
func (m *localTPSLMonitor) run() {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-m.stopCh:
			return
		case <-ticker.C:
			m.checkAll()
		}
	}
}

// checkAll 遍历所有活跃条件，检查是否触发
func (m *localTPSLMonitor) checkAll() {
	m.mu.RLock()
	// 复制要检查的 symbol 列表
	symbols := make([]string, 0, len(m.conditions))
	for sym := range m.conditions {
		symbols = append(symbols, sym)
	}
	m.mu.RUnlock()

	cache := GetPriceCache()
	for _, symbol := range symbols {
		price, err := cache.GetPrice(symbol)
		if err != nil {
			continue // 价格不可用，跳过
		}

		m.mu.RLock()
		conds := m.conditions[symbol]
		// 复制一份避免持锁时触发
		toCheck := make([]*LocalTPSLCondition, len(conds))
		copy(toCheck, conds)
		m.mu.RUnlock()

		for _, cond := range toCheck {
			if cond.ConditionType == "TRAILING_STOP" {
				m.updateTrailingStop(cond, price)
			}
			if shouldTrigger(cond, price) {
				m.triggerCondition(cond)
			}
		}
	}
}

// updateTrailingStop 更新移动止损的追踪极值，仅在内存中维护
func (m *localTPSLMonitor) updateTrailingStop(cond *LocalTPSLCondition, price float64) {
	isSell := cond.Side == "SELL" // 平多仓，追踪最高价
	changed := false

	// 检查激活条件
	if !cond.TrailingActivated {
		if cond.TrailingActivationPrice <= 0 {
			// 无激活价格要求，立即激活
			cond.TrailingActivated = true
			cond.TrailingHighestPrice = price
			changed = true
		} else if isSell && price >= cond.TrailingActivationPrice {
			// 多头：价格突破激活价才开始追踪
			cond.TrailingActivated = true
			cond.TrailingHighestPrice = price
			changed = true
			log.Printf("[LocalTPSL][TS] Activated LONG trailing stop for %s at price=%.4f (activation=%.4f)",
				cond.Symbol, price, cond.TrailingActivationPrice)
		} else if !isSell && price <= cond.TrailingActivationPrice {
			// 空头：价格跌破激活价才开始追踪
			cond.TrailingActivated = true
			cond.TrailingHighestPrice = price
			changed = true
			log.Printf("[LocalTPSL][TS] Activated SHORT trailing stop for %s at price=%.4f (activation=%.4f)",
				cond.Symbol, price, cond.TrailingActivationPrice)
		}
		if changed {
			upsertActiveTPSLToRedis(cond)
		}
		return
	}

	// 已激活：更新追踪极值
	if isSell {
		// 多头追踪最高价
		if price > cond.TrailingHighestPrice {
			cond.TrailingHighestPrice = price
			changed = true
		}
	} else {
		// 空头追踪最低价（复用 TrailingHighestPrice 字段存储最低价）
		if cond.TrailingHighestPrice <= 0 || price < cond.TrailingHighestPrice {
			cond.TrailingHighestPrice = price
			changed = true
		}
	}
	if changed {
		upsertActiveTPSLToRedis(cond)
	}
}

// shouldTrigger 判断条件是否应该触发
func shouldTrigger(cond *LocalTPSLCondition, price float64) bool {
	isSell := cond.Side == "SELL" // 平多仓

	switch cond.ConditionType {
	case "TAKE_PROFIT":
		if isSell {
			// 平多 TP: price >= triggerPrice
			return price >= cond.TriggerPrice
		}
		// 平空 TP: price <= triggerPrice
		return price <= cond.TriggerPrice
	case "STOP_LOSS":
		if isSell {
			// 平多 SL: price <= triggerPrice
			return price <= cond.TriggerPrice
		}
		// 平空 SL: price >= triggerPrice
		return price >= cond.TriggerPrice
	case "TRAILING_STOP":
		// 未激活则不触发
		if !cond.TrailingActivated || cond.TrailingHighestPrice <= 0 {
			return false
		}
		rate := cond.TrailingCallbackRate
		if isSell {
			// 平多：止损价 = highestPrice * (1 - rate/100)，当前价 <= 止损价时触发
			stopPrice := cond.TrailingHighestPrice * (1 - rate/100)
			return price <= stopPrice
		}
		// 平空：止损价 = lowestPrice * (1 + rate/100)，当前价 >= 止损价时触发
		stopPrice := cond.TrailingHighestPrice * (1 + rate/100)
		return price >= stopPrice
	}
	return false
}

// triggerCondition 触发条件：执行减仓 + 更新DB + 联动取消
func (m *localTPSLMonitor) triggerCondition(cond *LocalTPSLCondition) {
	ctx := context.Background()

	// 移动止损：触发时记录实际触发价（追踪极值回调点）
	if cond.ConditionType == "TRAILING_STOP" && cond.TrailingHighestPrice > 0 {
		rate := cond.TrailingCallbackRate
		isSell := cond.Side == "SELL"
		if isSell {
			cond.TriggerPrice = cond.TrailingHighestPrice * (1 - rate/100)
		} else {
			cond.TriggerPrice = cond.TrailingHighestPrice * (1 + rate/100)
		}
		// 将追踪极值持久化到 DB（仅此时写一次）
		if DB != nil {
			DB.Model(&LocalTPSLCondition{}).Where("id = ?", cond.ID).Updates(map[string]interface{}{
				"trailing_highest_price": cond.TrailingHighestPrice,
				"trailing_activated":     true,
				"trigger_price":          cond.TriggerPrice,
			})
		}
	}

	// 部分止盈 + 移动止损保护：
	// 非阶梯 TAKE_PROFIT（LevelIndex == -1）触发时，只平 50% 仓位，剩余 50% 注册 TRAILING_STOP
	if cond.ConditionType == "TAKE_PROFIT" && cond.LevelIndex == -1 {
		totalQty, parseErr := strconv.ParseFloat(cond.Quantity, 64)
		if parseErr == nil && totalQty > 0 {
			ctx2 := context.Background()
			qtyPrecision, stepSize, precErr := getSymbolPrecision(ctx2, cond.Symbol)
			if precErr == nil {
				halfQty := roundToStepSize(totalQty*0.5, stepSize)
				halfQtyStr := formatQuantity(halfQty, qtyPrecision)
				remainQty := roundToStepSize(totalQty-halfQty, stepSize)
				remainQtyStr := formatQuantity(remainQty, qtyPrecision)

				log.Printf("[LocalTPSL] TP partial: total=%s, half=%s, remain=%s for %s groupID=%s",
					cond.Quantity, halfQtyStr, remainQtyStr, cond.Symbol, cond.GroupID)

				// 仅平掉 50%
				_, err := reduceOrderViaWs(ctx,
					cond.Symbol,
					futures.SideType(cond.Side),
					futures.PositionSideType(cond.PositionSide),
					halfQtyStr,
				)

				now := time.Now()
				if err != nil {
					log.Printf("[LocalTPSL] TP partial reduce failed for %s: %v", cond.Symbol, err)
					SaveFailedOperation("TPSL_TRIGGER", cond.Source, cond.Symbol, cond, cond.OrderID, err)
					errStr := err.Error()
					if isPositionClosedError(errStr) {
						log.Printf("[LocalTPSL] Position already closed for %s, cancelling group %s", cond.Symbol, cond.GroupID)
						m.updateConditionStatus(cond, "CANCELLED", &now)
						m.cancelGroupConditions(cond.GroupID, cond.ID)
					}
					return
				}

				// 平掉 50% 成功，更新 TP 条件为 TRIGGERED
				m.updateConditionStatus(cond, "TRIGGERED", &now)

				// 注册剩余 50% 的 TRAILING_STOP，回调率 0.5%
				_, tsErr := RegisterTrailingStop(
					cond.Symbol,
					cond.Side,
					cond.PositionSide,
					remainQtyStr,
					0.5, // 回调率 0.5%
					0,   // 立即激活
					"tp_partial_trailing",
					cond.OrderID,
				)
				if tsErr != nil {
					log.Printf("[LocalTPSL] Register trailing stop after partial TP failed: %v", tsErr)
				} else {
					log.Printf("[LocalTPSL] Trailing stop registered for remaining %s after partial TP on %s", remainQtyStr, cond.Symbol)
				}

				// 联动取消逻辑（此时 TP 已触发，取消同组 SL）
				m.handleLinkedCancellation(cond)
				SaveSuccessOperation("TPSL_TRIGGER", cond.Source, cond.Symbol, map[string]any{
					"groupId":       cond.GroupID,
					"conditionType": cond.ConditionType,
					"triggerPrice":  cond.TriggerPrice,
					"quantity":      halfQtyStr,
					"mode":          "partial_tp",
				}, cond.OrderID)
				NotifyTPSLTriggered(cond.ConditionType+" (partial 50%+trailing)", cond.Symbol, cond.TriggerPrice, halfQtyStr)
				return
			}
			// 精度获取失败，降级到全量平仓
			log.Printf("[LocalTPSL] getSymbolPrecision failed for %s, fallback to full close: %v", cond.Symbol, precErr)
		}
	}

	log.Printf("[LocalTPSL] Triggering %s for %s: triggerPrice=%.4f, qty=%s, groupID=%s",
		cond.ConditionType, cond.Symbol, cond.TriggerPrice, cond.Quantity, cond.GroupID)

	// 执行市价减仓
	_, err := reduceOrderViaWs(ctx,
		cond.Symbol,
		futures.SideType(cond.Side),
		futures.PositionSideType(cond.PositionSide),
		cond.Quantity,
	)

	now := time.Now()
	if err != nil {
		log.Printf("[LocalTPSL] Trigger reduce failed for %s: %v", cond.Symbol, err)
		SaveFailedOperation("TPSL_TRIGGER", cond.Source, cond.Symbol, cond, cond.OrderID, err)
		// 检查是否因为没有仓位导致失败（仓位已手动平掉）
		errStr := err.Error()
		if isPositionClosedError(errStr) {
			log.Printf("[LocalTPSL] Position already closed for %s, cancelling group %s", cond.Symbol, cond.GroupID)
			m.updateConditionStatus(cond, "CANCELLED", &now)
			m.cancelGroupConditions(cond.GroupID, cond.ID)
			return
		}
		// 网络或其他错误，保留 ACTIVE，下次 tick 重试
		return
	}

	// 减仓成功，更新状态
	m.updateConditionStatus(cond, "TRIGGERED", &now)

	// 联动取消逻辑
	m.handleLinkedCancellation(cond)
	SaveSuccessOperation("TPSL_TRIGGER", cond.Source, cond.Symbol, map[string]any{
		"groupId":       cond.GroupID,
		"conditionType": cond.ConditionType,
		"triggerPrice":  cond.TriggerPrice,
		"quantity":      cond.Quantity,
		"mode":          "full",
	}, cond.OrderID)

	log.Printf("[LocalTPSL] %s triggered successfully for %s, qty=%s",
		cond.ConditionType, cond.Symbol, cond.Quantity)

	NotifyTPSLTriggered(cond.ConditionType, cond.Symbol, cond.TriggerPrice, cond.Quantity)
}

// isPositionClosedError 判断是否因仓位已关闭导致的错误
func isPositionClosedError(errStr string) bool {
	// 常见的仓位不存在错误关键字
	keywords := []string{
		"no open position",
		"ReduceOnly Order is rejected",
		"position side does not match",
		"2022", // Binance error code for ReduceOnly order rejected
	}
	for _, kw := range keywords {
		if containsIgnoreCase(errStr, kw) {
			return true
		}
	}
	return false
}

func containsIgnoreCase(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && findSubstring(s, substr))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		match := true
		for j := 0; j < len(substr); j++ {
			sc := s[i+j]
			pc := substr[j]
			if sc != pc && sc != pc+32 && sc != pc-32 {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

// handleLinkedCancellation 联动取消逻辑
func (m *localTPSLMonitor) handleLinkedCancellation(triggered *LocalTPSLCondition) {
	switch triggered.ConditionType {
	case "STOP_LOSS":
		// SL 触发 → 取消同组所有 TP
		m.cancelGroupConditionsByType(triggered.GroupID, triggered.ID, "TAKE_PROFIT")
	case "TAKE_PROFIT":
		if triggered.TotalLevels <= 1 {
			// 单级 TP 触发 → 取消同组 SL
			m.cancelGroupConditionsByType(triggered.GroupID, triggered.ID, "STOP_LOSS")
		} else {
			// 阶梯 TP：检查是否为最后一个
			if m.isLastActiveTP(triggered.GroupID) {
				m.cancelGroupConditionsByType(triggered.GroupID, triggered.ID, "STOP_LOSS")
			}
		}
	}
}

// isLastActiveTP 检查同组是否还有其他 ACTIVE 的 TP
func (m *localTPSLMonitor) isLastActiveTP(groupID string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, conds := range m.conditions {
		for _, c := range conds {
			if c.GroupID == groupID && c.ConditionType == "TAKE_PROFIT" && c.Status == "ACTIVE" {
				return false
			}
		}
	}
	return true
}

// updateConditionStatus 更新条件状态（内存 + DB）
func (m *localTPSLMonitor) updateConditionStatus(cond *LocalTPSLCondition, status string, triggeredAt *time.Time) {
	// 更新内存
	cond.Status = status
	cond.TriggeredAt = triggeredAt

	// 从活跃列表移除
	if status != "ACTIVE" {
		m.removeFromMemory(cond)
	}

	// 更新数据库
	if DB != nil {
		updates := map[string]interface{}{
			"status":     status,
			"updated_at": time.Now(),
		}
		if triggeredAt != nil {
			updates["triggered_at"] = triggeredAt
		}
		DB.Model(&LocalTPSLCondition{}).Where("id = ?", cond.ID).Updates(updates)
	}

	if status == "ACTIVE" {
		upsertActiveTPSLToRedis(cond)
		return
	}
	removeTPSLFromRedis(cond.ID)
}

// removeFromMemory 从内存活跃列表移除条件
func (m *localTPSLMonitor) removeFromMemory(cond *LocalTPSLCondition) {
	if cond == nil {
		return
	}
	symbol := strings.ToUpper(strings.TrimSpace(cond.Symbol))
	if symbol == "" {
		symbol = cond.Symbol
	} else {
		cond.Symbol = symbol
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	conds := m.conditions[symbol]
	for i, c := range conds {
		if c.ID == cond.ID {
			m.conditions[symbol] = append(conds[:i], conds[i+1:]...)
			break
		}
	}
	// 如果该 symbol 没有活跃条件了，删除 key
	if len(m.conditions[symbol]) == 0 {
		delete(m.conditions, symbol)
	}
}

// cancelGroupConditions 取消同组所有条件（排除指定ID）
func (m *localTPSLMonitor) cancelGroupConditions(groupID string, excludeID uint) {
	now := time.Now()
	m.mu.RLock()
	var toCancel []*LocalTPSLCondition
	for _, conds := range m.conditions {
		for _, c := range conds {
			if c.GroupID == groupID && c.ID != excludeID && c.Status == "ACTIVE" {
				toCancel = append(toCancel, c)
			}
		}
	}
	m.mu.RUnlock()

	for _, c := range toCancel {
		m.updateConditionStatus(c, "CANCELLED", &now)
		log.Printf("[LocalTPSL] Cancelled linked %s (id=%d) for group %s", c.ConditionType, c.ID, groupID)
	}
}

// cancelGroupConditionsByType 取消同组指定类型的条件
func (m *localTPSLMonitor) cancelGroupConditionsByType(groupID string, excludeID uint, condType string) {
	now := time.Now()
	m.mu.RLock()
	var toCancel []*LocalTPSLCondition
	for _, conds := range m.conditions {
		for _, c := range conds {
			if c.GroupID == groupID && c.ID != excludeID && c.ConditionType == condType && c.Status == "ACTIVE" {
				toCancel = append(toCancel, c)
			}
		}
	}
	m.mu.RUnlock()

	for _, c := range toCancel {
		m.updateConditionStatus(c, "CANCELLED", &now)
		log.Printf("[LocalTPSL] Cancelled linked %s (id=%d) for group %s", c.ConditionType, c.ID, groupID)
	}
}

// addToMemory 添加条件到内存活跃列表
func (m *localTPSLMonitor) addToMemory(cond *LocalTPSLCondition) {
	if cond == nil {
		return
	}
	symbol := strings.ToUpper(strings.TrimSpace(cond.Symbol))
	if symbol == "" {
		symbol = cond.Symbol
	} else {
		cond.Symbol = symbol
	}

	m.mu.Lock()
	conds := m.conditions[symbol]
	for _, existing := range conds {
		if existing != nil && existing.ID == cond.ID {
			*existing = *cond
			m.mu.Unlock()
			upsertActiveTPSLToRedis(cond)
			return
		}
	}
	m.conditions[symbol] = append(conds, cond)
	m.mu.Unlock()
	upsertActiveTPSLToRedis(cond)
}

// RegisterLocalTPSLFromOrder 下单后注册本地止盈止损条件
// 复用 calcStopLossPrice 计算价格，写DB + 加内存
func RegisterLocalTPSLFromOrder(req PlaceOrderReq, entryPrice float64, quantity string, orderID int64) (groupID string, err error) {
	if tpslMonitor == nil {
		return "", fmt.Errorf("local TPSL monitor not started")
	}

	isBuy := req.Side == futures.SideTypeBuy

	// 计算止损价
	stopLossPrice, slDistance, err := calcStopLossPrice(req, entryPrice, quantity)
	if err != nil {
		return "", err
	}

	// 验证止损价合理性
	if isBuy && stopLossPrice >= entryPrice {
		return "", fmt.Errorf("stopLossPrice (%.2f) must be below entryPrice (%.2f) for BUY", stopLossPrice, entryPrice)
	}
	if !isBuy && stopLossPrice <= entryPrice {
		return "", fmt.Errorf("stopLossPrice (%.2f) must be above entryPrice (%.2f) for SELL", stopLossPrice, entryPrice)
	}

	// 平仓方向
	closeSide := futures.SideTypeSell
	if !isBuy {
		closeSide = futures.SideTypeBuy
	}

	positionSide := req.PositionSide
	if positionSide == "" {
		positionSide = futures.PositionSideTypeBoth
	}

	groupID = uuid.New().String()
	source := req.Source
	if source == "" {
		source = "manual"
	}

	var conditions []*LocalTPSLCondition

	if len(req.TPLevels) > 0 {
		// 阶梯止盈
		totalPct := 0.0
		for _, lv := range req.TPLevels {
			totalPct += lv.Percent
		}
		if math.Abs(totalPct-100) > 0.01 {
			return "", fmt.Errorf("tpLevels: total percent must equal 100, got %.2f", totalPct)
		}

		// 获取数量精度
		ctx := context.Background()
		qtyPrecision, stepSize, err := getSymbolPrecision(ctx, req.Symbol)
		if err != nil {
			return "", err
		}
		totalQty, _ := strconv.ParseFloat(quantity, 64)

		for i, lv := range req.TPLevels {
			var tpPrice float64
			if isBuy {
				tpPrice = entryPrice + slDistance*lv.RiskReward
			} else {
				tpPrice = entryPrice - slDistance*lv.RiskReward
			}

			levelQty := totalQty * lv.Percent / 100
			levelQty = roundToStepSize(levelQty, stepSize)
			levelQtyStr := formatQuantity(levelQty, qtyPrecision)

			log.Printf("[LocalTPSL] Register combo TP level %d: %.0f%% qty=%s, TP=%.4f (rr=1:%.1f)",
				i+1, lv.Percent, levelQtyStr, tpPrice, lv.RiskReward)

			conditions = append(conditions, &LocalTPSLCondition{
				GroupID:       groupID,
				Symbol:        req.Symbol,
				ConditionType: "TAKE_PROFIT",
				Side:          string(closeSide),
				PositionSide:  string(positionSide),
				TriggerPrice:  tpPrice,
				Quantity:      levelQtyStr,
				EntryPrice:    entryPrice,
				LevelIndex:    i,
				TotalLevels:   len(req.TPLevels),
				Status:        "ACTIVE",
				OrderID:       orderID,
				Source:        source,
			})
		}

		// SL 使用全部数量
		conditions = append(conditions, &LocalTPSLCondition{
			GroupID:       groupID,
			Symbol:        req.Symbol,
			ConditionType: "STOP_LOSS",
			Side:          string(closeSide),
			PositionSide:  string(positionSide),
			TriggerPrice:  stopLossPrice,
			Quantity:      quantity,
			EntryPrice:    entryPrice,
			LevelIndex:    -1,
			TotalLevels:   len(req.TPLevels),
			Status:        "ACTIVE",
			OrderID:       orderID,
			Source:        source,
		})
	} else {
		// 单级止盈
		var takeProfitPrice float64
		if isBuy {
			takeProfitPrice = entryPrice + slDistance*req.RiskReward
		} else {
			takeProfitPrice = entryPrice - slDistance*req.RiskReward
		}

		// 验证止盈价
		if isBuy && takeProfitPrice <= entryPrice {
			return "", fmt.Errorf("calculated takeProfitPrice (%.2f) must be above entryPrice (%.2f)", takeProfitPrice, entryPrice)
		}
		if !isBuy && takeProfitPrice >= entryPrice {
			return "", fmt.Errorf("calculated takeProfitPrice (%.2f) must be below entryPrice (%.2f)", takeProfitPrice, entryPrice)
		}

		log.Printf("[LocalTPSL] Register TP=%.4f, SL=%.4f, qty=%s, rr=1:%.1f",
			takeProfitPrice, stopLossPrice, quantity, req.RiskReward)

		conditions = append(conditions, &LocalTPSLCondition{
			GroupID:       groupID,
			Symbol:        req.Symbol,
			ConditionType: "TAKE_PROFIT",
			Side:          string(closeSide),
			PositionSide:  string(positionSide),
			TriggerPrice:  takeProfitPrice,
			Quantity:      quantity,
			EntryPrice:    entryPrice,
			LevelIndex:    -1,
			TotalLevels:   1,
			Status:        "ACTIVE",
			OrderID:       orderID,
			Source:        source,
		})

		conditions = append(conditions, &LocalTPSLCondition{
			GroupID:       groupID,
			Symbol:        req.Symbol,
			ConditionType: "STOP_LOSS",
			Side:          string(closeSide),
			PositionSide:  string(positionSide),
			TriggerPrice:  stopLossPrice,
			Quantity:      quantity,
			EntryPrice:    entryPrice,
			LevelIndex:    -1,
			TotalLevels:   1,
			Status:        "ACTIVE",
			OrderID:       orderID,
			Source:        source,
		})
	}

	// 写入数据库 + 加入内存
	for _, cond := range conditions {
		if DB != nil {
			if err := DB.Create(cond).Error; err != nil {
				log.Printf("[LocalTPSL] Failed to save condition to DB: %v", err)
				return "", fmt.Errorf("save TPSL condition: %w", err)
			}
		}
		tpslMonitor.addToMemory(cond)
	}

	// 确保价格缓存订阅了该 symbol
	_ = GetPriceCache().Subscribe(req.Symbol)

	log.Printf("[LocalTPSL] Registered %d conditions for %s, groupID=%s", len(conditions), req.Symbol, groupID)
	return groupID, nil
}

// CancelTPSLByID 根据条件ID手动取消
func CancelTPSLByID(condID uint) error {
	if tpslMonitor == nil {
		return fmt.Errorf("local TPSL monitor not started")
	}

	now := time.Now()
	tpslMonitor.mu.RLock()
	var target *LocalTPSLCondition
	for _, conds := range tpslMonitor.conditions {
		for _, c := range conds {
			if c.ID == condID && c.Status == "ACTIVE" {
				target = c
				break
			}
		}
		if target != nil {
			break
		}
	}
	tpslMonitor.mu.RUnlock()

	if target == nil {
		redisRows, redisErr := loadActiveTPSLFromRedis()
		if redisErr == nil {
			for _, c := range redisRows {
				if c != nil && c.ID == condID && c.Status == "ACTIVE" {
					target = c
					tpslMonitor.addToMemory(c)
					break
				}
			}
		}
		if redisErr != nil {
			log.Printf("[LocalTPSL] Redis lookup failed for cancel by id %d: %v", condID, redisErr)
		}
	}

	if target == nil {
		rows, err := loadActiveTPSLFromDB("")
		if err != nil {
			return fmt.Errorf("query active conditions from DB: %w", err)
		}
		for _, c := range rows {
			if c != nil && c.ID == condID {
				target = c
				tpslMonitor.addToMemory(c)
				break
			}
		}
		if target == nil {
			return fmt.Errorf("condition %d not found or not active", condID)
		}
	}

	tpslMonitor.updateConditionStatus(target, "CANCELLED", &now)
	log.Printf("[LocalTPSL] Manually cancelled condition %d (%s) for %s", condID, target.ConditionType, target.Symbol)
	return nil
}

// CancelTPSLByGroup 根据 GroupID 取消同组所有条件
func CancelTPSLByGroup(groupID string) error {
	if tpslMonitor == nil {
		return fmt.Errorf("local TPSL monitor not started")
	}

	now := time.Now()
	tpslMonitor.mu.RLock()
	var toCancel []*LocalTPSLCondition
	for _, conds := range tpslMonitor.conditions {
		for _, c := range conds {
			if c.GroupID == groupID && c.Status == "ACTIVE" {
				toCancel = append(toCancel, c)
			}
		}
	}
	tpslMonitor.mu.RUnlock()

	if len(toCancel) == 0 {
		redisRows, redisErr := loadActiveTPSLFromRedis()
		if redisErr == nil {
			for _, c := range redisRows {
				if c != nil && c.GroupID == groupID && c.Status == "ACTIVE" {
					toCancel = append(toCancel, c)
					tpslMonitor.addToMemory(c)
				}
			}
		}
		if redisErr != nil {
			log.Printf("[LocalTPSL] Redis lookup failed for cancel group %s: %v", groupID, redisErr)
		}
	}

	if len(toCancel) == 0 {
		rows, err := loadActiveTPSLFromDB("")
		if err != nil {
			return fmt.Errorf("query active conditions from DB: %w", err)
		}
		for _, c := range rows {
			if c != nil && c.GroupID == groupID && c.Status == "ACTIVE" {
				toCancel = append(toCancel, c)
				tpslMonitor.addToMemory(c)
			}
		}
		if len(toCancel) == 0 {
			return fmt.Errorf("no active conditions found for group %s", groupID)
		}
	}

	for _, c := range toCancel {
		tpslMonitor.updateConditionStatus(c, "CANCELLED", &now)
	}
	log.Printf("[LocalTPSL] Manually cancelled %d conditions for group %s", len(toCancel), groupID)
	return nil
}

// GetActiveTPSLConditions 获取指定 symbol 的活跃条件
func GetActiveTPSLConditions(symbol string) []*LocalTPSLCondition {
	upperSym := strings.ToUpper(strings.TrimSpace(symbol))

	if isLocalTPSLRedisEnabled() {
		redisConds, err := loadActiveTPSLFromRedis()
		if err == nil {
			filtered := filterTPSLBySymbol(redisConds, upperSym)
			if len(filtered) > 0 {
				return filtered
			}
			// Redis 可用但数据为空时，回退 DB，防止 Redis 丢数据导致漏单。
			dbConds, dbErr := loadActiveTPSLFromDB(upperSym)
			if dbErr == nil {
				for _, cond := range dbConds {
					upsertActiveTPSLToRedis(cond)
				}
				return dbConds
			}
			log.Printf("[LocalTPSL] DB fallback failed after Redis empty: %v", dbErr)
			return filtered
		}
		log.Printf("[LocalTPSL] Redis query failed, fallback DB: %v", err)
	}

	dbConds, dbErr := loadActiveTPSLFromDB(upperSym)
	if dbErr == nil && dbConds != nil {
		for _, cond := range dbConds {
			upsertActiveTPSLToRedis(cond)
		}
		return dbConds
	}
	if dbErr != nil {
		log.Printf("[LocalTPSL] DB query failed, fallback memory: %v", dbErr)
	}

	if tpslMonitor == nil {
		return nil
	}

	tpslMonitor.mu.RLock()
	defer tpslMonitor.mu.RUnlock()

	if upperSym == "" {
		// 返回所有
		var all []*LocalTPSLCondition
		for _, conds := range tpslMonitor.conditions {
			all = append(all, conds...)
		}
		return all
	}

	var result []*LocalTPSLCondition
	for _, conds := range tpslMonitor.conditions {
		for _, cond := range conds {
			if cond == nil {
				continue
			}
			if strings.EqualFold(strings.TrimSpace(cond.Symbol), upperSym) {
				result = append(result, cond)
			}
		}
	}
	return result
}

func filterTPSLBySymbol(conds []*LocalTPSLCondition, symbol string) []*LocalTPSLCondition {
	if len(conds) == 0 {
		return nil
	}
	if symbol == "" {
		result := make([]*LocalTPSLCondition, 0, len(conds))
		for _, cond := range conds {
			if cond != nil {
				result = append(result, cond)
			}
		}
		return result
	}

	result := make([]*LocalTPSLCondition, 0, len(conds))
	for _, cond := range conds {
		if cond == nil {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(cond.Symbol), symbol) {
			result = append(result, cond)
		}
	}
	return result
}

// GetTPSLHistory 查询止盈止损历史（已触发/已取消）
func GetTPSLHistory(symbol string, limit int) ([]LocalTPSLCondition, error) {
	if DB == nil {
		return nil, nil
	}

	var records []LocalTPSLCondition
	q := DB.Where("status IN ?", []string{"TRIGGERED", "CANCELLED"}).Order("updated_at DESC")
	if symbol != "" {
		q = q.Where("symbol = ?", symbol)
	}
	if limit > 0 {
		q = q.Limit(limit)
	}
	err := q.Find(&records).Error
	return records, err
}

// RegisterTrailingStop 注册移动止损条件
// side/positionSide 遵循平仓约定：平多 SELL/LONG，平空 BUY/SHORT，单向持仓 SELL 或 BUY / BOTH
// callbackRate: 回调比例，如 1.0 = 1%
// activationPrice: 激活价格，0 表示立即激活
// orderID: 关联主单 ID，无则传 0
func RegisterTrailingStop(
	symbol string,
	side string,
	positionSide string,
	quantity string,
	callbackRate float64,
	activationPrice float64,
	source string,
	orderID int64,
) (groupID string, err error) {
	if tpslMonitor == nil {
		return "", fmt.Errorf("local TPSL monitor not started")
	}
	if callbackRate <= 0 {
		return "", fmt.Errorf("callbackRate must be > 0")
	}
	if quantity == "" {
		return "", fmt.Errorf("quantity is required")
	}
	if side != "SELL" && side != "BUY" {
		return "", fmt.Errorf("side must be SELL or BUY")
	}
	if positionSide == "" {
		positionSide = string(futures.PositionSideTypeBoth)
	}
	if source == "" {
		source = "manual"
	}

	groupID = uuid.New().String()

	cond := &LocalTPSLCondition{
		GroupID:                 groupID,
		Symbol:                  symbol,
		ConditionType:           "TRAILING_STOP",
		Side:                    side,
		PositionSide:            positionSide,
		TriggerPrice:            0, // 动态计算，触发时更新
		Quantity:                quantity,
		LevelIndex:              -1,
		TotalLevels:             1,
		Status:                  "ACTIVE",
		OrderID:                 orderID,
		Source:                  source,
		TrailingCallbackRate:    callbackRate,
		TrailingActivationPrice: activationPrice,
		TrailingHighestPrice:    0,
		TrailingActivated:       activationPrice <= 0, // 无激活价则直接激活
	}

	// 如果立即激活，尝试用当前价格初始化追踪极值
	if cond.TrailingActivated {
		if p, perr := GetPriceCache().GetPrice(symbol); perr == nil {
			cond.TrailingHighestPrice = p
		}
	}

	if DB != nil {
		if err := DB.Create(cond).Error; err != nil {
			return "", fmt.Errorf("save trailing stop: %w", err)
		}
	}
	tpslMonitor.addToMemory(cond)

	// 确保价格缓存订阅了该 symbol
	_ = GetPriceCache().Subscribe(symbol)

	log.Printf("[LocalTPSL][TS] Registered trailing stop for %s: side=%s positionSide=%s qty=%s callbackRate=%.2f%% activationPrice=%.4f groupID=%s",
		symbol, side, positionSide, quantity, callbackRate, activationPrice, groupID)

	return groupID, nil
}

// --- HTTP Handlers ---

// HandleGetTPSLList GET /tool/tpsl/list?symbol=BTCUSDT
func HandleGetTPSLList(c context.Context, ctx *app.RequestContext) {
	symbol := ctx.DefaultQuery("symbol", "")
	conditions := GetActiveTPSLConditions(symbol)
	ctx.JSON(http.StatusOK, utils.H{"data": conditions})
}

// HandleCancelTPSL POST /tool/tpsl/cancel
func HandleCancelTPSL(c context.Context, ctx *app.RequestContext) {
	var req struct {
		ID      uint   `json:"id,omitempty"`
		GroupID string `json:"groupId,omitempty"`
	}
	if err := ctx.BindAndValidate(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, utils.H{"error": err.Error()})
		return
	}

	var err error
	if req.GroupID != "" {
		err = CancelTPSLByGroup(req.GroupID)
	} else if req.ID > 0 {
		err = CancelTPSLByID(req.ID)
	} else {
		ctx.JSON(http.StatusBadRequest, utils.H{"error": "id or groupId is required"})
		return
	}

	if err != nil {
		ctx.JSON(http.StatusBadRequest, utils.H{"error": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, utils.H{"message": "cancelled"})
}

// HandleGetTPSLHistory GET /tool/tpsl/history?symbol=BTCUSDT&limit=50
func HandleGetTPSLHistory(c context.Context, ctx *app.RequestContext) {
	symbol := ctx.DefaultQuery("symbol", "")
	limitStr := ctx.DefaultQuery("limit", "50")
	limit, _ := strconv.Atoi(limitStr)
	if limit <= 0 {
		limit = 50
	}

	records, err := GetTPSLHistory(symbol, limit)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, utils.H{"error": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, utils.H{"data": records})
}

// HandleSetTrailingStop POST /tool/tpsl/trailing
// 请求体：{"symbol":"BTCUSDT","positionSide":"LONG","callbackRate":1.0,"activationPrice":50000,"quantity":"0.01"}
// activationPrice 可选（传 0 或不传则立即激活）
// positionSide: LONG / SHORT / BOTH
// quantity: 平仓数量字符串
func HandleSetTrailingStop(c context.Context, ctx *app.RequestContext) {
	var req struct {
		Symbol          string  `json:"symbol"`
		PositionSide    string  `json:"positionSide"`
		CallbackRate    float64 `json:"callbackRate"`
		ActivationPrice float64 `json:"activationPrice,omitempty"`
		Quantity        string  `json:"quantity"`
		OrderID         int64   `json:"orderId,omitempty"`
		Source          string  `json:"source,omitempty"`
	}
	if err := ctx.BindAndValidate(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, utils.H{"error": err.Error()})
		return
	}

	if req.Symbol == "" {
		ctx.JSON(http.StatusBadRequest, utils.H{"error": "symbol is required"})
		return
	}
	if req.CallbackRate <= 0 {
		ctx.JSON(http.StatusBadRequest, utils.H{"error": "callbackRate must be > 0"})
		return
	}
	if req.Quantity == "" {
		ctx.JSON(http.StatusBadRequest, utils.H{"error": "quantity is required"})
		return
	}

	// 根据 positionSide 推断平仓 side
	side := "SELL" // 默认平多
	positionSide := req.PositionSide
	switch positionSide {
	case "LONG", "":
		side = "SELL"
		if positionSide == "" {
			positionSide = "LONG"
		}
	case "SHORT":
		side = "BUY"
	case "BOTH":
		// 单向持仓，需要调用方通过 source 或前端区分；默认 SELL 平多
		side = "SELL"
	default:
		ctx.JSON(http.StatusBadRequest, utils.H{"error": "positionSide must be LONG / SHORT / BOTH"})
		return
	}

	groupID, err := RegisterTrailingStop(
		req.Symbol,
		side,
		positionSide,
		req.Quantity,
		req.CallbackRate,
		req.ActivationPrice,
		req.Source,
		req.OrderID,
	)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, utils.H{"error": err.Error()})
		return
	}

	ctx.JSON(http.StatusOK, utils.H{
		"groupId": groupID,
		"message": "trailing stop registered",
	})
}
