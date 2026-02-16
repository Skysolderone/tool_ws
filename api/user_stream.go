package api

import (
	"context"
	"log"
	"math"
	"strconv"
	"sync"
	"time"

	"github.com/adshao/go-binance/v2/futures"
)

// userStreamManager 管理 User Data Stream 生命周期
type userStreamManager struct {
	mu      sync.Mutex
	stopC   chan struct{}
	doneC   chan struct{}
	running bool
}

var userStream = &userStreamManager{}

// StartUserStream 启动 User Data Stream 监听
// 监听订单更新事件，自动更新数据库中的交易记录
func StartUserStream() {
	userStream.mu.Lock()
	if userStream.running {
		userStream.mu.Unlock()
		return
	}
	userStream.mu.Unlock()

	go userStreamLoop()
}

// StopUserStream 停止 User Data Stream
func StopUserStream() {
	userStream.mu.Lock()
	defer userStream.mu.Unlock()

	if userStream.running && userStream.stopC != nil {
		close(userStream.stopC)
		userStream.running = false
		log.Println("[UserStream] Stopped")
	}
}

// userStreamLoop 带重连的 User Data Stream 主循环
func userStreamLoop() {
	backoff := time.Second * 2
	maxBackoff := time.Minute * 2

	for {
		err := connectUserStream()
		if err != nil {
			log.Printf("[UserStream] Connection failed: %v, retrying in %v", err, backoff)
			time.Sleep(backoff)
			backoff = time.Duration(math.Min(float64(backoff)*2, float64(maxBackoff)))
			continue
		}
		// 连接成功后重置 backoff
		backoff = time.Second * 2

		// 等待断线
		userStream.mu.Lock()
		doneC := userStream.doneC
		userStream.mu.Unlock()

		if doneC != nil {
			<-doneC
		}

		log.Println("[UserStream] Disconnected, reconnecting in 3s...")
		time.Sleep(3 * time.Second)
	}
}

// connectUserStream 建立一次 User Data Stream 连接
func connectUserStream() error {
	ctx := context.Background()

	handler := func(event *futures.WsUserDataEvent) {
		handleUserDataEvent(event)
	}

	errHandler := func(err error) {
		log.Printf("[UserStream] Error: %v", err)
	}

	doneC, stopC, err := WsUserData(ctx, handler, errHandler)
	if err != nil {
		return err
	}

	userStream.mu.Lock()
	userStream.doneC = doneC
	userStream.stopC = stopC
	userStream.running = true
	userStream.mu.Unlock()

	log.Println("[UserStream] Connected, listening for order updates...")
	return nil
}

// handleUserDataEvent 处理 User Data Stream 事件
func handleUserDataEvent(event *futures.WsUserDataEvent) {
	if event == nil {
		return
	}

	switch event.Event {
	case futures.UserDataEventTypeOrderTradeUpdate:
		handleOrderUpdate(event.OrderTradeUpdate)
	case futures.UserDataEventTypeAccountUpdate:
		handleAccountUpdate(event.AccountUpdate)
	}
}

// handleOrderUpdate 处理订单更新事件
// 当订单成交、部分成交或被取消时，更新数据库中对应的交易记录
func handleOrderUpdate(update futures.WsOrderTradeUpdate) {
	if DB == nil {
		return
	}

	orderID := update.ID
	status := string(update.Status)
	realizedPnl := update.RealizedPnL

	log.Printf("[UserStream] OrderUpdate: orderId=%d, symbol=%s, status=%s, realizedPnl=%s",
		orderID, update.Symbol, status, realizedPnl)

	// 查找关联的交易记录
	// 1. 先按 orderID 精确匹配（开仓主单）
	record, err := GetTradeByOrderID(orderID)
	if err == nil && record != nil {
		updateTradeFromOrder(record, update)
		return
	}

	// 2. 如果是平仓单（reduceOnly / 止盈止损触发），找到对应的 OPEN 记录
	if realizedPnl != "" && realizedPnl != "0" && realizedPnl != "0.00000000" {
		pnl, _ := strconv.ParseFloat(realizedPnl, 64)
		if pnl != 0 {
			updateOpenTradeWithPnl(update)
		}
	}
}

// updateTradeFromOrder 更新主单的交易记录
func updateTradeFromOrder(record *TradeRecord, update futures.WsOrderTradeUpdate) {
	changed := false

	// 更新成交均价
	if update.OriginalPrice != "" && update.OriginalPrice != "0" {
		// 如果有 lastFilledPrice 更有意义
	}
	if update.AveragePrice != "" && update.AveragePrice != "0" {
		record.Price = update.AveragePrice
		changed = true
	}

	// 更新数量
	if update.AccumulatedFilledQty != "" && update.AccumulatedFilledQty != "0" {
		record.Quantity = update.AccumulatedFilledQty
		changed = true
	}

	// 更新 realizedPnl
	if update.RealizedPnL != "" && update.RealizedPnL != "0" && update.RealizedPnL != "0.00000000" {
		record.RealizedPnl = update.RealizedPnL
		changed = true
	}

	// 订单完全成交或取消 → 更新状态
	switch update.Status {
	case futures.OrderStatusTypeFilled:
		// 开仓单成交后还是 OPEN 状态（等待平仓时再改 CLOSED）
		changed = true
	case futures.OrderStatusTypeCanceled, futures.OrderStatusTypeExpired, futures.OrderStatusTypeRejected:
		record.Status = "CANCELED"
		changed = true
	}

	if changed {
		if err := UpdateTradeRecord(record); err != nil {
			log.Printf("[UserStream] Failed to update trade record %d: %v", record.ID, err)
		} else {
			log.Printf("[UserStream] Updated trade record: id=%d, orderId=%d, price=%s", record.ID, record.OrderID, record.Price)
		}
	}
}

// updateOpenTradeWithPnl 当平仓单产生 realizedPnl 时，更新对应的 OPEN 记录
func updateOpenTradeWithPnl(update futures.WsOrderTradeUpdate) {
	if DB == nil {
		return
	}

	symbol := update.Symbol
	positionSide := string(update.PositionSide)
	realizedPnl := update.RealizedPnL

	// 找到最近的同 symbol + positionSide 的 OPEN 记录
	var record TradeRecord
	q := DB.Where("symbol = ? AND status = ?", symbol, "OPEN").
		Order("created_at DESC")

	if positionSide != "" && positionSide != "BOTH" {
		q = q.Where("position_side = ?", positionSide)
	}

	if err := q.First(&record).Error; err != nil {
		// 没找到 OPEN 记录，可能是手动在交易所下的单
		log.Printf("[UserStream] No OPEN trade found for %s %s, skip PnL update", symbol, positionSide)
		return
	}

	// 累加 realizedPnl（可能多次部分平仓）
	oldPnl, _ := strconv.ParseFloat(record.RealizedPnl, 64)
	newPnl, _ := strconv.ParseFloat(realizedPnl, 64)
	record.RealizedPnl = strconv.FormatFloat(oldPnl+newPnl, 'f', 8, 64)

	// 判断是否完全平仓：查询该 symbol 的当前仓位
	ctx := context.Background()
	positions, err := Client.NewGetPositionRiskService().Symbol(symbol).Do(ctx)
	if err == nil {
		allClosed := true
		for _, pos := range positions {
			if string(pos.PositionSide) == positionSide || positionSide == "BOTH" {
				amt, _ := strconv.ParseFloat(pos.PositionAmt, 64)
				if amt != 0 {
					allClosed = false
					break
				}
			}
		}
		if allClosed {
			record.Status = "CLOSED"
			log.Printf("[UserStream] Position fully closed: %s %s, PnL=%s", symbol, positionSide, record.RealizedPnl)
		}
	}

	if err := UpdateTradeRecord(&record); err != nil {
		log.Printf("[UserStream] Failed to update PnL for trade %d: %v", record.ID, err)
	} else {
		log.Printf("[UserStream] Updated PnL: id=%d, symbol=%s, pnl=%s, status=%s",
			record.ID, symbol, record.RealizedPnl, record.Status)
	}

	// 通知风控模块
	pnlFloat, _ := strconv.ParseFloat(realizedPnl, 64)
	if pnlFloat != 0 {
		AddDailyPnl(pnlFloat)
	}
}

// handleAccountUpdate 处理账户更新事件（余额变动等）
// 目前用于日志记录，后续可用于风控
func handleAccountUpdate(update futures.WsAccountUpdate) {
	for _, b := range update.Balances {
		if b.Asset == "USDT" {
			log.Printf("[UserStream] Balance update: USDT balance=%s, crossWallet=%s",
				b.Balance, b.CrossWalletBalance)
		}
	}
}
