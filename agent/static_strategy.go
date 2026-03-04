package agent

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"

	"tools/api"
)

func runStaticStrategyAnalysis(ctx context.Context, req AnalysisRequest) (*AnalysisOutput, error) {
	resp, err := api.GetAnalyzedPositions(ctx)
	if err != nil {
		return nil, fmt.Errorf("获取持仓分析失败: %w", err)
	}
	if resp == nil {
		return &AnalysisOutput{}, nil
	}

	items := filterPositionAnalysisBySymbols(resp.Items, req.Symbols)
	out := buildStaticAnalysisOutput(items, resp.Sentiment)
	return out, nil
}

func filterPositionAnalysisBySymbols(items []api.PositionAnalysis, symbols []string) []api.PositionAnalysis {
	if len(items) == 0 {
		return nil
	}
	symbols = normalizeAndDedupeSymbols(symbols)
	if len(symbols) == 0 {
		return append([]api.PositionAnalysis(nil), items...)
	}

	allowed := make(map[string]struct{}, len(symbols))
	for _, symbol := range symbols {
		allowed[strings.ToUpper(strings.TrimSpace(symbol))] = struct{}{}
	}

	out := make([]api.PositionAnalysis, 0, len(items))
	for _, item := range items {
		symbol := strings.ToUpper(strings.TrimSpace(item.Symbol))
		if _, ok := allowed[symbol]; !ok {
			continue
		}
		out = append(out, item)
	}
	return out
}

func buildStaticAnalysisOutput(items []api.PositionAnalysis, sentiment api.MarketSentiment) *AnalysisOutput {
	out := &AnalysisOutput{
		PositionAnalysis: make([]PositionAdvice, 0, len(items)),
		SignalEvaluation: make([]SignalEval, 0, len(items)),
		ActionItems:      make([]ActionItem, 0, len(items)),
		JournalReview: JournalInsight{
			Patterns:   []string{},
			Weaknesses: []string{},
			Strengths:  []string{},
			Suggestion: "本次结果基于写死策略生成，建议优先处理高风险仓位并设置止损。",
		},
	}

	if len(items) == 0 {
		out.Summary = "当前无持仓，暂无需要处理的仓位建议。"
		out.JournalReview.Patterns = append(out.JournalReview.Patterns, "当前无持仓数据")
		return out
	}

	profitCount := 0
	lossCount := 0
	highRiskCount := 0
	actionStats := map[string]int{}

	for _, item := range items {
		riskLevel := staticRiskLevel(item)
		if riskLevel == "high" || riskLevel == "critical" {
			highRiskCount++
		}
		if item.PnlPercent > 0 {
			profitCount++
		} else if item.PnlPercent < 0 {
			lossCount++
		}

		pa := PositionAdvice{
			Symbol:     item.Symbol,
			Assessment: staticAssessment(item),
			Risk:       riskLevel,
			Suggestion: staticSuggestion(item),
			Reasons:    staticReasons(item),
		}
		out.PositionAnalysis = append(out.PositionAnalysis, pa)

		if sig, ok := staticSignalEval(item, riskLevel); ok {
			out.SignalEvaluation = append(out.SignalEvaluation, sig)
		}

		if action, ok := staticActionItem(item, riskLevel); ok {
			out.ActionItems = append(out.ActionItems, action)
			actionStats[action.Action]++
		}
	}

	sort.Slice(out.ActionItems, func(i, j int) bool {
		return actionPriorityScore(out.ActionItems[i].Priority) > actionPriorityScore(out.ActionItems[j].Priority)
	})

	out.Summary = fmt.Sprintf(
		"策略持仓分析完成：共 %d 个仓位（盈利 %d / 亏损 %d），高风险 %d；市场情绪 %s(%.0f)。建议动作：平仓%d、减仓%d、止损%d、止盈%d、加仓%d。",
		len(items),
		profitCount,
		lossCount,
		highRiskCount,
		strings.ToLower(strings.TrimSpace(sentiment.Bias)),
		sentiment.Score,
		actionStats["close"],
		actionStats["reduce"],
		actionStats["set_sl"],
		actionStats["set_tp"],
		actionStats["add"],
	)

	if highRiskCount > 0 {
		out.JournalReview.Weaknesses = append(out.JournalReview.Weaknesses, fmt.Sprintf("高风险仓位数量 %d，需要优先处理。", highRiskCount))
	}
	if profitCount > 0 {
		out.JournalReview.Strengths = append(out.JournalReview.Strengths, fmt.Sprintf("当前 %d 个仓位处于盈利，可考虑分批止盈。", profitCount))
	}
	if lossCount > 0 {
		out.JournalReview.Patterns = append(out.JournalReview.Patterns, fmt.Sprintf("当前 %d 个仓位为亏损状态，建议严格止损。", lossCount))
	}
	if len(out.JournalReview.Patterns) == 0 {
		out.JournalReview.Patterns = append(out.JournalReview.Patterns, "仓位整体较平衡，继续按策略跟踪。")
	}
	if len(out.JournalReview.Weaknesses) == 0 {
		out.JournalReview.Weaknesses = append(out.JournalReview.Weaknesses, "未发现明显结构性风险。")
	}
	if len(out.JournalReview.Strengths) == 0 {
		out.JournalReview.Strengths = append(out.JournalReview.Strengths, "风险暴露可控。")
	}

	return out
}

func staticRiskLevel(item api.PositionAnalysis) string {
	pnl := item.PnlPercent
	lev := item.Leverage
	switch {
	case lev >= 30 || pnl <= -20:
		return "critical"
	case lev >= 20 || pnl <= -10:
		return "high"
	case lev >= 10 || pnl <= -5:
		return "medium"
	default:
		return "low"
	}
}

func staticAssessment(item api.PositionAnalysis) string {
	side := "多头"
	if strings.EqualFold(strings.TrimSpace(item.Side), "SHORT") {
		side = "空头"
	}
	return fmt.Sprintf("%s仓位，当前浮盈亏 %.2f%%，杠杆 %dx。", side, item.PnlPercent, item.Leverage)
}

func staticSuggestion(item api.PositionAnalysis) string {
	if label := strings.TrimSpace(item.AdviceLabel); label != "" {
		return label
	}
	switch strings.ToLower(strings.TrimSpace(item.Advice)) {
	case "close":
		return "建议平仓"
	case "reduce":
		return "建议减仓"
	case "add":
		return "建议加仓"
	case "take_profit":
		return "建议止盈"
	case "stop_loss":
		return "建议止损"
	default:
		return "建议继续观察"
	}
}

func staticReasons(item api.PositionAnalysis) []string {
	out := make([]string, 0, len(item.Reasons)+2)
	seen := make(map[string]struct{})
	add := func(text string) {
		text = strings.TrimSpace(text)
		if text == "" {
			return
		}
		if _, ok := seen[text]; ok {
			return
		}
		seen[text] = struct{}{}
		out = append(out, text)
	}

	for _, reason := range item.Reasons {
		add(reason)
		if len(out) >= 4 {
			break
		}
	}

	add(fmt.Sprintf("杠杆 %dx，需关注波动放大风险。", item.Leverage))
	add(fmt.Sprintf("当前浮盈亏 %.2f%%。", item.PnlPercent))

	if len(out) == 0 {
		return []string{"策略信号不足，建议降低仓位等待确认。"}
	}
	return out
}

func staticSignalEval(item api.PositionAnalysis, riskLevel string) (SignalEval, bool) {
	direction := strings.ToUpper(strings.TrimSpace(item.Direction))
	if direction == "" {
		direction = strings.ToUpper(strings.TrimSpace(item.Side))
	}
	if direction == "" {
		return SignalEval{}, false
	}

	score := math.Max(0, math.Min(10, float64(item.Confidence)/10.0))
	comment := strings.TrimSpace(item.AdviceLabel)
	if comment == "" {
		comment = "信号强度一般，建议结合风险控制执行。"
	}

	return SignalEval{
		Symbol:    item.Symbol,
		Direction: direction,
		Score:     score,
		RiskLevel: riskLevel,
		Comment:   comment,
	}, true
}

func staticActionItem(item api.PositionAnalysis, riskLevel string) (ActionItem, bool) {
	action := strings.ToLower(strings.TrimSpace(item.Advice))
	symbol := strings.ToUpper(strings.TrimSpace(item.Symbol))
	if symbol == "" {
		return ActionItem{}, false
	}

	riskText := strings.TrimSpace(staticSuggestion(item))
	if riskText == "" {
		riskText = "请先确认仓位风险后再执行。"
	}

	switch action {
	case "close":
		return ActionItem{
			Action:   "close",
			Symbol:   symbol,
			Detail:   "平掉当前仓位，优先保护本金",
			Priority: "high",
			Risk:     riskText,
		}, true
	case "reduce":
		return ActionItem{
			Action:   "reduce",
			Symbol:   symbol,
			Detail:   "减仓 50%",
			Priority: "high",
			Risk:     riskText,
		}, true
	case "stop_loss":
		detail := "设置动态止损"
		if item.StopLoss > 0 {
			detail = fmt.Sprintf("止损价 %.6f", item.StopLoss)
		}
		return ActionItem{
			Action:   "set_sl",
			Symbol:   symbol,
			Detail:   detail,
			Priority: "high",
			Risk:     riskText,
		}, true
	case "take_profit":
		if item.TakeProfit > 0 {
			return ActionItem{
				Action:   "set_tp",
				Symbol:   symbol,
				Detail:   fmt.Sprintf("止盈价 %.6f", item.TakeProfit),
				Priority: "medium",
				Risk:     riskText,
			}, true
		}
		return ActionItem{
			Action:   "reduce",
			Symbol:   symbol,
			Detail:   "减仓 50% 锁定利润",
			Priority: "medium",
			Risk:     riskText,
		}, true
	case "add":
		sideText := "做多"
		if strings.EqualFold(strings.TrimSpace(item.Side), "SHORT") {
			sideText = "做空"
		}
		priority := "low"
		if riskLevel == "low" || riskLevel == "medium" {
			priority = "medium"
		}
		return ActionItem{
			Action:   "add",
			Symbol:   symbol,
			Detail:   fmt.Sprintf("%s 方向加仓 3u", sideText),
			Priority: priority,
			Risk:     riskText,
		}, true
	default:
		return ActionItem{}, false
	}
}

func actionPriorityScore(priority string) int {
	switch strings.ToLower(strings.TrimSpace(priority)) {
	case "high":
		return 3
	case "medium":
		return 2
	default:
		return 1
	}
}
