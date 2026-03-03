package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"tools/api"

	"github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/schema"
	"github.com/cloudwego/hertz/pkg/app"
	hertzutils "github.com/cloudwego/hertz/pkg/common/utils"
)

var chatModel *openai.ChatModel
var chatModelName string

const analyzeOverallTimeout = 55 * time.Second

// InitAgent 初始化 Eino Agent（在 main.go 中调用）。
func InitAgent(cfg api.LLMConfig) error {
	if strings.TrimSpace(cfg.APIKey) == "" {
		log.Printf("[Agent] LLM not configured, agent disabled")
		return nil
	}

	if strings.TrimSpace(cfg.Model) == "" {
		cfg.Model = "deepseek-chat"
	}
	if cfg.MaxTokens <= 0 {
		cfg.MaxTokens = 4096
	}
	if cfg.Temperature < 0 {
		cfg.Temperature = 0
	}
	temperature := float32(cfg.Temperature)

	model, err := openai.NewChatModel(context.Background(), &openai.ChatModelConfig{
		BaseURL:     strings.TrimSpace(cfg.BaseURL),
		APIKey:      strings.TrimSpace(cfg.APIKey),
		Model:       strings.TrimSpace(cfg.Model),
		MaxTokens:   &cfg.MaxTokens,
		Temperature: &temperature,
	})
	if err != nil {
		return err
	}

	chatModel = model
	chatModelName = strings.TrimSpace(cfg.Model)
	log.Printf("[Agent] Initialized with provider=%s model=%s", cfg.Provider, cfg.Model)
	return nil
}

// RunAnalysis 执行分析。
func RunAnalysis(ctx context.Context, req AnalysisRequest) (*AnalysisOutput, error) {
	if chatModel == nil {
		return nil, errors.New("agent llm not configured")
	}
	analysisStart := time.Now()
	log.Printf("[Agent] RunAnalysis start mode=%s symbols=%v", strings.TrimSpace(req.Mode), req.Symbols)

	collectStart := time.Now()
	dataJSON, err := collectData(ctx, req)
	if err != nil {
		log.Printf("[Agent] RunAnalysis collectData failed after=%v err=%v", time.Since(collectStart).Round(time.Millisecond), err)
		return nil, err
	}
	log.Printf("[Agent] RunAnalysis collectData done after=%v payload_bytes=%d", time.Since(collectStart).Round(time.Millisecond), len(dataJSON))

	userMsg := "以下是当前交易数据，请分析并给出建议（严格返回 JSON）：\n" + dataJSON
	messages := []*schema.Message{
		schema.SystemMessage(systemPrompt),
		schema.UserMessage(userMsg),
	}

	timeout := 60 * time.Second
	if strings.Contains(strings.ToLower(chatModelName), "reasoner") {
		// Cloudflare 回源请求常见等待上限约 100s，reasoner 需要预留收集/解析耗时。
		timeout = 95 * time.Second
	}
	llmCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	llmStart := time.Now()
	log.Printf("[Agent] RunAnalysis llm start model=%s timeout=%v", chatModelName, timeout)
	resp, err := chatModel.Generate(llmCtx, messages)
	if err != nil {
		log.Printf("[Agent] RunAnalysis llm failed after=%v err=%v", time.Since(llmStart).Round(time.Millisecond), err)
		return nil, err
	}
	log.Printf("[Agent] RunAnalysis llm done after=%v content_bytes=%d", time.Since(llmStart).Round(time.Millisecond), len(resp.Content))

	raw := strings.TrimSpace(resp.Content)
	var output AnalysisOutput
	if unmarshalAnalysisOutput(raw, &output) == nil {
		hydratePositionAnalysisFromCollectedData(&output, dataJSON)
		log.Printf("[Agent] RunAnalysis parse=json total=%v actions=%d", time.Since(analysisStart).Round(time.Millisecond), len(output.ActionItems))
		return &output, nil
	}

	output.Summary = raw
	log.Printf("[Agent] RunAnalysis parse=fallback_text total=%v", time.Since(analysisStart).Round(time.Millisecond))
	return &output, nil
}

func unmarshalAnalysisOutput(raw string, out *AnalysisOutput) error {
	if err := json.Unmarshal([]byte(raw), out); err == nil {
		return nil
	}

	trimmed := strings.TrimSpace(raw)
	if strings.HasPrefix(trimmed, "```") {
		trimmed = strings.TrimPrefix(trimmed, "```json")
		trimmed = strings.TrimPrefix(trimmed, "```")
		trimmed = strings.TrimSuffix(trimmed, "```")
		trimmed = strings.TrimSpace(trimmed)
	}
	return json.Unmarshal([]byte(trimmed), out)
}

func hydratePositionAnalysisFromCollectedData(out *AnalysisOutput, dataJSON string) {
	if out == nil || strings.TrimSpace(dataJSON) == "" {
		return
	}

	var payload struct {
		Positions struct {
			Items []api.PositionAnalysis `json:"items"`
		} `json:"positions"`
	}
	if err := json.Unmarshal([]byte(dataJSON), &payload); err != nil {
		return
	}
	if len(payload.Positions.Items) == 0 {
		return
	}

	bySymbol := make(map[string]api.PositionAnalysis, len(payload.Positions.Items))
	for _, it := range payload.Positions.Items {
		symbol := strings.ToUpper(strings.TrimSpace(it.Symbol))
		if symbol == "" {
			continue
		}
		bySymbol[symbol] = it
	}

	if len(out.PositionAnalysis) == 0 {
		out.PositionAnalysis = make([]PositionAdvice, 0, len(payload.Positions.Items))
		for _, it := range payload.Positions.Items {
			out.PositionAnalysis = append(out.PositionAnalysis, PositionAdvice{
				Symbol:     it.Symbol,
				Assessment: buildFallbackAssessment(it),
				Risk:       fallbackRisk(it),
				Suggestion: chooseText(strings.TrimSpace(it.AdviceLabel), "继续观察并控制风险"),
				Reasons:    fallbackReasons(it),
			})
		}
		return
	}

	for i := range out.PositionAnalysis {
		symbol := strings.ToUpper(strings.TrimSpace(out.PositionAnalysis[i].Symbol))
		if symbol == "" {
			continue
		}
		src, ok := bySymbol[symbol]
		if !ok {
			continue
		}
		if strings.TrimSpace(out.PositionAnalysis[i].Assessment) == "" {
			out.PositionAnalysis[i].Assessment = buildFallbackAssessment(src)
		}
		if strings.TrimSpace(out.PositionAnalysis[i].Suggestion) == "" {
			out.PositionAnalysis[i].Suggestion = chooseText(strings.TrimSpace(src.AdviceLabel), "继续观察并控制风险")
		}
		if strings.TrimSpace(out.PositionAnalysis[i].Risk) == "" {
			out.PositionAnalysis[i].Risk = fallbackRisk(src)
		}
		if len(out.PositionAnalysis[i].Reasons) == 0 {
			out.PositionAnalysis[i].Reasons = fallbackReasons(src)
		}
	}
}

func fallbackReasons(it api.PositionAnalysis) []string {
	if len(it.Reasons) > 0 {
		cp := append([]string(nil), it.Reasons...)
		if len(cp) > 4 {
			cp = cp[:4]
		}
		return cp
	}
	reasons := []string{}
	if it.Direction != "" && it.Confidence > 0 {
		reasons = append(reasons, fmt.Sprintf("AI方向 %s，置信度 %d%%", it.Direction, it.Confidence))
	}
	if it.AdviceLabel != "" {
		reasons = append(reasons, it.AdviceLabel)
	}
	if len(reasons) == 0 {
		reasons = append(reasons, "暂无充足信号，建议控制仓位风险")
	}
	return reasons
}

func fallbackRisk(it api.PositionAnalysis) string {
	switch {
	case it.PnlPercent <= -20:
		return "critical"
	case it.Leverage >= 20 || it.PnlPercent <= -10:
		return "high"
	case it.Leverage >= 10 || it.PnlPercent <= -5:
		return "medium"
	default:
		return "low"
	}
}

func buildFallbackAssessment(it api.PositionAnalysis) string {
	return fmt.Sprintf("%s 仓位，浮盈亏 %.2f USDT（%.2f%%），杠杆 %dx。", chooseText(strings.TrimSpace(it.Side), "UNKNOWN"), it.UnrealizedPnl, it.PnlPercent, it.Leverage)
}

func chooseText(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func collectData(ctx context.Context, req AnalysisRequest) (string, error) {
	mode := strings.ToLower(strings.TrimSpace(req.Mode))
	if mode == "" {
		mode = "full"
	}

	data := map[string]any{}
	var warnings []string
	symbols := req.Symbols

	switch mode {
	case "full", "positions", "signals", "journal", "sentiment":
	default:
		return "", errors.New("invalid mode, should be one of: full|positions|signals|journal|sentiment")
	}

	if mode == "full" || mode == "positions" {
		positions, err := collectPositionsData(ctx, symbols)
		if err != nil {
			warnings = append(warnings, "持仓数据获取失败: "+err.Error())
		} else if positions != nil {
			data["positions"] = positions
		}
	}

	if mode == "full" || mode == "signals" {
		if signals := collectSignalsData(symbols); signals != nil {
			data["signals"] = signals
		} else {
			warnings = append(warnings, "推荐信号缓存为空（可能引擎尚未就绪）")
		}
	}

	if mode == "full" || mode == "journal" {
		journal, err := collectJournalData(30)
		if err != nil {
			warnings = append(warnings, "交易日志获取失败: "+err.Error())
		} else if journal != nil {
			data["journal"] = journal
		}
	}

	if mode == "full" || mode == "sentiment" {
		data["sentiment"] = collectSentimentData()
	}

	// 加入账户余额
	if mode == "full" || mode == "positions" {
		if balance, err := collectBalanceData(ctx); err != nil {
			warnings = append(warnings, "账户余额获取失败: "+err.Error())
		} else if balance != nil {
			data["balance"] = balance
		}
	}

	if len(warnings) > 0 {
		data["_warnings"] = warnings
	}

	b, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// HandleAnalyze POST/GET /tool/agent/analyze。
func HandleAnalyze(c context.Context, ctx *app.RequestContext) {
	start := time.Now()
	reqID := strconv.FormatInt(time.Now().UnixNano(), 36)
	var req AnalysisRequest
	if string(ctx.Method()) == http.MethodPost {
		if err := ctx.BindJSON(&req); err != nil {
			log.Printf("[Agent][%s] analyze bad_request err=%v", reqID, err)
			ctx.JSON(http.StatusBadRequest, hertzutils.H{"error": "请求体 JSON 无效"})
			return
		}
	} else {
		req.Mode = ctx.DefaultQuery("mode", "full")
		req.Symbols = parseSymbolsQuery(ctx.DefaultQuery("symbols", ""))
		execRaw := strings.TrimSpace(ctx.DefaultQuery("execute", "false"))
		req.Execute = execRaw == "1" || strings.EqualFold(execRaw, "true")
		mockRaw := strings.TrimSpace(ctx.DefaultQuery("mock", "false"))
		req.Mock = mockRaw == "1" || strings.EqualFold(mockRaw, "true")
	}

	if req.Mode == "" {
		req.Mode = "full"
	}
	req.Symbols = normalizeAndDedupeSymbols(req.Symbols)
	log.Printf("[Agent][%s] analyze start method=%s mode=%s symbols=%v execute=%v mock=%v", reqID, string(ctx.Method()), req.Mode, req.Symbols, req.Execute, req.Mock)
	if !req.Mock && chatModel == nil {
		log.Printf("[Agent][%s] analyze llm_not_configured", reqID)
		ctx.JSON(http.StatusServiceUnavailable, hertzutils.H{"error": "Agent 未配置 LLM"})
		return
	}

	var (
		result *AnalysisOutput
		err    error
	)
	if req.Mock {
		log.Printf("[Agent][%s] analyze using_mock_response", reqID)
		result = buildMockAnalysisOutput(req)
	} else {
		runCtx, cancel := context.WithTimeout(c, analyzeOverallTimeout)
		defer cancel()
		if shouldSplitAnalyzeBySymbol(req) {
			log.Printf("[Agent][%s] analyze split_by_symbol symbols=%v", reqID, req.Symbols)
			result, err = runAnalysisBySymbol(runCtx, req)
		} else {
			result, err = RunAnalysis(runCtx, req)
		}
		if err != nil {
			if isLLMTimeoutError(err) {
				fallback := buildTimeoutFallbackOutput(req, err)
				saveAnalyzeLog(req, fallback, nil, err, time.Since(start))
				log.Printf("[Agent][%s] analyze timeout after=%v err=%v (returned fallback)", reqID, time.Since(start).Round(time.Millisecond), err)
				ctx.JSON(http.StatusOK, hertzutils.H{
					"data":    fallback,
					"warning": "agent analyze timeout, returned fallback result",
				})
				return
			}
			saveAnalyzeLog(req, nil, nil, err, time.Since(start))
			log.Printf("[Agent][%s] analyze failed after=%v err=%v", reqID, time.Since(start).Round(time.Millisecond), err)
			ctx.JSON(http.StatusInternalServerError, hertzutils.H{"error": err.Error()})
			return
		}
	}

	if req.Execute {
		items := req.ActionItems
		if len(items) == 0 {
			items = result.ActionItems
		}
		execStart := time.Now()
		log.Printf("[Agent][%s] execute start items=%d", reqID, len(items))
		result.Execution = executeActionItems(c, items)
		if result.Execution != nil {
			log.Printf("[Agent][%s] execute done after=%v requested=%d success=%d failed=%d skipped=%d",
				reqID,
				time.Since(execStart).Round(time.Millisecond),
				result.Execution.Requested,
				result.Execution.Success,
				result.Execution.Failed,
				result.Execution.Skipped,
			)
		} else {
			log.Printf("[Agent][%s] execute done after=%v result=nil", reqID, time.Since(execStart).Round(time.Millisecond))
		}
	}

	saveAnalyzeLog(req, result, result.Execution, nil, time.Since(start))
	log.Printf("[Agent][%s] analyze done after=%v action_items=%d", reqID, time.Since(start).Round(time.Millisecond), len(result.ActionItems))
	ctx.JSON(http.StatusOK, hertzutils.H{"data": result})
}

// HandleExecute POST /tool/agent/execute。
// 仅执行前端传入的 action_items，不触发 LLM 分析。
func HandleExecute(c context.Context, ctx *app.RequestContext) {
	var req AnalysisRequest
	if err := ctx.BindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, hertzutils.H{"error": "请求体 JSON 无效"})
		return
	}
	if len(req.ActionItems) == 0 {
		ctx.JSON(http.StatusBadRequest, hertzutils.H{"error": "action_items 不能为空"})
		return
	}

	result := executeActionItems(c, req.ActionItems)
	ctx.JSON(http.StatusOK, hertzutils.H{"data": result})
}

func parseSymbolsQuery(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	res := make([]string, 0, len(parts))
	for _, part := range parts {
		s := strings.ToUpper(strings.TrimSpace(part))
		if s == "" {
			continue
		}
		res = append(res, s)
	}
	return normalizeAndDedupeSymbols(res)
}

func normalizeAndDedupeSymbols(symbols []string) []string {
	if len(symbols) == 0 {
		return nil
	}
	res := make([]string, 0, len(symbols))
	seen := make(map[string]struct{}, len(symbols))
	for _, item := range symbols {
		s := strings.ToUpper(strings.TrimSpace(item))
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		res = append(res, s)
	}
	if len(res) == 0 {
		return nil
	}
	return res
}

func shouldSplitAnalyzeBySymbol(req AnalysisRequest) bool {
	mode := strings.ToLower(strings.TrimSpace(req.Mode))
	return mode == "positions" && len(req.Symbols) > 1
}

func runAnalysisBySymbol(ctx context.Context, req AnalysisRequest) (*AnalysisOutput, error) {
	merged := &AnalysisOutput{
		PositionAnalysis: make([]PositionAdvice, 0, len(req.Symbols)),
		SignalEvaluation: make([]SignalEval, 0, len(req.Symbols)),
		ActionItems:      make([]ActionItem, 0, len(req.Symbols)*2),
	}
	summaryParts := make([]string, 0, len(req.Symbols))

	for _, symbol := range req.Symbols {
		subReq := req
		subReq.Symbols = []string{symbol}

		out, err := RunAnalysis(ctx, subReq)
		if err != nil {
			return nil, fmt.Errorf("symbol %s: %w", symbol, err)
		}
		if out == nil {
			continue
		}

		if summary := strings.TrimSpace(out.Summary); summary != "" {
			summaryParts = append(summaryParts, fmt.Sprintf("[%s] %s", symbol, summary))
		}
		merged.PositionAnalysis = append(merged.PositionAnalysis, out.PositionAnalysis...)
		merged.SignalEvaluation = append(merged.SignalEvaluation, out.SignalEvaluation...)
		merged.ActionItems = append(merged.ActionItems, out.ActionItems...)
	}

	if len(summaryParts) > 0 {
		merged.Summary = strings.Join(summaryParts, "\n\n")
	}
	return merged, nil
}

func buildMockAnalysisOutput(req AnalysisRequest) *AnalysisOutput {
	symbol := "BTCUSDT"
	if len(req.Symbols) > 0 && strings.TrimSpace(req.Symbols[0]) != "" {
		symbol = strings.ToUpper(strings.TrimSpace(req.Symbols[0]))
	}
	return &AnalysisOutput{
		Summary: "mock: " + symbol + " 短周期震荡偏多，建议先控风险再观察延续。",
		PositionAnalysis: []PositionAdvice{
			{
				Symbol:     symbol,
				Assessment: "杠杆风险中等，浮盈浮亏切换较快。",
				Risk:       "medium",
				Suggestion: "优先设置止损，避免情绪化追单。",
				Reasons: []string{
					"短周期波动加大，回撤风险提升",
					"当前仓位杠杆较高，容错率有限",
				},
			},
		},
		SignalEvaluation: []SignalEval{
			{
				Symbol:    symbol,
				Direction: "LONG",
				Score:     6.8,
				RiskLevel: "medium",
				Comment:   "1H 结构偏多，但量能一般，追高性价比有限。",
			},
		},
		JournalReview: JournalInsight{
			Patterns:   []string{"盈利单平均持仓时间短于亏损单"},
			Weaknesses: []string{"止损执行不稳定"},
			Strengths:  []string{"顺势单胜率较高"},
			Suggestion: "固定单笔风险上限，并在开仓时同步挂止损。",
		},
		ActionItems: []ActionItem{
			{
				Action:   "set_sl",
				Symbol:   symbol,
				Detail:   "为现有仓位设置止损价 86200",
				Priority: "high",
				Risk:     "若不设止损，波动放大会快速扩大亏损。",
			},
			{
				Action:   "wait",
				Symbol:   symbol,
				Detail:   "等待 1H 回踩确认后再考虑加仓",
				Priority: "medium",
				Risk:     "提前追多可能在震荡区被反向扫损。",
			},
		},
	}
}

func buildTimeoutFallbackOutput(req AnalysisRequest, cause error) *AnalysisOutput {
	symbol := "BTCUSDT"
	if len(req.Symbols) > 0 && strings.TrimSpace(req.Symbols[0]) != "" {
		symbol = strings.ToUpper(strings.TrimSpace(req.Symbols[0]))
	}
	msg := "本次 AI 分析超时，已返回降级结果。建议切换到响应更快的模型（如 deepseek-chat）或缩小分析范围后重试。"
	if cause != nil {
		msg += " 超时原因: " + cause.Error()
	}
	return &AnalysisOutput{
		Summary: msg,
		PositionAnalysis: []PositionAdvice{
			{
				Symbol:     symbol,
				Assessment: "模型未在限定时间内完成推理，暂无完整仓位评估。",
				Risk:       "medium",
				Suggestion: "保持当前仓位，先确认止损，再稍后重试分析。",
				Reasons: []string{
					"请求执行时间超过服务端安全阈值",
					"长链路分析可能触发网关超时",
				},
			},
		},
		JournalReview: JournalInsight{
			Patterns:   []string{"本次分析任务未完成"},
			Weaknesses: []string{"LLM 响应耗时过长"},
			Strengths:  []string{"系统已返回可展示降级结果"},
			Suggestion: "优先改用非 reasoner 模型并减少输入数据。",
		},
		ActionItems: []ActionItem{
			{
				Action:   "wait",
				Symbol:   symbol,
				Detail:   "暂不执行交易动作，60 秒后重试分析",
				Priority: "high",
				Risk:     "超时情况下执行不完整建议可能导致误判。",
			},
		},
	}
}

// HandleLogs GET /tool/agent/logs?limit=50&status=SUCCESS|FAILED&execute=true|false
func HandleLogs(c context.Context, ctx *app.RequestContext) {
	limit, _ := strconv.Atoi(strings.TrimSpace(ctx.DefaultQuery("limit", "50")))
	status := strings.ToUpper(strings.TrimSpace(ctx.DefaultQuery("status", "")))

	var executePtr *bool
	exeRaw := strings.TrimSpace(ctx.DefaultQuery("execute", ""))
	if exeRaw != "" {
		v := exeRaw == "1" || strings.EqualFold(exeRaw, "true")
		if exeRaw == "0" || strings.EqualFold(exeRaw, "false") || v {
			executePtr = &v
		}
	}

	records, err := api.GetAgentAnalysisLogs(limit, status, executePtr)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, hertzutils.H{"error": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, hertzutils.H{"data": records})
}

// HandlePolicy GET /tool/agent/policy
func HandlePolicy(c context.Context, ctx *app.RequestContext) {
	_ = c
	ctx.JSON(http.StatusOK, hertzutils.H{"data": api.ResolveAgentExecutionPolicy()})
}

func saveAnalyzeLog(req AnalysisRequest, output *AnalysisOutput, exec *ExecutionResult, runErr error, duration time.Duration) {
	reqJSON := marshalToString(req)
	respJSON := marshalToString(output)
	execJSON := marshalToString(exec)

	mode := strings.TrimSpace(req.Mode)
	if mode == "" {
		mode = "full"
	}
	status := "SUCCESS"
	errMsg := ""
	if runErr != nil {
		status = "FAILED"
		errMsg = runErr.Error()
	}

	_ = api.SaveAgentAnalysisLog(&api.AgentAnalysisLog{
		Mode:          mode,
		Symbols:       strings.Join(req.Symbols, ","),
		Execute:       req.Execute,
		Status:        status,
		ErrorMessage:  errMsg,
		DurationMs:    int64(duration / time.Millisecond),
		RequestBody:   reqJSON,
		ResponseBody:  respJSON,
		ExecutionBody: execJSON,
	})
}

func isLLMTimeoutError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "deadline exceeded") || strings.Contains(msg, "timeout")
}

func marshalToString(v any) string {
	if v == nil {
		return ""
	}
	b, err := json.Marshal(v)
	if err != nil {
		fallback, _ := json.Marshal(map[string]string{
			"marshal_error": err.Error(),
		})
		return string(fallback)
	}
	return string(b)
}
