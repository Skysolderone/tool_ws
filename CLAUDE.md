# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## 项目概述

币安合约交易工具，前后端分离：Go 后端 + React Native Expo 移动端。

## 技术栈

**后端**：Go 1.26 / Hertz HTTP / go-binance/v2 futures / PostgreSQL (GORM) / gorilla/websocket
**前端**：React Native 0.81 + Expo 54 / react-native-webview

## 构建与测试命令

```bash
# 后端
make run           # 本地运行 go run main.go
make build         # 编译 Linux/amd64 → ./tool
make main          # tidy + build + SSH 部署到 wws 服务器
make migrate       # 仅执行 DB 迁移 (go run main.go -migrate-only)
make build-proxy   # 编译反代服务 → ./proxy
go test ./api/...       # 后端业务测试
go test ./websocket/... # WS 模块测试

# 前端
cd trading-app && npx expo start              # 开发调试
cd trading-app && eas build --platform android --profile preview --local  # 打 APK
```

## 后端架构

双端口：10088 (Hertz REST, `/tool` 前缀, Token 认证) + 10089 (WebSocket 价格/订单簿/新闻/Hyper监控)。

启动顺序：`LoadConfig` → `InitClient` → `InitDB` → `InitRiskControl` → `InitWsClient`(异步) → `StartUserStream` → `StartWsPriceServer`

路由全部注册在 `main.go`，Handler 在 `api/handler.go`，业务逻辑按功能分散到 `api/` 各文件。

### 双重下单通道

代码维护两个客户端（`api/client.go`）：
- `Client *futures.Client`：REST API，HMAC SHA256 签名，主路径
- `WsOrderClient *ws.WsClient`：WebSocket API，Ed25519 签名（`websocket/` 包），优先使用，失败自动降级到 REST

**重要**：自 2025-12-09 币安要求 `STOP_MARKET`/`TAKE_PROFIT_MARKET` 等条件单必须走 `/fapi/v1/algoOrder`，不能用 go-binance `NewCreateOrderService`，相关逻辑在 `api/algo_order.go` 中自行构造 HMAC 签名。

### 后端关键文件

| 文件 | 职责 |
|------|------|
| `api/order.go` | 下单/撤单/平仓/减仓，`PlaceOrderReq` 结构体 |
| `api/algo_order.go` | 条件单（STOP_MARKET 等必须用此，不能用 go-binance NewCreateOrderService） |
| `api/db.go` | GORM 模型 `TradeRecord` / `OperationRecord` / `LiquidationRecord` |
| `api/risk_control.go` | 每日最大亏损限制，锁定后返回 403 |
| `api/user_stream.go` | 监听成交 → 更新盈亏 + 触发风控 |
| `api/ws_price_proxy.go` | WS 服务主文件，`wsClient` 公共结构，注册所有 WS 路由 |
| `api/ws_news_hyper_proxy.go` | 新闻聚合 (BlockBeats + 0xzx RSS) + Hyperliquid 监控代理 |
| `api/hyper_follow.go` | Hyperliquid 跟单策略 |
| `api/auto_scale.go` | 浮盈加仓策略 |
| `api/grid_trading.go` | 网格交易 |
| `api/dca.go` | DCA 定投 |
| `api/signal_strategy.go` | RSI + 成交量信号 |
| `api/doji_strategy.go` | K线十字星形态 |
| `api/liquidation_history.go` | 爆仓历史 |
| `api/ws_liquidation_stats.go` | 爆仓统计 WS |
| `api/strategy_link.go` | 跨策略联动规则（触发源→动作，带冷却期） |
| `websocket/` | 币安 WS-FAPI 客户端封装（Ed25519 签名下单） |
| `cmd/proxy_server/` | 独立币安 API 反代服务（port 10087） |

## 前端架构

React Native Expo 应用，暖色金融风主题（Bloomberg 风格）。5 个主 Tab：交易 / 策略 / 监控 / 资讯 / 我的。

### 设计系统 (`src/services/theme.js`)

核心色：`bg #0c0a08` / `card #1a1613` / `gold #d4a54a`(主强调) / `green #2ebd6e`(多头) / `red #d94452`(空头) / `text #e8ddd0`

统一使用 `colors`、`spacing`、`radius`、`fontSize` 四组 design token，禁止硬编码颜色值。

### 前端文件结构

```
trading-app/
  App.js              # 入口，5-tab 导航（交易/策略/监控/资讯/我的），全局 WS 价格连接
  src/
    services/
      api.js           # 后端 HTTP/WS 请求封装
      theme.js         # 设计 token
    components/
      SubTabBar.js     # 子选项卡组件
      AccountBar.js    # 账户余额栏
      SymbolPicker.js  # 交易对选择器
      OrderPanel.js    # 下单面板
      PositionPanel.js # 持仓面板
      OrderBookPanel.js # 订单簿
      TradeLogPanel.js  # 交易记录
      NewsPanel.js      # 新闻（BlockBeats 纯文本 + 0xzx HTML 本地渲染）
      HyperMonitorPanel.js  # Hyperliquid 监控
      AutoScalePanel.js     # 浮盈加仓策略
      GridPanel.js          # 网格交易策略
      DCAPanel.js           # DCA 定投策略
      SignalPanel.js        # RSI 信号策略
      DojiPanel.js          # 十字星策略
      LiquidationMonitorPanel.js # 爆仓监控
```

### 前端关键约定

- App.js 通过 WS 获取实时价格，以 `externalMarkPrice` prop 传给子组件
- NewsPanel 中 0xzx 文章使用 `WebView source={{ html }}` 本地渲染 RSS 全文，不跳转外部链接
- 所有组件统一从 `theme.js` 导入 design token，金色 `colors.gold` 为主强调色
- 策略面板统一模式：start/stop 按钮 + 参数配置 + 状态展示

## 关键约定（通用）

- `api.Cfg` 全局配置，`api.Client` 全局 futures 客户端
- go-binance SDK：`Client.NewXxxService().Param(val).Do(ctx)` 链式调用
- GORM AutoMigrate 自动建表（不删列）
- 配置通过 `config.json`（`-config` 参数指定路径）
- 所有接口需 Token 认证（Header `X-Auth-Token` / `Authorization: Bearer` 或 WS `?token=`）
- 市场告警规则持久化到 `data/ws-monitor/` 目录下 JSON 文件（原子写：临时文件 + rename）
- 策略联动规则（`strategy_link.go`）：触发源 `rsi_buy/rsi_sell/liq_spike/funding_high` → 动作 `start_grid/close_position/reduce_position` 等，带冷却期防重复
- 价格数据流：币安 aggTrade WS → 后端 priceHub 广播 → App 客户端（App 不直连币安，解决网络问题）
