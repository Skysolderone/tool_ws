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
  SafeAreaView,
  StatusBar,
  Platform,
} from 'react-native';
import { WebView } from 'react-native-webview';
import { colors, spacing, radius, fontSize } from '../services/theme';
import { AUTH_TOKEN, WS_NEWS_BASE } from '../services/api';

const FEED_SOURCES = [
  {
    key: 'blockbeats',
    name: 'BlockBeats',
  },
  {
    key: '0xzx',
    name: '0xzx',
  },
];
const WS_RECONNECT_MS = 3000;
const WS_PING_MS = 30000;

const EMPTY_NEWS_BY_SOURCE = FEED_SOURCES.reduce((acc, feed) => {
  acc[feed.key] = [];
  return acc;
}, {});

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

export default function NewsPanel({ onHasNew }) {
  const [loading, setLoading] = useState(true);
  const [refreshing, setRefreshing] = useState(false);
  const [error, setError] = useState('');
  const [wsConnected, setWsConnected] = useState(false);
  const [newsBySource, setNewsBySource] = useState(EMPTY_NEWS_BY_SOURCE);
  const [activeSourceKey, setActiveSourceKey] = useState(FEED_SOURCES[0].key);
  const [selected, setSelected] = useState(null);

  const wsRef = useRef(null);
  const pingTimerRef = useRef(null);
  const reconnectTimerRef = useRef(null);
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

  const applyNewsPayload = useCallback((payload = {}) => {
    const nextNewsBySource = { ...EMPTY_NEWS_BY_SOURCE };
    FEED_SOURCES.forEach((feed) => {
      nextNewsBySource[feed.key] = Array.isArray(payload.data?.[feed.key]) ? payload.data[feed.key] : [];
    });

    const nextTopKeys = {};
    FEED_SOURCES.forEach((feed) => {
      const top = (nextNewsBySource[feed.key] || [])[0];
      nextTopKeys[feed.key] = top ? `${top.link || top.id || top.title || '-'}::${top.pubDate || '-'}` : '';
    });

    if (!initializedRef.current) {
      initializedRef.current = true;
    } else {
      const hasNew = FEED_SOURCES.some((feed) => {
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
  }, [onHasNew]);

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

  const activeFeed = FEED_SOURCES.find((item) => item.key === activeSourceKey) || FEED_SOURCES[0];
  const activeList = newsBySource[activeFeed.key] || [];

  const openExternal = async (url) => {
    if (!url) return;
    try {
      await Linking.openURL(url);
    } catch (e) {
      Alert.alert('打开失败', e.message);
    }
  };

  // 当前选中文章是否有 HTML 富文本内容
  const selectedHasHtml = useMemo(
    () => selected && hasHtmlTags(selected.summary),
    [selected],
  );

  // 为 WebView 构建本地 HTML
  const articleHtml = useMemo(() => {
    if (!selected || !selectedHasHtml) return '';
    return buildArticleHtml(
      selected.title,
      formatTime(selected.pubDate),
      selected.summary,
      selected.link,
    );
  }, [selected, selectedHasHtml]);

  return (
    <View style={styles.card}>
      <View style={styles.header}>
        <Text style={styles.title}>快讯切卡</Text>
        <TouchableOpacity onPress={onRefresh} style={styles.refreshBtn}>
          <Text style={styles.refreshText}>{refreshing ? '刷新中...' : '刷新'}</Text>
        </TouchableOpacity>
      </View>
      <Text style={styles.hintText}>连接状态: {wsConnected ? '已连接' : '重连中'} | 点击刷新触发服务端拉取</Text>

      {loading ? (
        <View style={styles.loadingBox}>
          <ActivityIndicator color={colors.gold} />
          <Text style={styles.loadingText}>加载资讯中...</Text>
        </View>
      ) : (
        <>
          {error ? <Text style={styles.errorText}>{error}</Text> : null}
          <View style={styles.tabRow}>
            {FEED_SOURCES.map((feed) => (
              <TouchableOpacity
                key={feed.key}
                style={[styles.tabBtn, activeSourceKey === feed.key && styles.tabBtnActive]}
                onPress={() => setActiveSourceKey(feed.key)}
              >
                <Text style={[styles.tabText, activeSourceKey === feed.key && styles.tabTextActive]}>
                  {feed.name}
                </Text>
              </TouchableOpacity>
            ))}
          </View>
          <View style={styles.sectionHeader}>
            <Text style={styles.sectionTitle}>{activeFeed.name}</Text>
            <Text style={styles.sectionCount}>{activeList.length} 条</Text>
          </View>
          {activeList.length === 0 ? (
            <View style={styles.emptyBox}>
              <Text style={styles.emptyText}>暂无资讯</Text>
            </View>
          ) : (
            activeList.map((item) => (
              <TouchableOpacity
                key={`${activeFeed.key}-${item.id}`}
                style={styles.newsCard}
                onPress={() => setSelected(item)}
                activeOpacity={0.7}
              >
                <Text style={styles.newsTitle} numberOfLines={2}>{item.title}</Text>
                <Text style={styles.newsSummary} numberOfLines={2}>
                  {hasHtmlTags(item.summary) ? stripHtml(item.summary) : (item.summary || '暂无摘要')}
                </Text>
                <View style={styles.metaRow}>
                  <Text style={styles.meta} numberOfLines={1}>{item.source}</Text>
                  <Text style={styles.meta}>{formatTime(item.pubDate)}</Text>
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
                <Text style={styles.modalTitle}>{selected.title}</Text>
                <Text style={styles.modalTime}>{formatTime(selected.pubDate)}</Text>
                <ScrollView style={styles.modalBody}>
                  <Text style={styles.modalSummary}>{selected.summary || '暂无摘要'}</Text>
                </ScrollView>
                <View style={styles.modalActions}>
                  <TouchableOpacity style={styles.modalBtn} onPress={() => setSelected(null)}>
                    <Text style={styles.modalBtnText}>关闭</Text>
                  </TouchableOpacity>
                  {selected.link ? (
                    <TouchableOpacity
                      style={[styles.modalBtn, styles.modalBtnPrimary]}
                      onPress={() => openExternal(selected.link)}
                    >
                      <Text style={[styles.modalBtnText, styles.modalBtnTextPrimary]}>查看原文</Text>
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
            {selected?.link ? (
              <TouchableOpacity
                style={styles.articleExternalBtn}
                onPress={() => openExternal(selected.link)}
              >
                <Text style={styles.articleExternalText}>浏览器</Text>
              </TouchableOpacity>
            ) : (
              <View style={{ width: 60 }} />
            )}
          </View>

          {/* 本地 HTML 渲染 */}
          {articleHtml ? (
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
    padding: spacing.lg,
    marginBottom: spacing.md,
    minHeight: 98,
    shadowColor: colors.shadow,
    shadowRadius: 8,
    shadowOpacity: 0.3,
    shadowOffset: { width: 0, height: 2 },
    elevation: 2,
  },
  newsTitle: {
    color: colors.white,
    fontSize: fontSize.md,
    fontWeight: '700',
    lineHeight: 20,
    marginBottom: 8,
  },
  newsSummary: {
    color: colors.textSecondary,
    fontSize: fontSize.sm,
    lineHeight: 19,
  },
  metaRow: {
    marginTop: 10,
    flexDirection: 'row',
    justifyContent: 'space-between',
  },
  meta: {
    color: colors.textSecondary,
    fontSize: 12,
    maxWidth: '65%',
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
  tabRow: {
    flexDirection: 'row',
    gap: 3,
    marginBottom: 10,
    backgroundColor: colors.surface,
    borderRadius: radius.md,
    padding: 3,
  },
  tabBtn: {
    flex: 1,
    backgroundColor: colors.surface,
    borderRadius: radius.sm,
    alignItems: 'center',
    paddingVertical: spacing.xs,
  },
  tabBtnActive: {
    backgroundColor: colors.goldBg,
  },
  tabText: {
    color: colors.textSecondary,
    fontSize: 13,
    fontWeight: '600',
  },
  tabTextActive: {
    color: colors.white,
  },
  sectionHeader: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'center',
    marginBottom: 8,
  },
  sectionTitle: {
    color: colors.white,
    fontSize: 14,
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
});
