package api

import (
	"context"
	"errors"
	"log"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/utils"
	"gorm.io/gorm"
)

const (
	evalWindow1H  = time.Hour
	evalWindow4H  = 4 * time.Hour
	evalWindow24H = 24 * time.Hour
)

// AgentEvaluationRecord Agent 建议效果评估记录。
type AgentEvaluationRecord struct {
	gorm.Model
	AnalysisLogID uint      `gorm:"index" json:"analysisLogId"`
	TradeRecordID uint      `gorm:"index" json:"tradeRecordId"`
	Mode          string    `gorm:"type:varchar(20);index" json:"mode"`
	ActionType    string    `gorm:"type:varchar(20)" json:"actionType"`
	Symbol        string    `gorm:"type:varchar(20);index" json:"symbol"`
	Direction     string    `gorm:"type:varchar(10)" json:"direction"`
	EntryPrice    float64   `gorm:"type:numeric(36,18)" json:"entryPrice"`
	CurrentPrice  float64   `gorm:"type:numeric(36,18)" json:"currentPrice"`
	PnlUSDT       float64   `gorm:"type:numeric(18,4)" json:"pnlUsdt"`
	PnlPct        float64   `gorm:"type:numeric(8,4)" json:"pnlPct"`
	Hit           bool      `json:"hit"`
	EvaluatedAt   time.Time `json:"evaluatedAt"`

	// 多时间窗口评估结果
	PnlPct1H     float64 `gorm:"type:numeric(8,4)" json:"pnlPct1H"`
	PnlPct4H     float64 `gorm:"type:numeric(8,4)" json:"pnlPct4H"`
	PnlPct24H    float64 `gorm:"type:numeric(8,4)" json:"pnlPct24H"`
	Evaluated1H  bool    `gorm:"default:false" json:"evaluated1H"`
	Evaluated4H  bool    `gorm:"default:false" json:"evaluated4H"`
	Evaluated24H bool    `gorm:"default:false" json:"evaluated24H"`
	Final        bool    `gorm:"default:false;index" json:"final"`
}

// AgentEvalSummary 评估汇总（新结构 + 兼容旧字段）。
type AgentEvalSummary struct {
	TotalSuggestions int                     `json:"total_suggestions"`
	HitRate1H        float64                 `json:"hit_rate_1h"`
	HitRate4H        float64                 `json:"hit_rate_4h"`
	HitRate24H       float64                 `json:"hit_rate_24h"`
	AvgPnl1H         float64                 `json:"avg_pnl_1h"`
	AvgPnl24H        float64                 `json:"avg_pnl_24h"`
	BestMode         string                  `json:"best_mode"`
	WorstSymbols     []string                `json:"worst_symbols,omitempty"`
	Records          []AgentEvaluationRecord `json:"records,omitempty"`

	// 兼容旧版前端字段
	TotalAdvices  int     `json:"totalAdvices,omitempty"`
	ExecutedCount int     `json:"executedCount,omitempty"`
	HitRate       float64 `json:"hitRate,omitempty"`
	AvgPnlUSDT    float64 `json:"avgPnlUsdt,omitempty"`
	TotalPnlUSDT  float64 `json:"totalPnlUsdt,omitempty"`
}

var agentEvalStopCh chan struct{}

// StartAgentEvaluator 启动后台评估器。
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

	now := time.Now()
	since := now.AddDate(0, 0, -3)
	var logs []AgentAnalysisLog
	if err := DB.Where("execute = true AND status = ? AND created_at >= ?", AgentAnalysisStatusSuccess, since).
		Order("created_at DESC").
		Limit(300).
		Find(&logs).Error; err != nil {
		log.Printf("[AgentEval] load logs failed: %v", err)
		return
	}

	for _, alog := range logs {
		evaluateSingleLog(alog, now)
	}

	summary, err := GetAgentEvalSummary(7)
	if err != nil {
		log.Printf("[AgentEval] summary failed: %v", err)
		return
	}
	AutoTuneAgentPolicy(*summary)
}

func evaluateSingleLog(alog AgentAnalysisLog, now time.Time) {
	if alog.ExecutionBody == "" {
		return
	}

	start := alog.CreatedAt
	end := start.Add(evalWindow1H)

	var trades []TradeRecord
	if err := DB.Where("source = 'agent' AND created_at BETWEEN ? AND ?", start, end).Find(&trades).Error; err != nil {
		log.Printf("[AgentEval] load trades failed log_id=%d err=%v", alog.ID, err)
		return
	}

	for _, trade := range trades {
		symbol := strings.ToUpper(strings.TrimSpace(trade.Symbol))
		if symbol == "" || trade.Price <= 0 {
			continue
		}

		currentPrice, err := GetPriceCache().GetPrice(symbol)
		if err != nil || currentPrice <= 0 {
			continue
		}

		pnlUSDT, pnlPct, hit := calcTradeEvalMetrics(trade, currentPrice)

		var rec AgentEvaluationRecord
		err = DB.Where("analysis_log_id = ? AND trade_record_id = ?", alog.ID, trade.ID).First(&rec).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			rec = AgentEvaluationRecord{
				AnalysisLogID: alog.ID,
				TradeRecordID: trade.ID,
				Mode:          strings.TrimSpace(alog.Mode),
				ActionType:    "open",
				Symbol:        symbol,
				Direction:     strings.ToUpper(strings.TrimSpace(trade.Side)),
				EntryPrice:    trade.Price,
			}
			if createErr := DB.Create(&rec).Error; createErr != nil {
				log.Printf("[AgentEval] create record failed log_id=%d trade_id=%d err=%v", alog.ID, trade.ID, createErr)
				continue
			}
		} else if err != nil {
			log.Printf("[AgentEval] query record failed log_id=%d trade_id=%d err=%v", alog.ID, trade.ID, err)
			continue
		}

		elapsed := now.Sub(alog.CreatedAt)
		updates := map[string]any{
			"current_price": currentPrice,
			"pnl_usdt":      roundFloat(pnlUSDT, 4),
			"pnl_pct":       roundFloat(pnlPct, 4),
			"hit":           hit,
			"evaluated_at":  now,
		}
		if elapsed >= evalWindow1H && !rec.Evaluated1H {
			updates["pnl_pct_1h"] = roundFloat(pnlPct, 4)
			updates["evaluated_1h"] = true
		}
		if elapsed >= evalWindow4H && !rec.Evaluated4H {
			updates["pnl_pct_4h"] = roundFloat(pnlPct, 4)
			updates["evaluated_4h"] = true
		}
		if elapsed >= evalWindow24H && !rec.Evaluated24H {
			updates["pnl_pct_24h"] = roundFloat(pnlPct, 4)
			updates["evaluated_24h"] = true
			updates["final"] = true
		}

		if err := DB.Model(&AgentEvaluationRecord{}).Where("id = ?", rec.ID).Updates(updates).Error; err != nil {
			log.Printf("[AgentEval] update record failed id=%d err=%v", rec.ID, err)
		}
	}
}

func calcTradeEvalMetrics(trade TradeRecord, currentPrice float64) (pnlUSDT float64, pnlPct float64, hit bool) {
	qty := math.Abs(trade.Quantity)
	side := strings.ToUpper(strings.TrimSpace(trade.Side))

	if side == "BUY" {
		pnlUSDT = (currentPrice - trade.Price) * qty
		hit = currentPrice > trade.Price
	} else {
		pnlUSDT = (trade.Price - currentPrice) * qty
		hit = currentPrice < trade.Price
	}

	if trade.Price > 0 {
		pnlPct = math.Abs(currentPrice-trade.Price) / trade.Price * 100
		if !hit {
			pnlPct = -pnlPct
		}
	}
	return pnlUSDT, pnlPct, hit
}

// GetAgentEvalSummary 获取评估汇总。
func GetAgentEvalSummary(days int) (*AgentEvalSummary, error) {
	if DB == nil {
		return &AgentEvalSummary{}, nil
	}
	if days <= 0 {
		days = 30
	}

	since := time.Now().AddDate(0, 0, -days)
	var records []AgentEvaluationRecord
	if err := DB.Where("created_at >= ?", since).
		Order("created_at DESC").
		Limit(800).
		Find(&records).Error; err != nil {
		return nil, err
	}

	summary := &AgentEvalSummary{
		TotalSuggestions: len(records),
		Records:          records,
	}
	if len(records) == 0 {
		return summary, nil
	}

	type horizonStat struct {
		total int
		hits  int
		sum   float64
	}
	type ratioStat struct {
		total int
		hits  int
	}

	var stat1h horizonStat
	var stat4h horizonStat
	var stat24h horizonStat
	modeStats := make(map[string]*ratioStat)
	symbolStats := make(map[string]*ratioStat)

	for _, record := range records {
		if record.Evaluated1H {
			stat1h.total++
			stat1h.sum += record.PnlPct1H
			if record.PnlPct1H > 0 {
				stat1h.hits++
			}
		}
		if record.Evaluated4H {
			stat4h.total++
			stat4h.sum += record.PnlPct4H
			if record.PnlPct4H > 0 {
				stat4h.hits++
			}
		}
		if record.Evaluated24H {
			stat24h.total++
			stat24h.sum += record.PnlPct24H
			if record.PnlPct24H > 0 {
				stat24h.hits++
			}

			mode := strings.TrimSpace(record.Mode)
			if mode == "" {
				mode = "unknown"
			}
			if modeStats[mode] == nil {
				modeStats[mode] = &ratioStat{}
			}
			modeStats[mode].total++
			if record.PnlPct24H > 0 {
				modeStats[mode].hits++
			}

			symbol := strings.ToUpper(strings.TrimSpace(record.Symbol))
			if symbol != "" {
				if symbolStats[symbol] == nil {
					symbolStats[symbol] = &ratioStat{}
				}
				symbolStats[symbol].total++
				if record.PnlPct24H > 0 {
					symbolStats[symbol].hits++
				}
			}
		}
	}

	hitRate := func(stat horizonStat) float64 {
		if stat.total == 0 {
			return 0
		}
		return roundFloat(float64(stat.hits)/float64(stat.total)*100, 2)
	}
	avgPct := func(stat horizonStat) float64 {
		if stat.total == 0 {
			return 0
		}
		return roundFloat(stat.sum/float64(stat.total), 4)
	}
	rateOf := func(s *ratioStat) float64 {
		if s == nil || s.total == 0 {
			return 0
		}
		return float64(s.hits) / float64(s.total) * 100
	}

	summary.HitRate1H = hitRate(stat1h)
	summary.HitRate4H = hitRate(stat4h)
	summary.HitRate24H = hitRate(stat24h)
	summary.AvgPnl1H = avgPct(stat1h)
	summary.AvgPnl24H = avgPct(stat24h)

	bestMode := ""
	bestModeRate := -1.0
	bestModeCount := -1
	for mode, stat := range modeStats {
		rate := rateOf(stat)
		if rate > bestModeRate || (rate == bestModeRate && stat.total > bestModeCount) {
			bestMode = mode
			bestModeRate = rate
			bestModeCount = stat.total
		}
	}
	summary.BestMode = bestMode

	type symbolRank struct {
		Symbol string
		Rate   float64
		Count  int
	}
	ranked := make([]symbolRank, 0, len(symbolStats))
	for symbol, stat := range symbolStats {
		if stat.total == 0 {
			continue
		}
		ranked = append(ranked, symbolRank{
			Symbol: symbol,
			Rate:   rateOf(stat),
			Count:  stat.total,
		})
	}
	sort.Slice(ranked, func(i, j int) bool {
		if ranked[i].Rate == ranked[j].Rate {
			if ranked[i].Count == ranked[j].Count {
				return ranked[i].Symbol < ranked[j].Symbol
			}
			return ranked[i].Count > ranked[j].Count
		}
		return ranked[i].Rate < ranked[j].Rate
	})
	for _, item := range ranked {
		if item.Count < 2 {
			continue
		}
		summary.WorstSymbols = append(summary.WorstSymbols, item.Symbol)
		if len(summary.WorstSymbols) >= 5 {
			break
		}
	}
	if len(summary.WorstSymbols) == 0 {
		for i := 0; i < len(ranked) && i < 3; i++ {
			summary.WorstSymbols = append(summary.WorstSymbols, ranked[i].Symbol)
		}
	}

	// 兼容旧版字段映射
	summary.TotalAdvices = summary.TotalSuggestions
	summary.ExecutedCount = len(records)
	summary.HitRate = summary.HitRate24H
	summary.AvgPnlUSDT = summary.AvgPnl24H
	summary.TotalPnlUSDT = roundFloat(stat24h.sum, 4)

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
