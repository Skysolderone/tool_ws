package api

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/utils"
)

const (
	monitorOverviewDefaultDays = 30
	monitorOverviewMaxDays     = 366
)

// MonitorOverview 监控总览聚合指标
type MonitorOverview struct {
	NetValue         float64  `json:"netValue"`         // 净值（钱包余额 + 未实现盈亏）
	DayPnl           float64  `json:"dayPnl"`           // 当日已实现盈亏
	Drawdown         float64  `json:"drawdown"`         // 回撤金额（窗口期）
	DrawdownPct      float64  `json:"drawdownPct"`      // 回撤百分比（窗口期）
	MarginRatio      float64  `json:"marginRatio"`      // 保证金使用率（总名义/净值）
	Var95            float64  `json:"var95"`            // VaR95
	SlippageP95Bps   float64  `json:"slippageP95Bps"`   // 滑点 P95（bps）
	RejectRate       float64  `json:"rejectRate"`       // 拒单率（失败/总下单）
	RiskTriggerCount int64    `json:"riskTriggerCount"` // 风控触发总数（含 kill-switch）
	WindowDays       int      `json:"windowDays"`
	UpdatedAt        int64    `json:"updatedAt"`
	Warnings         []string `json:"warnings,omitempty"`
}

// HandleGetMonitorOverview GET /tool/monitor/overview?days=30
func HandleGetMonitorOverview(c context.Context, ctx *app.RequestContext) {
	days := normalizeMonitorOverviewDays(ctx.DefaultQuery("days", ""))
	ctx.JSON(http.StatusOK, utils.H{"data": buildMonitorOverview(c, days)})
}

func normalizeMonitorOverviewDays(raw string) int {
	if strings.TrimSpace(raw) == "" {
		return monitorOverviewDefaultDays
	}
	days, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || days <= 0 {
		return monitorOverviewDefaultDays
	}
	if days > monitorOverviewMaxDays {
		return monitorOverviewMaxDays
	}
	return days
}

func buildMonitorOverview(ctx context.Context, days int) *MonitorOverview {
	now := time.Now().UTC()
	out := &MonitorOverview{
		WindowDays: days,
		UpdatedAt:  now.UnixMilli(),
	}
	warnings := make([]string, 0, 3)

	// 1) 净值
	balance, err := GetBalance(ctx)
	if err != nil {
		warnings = append(warnings, "balance:"+err.Error())
	} else {
		wallet := parseStringFloat(balance["crossWalletBalance"])
		if wallet == 0 {
			wallet = parseStringFloat(balance["balance"])
		}
		unPnl := parseStringFloat(balance["crossUnPnl"])
		out.NetValue = roundFloat(wallet+unPnl, 2)
	}

	// 2) 当日 PnL（风控模块维护）
	out.DayPnl = roundFloat(readMapFloat(GetRiskStatus(), "dailyPnl"), 2)

	// 3) 回撤（默认近 30 天）
	drawdown, drawdownPct, drawdownErr := calcOverviewDrawdown(days, now)
	if drawdownErr != nil {
		warnings = append(warnings, "drawdown:"+drawdownErr.Error())
	} else {
		out.Drawdown = drawdown
		out.DrawdownPct = drawdownPct
	}

	// 4) 保证金率（组合总名义 / 净值）
	totalNotional := readMapFloat(GetPortfolioStatus(), "totalNotional")
	if out.NetValue > 0 {
		out.MarginRatio = roundFloat(totalNotional/out.NetValue*100, 2)
	}

	// 5) VaR95
	out.Var95 = roundFloat(GetVarStatus().TotalVar95, 2)

	// 6) 滑点 P95（全币种）
	p95Bps, slipErr := getOverviewSlippageP95(days, now)
	if slipErr != nil {
		warnings = append(warnings, "slippage:"+slipErr.Error())
	} else {
		out.SlippageP95Bps = roundFloat(p95Bps, 4)
	}

	// 7) 拒单率 + 8) 风控触发
	ops := GetOpsMetrics()
	if ops.OrderTotal > 0 {
		out.RejectRate = roundFloat(float64(ops.OrderFailed)/float64(ops.OrderTotal)*100, 2)
	}
	out.RiskTriggerCount = ops.RiskTriggerCount + ops.KillSwitchCount

	if len(warnings) > 0 {
		out.Warnings = warnings
	}
	return out
}

func calcOverviewDrawdown(days int, now time.Time) (float64, float64, error) {
	from := now.Add(-time.Duration(days) * 24 * time.Hour)
	records, err := loadTradesForAnalytics(from, now)
	if err != nil {
		return 0, 0, err
	}

	filtered := make([]TradeRecord, 0, len(records))
	for _, r := range records {
		if includeTradeForAnalytics(r) {
			filtered = append(filtered, r)
		}
	}

	m := calcJournalMetrics(filtered, now)
	return roundFloat(m.MaxDrawdown, 2), roundFloat(m.MaxDrawdownPct*100, 2), nil
}

func getOverviewSlippageP95(days int, now time.Time) (float64, error) {
	if DB == nil {
		return 0, nil
	}
	from := now.Add(-time.Duration(days) * 24 * time.Hour)
	var records []SlippageRecord
	if err := DB.
		Select("slippage_bps").
		Where("created_at >= ?", from).
		Order("created_at DESC").
		Limit(2000).
		Find(&records).Error; err != nil {
		return 0, fmt.Errorf("query slippage records: %w", err)
	}
	if len(records) == 0 {
		return 0, nil
	}

	bps := make([]float64, 0, len(records))
	for _, r := range records {
		bps = append(bps, r.SlippageBps)
	}
	sort.Float64s(bps)
	return percentile(bps, 95), nil
}

func parseStringFloat(raw string) float64 {
	v, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil {
		return 0
	}
	return v
}

func readMapFloat(m map[string]interface{}, key string) float64 {
	if m == nil {
		return 0
	}
	v, ok := m[key]
	if !ok || v == nil {
		return 0
	}
	switch vv := v.(type) {
	case float64:
		return vv
	case float32:
		return float64(vv)
	case int:
		return float64(vv)
	case int32:
		return float64(vv)
	case int64:
		return float64(vv)
	case uint:
		return float64(vv)
	case uint32:
		return float64(vv)
	case uint64:
		return float64(vv)
	case string:
		return parseStringFloat(vv)
	default:
		return 0
	}
}
