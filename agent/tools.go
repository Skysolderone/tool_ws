package agent

import (
	"context"
	"strings"

	"tools/api"
)

func normalizeSymbols(symbols []string) map[string]struct{} {
	if len(symbols) == 0 {
		return nil
	}
	set := make(map[string]struct{}, len(symbols))
	for _, s := range symbols {
		s = strings.ToUpper(strings.TrimSpace(s))
		if s == "" {
			continue
		}
		set[s] = struct{}{}
	}
	if len(set) == 0 {
		return nil
	}
	return set
}

func collectPositionsData(ctx context.Context, symbols []string) (any, error) {
	resp, err := api.GetAnalyzedPositions(ctx)
	if err != nil || resp == nil {
		return nil, err
	}

	symbolSet := normalizeSymbols(symbols)
	if len(symbolSet) == 0 {
		return resp, nil
	}

	filtered := *resp
	filtered.Items = make([]api.PositionAnalysis, 0, len(resp.Items))
	for _, item := range resp.Items {
		if _, ok := symbolSet[strings.ToUpper(item.Symbol)]; ok {
			filtered.Items = append(filtered.Items, item)
		}
	}
	filtered.Count = len(filtered.Items)
	return &filtered, nil
}

func collectSignalsData(symbols []string) any {
	resp := api.GetRecommendCache()
	if resp == nil {
		return nil
	}

	symbolSet := normalizeSymbols(symbols)
	if len(symbolSet) == 0 {
		return resp
	}

	filtered := *resp
	filtered.Items = make([]api.RecommendItem, 0, len(resp.Items))
	for _, item := range resp.Items {
		if _, ok := symbolSet[strings.ToUpper(item.Symbol)]; ok {
			filtered.Items = append(filtered.Items, item)
		}
	}
	filtered.Count = len(filtered.Items)
	return &filtered
}

func collectJournalData(days int) (any, error) {
	return api.GetJournalMetrics(days)
}

func collectSentimentData() any {
	return api.GetMarketSentiment()
}

func collectBalanceData(ctx context.Context) (any, error) {
	return api.GetBalance(ctx)
}
