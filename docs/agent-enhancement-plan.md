# Agent 模块扩展与优化计划

## 一、流式输出（SSE）

### 目标
将异步分析的轮询机制改为 SSE 流式推送，用户实时看到分析进度和 LLM 输出。

### 涉及文件
- `agent/agent.go` — 分析主逻辑，改用流式 Generate
- `main.go` — 在主 API 路由组注册 SSE 端点
- `trading-app/src/services/api.js` — 新增 SSE 客户端
- `trading-app/src/components/AIAnalysisPanel.js` — 前端从轮询改为监听流

### 实现步骤

1. **后端：LLM 流式调用**
   - `agent/agent.go` 中新增 `runAnalyzeStream` 函数
   - 使用 eino 的 `Stream()` 方法替代 `Generate()`，逐 token 输出
   - 每个 token chunk 通过 channel 发送

2. **后端：SSE 端点**
   - 在 `main.go` 的 `/tool` 路由组注册 `GET /tool/agent/analyze/stream?token=xxx`
   - Handler 设置 `Content-Type: text/event-stream`
   - 从 channel 读取数据，格式化为 SSE event 推送：
     ```
     event: progress
     data: {"phase":"collecting","detail":"正在获取持仓数据..."}

     event: token
     data: {"text":"根据当前持仓分析..."}

     event: done
     data: {"task_id":123}
     ```
   - 数据收集阶段推送 progress 事件（共 5 个阶段）
   - LLM 生成阶段推送 token 事件
   - 完成后推送 done 事件并关闭连接

3. **前端：SSE 客户端**
   - `api.js` 新增 `analyzeAgentStream(req)` 函数
   - 使用 `fetch` + `ReadableStream` 监听 SSE，解析 `progress/token/done/error` 事件
   - 通过 `handlers` 回调分发事件；取消由 `AbortController`（`requestOptions.signal`）控制

4. **前端：UI 更新**
   - `AIAnalysisPanel.js` 新增 streaming 状态
   - 数据收集阶段显示进度条（5 步）
   - LLM 生成阶段实时渲染文本（类似 ChatGPT 打字效果）
   - 生成完毕后解析 JSON 切换到结构化卡片展示
   - 保留 async 轮询作为降级方案（SSE 连接失败时自动切换）

### 注意事项
- SSE 走主 API 域名（`/tool`），需要 token 认证，通过 query param `?token=xxx` 传递
- 超时仍为 10 分钟，SSE 连接期间每 30s 发送心跳 `:keepalive\n\n`
- 数据库日志仍然正常写入（流式完成后更新 status=SUCCESS）

---

## 二、上下文记忆

### 目标
让 LLM 感知历史分析记录，实现连续性洞察（如"上次建议你平仓你没执行，现在亏损扩大了"）。

### 涉及文件
- `agent/agent.go` — `collectData` 和消息构建逻辑
- `agent/prompt.go` — 系统提示词增加历史上下文指令
- `agent/tools.go` — 新增历史摘要收集函数
- `api/agent_analysis_log.go` — 新增查询最近 N 条成功日志的函数

### 实现步骤

1. **数据库查询层**
   - `api/agent_analysis_log.go` 新增函数：
     ```go
     func GetRecentAnalysisSummaries(limit int) ([]AgentAnalysisLog, error)
     ```
   - 查询最近 N 条 status=SUCCESS 的日志，只取 `mode`、`symbols`、`response_body`（summary + action_items 部分）、`execution_body`、`created_at`

2. **数据收集**
   - `agent/tools.go` 新增 `CollectHistory(limit int)` 函数
   - 从每条历史日志中提取：
     ```json
     {
       "date": "2026-03-03",
       "summary": "建议减仓BTC，设置止损",
       "action_items": [...],
       "executed": true,
       "execution_result": {"success": 2, "failed": 0}
     }
     ```
   - 限制 limit=5，避免 context 过长

3. **消息构建**
   - `agent/agent.go` 的 `collectData` 增加 `history` 维度
   - 在用户消息中新增 `recent_analysis_history` 字段

4. **提示词更新**
   - `agent/prompt.go` 增加指令：
     ```
     ## 历史上下文
     你可以参考 recent_analysis_history 中的最近分析记录。
     - 如果你之前建议的操作用户未执行，评估当前是否仍然有效
     - 如果之前的建议导致亏损，反思原因并调整策略
     - 避免重复给出相同的无效建议
     ```

### 注意事项
- 历史数据做 token 预算控制，每条摘要压缩在 200 字以内
- 如果 DB 查询失败，跳过历史（不阻塞主流程）
- `mock` 模式下不注入历史数据

---

## 三、新闻情绪整合

### 目标
将 BlockBeats + 0xzx 的新闻数据接入 Agent 分析，让 LLM 综合基本面信息做判断。

### 涉及文件
- `api/ws_news_hyper_proxy.go` — 现有新闻缓存，需暴露给 agent
- `api/agent_bridge.go` — 新增新闻数据收集接口
- `agent/tools.go` — 新增新闻收集函数
- `agent/agent.go` — collectData 增加 news 维度
- `agent/prompt.go` — 提示词增加新闻分析指令

### 实现步骤

1. **暴露新闻缓存**
   - `api/ws_news_hyper_proxy.go` 中现有 `cachedBlockbeatsNews` 和 `cached0xzxArticles` 是模块内变量
   - 新增导出函数：
     ```go
     func GetRecentNews(limit int) []NewsDigest
     ```
   - `NewsDigest` 结构：
     ```go
     type NewsDigest struct {
         Source    string    `json:"source"`     // "blockbeats" | "0xzx"
         Title     string    `json:"title"`
         Summary   string    `json:"summary"`    // 截取前 200 字
         Timestamp time.Time `json:"timestamp"`
     }
     ```
   - 合并两个来源，按时间倒序，取最近 limit 条

2. **Bridge 层**
   - `api/agent_bridge.go` 新增：
     ```go
     func GetNewsForAgent(limit int) []NewsDigest
     ```
   - 调用 `GetRecentNews(limit)`，过滤掉超过 24 小时的旧闻

3. **Agent 数据收集**
   - `agent/tools.go` 新增 `CollectNews()` 函数，调用 bridge
   - `agent/agent.go` 的 `collectData` 并发任务中增加 news 收集
   - 在用户消息 JSON 中新增 `recent_news` 字段

4. **提示词更新**
   - `agent/prompt.go` 增加：
     ```
     ## 新闻与事件
     recent_news 包含最近 24 小时的加密货币新闻。
     - 识别可能影响价格的重大事件（监管、黑客、升级、合作）
     - 新闻情绪与技术信号冲突时，明确指出并给出权衡建议
     - 不要过度解读普通新闻，聚焦真正有市场影响力的事件
     ```

5. **输出结构扩展**
   - `agent/types.go` 的 `AnalysisOutput` 新增字段：
     ```go
     NewsImpact []NewsImpactItem `json:"news_impact,omitempty"`
     ```
   - ```go
     type NewsImpactItem struct {
         Title    string `json:"title"`
         Impact   string `json:"impact"`    // "bullish" | "bearish" | "neutral"
         Affected string `json:"affected"`  // 受影响币种
         Comment  string `json:"comment"`
     }
     ```

### 注意事项
- 新闻内容做长度截断（每条 summary ≤ 200 字），控制总 token
- 0xzx 文章是 HTML 全文，需要 strip HTML tags 再截取
- 如果新闻缓存为空（服务刚启动、RSS 拉取失败），跳过此维度

---

## 四、自然语言对话

### 目标
支持用户通过自然语言提问，Agent 根据问题自动选择数据源和分析维度。

### 涉及文件
- `agent/agent.go` — 新增对话模式 Handler
- `agent/prompt.go` — 新增对话模式提示词
- `agent/types.go` — 新增对话请求/响应结构
- `main.go` — 注册新路由
- `trading-app/src/services/api.js` — 新增对话 API
- `trading-app/src/components/AIAnalysisPanel.js` — 新增对话 UI

### 实现步骤

1. **后端：对话请求结构**
   - `agent/types.go` 新增：
     ```go
     type ChatRequest struct {
         Message string   `json:"message"`           // 用户问题
         Symbols []string `json:"symbols,omitempty"`  // 可选：指定币种上下文
     }
     type ChatResponse struct {
         Reply       string       `json:"reply"`                  // 文本回复
         ActionItems []ActionItem `json:"action_items,omitempty"` // 可选：附带操作建议
     }
     ```

2. **后端：对话 Handler**
   - `agent/agent.go` 新增 `HandleChat` 函数
   - 流程：
     1. 解析用户消息，用简单关键词匹配判断需要哪些数据（持仓/信号/新闻/余额）
     2. 只收集相关数据（不像 full mode 全量收集）
     3. 构建对话提示词 + 数据 + 用户问题
     4. 调用 LLM，返回自然语言回复
     5. 如果回复中包含操作建议，解析为 action_items

3. **对话提示词**
   - `agent/prompt.go` 新增 `chatSystemPrompt`：
     ```
     你是一个加密货币合约交易助手。用户会用自然语言向你提问。
     根据提供的实时数据回答用户的问题。
     - 回答要简洁直接，不超过 300 字
     - 如果问题涉及操作建议，以 JSON action_items 格式附在回复末尾
     - 如果数据不足以回答，明确说明
     ```

4. **路由注册**
   - `main.go` 新增：
     ```go
     toolGroup.POST("/agent/chat", agent.HandleChat)
     ```

5. **前端：对话 UI**
   - `AIAnalysisPanel.js` 新增一个 Tab 或模式切换："分析模式" / "对话模式"
   - 对话模式：底部 TextInput + 发送按钮，上方消息气泡列表
   - 消息列表本地存储（不做后端持久化，退出清空）
   - 如果回复中有 action_items，显示可执行的操作建议卡片

### 注意事项
- 对话模式不写入 AgentAnalysisLog（轻量级，无需持久化）
- 对话超时 2 分钟（比分析模式短）
- 关键词匹配逻辑用简单规则即可，不需要额外 LLM 调用：
  - 包含"持仓/仓位/position" → 收集 positions
  - 包含"信号/signal/买/卖" → 收集 signals
  - 包含币种名（BTC/ETH 等）→ 自动填入 symbols
  - 包含"新闻/消息/news" → 收集 news

---

## 五、评估系统升级

### 目标
多时间维度评估 + 自动调参 + 评估结果反馈给 LLM。

### 涉及文件
- `api/agent_evaluation.go` — 核心评估逻辑重构
- `api/agent_bridge.go` — 新增评估摘要供 LLM 使用
- `agent/prompt.go` — 注入评估数据

### 实现步骤

1. **多时间维度评估**
   - `AgentEvaluationRecord` 新增字段：
     ```go
     PnlPct1H   float64 // 1小时后收益率
     PnlPct4H   float64 // 4小时后收益率
     PnlPct24H  float64 // 24小时后收益率
     ```
   - 评估器改为多次评估：
     - 1h 后做首次评估（写入 PnlPct1H）
     - 4h 后更新（写入 PnlPct4H）
     - 24h 后最终评估（写入 PnlPct24H，标记为 final）

2. **评估汇总增强**
   - `GetAgentEvalSummary` 返回结构扩展：
     ```go
     type EvalSummary struct {
         TotalSuggestions int     `json:"total_suggestions"`
         HitRate1H       float64 `json:"hit_rate_1h"`
         HitRate4H       float64 `json:"hit_rate_4h"`
         HitRate24H      float64 `json:"hit_rate_24h"`
         AvgPnl1H        float64 `json:"avg_pnl_1h"`
         AvgPnl24H       float64 `json:"avg_pnl_24h"`
         BestMode         string  `json:"best_mode"`          // 命中率最高的分析模式
         WorstSymbols     []string `json:"worst_symbols"`     // 命中率最低的币种
     }
     ```

3. **自动调参**
   - 新增 `api/agent_auto_tune.go`：
     ```go
     func AutoTuneAgentPolicy(evalSummary EvalSummary)
     ```
   - 规则：
     - 24H 命中率 < 40% 连续 3 天 → 自动切换 execution_profile 为 conservative
     - worst_symbols 连续 7 天表现差 → 加入 config 黑名单
     - 命中率恢复 > 60% → 恢复原有 profile
   - 调参结果写入 config（内存中的 `api.Cfg.Agent`），不修改 config.json 文件
   - 记录调参日志到数据库

4. **评估结果注入 LLM**
   - `agent/prompt.go` 系统提示词动态注入：
     ```
     ## 你的历史表现
     近 7 天命中率：1H {{hit_rate_1h}}% / 24H {{hit_rate_24h}}%
     表现最差币种：{{worst_symbols}}
     请在这些币种上更加谨慎。
     ```

### 数据库变更
- `AgentEvaluationRecord` 表新增 3 个 float64 列（pnl_pct_1h, pnl_pct_4h, pnl_pct_24h）
- 新增 `AgentTuneLog` 表记录自动调参历史
- **注意**：使用 GORM AutoMigrate 自动加列，不会删除现有数据

## 实现优先级

| 优先级 | 功能 | 预估工作量 | 价值 |
|--------|------|-----------|------|
| P0 | 流式输出（SSE） | 中 | 用户体验大幅提升 |
| P0 | 上下文记忆 | 小 | 分析质量提升明显 |
| P0 | 新闻情绪整合 | 小 | 数据维度补全 |
| P1 | 自然语言对话 | 中 | 交互方式升级 |
| P1 | 评估系统升级 | 中 | 形成闭环反馈 |

建议按 P0 → P1 顺序实现。P0 三个功能可以并行开发，互不依赖。
