import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import {
  View,
  Text,
  StyleSheet,
  TouchableOpacity,
  ActivityIndicator,
  Alert,
  Modal,
  ScrollView,
  Linking,
  Share,
  SafeAreaView,
  StatusBar,
  Platform,
} from 'react-native';
import { WebView } from 'react-native-webview';
import { colors, spacing, radius, fontSize } from '../services/theme';
import api, { AUTH_TOKEN, WS_NEWS_BASE } from '../services/api';

const FEED_CATEGORY_LABELS = {
  crypto: '加密',
  global: '国际',
  finance: '财经',
  tech: '科技',
  science: '科学',
  visual: '视觉',
  adult: '成人',
  telegram: 'TG',
  other: '其他',
};

const BASE_FEED_CATEGORY_OVERRIDES = {
  blockbeats: 'crypto',
  '0xzx': 'crypto',
  bbc_world: 'global',
  aljazeera_all: 'global',
  guardian_world: 'global',
  npr_world: 'global',
  cnbc_world: 'finance',
  google_reuters_24h: 'finance',
  reuters_world_all: 'finance',
  reuters_world_us: 'finance',
  jin10: 'finance',
  wallstreetcn_live_global: 'finance',
  wallstreetcn_hot_day: 'finance',
  nature_news: 'science',
  t66y_7: 'adult',
  huggingface_daily_papers: 'tech',
  anthropic_news: 'tech',
  xsijishe_rank_weekly: 'adult',
  jpxgmn_weekly: 'adult',
  hackernews_index: 'tech',
  '36kr_newsflashes': 'tech',
  '1x_latest_awarded': 'visual',
  sspai_index: 'tech',
  pornhub: 'adult',
};

const BASE_FEED_SOURCES = [
  {
    key: 'blockbeats',
    name: 'BlockBeats',
  },
  {
    key: '0xzx',
    name: '0xzx',
  },
  {
    key: 'bbc_world',
    name: 'BBC国际',
  },
  {
    key: 'aljazeera_all',
    name: '半岛电视台',
  },
  {
    key: 'guardian_world',
    name: 'Guardian国际',
  },
  {
    key: 'npr_world',
    name: 'NPR国际',
  },
  {
    key: 'cnbc_world',
    name: 'CNBC国际',
  },
  {
    key: 'google_reuters_24h',
    name: 'Reuters24h',
  },
  {
    key: 'reuters_world_all',
    name: 'Reuters World',
  },
  {
    key: 'reuters_world_us',
    name: 'Reuters US',
  },
  {
    key: 'jin10',
    name: '金十快讯',
  },
  {
    key: 'wallstreetcn_live_global',
    name: '华尔街见闻快讯',
  },
  {
    key: 'wallstreetcn_hot_day',
    name: '华尔街见闻热榜',
  },
  {
    key: 'nature_news',
    name: 'Nature News',
  },
  {
    key: 't66y_7',
    name: 't66y(7)',
  },
  {
    key: 'huggingface_daily_papers',
    name: 'Huggingface Papers',
  },
  {
    key: 'anthropic_news',
    name: 'Anthropic News',
  },
  {
    key: 'xsijishe_rank_weekly',
    name: '司机社周榜',
  },
  {
    key: 'jpxgmn_weekly',
    name: '极品性感美女周榜',
  },
  {
    key: 'hackernews_index',
    name: 'Hacker News',
  },
  {
    key: '36kr_newsflashes',
    name: '36氪快讯',
  },
  {
    key: '1x_latest_awarded',
    name: '1x 每日获奖',
  },
  {
    key: 'sspai_index',
    name: '少数派首页',
  },
  {
    key: 'pornhub',
    name: 'Pornhub - 国产',
  },
  {
    key: 'pornhub_popular_with_women',
    name: 'Pornhub - 女性向热门',
  },
  {
    key: 'pornhub_korean',
    name: 'Pornhub - Korean (103)',
  },
  {
    key: 'pornhub_cosplay',
    name: 'Pornhub - Cosplay (241)',
  },
  {
    key: 'pornhub_asian',
    name: 'Pornhub - Asian (1)',
  },
  {
    key: 'pornhub_pornstar_cn',
    name: 'Pornstar - 中文',
  },
].map((item) => ({
  ...item,
  category: normalizeFeedCategory(item.key, item.name),
}));
const BASE_FEED_NAME_OVERRIDES = {
  blockbeats: 'BlockBeats',
  '0xzx': '0xzx',
  bbc_world: 'BBC国际',
  aljazeera_all: '半岛电视台',
  guardian_world: 'Guardian国际',
  npr_world: 'NPR国际',
  cnbc_world: 'CNBC国际',
  google_reuters_24h: 'Reuters24h',
  reuters_world_all: 'Reuters World',
  reuters_world_us: 'Reuters US',
  jin10: '金十快讯',
  wallstreetcn_live_global: '华尔街见闻快讯',
  wallstreetcn_hot_day: '华尔街见闻热榜',
  nature_news: 'Nature News',
  t66y_7: 't66y(7)',
  huggingface_daily_papers: 'Huggingface Papers',
  anthropic_news: 'Anthropic News',
  xsijishe_rank_weekly: '司机社周榜',
  jpxgmn_weekly: '极品性感美女周榜',
  hackernews_index: 'Hacker News',
  '36kr_newsflashes': '36氪快讯',
  '1x_latest_awarded': '1x 每日获奖',
  sspai_index: '少数派首页',
  pornhub: 'Pornhub - 国产',
  pornhub_popular_with_women: 'Pornhub - 女性向热门',
  pornhub_korean: 'Pornhub - Korean (103)',
  pornhub_cosplay: 'Pornhub - Cosplay (241)',
  pornhub_asian: 'Pornhub - Asian (1)',
  pornhub_pornstar_cn: 'Pornstar - 中文',
};
const BASE_FEED_KEY_SET = new Set(BASE_FEED_SOURCES.map((x) => x.key));
const WS_RECONNECT_MS = 3000;
const WS_PING_MS = 30000;
const SOURCE_STATUS_REFRESH_MS = 60 * 60 * 1000;
const TRANSLATE_ENDPOINT = 'https://translate.googleapis.com/translate_a/single';
const TRANSLATE_MAX_CHARS = 1800;
const TRANSLATE_BATCH_LIMIT = 12;

function buildEmptyNewsBySource(feeds) {
  return feeds.reduce((acc, feed) => {
    acc[feed.key] = [];
    return acc;
  }, {});
}

function isTelegramFeedKey(key) {
  return String(key || '').startsWith('tg_');
}

function isPornFeedKey(key) {
  return String(key || '').startsWith('pornhub');
}

function inferFeedCategory(key, name) {
  const k = String(key || '').toLowerCase();
  const n = String(name || '').toLowerCase();
  if (isTelegramFeedKey(k)) return 'telegram';
  if (k.includes('hackernews') || k.includes('huggingface') || k.includes('sspai') || k.includes('36kr')) return 'tech';
  if (k.includes('reuters') || k.includes('jin10') || k.includes('cnbc') || k.includes('bbc') || k.includes('guardian')) return 'finance';
  if (k.includes('nature')) return 'science';
  if (k.includes('1x') || n.includes('photo') || n.includes('摄影') || n.includes('视觉')) return 'visual';
  if (k.includes('porn') || k.includes('t66y') || k.includes('xsijishe') || k.includes('jpxgmn')) return 'adult';
  if (k.includes('blockbeats') || k.includes('0xzx')) return 'crypto';
  return 'other';
}

function normalizeFeedCategory(key, name) {
  if (BASE_FEED_CATEGORY_OVERRIDES[key]) return BASE_FEED_CATEGORY_OVERRIDES[key];
  return inferFeedCategory(key, name);
}

function formatFeedCategoryLabel(category) {
  return FEED_CATEGORY_LABELS[category] || FEED_CATEGORY_LABELS.other;
}

function normalizeFeedDisplayName(key, name) {
  if (BASE_FEED_NAME_OVERRIDES[key]) return BASE_FEED_NAME_OVERRIDES[key];
  const n = String(name || key || '').trim();
  if (isTelegramFeedKey(key)) {
    if (n.toUpperCase().startsWith('TG ')) return n;
    return `TG ${n}`;
  }
  return n || key;
}

function formatTime(pubDate) {
  if (!pubDate) return '-';
  const date = new Date(pubDate);
  if (Number.isNaN(date.getTime())) return pubDate;
  return date.toLocaleString('zh-CN', {
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    hour12: false,
  });
}

/** 判断内容是否包含 HTML 标签 */
function hasHtmlTags(text) {
  if (!text) return false;
  return /<[a-z][\s\S]*>/i.test(text);
}

/** 生成暗色主题 HTML 包裹页 */
function buildArticleHtml(title, time, htmlContent, link) {
  // 对特殊字符转义，防止注入
  const safeTitle = (title || '').replace(/</g, '&lt;').replace(/>/g, '&gt;');
  const safeTime = (time || '').replace(/</g, '&lt;').replace(/>/g, '&gt;');
  const safeLink = (link || '').replace(/"/g, '&quot;');

  return `<!DOCTYPE html>
<html>
<head>
  <meta charset="utf-8"/>
  <meta name="viewport" content="width=device-width,initial-scale=1,maximum-scale=3"/>
  <style>
    * { box-sizing: border-box; margin: 0; padding: 0; }
    body {
      background: ${colors.bg};
      color: ${colors.text};
      font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif;
      font-size: 15px;
      line-height: 1.7;
      padding: 16px;
      word-wrap: break-word;
      overflow-wrap: break-word;
    }
    .article-title {
      font-size: 20px;
      font-weight: 800;
      color: ${colors.white};
      line-height: 1.4;
      margin-bottom: 8px;
    }
    .article-meta {
      font-size: 12px;
      color: ${colors.textMuted};
      margin-bottom: 16px;
      padding-bottom: 12px;
      border-bottom: 1px solid ${colors.cardBorder};
    }
    .article-body img {
      max-width: 100%;
      height: auto;
      border-radius: 8px;
      margin: 12px 0;
    }
    .article-body p {
      margin-bottom: 12px;
    }
    .article-body a {
      color: ${colors.goldLight};
      text-decoration: none;
    }
    .article-body h1, .article-body h2, .article-body h3,
    .article-body h4, .article-body h5 {
      color: ${colors.white};
      margin: 16px 0 8px;
      font-weight: 700;
    }
    .article-body blockquote {
      border-left: 3px solid ${colors.gold};
      padding-left: 12px;
      margin: 12px 0;
      color: ${colors.textSecondary};
    }
    .article-body pre, .article-body code {
      background: ${colors.surface};
      border-radius: 6px;
      padding: 2px 6px;
      font-size: 13px;
    }
    .article-body pre {
      padding: 12px;
      overflow-x: auto;
    }
    .article-body table {
      width: 100%;
      border-collapse: collapse;
      margin: 12px 0;
    }
    .article-body th, .article-body td {
      border: 1px solid ${colors.cardBorder};
      padding: 8px;
      text-align: left;
      font-size: 13px;
    }
    .article-body th {
      background: ${colors.surface};
      color: ${colors.white};
    }
    .article-body ul, .article-body ol {
      padding-left: 20px;
      margin-bottom: 12px;
    }
    .article-body li {
      margin-bottom: 4px;
    }
    .article-body figure {
      margin: 12px 0;
    }
    .article-body figcaption {
      font-size: 12px;
      color: ${colors.textMuted};
      text-align: center;
      margin-top: 4px;
    }
    .article-body iframe {
      max-width: 100%;
    }
    .source-link {
      display: block;
      margin-top: 20px;
      padding-top: 12px;
      border-top: 1px solid ${colors.cardBorder};
      font-size: 12px;
      color: ${colors.textMuted};
    }
    .source-link a {
      color: ${colors.goldLight};
    }
  </style>
</head>
<body>
  <div class="article-title">${safeTitle}</div>
  <div class="article-meta">${safeTime}</div>
  <div class="article-body">${htmlContent || '<p>暂无内容</p>'}</div>
  ${safeLink ? `<div class="source-link">原文链接: <a href="${safeLink}">${safeLink}</a></div>` : ''}
</body>
</html>`;
}

/** 剥离 HTML 标签，仅取纯文本（给列表卡片用） */
function stripHtml(html) {
  if (!html) return '';
  return html
    .replace(/<br\s*\/?>/gi, '\n')
    .replace(/<\/p>/gi, '\n')
    .replace(/<[^>]+>/g, '')
    .replace(/&nbsp;/g, ' ')
    .replace(/&amp;/g, '&')
    .replace(/&lt;/g, '<')
    .replace(/&gt;/g, '>')
    .replace(/&quot;/g, '"')
    .replace(/&#39;/g, "'")
    .replace(/\n{3,}/g, '\n\n')
    .trim();
}

function getNewsItemKey(item) {
  return `${item?.link || item?.id || item?.title || '-'}::${item?.pubDate || '-'}`;
}

function containsChinese(text) {
  return /[\u4e00-\u9fff]/.test(text || '');
}

function isLikelyEnglish(text) {
  const t = String(text || '').trim();
  if (!t) return false;
  const letters = (t.match(/[A-Za-z]/g) || []).length;
  const cjk = (t.match(/[\u4e00-\u9fff]/g) || []).length;
  return letters >= 8 && letters > cjk * 3;
}

function languageLabel(langCode) {
  const code = String(langCode || '').toLowerCase();
  if (code.startsWith('en')) return '英文';
  if (code.startsWith('zh')) return '中文';
  if (code.startsWith('ja')) return '日文';
  if (code.startsWith('ko')) return '韩文';
  if (code.startsWith('fr')) return '法文';
  if (code.startsWith('de')) return '德文';
  if (code.startsWith('es')) return '西班牙文';
  if (code.startsWith('ru')) return '俄文';
  return '源语言';
}

function parseTranslateResponse(payload) {
  if (!Array.isArray(payload) || !Array.isArray(payload[0])) return { translated: '', sourceLang: '' };
  const translated = payload[0].map((seg) => (Array.isArray(seg) ? (seg[0] || '') : '')).join('').trim();
  const sourceLang = typeof payload[2] === 'string' ? payload[2] : '';
  return { translated, sourceLang };
}

async function translateTextToZh(text) {
  const raw = String(text || '').trim();
  if (!raw) return { translated: '', sourceLang: '' };
  const truncated = raw.length > TRANSLATE_MAX_CHARS ? raw.slice(0, TRANSLATE_MAX_CHARS) : raw;
  const url = `${TRANSLATE_ENDPOINT}?client=gtx&sl=auto&tl=zh-CN&dt=t&q=${encodeURIComponent(truncated)}`;
  const res = await fetch(url);
  if (!res.ok) {
    throw new Error(`翻译请求失败(${res.status})`);
  }
  const data = await res.json();
  return parseTranslateResponse(data);
}

export default function NewsPanel({ onHasNew }) {
  const [feedSources, setFeedSources] = useState(BASE_FEED_SOURCES);
  const [loading, setLoading] = useState(true);
  const [refreshing, setRefreshing] = useState(false);
  const [error, setError] = useState('');
  const [wsConnected, setWsConnected] = useState(false);
  const [newsBySource, setNewsBySource] = useState(buildEmptyNewsBySource(BASE_FEED_SOURCES));
  const [activeSourceKey, setActiveSourceKey] = useState(BASE_FEED_SOURCES[0].key);
  const [activeFeedGroup, setActiveFeedGroup] = useState('main');
  const [selected, setSelected] = useState(null);
  const [showSourceLang, setShowSourceLang] = useState(false);
  const [translationMap, setTranslationMap] = useState({});

  const wsRef = useRef(null);
  const pingTimerRef = useRef(null);
  const reconnectTimerRef = useRef(null);
  const translatingKeysRef = useRef(new Set());
  const mountedRef = useRef(false);
  const closedByUserRef = useRef(false);
  const initializedRef = useRef(false);
  const latestTopKeyRef = useRef({});

  const clearWsTimers = useCallback(() => {
    if (pingTimerRef.current) {
      clearInterval(pingTimerRef.current);
      pingTimerRef.current = null;
    }
    if (reconnectTimerRef.current) {
      clearTimeout(reconnectTimerRef.current);
      reconnectTimerRef.current = null;
    }
  }, []);

  const sendWs = useCallback((payload) => {
    if (!wsRef.current || wsRef.current.readyState !== 1) return;
    wsRef.current.send(JSON.stringify(payload));
  }, []);

  const loadSourceStatus = useCallback(async () => {
    try {
      const res = await api.getNewsSourceStatus();
      const report = res?.data || res;
      const items = Array.isArray(report?.sources) ? report.sources : [];
      if (items.length === 0) return;

      const statusByKey = new Map(items.map((x) => [x.key, x]));
      const fixedSources = BASE_FEED_SOURCES.filter((feed) => {
        const status = statusByKey.get(feed.key);
        if (!status) return true;
        return !!status.reachable;
      }).map((feed) => ({
        ...feed,
        name: normalizeFeedDisplayName(feed.key, feed.name),
        category: normalizeFeedCategory(feed.key, feed.name),
      }));

      const extraSources = items
        .filter((item) => item && item.key && !BASE_FEED_KEY_SET.has(item.key))
        .filter((item) => item.reachable || isTelegramFeedKey(item.key))
        .map((item) => ({
          key: item.key,
          name: normalizeFeedDisplayName(item.key, item.name),
          category: normalizeFeedCategory(item.key, item.name),
        }));

      const nextSources = [...fixedSources, ...extraSources];
      if (nextSources.length > 0) setFeedSources(nextSources);
    } catch (_) {}
  }, []);

  useEffect(() => {
    loadSourceStatus();
    const timer = setInterval(loadSourceStatus, SOURCE_STATUS_REFRESH_MS);
    return () => clearInterval(timer);
  }, [loadSourceStatus]);

  useEffect(() => {
    setNewsBySource((prev) => {
      const next = buildEmptyNewsBySource(feedSources);
      feedSources.forEach((feed) => {
        next[feed.key] = prev[feed.key] || [];
      });
      return next;
    });
    setActiveSourceKey((prev) => {
      if (feedSources.some((f) => f.key === prev)) return prev;
      return feedSources[0]?.key || prev;
    });
    const nextTop = {};
    feedSources.forEach((feed) => {
      nextTop[feed.key] = latestTopKeyRef.current[feed.key] || '';
    });
    latestTopKeyRef.current = nextTop;
  }, [feedSources]);

  const hasPornFeeds = useMemo(
    () => feedSources.some((feed) => isPornFeedKey(feed.key)),
    [feedSources],
  );
  const hasMainFeeds = useMemo(
    () => feedSources.some((feed) => !isPornFeedKey(feed.key)),
    [feedSources],
  );
  const visibleFeedSources = useMemo(() => {
    if (activeFeedGroup === 'porn') {
      return feedSources.filter((feed) => isPornFeedKey(feed.key));
    }
    return feedSources.filter((feed) => !isPornFeedKey(feed.key));
  }, [activeFeedGroup, feedSources]);

  useEffect(() => {
    if (activeFeedGroup === 'porn' && !hasPornFeeds) {
      setActiveFeedGroup('main');
      return;
    }
    if (activeFeedGroup === 'main' && !hasMainFeeds && hasPornFeeds) {
      setActiveFeedGroup('porn');
    }
  }, [activeFeedGroup, hasMainFeeds, hasPornFeeds]);

  useEffect(() => {
    if (visibleFeedSources.length === 0) return;
    if (visibleFeedSources.some((f) => f.key === activeSourceKey)) return;
    setActiveSourceKey(visibleFeedSources[0].key);
  }, [activeSourceKey, visibleFeedSources]);

  const applyNewsPayload = useCallback((payload = {}) => {
    const nextNewsBySource = buildEmptyNewsBySource(feedSources);
    feedSources.forEach((feed) => {
      nextNewsBySource[feed.key] = Array.isArray(payload.data?.[feed.key]) ? payload.data[feed.key] : [];
    });

    const nextTopKeys = {};
    feedSources.forEach((feed) => {
      const top = (nextNewsBySource[feed.key] || [])[0];
      nextTopKeys[feed.key] = top ? `${top.link || top.id || top.title || '-'}::${top.pubDate || '-'}` : '';
    });

    if (!initializedRef.current) {
      initializedRef.current = true;
    } else {
      const hasNew = feedSources.some((feed) => {
        const prevKey = latestTopKeyRef.current[feed.key] || '';
        const nextKey = nextTopKeys[feed.key] || '';
        return prevKey && nextKey && prevKey !== nextKey;
      });
      if (hasNew) onHasNew?.(true);
    }
    latestTopKeyRef.current = nextTopKeys;

    setNewsBySource(nextNewsBySource);
    const totalCount = Object.values(nextNewsBySource).reduce((sum, list) => sum + list.length, 0);
    if (payload.error) {
      setError(`拉取失败：${payload.error}`);
    } else if (totalCount === 0) {
      setError('暂无资讯');
    } else if (Array.isArray(payload.failures) && payload.failures.length > 0) {
      setError(`部分源拉取失败：${payload.failures.join(' | ')}`);
    } else {
      setError('');
    }

    setLoading(false);
    setRefreshing(false);
  }, [feedSources, onHasNew]);

  const requestRefresh = useCallback(() => {
    setRefreshing(true);
    sendWs({ action: 'refresh' });
  }, [sendWs]);

  const connectWs = useCallback(() => {
    if (!mountedRef.current) return;
    if (wsRef.current && (wsRef.current.readyState === 0 || wsRef.current.readyState === 1)) return;

    const ws = new WebSocket(`${WS_NEWS_BASE}?token=${AUTH_TOKEN}`);
    wsRef.current = ws;

    ws.onopen = () => {
      setWsConnected(true);
      clearWsTimers();
      sendWs({ action: 'refresh' });
      pingTimerRef.current = setInterval(() => {
        sendWs({ action: 'ping' });
      }, WS_PING_MS);
    };

    ws.onmessage = (event) => {
      let msg;
      try {
        msg = JSON.parse(event.data);
      } catch (_) {
        return;
      }

      if (!msg || typeof msg !== 'object') return;
      if (msg.action === 'pong') return;

      if (msg.channel === 'news') {
        applyNewsPayload(msg);
      }
    };

    ws.onerror = () => {
      setWsConnected(false);
    };

    ws.onclose = () => {
      setWsConnected(false);
      clearWsTimers();
      wsRef.current = null;
      if (!mountedRef.current || closedByUserRef.current) return;
      reconnectTimerRef.current = setTimeout(() => {
        connectWs();
      }, WS_RECONNECT_MS);
    };
  }, [applyNewsPayload, clearWsTimers, sendWs]);

  useEffect(() => {
    mountedRef.current = true;
    closedByUserRef.current = false;
    setLoading(true);
    setError('');
    connectWs();

    return () => {
      mountedRef.current = false;
      closedByUserRef.current = true;
      clearWsTimers();
      if (wsRef.current) {
        try {
          wsRef.current.close();
        } catch (_) {}
        wsRef.current = null;
      }
    };
  }, [clearWsTimers, connectWs]);

  const onRefresh = () => {
    requestRefresh();
  };

  const activeFeed = visibleFeedSources.find((item) => item.key === activeSourceKey)
    || visibleFeedSources[0]
    || feedSources[0]
    || BASE_FEED_SOURCES[0];
  const activeList = activeFeed ? (newsBySource[activeFeed.key] || []) : [];
  const hiddenCount = BASE_FEED_SOURCES.length - feedSources.length;
  const showGroupSwitch = hasPornFeeds && hasMainFeeds;
  const sourceCountByKey = useMemo(() => {
    const out = {};
    feedSources.forEach((feed) => {
      const list = newsBySource[feed.key];
      out[feed.key] = Array.isArray(list) ? list.length : 0;
    });
    return out;
  }, [feedSources, newsBySource]);
  const pornFeedCount = useMemo(
    () => feedSources.filter((feed) => isPornFeedKey(feed.key)).length,
    [feedSources],
  );
  const mainFeedCount = useMemo(
    () => feedSources.filter((feed) => !isPornFeedKey(feed.key)).length,
    [feedSources],
  );
  const pornItemCount = useMemo(
    () => feedSources
      .filter((feed) => isPornFeedKey(feed.key))
      .reduce((sum, feed) => sum + (sourceCountByKey[feed.key] || 0), 0),
    [feedSources, sourceCountByKey],
  );
  const mainItemCount = useMemo(
    () => feedSources
      .filter((feed) => !isPornFeedKey(feed.key))
      .reduce((sum, feed) => sum + (sourceCountByKey[feed.key] || 0), 0),
    [feedSources, sourceCountByKey],
  );

  const openExternal = async (url) => {
    if (!url) return;
    try {
      await Linking.openURL(url);
    } catch (e) {
      Alert.alert('打开失败', e.message);
    }
  };

  const openSystemTranslate = useCallback(async (text) => {
    const payload = String(text || '').trim();
    if (!payload) {
      Alert.alert('提示', '暂无可翻译内容');
      return;
    }

    // Android 优先调用系统文本处理（包含厂商翻译能力）
    if (Platform.OS === 'android' && typeof Linking.sendIntent === 'function') {
      try {
        await Linking.sendIntent('android.intent.action.PROCESS_TEXT', [
          { key: 'android.intent.extra.PROCESS_TEXT', value: payload },
          { key: 'android.intent.extra.PROCESS_TEXT_READONLY', value: true },
        ]);
        return;
      } catch (_) {
        // 忽略并走分享面板兜底
      }
    }

    // iOS/其他平台兜底：系统分享面板（通常可选翻译）
    try {
      await Share.share({
        title: '翻译文本',
        message: payload,
      });
    } catch (e) {
      Alert.alert('打开翻译失败', e.message);
    }
  }, []);

  const selectedKey = useMemo(
    () => (selected ? getNewsItemKey(selected) : ''),
    [selected],
  );
  const selectedTranslate = selectedKey ? translationMap[selectedKey] : null;
  const selectedHasTranslation = !!(
    selectedTranslate && (selectedTranslate.titleZh || selectedTranslate.summaryZh)
  );
  const selectedSourceLangLabel = languageLabel(selectedTranslate?.sourceLang || 'en');

  const getPlainSummary = useCallback(
    (item) => {
      const raw = item?.summary || '';
      return hasHtmlTags(raw) ? stripHtml(raw) : raw;
    },
    [],
  );

  const shouldAutoTranslate = useCallback(
    (item) => {
      const text = `${item?.title || ''}\n${getPlainSummary(item)}`;
      if (!text.trim()) return false;
      if (containsChinese(text)) return false;
      return isLikelyEnglish(text);
    },
    [getPlainSummary],
  );

  useEffect(() => {
    setShowSourceLang(false);
  }, [selectedKey]);

  useEffect(() => {
    const list = Array.isArray(activeList) ? activeList.slice(0, TRANSLATE_BATCH_LIMIT) : [];
    list.forEach((item) => {
      if (!shouldAutoTranslate(item)) return;
      const key = getNewsItemKey(item);
      const cached = translationMap[key];
      if (cached?.done || cached?.failed || cached?.loading || translatingKeysRef.current.has(key)) return;

      translatingKeysRef.current.add(key);
      setTranslationMap((prev) => ({
        ...prev,
        [key]: { ...(prev[key] || {}), loading: true, done: false, failed: false },
      }));

      (async () => {
        try {
          const titleRes = await translateTextToZh(item.title || '');
          const summaryRes = await translateTextToZh(getPlainSummary(item));
          const sourceLang = titleRes.sourceLang || summaryRes.sourceLang || 'en';
          if (!mountedRef.current) return;
          setTranslationMap((prev) => ({
            ...prev,
            [key]: {
              titleZh: titleRes.translated || '',
              summaryZh: summaryRes.translated || '',
              sourceLang,
              loading: false,
              done: true,
              failed: false,
            },
          }));
        } catch (_e) {
          if (!mountedRef.current) return;
          setTranslationMap((prev) => ({
            ...prev,
            [key]: { ...(prev[key] || {}), loading: false, done: false, failed: true },
          }));
        } finally {
          translatingKeysRef.current.delete(key);
        }
      })();
    });
  }, [activeList, getPlainSummary, shouldAutoTranslate, translationMap]);

  const getDisplayTitle = useCallback(
    (item, preferSourceLanguage = false) => {
      const key = getNewsItemKey(item);
      const t = translationMap[key];
      if (preferSourceLanguage) return item?.title || '暂无标题';
      return t?.titleZh || item?.title || '暂无标题';
    },
    [translationMap],
  );

  const getDisplaySummary = useCallback(
    (item, preferSourceLanguage = false) => {
      const key = getNewsItemKey(item);
      const t = translationMap[key];
      if (preferSourceLanguage) return getPlainSummary(item) || '暂无摘要';
      return t?.summaryZh || getPlainSummary(item) || '暂无摘要';
    },
    [getPlainSummary, translationMap],
  );

  // 当前选中文章是否有 HTML 富文本内容
  const selectedHasHtml = useMemo(
    () => selected && hasHtmlTags(selected.summary),
    [selected],
  );

  // 为 WebView 构建本地 HTML
  const articleHtml = useMemo(() => {
    if (!selected || !selectedHasHtml) return '';
    return buildArticleHtml(
      getDisplayTitle(selected, true),
      formatTime(selected.pubDate),
      selected.summary,
      selected.link,
    );
  }, [getDisplayTitle, selected, selectedHasHtml]);

  return (
    <View style={styles.card}>
      <View style={styles.header}>
        <Text style={styles.title}>快讯切卡</Text>
        <TouchableOpacity onPress={onRefresh} style={styles.refreshBtn}>
          <Text style={styles.refreshText}>{refreshing ? '刷新中...' : '刷新'}</Text>
        </TouchableOpacity>
      </View>
      <Text style={styles.hintText}>
        连接状态: {wsConnected ? '已连接' : '重连中'} | 点击刷新触发服务端拉取
        {hiddenCount > 0 ? ` | 已隐藏不可用源 ${hiddenCount} 个` : ''}
      </Text>

      {loading ? (
        <View style={styles.loadingBox}>
          <ActivityIndicator color={colors.gold} />
          <Text style={styles.loadingText}>加载资讯中...</Text>
        </View>
      ) : (
        <>
          {error ? <Text style={styles.errorText}>{error}</Text> : null}
          {showGroupSwitch ? (
            <View style={styles.groupRowWrap}>
              <TouchableOpacity
                style={[styles.groupBtn, activeFeedGroup === 'main' && styles.groupBtnActive]}
                onPress={() => setActiveFeedGroup('main')}
                activeOpacity={0.85}
              >
                <Text style={[styles.groupBtnText, activeFeedGroup === 'main' && styles.groupBtnTextActive]}>
                  主资讯
                </Text>
                <View style={[styles.groupBadge, activeFeedGroup === 'main' && styles.groupBadgeActive]}>
                  <Text style={[styles.groupBadgeText, activeFeedGroup === 'main' && styles.groupBadgeTextActive]}>
                    {mainFeedCount}/{mainItemCount}
                  </Text>
                </View>
              </TouchableOpacity>
              <TouchableOpacity
                style={[styles.groupBtn, activeFeedGroup === 'porn' && styles.groupBtnActive]}
                onPress={() => setActiveFeedGroup('porn')}
                activeOpacity={0.85}
              >
                <Text style={[styles.groupBtnText, activeFeedGroup === 'porn' && styles.groupBtnTextActive]}>
                  Porn
                </Text>
                <View style={[styles.groupBadge, activeFeedGroup === 'porn' && styles.groupBadgeActive]}>
                  <Text style={[styles.groupBadgeText, activeFeedGroup === 'porn' && styles.groupBadgeTextActive]}>
                    {pornFeedCount}/{pornItemCount}
                  </Text>
                </View>
              </TouchableOpacity>
            </View>
          ) : null}
          <View style={styles.tabRowWrap}>
            <ScrollView
              horizontal
              showsHorizontalScrollIndicator={false}
              contentContainerStyle={styles.tabRow}
            >
              {visibleFeedSources.map((feed) => {
                const isActive = activeSourceKey === feed.key;
                const count = sourceCountByKey[feed.key] || 0;
                const categoryLabel = formatFeedCategoryLabel(feed.category);
                return (
                  <TouchableOpacity
                    key={feed.key}
                    style={[styles.tabBtn, isActive && styles.tabBtnActive]}
                    onPress={() => setActiveSourceKey(feed.key)}
                    activeOpacity={0.8}
                  >
                    <Text style={[styles.tabText, isActive && styles.tabTextActive]} numberOfLines={1}>
                      {feed.name}
                    </Text>
                    <View style={[styles.tabCategoryBadge, isActive && styles.tabCategoryBadgeActive]}>
                      <Text style={[styles.tabCategoryBadgeText, isActive && styles.tabCategoryBadgeTextActive]}>
                        {categoryLabel}
                      </Text>
                    </View>
                    <View style={[styles.tabBadge, isActive && styles.tabBadgeActive]}>
                      <Text style={[styles.tabBadgeText, isActive && styles.tabBadgeTextActive]}>
                        {count}
                      </Text>
                    </View>
                  </TouchableOpacity>
                );
              })}
            </ScrollView>
          </View>
          <View style={styles.sectionHeader}>
            <View style={styles.sectionLeft}>
              <Text style={styles.sectionTitle}>{activeFeed.name}</Text>
              <View style={styles.sectionCategoryBadge}>
                <Text style={styles.sectionCategoryText}>
                  {formatFeedCategoryLabel(activeFeed.category)}
                </Text>
              </View>
            </View>
            <Text style={styles.sectionCount}>{activeList.length} 条 · 分组 {visibleFeedSources.length} 源</Text>
          </View>
          {activeList.length === 0 ? (
            <View style={styles.emptyBox}>
              <Text style={styles.emptyText}>暂无资讯</Text>
            </View>
          ) : (
            activeList.map((item) => (
              <TouchableOpacity
                key={`${activeFeed.key}-${getNewsItemKey(item)}`}
                style={styles.newsCard}
                onPress={() => {
                  setSelected(item);
                  setShowSourceLang(false);
                }}
                activeOpacity={0.7}
              >
                <Text style={styles.newsTitle} numberOfLines={2}>{getDisplayTitle(item)}</Text>
                <Text style={styles.newsSummary} numberOfLines={2}>
                  {getDisplaySummary(item)}
                </Text>
                <View style={styles.metaRow}>
                  <Text style={[styles.meta, styles.metaSource]} numberOfLines={1}>{item.source}</Text>
                  <Text style={[styles.meta, styles.metaTime]}>{formatTime(item.pubDate)}</Text>
                </View>
              </TouchableOpacity>
            ))
          )}
        </>
      )}

      {/* ===== 纯文本详情弹窗（BlockBeats 等无 HTML 的源） ===== */}
      <Modal
        visible={!!selected && !selectedHasHtml}
        transparent
        animationType="slide"
        onRequestClose={() => setSelected(null)}
      >
        <View style={styles.modalOverlay}>
          <View style={styles.modalCard}>
            {selected ? (
              <>
                <Text style={styles.modalTitle}>{getDisplayTitle(selected, showSourceLang)}</Text>
                <Text style={styles.modalTime}>{formatTime(selected.pubDate)}</Text>
                <ScrollView style={styles.modalBody}>
                  <Text style={styles.modalSummary}>{getDisplaySummary(selected, showSourceLang)}</Text>
                </ScrollView>
                <View style={styles.modalActions}>
                  <TouchableOpacity style={styles.modalBtn} onPress={() => setSelected(null)}>
                    <Text style={styles.modalBtnText}>关闭</Text>
                  </TouchableOpacity>
                  {selectedHasTranslation ? (
                    <TouchableOpacity
                      style={styles.modalBtn}
                      onPress={() => setShowSourceLang((prev) => !prev)}
                    >
                      <Text style={styles.modalBtnText}>
                        {showSourceLang ? '查看中文' : `查看${selectedSourceLangLabel}`}
                      </Text>
                    </TouchableOpacity>
                  ) : null}
                  <TouchableOpacity
                    style={styles.modalBtn}
                    onPress={() => openSystemTranslate(`${getDisplayTitle(selected, showSourceLang)}\n\n${getDisplaySummary(selected, showSourceLang)}`)}
                  >
                    <Text style={styles.modalBtnText}>系统翻译</Text>
                  </TouchableOpacity>
                  {selected.link ? (
                    <TouchableOpacity
                      style={[styles.modalBtn, styles.modalBtnPrimary]}
                      onPress={() => openExternal(selected.link)}
                    >
                      <Text style={[styles.modalBtnText, styles.modalBtnTextPrimary]}>打开链接</Text>
                    </TouchableOpacity>
                  ) : null}
                </View>
              </>
            ) : null}
          </View>
        </View>
      </Modal>

      {/* ===== HTML 富文本详情（0xzx 等含 HTML 的源 — 本地渲染，无需跳转） ===== */}
      <Modal
        visible={!!selected && selectedHasHtml}
        animationType="slide"
        onRequestClose={() => setSelected(null)}
      >
        <SafeAreaView style={styles.articleContainer}>
          {/* 顶部导航栏 */}
          <View style={styles.articleHeader}>
            <TouchableOpacity style={styles.articleBackBtn} onPress={() => setSelected(null)}>
              <Text style={styles.articleBackText}>✕ 返回</Text>
            </TouchableOpacity>
            <Text style={styles.articleHeaderTitle} numberOfLines={1}>
              {selected?.source || '文章详情'}
            </Text>
            <View style={styles.articleHeaderActions}>
              {selectedHasTranslation ? (
                <TouchableOpacity
                  style={styles.articleToggleBtn}
                  onPress={() => setShowSourceLang((prev) => !prev)}
                >
                  <Text style={styles.articleToggleText}>
                    {showSourceLang ? '查看中文' : `查看${selectedSourceLangLabel}`}
                  </Text>
                </TouchableOpacity>
              ) : null}
              <TouchableOpacity
                style={styles.articleToggleBtn}
                onPress={() => openSystemTranslate(`${getDisplayTitle(selected, showSourceLang)}\n\n${getDisplaySummary(selected, showSourceLang)}`)}
              >
                <Text style={styles.articleToggleText}>系统翻译</Text>
              </TouchableOpacity>
              {selected?.link ? (
                <TouchableOpacity
                  style={styles.articleExternalBtn}
                  onPress={() => openExternal(selected.link)}
                >
                  <Text style={styles.articleExternalText}>浏览器</Text>
                </TouchableOpacity>
              ) : null}
            </View>
          </View>

          {/* 默认显示中文翻译，切换后显示源语言 HTML */}
          {!showSourceLang && selectedHasTranslation ? (
            <ScrollView style={styles.articleTranslatedWrap} contentContainerStyle={styles.articleTranslatedContent}>
              <Text style={styles.articleTranslatedTitle}>{getDisplayTitle(selected, false)}</Text>
              <Text style={styles.articleTranslatedTime}>{formatTime(selected?.pubDate)}</Text>
              <Text style={styles.articleTranslatedSummary}>{getDisplaySummary(selected, false)}</Text>
            </ScrollView>
          ) : articleHtml ? (
            <WebView
              originWhitelist={['*']}
              source={{ html: articleHtml }}
              style={styles.articleWebView}
              javaScriptEnabled={false}
              showsVerticalScrollIndicator={false}
              scrollEnabled
              onShouldStartLoadWithRequest={(request) => {
                // 拦截文章内链接 → 用系统浏览器打开，不在 WebView 内跳转
                if (request.url && request.url !== 'about:blank' && !request.url.startsWith('data:')) {
                  openExternal(request.url);
                  return false;
                }
                return true;
              }}
            />
          ) : null}
        </SafeAreaView>
      </Modal>
    </View>
  );
}

const styles = StyleSheet.create({
  card: {
    backgroundColor: colors.card,
    borderRadius: radius.lg,
    borderWidth: 1,
    borderColor: colors.cardBorder,
    padding: spacing.lg,
  },
  header: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'center',
    marginBottom: 10,
  },
  title: {
    fontSize: fontSize.lg,
    fontWeight: '700',
    color: colors.white,
  },
  refreshBtn: {
    borderRadius: radius.pill,
    paddingHorizontal: spacing.md,
    paddingVertical: spacing.xs,
    backgroundColor: colors.goldBg,
  },
  refreshText: {
    color: colors.goldLight,
    fontWeight: '600',
  },
  hintText: {
    color: colors.textSecondary,
    fontSize: fontSize.xs,
    marginBottom: 10,
  },
  newsCard: {
    backgroundColor: colors.surface,
    borderRadius: radius.lg,
    borderWidth: 1,
    borderColor: colors.cardBorder,
    padding: spacing.md,
    marginBottom: spacing.md,
    minHeight: 94,
  },
  newsTitle: {
    color: colors.white,
    fontSize: fontSize.md,
    fontWeight: '700',
    lineHeight: 20,
    marginBottom: 6,
  },
  newsSummary: {
    color: colors.textSecondary,
    fontSize: fontSize.sm,
    lineHeight: 18,
  },
  metaRow: {
    marginTop: 8,
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'center',
    gap: 8,
  },
  meta: {
    color: colors.textSecondary,
    fontSize: 12,
  },
  metaSource: {
    maxWidth: '68%',
    paddingHorizontal: 8,
    paddingVertical: 2,
    borderRadius: radius.pill,
    backgroundColor: colors.card,
    borderWidth: 1,
    borderColor: colors.cardBorder,
  },
  metaTime: {
    color: colors.textMuted,
  },
  loadingBox: {
    borderRadius: 16,
    borderWidth: 1,
    borderColor: colors.cardBorder,
    backgroundColor: colors.surface,
    paddingVertical: 24,
    alignItems: 'center',
    gap: 8,
    marginBottom: 10,
  },
  loadingText: {
    color: colors.textSecondary,
    fontSize: 12,
  },
  errorText: {
    color: colors.redLight,
    fontSize: 12,
    marginBottom: 10,
  },
  groupRowWrap: {
    flexDirection: 'row',
    gap: 8,
    marginBottom: 10,
  },
  groupBtn: {
    flex: 1,
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
    borderRadius: radius.pill,
    borderWidth: 1,
    borderColor: colors.cardBorder,
    backgroundColor: colors.surface,
    paddingHorizontal: spacing.md,
    paddingVertical: 8,
  },
  groupBtnActive: {
    borderColor: colors.gold,
    backgroundColor: colors.goldBg,
  },
  groupBtnText: {
    color: colors.textSecondary,
    fontSize: 12,
    fontWeight: '700',
  },
  groupBtnTextActive: {
    color: colors.white,
  },
  groupBadge: {
    minWidth: 44,
    alignItems: 'center',
    borderRadius: radius.pill,
    borderWidth: 1,
    borderColor: colors.cardBorder,
    backgroundColor: colors.card,
    paddingHorizontal: 8,
    paddingVertical: 1,
  },
  groupBadgeActive: {
    borderColor: colors.gold,
    backgroundColor: colors.gold,
  },
  groupBadgeText: {
    color: colors.textSecondary,
    fontSize: 11,
    fontWeight: '700',
  },
  groupBadgeTextActive: {
    color: colors.white,
  },
  tabRowWrap: {
    marginBottom: 10,
  },
  tabRow: {
    flexDirection: 'row',
    gap: 6,
    paddingRight: 8,
  },
  tabBtn: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 6,
    minWidth: 92,
    backgroundColor: colors.surface,
    borderWidth: 1,
    borderColor: colors.cardBorder,
    borderRadius: radius.pill,
    paddingHorizontal: spacing.sm,
    paddingVertical: 7,
  },
  tabBtnActive: {
    backgroundColor: colors.goldBg,
    borderColor: colors.gold,
  },
  tabText: {
    color: colors.textSecondary,
    fontSize: 12,
    fontWeight: '600',
    maxWidth: 110,
  },
  tabTextActive: {
    color: colors.white,
  },
  tabCategoryBadge: {
    paddingHorizontal: 6,
    paddingVertical: 1,
    borderRadius: radius.pill,
    backgroundColor: colors.card,
    borderWidth: 1,
    borderColor: colors.cardBorder,
  },
  tabCategoryBadgeActive: {
    backgroundColor: 'rgba(255,255,255,0.14)',
    borderColor: 'rgba(255,255,255,0.18)',
  },
  tabCategoryBadgeText: {
    color: colors.textMuted,
    fontSize: 10,
    fontWeight: '700',
  },
  tabCategoryBadgeTextActive: {
    color: colors.white,
  },
  tabBadge: {
    minWidth: 20,
    paddingHorizontal: 6,
    paddingVertical: 1,
    borderRadius: radius.pill,
    backgroundColor: colors.card,
    borderWidth: 1,
    borderColor: colors.cardBorder,
    alignItems: 'center',
  },
  tabBadgeActive: {
    backgroundColor: colors.gold,
    borderColor: colors.gold,
  },
  tabBadgeText: {
    color: colors.textSecondary,
    fontSize: 11,
    fontWeight: '700',
  },
  tabBadgeTextActive: {
    color: colors.white,
  },
  sectionHeader: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'center',
    marginBottom: 8,
  },
  sectionLeft: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 8,
    flexShrink: 1,
    marginRight: 8,
  },
  sectionTitle: {
    color: colors.white,
    fontSize: 14,
    fontWeight: '700',
    flexShrink: 1,
  },
  sectionCategoryBadge: {
    paddingHorizontal: 8,
    paddingVertical: 2,
    borderRadius: radius.pill,
    borderWidth: 1,
    borderColor: colors.cardBorder,
    backgroundColor: colors.surface,
  },
  sectionCategoryText: {
    color: colors.textSecondary,
    fontSize: 11,
    fontWeight: '700',
  },
  sectionCount: {
    color: colors.textSecondary,
    fontSize: 12,
  },
  emptyBox: {
    borderRadius: 16,
    borderWidth: 1,
    borderColor: colors.cardBorder,
    backgroundColor: colors.surface,
    paddingVertical: 24,
    alignItems: 'center',
    marginBottom: 10,
  },
  emptyText: {
    color: colors.textSecondary,
  },

  // ===== 纯文本摘要弹窗 =====
  modalOverlay: {
    flex: 1,
    backgroundColor: 'rgba(0,0,0,0.5)',
    justifyContent: 'center',
    padding: 14,
  },
  modalCard: {
    maxHeight: '75%',
    backgroundColor: colors.card,
    borderRadius: radius.xxl,
    borderWidth: 1,
    borderColor: colors.cardBorder,
    padding: spacing.xl,
  },
  modalTitle: {
    color: colors.white,
    fontSize: 16,
    fontWeight: '700',
    lineHeight: 22,
  },
  modalTime: {
    marginTop: 8,
    color: colors.textSecondary,
    fontSize: 12,
  },
  modalBody: {
    marginTop: 12,
    marginBottom: 12,
  },
  modalSummary: {
    color: colors.text,
    fontSize: 14,
    lineHeight: 21,
  },
  modalActions: {
    flexDirection: 'row',
    gap: 8,
  },
  modalBtn: {
    flex: 1,
    alignItems: 'center',
    paddingVertical: 10,
    borderRadius: 14,
    borderWidth: 1,
    borderColor: colors.cardBorder,
    backgroundColor: colors.surface,
  },
  modalBtnPrimary: {
    backgroundColor: colors.gold,
  },
  modalBtnText: {
    color: colors.text,
    fontWeight: '600',
  },
  modalBtnTextPrimary: {
    color: colors.white,
    fontWeight: '700',
  },

  // ===== HTML 文章全屏阅读器 =====
  articleContainer: {
    flex: 1,
    backgroundColor: colors.bg,
  },
  articleHeader: {
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
    backgroundColor: colors.card,
    borderBottomWidth: 1,
    borderBottomColor: colors.cardBorder,
    paddingHorizontal: spacing.md,
    paddingVertical: spacing.sm,
    paddingTop: Platform.OS === 'android' ? (StatusBar.currentHeight || 0) + spacing.sm : spacing.sm,
  },
  articleBackBtn: {
    paddingVertical: spacing.xs,
    paddingRight: spacing.md,
  },
  articleBackText: {
    color: colors.goldLight,
    fontSize: fontSize.md,
    fontWeight: '600',
  },
  articleHeaderTitle: {
    flex: 1,
    color: colors.textSecondary,
    fontSize: fontSize.sm,
    fontWeight: '600',
    textAlign: 'center',
    marginHorizontal: spacing.sm,
  },
  articleHeaderActions: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: spacing.xs,
    minWidth: 60,
    justifyContent: 'flex-end',
  },
  articleToggleBtn: {
    backgroundColor: colors.surface,
    borderRadius: radius.pill,
    paddingHorizontal: spacing.sm,
    paddingVertical: spacing.xs,
    borderWidth: 1,
    borderColor: colors.cardBorder,
  },
  articleToggleText: {
    color: colors.textSecondary,
    fontSize: fontSize.xs,
    fontWeight: '600',
  },
  articleExternalBtn: {
    backgroundColor: colors.goldBg,
    borderRadius: radius.pill,
    paddingHorizontal: spacing.md,
    paddingVertical: spacing.xs,
  },
  articleExternalText: {
    color: colors.goldLight,
    fontSize: fontSize.sm,
    fontWeight: '600',
  },
  articleWebView: {
    flex: 1,
    backgroundColor: colors.bg,
  },
  articleTranslatedWrap: {
    flex: 1,
    backgroundColor: colors.bg,
  },
  articleTranslatedContent: {
    padding: spacing.lg,
    gap: spacing.sm,
  },
  articleTranslatedTitle: {
    color: colors.white,
    fontSize: fontSize.lg,
    fontWeight: '700',
    lineHeight: 24,
  },
  articleTranslatedTime: {
    color: colors.textMuted,
    fontSize: fontSize.xs,
  },
  articleTranslatedSummary: {
    color: colors.text,
    fontSize: fontSize.sm,
    lineHeight: 22,
  },
});
