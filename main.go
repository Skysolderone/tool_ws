package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"tools/agent"
	"tools/api"

	"github.com/cloudwego/hertz/pkg/app/server"
)

func main() {
	cfgPath := flag.String("config", "config.json", "配置文件路径")
	migrateOnly := flag.Bool("migrate-only", false, "仅执行数据库迁移后退出")
	flag.Parse()

	// 加载配置文件
	if err := api.LoadConfig(*cfgPath); err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}
	api.InitNewsSourcesFromConfig(api.Cfg.News)

	api.InitClient(*cfgPath)

	// 初始化数据库
	if err := api.InitDB(); err != nil {
		log.Fatalf("Failed to init database: %v", err)
	}
	if api.Cfg.Database.AutoMigrate || *migrateOnly {
		if err := api.RunMigrations(); err != nil {
			log.Fatalf("Failed to run migrations: %v", err)
		}
		log.Printf("[DB] Migrations completed")
	}
	if *migrateOnly {
		log.Printf("[DB] migrate-only finished, exiting")
		return
	}

	// 初始化模拟交易引擎
	api.InitPaperEngine(api.Cfg.DryRun)

	// 初始化风控
	api.InitRiskControl(api.Cfg.Risk)

	// 初始化 Telegram 通知
	api.InitNotify(api.Cfg.Notify)

	// 初始化组合风控
	api.InitPortfolioRisk(api.Cfg.PortfolioRisk)

	// 启动异常波动守卫（如果配置启用）
	api.StartVolatilityGuard(api.Cfg.VolatilityGuard)

	// 初始化 Kill-Switch、数据质量监控、运营指标、VaR 风控、数据降级
	api.InitKillSwitch()
	api.InitDataQualityTracker()
	api.InitOpsMetrics()
	api.InitVarRisk(api.Cfg.VarRisk)
	api.InitDataFallback(api.FallbackConfig{})
	api.StartRegimeDetector("BTCUSDT", 60)
	api.StartAgentEvaluator(30)

	// 初始化 WebSocket 订单客户端（异步，不阻塞启动）
	go api.InitWsClient()

	// 启动 User Data Stream（自动更新交易记录盈亏 + 风控联动）
	api.StartUserStream()

	// 启动推荐交易预计算引擎（后台多时间框架定时刷新）
	api.StartRecommendEngine()
	// 启动常驻资讯抓取（不依赖 ws/news 客户端连接）
	api.StartNewsBackgroundFetcher()
	// 每小时检测资讯源可用性
	api.StartNewsSourceHealthMonitor()

	// 初始化 LLM 分析 Agent（可选配置，失败不影响主流程）
	if err := agent.InitAgent(api.Cfg.LLM); err != nil {
		log.Printf("[Agent] Init failed: %v (agent disabled)", err)
	}

	// 启动本地止盈止损监控器（从DB恢复ACTIVE条件）
	api.StartLocalTPSLMonitor()

	// 恢复持久化的策略
	api.RecoverStrategies()

	// 启动 WebSocket 价格转发服务
	wsPort := api.Cfg.Server.WsPort
	if wsPort == 0 {
		wsPort = 10089
	}
	wsServer := api.StartWsPriceServer(wsPort)

	// 从配置读取监听地址
	host := api.Cfg.Server.Host
	port := api.Cfg.Server.Port
	if host == "" {
		host = "0.0.0.0"
	}
	if port == 0 {
		port = 10088
	}
	addr := fmt.Sprintf("%s:%d", host, port)
	log.Printf("[Server] Listening on %s", addr)

	h := server.New(
		server.WithHostPorts(addr),
		server.WithReadTimeout(15*time.Second),
		// Agent 分析接口可能耗时接近 55s；写超时过短会被网关放大成 504。
		server.WithWriteTimeout(90*time.Second),
		server.WithIdleTimeout(60*time.Second),
		server.WithKeepAliveTimeout(60*time.Second),
		server.WithExitWaitTime(20*time.Second),
	)

	apiGroup := h.Group("/tool")
	// Token 认证中间件
	apiGroup.Use(api.AuthMiddleware())
	{
		apiGroup.GET("/balance", api.HandleGetBalance)
		apiGroup.GET("/positions", api.HandleGetPositions)
		apiGroup.POST("/order", api.HandlePlaceOrder)
		apiGroup.GET("/orders", api.HandleGetOrders)
		apiGroup.GET("/orderbook", api.HandleGetOrderBook)
		apiGroup.GET("/orderbook/whale", api.HandleGetOrderBookWhale)
		apiGroup.DELETE("/order", api.HandleCancelOrder)
		apiGroup.POST("/leverage", api.HandleChangeLeverage)
		apiGroup.POST("/reduce", api.HandleReducePosition)
		apiGroup.POST("/close", api.HandleClosePosition)
		apiGroup.POST("/reverse", api.HandleReversePosition)

		// 交易记录
		apiGroup.GET("/trades", api.HandleGetTrades)
		apiGroup.GET("/operations", api.HandleGetOperations)
		apiGroup.GET("/liquidation/history", api.HandleGetLiquidationHistory)
		apiGroup.GET("/analytics/journal", api.HandleGetAnalyticsJournal)
		apiGroup.GET("/analytics/attribution", api.HandleGetAnalyticsAttribution)
		apiGroup.GET("/analytics/sentiment", api.HandleGetAnalyticsSentiment)
		apiGroup.POST("/hyper/follow/start", api.HandleStartHyperFollow)
		apiGroup.POST("/hyper/follow/stop", api.HandleStopHyperFollow)
		apiGroup.GET("/hyper/follow/status", api.HandleHyperFollowStatus)
		apiGroup.GET("/hyper/positions", api.HandleGetHyperPositions)

		// 浮盈加仓
		apiGroup.POST("/autoscale/start", api.HandleStartAutoScale)
		apiGroup.POST("/autoscale/stop", api.HandleStopAutoScale)
		apiGroup.GET("/autoscale/status", api.HandleAutoScaleStatus)

		// 风控
		apiGroup.GET("/risk/status", api.HandleGetRiskStatus)
		apiGroup.POST("/risk/unlock", api.HandleUnlockRisk)
		apiGroup.GET("/risk/portfolio", api.HandleGetPortfolioStatus)
		apiGroup.GET("/risk/volatility", api.HandleGetVolatilityGuardStatus)

		// 网格交易
		apiGroup.POST("/grid/start", api.HandleStartGrid)
		apiGroup.POST("/grid/stop", api.HandleStopGrid)
		apiGroup.GET("/grid/status", api.HandleGridStatus)

		// DCA 定投
		apiGroup.POST("/dca/start", api.HandleStartDCA)
		apiGroup.POST("/dca/stop", api.HandleStopDCA)
		apiGroup.GET("/dca/status", api.HandleDCAStatus)

		// RSI+成交量 信号策略
		apiGroup.POST("/signal/start", api.HandleStartSignal)
		apiGroup.POST("/signal/stop", api.HandleStopSignal)
		apiGroup.GET("/signal/status", api.HandleSignalStatus)

		// K线形态（十字星）策略
		apiGroup.POST("/doji/start", api.HandleStartDoji)
		apiGroup.POST("/doji/stop", api.HandleStopDoji)
		apiGroup.GET("/doji/status", api.HandleDojiStatus)

		// 资金费率监控
		apiGroup.POST("/funding/start", api.HandleStartFundingMonitor)
		apiGroup.POST("/funding/stop", api.HandleStopFundingMonitor)
		apiGroup.GET("/funding/status", api.HandleFundingStatus)

		// 多策略联动
		apiGroup.POST("/link/start", api.HandleStartStrategyLink)
		apiGroup.POST("/link/stop", api.HandleStopStrategyLink)
		apiGroup.GET("/link/status", api.HandleStrategyLinkStatus)
		apiGroup.POST("/link/rules", api.HandleUpdateStrategyLinkRules)

		// 支撑/阻力位
		apiGroup.GET("/sr/levels", api.HandleGetSRLevels)

		// 推荐交易扫描
		apiGroup.GET("/recommend/scan", api.HandleRecommendScan)
		apiGroup.GET("/recommend/analyze", api.HandleRecommendAnalyze)
		apiGroup.POST("/agent/analyze", agent.HandleAnalyze)
		apiGroup.GET("/agent/analyze", agent.HandleAnalyze)
		apiGroup.POST("/agent/execute", agent.HandleExecute)
		apiGroup.GET("/agent/log", agent.HandleLog)
		apiGroup.GET("/agent/logs", agent.HandleLogs)
		apiGroup.GET("/agent/policy", agent.HandlePolicy)

		// 本地止盈止损
		apiGroup.GET("/tpsl/list", api.HandleGetTPSLList)
		apiGroup.POST("/tpsl/cancel", api.HandleCancelTPSL)
		apiGroup.GET("/tpsl/history", api.HandleGetTPSLHistory)
		apiGroup.POST("/tpsl/trailing", api.HandleSetTrailingStop)

		// 1分钟 Scalp 策略
		apiGroup.POST("/scalp/start", api.HandleStartScalp)
		apiGroup.POST("/scalp/stop", api.HandleStopScalp)
		apiGroup.GET("/scalp/status", api.HandleScalpStatus)

		// 模拟交易（Paper Trading / DryRun）
		apiGroup.GET("/paper/status", api.HandleGetPaperStatus)
		apiGroup.POST("/paper/reset", api.HandleResetPaper)

		// 回测系统
		apiGroup.POST("/backtest/run", api.HandleRunBacktest)

		// 订单流分析
		apiGroup.GET("/orderflow", api.HandleGetOrderFlow)

		// 滑点统计
		apiGroup.GET("/slippage/stats", api.HandleGetSlippageStats)

		// 新闻情绪事件驱动策略
		apiGroup.POST("/news-sentiment/start", api.HandleStartNewsSentiment)
		apiGroup.POST("/news-sentiment/stop", api.HandleStopNewsSentiment)
		apiGroup.GET("/news-sentiment/status", api.HandleNewsSentimentStatus)
		apiGroup.GET("/news/sources/status", api.HandleGetNewsSourceStatus)

		// 爆仓级联交易策略
		apiGroup.POST("/liq-cascade/start", api.HandleStartLiqCascade)
		apiGroup.POST("/liq-cascade/stop", api.HandleStopLiqCascade)
		apiGroup.GET("/liq-cascade/status", api.HandleLiqCascadeStatus)

		// 资金费率极端套利策略
		apiGroup.POST("/funding-arb/start", api.HandleStartFundingArb)
		apiGroup.POST("/funding-arb/stop", api.HandleStopFundingArb)
		apiGroup.GET("/funding-arb/status", api.HandleFundingArbStatus)

		// Analytics 相关
		apiGroup.GET("/analytics/correlation", api.HandleGetAnalyticsCorrelation)

		// 执行质量
		apiGroup.GET("/execution/quality", api.HandleGetExecutionQuality)

		// Kill-Switch
		apiGroup.POST("/risk/kill-switch", api.HandleKillSwitch)
		apiGroup.GET("/risk/kill-switch", api.HandleGetKillSwitchStatus)

		// 数据质量
		apiGroup.GET("/data-quality", api.HandleGetDataQuality)

		// 运营指标
		apiGroup.GET("/ops/metrics", api.HandleGetOpsMetrics)

		// VaR 风控
		apiGroup.GET("/risk/var", api.HandleGetVarStatus)

		// 冲击测试
		apiGroup.POST("/risk/stress-test", api.HandleRunStressTest)

		// 数据降级
		apiGroup.GET("/data-fallback/status", api.HandleGetFallbackStatus)

		// 资金分配器
		apiGroup.POST("/allocator/start", api.HandleStartAllocator)
		apiGroup.POST("/allocator/stop", api.HandleStopAllocator)
		apiGroup.GET("/allocator/status", api.HandleGetAllocation)

		// 市场状态
		apiGroup.GET("/regime/status", api.HandleGetRegime)

		// 参数稳定性
		apiGroup.GET("/param-stability/status", api.HandleGetParamStability)

		// 自适应下单量
		apiGroup.GET("/adaptive-size", api.HandleGetAdaptiveSize)

		// Agent 建议评估
		apiGroup.GET("/agent/evaluation", api.HandleGetAgentEval)

		// Agent 风控预检
		apiGroup.POST("/agent/risk-check", api.HandleAgentRiskCheck)

		// 策略管理
		apiGroup.POST("/strategy/admin", api.HandleStrategyAdmin)
	}

	hErrCh := make(chan error, 1)
	go func() {
		hErrCh <- h.Run()
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	defer signal.Stop(sigCh)

	select {
	case sig := <-sigCh:
		log.Printf("[Server] Received signal: %s, shutting down...", sig.String())
	case err := <-hErrCh:
		if err != nil {
			log.Printf("[Server] Hertz stopped with error: %v", err)
		} else {
			log.Printf("[Server] Hertz stopped")
		}
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	if wsServer != nil {
		if err := wsServer.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("[WsProxy] Shutdown error: %v", err)
		}
	}

	if err := h.Shutdown(shutdownCtx); err != nil {
		log.Printf("[Server] Shutdown error: %v", err)
	}

	log.Printf("[Server] Shutdown complete")
}
