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

// LLMModel 单个模型配置。
type LLMModel struct {
	Provider    string  `json:"provider"`
	APIKey      string  `json:"api_key"`
	BaseURL     string  `json:"base_url"`
	Model       string  `json:"model"`
	MaxTokens   int     `json:"max_tokens"`
	Temperature float64 `json:"temperature"`
}

// LLMConfig 大模型配置
type LLMConfig struct {
	Provider    string  `json:"provider"`
	APIKey      string  `json:"api_key"`
	BaseURL     string  `json:"base_url"`
	Model       string  `json:"model"`
	MaxTokens   int     `json:"max_tokens"`
	Temperature float64 `json:"temperature"`

	// 多模型路由
	RouterEnabled bool      `json:"router_enabled"` // 是否启用多模型路由
	FastModel     *LLMModel `json:"fast_model"`     // 快速筛选模型（如 deepseek-chat）
	DeepModel     *LLMModel `json:"deep_model"`     // 深度推理模型（如 deepseek-reasoner）
}

// NewsConfig 资讯源配置
type NewsConfig struct {
	RSSHubBaseURL    string   `json:"rsshubBaseUrl"`    // RSSHub 实例地址
	TelegramChannels []string `json:"telegramChannels"` // Telegram 频道用户名或 t.me 链接
}

// RedisConfig Redis 缓存配置（可选）。
type RedisConfig struct {
	Enabled            bool   `json:"enabled"`
	Addr               string `json:"addr"`
	Password           string `json:"password"`
	DB                 int    `json:"db"`
	KeyPrefix          string `json:"keyPrefix"`
	SnapshotTTLSeconds int    `json:"snapshotTTLSeconds"`
}

// Config 应用配置
type Config struct {
	Server          ServerConfig          `json:"server"`
	REST            RESTConfig            `json:"rest"`
	WebSocket       WebSocketConfig       `json:"websocket"`
	Database        DatabaseConfig        `json:"database"`
	Auth            AuthConfig            `json:"auth"`
	LLM             LLMConfig             `json:"llm"`
	News            NewsConfig            `json:"news"`
	Redis           RedisConfig           `json:"redis"`
	Risk            RiskConfig            `json:"risk"`
	PortfolioRisk   PortfolioRiskConfig   `json:"portfolioRisk"`
	Notify          NotifyConfig          `json:"notify"`
	VolatilityGuard VolatilityGuardConfig `json:"volatilityGuard"`
	VarRisk         VarRiskConfig         `json:"varRisk"`
	Testnet         bool                  `json:"testnet"`
	DryRun          bool                  `json:"dryRun"` // 模拟交易模式，不实际下单
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
	// AutoMigrate 是否在启动时自动执行数据库迁移；默认 true
	AutoMigrate bool `json:"autoMigrate"`
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
	// 默认值
	Cfg = Config{
		Database: DatabaseConfig{
			AutoMigrate: true,
		},
		News: NewsConfig{
			RSSHubBaseURL: "https://rsshub.wws741.top",
		},
		Redis: RedisConfig{
			Enabled:            false,
			Addr:               "127.0.0.1:6379",
			DB:                 0,
			KeyPrefix:          "tool:",
			SnapshotTTLSeconds: 86400,
		},
	}

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
	Cfg.News.RSSHubBaseURL = strings.TrimSpace(Cfg.News.RSSHubBaseURL)
	Cfg.News.RSSHubBaseURL = strings.TrimRight(Cfg.News.RSSHubBaseURL, "/")
	if Cfg.News.RSSHubBaseURL == "" {
		Cfg.News.RSSHubBaseURL = "https://rsshub.wws741.top"
	}
	Cfg.Redis.Addr = strings.TrimSpace(Cfg.Redis.Addr)
	Cfg.Redis.KeyPrefix = strings.TrimSpace(Cfg.Redis.KeyPrefix)
	if Cfg.Redis.KeyPrefix == "" {
		Cfg.Redis.KeyPrefix = "tool:"
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
