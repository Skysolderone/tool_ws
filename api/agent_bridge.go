package api

import (
	"context"
	"sort"
	"strconv"
	"sync"
	"time"
)

// JournalMetrics 导出交易日志指标类型，供 agent 包使用。
type JournalMetrics = journalMetrics

// JournalBucket 导出交易日志分桶类型，供 agent 包使用。
type JournalBucket = journalBucket

// JournalResponse 导出交易日志响应类型，供 agent 包使用。
type JournalResponse = journalResponse

// GetAnalyzedPositions 导出持仓分析结果，供 agent 包调用。
func GetAnalyzedPositions(ctx context.Context) (*AnalyzeResponse, error) {
	positions, err := GetPositionsViaWs(ctx)
	if err != nil {
		return nil, err
	}

	sentimentCache.RLock()
	sentiment := sentimentCache.data
	sentimentCache.RUnlock()

	sem := make(chan struct{}, 6)
	var wg sync.WaitGroup
	type result struct {
		pa *PositionAnalysis
	}
	resultCh := make(chan result, len(positions))

	for _, pos := range positions {
		amt, _ := strconv.ParseFloat(pos.PositionAmt, 64)
		if amt == 0 {
			continue
		}
		wg.Add(1)
		go func(p posInfo) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			pa := analyzePosition(ctx, p)
			resultCh <- result{pa: pa}
		}(posInfo{
			symbol:     pos.Symbol,
			side:       pos.PositionSide,
			entryPrice: parseF(pos.EntryPrice),
			markPrice:  parseF(pos.MarkPrice),
			amount:     amt,
			leverage:   parseInt(pos.Leverage),
			pnl:        parseF(pos.UnRealizedProfit),
		})
	}

	go func() {
		wg.Wait()
		close(resultCh)
	}()

	var items []PositionAnalysis
	for res := range resultCh {
		if res.pa != nil {
			items = append(items, *res.pa)
		}
	}

	sort.Slice(items, func(i, j int) bool {
		return items[i].PnlPercent < items[j].PnlPercent
	})

	return &AnalyzeResponse{
		Items:      items,
		Sentiment:  sentiment,
		AnalyzedAt: time.Now().Format(time.RFC3339),
		Count:      len(items),
	}, nil
}

// GetRecommendCache 导出推荐缓存，供 agent 包调用。
func GetRecommendCache() *RecommendResponse {
	finalCache.RLock()
	defer finalCache.RUnlock()

	if finalCache.resp == nil {
		return nil
	}

	clone := *finalCache.resp
	if len(finalCache.resp.Items) > 0 {
		clone.Items = append([]RecommendItem(nil), finalCache.resp.Items...)
	}
	return &clone
}

// GetJournalMetrics 导出交易日志统计，供 agent 包调用。
func GetJournalMetrics(days int) (*JournalResponse, error) {
	if days <= 0 {
		days = analyticsDefaultDays
	}

	now := time.Now().UTC()
	from := now.AddDate(0, 0, -days)

	records, err := loadTradesForAnalytics(from, now)
	if err != nil {
		return nil, err
	}

	filtered := make([]TradeRecord, 0, len(records))
	for _, r := range records {
		if includeTradeForAnalytics(r) {
			filtered = append(filtered, r)
		}
	}

	return &JournalResponse{
		From:    from.UnixMilli(),
		To:      now.UnixMilli(),
		Period:  "daily",
		Overall: calcJournalMetrics(filtered, now),
		Buckets: buildJournalBuckets(filtered, "daily", now),
	}, nil
}

// GetMarketSentiment 导出市场情绪，供 agent 包调用。
func GetMarketSentiment() MarketSentiment {
	return calcMarketSentiment()
}
