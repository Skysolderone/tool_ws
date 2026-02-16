# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## 项目概述

基于 Go 的币安（Binance）合约交易工具，使用 Hertz 作为 HTTP 框架，通过 go-binance SDK 与币安 Futures API 交互。

## 技术栈

- **Go 1.26**
- **HTTP 框架**: Hertz (cloudwego/hertz)
- **交易 SDK**: go-binance/v2 (futures 模块)

## 构建与运行

```bash
make run          # go run main.go
go build .        # 编译
go test ./...     # 运行所有测试
go test ./api/... # 仅测试 api 包
```

需要设置环境变量：
```bash
export BINANCE_API_KEY=xxx
export BINANCE_SECRET_KEY=xxx
export BINANCE_TESTNET=true  # 可选，启用测试网
```

## 架构

两层结构：`main.go` 注册路由 → `api/` 包处理业务逻辑。

- `main.go` — 入口，调用 `api.InitClient()` 初始化客户端，注册 Hertz 路由
- `api/client.go` — 全局 `*futures.Client` 初始化（从环境变量读取密钥）
- `api/handler.go` — Hertz HTTP handler 层，参数解析与 JSON 响应
- `api/order.go` — 下单（`PlaceOrder`）、订单查询（`GetOrderList`）、撤单（`CancelOrder`）、杠杆调整（`ChangeLeverage`）；定义了 `PlaceOrderReq` 请求结构体
- `api/position.go` — 仓位查询（`GetPositions`）
- `api/websocket.go` — 实时标记价格（`WsTokenPrice`）、账户数据流（`WsUserData`，自动续期 listenKey）

## HTTP 接口

| 方法 | 路径 | 功能 |
|------|------|------|
| GET | `/api/positions?symbol=` | 查询仓位 |
| POST | `/api/order` | 下单 (JSON body: PlaceOrderReq) |
| GET | `/api/orders?symbol=` | 查询未成交订单 |
| DELETE | `/api/order?symbol=&orderId=` | 撤单 |
| POST | `/api/leverage` | 调整杠杆 (JSON body: symbol, leverage) |

## 关键约定

- `api.Client` 是包级全局变量，所有 API 函数依赖它，`main.go` 启动时必须先调用 `api.InitClient()`
- Handler 层（`handler.go`）与业务逻辑层（`order.go` / `position.go`）分离，新增接口时两层都需要添加
- go-binance SDK 使用 Service 模式：`Client.NewXxxService().Param(val).Do(ctx)` 链式调用
- WebSocket 函数（`WsTokenPrice`、`WsUserData`）尚未暴露为 HTTP 接口，目前仅供内部调用
