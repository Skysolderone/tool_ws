package api

import (
	"context"
	"strconv"

	"github.com/adshao/go-binance/v2/futures"
)

// GetBalance 获取期货账户 USDT 余额
func GetBalance(ctx context.Context) (map[string]string, error) {
	balances, err := Client.NewGetBalanceService().Do(ctx)
	if err != nil {
		return nil, err
	}
	for _, b := range balances {
		if b.Asset == "USDT" {
			return map[string]string{
				"asset":            b.Asset,
				"balance":          b.Balance,
				"availableBalance": b.AvailableBalance,
				"crossWalletBalance": b.CrossWalletBalance,
				"crossUnPnl":        b.CrossUnPnl,
			}, nil
		}
	}
	return map[string]string{"asset": "USDT", "balance": "0", "availableBalance": "0", "crossWalletBalance": "0", "crossUnPnl": "0"}, nil
}

// GetPositions 获取当前仓位，symbol 为空则获取所有仓位，只返回持仓量不为 0 的仓位
func GetPositions(ctx context.Context) ([]*futures.PositionRisk, error) {
	service := Client.NewGetPositionRiskService()

	positions, err := service.Do(ctx)
	if err != nil {
		return nil, err
	}

	// 过滤出持仓量不为 0 的仓位
	var activePositions []*futures.PositionRisk
	for _, pos := range positions {
		amtFloat, _ := strconv.ParseFloat(pos.PositionAmt, -1)
		if amtFloat != 0 {
			activePositions = append(activePositions, pos)
		}
	}

	return activePositions, nil
}
