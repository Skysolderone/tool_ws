package agent

import (
	"context"
	"encoding/json"
	"errors"
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

	dataJSON, err := collectData(ctx, req)
	if err != nil {
		return nil, err
	}

	userMsg := "以下是当前交易数据，请分析并给出建议（严格返回 JSON）：\n" + dataJSON
	messages := []*schema.Message{
		schema.SystemMessage(systemPrompt),
		schema.UserMessage(userMsg),
	}

	timeout := 60 * time.Second
	if strings.Contains(strings.ToLower(chatModelName), "reasoner") {
		timeout = 180 * time.Second
	}
	llmCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	resp, err := chatModel.Generate(llmCtx, messages)
	if err != nil {
		return nil, err
	}

	raw := strings.TrimSpace(resp.Content)
	var output AnalysisOutput
	if unmarshalAnalysisOutput(raw, &output) == nil {
		return &output, nil
	}

	output.Summary = raw
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

func collectData(ctx context.Context, req AnalysisRequest) (string, error) {
	mode := strings.ToLower(strings.TrimSpace(req.Mode))
	if mode == "" {
		mode = "full"
	}

	data := map[string]any{}
	symbols := req.Symbols

	switch mode {
	case "full", "positions", "signals", "journal", "sentiment":
	default:
		return "", errors.New("invalid mode, should be one of: full|positions|signals|journal|sentiment")
	}

	if mode == "full" || mode == "positions" {
		if positions, err := collectPositionsData(ctx, symbols); err == nil && positions != nil {
			data["positions"] = positions
		}
	}

	if mode == "full" || mode == "signals" {
		if signals := collectSignalsData(symbols); signals != nil {
			data["signals"] = signals
		}
	}

	if mode == "full" || mode == "journal" {
		if journal, err := collectJournalData(30); err == nil && journal != nil {
			data["journal"] = journal
		}
	}

	if mode == "full" || mode == "sentiment" {
		data["sentiment"] = collectSentimentData()
	}

	b, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// HandleAnalyze POST/GET /tool/agent/analyze。
func HandleAnalyze(c context.Context, ctx *app.RequestContext) {
	if chatModel == nil {
		ctx.JSON(http.StatusServiceUnavailable, hertzutils.H{"error": "Agent 未配置 LLM"})
		return
	}

	start := time.Now()
	var req AnalysisRequest
	if string(ctx.Method()) == http.MethodPost {
		if err := ctx.BindJSON(&req); err != nil {
			ctx.JSON(http.StatusBadRequest, hertzutils.H{"error": "请求体 JSON 无效"})
			return
		}
	} else {
		req.Mode = ctx.DefaultQuery("mode", "full")
		req.Symbols = parseSymbolsQuery(ctx.DefaultQuery("symbols", ""))
		execRaw := strings.TrimSpace(ctx.DefaultQuery("execute", "false"))
		req.Execute = execRaw == "1" || strings.EqualFold(execRaw, "true")
	}

	if req.Mode == "" {
		req.Mode = "full"
	}

	result, err := RunAnalysis(c, req)
	if err != nil {
		saveAnalyzeLog(req, nil, nil, err, time.Since(start))
		ctx.JSON(http.StatusInternalServerError, hertzutils.H{"error": err.Error()})
		return
	}

	if req.Execute {
		items := req.ActionItems
		if len(items) == 0 {
			items = result.ActionItems
		}
		result.Execution = executeActionItems(c, items)
	}

	saveAnalyzeLog(req, result, result.Execution, nil, time.Since(start))
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
	return res
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
