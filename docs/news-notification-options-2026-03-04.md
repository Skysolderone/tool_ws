# 新资讯推送通知方案（2026-03-04）

## 1. 目标

当资讯源出现“新内容”时，自动触发通知，尽量做到：

- 快速（秒级）
- 去重（不重复提醒同一条）
- 可控（按来源、关键词、频率过滤）
- 可追踪（日志可排查）

## 2. 你当前项目已有能力

当前代码中已经具备可直接复用的基础能力：

- 资讯聚合与后台抓取：`api/ws_news_hyper_proxy.go`
- 资讯刷新周期：`5s`（`newsRefreshInterval`）
- 去重与排序：`normalizeNewsList`
- 通知通道封装：`SendNotify(...)`（支持 Telegram / 微信）
  - 文件：`api/notify.go`
- 通知配置入口：`Cfg.Notify`
  - 文件：`api/config.go`

结论：你已经有“采集 + 通知通道”，只差“新资讯触发逻辑”这一层。

## 3. 可选方案

## 方案 A：服务端直接推送到 Telegram / 微信（推荐优先）

工作方式：

1. 服务端抓取到新资讯
2. 在后端判断是否首次出现
3. 调用 `SendNotify` 推送到 Telegram/微信

优点：

- 不依赖 App 是否在线
- 延迟低
- 实现成本最低（复用现有 `SendNotify`）

缺点：

- 通知会比较“全局”，需要做好过滤降噪

适用：

- 你自己使用、或小团队运维群接收“新资讯预警”

## 方案 B：移动端本地通知（App 内）

工作方式：

1. App 监听 `/ws/news`
2. 检测到新条目后调用本地通知 API

优点：

- 用户体验好
- 可按设备/用户偏好定制

缺点：

- App 在后台/被系统杀掉时可靠性弱
- iOS/Android 权限、后台策略复杂

适用：

- 只关注 App 前台使用场景

## 方案 C：服务端推送到第三方 Webhook（Slack/飞书/钉钉）

工作方式：

1. 服务端检测到新资讯
2. POST 到目标系统 Webhook

优点：

- 与团队协作工具集成方便
- 可和值班/告警体系打通

缺点：

- 需要维护各平台签名和重试逻辑

适用：

- 团队协作、监控告警场景

## 方案 D：混合方案（推荐长期）

组合策略：

- 关键资讯：服务端 Telegram/微信强通知
- 普通资讯：App 内提示
- 团队协作：可选 Webhook 抄送

优点：

- 覆盖最全，弹性最好

缺点：

- 规则设计稍复杂

## 4. 推荐落地顺序

## 第一步（1 天内可完成）

先做方案 A：

- 在后端增加“新资讯触发器”
- 调用 `SendNotify` 推送
- 增加最小限度的去重和频控

## 第二步

增加过滤规则：

- 仅某些源推送（source whitelist）
- 关键词匹配推送（keyword include/exclude）
- 冷却时间（cooldown）
- 每小时上限（rate limit）

## 第三步

再做 App 本地通知或 Webhook 集成。

## 5. 关键设计建议（避免通知轰炸）

- 去重键：`source + link + pubDate`（或已有 `getNewsItemKey` 逻辑等价键）
- 触发条件：仅“每个源最新一条发生变化”时通知
- 频率控制：
  - 同一源冷却：例如 `120s`
  - 全局每小时上限：例如 `60` 条
- 失败处理：
  - 通知发送失败仅记录日志，不阻塞资讯抓取
- 消息模板统一：
  - 标题、来源、时间、链接、摘要前 120 字

## 6. 建议配置结构（示例）

可在 `config.json` 增加：

```json
{
  "newsNotify": {
    "enabled": true,
    "sources": ["blockbeats", "jin10", "google_reuters_24h"],
    "keywordsInclude": ["ETF", "FOMC", "liquidation", "hack"],
    "keywordsExclude": ["advertisement"],
    "cooldownSec": 120,
    "maxPerHour": 60,
    "minSeverity": "normal"
  }
}
```

说明：

- `enabled=false` 时仅聚合不通知
- `sources` 为空可表示全量来源

## 7. 后端实现挂载点（建议）

建议在 `api/ws_news_hyper_proxy.go` 的 `fetchAndBroadcast()` 中，在获取 `data` 后做：

1. 对比上次快照，识别“新增 top item”
2. 经过过滤器与频控
3. 调用 `SendNotify(msg)`

伪代码：

```go
func (h *newsHub) fetchAndBroadcast() {
    data, failures, err := fetchNewsSnapshot()
    detectAndNotifyNews(data) // 新增
    // 现有广播逻辑...
}
```

## 8. 发送消息模板（示例）

```text
📰 新资讯
来源: BlockBeats
时间: 2026-03-04 15:21
标题: xxxxx
链接: https://...
摘要: xxxxx...
```

## 9. 验收标准

- 同一条资讯不会重复推送
- 新资讯出现后 5~10 秒内收到通知
- 关闭开关后不再发送
- 网络异常时服务不崩溃且可恢复

## 10. 最终建议

短期直接采用“方案 A（服务端 Telegram/微信）”，这是你当前代码改动最小、收益最高的路径。  
如果你希望我继续实现，我建议下一步直接做：

1. 新增 `newsNotify` 配置结构
2. 在 `fetchAndBroadcast` 增加 `detectAndNotifyNews`
3. 加入去重、冷却、每小时限流
4. 提供 `/tool/news/notify/status` 查看统计
