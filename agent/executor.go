package agent

import (
	"context"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"

	"tools/api"

	"github.com/adshao/go-binance/v2/futures"
)

var percentRe = regexp.MustCompile(`(\d+(?:\.\d+)?)\s*%`)
var leverageRe = regexp.MustCompile(`(\d+)\s*x`)
var usdtRe = regexp.MustCompile(`(\d+(?:\.\d+)?)\s*(?:u|usdt)\b`)
var numberRe = regexp.MustCompile(`(\d+(?:\.\d+)?)`)

const (
	defaultOpenUSDT = 5.0
	defaultOpenLev  = 5
	defaultAddUSDT  = 3.0
	defaultTPPct    = 0.01
	defaultSLPct    = 0.01
)

func executeActionItems(ctx context.Context, items []ActionItem) *ExecutionResult {
	res := &ExecutionResult{
		Requested: len(items),
		Results:   make([]ActionExecution, 0, len(items)),
	}

	for _, item := range items {
		action := strings.ToLower(strings.TrimSpace(item.Action))
		symbol := strings.ToUpper(strings.TrimSpace(item.Symbol))
		if symbol == "" || symbol == "N/A" {
			res.Skipped++
			res.Results = append(res.Results, ActionExecution{
				Action:  item.Action,
				Symbol:  item.Symbol,
				Status:  "skipped",
				Message: "invalid symbol",
			})
			continue
		}

		switch action {
		case "close":
			out := executeClose(ctx, symbol, item)
			res.Results = append(res.Results, out)
			countExecutionResult(res, out)
		case "reduce":
			out := executeReduce(ctx, symbol, item)
			res.Results = append(res.Results, out)
			countExecutionResult(res, out)
		case "open":
			out := executeOpen(ctx, symbol, item)
			res.Results = append(res.Results, out)
			countExecutionResult(res, out)
		case "add":
			out := executeAdd(ctx, symbol, item)
			res.Results = append(res.Results, out)
			countExecutionResult(res, out)
		case "set_sl":
			out := executeSetSL(ctx, symbol, item)
			res.Results = append(res.Results, out)
			countExecutionResult(res, out)
		case "set_tp":
			out := executeSetTP(ctx, symbol, item)
			res.Results = append(res.Results, out)
			countExecutionResult(res, out)
		case "wait":
			out := ActionExecution{
				Action:  item.Action,
				Symbol:  symbol,
				Status:  "skipped",
				Message: "wait action does not place orders",
			}
			res.Results = append(res.Results, out)
			countExecutionResult(res, out)
		default:
			out := ActionExecution{
				Action:  item.Action,
				Symbol:  symbol,
				Status:  "skipped",
				Message: "unsupported action, currently executable: open/add/close/reduce/set_sl/set_tp",
			}
			res.Results = append(res.Results, out)
			countExecutionResult(res, out)
		}
	}

	return res
}

func countExecutionResult(res *ExecutionResult, out ActionExecution) {
	switch out.Status {
	case "success":
		res.Executed++
		res.Success++
	case "failed":
		res.Executed++
		res.Failed++
	default:
		res.Skipped++
	}
}

func executeClose(ctx context.Context, symbol string, item ActionItem) ActionExecution {
	resp, err := api.ClosePositionViaWs(ctx, api.ClosePositionReq{
		Symbol: symbol,
	})
	if err != nil {
		api.SaveFailedOperation("AGENT_CLOSE", "agent", symbol, item, 0, err)
		return ActionExecution{
			Action:  item.Action,
			Symbol:  symbol,
			Status:  "failed",
			Message: err.Error(),
		}
	}

	var orderID int64
	if resp != nil {
		orderID = resp.OrderID
	}
	api.SaveSuccessOperation("AGENT_CLOSE", "agent", symbol, item, orderID)
	return ActionExecution{
		Action:  item.Action,
		Symbol:  symbol,
		Status:  "success",
		Message: "position closed",
		OrderID: orderID,
	}
}

func executeReduce(ctx context.Context, symbol string, item ActionItem) ActionExecution {
	percent := parseReducePercent(item.Detail)
	resp, err := api.ReducePositionViaWs(ctx, api.ReducePositionReq{
		Symbol:  symbol,
		Percent: percent,
	})
	if err != nil {
		api.SaveFailedOperation("AGENT_REDUCE", "agent", symbol, item, 0, err)
		return ActionExecution{
			Action:  item.Action,
			Symbol:  symbol,
			Status:  "failed",
			Message: err.Error(),
		}
	}

	var orderID int64
	if resp != nil {
		orderID = resp.OrderID
	}
	api.SaveSuccessOperation("AGENT_REDUCE", "agent", symbol, item, orderID)
	return ActionExecution{
		Action:  item.Action,
		Symbol:  symbol,
		Status:  "success",
		Message: fmt.Sprintf("position reduced by %.0f%%", percent),
		OrderID: orderID,
	}
}

func executeOpen(ctx context.Context, symbol string, item ActionItem) ActionExecution {
	side, posSide, ok := inferDirection(item.Detail)
	if !ok {
		return ActionExecution{
			Action:  item.Action,
			Symbol:  symbol,
			Status:  "failed",
			Message: "cannot infer direction for open action",
		}
	}

	quote := parseQuoteUSDT(item.Detail, defaultOpenUSDT)
	lev := parseLeverage(item.Detail, defaultOpenLev)

	resp, err := api.PlaceOrderViaWs(ctx, api.PlaceOrderReq{
		Source:        "agent",
		Symbol:        symbol,
		Side:          futures.SideType(side),
		OrderType:     "MARKET",
		PositionSide:  futures.PositionSideType(posSide),
		QuoteQuantity: formatDecimal(quote),
		Leverage:      lev,
	})
	if err != nil {
		api.SaveFailedOperation("AGENT_OPEN", "agent", symbol, item, 0, err)
		return ActionExecution{
			Action:  item.Action,
			Symbol:  symbol,
			Status:  "failed",
			Message: err.Error(),
		}
	}

	orderID := int64(0)
	if resp != nil && resp.Order != nil {
		orderID = resp.Order.OrderID
	}
	api.SaveSuccessOperation("AGENT_OPEN", "agent", symbol, item, orderID)
	return ActionExecution{
		Action:  item.Action,
		Symbol:  symbol,
		Status:  "success",
		Message: fmt.Sprintf("opened with %.2f USDT @ %dx", quote, lev),
		OrderID: orderID,
	}
}

func executeAdd(ctx context.Context, symbol string, item ActionItem) ActionExecution {
	pos, err := getSymbolPosition(ctx, symbol)
	if err != nil {
		return ActionExecution{
			Action:  item.Action,
			Symbol:  symbol,
			Status:  "failed",
			Message: err.Error(),
		}
	}

	side := "BUY"
	if pos.Amount < 0 {
		side = "SELL"
	}

	quote := parseQuoteUSDT(item.Detail, defaultAddUSDT)
	lev := pos.Leverage
	if lev <= 0 {
		lev = defaultOpenLev
	}

	resp, placeErr := api.PlaceOrderViaWs(ctx, api.PlaceOrderReq{
		Source:        "agent",
		Symbol:        symbol,
		Side:          futures.SideType(side),
		OrderType:     "MARKET",
		PositionSide:  futures.PositionSideType(pos.PositionSide),
		QuoteQuantity: formatDecimal(quote),
		Leverage:      lev,
	})
	if placeErr != nil {
		api.SaveFailedOperation("AGENT_ADD", "agent", symbol, item, 0, placeErr)
		return ActionExecution{
			Action:  item.Action,
			Symbol:  symbol,
			Status:  "failed",
			Message: placeErr.Error(),
		}
	}

	orderID := int64(0)
	if resp != nil && resp.Order != nil {
		orderID = resp.Order.OrderID
	}
	api.SaveSuccessOperation("AGENT_ADD", "agent", symbol, item, orderID)
	return ActionExecution{
		Action:  item.Action,
		Symbol:  symbol,
		Status:  "success",
		Message: fmt.Sprintf("added %.2f USDT @ %dx", quote, lev),
		OrderID: orderID,
	}
}

func executeSetSL(ctx context.Context, symbol string, item ActionItem) ActionExecution {
	pos, err := getSymbolPosition(ctx, symbol)
	if err != nil {
		return ActionExecution{
			Action:  item.Action,
			Symbol:  symbol,
			Status:  "failed",
			Message: err.Error(),
		}
	}

	triggerPrice := parseTriggerPrice(item.Detail)
	if triggerPrice <= 0 {
		if pos.Amount > 0 {
			triggerPrice = pos.MarkPrice * (1 - defaultSLPct)
		} else {
			triggerPrice = pos.MarkPrice * (1 + defaultSLPct)
		}
	}

	closeSide := "SELL"
	if pos.Amount < 0 {
		closeSide = "BUY"
	}

	algoResp, placeErr := api.PlaceAlgoOrder(ctx, api.AlgoOrderParams{
		Symbol:       symbol,
		Side:         closeSide,
		OrderType:    "STOP_MARKET",
		TriggerPrice: formatDecimal(triggerPrice),
		Quantity:     pos.Quantity,
		PositionSide: string(pos.PositionSide),
		WorkingType:  "MARK_PRICE",
		PriceProtect: true,
	})
	if placeErr != nil {
		api.SaveFailedOperation("AGENT_SET_SL", "agent", symbol, item, 0, placeErr)
		return ActionExecution{
			Action:  item.Action,
			Symbol:  symbol,
			Status:  "failed",
			Message: placeErr.Error(),
		}
	}

	api.SaveSuccessOperation("AGENT_SET_SL", "agent", symbol, item, algoResp.AlgoID)
	return ActionExecution{
		Action:  item.Action,
		Symbol:  symbol,
		Status:  "success",
		Message: fmt.Sprintf("stop-loss set at %s", algoResp.TriggerPrice),
		OrderID: algoResp.AlgoID,
	}
}

func executeSetTP(ctx context.Context, symbol string, item ActionItem) ActionExecution {
	pos, err := getSymbolPosition(ctx, symbol)
	if err != nil {
		return ActionExecution{
			Action:  item.Action,
			Symbol:  symbol,
			Status:  "failed",
			Message: err.Error(),
		}
	}

	triggerPrice := parseTriggerPrice(item.Detail)
	if triggerPrice <= 0 {
		if pos.Amount > 0 {
			triggerPrice = pos.MarkPrice * (1 + defaultTPPct)
		} else {
			triggerPrice = pos.MarkPrice * (1 - defaultTPPct)
		}
	}

	closeSide := "SELL"
	if pos.Amount < 0 {
		closeSide = "BUY"
	}

	algoResp, placeErr := api.PlaceAlgoOrder(ctx, api.AlgoOrderParams{
		Symbol:       symbol,
		Side:         closeSide,
		OrderType:    "TAKE_PROFIT_MARKET",
		TriggerPrice: formatDecimal(triggerPrice),
		Quantity:     pos.Quantity,
		PositionSide: string(pos.PositionSide),
		WorkingType:  "MARK_PRICE",
		PriceProtect: true,
	})
	if placeErr != nil {
		api.SaveFailedOperation("AGENT_SET_TP", "agent", symbol, item, 0, placeErr)
		return ActionExecution{
			Action:  item.Action,
			Symbol:  symbol,
			Status:  "failed",
			Message: placeErr.Error(),
		}
	}

	api.SaveSuccessOperation("AGENT_SET_TP", "agent", symbol, item, algoResp.AlgoID)
	return ActionExecution{
		Action:  item.Action,
		Symbol:  symbol,
		Status:  "success",
		Message: fmt.Sprintf("take-profit set at %s", algoResp.TriggerPrice),
		OrderID: algoResp.AlgoID,
	}
}

func parseReducePercent(detail string) float64 {
	matches := percentRe.FindStringSubmatch(detail)
	if len(matches) >= 2 {
		if v, err := strconv.ParseFloat(matches[1], 64); err == nil {
			if v > 0 && v <= 100 {
				return v
			}
		}
	}
	return 50
}

func parseLeverage(detail string, fallback int) int {
	matches := leverageRe.FindStringSubmatch(strings.ToLower(detail))
	if len(matches) >= 2 {
		if v, err := strconv.Atoi(matches[1]); err == nil && v > 0 {
			return v
		}
	}
	return fallback
}

func parseQuoteUSDT(detail string, fallback float64) float64 {
	matches := usdtRe.FindStringSubmatch(strings.ToLower(detail))
	if len(matches) >= 2 {
		if v, err := strconv.ParseFloat(matches[1], 64); err == nil && v > 0 {
			return v
		}
	}
	return fallback
}

func parseTriggerPrice(detail string) float64 {
	// 过滤百分数，仅提取绝对价格。
	lower := strings.ToLower(detail)
	for _, m := range numberRe.FindAllStringSubmatchIndex(lower, -1) {
		if len(m) < 4 {
			continue
		}
		start := m[2]
		end := m[3]
		if end < len(lower) && lower[end] == '%' {
			continue
		}
		v, err := strconv.ParseFloat(lower[start:end], 64)
		if err != nil {
			continue
		}
		if v > 1 {
			return v
		}
	}
	return 0
}

func formatDecimal(v float64) string {
	s := strconv.FormatFloat(v, 'f', 8, 64)
	s = strings.TrimRight(s, "0")
	s = strings.TrimRight(s, ".")
	if s == "" {
		return "0"
	}
	return s
}

func inferDirection(detail string) (side, posSide string, ok bool) {
	t := strings.ToLower(detail)
	if strings.Contains(t, "long") || strings.Contains(t, "buy") || strings.Contains(t, "做多") || strings.Contains(t, "看多") || strings.Contains(t, "多") {
		return "BUY", "LONG", true
	}
	if strings.Contains(t, "short") || strings.Contains(t, "sell") || strings.Contains(t, "做空") || strings.Contains(t, "看空") || strings.Contains(t, "空") {
		return "SELL", "SHORT", true
	}
	return "", "", false
}

type symbolPosition struct {
	Symbol       string
	PositionSide string
	Amount       float64
	Leverage     int
	Quantity     string
	MarkPrice    float64
}

func getSymbolPosition(ctx context.Context, symbol string) (*symbolPosition, error) {
	positions, err := api.GetPositionsViaWs(ctx)
	if err != nil {
		return nil, fmt.Errorf("get positions failed: %w", err)
	}

	for _, p := range positions {
		if strings.ToUpper(strings.TrimSpace(p.Symbol)) != symbol {
			continue
		}
		amt, _ := strconv.ParseFloat(strings.TrimSpace(p.PositionAmt), 64)
		if amt == 0 {
			continue
		}
		lev, _ := strconv.Atoi(strings.TrimSpace(p.Leverage))
		mark, _ := strconv.ParseFloat(strings.TrimSpace(p.MarkPrice), 64)

		qty := math.Abs(amt)
		return &symbolPosition{
			Symbol:       symbol,
			PositionSide: normalizePositionSide(p.PositionSide, amt),
			Amount:       amt,
			Leverage:     lev,
			Quantity:     formatDecimal(qty),
			MarkPrice:    mark,
		}, nil
	}

	return nil, fmt.Errorf("no open position found for %s", symbol)
}

func normalizePositionSide(raw string, amt float64) string {
	ps := strings.ToUpper(strings.TrimSpace(raw))
	switch ps {
	case "LONG", "SHORT", "BOTH":
		return ps
	default:
		if amt < 0 {
			return "SHORT"
		}
		return "LONG"
	}
}
