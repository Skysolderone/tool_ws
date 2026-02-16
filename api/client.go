package api

import (
	"log"
	"path/filepath"
	"sync"

	"github.com/adshao/go-binance/v2/futures"
	ws "tools/websocket"
)

var Client *futures.Client

// WsOrderClient 全局 WebSocket 订单客户端，优先使用 WS 下单
var WsOrderClient *ws.WsClient
var wsClientMu sync.Mutex

// configDir 配置文件所在目录，用于解析相对路径
var configDir string

// InitClient 根据 Cfg 初始化 REST API 客户端
// 调用前必须先调用 LoadConfig
func InitClient(cfgPath string) {
	configDir = filepath.Dir(cfgPath)

	if Cfg.Testnet {
		futures.UseTestnet = true
	}

	Client = futures.NewClient(Cfg.REST.APIKey, Cfg.REST.SecretKey)
}

// InitWsClient 初始化 WebSocket 订单客户端（Ed25519 签名）
// 币安合约 WebSocket API (ws-fapi) 的 session.logon 仅支持 Ed25519 密钥
func InitWsClient() {
	wsClientMu.Lock()
	defer wsClientMu.Unlock()

	wsApiKey := Cfg.WebSocket.APIKey
	wsPrivKeyPEM := GetWsPrivateKey(configDir)

	if wsApiKey == "" || wsPrivKeyPEM == "" {
		log.Println("[WsOrder] WebSocket Ed25519 key not configured, using REST API only")
		return
	}

	client, err := ws.NewWsClientEd25519(wsApiKey, wsPrivKeyPEM, Cfg.Testnet)
	if err != nil {
		log.Printf("[WsOrder] Failed to create Ed25519 WebSocket client: %v, will use REST API fallback", err)
		return
	}
	if err := client.ConnectAndLogon(); err != nil {
		log.Printf("[WsOrder] WebSocket client init failed: %v, will use REST API fallback", err)
		return
	}
	WsOrderClient = client
	log.Println("[WsOrder] WebSocket client (Ed25519) connected and authenticated")
}

// GetWsClient 获取可用的 WebSocket 客户端
func GetWsClient() *ws.WsClient {
	wsClientMu.Lock()
	defer wsClientMu.Unlock()
	return WsOrderClient
}

// ReconnectWsClient 重连 WebSocket 客户端
func ReconnectWsClient() {
	wsClientMu.Lock()
	defer wsClientMu.Unlock()

	// 关闭旧连接
	if WsOrderClient != nil {
		WsOrderClient.Close()
		WsOrderClient = nil
	}

	wsApiKey := Cfg.WebSocket.APIKey
	wsPrivKeyPEM := GetWsPrivateKey(configDir)

	if wsApiKey == "" || wsPrivKeyPEM == "" {
		log.Println("[WsOrder] Ed25519 keys not configured, skip reconnect")
		return
	}

	client, err := ws.NewWsClientEd25519(wsApiKey, wsPrivKeyPEM, Cfg.Testnet)
	if err != nil {
		log.Printf("[WsOrder] WebSocket reconnect create client failed: %v", err)
		return
	}
	if err := client.ConnectAndLogon(); err != nil {
		log.Printf("[WsOrder] WebSocket reconnect failed: %v", err)
		return
	}
	WsOrderClient = client
	log.Println("[WsOrder] WebSocket client reconnected successfully")
}
