package main

import (
	"bytes"
	"context"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/hlog"
)

// 反向代理服务器 - 直接转发所有请求到币安 Futures API
// 支持正式网和测试网切换

func main() {
	// 从环境变量读取目标地址，默认使用正式网
	binanceURL := os.Getenv("BINANCE_API_URL")
	if binanceURL == "" {
		// 检查是否使用测试网
		if os.Getenv("BINANCE_TESTNET") == "true" {
			binanceURL = "https://testnet.binancefuture.com"
			hlog.Info("Using Binance TESTNET")
		} else {
			binanceURL = "https://fapi.binance.com"
			hlog.Info("Using Binance PRODUCTION")
		}
	}

	// 创建 Hertz 服务器
	h := server.Default(
		server.WithHostPorts(":10087"),
		server.WithMaxRequestBodySize(10*1024*1024), // 10MB
	)

	// 注册反向代理路由
	registerProxyRoutes(h, binanceURL)

	// 优雅退出
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		hlog.Infof("Proxy server running on :10087")
		hlog.Infof("Forwarding all requests to: %s", binanceURL)
		if err := h.Run(); err != nil {
			log.Fatalf("Server failed: %v", err)
		}
	}()

	<-quit
	hlog.Info("Shutting down proxy server...")
	hlog.Info("Proxy server stopped")
}

func registerProxyRoutes(h *server.Hertz, targetURL string) {
	// 健康检查
	h.GET("/health", func(c context.Context, ctx *app.RequestContext) {
		ctx.JSON(200, map[string]string{
			"status": "ok",
			"proxy":  targetURL,
		})
	})

	// 捕获所有请求并转发到币安
	h.Any("/*path", func(c context.Context, ctx *app.RequestContext) {
		proxyRequest(ctx, targetURL)
	})

	hlog.Info("Reverse proxy configured:")
	hlog.Info("  GET  /health     - 健康检查")
	hlog.Info("  ANY  /*          - 转发所有请求到币安")
}

// proxyRequest 将请求转发到目标 URL
func proxyRequest(ctx *app.RequestContext, targetURL string) {
	// 构建完整的目标 URL
	path := string(ctx.Path())
	query := string(ctx.URI().QueryString())
	fullURL := targetURL + path
	if query != "" {
		fullURL += "?" + query
	}

	// 读取请求体
	body := ctx.Request.Body()
	var bodyReader io.Reader
	if len(body) > 0 {
		bodyReader = bytes.NewReader(body)
	}

	// 创建新的 HTTP 请求
	req, err := http.NewRequest(string(ctx.Method()), fullURL, bodyReader)
	if err != nil {
		hlog.Errorf("Failed to create request: %v", err)
		ctx.JSON(500, map[string]string{"error": err.Error()})
		return
	}

	// 复制所有请求头
	ctx.Request.Header.VisitAll(func(key, value []byte) {
		keyStr := string(key)
		// 跳过某些不需要转发的头
		if keyStr != "Host" && keyStr != "Connection" {
			req.Header.Set(keyStr, string(value))
		}
	})

	// 发送请求
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		hlog.Errorf("Failed to forward request: %v", err)
		ctx.JSON(502, map[string]string{"error": "Failed to connect to Binance"})
		return
	}
	defer resp.Body.Close()

	// 复制响应头
	for key, values := range resp.Header {
		for _, value := range values {
			ctx.Response.Header.Set(key, value)
		}
	}

	// 设置状态码
	ctx.SetStatusCode(resp.StatusCode)

	// 复制响应体
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		hlog.Errorf("Failed to read response body: %v", err)
		ctx.JSON(502, map[string]string{"error": "Failed to read response"})
		return
	}

	ctx.Write(respBody)

	hlog.Infof("%s %s -> %d (%d bytes)", ctx.Method(), fullURL, resp.StatusCode, len(respBody))
}
