# CLAUDE.md

## 项目概述

币安合约交易工具，前后端分离：Go 后端 + React Native Expo 移动端。

## 技术栈

**后端**：Go 1.26 / Hertz HTTP / go-binance/v2 futures / PostgreSQL (GORM) / gorilla/websocket
**前端**：React Native 0.81 + Expo 54 / react-native-webview

## 构建命令

```bash
make run           # 本地运行 go run main.go
make build         # 编译 Linux/amd64 → ./tool
make main          # tidy + build + SSH 部署到 wws 服务器
go test ./api/...  # 测试
cd trading-app && npx expo start  # 前端开发
```

## 后端架构

双端口：10088 (Hertz REST, `/tool` 前缀, Token 认证) + 10089 (WebSocket 价格/订单簿/新闻/Hyper监控)。

启动顺序：`LoadConfig` → `InitClient` → `InitDB` → `InitRiskControl` → `InitWsClient`(异步) → `StartUserStream` → `StartWsPriceServer`

路由全部注册在 `main.go`，Handler 在 `api/handler.go`，业务逻辑按功能分散到 `api/` 各文件。

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

## 前端架构

React Native Expo 单页应用，暖色金融风主题（Bloomberg 风格）。

### 设计系统 (`src/services/theme.js`)

核心色：`bg #0c0a08` / `card #1a1613` / `gold #d4a54a`(主强调) / `green #2ebd6e`(多头) / `red #d94452`(空头) / `text #e8ddd0`

统一使用 `colors`、`spacing`、`radius`、`fontSize` 四组 design token，禁止硬编码颜色值。

### 前端文件结构

```
trading-app/
  App.js              # 入口，4-tab 导航（交易/策略/资讯/我的），全局 WS 价格连接
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
- 所有接口需 Token 认证（Header `Authorization` 或 WS `?token=`）
