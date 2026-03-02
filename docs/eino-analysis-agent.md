# Eino 分析 Agent 实现流程文档

## 1. 架构概览

```
┌─────────────┐     POST /tool/agent/analyze      ┌──────────────────┐
│  前端 App    │ ──────────────────────────────────→ │  Hertz Handler   │
│  (移动端)    │ ←────────────── JSON Response ──── │  (agent pkg)     │
└─────────────┘                                     └────────┬─────────┘
                                                             │
                                                   ┌────────▼─────────┐
                                                   │  Eino Agent      │
                                                   │  (ChatModel +    │
                                                   │   4 Tools)       │
                                                   └────────┬─────────┘
                                                             │
                          ┌──────────────────────────────────┼──────────────────────────────┐
                          │                    │                    │                         │
                 ┌────────▼──────┐   ┌────────▼──────┐   ┌────────▼──────┐   ┌──────────────▼──┐
                 │ get_positions │   │ get_signals   │   │ get_journal   │   │ get_sentiment   │
                 │ 持仓+AI分析   │   │ 推荐交易信号  │   │ 交易日志统计  │   │ 市场情绪       │
                 └───────┬───────┘   └───────┬───────┘   └───────┬───────┘   └───────┬────────┘
                         │                   │                   │                   │
                         ▼                   ▼                   ▼                   ▼
                  api.GetPositions    api.finalCache      api.loadTrades      api.calcMarket
                  api.analyzePos     (预计算缓存)         api.calcJournal      Sentiment()
                  (recommend.go)     (recommend.go)       (analytics.go)      (recommend.go)
```

### 数据流

1. 前端发起 `POST /tool/agent/analyze` 请求
2. Handler 解析请求参数（分析模式、币种过滤）
3. Agent 按需调用 4 个 Tool 收集数据
4. 将数据汇总后连同 System Prompt 发给 DeepSeek LLM
5. LLM 返回结构化 JSON（结论 + 建议 + 操作步骤清单）
6. Handler 解析并返回给前端

---

## 2. 文件结构

```
agent/
├── types.go      # 输入输出结构体定义
├── tools.go      # 4 个 Tool（封装现有 api 包数据获取函数）
├── prompt.go     # System Prompt 模板
└── agent.go      # Agent 初始化 + RunAnalysis 入口 + HTTP Handler
```

修改的现有文件：
| 文件 | 修改内容 |
|------|---------|
| `api/config.go` | Config 结构体新增 `LLM LLMConfig` 字段 |
| `main.go` | 注册 `/tool/agent/analyze` 路由 + 初始化 Agent |
| `go.mod` | 新增 eino + eino-ext 依赖（已完成） |

---

## 3. 配置项 (config.json)

```json
{
  "llm": {
    "provider": "deepseek",
    "api_key": "sk-xxx",
    "base_url": "https://api.deepseek.com/v1",
    "model": "deepseek-chat",
    "max_tokens": 4096,
    "temperature": 0.3
  }
}
```

对应 Go 结构体（加到 `api/config.go`）：

```go
type LLMConfig struct {
    Provider    string  `json:"provider"`
    APIKey      string  `json:"api_key"`
    BaseURL     string  `json:"base_url"`
    Model       string  `json:"model"`
    MaxTokens   int     `json:"max_tokens"`
    Temperature float64 `json:"temperature"`
}
```

---

## 4. 各文件实现细节

### 4.1 agent/types.go

```go
package agent

// AnalysisRequest HTTP 请求体
type AnalysisRequest struct {
    Mode    string   `json:"mode"`    // "full" | "positions" | "signals" | "journal"
    Symbols []string `json:"symbols"` // 可选：指定币种过滤
}

// AnalysisOutput Agent 最终输出
type AnalysisOutput struct {
    Summary          string             `json:"summary"`
    PositionAnalysis []PositionAdvice   `json:"position_analysis"`
    SignalEvaluation []SignalEval       `json:"signal_evaluation"`
    JournalReview    JournalInsight     `json:"journal_review"`
    ActionItems      []ActionItem       `json:"action_items"`
}

// PositionAdvice 单个持仓的 LLM 分析
type PositionAdvice struct {
    Symbol     string `json:"symbol"`
    Assessment string `json:"assessment"` // 当前状态评估
    Risk       string `json:"risk"`       // 风险等级: low/medium/high/critical
    Suggestion string `json:"suggestion"` // 建议操作
}

// SignalEval 推荐信号评估
type SignalEval struct {
    Symbol     string  `json:"symbol"`
    Direction  string  `json:"direction"`
    Score      float64 `json:"score"`       // LLM 重新评分 0-10
    RiskLevel  string  `json:"riskLevel"`
    Comment    string  `json:"comment"`
}

// JournalInsight 交易复盘洞察
type JournalInsight struct {
    Patterns   []string `json:"patterns"`   // 发现的规律
    Weaknesses []string `json:"weaknesses"` // 薄弱环节
    Strengths  []string `json:"strengths"`  // 优势
    Suggestion string   `json:"suggestion"` // 改进建议
}

// ActionItem 单个操作步骤
type ActionItem struct {
    Action   string `json:"action"`   // 操作类型: close/reduce/add/set_sl/set_tp/open/wait
    Symbol   string `json:"symbol"`
    Detail   string `json:"detail"`   // 具体描述
    Priority string `json:"priority"` // high/medium/low
    Risk     string `json:"risk"`     // 风险说明
}
```

### 4.2 agent/tools.go — 4 个 Tool

每个 Tool 封装现有 `api` 包的数据获取函数，返回 JSON 字符串供 Agent 使用。

| Tool 名称 | 封装的函数 | 数据来源 |
|-----------|-----------|---------|
| `get_positions` | `api.GetPositionsViaWs()` + `api.analyzePosition()` | 当前持仓 + 技术分析 |
| `get_signals` | `api.finalCache` (推荐缓存) | 预计算的多时间框架推荐信号 |
| `get_journal` | `api.loadTradesForAnalytics()` + `api.calcJournalMetrics()` | 近 30 天交易统计 |
| `get_sentiment` | `api.calcMarketSentiment()` | BTC 资金费率 + 多空比 + 爆仓 |

**注意事项**：
- `analyzePosition` 和 `calcMarketSentiment` 等目前是 `api` 包的小写未导出函数
- 需要在 `api` 包新增导出函数（如 `ExportAnalyzePositions()`, `ExportCalcSentiment()`）供 `agent` 包调用
- 或者直接在 `api` 包内新增文件 `api/agent_bridge.go` 暴露桥接函数

**桥接函数方案** (`api/agent_bridge.go`)：

```go
package api

import (
    "context"
    "time"
)

// GetAnalyzedPositions 导出持仓分析结果，供 agent 包调用
func GetAnalyzedPositions(ctx context.Context) (*AnalyzeResponse, error) {
    // 复用 HandleRecommendAnalyze 内部逻辑
}

// GetRecommendCache 导出推荐缓存，供 agent 包调用
func GetRecommendCache() *RecommendResponse {
    finalCache.RLock()
    defer finalCache.RUnlock()
    return finalCache.resp
}

// GetJournalMetrics 导出交易日志统计，供 agent 包调用
func GetJournalMetrics(days int) (*journalResponse, error) {
    now := time.Now()
    from := now.AddDate(0, 0, -days)
    records, err := loadTradesForAnalytics(from, now)
    if err != nil {
        return nil, err
    }
    metrics := calcJournalMetrics(records, now)
    // ... 构造 response
}

// GetMarketSentiment 导出市场情绪，供 agent 包调用
func GetMarketSentiment() MarketSentiment {
    return calcMarketSentiment()
}
```

### 4.3 agent/prompt.go — System Prompt

```go
package agent

const systemPrompt = `你是一个专业的加密货币合约交易分析师。你的任务是基于提供的数据进行深度分析。

## 分析维度
1. **持仓分析** - 评估每个持仓的风险、盈亏状态、是否应调整
2. **信号评估** - 评估推荐交易信号的可靠性和风险回报比
3. **交易复盘** - 分析近期交易记录，找出规律、优势和改进点
4. **市场情绪** - 结合资金费率、多空比、爆仓数据判断市场状态

## 输出格式（严格JSON）
{
  "summary": "一句话总结当前状态和最重要的建议",
  "position_analysis": [
    {"symbol": "BTCUSDT", "assessment": "状态评估", "risk": "low|medium|high|critical", "suggestion": "建议"}
  ],
  "signal_evaluation": [
    {"symbol": "ETHUSDT", "direction": "LONG", "score": 7.5, "riskLevel": "medium", "comment": "评价"}
  ],
  "journal_review": {
    "patterns": ["发现的规律"],
    "weaknesses": ["薄弱环节"],
    "strengths": ["优势"],
    "suggestion": "改进建议"
  },
  "action_items": [
    {"action": "close|reduce|add|set_sl|set_tp|open|wait", "symbol": "BTCUSDT", "detail": "具体操作", "priority": "high|medium|low", "risk": "风险说明"}
  ]
}

## 重要规则
- action_items 按优先级排序，high 在前
- 每个建议必须有明确的风险说明
- 不要建议超过当前账户能力的操作
- 保守为主，宁可错过机会也不要增加风险
- 只输出 JSON，不要输出其他内容`
```

### 4.4 agent/agent.go — 核心逻辑 + HTTP Handler

```go
package agent

import (
    "context"
    "encoding/json"
    "log"
    "net/http"

    "tools/api"

    "github.com/cloudwego/eino-ext/components/model/openai"
    "github.com/cloudwego/eino/schema"
    "github.com/cloudwego/hertz/pkg/app"
    hertzutils "github.com/cloudwego/hertz/pkg/common/utils"
)

var chatModel *openai.ChatModel

// InitAgent 初始化 Eino Agent（在 main.go 中调用）
func InitAgent(cfg api.LLMConfig) error {
    if cfg.APIKey == "" {
        log.Printf("[Agent] LLM not configured, agent disabled")
        return nil
    }
    model, err := openai.NewChatModel(context.Background(), &openai.ChatModelConfig{
        BaseURL:     cfg.BaseURL,
        APIKey:      cfg.APIKey,
        Model:       cfg.Model,
        MaxTokens:   &cfg.MaxTokens,
        Temperature: &cfg.Temperature,
    })
    if err != nil {
        return err
    }
    chatModel = model
    log.Printf("[Agent] Initialized with provider=%s model=%s", cfg.Provider, cfg.Model)
    return nil
}

// RunAnalysis 执行分析
func RunAnalysis(ctx context.Context, req AnalysisRequest) (*AnalysisOutput, error) {
    // 1. 收集数据
    dataJSON := collectData(ctx, req)

    // 2. 构造消息
    messages := []*schema.Message{
        schema.SystemMessage(systemPrompt),
        schema.UserMessage("以下是当前交易数据，请分析并给出建议：\n" + dataJSON),
    }

    // 3. 调用 LLM
    resp, err := chatModel.Generate(ctx, messages)
    if err != nil {
        return nil, err
    }

    // 4. 解析输出
    var output AnalysisOutput
    if err := json.Unmarshal([]byte(resp.Content), &output); err != nil {
        // 如果解析失败，把原文放到 summary
        output.Summary = resp.Content
    }
    return &output, nil
}

// collectData 收集所有分析数据
func collectData(ctx context.Context, req AnalysisRequest) string {
    data := map[string]any{}

    mode := req.Mode
    if mode == "" {
        mode = "full"
    }

    if mode == "full" || mode == "positions" {
        if pos, err := api.GetAnalyzedPositions(ctx); err == nil {
            data["positions"] = pos
        }
    }
    if mode == "full" || mode == "signals" {
        if rec := api.GetRecommendCache(); rec != nil {
            data["signals"] = rec
        }
    }
    if mode == "full" || mode == "journal" {
        if journal, err := api.GetJournalMetrics(30); err == nil {
            data["journal"] = journal
        }
    }
    if mode == "full" || mode == "sentiment" {
        data["sentiment"] = api.GetMarketSentiment()
    }

    b, _ := json.MarshalIndent(data, "", "  ")
    return string(b)
}

// HandleAnalyze POST/GET /tool/agent/analyze
func HandleAnalyze(c context.Context, ctx *app.RequestContext) {
    if chatModel == nil {
        ctx.JSON(http.StatusServiceUnavailable, hertzutils.H{"error": "Agent 未配置 LLM"})
        return
    }

    var req AnalysisRequest
    if string(ctx.Method()) == "POST" {
        _ = ctx.BindJSON(&req)
    } else {
        req.Mode = string(ctx.DefaultQuery("mode", "full"))
    }
    if req.Mode == "" {
        req.Mode = "full"
    }

    result, err := RunAnalysis(c, req)
    if err != nil {
        ctx.JSON(http.StatusInternalServerError, hertzutils.H{"error": err.Error()})
        return
    }

    ctx.JSON(http.StatusOK, hertzutils.H{"data": result})
}
```

---

## 5. main.go 修改

```go
import "tools/agent"

// 在初始化部分添加（在 StartRecommendEngine() 之后）：
if err := agent.InitAgent(api.Cfg.LLM); err != nil {
    log.Printf("[Agent] Init failed: %v (agent disabled)", err)
}

// 在路由注册部分添加：
apiGroup.POST("/agent/analyze", agent.HandleAnalyze)
apiGroup.GET("/agent/analyze", agent.HandleAnalyze)
```

---

## 6. api 包导出函数（api/agent_bridge.go）

需要新增的导出函数：

| 函数名 | 返回值 | 说明 |
|--------|--------|------|
| `GetAnalyzedPositions(ctx)` | `(*AnalyzeResponse, error)` | 持仓 + 技术分析结果 |
| `GetRecommendCache()` | `*RecommendResponse` | 推荐信号缓存 |
| `GetJournalMetrics(days)` | `(*journalResponse, error)` | 交易日志统计 |
| `GetMarketSentiment()` | `MarketSentiment` | 市场情绪数据 |

这些函数复用现有逻辑，只是将内部小写函数包装为大写导出。

---

## 7. API 接口文档

### POST /tool/agent/analyze

**请求体：**
```json
{
  "mode": "full",        // full | positions | signals | journal | sentiment
  "symbols": ["BTCUSDT"] // 可选，币种过滤
}
```

**响应：**
```json
{
  "data": {
    "summary": "当前持有 BTC 多仓盈利中，ETH 空仓面临风险，建议减仓",
    "position_analysis": [
      {
        "symbol": "BTCUSDT",
        "assessment": "多仓 +3.2%，趋势仍然向上，但接近关键阻力位 98500",
        "risk": "medium",
        "suggestion": "建议移动止盈到 96000，保护利润"
      }
    ],
    "signal_evaluation": [
      {
        "symbol": "SOLUSDT",
        "direction": "LONG",
        "score": 7.5,
        "riskLevel": "medium",
        "comment": "RSI 超卖回升 + 量能放大，信号较强，但日线仍是下降趋势"
      }
    ],
    "journal_review": {
      "patterns": ["胜率在亚洲时段较高", "大仓位交易亏损概率更大"],
      "weaknesses": ["止损执行不够果断", "追涨倾向明显"],
      "strengths": ["短线交易胜率 62%", "资金管理纪律良好"],
      "suggestion": "建议减少追涨操作，设置更严格的入场条件"
    },
    "action_items": [
      {
        "action": "set_sl",
        "symbol": "BTCUSDT",
        "detail": "将止损上移到 96000（保护已有利润）",
        "priority": "high",
        "risk": "可能被震荡洗出"
      },
      {
        "action": "reduce",
        "symbol": "ETHUSDT",
        "detail": "空仓减仓 50%，多空比偏多，空头风险增大",
        "priority": "high",
        "risk": "减仓后可能错过下跌"
      },
      {
        "action": "wait",
        "symbol": "SOLUSDT",
        "detail": "信号待确认，等待 4H 收线后再决定",
        "priority": "medium",
        "risk": "可能错过入场时机"
      }
    ]
  }
}
```

### GET /tool/agent/analyze?mode=positions

同上，支持 query 参数 `mode`，默认 `full`。

---

## 8. 实现顺序

1. ✅ `go get` 安装 eino 依赖（已完成）
2. `api/config.go` — 新增 `LLMConfig` 结构体和字段
3. `api/agent_bridge.go` — 新增 4 个导出桥接函数
4. `agent/types.go` — 结构体定义
5. `agent/prompt.go` — System Prompt
6. `agent/tools.go` — Tool 定义（预留，当前版本直接调用桥接函数）
7. `agent/agent.go` — Agent 核心 + HTTP Handler
8. `main.go` — 注册路由 + 初始化
9. `go build ./...` 编译验证
10. `config.json` 添加 llm 配置并测试

---

## 9. 注意事项

- **未导出函数问题**：`analyzePosition`、`calcMarketSentiment`、`loadTradesForAnalytics`、`calcJournalMetrics` 都是 api 包小写函数，agent 包不能直接调用，必须通过桥接函数
- **journalMetrics / journalResponse 也是小写**：需要新增导出类型或在桥接函数中转换
- **Agent 执行方式**：当前方案 A（仅返回步骤清单），不自动执行交易操作
- **LLM 超时**：DeepSeek 响应可能较慢，建议给 Agent 调用设置 60s 超时
- **Token 消耗**：full 模式数据量较大，注意 max_tokens 设置
- **降级方案**：如果 LLM 未配置（api_key 为空），agent 接口返回 503，不影响其他功能
