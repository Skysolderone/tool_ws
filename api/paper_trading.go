package api

import (
	"fmt"
	"log"
	"sync"
	"sync/atomic"
	"time"
)

// paperEngine 全局模拟交易引擎实例
var paperEngine *PaperEngine

// PaperPosition 模拟持仓
type PaperPosition struct {
	Symbol     string    `json:"symbol"`
	Side       string    `json:"side"`       // LONG / SHORT
	EntryPrice float64   `json:"entryPrice"`
	Quantity   float64   `json:"quantity"`
	Leverage   int       `json:"leverage"`
	OpenTime   time.Time `json:"openTime"`
}

// PaperTrade 模拟成交记录
type PaperTrade struct {
	ID       int64     `json:"id"`
	Symbol   string    `json:"symbol"`
	Side     string    `json:"side"`     // LONG / SHORT
	Action   string    `json:"action"`   // OPEN / CLOSE / REDUCE
	Price    float64   `json:"price"`
	Quantity float64   `json:"quantity"`
	PnL      float64   `json:"pnl"`
	Fee      float64   `json:"fee"`
	Time     time.Time `json:"time"`
	Reason   string    `json:"reason"`
}

// PaperEngine 模拟撮合引擎
type PaperEngine struct {
	mu        sync.RWMutex
	enabled   bool
	balance   float64                    // 虚拟可用余额（USDT）
	positions map[string]*PaperPosition  // key: symbol
	trades    []PaperTrade
	tradeSeq  atomic.Int64
}

// InitPaperEngine 初始化全局模拟交易引擎
// enabled=true 时开启模拟模式，余额初始化为 10000 USDT
func InitPaperEngine(enabled bool) {
	paperEngine = &PaperEngine{
		enabled:   enabled,
		balance:   10000.0,
		positions: make(map[string]*PaperPosition),
		trades:    make([]PaperTrade, 0),
	}
	if enabled {
		log.Printf("[PaperTrading] DryRun mode enabled, virtual balance = 10000 USDT")
	}
}

// IsDryRun 返回当前是否处于模拟交易模式
func IsDryRun() bool {
	if paperEngine == nil {
		return false
	}
	return paperEngine.enabled
}

// calcFee 计算手续费（双边 taker 费率合计 0.04%）
func calcFee(price, quantity float64) float64 {
	return quantity * price * 0.0004
}

// PaperPlaceOrder 模拟开仓
// side: "LONG" 或 "SHORT"
// quantity: 合约张数
// price: 成交价（传 0 时从 PriceCache 自动获取）
// leverage: 杠杆倍数
func PaperPlaceOrder(symbol, side string, quantity, price float64, leverage int, reason string) (*PaperTrade, error) {
	if paperEngine == nil {
		return nil, fmt.Errorf("paper engine not initialized")
	}

	// 如果 price 为 0，从价格缓存获取实时价格
	if price <= 0 {
		p, err := GetPriceCache().GetPrice(symbol)
		if err != nil {
			return nil, fmt.Errorf("paper order: get price for %s: %w", symbol, err)
		}
		price = p
	}

	fee := calcFee(price, quantity)
	// 保证金 = price * quantity / leverage
	margin := price * quantity / float64(leverage)
	cost := margin + fee

	paperEngine.mu.Lock()
	defer paperEngine.mu.Unlock()

	if paperEngine.balance < cost {
		return nil, fmt.Errorf("paper order: insufficient balance (need %.4f, have %.4f)", cost, paperEngine.balance)
	}

	// 检查是否已有同方向持仓，如有则合并（均价）
	existing, ok := paperEngine.positions[symbol]
	if ok && existing.Side == side {
		// 合并持仓：加权平均入场价
		totalQty := existing.Quantity + quantity
		existing.EntryPrice = (existing.EntryPrice*existing.Quantity + price*quantity) / totalQty
		existing.Quantity = totalQty
		if leverage > 0 {
			existing.Leverage = leverage
		}
	} else if ok && existing.Side != side {
		// 反向持仓：先平旧仓再开新仓（此处简化处理：返回错误，要求先手动平仓）
		paperEngine.mu.Unlock()
		_, closeErr := PaperReducePosition(symbol, existing.Side, existing.Quantity, price, "reverse open")
		paperEngine.mu.Lock()
		if closeErr != nil {
			return nil, fmt.Errorf("paper order: close opposite position failed: %w", closeErr)
		}
		paperEngine.positions[symbol] = &PaperPosition{
			Symbol:     symbol,
			Side:       side,
			EntryPrice: price,
			Quantity:   quantity,
			Leverage:   leverage,
			OpenTime:   time.Now(),
		}
	} else {
		paperEngine.positions[symbol] = &PaperPosition{
			Symbol:     symbol,
			Side:       side,
			EntryPrice: price,
			Quantity:   quantity,
			Leverage:   leverage,
			OpenTime:   time.Now(),
		}
	}

	paperEngine.balance -= cost

	trade := PaperTrade{
		ID:       paperEngine.tradeSeq.Add(1),
		Symbol:   symbol,
		Side:     side,
		Action:   "OPEN",
		Price:    price,
		Quantity: quantity,
		PnL:      0,
		Fee:      fee,
		Time:     time.Now(),
		Reason:   reason,
	}
	paperEngine.trades = append(paperEngine.trades, trade)

	log.Printf("[PaperTrading] OPEN %s %s qty=%.4f price=%.4f fee=%.4f balance=%.4f",
		side, symbol, quantity, price, fee, paperEngine.balance)
	return &trade, nil
}

// PaperReducePosition 模拟减仓或平仓
// side: 被平仓方向（"LONG" 或 "SHORT"），即原有持仓方向
// quantity: 平仓数量，传 0 则全平
// price: 成交价（传 0 时从 PriceCache 自动获取）
func PaperReducePosition(symbol, side string, quantity, price float64, reason string) (*PaperTrade, error) {
	if paperEngine == nil {
		return nil, fmt.Errorf("paper engine not initialized")
	}

	// 如果 price 为 0，从价格缓存获取实时价格
	if price <= 0 {
		p, err := GetPriceCache().GetPrice(symbol)
		if err != nil {
			return nil, fmt.Errorf("paper reduce: get price for %s: %w", symbol, err)
		}
		price = p
	}

	paperEngine.mu.Lock()
	defer paperEngine.mu.Unlock()

	pos, ok := paperEngine.positions[symbol]
	if !ok {
		return nil, fmt.Errorf("paper reduce: no open position for %s", symbol)
	}
	if pos.Side != side {
		return nil, fmt.Errorf("paper reduce: position side mismatch (have %s, got %s)", pos.Side, side)
	}

	// quantity=0 视为全平
	if quantity <= 0 || quantity >= pos.Quantity {
		quantity = pos.Quantity
	}

	// PnL 计算
	var pnl float64
	if side == "LONG" {
		pnl = (price - pos.EntryPrice) * quantity
	} else { // SHORT
		pnl = (pos.EntryPrice - price) * quantity
	}

	fee := calcFee(price, quantity)
	pnl -= fee

	// 归还保证金
	margin := pos.EntryPrice * quantity / float64(pos.Leverage)
	paperEngine.balance += margin + pnl

	// 更新或删除持仓
	if quantity >= pos.Quantity {
		delete(paperEngine.positions, symbol)
	} else {
		pos.Quantity -= quantity
	}

	action := "REDUCE"
	if _, stillHas := paperEngine.positions[symbol]; !stillHas {
		action = "CLOSE"
	}

	trade := PaperTrade{
		ID:       paperEngine.tradeSeq.Add(1),
		Symbol:   symbol,
		Side:     side,
		Action:   action,
		Price:    price,
		Quantity: quantity,
		PnL:      pnl,
		Fee:      fee,
		Time:     time.Now(),
		Reason:   reason,
	}
	paperEngine.trades = append(paperEngine.trades, trade)

	log.Printf("[PaperTrading] %s %s %s qty=%.4f price=%.4f pnl=%.4f fee=%.4f balance=%.4f",
		action, side, symbol, quantity, price, pnl, fee, paperEngine.balance)
	return &trade, nil
}

// GetPaperPositions 获取当前所有模拟持仓快照
func GetPaperPositions() []PaperPosition {
	if paperEngine == nil {
		return nil
	}
	paperEngine.mu.RLock()
	defer paperEngine.mu.RUnlock()

	result := make([]PaperPosition, 0, len(paperEngine.positions))
	for _, p := range paperEngine.positions {
		result = append(result, *p)
	}
	return result
}

// GetPaperBalance 获取当前模拟账户余额
func GetPaperBalance() float64 {
	if paperEngine == nil {
		return 0
	}
	paperEngine.mu.RLock()
	defer paperEngine.mu.RUnlock()
	return paperEngine.balance
}

// GetPaperTrades 获取所有模拟成交记录（副本）
func GetPaperTrades() []PaperTrade {
	if paperEngine == nil {
		return nil
	}
	paperEngine.mu.RLock()
	defer paperEngine.mu.RUnlock()

	result := make([]PaperTrade, len(paperEngine.trades))
	copy(result, paperEngine.trades)
	return result
}

// GetPaperStatus 获取模拟账户完整状态（余额 + 持仓 + 最近成交）
type PaperStatusResp struct {
	Enabled   bool            `json:"enabled"`
	Balance   float64         `json:"balance"`
	Positions []PaperPosition `json:"positions"`
	Trades    []PaperTrade    `json:"trades"`
	TotalPnL  float64         `json:"totalPnl"`
}

func GetPaperStatus() PaperStatusResp {
	if paperEngine == nil {
		return PaperStatusResp{}
	}
	paperEngine.mu.RLock()
	defer paperEngine.mu.RUnlock()

	positions := make([]PaperPosition, 0, len(paperEngine.positions))
	for _, p := range paperEngine.positions {
		positions = append(positions, *p)
	}

	trades := make([]PaperTrade, len(paperEngine.trades))
	copy(trades, paperEngine.trades)

	var totalPnL float64
	for _, t := range trades {
		totalPnL += t.PnL
	}

	return PaperStatusResp{
		Enabled:   paperEngine.enabled,
		Balance:   paperEngine.balance,
		Positions: positions,
		Trades:    trades,
		TotalPnL:  totalPnL,
	}
}

// ResetPaper 重置模拟账户，清空持仓和交易记录，余额恢复为 10000 USDT
func ResetPaper() {
	if paperEngine == nil {
		return
	}
	paperEngine.mu.Lock()
	defer paperEngine.mu.Unlock()

	paperEngine.balance = 10000.0
	paperEngine.positions = make(map[string]*PaperPosition)
	paperEngine.trades = make([]PaperTrade, 0)
	paperEngine.tradeSeq.Store(0)

	log.Printf("[PaperTrading] Account reset, balance = 10000 USDT")
}
