package agent

// AnalysisRequest HTTP 请求体。
type AnalysisRequest struct {
	Mode        string       `json:"mode"`                   // full|positions|signals|journal|sentiment
	Symbols     []string     `json:"symbols"`                // 可选：指定币种过滤
	Mock        bool         `json:"mock,omitempty"`         // 调试：返回 mock 分析结果（不调用 LLM）
	Async       bool         `json:"async,omitempty"`        // 异步执行：立即返回任务ID，结果写入日志
	Execute     bool         `json:"execute,omitempty"`      // 是否执行 action_items
	ActionItems []ActionItem `json:"action_items,omitempty"` // 可选：执行指定动作；为空时执行本次分析动作
}

// ChatRequest 自然语言对话请求。
type ChatRequest struct {
	Message string   `json:"message"`
	Symbols []string `json:"symbols,omitempty"`
}

// ChatResponse 自然语言对话响应。
type ChatResponse struct {
	Reply       string       `json:"reply"`
	ActionItems []ActionItem `json:"action_items,omitempty"`
}

// AnalysisOutput Agent 最终输出。
type AnalysisOutput struct {
	Summary          string           `json:"summary"`
	PositionAnalysis []PositionAdvice `json:"position_analysis"`
	SignalEvaluation []SignalEval     `json:"signal_evaluation"`
	JournalReview    JournalInsight   `json:"journal_review"`
	ActionItems      []ActionItem     `json:"action_items"`
	NewsImpact       []NewsImpactItem `json:"news_impact,omitempty"`
	Execution        *ExecutionResult `json:"execution,omitempty"`
}

// PositionAdvice 单个持仓的 LLM 分析。
type PositionAdvice struct {
	Symbol     string   `json:"symbol"`
	Assessment string   `json:"assessment"`
	Risk       string   `json:"risk"` // low|medium|high|critical
	Suggestion string   `json:"suggestion"`
	Reasons    []string `json:"reasons,omitempty"` // 触发该建议的原因
}

// SignalEval 推荐信号评估。
type SignalEval struct {
	Symbol    string  `json:"symbol"`
	Direction string  `json:"direction"`
	Score     float64 `json:"score"` // 0-10
	RiskLevel string  `json:"riskLevel"`
	Comment   string  `json:"comment"`
}

// JournalInsight 交易复盘洞察。
type JournalInsight struct {
	Patterns   []string `json:"patterns"`
	Weaknesses []string `json:"weaknesses"`
	Strengths  []string `json:"strengths"`
	Suggestion string   `json:"suggestion"`
}

// ActionItem 单个操作步骤。
type ActionItem struct {
	Action   string `json:"action"` // close|reduce|add|set_sl|set_tp|open|wait
	Symbol   string `json:"symbol"`
	Detail   string `json:"detail"`
	Priority string `json:"priority"` // high|medium|low
	Risk     string `json:"risk"`
}

// ActionExecution 单条动作执行结果。
type ActionExecution struct {
	Action  string `json:"action"`
	Symbol  string `json:"symbol"`
	Status  string `json:"status"` // success|failed|skipped
	Message string `json:"message"`
	OrderID int64  `json:"order_id,omitempty"`
}

// ExecutionResult 执行汇总结果。
type ExecutionResult struct {
	Requested int               `json:"requested"`
	Executed  int               `json:"executed"`
	Success   int               `json:"success"`
	Failed    int               `json:"failed"`
	Skipped   int               `json:"skipped"`
	Results   []ActionExecution `json:"results"`
}

// NewsImpactItem 新闻影响分析。
type NewsImpactItem struct {
	Title    string `json:"title"`
	Impact   string `json:"impact"`   // bullish|bearish|neutral
	Affected string `json:"affected"` // 受影响币种
	Comment  string `json:"comment"`
}

// HistorySummary 历史分析摘要（用于注入上下文）。
type HistorySummary struct {
	Date            string         `json:"date"`
	Summary         string         `json:"summary"`
	ActionItems     []ActionItem   `json:"action_items,omitempty"`
	Executed        bool           `json:"executed"`
	ExecutionResult map[string]int `json:"execution_result,omitempty"`
	Mode            string         `json:"mode,omitempty"`
	Symbols         []string       `json:"symbols,omitempty"`
}
