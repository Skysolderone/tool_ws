package main

import (
	"flag"
	"fmt"
	"log"

	"tools/api"

	"github.com/cloudwego/hertz/pkg/app/server"
)

func main() {
	cfgPath := flag.String("config", "config.json", "配置文件路径")
	flag.Parse()

	// 加载配置文件
	if err := api.LoadConfig(*cfgPath); err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	api.InitClient(*cfgPath)

	// 初始化数据库
	if err := api.InitDB(); err != nil {
		log.Fatalf("Failed to init database: %v", err)
	}

	// 初始化风控
	api.InitRiskControl(api.Cfg.Risk)

	// 初始化 WebSocket 订单客户端（异步，不阻塞启动）
	go api.InitWsClient()

	// 启动 User Data Stream（自动更新交易记录盈亏 + 风控联动）
	api.StartUserStream()

	// 启动 WebSocket 价格转发服务
	wsPort := api.Cfg.Server.WsPort
	if wsPort == 0 {
		wsPort = 10089
	}
	go api.StartWsPriceServer(wsPort)

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

	h := server.New(server.WithHostPorts(addr))

	apiGroup := h.Group("/tool")
	// Token 认证中间件
	apiGroup.Use(api.AuthMiddleware())
	{
		apiGroup.GET("/balance", api.HandleGetBalance)
		apiGroup.GET("/positions", api.HandleGetPositions)
		apiGroup.POST("/order", api.HandlePlaceOrder)
		apiGroup.GET("/orders", api.HandleGetOrders)
		apiGroup.DELETE("/order", api.HandleCancelOrder)
		apiGroup.POST("/leverage", api.HandleChangeLeverage)
		apiGroup.POST("/reduce", api.HandleReducePosition)
		apiGroup.POST("/close", api.HandleClosePosition)

		// 交易记录
		apiGroup.GET("/trades", api.HandleGetTrades)

		// 浮盈加仓
		apiGroup.POST("/autoscale/start", api.HandleStartAutoScale)
		apiGroup.POST("/autoscale/stop", api.HandleStopAutoScale)
		apiGroup.GET("/autoscale/status", api.HandleAutoScaleStatus)

		// 风控
		apiGroup.GET("/risk/status", api.HandleGetRiskStatus)
		apiGroup.POST("/risk/unlock", api.HandleUnlockRisk)

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
	}

	h.Spin()
}
