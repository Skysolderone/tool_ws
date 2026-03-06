package api

import (
	"context"
	"log"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/utils"
)

const (
	recommendEvalWindow1H  = time.Hour
	recommendEvalWindow4H  = 4 * time.Hour
	recommendEvalWindow24H = 24 * time.Hour
)

// RecommendSignalEvaluationRecord 推荐信号效果评估记录。
type RecommendSignalEvaluationRecord struct {
	ID             uint      `gorm:"primaryKey" json:"id"`
	SignalRecordID uint      `gorm:"uniqueIndex;index" json:"signalRecordId"`
	Symbol         string    `gorm:"type:varchar(20);index" json:"symbol"`
	Direction      string    `gorm:"type:varchar(10);index" json:"direction"`
	Source         string    `gorm:"type:varchar(30);index" json:"source"`
	Confidence     int       `json:"confidence"`
	EntryPrice     float64   `gorm:"type:numeric(36,8)" json:"entryPrice"`
	ScannedAt      time.Time `gorm:"index" json:"scannedAt"`

	CurrentPrice float64   `gorm:"type:numeric(36,8)" json:"currentPrice"`
	PnlPct       float64   `gorm:"type:numeric(10,4)" json:"pnlPct"`
	Hit          bool      `json:"hit"`
	EvaluatedAt  time.Time `json:"evaluatedAt"`

	PnlPct1H     float64 `gorm:"type:numeric(10,4)" json:"pnlPct1H"`
	PnlPct4H     float64 `gorm:"type:numeric(10,4)" json:"pnlPct4H"`
	PnlPct24H    float64 `gorm:"type:numeric(10,4)" json:"pnlPct24H"`
	Evaluated1H  bool    `gorm:"default:false;index" json:"evaluated1H"`
	Evaluated4H  bool    `gorm:"default:false;index" json:"evaluated4H"`
	Evaluated24H bool    `gorm:"default:false;index" json:"evaluated24H"`
	Final        bool    `gorm:"default:false;index" json:"final"`

	CreatedAt time.Time `gorm:"autoCreateTime" json:"createdAt"`
	UpdatedAt time.Time `gorm:"autoUpdateTime" json:"updatedAt"`
}

// RecommendEvalSummary 推荐信号评估汇总。
type RecommendEvalSummary struct {
	TotalSignals int `json:"total_signals"`

	HitRate1H  float64 `json:"hit_rate_1h"`
	HitRate4H  float64 `json:"hit_rate_4h"`
	HitRate24H float64 `json:"hit_rate_24h"`

	AvgPnl1H  float64 `json:"avg_pnl_1h"`
	AvgPnl24H float64 `json:"avg_pnl_24h"`

	WorstSymbols []string                          `json:"worst_symbols,omitempty"`
	Records      []RecommendSignalEvaluationRecord `json:"records,omitempty"`
}

var recommendEvalStopCh chan struct{}

// StartRecommendSignalEvaluator 启动推荐信号后台评估器。
func StartRecommendSignalEvaluator(intervalMin int) {
	if intervalMin <= 0 {
		intervalMin = 30
	}
	recommendEvalStopCh = make(chan struct{})
	go recommendEvalLoop(intervalMin)
	log.Printf("[RecommendEval] Started with interval=%dm", intervalMin)
}

func recommendEvalLoop(intervalMin int) {
	time.Sleep(20 * time.Second)
	evaluateRecentRecommendSignals()

	ticker := time.NewTicker(time.Duration(intervalMin) * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-recommendEvalStopCh:
			return
		case <-ticker.C:
			evaluateRecentRecommendSignals()
		}
	}
}

func evaluateRecentRecommendSignals() {
	if DB == nil {
		return
	}

	now := time.Now()
	since := now.AddDate(0, 0, -3)

	var signals []RecommendSignalRecord
	if err := DB.Where("scanned_at >= ?", since).
		Order("scanned_at DESC").
		Limit(3000).
		Find(&signals).Error; err != nil {
		log.Printf("[RecommendEval] load signals failed: %v", err)
		return
	}
	if len(signals) == 0 {
		return
	}

	signalIDs := make([]uint, 0, len(signals))
	for _, signal := range signals {
		if signal.ID > 0 {
			signalIDs = append(signalIDs, signal.ID)
		}
	}

	var existing []RecommendSignalEvaluationRecord
	if len(signalIDs) > 0 {
		if err := DB.Where("signal_record_id IN ?", signalIDs).Find(&existing).Error; err != nil {
			log.Printf("[RecommendEval] load existing records failed: %v", err)
			return
		}
	}
	existingBySignalID := make(map[uint]*RecommendSignalEvaluationRecord, len(existing))
	for i := range existing {
		row := existing[i]
		existingBySignalID[row.SignalRecordID] = &row
	}

	for _, signal := range signals {
		symbol := strings.ToUpper(strings.TrimSpace(signal.Symbol))
		direction := strings.ToUpper(strings.TrimSpace(signal.Direction))
		if symbol == "" || signal.Entry <= 0 || (direction != "LONG" && direction != "SHORT") {
			continue
		}

		record := existingBySignalID[signal.ID]
		if record == nil {
			row := RecommendSignalEvaluationRecord{
				SignalRecordID: signal.ID,
				Symbol:         symbol,
				Direction:      direction,
				Source:         strings.TrimSpace(signal.Source),
				Confidence:     signal.Confidence,
				EntryPrice:     signal.Entry,
				ScannedAt:      signal.ScannedAt,
			}
			if err := DB.Create(&row).Error; err != nil {
				log.Printf("[RecommendEval] create record failed signal_id=%d err=%v", signal.ID, err)
				continue
			}
			record = &row
			existingBySignalID[signal.ID] = record
		}

		currentPrice, ok := resolveRecommendEvalPrice(symbol)
		if !ok || currentPrice <= 0 {
			continue
		}
		pnlPct, hit := calcRecommendSignalEvalMetrics(direction, signal.Entry, currentPrice)

		updates := map[string]any{
			"current_price": currentPrice,
			"pnl_pct":       roundFloat(pnlPct, 4),
			"hit":           hit,
			"evaluated_at":  now,
		}

		elapsed := now.Sub(signal.ScannedAt)
		if elapsed >= recommendEvalWindow1H && !record.Evaluated1H {
			updates["pnl_pct_1h"] = roundFloat(pnlPct, 4)
			updates["evaluated_1h"] = true
			record.Evaluated1H = true
		}
		if elapsed >= recommendEvalWindow4H && !record.Evaluated4H {
			updates["pnl_pct_4h"] = roundFloat(pnlPct, 4)
			updates["evaluated_4h"] = true
			record.Evaluated4H = true
		}
		if elapsed >= recommendEvalWindow24H && !record.Evaluated24H {
			updates["pnl_pct_24h"] = roundFloat(pnlPct, 4)
			updates["evaluated_24h"] = true
			updates["final"] = true
			record.Evaluated24H = true
		}

		if err := DB.Model(&RecommendSignalEvaluationRecord{}).Where("id = ?", record.ID).Updates(updates).Error; err != nil {
			log.Printf("[RecommendEval] update record failed id=%d err=%v", record.ID, err)
		}
	}
}

func resolveRecommendEvalPrice(symbol string) (float64, bool) {
	if price, err := GetPriceCache().GetPrice(symbol); err == nil && price > 0 {
		return price, true
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	price, err := getCurrentPrice(ctx, symbol, "")
	if err != nil || price <= 0 {
		return 0, false
	}
	GetPriceCache().UpdatePrice(symbol, price)
	return price, true
}

func calcRecommendSignalEvalMetrics(direction string, entryPrice, currentPrice float64) (pnlPct float64, hit bool) {
	if entryPrice <= 0 || currentPrice <= 0 {
		return 0, false
	}
	switch strings.ToUpper(strings.TrimSpace(direction)) {
	case "LONG":
		pnlPct = (currentPrice - entryPrice) / entryPrice * 100
	case "SHORT":
		pnlPct = (entryPrice - currentPrice) / entryPrice * 100
	default:
		return 0, false
	}
	hit = pnlPct > 0
	return pnlPct, hit
}

// GetRecommendEvalSummary 获取推荐信号评估汇总。
func GetRecommendEvalSummary(days int) (*RecommendEvalSummary, error) {
	if DB == nil {
		return &RecommendEvalSummary{}, nil
	}
	if days <= 0 {
		days = 30
	}

	since := time.Now().AddDate(0, 0, -days)
	var records []RecommendSignalEvaluationRecord
	if err := DB.Where("scanned_at >= ?", since).
		Order("scanned_at DESC").
		Limit(3000).
		Find(&records).Error; err != nil {
		return nil, err
	}

	summary := &RecommendEvalSummary{
		TotalSignals: len(records),
		Records:      records,
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

	type symbolRank struct {
		Symbol string
		Rate   float64
		Count  int
	}
	ranked := make([]symbolRank, 0, len(symbolStats))
	for symbol, stat := range symbolStats {
		if stat == nil || stat.total == 0 {
			continue
		}
		ranked = append(ranked, symbolRank{
			Symbol: symbol,
			Rate:   rateOf(stat),
			Count:  stat.total,
		})
	}
	sort.Slice(ranked, func(i, j int) bool {
		if math.Abs(ranked[i].Rate-ranked[j].Rate) < 1e-9 {
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

	return summary, nil
}

// HandleGetRecommendEval GET /tool/recommend/evaluation?days=30
func HandleGetRecommendEval(c context.Context, ctx *app.RequestContext) {
	days, _ := strconv.Atoi(strings.TrimSpace(string(ctx.Query("days"))))
	summary, err := GetRecommendEvalSummary(days)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, utils.H{"error": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, utils.H{"data": summary})
}
