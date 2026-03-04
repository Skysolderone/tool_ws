package agent

import (
	"context"
	"encoding/json"
	"strings"
	"time"

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

// CollectHistory 收集最近成功分析摘要，默认最多 5 条。
func CollectHistory(limit int) ([]HistorySummary, error) {
	if limit <= 0 {
		limit = 5
	}
	if limit > 10 {
		limit = 10
	}

	records, err := api.GetRecentAnalysisSummaries(limit)
	if err != nil {
		return nil, err
	}
	if len(records) == 0 {
		return nil, nil
	}

	out := make([]HistorySummary, 0, len(records))
	for _, record := range records {
		item := HistorySummary{
			Date:     record.CreatedAt.In(time.Local).Format("2006-01-02"),
			Mode:     strings.TrimSpace(record.Mode),
			Symbols:  normalizeAndDedupeSymbols(strings.Split(strings.TrimSpace(record.Symbols), ",")),
			Executed: strings.TrimSpace(record.ExecutionBody) != "",
		}

		var parsed struct {
			Summary     string       `json:"summary"`
			ActionItems []ActionItem `json:"action_items"`
		}
		if err := json.Unmarshal([]byte(record.ResponseBody), &parsed); err == nil {
			item.Summary = truncateText(parsed.Summary, 200)
			if len(parsed.ActionItems) > 3 {
				item.ActionItems = append([]ActionItem(nil), parsed.ActionItems[:3]...)
			} else if len(parsed.ActionItems) > 0 {
				item.ActionItems = append([]ActionItem(nil), parsed.ActionItems...)
			}
		}
		if item.Summary == "" {
			item.Summary = truncateText(record.ResponseBody, 200)
		}

		if item.Executed {
			var exec struct {
				Success int `json:"success"`
				Failed  int `json:"failed"`
			}
			if err := json.Unmarshal([]byte(record.ExecutionBody), &exec); err == nil {
				item.ExecutionResult = map[string]int{
					"success": exec.Success,
					"failed":  exec.Failed,
				}
			}
		}

		out = append(out, item)
	}
	return out, nil
}

// CollectNews 收集最近新闻摘要。
func CollectNews(limit int) []api.NewsDigest {
	if limit <= 0 {
		limit = 10
	}
	items := api.GetNewsForAgent(limit)
	if len(items) == 0 {
		return nil
	}
	return items
}

func truncateText(s string, maxRunes int) string {
	s = strings.TrimSpace(s)
	if s == "" || maxRunes <= 0 {
		return s
	}
	rs := []rune(s)
	if len(rs) <= maxRunes {
		return s
	}
	return strings.TrimSpace(string(rs[:maxRunes]))
}
