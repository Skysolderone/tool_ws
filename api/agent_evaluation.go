package api

import (
	"context"
	"log"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/utils"
	"gorm.io/gorm"
)

// AgentEvaluationRecord Agent 建议效果评估记录
type AgentEvaluationRecord struct {
	gorm.Model
	AnalysisLogID uint    `gorm:"index" json:"analysisLogId"`
	ActionType    string  `gorm:"type:varchar(20)" json:"actionType"`
	Symbol        string  `gorm:"type:varchar(20);index" json:"symbol"`
	Direction     string  `gorm:"type:varchar(10)" json:"direction"`
	EntryPrice    float64 `gorm:"type:numeric(36,18)" json:"entryPrice"`
	CurrentPrice  float64 `gorm:"type:numeric(36,18)" json:"currentPrice"`
	PnlUSDT      float64 `gorm:"type:numeric(18,4)" json:"pnlUsdt"`
	PnlPct       float64 `gorm:"type:numeric(8,4)" json:"pnlPct"`
	Hit          bool    `json:"hit"`
	EvaluatedAt  time.Time `json:"evaluatedAt"`
}

// AgentEvalSummary 评估汇总
type AgentEvalSummary struct {
	TotalAdvices  int     `json:"totalAdvices"`
	ExecutedCount int     `json:"executedCount"`
	HitRate       float64 `json:"hitRate"`
	AvgPnlUSDT   float64 `json:"avgPnlUsdt"`
	TotalPnlUSDT float64 `json:"totalPnlUsdt"`
	Records       []AgentEvaluationRecord `json:"records,omitempty"`
}

var agentEvalStopCh chan struct{}

// StartAgentEvaluator 启动后台评估器
func StartAgentEvaluator(intervalMin int) {
	if intervalMin <= 0 {
		intervalMin = 30
	}
	agentEvalStopCh = make(chan struct{})
	go agentEvalLoop(intervalMin)
	log.Printf("[AgentEval] Started with interval=%dm", intervalMin)
}

func agentEvalLoop(intervalMin int) {
	time.Sleep(30 * time.Second)
	evaluateRecentAdvices()

	ticker := time.NewTicker(time.Duration(intervalMin) * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-agentEvalStopCh:
			return
		case <-ticker.C:
			evaluateRecentAdvices()
		}
	}
}

func evaluateRecentAdvices() {
	if DB == nil {
		return
	}

	// 查找最近 7 天的已执行分析日志
	since := time.Now().AddDate(0, 0, -7)
	var logs []AgentAnalysisLog
	DB.Where("execute = true AND status = 'SUCCESS' AND created_at >= ?", since).
		Order("created_at DESC").Limit(50).Find(&logs)

	for _, alog := range logs {
		// 检查是否已评估
		var count int64
		DB.Model(&AgentEvaluationRecord{}).Where("analysis_log_id = ?", alog.ID).Count(&count)
		if count > 0 {
			continue
		}
		evaluateSingleLog(alog)
	}
}

func evaluateSingleLog(alog AgentAnalysisLog) {
	// 解析执行结果中的成功订单
	if alog.ExecutionBody == "" {
		return
	}

	// 找出执行了哪些交易
	var trades []TradeRecord
	// 查找分析后 1 小时内 agent 来源的交易
	start := alog.CreatedAt
	end := start.Add(1 * time.Hour)
	DB.Where("source = 'agent' AND created_at BETWEEN ? AND ?", start, end).Find(&trades)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	for _, trade := range trades {
		currentPrice, err := GetPriceCache().GetPrice(trade.Symbol)
		if err != nil || currentPrice <= 0 {
			continue
		}

		// 计算方向命中
		pnl := 0.0
		hit := false
		if trade.Side == "BUY" {
			pnl = (currentPrice - trade.Price) * trade.Quantity
			hit = currentPrice > trade.Price
		} else {
			pnl = (trade.Price - currentPrice) * trade.Quantity
			hit = currentPrice < trade.Price
		}

		pnlPct := 0.0
		if trade.Price > 0 {
			pnlPct = math.Abs(currentPrice-trade.Price) / trade.Price * 100
			if !hit {
				pnlPct = -pnlPct
			}
		}

		record := &AgentEvaluationRecord{
			AnalysisLogID: alog.ID,
			ActionType:    "open",
			Symbol:        trade.Symbol,
			Direction:     trade.Side,
			EntryPrice:    trade.Price,
			CurrentPrice:  currentPrice,
			PnlUSDT:      roundFloat(pnl, 4),
			PnlPct:       roundFloat(pnlPct, 4),
			Hit:          hit,
			EvaluatedAt:  time.Now(),
		}
		DB.Create(record)
	}
	_ = ctx
}

// GetAgentEvalSummary 获取评估汇总
func GetAgentEvalSummary(days int) (*AgentEvalSummary, error) {
	if DB == nil {
		return &AgentEvalSummary{}, nil
	}
	if days <= 0 {
		days = 30
	}

	since := time.Now().AddDate(0, 0, -days)
	var records []AgentEvaluationRecord
	DB.Where("created_at >= ?", since).Order("created_at DESC").Limit(200).Find(&records)

	summary := &AgentEvalSummary{
		ExecutedCount: len(records),
		Records:       records,
	}

	if len(records) == 0 {
		return summary, nil
	}

	var hits int
	var totalPnl float64
	for _, r := range records {
		if r.Hit {
			hits++
		}
		totalPnl += r.PnlUSDT
	}

	summary.HitRate = roundFloat(float64(hits)/float64(len(records))*100, 2)
	summary.TotalPnlUSDT = roundFloat(totalPnl, 4)
	summary.AvgPnlUSDT = roundFloat(totalPnl/float64(len(records)), 4)

	// 统计总建议数
	var logCount int64
	DB.Model(&AgentAnalysisLog{}).Where("created_at >= ?", since).Count(&logCount)
	summary.TotalAdvices = int(logCount)

	return summary, nil
}

// HandleGetAgentEval GET /tool/agent/evaluation?days=30
func HandleGetAgentEval(c context.Context, ctx *app.RequestContext) {
	days, _ := strconv.Atoi(strings.TrimSpace(string(ctx.Query("days"))))
	summary, err := GetAgentEvalSummary(days)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, utils.H{"error": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, utils.H{"data": summary})
}
