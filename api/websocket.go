package api

import (
	"context"
	"time"

	"github.com/adshao/go-binance/v2/futures"
)

// WsTokenPrice 订阅代币实时标记价格
func WsTokenPrice(symbol string, handler func(*futures.WsMarkPriceEvent), errHandler func(error)) (doneC, stopC chan struct{}, err error) {
	return futures.WsMarkPriceServe(symbol, handler, errHandler)
}

// WsUserData 订阅账户变动信息（仓位变化、订单更新、余额变动）
func WsUserData(ctx context.Context, handler func(*futures.WsUserDataEvent), errHandler func(error)) (doneC, stopC chan struct{}, err error) {
	listenKey, err := Client.NewStartUserStreamService().Do(ctx)
	if err != nil {
		return nil, nil, err
	}

	doneC, stopC, err = futures.WsUserDataServe(listenKey, handler, errHandler)
	if err != nil {
		return nil, nil, err
	}

	// 每 30 分钟续期 listenKey
	go func() {
		ticker := time.NewTicker(30 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				_ = Client.NewKeepaliveUserStreamService().ListenKey(listenKey).Do(ctx)
			case <-stopC:
				return
			}
		}
	}()

	return doneC, stopC, nil
}
