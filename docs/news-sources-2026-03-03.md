# 今日新增新闻源清单（2026-03-03）

本文档记录今天新增到资讯系统（`/ws/news`）的订阅源。

## 1. 配置化 TG 频道

通过 `config.json` 新增配置：

```json
"news": {
  "rsshubBaseUrl": "https://rsshub.umzzz.com",
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
| `reuters_world_us` | `Reuters US` | `/reuters/world/us` |
| `jin10` | `金十快讯` | `/jin10` |
| `hacking8_index` | `Hacking8` | `/hacking8/index` |
| `wsj_zh_cn_world` | `WSJ国际` | `/wsj/zh-cn/world` |
| `bbc_zhongwen` | `BBC中文` | `/bbc/zhongwen` |
| `gamer_gnn` | `巴哈姆特GNN` | `/gamer/gnn` |
| `nature_news` | `Nature News` | `/nature/news` |
| `t66y_7` | `t66y(7)` | `/t66y/7` |
| `gov_zhengce_zuixin` | `国办政策` | `/gov/zhengce/zuixin` |
| `smzdm_haowen_1` | `什么值得买` | `/smzdm/haowen/1` |
| `500px_tribe_set_dailyshot` | `500px每日一拍` | `/500px/tribe/set/f5de0b8aa6d54ec486f5e79616418001` |
| `huggingface_daily_papers` | `Huggingface Papers` | `/huggingface/daily-papers/date` |

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
- https://rsshub.umzzz.com
- https://rss.spriple.org
