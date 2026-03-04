package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"tools/api"

	"github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/schema"
	"github.com/cloudwego/hertz/pkg/app"
	hertzutils "github.com/cloudwego/hertz/pkg/common/utils"
	"gorm.io/gorm"
)

var chatModel *openai.ChatModel
var chatModelName string

var reChatSymbol = regexp.MustCompile(`\b[A-Z]{2,10}(?:USDT)?\b`)

var chatSymbolStopwords = map[string]struct{}{
	"POSITION": {}, "POSITIONS": {}, "SIGNAL": {}, "SIGNALS": {}, "NEWS": {}, "MESSAGE": {}, "MESSAGES": {},
	"BALANCE": {}, "ACCOUNT": {}, "PRICE": {}, "OPEN": {}, "CLOSE": {}, "SET": {}, "STOP": {}, "LOSS": {},
	"TAKE": {}, "PROFIT": {}, "LONG": {}, "SHORT": {}, "BUY": {}, "SELL": {}, "PLEASE": {}, "HELP": {},
	"WHAT": {}, "WHEN": {}, "WHERE": {}, "WHO": {}, "WHY": {}, "HOW": {}, "THE": {}, "AND": {}, "FOR": {},
	"WITH": {}, "THIS": {}, "THAT": {}, "FROM": {}, "YOUR": {}, "ARE": {}, "WILL": {}, "CAN": {}, "SHOULD": {},
}

const analyzeOverallTimeout = 10 * time.Minute
const analyzeAsyncTaskTimeout = 10 * time.Minute

func buildAnalyzeSystemPrompt() string {
	prompt := systemPrompt
	summary, err := api.GetAgentEvalSummaryForAgent(7)
	if err != nil || summary == nil {
		return prompt
	}
	if summary.TotalSuggestions <= 0 {
		return prompt
	}

	worst := "-"
	if len(summary.WorstSymbols) > 0 {
		worst = strings.Join(summary.WorstSymbols, ",")
	}

	return prompt + fmt.Sprintf(`

## 你的历史表现
近 7 天命中率：1H %.2f%% / 24H %.2f%%
表现最差币种：%s
请在这些币种上更加谨慎。`, summary.HitRate1H, summary.HitRate24H, worst)
}

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
	StartDailyAutoAnalyze()
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
		schema.SystemMessage(buildAnalyzeSystemPrompt()),
		schema.UserMessage(userMsg),
	}

	timeout := 10 * time.Minute
	if strings.Contains(strings.ToLower(chatModelName), "reasoner") {
		// 思考模型响应更慢，统一放宽到 10 分钟。
		timeout = 10 * time.Minute
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

type analyzeProgress struct {
	Phase  string `json:"phase"`
	Detail string `json:"detail"`
	Step   int    `json:"step"`
	Total  int    `json:"total"`
}

// RunAnalysisStream 执行流式分析，逐 chunk 产出 LLM 文本。
func RunAnalysisStream(
	ctx context.Context,
	req AnalysisRequest,
	onProgress func(analyzeProgress),
	onToken func(string),
) (*AnalysisOutput, error) {
	if chatModel == nil {
		return nil, errors.New("agent llm not configured")
	}
	analysisStart := time.Now()
	log.Printf("[Agent] RunAnalysisStream start mode=%s symbols=%v", strings.TrimSpace(req.Mode), req.Symbols)

	collectStart := time.Now()
	dataJSON, err := collectDataWithProgress(ctx, req, onProgress)
	if err != nil {
		log.Printf("[Agent] RunAnalysisStream collectData failed after=%v err=%v", time.Since(collectStart).Round(time.Millisecond), err)
		return nil, err
	}
	log.Printf("[Agent] RunAnalysisStream collectData done after=%v payload_bytes=%d", time.Since(collectStart).Round(time.Millisecond), len(dataJSON))

	userMsg := "以下是当前交易数据，请分析并给出建议（严格返回 JSON）：\n" + dataJSON
	messages := []*schema.Message{
		schema.SystemMessage(buildAnalyzeSystemPrompt()),
		schema.UserMessage(userMsg),
	}

	timeout := 10 * time.Minute
	if strings.Contains(strings.ToLower(chatModelName), "reasoner") {
		timeout = 10 * time.Minute
	}
	llmCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	llmStart := time.Now()
	log.Printf("[Agent] RunAnalysisStream llm start model=%s timeout=%v", chatModelName, timeout)
	stream, err := chatModel.Stream(llmCtx, messages)
	if err != nil {
		log.Printf("[Agent] RunAnalysisStream llm failed after=%v err=%v", time.Since(llmStart).Round(time.Millisecond), err)
		return nil, err
	}
	defer stream.Close()

	var rawBuilder strings.Builder
	for {
		chunk, recvErr := stream.Recv()
		if errors.Is(recvErr, io.EOF) {
			break
		}
		if errors.Is(recvErr, schema.ErrNoValue) {
			continue
		}
		if recvErr != nil {
			log.Printf("[Agent] RunAnalysisStream llm recv_failed after=%v err=%v", time.Since(llmStart).Round(time.Millisecond), recvErr)
			return nil, recvErr
		}
		if chunk == nil {
			continue
		}
		if strings.TrimSpace(chunk.Content) == "" {
			continue
		}
		rawBuilder.WriteString(chunk.Content)
		if onToken != nil {
			onToken(chunk.Content)
		}
	}

	raw := strings.TrimSpace(rawBuilder.String())
	var output AnalysisOutput
	if unmarshalAnalysisOutput(raw, &output) == nil {
		hydratePositionAnalysisFromCollectedData(&output, dataJSON)
		log.Printf("[Agent] RunAnalysisStream parse=json total=%v actions=%d", time.Since(analysisStart).Round(time.Millisecond), len(output.ActionItems))
		return &output, nil
	}

	output.Summary = raw
	log.Printf("[Agent] RunAnalysisStream parse=fallback_text total=%v", time.Since(analysisStart).Round(time.Millisecond))
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
	return collectDataWithProgress(ctx, req, nil)
}

func collectDataWithProgress(ctx context.Context, req AnalysisRequest, onProgress func(analyzeProgress)) (string, error) {
	mode := strings.ToLower(strings.TrimSpace(req.Mode))
	if mode == "" {
		mode = "full"
	}

	const totalSteps = 5
	emitProgress := func(step int, phase, detail string) {
		if onProgress == nil {
			return
		}
		onProgress(analyzeProgress{
			Phase:  phase,
			Detail: detail,
			Step:   step,
			Total:  totalSteps,
		})
	}

	data := map[string]any{}
	var warnings []string
	symbols := req.Symbols

	switch mode {
	case "full", "positions", "signals", "journal", "sentiment":
	default:
		return "", errors.New("invalid mode, should be one of: full|positions|signals|journal|sentiment")
	}

	emitProgress(1, "positions", "正在获取持仓与余额数据...")
	if mode == "full" || mode == "positions" {
		positions, err := collectPositionsData(ctx, symbols)
		if err != nil {
			warnings = append(warnings, "持仓数据获取失败: "+err.Error())
		} else if positions != nil {
			data["positions"] = positions
		}

		if balance, err := collectBalanceData(ctx); err != nil {
			warnings = append(warnings, "账户余额获取失败: "+err.Error())
		} else if balance != nil {
			data["balance"] = balance
		}
	}

	emitProgress(2, "signals", "正在收集多时间框架信号...")
	if mode == "full" || mode == "signals" {
		if signals := collectSignalsData(symbols); signals != nil {
			data["signals"] = signals
		} else {
			warnings = append(warnings, "推荐信号缓存为空（可能引擎尚未就绪）")
		}
	}

	emitProgress(3, "journal", "正在整理历史交易统计...")
	if mode == "full" || mode == "journal" {
		journal, err := collectJournalData(30)
		if err != nil {
			warnings = append(warnings, "交易日志获取失败: "+err.Error())
		} else if journal != nil {
			data["journal"] = journal
		}
	}

	emitProgress(4, "sentiment", "正在评估市场情绪...")
	if mode == "full" || mode == "sentiment" {
		data["sentiment"] = collectSentimentData()
	}

	emitProgress(5, "context", "正在注入历史上下文与新闻摘要...")
	if !req.Mock {
		history, err := CollectHistory(5)
		if err != nil {
			warnings = append(warnings, "历史分析记录读取失败: "+err.Error())
		} else if len(history) > 0 {
			data["recent_analysis_history"] = history
		}
	}

	if news := CollectNews(12); len(news) > 0 {
		data["recent_news"] = news
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
		asyncRaw := strings.TrimSpace(ctx.DefaultQuery("async", "false"))
		req.Async = asyncRaw == "1" || strings.EqualFold(asyncRaw, "true")
	}
	// query 参数优先级高于 body 字段，便于灰度切换。
	asyncRaw := strings.TrimSpace(ctx.DefaultQuery("async", ""))
	if asyncRaw != "" {
		req.Async = asyncRaw == "1" || strings.EqualFold(asyncRaw, "true")
	}

	if req.Mode == "" {
		req.Mode = "full"
	}
	req.Symbols = normalizeAndDedupeSymbols(req.Symbols)
	log.Printf("[Agent][%s] analyze start method=%s mode=%s symbols=%v execute=%v mock=%v async=%v", reqID, string(ctx.Method()), req.Mode, req.Symbols, req.Execute, req.Mock, req.Async)

	if req.Async {
		handleAnalyzeAsync(reqID, req, ctx)
		return
	}

	result, warning, err := runAnalyzePipeline(c, reqID, req)
	if err != nil {
		saveAnalyzeLog(req, nil, nil, err, time.Since(start))
		log.Printf("[Agent][%s] analyze failed after=%v err=%v", reqID, time.Since(start).Round(time.Millisecond), err)
		ctx.JSON(http.StatusInternalServerError, hertzutils.H{"error": err.Error()})
		return
	}

	saveAnalyzeLog(req, result, result.Execution, nil, time.Since(start))
	log.Printf("[Agent][%s] analyze done after=%v action_items=%d", reqID, time.Since(start).Round(time.Millisecond), len(result.ActionItems))
	if warning != "" {
		ctx.JSON(http.StatusOK, hertzutils.H{
			"data":    result,
			"warning": warning,
		})
		return
	}
	ctx.JSON(http.StatusOK, hertzutils.H{"data": result})
}

type analyzeSSEEvent struct {
	Event string
	Data  any
}

// HandleAnalyzeStream GET /tool/agent/analyze/stream
// 通过 SSE 推送策略分析进度，并返回最终 JSON 结果。
func HandleAnalyzeStream(c context.Context, ctx *app.RequestContext) {
	reqID := strconv.FormatInt(time.Now().UnixNano(), 36)
	req := AnalysisRequest{
		Mode:    ctx.DefaultQuery("mode", "full"),
		Symbols: parseSymbolsQuery(ctx.DefaultQuery("symbols", "")),
	}
	req.Symbols = normalizeAndDedupeSymbols(req.Symbols)
	if req.Mode == "" {
		req.Mode = "full"
	}

	log.Printf("[Agent][%s] analyze stream start mode=%s symbols=%v", reqID, req.Mode, req.Symbols)

	start := time.Now()
	logRecord := &api.AgentAnalysisLog{
		Mode:        normalizeMode(req.Mode),
		Source:      api.AgentAnalysisSourceAppManual,
		Symbols:     strings.Join(req.Symbols, ","),
		Execute:     false,
		Status:      api.AgentAnalysisStatusRunning,
		RequestBody: marshalToString(req),
	}
	if err := api.SaveAgentAnalysisLog(logRecord); err != nil {
		log.Printf("[Agent][%s] analyze stream create_log_failed err=%v", reqID, err)
	}
	taskID := logRecord.ID

	ctx.SetStatusCode(http.StatusOK)
	ctx.SetContentType("text/event-stream")
	ctx.Response.Header.Set("Cache-Control", "no-cache")
	ctx.Response.Header.Set("Connection", "keep-alive")
	ctx.Response.Header.Set("X-Accel-Buffering", "no")
	ctx.Response.Header.Set("Access-Control-Allow-Origin", "*")

	pr, pw := io.Pipe()
	ctx.SetBodyStream(pr, -1)

	go func() {
		defer func() {
			_ = pw.Close()
		}()

		writeEvent := func(event string, payload any) bool {
			b, err := json.Marshal(payload)
			if err != nil {
				return false
			}
			if _, err = fmt.Fprintf(pw, "event: %s\ndata: %s\n\n", event, string(b)); err != nil {
				return false
			}
			return true
		}
		writeKeepalive := func() bool {
			if _, err := io.WriteString(pw, ":keepalive\n\n"); err != nil {
				return false
			}
			return true
		}

		events := make(chan analyzeSSEEvent, 256)
		runCtx, cancel := context.WithTimeout(c, analyzeOverallTimeout)
		defer cancel()

		go func() {
			defer close(events)
			select {
			case events <- analyzeSSEEvent{Event: "progress", Data: analyzeProgress{Phase: "collecting", Detail: "正在读取持仓并应用写死策略...", Step: 1, Total: 3}}:
			case <-runCtx.Done():
				return
			}

			output, warning, err := runAnalyzePipeline(runCtx, reqID, req)

			durationMs := int64(time.Since(start) / time.Millisecond)
			if err != nil {
				log.Printf("[Agent][%s] analyze stream failed after=%v err=%v", reqID, time.Since(start).Round(time.Millisecond), err)
				if taskID > 0 {
					_ = api.UpdateAgentAnalysisLog(taskID, map[string]any{
						"status":         api.AgentAnalysisStatusFailed,
						"error_message":  err.Error(),
						"duration_ms":    durationMs,
						"response_body":  "",
						"execution_body": "",
					})
				}
				select {
				case events <- analyzeSSEEvent{Event: "error", Data: hertzutils.H{"message": err.Error()}}:
				case <-runCtx.Done():
				}
				return
			}

			if output == nil {
				output = &AnalysisOutput{}
			}
			if warning != "" {
				select {
				case events <- analyzeSSEEvent{Event: "progress", Data: analyzeProgress{Phase: "warning", Detail: warning, Step: 2, Total: 3}}:
				case <-runCtx.Done():
					return
				}
			}
			if taskID > 0 {
				_ = api.UpdateAgentAnalysisLog(taskID, map[string]any{
					"status":         api.AgentAnalysisStatusSuccess,
					"error_message":  "",
					"duration_ms":    durationMs,
					"response_body":  marshalToString(output),
					"execution_body": marshalToString(output.Execution),
				})
			}

			log.Printf("[Agent][%s] analyze stream done after=%v action_items=%d", reqID, time.Since(start).Round(time.Millisecond), len(output.ActionItems))
			select {
			case events <- analyzeSSEEvent{Event: "token", Data: hertzutils.H{"text": marshalToString(output)}}:
			case <-runCtx.Done():
				return
			}
			select {
			case events <- analyzeSSEEvent{Event: "progress", Data: analyzeProgress{Phase: "done", Detail: "策略分析完成", Step: 3, Total: 3}}:
			case <-runCtx.Done():
				return
			}
			select {
			case events <- analyzeSSEEvent{Event: "done", Data: hertzutils.H{"task_id": taskID}}:
			case <-runCtx.Done():
			}
		}()

		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-runCtx.Done():
				return
			case ev, ok := <-events:
				if !ok {
					return
				}
				if !writeEvent(ev.Event, ev.Data) {
					return
				}
			case <-ticker.C:
				if !writeKeepalive() {
					return
				}
			}
		}
	}()
}

type chatIntent struct {
	Symbols       []string
	NeedPositions bool
	NeedSignals   bool
	NeedNews      bool
	NeedBalance   bool
	NeedSentiment bool
	NeedJournal   bool
}

func (i chatIntent) requestedFields() []string {
	fields := make([]string, 0, 6)
	if i.NeedPositions {
		fields = append(fields, "positions")
	}
	if i.NeedSignals {
		fields = append(fields, "signals")
	}
	if i.NeedNews {
		fields = append(fields, "recent_news")
	}
	if i.NeedBalance {
		fields = append(fields, "balance")
	}
	if i.NeedSentiment {
		fields = append(fields, "sentiment")
	}
	if i.NeedJournal {
		fields = append(fields, "journal")
	}
	return fields
}

func detectChatIntent(req ChatRequest) chatIntent {
	msg := strings.ToLower(strings.TrimSpace(req.Message))
	intent := chatIntent{
		Symbols: normalizeAndDedupeSymbols(append(req.Symbols, extractSymbolsFromChatMessage(req.Message)...)),
	}

	intent.NeedPositions = containsAny(msg, "持仓", "仓位", "position")
	intent.NeedSignals = containsAny(msg, "信号", "signal", "买", "卖", "开仓", "平仓")
	intent.NeedNews = containsAny(msg, "新闻", "消息", "news", "事件")
	intent.NeedBalance = containsAny(msg, "余额", "balance", "资金")
	intent.NeedSentiment = containsAny(msg, "情绪", "sentiment", "费率", "爆仓", "多空比")
	intent.NeedJournal = containsAny(msg, "复盘", "journal", "胜率", "回撤", "盈亏比")

	if intent.NeedPositions {
		intent.NeedBalance = true
	}

	if !intent.NeedPositions && !intent.NeedSignals && !intent.NeedNews && !intent.NeedBalance && !intent.NeedSentiment && !intent.NeedJournal {
		intent.NeedPositions = true
		intent.NeedSignals = true
		intent.NeedSentiment = true
		intent.NeedBalance = true
	}

	return intent
}

func containsAny(text string, tokens ...string) bool {
	for _, token := range tokens {
		if token != "" && strings.Contains(text, token) {
			return true
		}
	}
	return false
}

func extractSymbolsFromChatMessage(message string) []string {
	upper := strings.ToUpper(message)
	rawMatches := reChatSymbol.FindAllString(upper, -1)
	if len(rawMatches) == 0 {
		return nil
	}

	out := make([]string, 0, len(rawMatches))
	for _, token := range rawMatches {
		token = strings.TrimSpace(strings.ToUpper(token))
		if token == "" {
			continue
		}
		if _, blocked := chatSymbolStopwords[token]; blocked {
			continue
		}
		if !strings.HasSuffix(token, "USDT") {
			token += "USDT"
		}
		if token == "USDT" {
			continue
		}
		out = append(out, token)
	}
	return normalizeAndDedupeSymbols(out)
}

func collectChatData(ctx context.Context, intent chatIntent) (string, error) {
	data := map[string]any{}
	var warnings []string

	if intent.NeedPositions {
		positions, err := collectPositionsData(ctx, intent.Symbols)
		if err != nil {
			warnings = append(warnings, "持仓数据获取失败: "+err.Error())
		} else if positions != nil {
			data["positions"] = positions
		}
	}

	if intent.NeedSignals {
		if signals := collectSignalsData(intent.Symbols); signals != nil {
			data["signals"] = signals
		} else {
			warnings = append(warnings, "推荐信号缓存为空（可能引擎尚未就绪）")
		}
	}

	if intent.NeedNews {
		if news := CollectNews(10); len(news) > 0 {
			data["recent_news"] = news
		} else {
			warnings = append(warnings, "新闻缓存为空（可能服务刚启动）")
		}
	}

	if intent.NeedBalance {
		if balance, err := collectBalanceData(ctx); err != nil {
			warnings = append(warnings, "账户余额获取失败: "+err.Error())
		} else if balance != nil {
			data["balance"] = balance
		}
	}

	if intent.NeedSentiment {
		data["sentiment"] = collectSentimentData()
	}

	if intent.NeedJournal {
		journal, err := collectJournalData(30)
		if err != nil {
			warnings = append(warnings, "交易日志获取失败: "+err.Error())
		} else if journal != nil {
			data["journal"] = journal
		}
	}

	if history, err := CollectHistory(3); err != nil {
		warnings = append(warnings, "历史分析记录读取失败: "+err.Error())
	} else if len(history) > 0 {
		data["recent_analysis_history"] = history
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

func unmarshalChatResponse(raw string, out *ChatResponse) error {
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

func trimReply(text string, maxRunes int) string {
	text = strings.TrimSpace(text)
	if maxRunes <= 0 || text == "" {
		return text
	}
	rs := []rune(text)
	if len(rs) <= maxRunes {
		return text
	}
	return strings.TrimSpace(string(rs[:maxRunes]))
}

// HandleChat POST /tool/agent/chat
func HandleChat(c context.Context, ctx *app.RequestContext) {
	start := time.Now()
	var req ChatRequest
	if err := ctx.BindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, hertzutils.H{"error": "请求体 JSON 无效"})
		return
	}
	req.Message = strings.TrimSpace(req.Message)
	req.Symbols = normalizeAndDedupeSymbols(req.Symbols)
	if req.Message == "" {
		ctx.JSON(http.StatusBadRequest, hertzutils.H{"error": "message 不能为空"})
		return
	}
	if chatModel == nil {
		ctx.JSON(http.StatusServiceUnavailable, hertzutils.H{"error": "Agent 未配置 LLM"})
		return
	}

	intent := detectChatIntent(req)
	dataJSON, err := collectChatData(c, intent)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, hertzutils.H{"error": "收集对话数据失败: " + err.Error()})
		return
	}

	payload := map[string]any{
		"question":         req.Message,
		"symbols":          intent.Symbols,
		"requested_fields": intent.requestedFields(),
		"data":             json.RawMessage(dataJSON),
	}
	payloadJSON, _ := json.MarshalIndent(payload, "", "  ")
	messages := []*schema.Message{
		schema.SystemMessage(chatSystemPrompt),
		schema.UserMessage("用户问题与上下文数据如下（请严格返回 JSON）：\n" + string(payloadJSON)),
	}

	llmCtx, cancel := context.WithTimeout(c, 2*time.Minute)
	defer cancel()

	resp, err := chatModel.Generate(llmCtx, messages)
	if err != nil {
		log.Printf("[Agent] chat failed after=%v err=%v", time.Since(start).Round(time.Millisecond), err)
		ctx.JSON(http.StatusInternalServerError, hertzutils.H{"error": err.Error()})
		return
	}

	raw := strings.TrimSpace(resp.Content)
	out := ChatResponse{}
	if unmarshalChatResponse(raw, &out) != nil {
		out.Reply = raw
	}
	out.Reply = trimReply(out.Reply, 320)
	if out.Reply == "" {
		out.Reply = "当前没有可用数据支持该问题，请稍后重试。"
	}

	log.Printf("[Agent] chat done after=%v symbols=%v requested=%v actions=%d", time.Since(start).Round(time.Millisecond), intent.Symbols, intent.requestedFields(), len(out.ActionItems))
	ctx.JSON(http.StatusOK, hertzutils.H{"data": out})
}

func runAnalyzePipeline(ctx context.Context, reqID string, req AnalysisRequest) (*AnalysisOutput, string, error) {
	var (
		result  *AnalysisOutput
		warning string
	)
	if req.Mock {
		log.Printf("[Agent][%s] analyze using_mock_response", reqID)
		result = buildMockAnalysisOutput(req)
	} else {
		runCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
		defer cancel()
		var err error
		result, err = runStaticStrategyAnalysis(runCtx, req)
		if err != nil {
			return nil, "", err
		}
	}

	if req.Execute && result != nil {
		items := req.ActionItems
		if len(items) == 0 {
			items = result.ActionItems
		}
		execStart := time.Now()
		log.Printf("[Agent][%s] execute start items=%d", reqID, len(items))
		result.Execution = executeActionItems(ctx, items)
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

	if result == nil {
		result = &AnalysisOutput{}
	}
	return result, warning, nil
}

func handleAnalyzeAsync(reqID string, req AnalysisRequest, ctx *app.RequestContext) {
	taskID, err := enqueueAsyncAnalyze(reqID, req, api.AgentAnalysisSourceAppManual)
	if err != nil {
		log.Printf("[Agent][%s] async create_log_failed err=%v", reqID, err)
		if strings.Contains(strings.ToLower(err.Error()), "database logging") {
			ctx.JSON(http.StatusInternalServerError, hertzutils.H{"error": "异步分析需要启用数据库日志"})
			return
		}
		ctx.JSON(http.StatusInternalServerError, hertzutils.H{"error": "创建异步任务失败: " + err.Error()})
		return
	}

	ctx.JSON(http.StatusAccepted, hertzutils.H{
		"data": hertzutils.H{
			"task_id": taskID,
			"status":  api.AgentAnalysisStatusPending,
		},
	})
}

func runAnalyzeAsyncTask(taskID uint, reqID string, req AnalysisRequest) {
	workerID := fmt.Sprintf("%s-%d", reqID, taskID)
	start := time.Now()
	_ = api.UpdateAgentAnalysisLog(taskID, map[string]any{
		"status":        api.AgentAnalysisStatusRunning,
		"error_message": "",
	})
	log.Printf("[Agent][%s] async start", workerID)

	taskCtx, cancel := context.WithTimeout(context.Background(), analyzeAsyncTaskTimeout)
	defer cancel()

	result, warning, runErr := runAnalyzePipeline(taskCtx, workerID, req)
	durationMs := int64(time.Since(start) / time.Millisecond)
	if runErr != nil {
		log.Printf("[Agent][%s] async failed after=%v err=%v", workerID, time.Since(start).Round(time.Millisecond), runErr)
		_ = api.UpdateAgentAnalysisLog(taskID, map[string]any{
			"status":         api.AgentAnalysisStatusFailed,
			"error_message":  runErr.Error(),
			"duration_ms":    durationMs,
			"response_body":  "",
			"execution_body": "",
		})
		return
	}

	respJSON := marshalToString(result)
	execJSON := ""
	if result != nil {
		execJSON = marshalToString(result.Execution)
	}
	errMsg := ""
	if warning != "" {
		errMsg = warning
	}
	if err := api.UpdateAgentAnalysisLog(taskID, map[string]any{
		"status":         api.AgentAnalysisStatusSuccess,
		"error_message":  errMsg,
		"duration_ms":    durationMs,
		"response_body":  respJSON,
		"execution_body": execJSON,
	}); err != nil {
		log.Printf("[Agent][%s] async update_log_failed err=%v", workerID, err)
		return
	}

	log.Printf("[Agent][%s] async done after=%v", workerID, time.Since(start).Round(time.Millisecond))
}

// HandleExecute POST /tool/agent/execute。
// 仅执行前端传入的 action_items，不触发 LLM 分析。
func HandleExecute(c context.Context, ctx *app.RequestContext) {
	start := time.Now()
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
	execErrMsg := ""
	if result != nil && result.Failed > 0 {
		execErrMsg = fmt.Sprintf("execution has failed items: %d", result.Failed)
	}
	_ = api.SaveAgentAnalysisLog(&api.AgentAnalysisLog{
		Mode:          "execute",
		Source:        api.AgentAnalysisSourceAppManual,
		Symbols:       strings.Join(extractSymbolsFromActionItems(req.ActionItems), ","),
		Execute:       true,
		Status:        api.AgentAnalysisStatusSuccess,
		ErrorMessage:  execErrMsg,
		DurationMs:    int64(time.Since(start) / time.Millisecond),
		RequestBody:   marshalToString(req),
		ExecutionBody: marshalToString(result),
	})
	ctx.JSON(http.StatusOK, hertzutils.H{"data": result})
}

func extractSymbolsFromActionItems(items []ActionItem) []string {
	if len(items) == 0 {
		return nil
	}
	set := make(map[string]struct{}, len(items))
	out := make([]string, 0, len(items))
	for _, item := range items {
		symbol := strings.ToUpper(strings.TrimSpace(item.Symbol))
		if symbol == "" {
			continue
		}
		if _, ok := set[symbol]; ok {
			continue
		}
		set[symbol] = struct{}{}
		out = append(out, symbol)
	}
	if len(out) == 0 {
		return nil
	}
	return out
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

// HandleLog GET /tool/agent/log?id=123
func HandleLog(c context.Context, ctx *app.RequestContext) {
	_ = c
	idRaw := strings.TrimSpace(ctx.DefaultQuery("id", ""))
	id64, err := strconv.ParseUint(idRaw, 10, 32)
	if err != nil || id64 == 0 {
		ctx.JSON(http.StatusBadRequest, hertzutils.H{"error": "id 参数无效"})
		return
	}

	record, err := api.GetAgentAnalysisLogByID(uint(id64))
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			ctx.JSON(http.StatusNotFound, hertzutils.H{"error": "记录不存在"})
			return
		}
		ctx.JSON(http.StatusInternalServerError, hertzutils.H{"error": err.Error()})
		return
	}
	if record == nil {
		ctx.JSON(http.StatusNotFound, hertzutils.H{"error": "记录不存在"})
		return
	}
	ctx.JSON(http.StatusOK, hertzutils.H{"data": record})
}

// HandleLogs GET /tool/agent/logs?limit=50&status=PENDING|RUNNING|SUCCESS|FAILED&execute=true|false
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

	status := api.AgentAnalysisStatusSuccess
	errMsg := ""
	if runErr != nil {
		status = api.AgentAnalysisStatusFailed
		errMsg = runErr.Error()
	}

	_ = api.SaveAgentAnalysisLog(&api.AgentAnalysisLog{
		Mode:          normalizeMode(req.Mode),
		Source:        api.AgentAnalysisSourceAppManual,
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

func normalizeMode(mode string) string {
	mode = strings.TrimSpace(mode)
	if mode == "" {
		return "full"
	}
	return mode
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
