import React, { useCallback, useEffect, useRef, useState } from 'react';
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
} from 'react-native';
import { colors } from '../services/theme';
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

  const openDetailLink = async () => {
    if (!selected?.link) return;
    try {
      const canOpen = await Linking.canOpenURL(selected.link);
      if (!canOpen) throw new Error('无法打开链接');
      await Linking.openURL(selected.link);
    } catch (e) {
      Alert.alert('打开失败', e.message);
    }
  };

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
          <ActivityIndicator color={colors.blue} />
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
                <Text style={styles.newsSummary} numberOfLines={2}>{item.summary || '暂无摘要'}</Text>
                <View style={styles.metaRow}>
                  <Text style={styles.meta} numberOfLines={1}>{item.source}</Text>
                  <Text style={styles.meta}>{formatTime(item.pubDate)}</Text>
                </View>
              </TouchableOpacity>
            ))
          )}
        </>
      )}

      <Modal visible={!!selected} transparent animationType="slide" onRequestClose={() => setSelected(null)}>
        <View style={styles.modalOverlay}>
          <View style={styles.modalCard}>
            {selected ? (
              <>
                <Text style={styles.modalTitle}>{selected.title}</Text>
                <Text style={styles.modalTime}>{formatTime(selected.pubDate)}</Text>
                <ScrollView style={styles.modalBody}>
                  <Text style={styles.modalSummary}>{selected.summary || '暂无摘要'}</Text>
                  <Text style={styles.modalLink} numberOfLines={2}>
                    {selected.link || '无原文链接'}
                  </Text>
                </ScrollView>
                <View style={styles.modalActions}>
                  <TouchableOpacity style={styles.modalBtn} onPress={() => setSelected(null)}>
                    <Text style={styles.modalBtnText}>关闭</Text>
                  </TouchableOpacity>
                  <TouchableOpacity
                    style={[styles.modalBtn, styles.modalBtnPrimary]}
                    onPress={openDetailLink}
                  >
                    <Text style={[styles.modalBtnText, styles.modalBtnTextPrimary]}>查看原文</Text>
                  </TouchableOpacity>
                </View>
              </>
            ) : null}
          </View>
        </View>
      </Modal>
    </View>
  );
}

const styles = StyleSheet.create({
  card: {
    backgroundColor: colors.card,
    borderRadius: 12,
    borderWidth: 1,
    borderColor: colors.cardBorder,
    padding: 14,
    marginBottom: 14,
  },
  header: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'center',
    marginBottom: 10,
  },
  title: {
    fontSize: 16,
    fontWeight: '700',
    color: colors.white,
  },
  refreshBtn: {
    borderWidth: 1,
    borderColor: colors.cardBorder,
    borderRadius: 8,
    paddingHorizontal: 10,
    paddingVertical: 5,
    backgroundColor: colors.surface,
  },
  refreshText: {
    fontSize: 12,
    color: colors.textSecondary,
  },
  hintText: {
    color: colors.textSecondary,
    fontSize: 12,
    marginBottom: 10,
  },
  newsCard: {
    borderWidth: 1,
    borderColor: colors.cardBorder,
    backgroundColor: colors.surface,
    borderRadius: 10,
    padding: 12,
    marginBottom: 10,
    minHeight: 98,
  },
  newsTitle: {
    color: colors.white,
    fontSize: 15,
    fontWeight: '700',
    lineHeight: 20,
    marginBottom: 8,
  },
  newsSummary: {
    color: colors.text,
    fontSize: 13,
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
    borderRadius: 10,
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
    gap: 8,
    marginBottom: 10,
  },
  tabBtn: {
    flex: 1,
    borderWidth: 1,
    borderColor: colors.cardBorder,
    backgroundColor: colors.surface,
    borderRadius: 8,
    alignItems: 'center',
    paddingVertical: 8,
  },
  tabBtnActive: {
    borderColor: colors.blue,
    backgroundColor: colors.blueBg,
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
    borderRadius: 10,
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
  modalOverlay: {
    flex: 1,
    backgroundColor: 'rgba(0,0,0,0.5)',
    justifyContent: 'center',
    padding: 16,
  },
  modalCard: {
    maxHeight: '75%',
    backgroundColor: colors.card,
    borderRadius: 12,
    borderWidth: 1,
    borderColor: colors.cardBorder,
    padding: 14,
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
  modalLink: {
    marginTop: 12,
    color: colors.blue,
    fontSize: 12,
  },
  modalActions: {
    flexDirection: 'row',
    gap: 8,
  },
  modalBtn: {
    flex: 1,
    alignItems: 'center',
    paddingVertical: 10,
    borderRadius: 8,
    borderWidth: 1,
    borderColor: colors.cardBorder,
    backgroundColor: colors.surface,
  },
  modalBtnPrimary: {
    borderColor: colors.blue,
    backgroundColor: colors.blueBg,
  },
  modalBtnText: {
    color: colors.text,
    fontWeight: '600',
  },
  modalBtnTextPrimary: {
    color: colors.white,
  },
});
