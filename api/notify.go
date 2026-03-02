package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// NotifyConfig 通知配置（Telegram + 微信）
type NotifyConfig struct {
	Enabled  bool   `json:"enabled"`
	BotToken string `json:"botToken"` // Telegram Bot Token
	ChatID   string `json:"chatId"`   // Telegram Chat ID

	// 微信推送
	WechatEnabled bool   `json:"wechatEnabled"`
	WechatKey     string `json:"wechatKey"`  // Server酱 SendKey 或 PushPlus token
	WechatType    string `json:"wechatType"` // "serverchan" 或 "pushplus"
}

var (
	notifyCfg  NotifyConfig
	notifyOnce sync.Once
	notifyHTTP = &http.Client{Timeout: 10 * time.Second}
)

// InitNotify 初始化通知模块
func InitNotify(cfg NotifyConfig) {
	notifyCfg = cfg
	if cfg.Enabled && cfg.BotToken != "" && cfg.ChatID != "" {
		log.Printf("[Notify] Telegram enabled, chatID=%s", cfg.ChatID)
		// 启动日终汇总定时器
		go dailySummaryLoop()
	} else {
		log.Println("[Notify] Telegram disabled")
	}
}

// SendNotify 发送通知（异步，不阻塞业务）
func SendNotify(message string) {
	if notifyCfg.Enabled && notifyCfg.BotToken != "" && notifyCfg.ChatID != "" {
		go sendTelegram(message)
	}
	if notifyCfg.WechatEnabled && notifyCfg.WechatKey != "" {
		// 提取标题（第一行）
		title := message
		if idx := strings.Index(message, "\n"); idx > 0 {
			title = message[:idx]
		}
		// 去掉 Markdown 星号
		title = strings.ReplaceAll(title, "*", "")
		go sendWechat(title, message)
	}
}

// sendWechat 发送微信推送
// wechatType="serverchan": POST https://sctapi.ftqq.com/{key}.send
// wechatType="pushplus":   POST https://www.pushplus.plus/send
func sendWechat(title, content string) {
	wType := strings.ToLower(strings.TrimSpace(notifyCfg.WechatType))
	key := notifyCfg.WechatKey

	var (
		reqURL  string
		reqBody io.Reader
		ctype   string
	)

	switch wType {
	case "pushplus":
		reqURL = "https://www.pushplus.plus/send"
		body := map[string]string{
			"token":   key,
			"title":   title,
			"content": content,
		}
		data, _ := json.Marshal(body)
		reqBody = bytes.NewReader(data)
		ctype = "application/json"

	default: // serverchan
		reqURL = fmt.Sprintf("https://sctapi.ftqq.com/%s.send", key)
		form := url.Values{}
		form.Set("title", title)
		form.Set("desp", content)
		reqBody = strings.NewReader(form.Encode())
		ctype = "application/x-www-form-urlencoded"
	}

	resp, err := notifyHTTP.Post(reqURL, ctype, reqBody)
	if err != nil {
		log.Printf("[Notify] Wechat send failed (%s): %v", wType, err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		log.Printf("[Notify] Wechat API returned %d (type=%s)", resp.StatusCode, wType)
	}
}

func sendTelegram(message string) {
	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", notifyCfg.BotToken)
	body := map[string]interface{}{
		"chat_id":    notifyCfg.ChatID,
		"text":       message,
		"parse_mode": "Markdown",
	}
	data, _ := json.Marshal(body)
	resp, err := notifyHTTP.Post(url, "application/json", bytes.NewReader(data))
	if err != nil {
		log.Printf("[Notify] Send failed: %v", err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		log.Printf("[Notify] Telegram API returned %d", resp.StatusCode)
	}
}

// NotifyTradeOpen 开仓通知
func NotifyTradeOpen(symbol, direction, amount string, leverage int, reason string) {
	msg := fmt.Sprintf("*开仓* `%s`\n方向: %s\n金额: %s USDT x %dx\n原因: %s",
		symbol, direction, amount, leverage, reason)
	SendNotify(msg)
}

// NotifyTradeClose 平仓通知
func NotifyTradeClose(symbol, direction string, pnl float64, reason string) {
	pnlSign := "+"
	if pnl < 0 {
		pnlSign = ""
	}
	msg := fmt.Sprintf("*平仓* `%s`\n方向: %s\nPnL: %s%.4f USDT\n原因: %s",
		symbol, direction, pnlSign, pnl, reason)
	SendNotify(msg)
}

// NotifyTPSLTriggered 止盈止损触发通知
func NotifyTPSLTriggered(condType, symbol string, triggerPrice float64, quantity string) {
	label := "止盈触发"
	if condType == "STOP_LOSS" || condType == "TRAILING_STOP" {
		label = "止损触发"
	}
	msg := fmt.Sprintf("*%s* `%s`\n触发价: %.4f\n数量: %s",
		label, symbol, triggerPrice, quantity)
	SendNotify(msg)
}

// NotifyRiskLocked 风控锁定通知
func NotifyRiskLocked(reason string) {
	msg := fmt.Sprintf("*风控锁定*\n%s\n所有策略已暂停至次日", reason)
	SendNotify(msg)
}

// dailySummaryLoop 每日 23:55 发送日终汇总
func dailySummaryLoop() {
	notifyOnce.Do(func() {
		for {
			now := time.Now()
			// 计算到今天 23:55 的时间
			target := time.Date(now.Year(), now.Month(), now.Day(), 23, 55, 0, 0, now.Location())
			if now.After(target) {
				target = target.Add(24 * time.Hour)
			}
			time.Sleep(time.Until(target))
			sendDailySummary()
		}
	})
}

func sendDailySummary() {
	status := GetRiskStatus()
	dailyPnl, _ := status["dailyPnl"].(float64)
	dailyLosses, _ := status["dailyLosses"].(int)

	pnlSign := "+"
	if dailyPnl < 0 {
		pnlSign = ""
	}

	msg := fmt.Sprintf("*日终汇总* %s\n盈亏: %s%.4f USDT\n亏损次数: %d",
		time.Now().Format("2006-01-02"), pnlSign, dailyPnl, dailyLosses)

	// 获取持仓状态
	portfolio := GetPortfolioStatus()
	totalNotional, _ := portfolio["totalNotional"].(float64)
	longCount, _ := portfolio["longCount"].(int)
	shortCount, _ := portfolio["shortCount"].(int)

	msg += fmt.Sprintf("\n持仓: 多%d/空%d, 名义%.0f USDT", longCount, shortCount, totalNotional)

	SendNotify(msg)
}
