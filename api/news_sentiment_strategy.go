package api

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/adshao/go-binance/v2/futures"
)

// ========== 新闻情绪事件驱动策略 ==========
// 监控新闻关键词，识别利多/利空信号并可选自动开仓

// NewsSentimentConfig 新闻情绪策略配置
type NewsSentimentConfig struct {
	Enabled        bool     `json:"enabled"`
	Symbols        []string `json:"symbols"`        // 默认 ["BTCUSDT"]
	AmountPerOrder string   `json:"amountPerOrder"` // 每笔金额(USDT)
	Leverage       int      `json:"leverage"`       // 杠杆
	CooldownMin    int      `json:"cooldownMin"`    // 冷却分钟，默认 30
	AutoTrade      bool     `json:"autoTrade"`      // true=自动下单，false=仅通知
}

// NewsSentimentStatus 策略状态
type NewsSentimentStatus struct {
	Config        NewsSentimentConfig `json:"config"`
	Active        bool                `json:"active"`
	LastCheckAt   string              `json:"lastCheckAt"`
	LastSignal    string              `json:"lastSignal"`    // BULLISH / BEARISH / NONE
	LastNewsID    string              `json:"lastNewsId"`
	LastNewsTitle string              `json:"lastNewsTitle"`
	TotalSignals  int                 `json:"totalSignals"`
	TotalTrades   int                 `json:"totalTrades"`
	LastError     string              `json:"lastError"`
}

// 利多关键词
var bullishKeywords = []string{
	"ETF", "降息", "批准", "通过", "合作", "上线", "利好", "bullish", "approved",
	"adoption", "approve", "launch", "partnership", "积极", "突破", "创新高",
}

// 利空关键词
var bearishKeywords = []string{
	"黑客", "攻击", "罚款", "监管", "暴跌", "破产", "关闭", "bearish", "hack", "exploit",
	"ban", "crackdown", "shutdown", "fraud", "骗局", "禁止", "警告", "崩盘",
}

type newsSentimentState struct {
	mu            sync.RWMutex
	config        NewsSentimentConfig
	active        bool
	stopC         chan struct{}
	lastCheckAt   time.Time
	lastSignal    string
	lastNewsID    string
	lastNewsTitle string
	totalSignals  int
	totalTrades   int
	lastError     string

	// 冷却：已触发过的新闻 ID 集合，防止重复触发
	triggeredIDs map[string]time.Time
	// 上次触发时间（全局冷却）
	lastTriggerAt time.Time
}

var (
	newsSentiment     = &newsSentimentState{triggeredIDs: make(map[string]time.Time)}
	newsSentimentOnce sync.Mutex
)

// StartNewsSentiment 启动新闻情绪策略
func StartNewsSentiment(cfg NewsSentimentConfig) error {
	newsSentimentOnce.Lock()
	defer newsSentimentOnce.Unlock()

	newsSentiment.mu.Lock()
	if newsSentiment.active {
		newsSentiment.mu.Unlock()
		return fmt.Errorf("news sentiment strategy already running")
	}
	newsSentiment.mu.Unlock()

	// 参数默认值
	if len(cfg.Symbols) == 0 {
		cfg.Symbols = []string{"BTCUSDT"}
	}
	if cfg.CooldownMin <= 0 {
		cfg.CooldownMin = 30
	}
	if cfg.Leverage <= 0 {
		cfg.Leverage = 5
	}
	if cfg.AmountPerOrder == "" {
		cfg.AmountPerOrder = "10"
	}

	stopC := make(chan struct{})

	newsSentiment.mu.Lock()
	newsSentiment.config = cfg
	newsSentiment.active = true
	newsSentiment.stopC = stopC
	newsSentiment.lastSignal = "NONE"
	newsSentiment.triggeredIDs = make(map[string]time.Time)
	newsSentiment.mu.Unlock()

	go runNewsSentimentLoop(stopC, cfg)

	SaveStrategyState("news_sentiment", "*", cfg)
	log.Printf("[NewsSentiment] Started, symbols=%v autoTrade=%v cooldown=%dmin",
		cfg.Symbols, cfg.AutoTrade, cfg.CooldownMin)
	return nil
}

// StopNewsSentiment 停止新闻情绪策略
func StopNewsSentiment() error {
	newsSentiment.mu.Lock()
	defer newsSentiment.mu.Unlock()

	if !newsSentiment.active {
		return fmt.Errorf("news sentiment strategy not running")
	}
	close(newsSentiment.stopC)
	newsSentiment.active = false
	newsSentiment.stopC = nil

	MarkStrategyStopped("news_sentiment", "*")
	log.Printf("[NewsSentiment] Stopped")
	return nil
}

// GetNewsSentimentStatus 查询策略状态
func GetNewsSentimentStatus() *NewsSentimentStatus {
	newsSentiment.mu.RLock()
	defer newsSentiment.mu.RUnlock()

	s := &NewsSentimentStatus{
		Config:        newsSentiment.config,
		Active:        newsSentiment.active,
		LastSignal:    newsSentiment.lastSignal,
		LastNewsID:    newsSentiment.lastNewsID,
		LastNewsTitle: newsSentiment.lastNewsTitle,
		TotalSignals:  newsSentiment.totalSignals,
		TotalTrades:   newsSentiment.totalTrades,
		LastError:     newsSentiment.lastError,
	}
	if !newsSentiment.lastCheckAt.IsZero() {
		s.LastCheckAt = newsSentiment.lastCheckAt.Format(time.RFC3339)
	}
	return s
}

func runNewsSentimentLoop(stopC chan struct{}, cfg NewsSentimentConfig) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	// 启动立即执行一次
	checkNewsSentiment(cfg)

	for {
		select {
		case <-stopC:
			return
		case <-ticker.C:
			checkNewsSentiment(cfg)
		}
	}
}

func checkNewsSentiment(cfg NewsSentimentConfig) {
	newsSentiment.mu.Lock()
	newsSentiment.lastCheckAt = time.Now()
	newsSentiment.mu.Unlock()

	// 拉取最新新闻
	data, _, err := fetchNewsSnapshot()
	if err != nil {
		newsSentiment.mu.Lock()
		newsSentiment.lastError = err.Error()
		newsSentiment.mu.Unlock()
		log.Printf("[NewsSentiment] Fetch news failed: %v", err)
		return
	}

	// 合并所有来源新闻
	var allNews []newsItem
	for _, items := range data {
		allNews = append(allNews, items...)
	}

	cooldownDur := time.Duration(cfg.CooldownMin) * time.Minute

	newsSentiment.mu.Lock()
	lastTrigger := newsSentiment.lastTriggerAt
	newsSentiment.mu.Unlock()

	// 全局冷却检查
	if time.Since(lastTrigger) < cooldownDur {
		return
	}

	// 清理过期已触发 ID（超过冷却期的清除）
	newsSentiment.mu.Lock()
	for id, t := range newsSentiment.triggeredIDs {
		if time.Since(t) > cooldownDur*2 {
			delete(newsSentiment.triggeredIDs, id)
		}
	}
	triggered := newsSentiment.triggeredIDs
	newsSentiment.mu.Unlock()

	for _, item := range allNews {
		// 跳过已触发的新闻
		if _, ok := triggered[item.ID]; ok {
			continue
		}

		signal := detectNewsSentiment(item)
		if signal == "" {
			continue
		}

		// 记录触发
		newsSentiment.mu.Lock()
		newsSentiment.triggeredIDs[item.ID] = time.Now()
		newsSentiment.lastTriggerAt = time.Now()
		newsSentiment.lastSignal = signal
		newsSentiment.lastNewsID = item.ID
		newsSentiment.lastNewsTitle = item.Title
		newsSentiment.totalSignals++
		newsSentiment.mu.Unlock()

		log.Printf("[NewsSentiment] Signal=%s news=%q", signal, item.Title)

		if cfg.AutoTrade {
			executeNewsSentimentTrade(cfg, signal, item)
		} else {
			msg := fmt.Sprintf("*新闻情绪信号* %s\n方向: %s\n标题: %s\n来源: %s",
				signal, signalToDirection(signal), item.Title, item.Source)
			SendNotify(msg)
		}

		// 触发一条后等待冷却，本轮不再处理其他新闻
		break
	}

	newsSentiment.mu.Lock()
	newsSentiment.lastError = ""
	newsSentiment.mu.Unlock()
}

// detectNewsSentiment 检测新闻情绪，返回 "BULLISH"/"BEARISH"/""
func detectNewsSentiment(item newsItem) string {
	text := strings.ToLower(item.Title + " " + item.Summary)

	bullishScore := 0
	bearishScore := 0

	for _, kw := range bullishKeywords {
		if strings.Contains(text, strings.ToLower(kw)) {
			bullishScore++
		}
	}
	for _, kw := range bearishKeywords {
		if strings.Contains(text, strings.ToLower(kw)) {
			bearishScore++
		}
	}

	if bullishScore > bearishScore && bullishScore > 0 {
		return "BULLISH"
	}
	if bearishScore > bullishScore && bearishScore > 0 {
		return "BEARISH"
	}
	return ""
}

func signalToDirection(signal string) string {
	if signal == "BULLISH" {
		return "多头 (做多)"
	}
	return "空头 (做空)"
}

func executeNewsSentimentTrade(cfg NewsSentimentConfig, signal string, item newsItem) {
	if err := CheckRisk(); err != nil {
		log.Printf("[NewsSentiment] Risk check failed: %v", err)
		return
	}

	side := futures.SideTypeBuy
	positionSide := futures.PositionSideTypeLong
	if signal == "BEARISH" {
		side = futures.SideTypeSell
		positionSide = futures.PositionSideTypeShort
	}

	for _, symbol := range cfg.Symbols {
		req := PlaceOrderReq{
			Source:        "news_sentiment",
			Symbol:        symbol,
			Side:          side,
			OrderType:     futures.OrderTypeMarket,
			PositionSide:  positionSide,
			QuoteQuantity: cfg.AmountPerOrder,
			Leverage:      cfg.Leverage,
		}

		resp, err := PlaceOrderViaWs(context.Background(), req)
		if err != nil {
			log.Printf("[NewsSentiment] Place order failed symbol=%s: %v", symbol, err)
			SendNotify(fmt.Sprintf("*新闻情绪* 下单失败 %s: %v", symbol, err))
			continue
		}

		newsSentiment.mu.Lock()
		newsSentiment.totalTrades++
		newsSentiment.mu.Unlock()

		orderID := int64(0)
		if resp != nil && resp.Order != nil {
			orderID = resp.Order.OrderID
		}
		log.Printf("[NewsSentiment] Order placed symbol=%s signal=%s orderID=%d", symbol, signal, orderID)

		msg := fmt.Sprintf("*新闻情绪自动开仓* %s\n信号: %s\n交易对: %s\n金额: %s USDT x %dx\n新闻: %s",
			signal, signalToDirection(signal), symbol, cfg.AmountPerOrder, cfg.Leverage, item.Title)
		SendNotify(msg)
	}
}
