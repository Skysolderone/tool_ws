package api

import (
	"math"
	"testing"
)

// --- 测试 USDT 金额到代币数量的计算逻辑 ---

func TestCalculateQuantity_Basic(t *testing.T) {
	// 测试基础计算逻辑
	// 示例：5 USDT 保证金，10x 杠杆，价格 43000
	// 代币数量 = (5 × 10) / 43000 = 0.00116279...

	usdtAmount := 5.0
	leverage := 10.0
	price := 43000.0

	notionalValue := usdtAmount * leverage // 50 USDT 总持仓价值
	quantity := notionalValue / price       // 0.00116279... BTC

	expected := 0.00116279
	if math.Abs(quantity-expected) > 0.00001 {
		t.Errorf("expected quantity ~%.8f, got %.8f", expected, quantity)
	}

	t.Logf("USDT: %.2f, Leverage: %.0fx, Price: %.2f => Quantity: %.8f",
		usdtAmount, leverage, price, quantity)
}

func TestCalculateQuantity_DifferentLeverages(t *testing.T) {
	tests := []struct {
		name      string
		usdt      float64
		leverage  float64
		price     float64
		wantRatio float64 // 预期与 1x 的比例关系
	}{
		{
			name:      "1x leverage",
			usdt:      10,
			leverage:  1,
			price:     50000,
			wantRatio: 1,
		},
		{
			name:      "5x leverage",
			usdt:      10,
			leverage:  5,
			price:     50000,
			wantRatio: 5,
		},
		{
			name:      "10x leverage",
			usdt:      10,
			leverage:  10,
			price:     50000,
			wantRatio: 10,
		},
		{
			name:      "125x leverage",
			usdt:      10,
			leverage:  125,
			price:     50000,
			wantRatio: 125,
		},
	}

	baseQuantity := (10.0 * 1.0) / 50000.0 // 1x 基准

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			quantity := (tt.usdt * tt.leverage) / tt.price
			expectedQuantity := baseQuantity * tt.wantRatio

			if math.Abs(quantity-expectedQuantity) > 0.000001 {
				t.Errorf("expected %.8f, got %.8f", expectedQuantity, quantity)
			}

			t.Logf("%s: Quantity = %.8f (%.0fx base)", tt.name, quantity, tt.wantRatio)
		})
	}
}

func TestRoundToStepSize(t *testing.T) {
	tests := []struct {
		name     string
		quantity float64
		stepSize float64
		expected float64
	}{
		{
			name:     "整数步长 - 向下取整",
			quantity: 10.7,
			stepSize: 1.0,
			expected: 10.0,
		},
		{
			name:     "小数步长 0.001",
			quantity: 0.123456,
			stepSize: 0.001,
			expected: 0.123,
		},
		{
			name:     "小数步长 0.0001",
			quantity: 0.00116279,
			stepSize: 0.0001,
			expected: 0.0011,
		},
		{
			name:     "小数步长 0.00001",
			quantity: 0.00116279,
			stepSize: 0.00001,
			expected: 0.00116,
		},
		{
			name:     "步长为 0 - 不调整",
			quantity: 0.123456,
			stepSize: 0,
			expected: 0.123456,
		},
		{
			name:     "已经符合步长",
			quantity: 0.123,
			stepSize: 0.001,
			expected: 0.123,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := roundToStepSize(tt.quantity, tt.stepSize)
			if math.Abs(result-tt.expected) > 0.0000001 {
				t.Errorf("roundToStepSize(%.8f, %.5f) = %.8f, want %.8f",
					tt.quantity, tt.stepSize, result, tt.expected)
			}
			t.Logf("%.8f with stepSize %.5f => %.8f", tt.quantity, tt.stepSize, result)
		})
	}
}

func TestFormatQuantity(t *testing.T) {
	tests := []struct {
		name      string
		quantity  float64
		precision int
		expected  string
	}{
		{
			name:      "去除尾部零",
			quantity:  0.001000,
			precision: 6,
			expected:  "0.001",
		},
		{
			name:      "保留有效数字",
			quantity:  0.00116,
			precision: 5,
			expected:  "0.00116",
		},
		{
			name:      "整数",
			quantity:  10.0,
			precision: 3,
			expected:  "10",
		},
		{
			name:      "多位小数",
			quantity:  0.123456,
			precision: 6,
			expected:  "0.123456",
		},
		{
			name:      "零",
			quantity:  0.0,
			precision: 3,
			expected:  "0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatQuantity(tt.quantity, tt.precision)
			if result != tt.expected {
				t.Errorf("formatQuantity(%.8f, %d) = %q, want %q",
					tt.quantity, tt.precision, result, tt.expected)
			}
			t.Logf("%.8f (precision %d) => %q", tt.quantity, tt.precision, result)
		})
	}
}

func TestCalculateQuantity_RealScenarios(t *testing.T) {
	// 真实场景测试
	scenarios := []struct {
		name      string
		usdt      float64
		leverage  float64
		price     float64
		stepSize  float64
		precision int
		wantApprox float64
	}{
		{
			name:       "BTC: 5 USDT, 10x, 43000",
			usdt:       5,
			leverage:   10,
			price:      43000,
			stepSize:   0.001,
			precision:  3,
			wantApprox: 0.001, // (5*10)/43000 = 0.00116 -> 向下到 0.001
		},
		{
			name:       "ETH: 10 USDT, 5x, 2300",
			usdt:       10,
			leverage:   5,
			price:      2300,
			stepSize:   0.001,
			precision:  3,
			wantApprox: 0.021, // (10*5)/2300 = 0.0217 -> 向下到 0.021
		},
		{
			name:       "BNB: 20 USDT, 3x, 310",
			usdt:       20,
			leverage:   3,
			price:      310,
			stepSize:   0.1,
			precision:  1,
			wantApprox: 0.1, // (20*3)/310 = 0.193 -> 向下到 0.1
		},
	}

	for _, sc := range scenarios {
		t.Run(sc.name, func(t *testing.T) {
			// 计算原始数量
			quantity := (sc.usdt * sc.leverage) / sc.price

			// 应用 stepSize
			rounded := roundToStepSize(quantity, sc.stepSize)

			// 格式化
			formatted := formatQuantity(rounded, sc.precision)

			t.Logf("Original: %.8f, Rounded: %.8f, Formatted: %s",
				quantity, rounded, formatted)

			// 验证结果接近预期
			if math.Abs(rounded-sc.wantApprox) > sc.stepSize {
				t.Errorf("expected quantity ~%.8f, got %.8f", sc.wantApprox, rounded)
			}
		})
	}
}

func TestCalculateQuantity_EdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		usdt     float64
		leverage float64
		price    float64
		wantErr  bool
	}{
		{
			name:     "最小金额",
			usdt:     0.01,
			leverage: 1,
			price:    50000,
			wantErr:  false,
		},
		{
			name:     "高杠杆",
			usdt:     1,
			leverage: 125,
			price:    50000,
			wantErr:  false,
		},
		{
			name:     "零金额",
			usdt:     0,
			leverage: 10,
			price:    50000,
			wantErr:  false, // 可以下 0 数量订单（虽然会被交易所拒绝）
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			quantity := (tt.usdt * tt.leverage) / tt.price
			t.Logf("USDT: %.4f, Leverage: %.0f, Price: %.2f => Quantity: %.8f",
				tt.usdt, tt.leverage, tt.price, quantity)

			if quantity < 0 {
				t.Error("quantity should not be negative")
			}
		})
	}
}

func TestPlaceOrderReq_WithQuoteQuantity(t *testing.T) {
	// 测试新增的 QuoteQuantity 和 Leverage 字段
	req := PlaceOrderReq{
		Symbol:        "BTCUSDT",
		Side:          "BUY",
		OrderType:     "MARKET",
		QuoteQuantity: "5",      // 5 USDT 保证金
		Leverage:      10,       // 10x 杠杆
	}

	if req.QuoteQuantity != "5" {
		t.Errorf("expected quoteQuantity 5, got %s", req.QuoteQuantity)
	}
	if req.Leverage != 10 {
		t.Errorf("expected leverage 10, got %d", req.Leverage)
	}

	t.Logf("Order request: %+v", req)
}

func TestNotionalValue_Calculation(t *testing.T) {
	// 验证名义价值（总持仓价值）的计算
	tests := []struct {
		name            string
		margin          float64
		leverage        float64
		expectedNotional float64
	}{
		{
			name:            "5 USDT × 10x",
			margin:          5,
			leverage:        10,
			expectedNotional: 50,
		},
		{
			name:            "10 USDT × 5x",
			margin:          10,
			leverage:        5,
			expectedNotional: 50,
		},
		{
			name:            "100 USDT × 1x",
			margin:          100,
			leverage:        1,
			expectedNotional: 100,
		},
		{
			name:            "1 USDT × 125x",
			margin:          1,
			leverage:        125,
			expectedNotional: 125,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			notional := tt.margin * tt.leverage
			if notional != tt.expectedNotional {
				t.Errorf("expected notional %.2f, got %.2f", tt.expectedNotional, notional)
			}
			t.Logf("%.2f USDT × %.0fx = %.2f USDT notional value",
				tt.margin, tt.leverage, notional)
		})
	}
}
