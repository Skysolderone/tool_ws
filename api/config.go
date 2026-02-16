package api

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// AuthConfig 认证配置
type AuthConfig struct {
	Token string `json:"token"` // API 访问令牌，为空则不启用认证
}

// Config 应用配置
type Config struct {
	Server    ServerConfig    `json:"server"`
	REST      RESTConfig      `json:"rest"`
	WebSocket WebSocketConfig `json:"websocket"`
	Database  DatabaseConfig  `json:"database"`
	Auth      AuthConfig      `json:"auth"`
	Risk      RiskConfig      `json:"risk"`
	Testnet   bool            `json:"testnet"`
}

// ServerConfig HTTP 服务器配置
type ServerConfig struct {
	Host   string `json:"host"`   // 监听地址，如 "0.0.0.0"
	Port   int    `json:"port"`   // 监听端口，如 10088
	WsPort int    `json:"wsPort"` // WebSocket 价格转发端口，如 10089
}

// RESTConfig REST API 配置（HMAC SHA256 密钥）
type RESTConfig struct {
	APIKey    string `json:"api_key"`
	SecretKey string `json:"secret_key"`
}

// WebSocketConfig WebSocket API 配置（Ed25519 密钥）
type WebSocketConfig struct {
	APIKey         string `json:"api_key"`
	PrivateKeyPath string `json:"private_key_path"` // Ed25519 私钥文件路径（PEM 格式）
}

// DatabaseConfig 数据库配置
type DatabaseConfig struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	User     string `json:"user"`
	Password string `json:"password"`
	DBName   string `json:"dbname"`
	SSLMode  string `json:"sslmode"`
	TimeZone string `json:"timezone"`
}

// DSN 生成 PostgreSQL 连接字符串
func (d DatabaseConfig) DSN() string {
	sslmode := d.SSLMode
	if sslmode == "" {
		sslmode = "disable"
	}
	tz := d.TimeZone
	if tz == "" {
		tz = "UTC"
	}
	return fmt.Sprintf("host=%s user=%s password=%s dbname=%s port=%d sslmode=%s TimeZone=%s",
		d.Host, d.User, d.Password, d.DBName, d.Port, sslmode, tz)
}

// Cfg 全局配置实例
var Cfg Config

// LoadConfig 从 JSON 文件加载配置
// configPath: 配置文件路径，如 "config.json"
func LoadConfig(configPath string) error {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("read config file %s: %w", configPath, err)
	}

	if err := json.Unmarshal(data, &Cfg); err != nil {
		return fmt.Errorf("parse config file: %w", err)
	}

	// 验证必填字段
	if Cfg.REST.APIKey == "" || Cfg.REST.SecretKey == "" {
		return fmt.Errorf("rest.api_key and rest.secret_key are required in config")
	}

	return nil
}

// GetWsPrivateKey 读取 Ed25519 私钥文件内容
// 返回 PEM 格式字符串，如果未配置则返回空字符串
func GetWsPrivateKey(configDir string) string {
	if Cfg.WebSocket.PrivateKeyPath == "" {
		return ""
	}

	keyPath := Cfg.WebSocket.PrivateKeyPath
	// 如果是相对路径，基于配置文件所在目录解析
	if !filepath.IsAbs(keyPath) {
		keyPath = filepath.Join(configDir, keyPath)
	}

	data, err := os.ReadFile(keyPath)
	if err != nil {
		return ""
	}

	return strings.TrimSpace(string(data))
}
