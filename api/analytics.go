package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
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
	analyticsDefaultDays      = 30
	analyticsMaxRangeDays     = 366
	binanceFundingURL         = "https://fapi.binance.com/fapi/v1/premiumIndex"
	binanceLongShortRatioURL  = "https://fapi.binance.com/futures/data/globalLongShortAccountRatio"
	sentimentDefaultSymbol    = "BTCUSDT"
	sentimentDefaultLSPeriod  = "5m"
	sentimentLiqNotionalScale = 10_000_000.0
)

type journalMetrics struct {
	TotalTrades         int64            `json:"totalTrades"`
	Wins                int64            `json:"wins"`
	Losses              int64            `json:"losses"`
	Breakeven           int64            `json:"breakeven"`
	WinRate             float64          `json:"winRate"`
	GrossProfit         float64          `json:"grossProfit"`
	GrossLoss           float64          `json:"grossLoss"`
	NetPnl              float64          `json:"netPnl"`
	AvgWin              float64          `json:"avgWin"`
	AvgLoss             float64          `json:"avgLoss"`
	PnlRatio            float64          `json:"pnlRatio"`
	ProfitFactor        float64          `json:"profitFactor"`
	MaxDrawdown         float64          `json:"maxDrawdown"`
	MaxDrawdownPct      float64          `json:"maxDrawdownPct"`
	AvgHoldMinutes      float64          `json:"avgHoldMinutes"`
	HoldDurationBuckets map[string]int64 `json:"holdDurationBuckets"`
}

type journalBucket struct {
	Key       string         `json:"key"`
	StartTime int64          `json:"startTime"`
	EndTime   int64          `json:"endTime"`
	Metrics   journalMetrics `json:"metrics"`
}

type journalResponse struct {
	From    int64           `json:"from"`
	To      int64           `json:"to"`
	Period  string          `json:"period"`
	Overall journalMetrics  `json:"overall"`
	Buckets []journalBucket `json:"buckets"`
}

type attributionItem struct {
	Key         string  `json:"key"`
	Trades      int64   `json:"trades"`
	Wins        int64   `json:"wins"`
	Losses      int64   `json:"losses"`
	WinRate     float64 `json:"winRate"`
	GrossProfit float64 `json:"grossProfit"`
	GrossLoss   float64 `json:"grossLoss"`
	NetPnl      float64 `json:"netPnl"`
	AvgPnl      float64 `json:"avgPnl"`
}

type attributionHourItem struct {
	HourUTC int `json:"hourUTC"`
	attributionItem
}

type attributionResponse struct {
	From   int64                 `json:"from"`
	To     int64                 `json:"to"`
	Source []attributionItem     `json:"source"`
	Symbol []attributionItem     `json:"symbol"`
	Hour   []attributionHourItem `json:"hour"`
}

type sentimentComponent struct {
	Score float64 `json:"score"`
	Value float64 `json:"value"`
}

type sentimentLongShort struct {
	Score        float64 `json:"score"`
	LongAccount  float64 `json:"longAccount"`
	ShortAccount float64 `json:"shortAccount"`
	Timestamp    int64   `json:"timestamp"`
}

type sentimentLiquidation struct {
	Score         float64 `json:"score"`
	Direction     float64 `json:"direction"`
	Intensity     float64 `json:"intensity"`
	TotalNotional float64 `json:"totalNotional"`
	BuyNotional   float64 `json:"buyNotional"`
	SellNotional  float64 `json:"sellNotional"`
}

type sentimentResponse struct {
	Symbol      string               `json:"symbol"`
	Time        int64                `json:"time"`
	Timezone    string               `json:"timezone"`
	Index       float64              `json:"index"`
	Regime      string               `json:"regime"`
	Liquidation sentimentLiquidation `json:"liquidation"`
	Funding     sentimentComponent   `json:"funding"`
	LongShort   sentimentLongShort   `json:"longShort"`
	Warnings    []string             `json:"warnings,omitempty"`
}

type analyticsAgg struct {
	trades      int64
	wins        int64
	losses      int64
	grossProfit float64
	grossLoss   float64
	netPnl      float64
}

var analyticsHTTP = &http.Client{Timeout: 8 * time.Second}

// HandleGetAnalyticsJournal GET /api/analytics/journal?period=daily|weekly&from=...&to=...
func HandleGetAnalyticsJournal(c context.Context, ctx *app.RequestContext) {
	period := strings.ToLower(strings.TrimSpace(ctx.DefaultQuery("period", "daily")))
	if period != "daily" && period != "weekly" {
		ctx.JSON(http.StatusBadRequest, utils.H{"error": "period must be daily or weekly"})
		return
	}

	from, to, err := parseAnalyticsRange(ctx)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, utils.H{"error": err.Error()})
		return
	}

	records, err := loadTradesForAnalytics(from, to)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, utils.H{"error": err.Error()})
		return
	}

	filtered := make([]TradeRecord, 0, len(records))
	for _, r := range records {
		if includeTradeForAnalytics(r) {
			filtered = append(filtered, r)
		}
	}

	overall := calcJournalMetrics(filtered, to)
	buckets := buildJournalBuckets(filtered, period, to)
	ctx.JSON(http.StatusOK, utils.H{"data": journalResponse{
		From:    from.UnixMilli(),
		To:      to.UnixMilli(),
		Period:  period,
		Overall: overall,
		Buckets: buckets,
	}})
}

// HandleGetAnalyticsAttribution GET /api/analytics/attribution?from=...&to=...
func HandleGetAnalyticsAttribution(c context.Context, ctx *app.RequestContext) {
	from, to, err := parseAnalyticsRange(ctx)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, utils.H{"error": err.Error()})
		return
	}

	records, err := loadTradesForAnalytics(from, to)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, utils.H{"error": err.Error()})
		return
	}

	sourceMap := make(map[string]*analyticsAgg)
	symbolMap := make(map[string]*analyticsAgg)
	hourMap := make(map[int]*analyticsAgg)

	for _, r := range records {
		if !includeTradeForAnalytics(r) {
			continue
		}
		pnl := parseTradePnl(r)
		source := strings.TrimSpace(r.Source)
		if source == "" {
			source = "unknown"
		}
		symbol := strings.TrimSpace(r.Symbol)
		if symbol == "" {
			symbol = "UNKNOWN"
		}
		h := analyticsCloseTime(r, to).UTC().Hour()

		for _, target := range []*analyticsAgg{
			getOrCreateAgg(sourceMap, source),
			getOrCreateAgg(symbolMap, symbol),
			getOrCreateHourAgg(hourMap, h),
		} {
			target.trades++
			target.netPnl += pnl
			if pnl > 0 {
				target.wins++
				target.grossProfit += pnl
			} else if pnl < 0 {
				target.losses++
				target.grossLoss += pnl
			}
		}
	}

	ctx.JSON(http.StatusOK, utils.H{"data": attributionResponse{
		From:   from.UnixMilli(),
		To:     to.UnixMilli(),
		Source: finalizeAttributionMap(sourceMap),
		Symbol: finalizeAttributionMap(symbolMap),
		Hour:   finalizeHourAttributionMap(hourMap),
	}})
}

// HandleGetAnalyticsSentiment GET /api/analytics/sentiment?symbol=BTCUSDT&period=5m
func HandleGetAnalyticsSentiment(c context.Context, ctx *app.RequestContext) {
	symbol := strings.ToUpper(strings.TrimSpace(ctx.DefaultQuery("symbol", sentimentDefaultSymbol)))
	if symbol == "" {
		symbol = sentimentDefaultSymbol
	}
	period := strings.TrimSpace(ctx.DefaultQuery("period", sentimentDefaultLSPeriod))
	if period == "" {
		period = sentimentDefaultLSPeriod
	}

	warnings := make([]string, 0, 2)
	fundingRate, fundingErr := fetchFundingRate(symbol)
	if fundingErr != nil {
		warnings = append(warnings, "funding rate unavailable: "+fundingErr.Error())
	}
	longShortData, longShortErr := fetchGlobalLongShortAccount(symbol, period)
	if longShortErr != nil {
		warnings = append(warnings, "long/short account unavailable: "+longShortErr.Error())
	}

	liq := loadLatestLiquidationStat()
	liqTotal := liq.TotalNotional
	liqDirection := 0.0
	if liqTotal > 0 {
		liqDirection = clamp((liq.BuyNotional-liq.SellNotional)/liqTotal, -1, 1)
	}
	liqDirectionScore := 50 + 50*liqDirection
	liqIntensityScore := clamp(liqTotal/sentimentLiqNotionalScale*100, 0, 100)
	liqScore := 0.7*liqDirectionScore + 0.3*liqIntensityScore

	fundingScore := 50.0
	if fundingErr == nil {
		fundingScore = clamp(50+fundingRate/0.0005*50, 0, 100)
	}
	longShortScore := 50.0
	if longShortErr == nil {
		longShortScore = clamp(50+(longShortData.LongAccount-longShortData.ShortAccount)*50, 0, 100)
	}

	index := 0.35*liqScore + 0.30*fundingScore + 0.35*longShortScore
	regime := "neutral"
	if index >= 65 {
		regime = "bullish"
	} else if index <= 35 {
		regime = "bearish"
	}

	ctx.JSON(http.StatusOK, utils.H{"data": sentimentResponse{
		Symbol:   symbol,
		Time:     time.Now().UTC().UnixMilli(),
		Timezone: "UTC",
		Index:    index,
		Regime:   regime,
		Liquidation: sentimentLiquidation{
			Score:         liqScore,
			Direction:     liqDirection,
			Intensity:     liqIntensityScore,
			TotalNotional: liqTotal,
			BuyNotional:   liq.BuyNotional,
			SellNotional:  liq.SellNotional,
		},
		Funding: sentimentComponent{Score: fundingScore, Value: fundingRate},
		LongShort: sentimentLongShort{
			Score:        longShortScore,
			LongAccount:  longShortData.LongAccount,
			ShortAccount: longShortData.ShortAccount,
			Timestamp:    longShortData.Timestamp,
		},
		Warnings: warnings,
	}})
}

func parseAnalyticsRange(ctx *app.RequestContext) (time.Time, time.Time, error) {
	now := time.Now().UTC()
	defaultFrom := now.Add(-analyticsDefaultDays * 24 * time.Hour)

	from := defaultFrom
	to := now
	var err error

	fromRaw := strings.TrimSpace(ctx.DefaultQuery("from", ""))
	toRaw := strings.TrimSpace(ctx.DefaultQuery("to", ""))

	if fromRaw != "" {
		from, err = parseFlexibleTime(fromRaw)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("invalid from")
		}
	}
	if toRaw != "" {
		to, err = parseFlexibleTime(toRaw)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("invalid to")
		}
	}

	from = from.UTC()
	to = to.UTC()
	if !from.Before(to) {
		return time.Time{}, time.Time{}, fmt.Errorf("from must be earlier than to")
	}
	if to.Sub(from) > analyticsMaxRangeDays*24*time.Hour {
		return time.Time{}, time.Time{}, fmt.Errorf("range too large")
	}
	return from, to, nil
}

func parseFlexibleTime(raw string) (time.Time, error) {
	layouts := []string{
		time.RFC3339,
		time.RFC3339Nano,
		"2006-01-02",
		"2006-01-02 15:04:05",
	}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, raw); err == nil {
			if layout == "2006-01-02" {
				return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC), nil
			}
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("invalid time")
}

func loadTradesForAnalytics(from, to time.Time) ([]TradeRecord, error) {
	if DB == nil {
		return []TradeRecord{}, nil
	}
	var records []TradeRecord
	err := DB.Where(
		"(closed_at >= ? AND closed_at < ?) OR (closed_at IS NULL AND updated_at >= ? AND updated_at < ?)",
		from, to, from, to,
	).
		Order("updated_at ASC").
		Find(&records).Error
	return records, err
}

func includeTradeForAnalytics(r TradeRecord) bool {
	status := strings.ToUpper(strings.TrimSpace(r.Status))
	if status == "CANCELED" || status == "REJECTED" || status == "EXPIRED" {
		return false
	}
	if status == "OPEN" {
		return parseTradePnl(r) != 0
	}
	return true
}

func parseTradePnl(r TradeRecord) float64 {
	v, _ := strconv.ParseFloat(strings.TrimSpace(r.RealizedPnl), 64)
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0
	}
	return v
}

func analyticsCloseTime(r TradeRecord, fallback time.Time) time.Time {
	if r.ClosedAt != nil && !r.ClosedAt.IsZero() {
		return r.ClosedAt.UTC()
	}
	if !r.UpdatedAt.IsZero() {
		return r.UpdatedAt.UTC()
	}
	if !r.CreatedAt.IsZero() {
		return r.CreatedAt.UTC()
	}
	return fallback.UTC()
}

func calcJournalMetrics(records []TradeRecord, now time.Time) journalMetrics {
	m := journalMetrics{HoldDurationBuckets: map[string]int64{
		"<=15m":  0,
		"15-60m": 0,
		"1-4h":   0,
		"4-24h":  0,
		">24h":   0,
	}}
	if len(records) == 0 {
		return m
	}

	sorted := make([]TradeRecord, 0, len(records))
	holdMinutesSum := 0.0
	holdCount := 0.0
	for _, r := range records {
		pnl := parseTradePnl(r)
		m.TotalTrades++
		m.NetPnl += pnl
		if pnl > 0 {
			m.Wins++
			m.GrossProfit += pnl
		} else if pnl < 0 {
			m.Losses++
			m.GrossLoss += pnl
		} else {
			m.Breakeven++
		}

		closeTime := analyticsCloseTime(r, now)
		if !r.CreatedAt.IsZero() && closeTime.After(r.CreatedAt) {
			d := closeTime.Sub(r.CreatedAt).Minutes()
			holdMinutesSum += d
			holdCount++
			switch {
			case d <= 15:
				m.HoldDurationBuckets["<=15m"]++
			case d <= 60:
				m.HoldDurationBuckets["15-60m"]++
			case d <= 240:
				m.HoldDurationBuckets["1-4h"]++
			case d <= 1440:
				m.HoldDurationBuckets["4-24h"]++
			default:
				m.HoldDurationBuckets[">24h"]++
			}
		}
		sorted = append(sorted, r)
	}

	if holdCount > 0 {
		m.AvgHoldMinutes = holdMinutesSum / holdCount
	}

	if m.Wins+m.Losses > 0 {
		m.WinRate = float64(m.Wins) / float64(m.Wins+m.Losses)
	}
	if m.Wins > 0 {
		m.AvgWin = m.GrossProfit / float64(m.Wins)
	}
	if m.Losses > 0 {
		m.AvgLoss = math.Abs(m.GrossLoss) / float64(m.Losses)
	}
	if m.AvgLoss > 0 {
		m.PnlRatio = m.AvgWin / m.AvgLoss
	}
	if m.GrossLoss < 0 {
		m.ProfitFactor = m.GrossProfit / math.Abs(m.GrossLoss)
	}

	sort.Slice(sorted, func(i, j int) bool {
		return analyticsCloseTime(sorted[i], now).Before(analyticsCloseTime(sorted[j], now))
	})

	equity := 0.0
	peak := 0.0
	maxDD := 0.0
	for _, r := range sorted {
		equity += parseTradePnl(r)
		if equity > peak {
			peak = equity
		}
		dd := peak - equity
		if dd > maxDD {
			maxDD = dd
		}
	}
	m.MaxDrawdown = maxDD
	if peak > 0 {
		m.MaxDrawdownPct = maxDD / peak
	}

	return m
}

func buildJournalBuckets(records []TradeRecord, period string, now time.Time) []journalBucket {
	type group struct {
		start time.Time
		end   time.Time
		key   string
		rows  []TradeRecord
	}
	groups := make(map[int64]*group)
	for _, r := range records {
		closeTime := analyticsCloseTime(r, now).UTC()
		var start time.Time
		var end time.Time
		key := ""
		if period == "weekly" {
			start = startOfISOWeek(closeTime)
			end = start.Add(7 * 24 * time.Hour)
			year, week := start.ISOWeek()
			key = fmt.Sprintf("%04d-W%02d", year, week)
		} else {
			start = time.Date(closeTime.Year(), closeTime.Month(), closeTime.Day(), 0, 0, 0, 0, time.UTC)
			end = start.Add(24 * time.Hour)
			key = start.Format("2006-01-02")
		}
		k := start.UnixMilli()
		g, ok := groups[k]
		if !ok {
			g = &group{start: start, end: end, key: key, rows: make([]TradeRecord, 0, 8)}
			groups[k] = g
		}
		g.rows = append(g.rows, r)
	}

	starts := make([]int64, 0, len(groups))
	for k := range groups {
		starts = append(starts, k)
	}
	sort.Slice(starts, func(i, j int) bool { return starts[i] > starts[j] })

	out := make([]journalBucket, 0, len(starts))
	for _, k := range starts {
		g := groups[k]
		out = append(out, journalBucket{
			Key:       g.key,
			StartTime: g.start.UnixMilli(),
			EndTime:   g.end.UnixMilli(),
			Metrics:   calcJournalMetrics(g.rows, now),
		})
	}
	return out
}

func startOfISOWeek(t time.Time) time.Time {
	t = t.UTC()
	weekday := int(t.Weekday())
	if weekday == 0 {
		weekday = 7
	}
	start := t.AddDate(0, 0, -(weekday - 1))
	return time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, time.UTC)
}

func getOrCreateAgg(m map[string]*analyticsAgg, key string) *analyticsAgg {
	if v, ok := m[key]; ok {
		return v
	}
	v := &analyticsAgg{}
	m[key] = v
	return v
}

func getOrCreateHourAgg(m map[int]*analyticsAgg, hour int) *analyticsAgg {
	if v, ok := m[hour]; ok {
		return v
	}
	v := &analyticsAgg{}
	m[hour] = v
	return v
}

func finalizeAttributionMap(m map[string]*analyticsAgg) []attributionItem {
	out := make([]attributionItem, 0, len(m))
	for k, v := range m {
		item := attributionItem{
			Key:         k,
			Trades:      v.trades,
			Wins:        v.wins,
			Losses:      v.losses,
			GrossProfit: v.grossProfit,
			GrossLoss:   v.grossLoss,
			NetPnl:      v.netPnl,
		}
		if v.wins+v.losses > 0 {
			item.WinRate = float64(v.wins) / float64(v.wins+v.losses)
		}
		if v.trades > 0 {
			item.AvgPnl = v.netPnl / float64(v.trades)
		}
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].NetPnl > out[j].NetPnl
	})
	return out
}

func finalizeHourAttributionMap(m map[int]*analyticsAgg) []attributionHourItem {
	keys := make([]int, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Ints(keys)
	out := make([]attributionHourItem, 0, len(keys))
	for _, k := range keys {
		v := m[k]
		item := attributionHourItem{
			HourUTC: k,
			attributionItem: attributionItem{
				Key:         fmt.Sprintf("%02d", k),
				Trades:      v.trades,
				Wins:        v.wins,
				Losses:      v.losses,
				GrossProfit: v.grossProfit,
				GrossLoss:   v.grossLoss,
				NetPnl:      v.netPnl,
			},
		}
		if v.wins+v.losses > 0 {
			item.WinRate = float64(v.wins) / float64(v.wins+v.losses)
		}
		if v.trades > 0 {
			item.AvgPnl = v.netPnl / float64(v.trades)
		}
		out = append(out, item)
	}
	return out
}

func loadLatestLiquidationStat() liquidationBucketStat {
	history, err := GetLiquidationHistory(1)
	if err == nil && len(history.H1) > 0 {
		h := history.H1[0]
		return liquidationBucketStat{
			StartTime:     h.StartTime,
			EndTime:       h.EndTime,
			TotalCount:    h.TotalCount,
			BuyCount:      h.BuyCount,
			SellCount:     h.SellCount,
			TotalNotional: h.TotalNotional,
			BuyNotional:   h.BuyNotional,
			SellNotional:  h.SellNotional,
		}
	}

	snapshot := liquidationStore.snapshot(time.Now().UTC())
	if len(snapshot.Stats.H1) > 0 {
		return snapshot.Stats.H1[0]
	}
	return liquidationBucketStat{}
}

func fetchFundingRate(symbol string) (float64, error) {
	url := fmt.Sprintf("%s?symbol=%s", binanceFundingURL, symbol)
	resp, err := analyticsHTTP.Get(url)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return 0, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return 0, err
	}

	var payload struct {
		LastFundingRate string `json:"lastFundingRate"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return 0, err
	}
	v, err := strconv.ParseFloat(strings.TrimSpace(payload.LastFundingRate), 64)
	if err != nil {
		return 0, err
	}
	return v, nil
}

func fetchGlobalLongShortAccount(symbol, period string) (sentimentLongShort, error) {
	url := fmt.Sprintf("%s?symbol=%s&period=%s&limit=1", binanceLongShortRatioURL, symbol, period)
	resp, err := analyticsHTTP.Get(url)
	if err != nil {
		return sentimentLongShort{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return sentimentLongShort{}, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return sentimentLongShort{}, err
	}

	var payload []struct {
		LongAccount  string          `json:"longAccount"`
		ShortAccount string          `json:"shortAccount"`
		Timestamp    json.RawMessage `json:"timestamp"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return sentimentLongShort{}, err
	}
	if len(payload) == 0 {
		return sentimentLongShort{}, fmt.Errorf("empty payload")
	}
	longAcc, err := strconv.ParseFloat(strings.TrimSpace(payload[0].LongAccount), 64)
	if err != nil {
		return sentimentLongShort{}, err
	}
	shortAcc, err := strconv.ParseFloat(strings.TrimSpace(payload[0].ShortAccount), 64)
	if err != nil {
		return sentimentLongShort{}, err
	}
	ts, err := parseRawInt64(payload[0].Timestamp)
	if err != nil {
		return sentimentLongShort{}, err
	}

	return sentimentLongShort{
		LongAccount:  longAcc,
		ShortAccount: shortAcc,
		Timestamp:    ts,
	}, nil
}

func parseRawInt64(raw json.RawMessage) (int64, error) {
	if len(raw) == 0 {
		return 0, fmt.Errorf("empty timestamp")
	}

	var num int64
	if err := json.Unmarshal(raw, &num); err == nil {
		return num, nil
	}

	var str string
	if err := json.Unmarshal(raw, &str); err == nil {
		return strconv.ParseInt(strings.TrimSpace(str), 10, 64)
	}

	return 0, fmt.Errorf("invalid timestamp")
}

func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
