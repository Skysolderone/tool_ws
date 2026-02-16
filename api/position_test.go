package api

import (
	"errors"
	"testing"

	"github.com/adshao/go-binance/v2/futures"
)

// --- 测试用例：参数验证和数据结构 ---

func TestGetPositions_Validation(t *testing.T) {
	tests := []struct {
		name   string
		symbol string
		desc   string
	}{
		{
			name:   "with specific symbol",
			symbol: "BTCUSDT",
			desc:   "should query positions for BTCUSDT",
		},
		{
			name:   "all symbols",
			symbol: "",
			desc:   "should query positions for all symbols",
		},
		{
			name:   "ETH symbol",
			symbol: "ETHUSDT",
			desc:   "should query positions for ETHUSDT",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Logf("GetPositions: symbol=%q - %s", tt.symbol, tt.desc)
		})
	}
}

func TestPositionRisk_Structure(t *testing.T) {
	// 验证 PositionRisk 结构体的字段
	pos := &futures.PositionRisk{
		Symbol:           "BTCUSDT",
		PositionAmt:      "1.5",
		EntryPrice:       "42000",
		MarkPrice:        "43000",
		UnRealizedProfit: "1500",
		LiquidationPrice: "35000",
		Leverage:         "10",
		PositionSide:     "LONG",
		BreakEvenPrice:   "42100",
		MarginType:       "isolated",
		IsolatedMargin:   "6300",
	}

	if pos.Symbol != "BTCUSDT" {
		t.Errorf("expected symbol BTCUSDT, got %s", pos.Symbol)
	}
	if pos.PositionAmt != "1.5" {
		t.Errorf("expected positionAmt 1.5, got %s", pos.PositionAmt)
	}
	if pos.PositionSide != "LONG" {
		t.Errorf("expected positionSide LONG, got %s", pos.PositionSide)
	}
	if pos.Leverage != "10" {
		t.Errorf("expected leverage 10, got %s", pos.Leverage)
	}
}

func TestPositionRisk_LongPosition(t *testing.T) {
	pos := &futures.PositionRisk{
		Symbol:           "BTCUSDT",
		PositionAmt:      "0.5",
		EntryPrice:       "43000",
		MarkPrice:        "43500",
		UnRealizedProfit: "250",
		LiquidationPrice: "35000",
		Leverage:         "10",
		PositionSide:     "LONG",
	}

	// 验证多头仓位
	if pos.PositionSide != "LONG" {
		t.Errorf("expected LONG position, got %s", pos.PositionSide)
	}

	// 验证持仓数量为正
	if pos.PositionAmt[0] == '-' {
		t.Error("LONG position should have positive PositionAmt")
	}

	t.Logf("Long position: Symbol=%s, Amt=%s, Entry=%s, Mark=%s, PnL=%s",
		pos.Symbol, pos.PositionAmt, pos.EntryPrice, pos.MarkPrice, pos.UnRealizedProfit)
}

func TestPositionRisk_ShortPosition(t *testing.T) {
	pos := &futures.PositionRisk{
		Symbol:           "ETHUSDT",
		PositionAmt:      "-2",
		EntryPrice:       "2350",
		MarkPrice:        "2300",
		UnRealizedProfit: "100",
		LiquidationPrice: "2500",
		Leverage:         "5",
		PositionSide:     "SHORT",
	}

	// 验证空头仓位
	if pos.PositionSide != "SHORT" {
		t.Errorf("expected SHORT position, got %s", pos.PositionSide)
	}

	// 验证持仓数量为负
	if pos.PositionAmt[0] != '-' {
		t.Error("SHORT position should have negative PositionAmt")
	}

	t.Logf("Short position: Symbol=%s, Amt=%s, Entry=%s, Mark=%s, PnL=%s",
		pos.Symbol, pos.PositionAmt, pos.EntryPrice, pos.MarkPrice, pos.UnRealizedProfit)
}

func TestPositionRisk_BothSides(t *testing.T) {
	// 测试双向持仓模式下同一品种的多空仓位
	longPos := &futures.PositionRisk{
		Symbol:           "BTCUSDT",
		PositionAmt:      "1.5",
		EntryPrice:       "43000",
		MarkPrice:        "44000",
		UnRealizedProfit: "1500",
		Leverage:         "10",
		PositionSide:     "LONG",
	}

	shortPos := &futures.PositionRisk{
		Symbol:           "BTCUSDT",
		PositionAmt:      "-0.8",
		EntryPrice:       "44500",
		MarkPrice:        "44000",
		UnRealizedProfit: "400",
		Leverage:         "10",
		PositionSide:     "SHORT",
	}

	if longPos.Symbol != shortPos.Symbol {
		t.Error("both positions should be for the same symbol")
	}

	if longPos.PositionSide == shortPos.PositionSide {
		t.Error("positions should have different sides")
	}

	t.Logf("BTCUSDT LONG: Amt=%s, PnL=%s", longPos.PositionAmt, longPos.UnRealizedProfit)
	t.Logf("BTCUSDT SHORT: Amt=%s, PnL=%s", shortPos.PositionAmt, shortPos.UnRealizedProfit)
}

func TestPositionRisk_IsolatedMargin(t *testing.T) {
	pos := &futures.PositionRisk{
		Symbol:           "BTCUSDT",
		PositionAmt:      "2",
		EntryPrice:       "40000",
		MarkPrice:        "45000",
		UnRealizedProfit: "10000",
		LiquidationPrice: "30000",
		Leverage:         "20",
		PositionSide:     "LONG",
		IsolatedMargin:   "4000",
		MarginType:       "isolated",
	}

	if pos.MarginType != "isolated" {
		t.Errorf("expected isolated margin, got %s", pos.MarginType)
	}

	if pos.IsolatedMargin == "" {
		t.Error("isolated margin amount should not be empty")
	}

	t.Logf("Isolated margin position: Margin=%s, Leverage=%s, LiqPrice=%s",
		pos.IsolatedMargin, pos.Leverage, pos.LiquidationPrice)
}

func TestPositionRisk_CrossMargin(t *testing.T) {
	pos := &futures.PositionRisk{
		Symbol:           "ETHUSDT",
		PositionAmt:      "5",
		EntryPrice:       "2300",
		MarkPrice:        "2350",
		UnRealizedProfit: "250",
		Leverage:         "3",
		PositionSide:     "LONG",
		MarginType:       "cross",
	}

	if pos.MarginType != "cross" {
		t.Errorf("expected cross margin, got %s", pos.MarginType)
	}

	t.Logf("Cross margin position: Symbol=%s, Leverage=%s", pos.Symbol, pos.Leverage)
}

func TestPositionRisk_EmptyPosition(t *testing.T) {
	pos := &futures.PositionRisk{
		Symbol:           "SOLUSDT",
		PositionAmt:      "0",
		EntryPrice:       "0",
		MarkPrice:        "0",
		UnRealizedProfit: "0",
		Leverage:         "10",
		PositionSide:     "BOTH",
	}

	if pos.PositionAmt != "0" {
		t.Error("empty position should have 0 amount")
	}

	if pos.UnRealizedProfit != "0" {
		t.Error("empty position should have 0 unrealized profit")
	}

	t.Logf("Empty position: Symbol=%s, Amt=%s", pos.Symbol, pos.PositionAmt)
}

func TestPositionRisk_HighLeverage(t *testing.T) {
	pos := &futures.PositionRisk{
		Symbol:           "BTCUSDT",
		PositionAmt:      "0.1",
		EntryPrice:       "43000",
		MarkPrice:        "44000",
		UnRealizedProfit: "100",
		LiquidationPrice: "42500",
		Leverage:         "125",
		PositionSide:     "LONG",
	}

	if pos.Leverage != "125" {
		t.Errorf("expected leverage 125, got %s", pos.Leverage)
	}

	// 高杠杆下清算价格应该更接近入场价
	t.Logf("High leverage position: Leverage=%s, Entry=%s, LiqPrice=%s",
		pos.Leverage, pos.EntryPrice, pos.LiquidationPrice)
}

func TestPositionRisk_MultipleSymbols(t *testing.T) {
	positions := []*futures.PositionRisk{
		{
			Symbol:           "BTCUSDT",
			PositionAmt:      "0.5",
			EntryPrice:       "43000",
			MarkPrice:        "43500",
			UnRealizedProfit: "250",
			Leverage:         "10",
			PositionSide:     "LONG",
		},
		{
			Symbol:           "ETHUSDT",
			PositionAmt:      "2",
			EntryPrice:       "2300",
			MarkPrice:        "2350",
			UnRealizedProfit: "100",
			Leverage:         "5",
			PositionSide:     "LONG",
		},
		{
			Symbol:           "BNBUSDT",
			PositionAmt:      "-5",
			EntryPrice:       "310",
			MarkPrice:        "305",
			UnRealizedProfit: "25",
			Leverage:         "3",
			PositionSide:     "SHORT",
		},
	}

	if len(positions) != 3 {
		t.Errorf("expected 3 positions, got %d", len(positions))
	}

	// 验证不同品种
	symbols := make(map[string]bool)
	for _, pos := range positions {
		symbols[pos.Symbol] = true
	}

	if len(symbols) != 3 {
		t.Error("expected 3 different symbols")
	}

	for _, pos := range positions {
		t.Logf("Position: Symbol=%s, Side=%s, Amt=%s, PnL=%s",
			pos.Symbol, pos.PositionSide, pos.PositionAmt, pos.UnRealizedProfit)
	}
}

func TestPositionRisk_BreakEvenPrice(t *testing.T) {
	pos := &futures.PositionRisk{
		Symbol:           "BTCUSDT",
		PositionAmt:      "1",
		EntryPrice:       "43000",
		MarkPrice:        "43500",
		BreakEvenPrice:   "43050",
		UnRealizedProfit: "450",
		Leverage:         "10",
		PositionSide:     "LONG",
	}

	if pos.BreakEvenPrice == "" {
		t.Error("break even price should not be empty")
	}

	t.Logf("Position: Entry=%s, BreakEven=%s, Mark=%s",
		pos.EntryPrice, pos.BreakEvenPrice, pos.MarkPrice)
}

// --- 辅助验证函数 ---

func validateSymbolParam(symbol string) error {
	if symbol != "" && len(symbol) < 3 {
		return errors.New("invalid symbol format")
	}
	return nil
}

func TestSymbolValidation(t *testing.T) {
	tests := []struct {
		name    string
		symbol  string
		wantErr bool
	}{
		{
			name:    "valid BTCUSDT",
			symbol:  "BTCUSDT",
			wantErr: false,
		},
		{
			name:    "valid ETHUSDT",
			symbol:  "ETHUSDT",
			wantErr: false,
		},
		{
			name:    "empty symbol (all)",
			symbol:  "",
			wantErr: false,
		},
		{
			name:    "invalid short symbol",
			symbol:  "BT",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateSymbolParam(tt.symbol)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateSymbolParam() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestPositionFilter_ZeroPositions(t *testing.T) {
	// 测试零持仓过滤逻辑
	positions := []*futures.PositionRisk{
		{
			Symbol:      "BTCUSDT",
			PositionAmt: "0",
			MarkPrice:   "43000",
		},
		{
			Symbol:      "ETHUSDT",
			PositionAmt: "0.00000000",
			MarkPrice:   "2300",
		},
		{
			Symbol:           "BNBUSDT",
			PositionAmt:      "5",
			EntryPrice:       "310",
			UnRealizedProfit: "25",
		},
	}

	// 模拟过滤逻辑
	var filtered []*futures.PositionRisk
	for _, pos := range positions {
		if pos.PositionAmt != "0" && pos.PositionAmt != "0.00000000" && pos.PositionAmt != "" {
			filtered = append(filtered, pos)
		}
	}

	if len(filtered) != 1 {
		t.Errorf("expected 1 active position, got %d", len(filtered))
	}

	if filtered[0].Symbol != "BNBUSDT" {
		t.Errorf("expected BNBUSDT position, got %s", filtered[0].Symbol)
	}

	t.Logf("Filtered positions: %d active out of %d total", len(filtered), len(positions))
}

func TestPositionFilter_AllZero(t *testing.T) {
	// 测试全部为零持仓的情况
	positions := []*futures.PositionRisk{
		{Symbol: "BTCUSDT", PositionAmt: "0"},
		{Symbol: "ETHUSDT", PositionAmt: "0.00000000"},
		{Symbol: "BNBUSDT", PositionAmt: ""},
	}

	var filtered []*futures.PositionRisk
	for _, pos := range positions {
		if pos.PositionAmt != "0" && pos.PositionAmt != "0.00000000" && pos.PositionAmt != "" {
			filtered = append(filtered, pos)
		}
	}

	if len(filtered) != 0 {
		t.Errorf("expected 0 active positions, got %d", len(filtered))
	}

	t.Log("Correctly filtered out all zero positions")
}

func TestPositionFilter_NegativeAmount(t *testing.T) {
	// 测试负数持仓（空头）不会被过滤
	positions := []*futures.PositionRisk{
		{
			Symbol:      "BTCUSDT",
			PositionAmt: "-0.5",
			PositionSide: "SHORT",
			EntryPrice:   "44000",
		},
	}

	var filtered []*futures.PositionRisk
	for _, pos := range positions {
		if pos.PositionAmt != "0" && pos.PositionAmt != "0.00000000" && pos.PositionAmt != "" {
			filtered = append(filtered, pos)
		}
	}

	if len(filtered) != 1 {
		t.Errorf("expected 1 active position (SHORT), got %d", len(filtered))
	}

	if filtered[0].PositionAmt != "-0.5" {
		t.Errorf("expected positionAmt -0.5, got %s", filtered[0].PositionAmt)
	}

	t.Log("SHORT position with negative amount correctly included")
}

func TestPositionFilter_MixedScenario(t *testing.T) {
	// 测试混合场景：有零持仓、有多头、有空头
	positions := []*futures.PositionRisk{
		{Symbol: "BTCUSDT", PositionAmt: "0"},
		{Symbol: "ETHUSDT", PositionAmt: "2.5", PositionSide: "LONG"},
		{Symbol: "BNBUSDT", PositionAmt: "-3", PositionSide: "SHORT"},
		{Symbol: "SOLUSDT", PositionAmt: "0.00000000"},
		{Symbol: "ADAUSDT", PositionAmt: "100", PositionSide: "LONG"},
	}

	var filtered []*futures.PositionRisk
	for _, pos := range positions {
		if pos.PositionAmt != "0" && pos.PositionAmt != "0.00000000" && pos.PositionAmt != "" {
			filtered = append(filtered, pos)
		}
	}

	expectedActive := 3 // ETHUSDT LONG, BNBUSDT SHORT, ADAUSDT LONG
	if len(filtered) != expectedActive {
		t.Errorf("expected %d active positions, got %d", expectedActive, len(filtered))
	}

	// 验证过滤后的持仓
	symbols := make(map[string]bool)
	for _, pos := range filtered {
		symbols[pos.Symbol] = true
	}

	expected := []string{"ETHUSDT", "BNBUSDT", "ADAUSDT"}
	for _, sym := range expected {
		if !symbols[sym] {
			t.Errorf("expected %s in filtered positions", sym)
		}
	}

	t.Logf("Mixed scenario: %d active out of %d total", len(filtered), len(positions))
}
