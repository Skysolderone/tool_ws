# 今日新增新闻源清单（2026-03-03）

本文档记录今天新增到资讯系统（`/ws/news`）的订阅源。

## 1. 配置化 TG 频道

通过 `config.json` 新增配置：

```json
"news": {
  "rsshubBaseUrl": "https://rsshub.wws741.top",
  "telegramChannels": [
    "jin10light"
  ]
}
```

- `telegramChannels` 支持：`username`、`@username`、`https://t.me/username`
- 后端会自动转为 RSSHub 路由：`/telegram/channel/:username`
- 前端 tab 自动标记为：`TG @username`

## 2. 今日新增固定 RSSHub 源

| key | tab 名称 | RSSHub 路由 |
|---|---|---|
| `reuters_world_all` | `Reuters World` | `/reuters/world` |
| `reuters_world_us` | `Reuters US` | `/reuters/world/us` |
| `jin10` | `金十快讯` | `/jin10` |
| `wallstreetcn_live_global` | `华尔街见闻快讯` | `/wallstreetcn/live/global/2` |
| `wallstreetcn_hot_day` | `华尔街见闻热榜` | `/wallstreetcn/hot` |
| `nature_news` | `Nature News` | `/nature/news` |
| `t66y_7` | `t66y(7)` | `/t66y/7` |
| `huggingface_daily_papers` | `Huggingface Papers` | `/huggingface/daily-papers/date` |
| `anthropic_news` | `Anthropic News` | `/anthropic/news` |
| `xsijishe_rank_weekly` | `司机社周榜` | `/xsijishe/rank/weekly` |
| `jpxgmn_weekly` | `极品性感美女周榜` | `/jpxgmn/weekly` |
| `hackernews_index` | `Hacker News` | `/hackernews` |
| `36kr_newsflashes` | `36氪快讯` | `/36kr/newsflashes` |
| `1x_latest_awarded` | `1x 每日获奖` | `/1x/latest/awarded` |
| `sspai_index` | `少数派首页` | `/sspai/index` |
| `pornhub` | `Pornhub - 国产` | `/pornhub/search/国产` |
| `pornhub_popular_with_women` | `Pornhub - 女性向热门` | `/pornhub/category/73` |
| `pornhub_korean` | `Pornhub - Korean (103)` | `/pornhub/category/103` |
| `pornhub_cosplay` | `Pornhub - Cosplay (241)` | `/pornhub/category/241` |
| `pornhub_asian` | `Pornhub - Asian (1)` | `/pornhub/category/1` |
| `pornhub_pornstar_cn` | `Pornstar - 中文` | `/pornhub/pornstar/june-liu/cn/mr` |

说明：最终请求地址为 `news.rsshubBaseUrl + 路由`。

## 3. 相关代码位置

- 后端源构建：`api/ws_news_hyper_proxy.go` 中 `buildRSSHubNewsSources` / `buildTelegramNewsSources`
- 配置结构：`api/config.go` 中 `NewsConfig`
- 前端 tab：`trading-app/src/components/NewsPanel.js` 中 `BASE_FEED_SOURCES`

## 4. 用户提供的原始地址

### 4.1 文档与路由页面

- https://docs.rsshub.app/en/guide/
- https://docs.rsshub.app/zh/guide/instances
- https://docs.rsshub.app/routes/reuters
- https://docs.rsshub.app/routes/jin10
- https://docs.rsshub.app/routes/mohw
- https://docs.rsshub.app/routes/wsj
- https://docs.rsshub.app/routes/bbc
- https://docs.rsshub.app/routes/gamer
- https://docs.rsshub.app/routes/nature
- https://docs.rsshub.app/routes/t66y
- https://docs.rsshub.app/routes/gov
- https://docs.rsshub.app/routes/smzdm
- https://docs.rsshub.app/routes/500px
- https://docs.rsshub.app/routes/huggingface

### 4.2 代码链接

- https://github.com/DIYgod/RSSHub/blob/master/lib/routes/hacking8/index.ts

### 4.3 频道与实例地址

- https://t.me/jin10light
- https://rsshub.wws741.top
- https://rss.spriple.org
