import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import {
  View,
  Text,
  StyleSheet,
  TouchableOpacity,
  ActivityIndicator,
  ScrollView,
  TextInput,
  Alert,
  Vibration,
} from 'react-native';
import { colors, spacing, radius, fontSize } from '../services/theme';
import api, { AUTH_TOKEN, WS_LIQUIDATION_BASE } from '../services/api';

const WS_RECONNECT_MS = 3000;
const WS_PING_MS = 30000;
const SNAPSHOT_REFRESH_MS = 30000;
const HISTORY_LIMIT = 120;
const DEFAULT_ALERT_THRESHOLD = 50000000; // 5000万 USD

const EMPTY_LIQ_STATS = Object.freeze({
  daily: [],
  h4: [],
  h1: [],
  timezone: 'UTC',
  updatedAt: 0,
  startedAt: 0,
  lastEventTime: 0,
  eventCount: 0,
  topSymbols: { h1: [], h4: [], day: [] },
});

const EMPTY_HISTORY = Object.freeze({
  daily: [],
  h4: [],
  h1: [],
});

const LIQ_PANEL_CACHE = {
  stats: EMPTY_LIQ_STATS,
  history: EMPTY_HISTORY,
  historyTab: 'h1',
  alertThreshold: DEFAULT_ALERT_THRESHOLD,
  alertInput: String(DEFAULT_ALERT_THRESHOLD / 1e6),
  alertEnabled: true,
  spikeDetected: null,
  lastUpdatedAt: 0,
  lastAlertAt: 0,
};

function toNumber(value) {
  const n = Number(value);
  return Number.isFinite(n) ? n : 0;
}

function fmtUsd(value) {
  const n = toNumber(value);
  if (n >= 1e9) return `${(n / 1e9).toFixed(2)}B`;
  if (n >= 1e6) return `${(n / 1e6).toFixed(1)}M`;
  if (n >= 1e3) return `${(n / 1e3).toFixed(0)}K`;
  return n.toLocaleString('en-US', { maximumFractionDigits: 0 });
}

function fmtUsdFull(value) {
  return toNumber(value).toLocaleString('en-US', { maximumFractionDigits: 0 });
}

function pad2(v) {
  return String(v).padStart(2, '0');
}

function fmtUtcTime(ms) {
  const t = toNumber(ms);
  if (t <= 0) return '-';
  const d = new Date(t);
  return `${d.getUTCFullYear()}-${pad2(d.getUTCMonth() + 1)}-${pad2(d.getUTCDate())} ${pad2(d.getUTCHours())}:${pad2(d.getUTCMinutes())}:${pad2(d.getUTCSeconds())} UTC`;
}

function fmtLocalTime(ms) {
  const t = toNumber(ms);
  if (t <= 0) return '-';
  return new Date(t).toLocaleString('zh-CN', {
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
    hour12: false,
  });
}

function fmtUtcBucketStart(ms, mode = 'h1') {
  const t = toNumber(ms);
  if (t <= 0) return '-';
  const d = new Date(t);
  if (mode === 'day') {
    return `${d.getUTCFullYear()}-${pad2(d.getUTCMonth() + 1)}-${pad2(d.getUTCDate())}`;
  }
  return `${pad2(d.getUTCMonth() + 1)}-${pad2(d.getUTCDate())} ${pad2(d.getUTCHours())}:00`;
}

function fmtLocalBucketStart(ms, mode = 'h1') {
  const t = toNumber(ms);
  if (t <= 0) return '-';
  const d = new Date(t);
  if (mode === 'day') {
    return `${d.getFullYear()}-${pad2(d.getMonth() + 1)}-${pad2(d.getDate())}`;
  }
  return `${pad2(d.getMonth() + 1)}-${pad2(d.getDate())} ${pad2(d.getHours())}:00`;
}

function fmtHour(ms) {
  const t = toNumber(ms);
  if (t <= 0) return '';
  const d = new Date(t);
  return `${pad2(d.getUTCHours())}`;
}

// ========== 多空比例条组件 ==========
function RatioBar({ buyValue, sellValue, height = 6 }) {
  const total = toNumber(buyValue) + toNumber(sellValue);
  if (total <= 0) return null;
  const buyPct = (toNumber(buyValue) / total) * 100;
  const sellPct = 100 - buyPct;
  return (
    <View style={[ratioStyles.wrap, { height }]}>
      <View style={[ratioStyles.buy, { flex: buyPct }]} />
      <View style={[ratioStyles.sell, { flex: sellPct }]} />
    </View>
  );
}

function RatioLabel({ buyValue, sellValue }) {
  const total = toNumber(buyValue) + toNumber(sellValue);
  if (total <= 0) return null;
  const buyPct = ((toNumber(buyValue) / total) * 100).toFixed(1);
  const sellPct = (100 - parseFloat(buyPct)).toFixed(1);
  return (
    <View style={ratioStyles.labelRow}>
      <Text style={ratioStyles.buyLabel}>BUY {buyPct}%</Text>
      <Text style={ratioStyles.sellLabel}>SELL {sellPct}%</Text>
    </View>
  );
}

const ratioStyles = StyleSheet.create({
  wrap: {
    flexDirection: 'row',
    borderRadius: 3,
    overflow: 'hidden',
    marginTop: spacing.xs,
  },
  buy: { backgroundColor: colors.green },
  sell: { backgroundColor: colors.red },
  labelRow: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    marginTop: 2,
  },
  buyLabel: { fontSize: fontSize.xs, color: colors.greenLight, fontWeight: '600' },
  sellLabel: { fontSize: fontSize.xs, color: colors.redLight, fontWeight: '600' },
});

// ========== 趋势柱状图组件 ==========
function TrendBarChart({ data, maxBars = 24 }) {
  const bars = useMemo(() => {
    const items = (data || []).slice(0, maxBars).reverse(); // 从早到晚
    const maxVal = Math.max(...items.map((d) => toNumber(d.totalNotional)), 1);
    return items.map((d) => ({
      value: toNumber(d.totalNotional),
      pct: (toNumber(d.totalNotional) / maxVal) * 100,
      label: fmtHour(d.startTime),
      buyRatio: toNumber(d.buyNotional) / (toNumber(d.totalNotional) || 1),
    }));
  }, [data, maxBars]);

  if (bars.length === 0) {
    return (
      <View style={chartStyles.emptyBox}>
        <Text style={chartStyles.emptyText}>暂无趋势数据</Text>
      </View>
    );
  }

  return (
    <View style={chartStyles.container}>
      <View style={chartStyles.barsRow}>
        {bars.map((bar, idx) => (
          <View key={idx} style={chartStyles.barCol}>
            <View style={chartStyles.barTrack}>
              <View
                style={[
                  chartStyles.barFill,
                  {
                    height: `${Math.max(bar.pct, 2)}%`,
                    backgroundColor: bar.buyRatio > 0.6 ? colors.green : bar.buyRatio < 0.4 ? colors.red : colors.gold,
                  },
                ]}
              />
            </View>
            {idx % 4 === 0 ? (
              <Text style={chartStyles.barLabel}>{bar.label}</Text>
            ) : (
              <Text style={chartStyles.barLabel}> </Text>
            )}
          </View>
        ))}
      </View>
      <View style={chartStyles.legend}>
        <View style={chartStyles.legendItem}>
          <View style={[chartStyles.legendDot, { backgroundColor: colors.green }]} />
          <Text style={chartStyles.legendText}>BUY主导</Text>
        </View>
        <View style={chartStyles.legendItem}>
          <View style={[chartStyles.legendDot, { backgroundColor: colors.red }]} />
          <Text style={chartStyles.legendText}>SELL主导</Text>
        </View>
        <View style={chartStyles.legendItem}>
          <View style={[chartStyles.legendDot, { backgroundColor: colors.gold }]} />
          <Text style={chartStyles.legendText}>均衡</Text>
        </View>
      </View>
    </View>
  );
}

const chartStyles = StyleSheet.create({
  container: {
    backgroundColor: colors.surface,
    borderRadius: radius.md,
    padding: spacing.md,
    marginBottom: spacing.md,
  },
  barsRow: {
    flexDirection: 'row',
    alignItems: 'flex-end',
    height: 100,
    gap: 1,
  },
  barCol: {
    flex: 1,
    alignItems: 'center',
  },
  barTrack: {
    width: '100%',
    height: 80,
    justifyContent: 'flex-end',
  },
  barFill: {
    width: '100%',
    borderRadius: 1.5,
    minHeight: 1,
  },
  barLabel: {
    fontSize: 8,
    color: colors.textMuted,
    marginTop: 2,
  },
  legend: {
    flexDirection: 'row',
    gap: spacing.md,
    marginTop: spacing.sm,
    justifyContent: 'center',
  },
  legendItem: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 4,
  },
  legendDot: {
    width: 8,
    height: 8,
    borderRadius: 4,
  },
  legendText: {
    fontSize: fontSize.xs,
    color: colors.textMuted,
  },
  emptyBox: {
    backgroundColor: colors.surface,
    borderRadius: radius.md,
    paddingVertical: spacing.xl,
    alignItems: 'center',
    marginBottom: spacing.md,
  },
  emptyText: {
    color: colors.textMuted,
    fontSize: fontSize.sm,
  },
});

// ========== 主组件 ==========
export default function LiquidationMonitorPanel({ onHasNew }) {
  const cachedStats = LIQ_PANEL_CACHE.stats || EMPTY_LIQ_STATS;

  const [wsConnected, setWsConnected] = useState(false);
  const [loading, setLoading] = useState(() => !(toNumber(cachedStats.updatedAt) > 0 || toNumber(cachedStats.eventCount) > 0));
  const [error, setError] = useState('');
  const [stats, setStats] = useState(cachedStats);

  const [history, setHistory] = useState(LIQ_PANEL_CACHE.history || EMPTY_HISTORY);
  const [historyLoading, setHistoryLoading] = useState(false);
  const [historyError, setHistoryError] = useState('');
  const [historyTab, setHistoryTab] = useState(LIQ_PANEL_CACHE.historyTab || 'h1');
  const [nowTs, setNowTs] = useState(Date.now());

  // 预警阈值
  const [alertThreshold, setAlertThreshold] = useState(LIQ_PANEL_CACHE.alertThreshold || DEFAULT_ALERT_THRESHOLD);
  const [alertInput, setAlertInput] = useState(LIQ_PANEL_CACHE.alertInput || String(DEFAULT_ALERT_THRESHOLD / 1e6));
  const [alertEnabled, setAlertEnabled] = useState(
    typeof LIQ_PANEL_CACHE.alertEnabled === 'boolean' ? LIQ_PANEL_CACHE.alertEnabled : true
  );
  const lastAlertRef = useRef(LIQ_PANEL_CACHE.lastAlertAt || 0);

  // 突增检测
  const [spikeDetected, setSpikeDetected] = useState(LIQ_PANEL_CACHE.spikeDetected || null); // { bucket, ratio }

  const wsRef = useRef(null);
  const pingTimerRef = useRef(null);
  const reconnectTimerRef = useRef(null);
  const mountedRef = useRef(false);
  const closedByUserRef = useRef(false);
  const lastUpdatedRef = useRef(Math.max(LIQ_PANEL_CACHE.lastUpdatedAt || 0, toNumber(cachedStats.updatedAt)));
  const onHasNewRef = useRef(onHasNew);

  useEffect(() => {
    onHasNewRef.current = onHasNew;
  }, [onHasNew]);

  useEffect(() => {
    LIQ_PANEL_CACHE.stats = stats;
    LIQ_PANEL_CACHE.history = history;
    LIQ_PANEL_CACHE.historyTab = historyTab;
    LIQ_PANEL_CACHE.alertThreshold = alertThreshold;
    LIQ_PANEL_CACHE.alertInput = alertInput;
    LIQ_PANEL_CACHE.alertEnabled = alertEnabled;
    LIQ_PANEL_CACHE.spikeDetected = spikeDetected;
    LIQ_PANEL_CACHE.lastUpdatedAt = Math.max(lastUpdatedRef.current, toNumber(stats.updatedAt));
    LIQ_PANEL_CACHE.lastAlertAt = lastAlertRef.current;
  }, [stats, history, historyTab, alertThreshold, alertInput, alertEnabled, spikeDetected]);

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

  const requestSnapshot = useCallback(() => {
    sendWs({ action: 'snapshot' });
  }, [sendWs]);

  const fetchHistory = useCallback(async (showLoading = false) => {
    if (showLoading) setHistoryLoading(true);
    try {
      const res = await api.getLiquidationHistory(HISTORY_LIMIT);
      const data = res?.data || {};
      setHistory({
        daily: Array.isArray(data.daily) ? data.daily : [],
        h4: Array.isArray(data.h4) ? data.h4 : [],
        h1: Array.isArray(data.h1) ? data.h1 : [],
      });
      setHistoryError('');
    } catch (e) {
      setHistoryError(e.message || '历史数据加载失败');
    } finally {
      if (showLoading) setHistoryLoading(false);
    }
  }, []);

  // 突增检测：当前 h1 是否超过前几小时均值的 3 倍
  const detectSpike = useCallback((statsData) => {
    const h1List = statsData.h1 || [];
    if (h1List.length < 3) {
      setSpikeDetected(null);
      return;
    }
    const current = toNumber(h1List[0]?.totalNotional);
    // 计算前 2~6 小时的均值
    const prevSlice = h1List.slice(1, Math.min(7, h1List.length));
    if (prevSlice.length === 0) { setSpikeDetected(null); return; }
    const avg = prevSlice.reduce((s, b) => s + toNumber(b.totalNotional), 0) / prevSlice.length;
    if (avg > 0 && current > avg * 3) {
      setSpikeDetected({ ratio: (current / avg).toFixed(1), value: current });
    } else {
      setSpikeDetected(null);
    }
  }, []);

  // 阈值通知
  const checkThresholdAlert = useCallback((statsData) => {
    if (!alertEnabled) return;
    const h1 = statsData.h1?.[0];
    if (!h1) return;
    const val = toNumber(h1.totalNotional);
    const now = Date.now();
    if (val >= alertThreshold && now - lastAlertRef.current > 60000) {
      lastAlertRef.current = now;
      Vibration.vibrate([0, 200, 100, 200]);
      Alert.alert(
        '爆仓预警',
        `当前 1H 爆仓总额 $${fmtUsdFull(val)} 已超过阈值 $${fmtUsdFull(alertThreshold)}`,
        [{ text: '知道了' }]
      );
    }
  }, [alertEnabled, alertThreshold]);

  const connectWs = useCallback(() => {
    if (!mountedRef.current) return;
    if (wsRef.current && (wsRef.current.readyState === 0 || wsRef.current.readyState === 1)) return;

    const ws = new WebSocket(`${WS_LIQUIDATION_BASE}?token=${AUTH_TOKEN}`);
    wsRef.current = ws;

    ws.onopen = () => {
      setWsConnected(true);
      setError('');
      clearWsTimers();
      requestSnapshot();
      pingTimerRef.current = setInterval(() => {
        sendWs({ action: 'ping' });
      }, WS_PING_MS);
    };

    ws.onmessage = (event) => {
      let msg;
      try { msg = JSON.parse(event.data); } catch (_) { return; }
      if (!msg || typeof msg !== 'object') return;
      if (msg.channel === 'pong' || msg.method === 'pong' || msg.action === 'pong') return;

      if (msg.channel === 'liquidationStats') {
        const nextUpdated = toNumber(msg.t || Date.now());
        if (nextUpdated > lastUpdatedRef.current) {
          onHasNewRef.current?.(true);
          lastUpdatedRef.current = nextUpdated;
        }
        const nextStats = msg.stats || {};
        const topSymbols = msg.topSymbols || { h1: [], h4: [], day: [] };
        const newStats = {
          daily: Array.isArray(nextStats.daily) ? nextStats.daily : [],
          h4: Array.isArray(nextStats.h4) ? nextStats.h4 : [],
          h1: Array.isArray(nextStats.h1) ? nextStats.h1 : [],
          timezone: String(msg.timezone || 'UTC'),
          updatedAt: nextUpdated,
          startedAt: toNumber(msg.startedAt || 0),
          lastEventTime: toNumber(msg.lastEventTime || 0),
          eventCount: toNumber(msg.eventCount || 0),
          topSymbols: {
            h1: Array.isArray(topSymbols.h1) ? topSymbols.h1 : [],
            h4: Array.isArray(topSymbols.h4) ? topSymbols.h4 : [],
            day: Array.isArray(topSymbols.day) ? topSymbols.day : [],
          },
        };
        setStats(newStats);
        detectSpike(newStats);
        checkThresholdAlert(newStats);
        setError('');
        setLoading(false);
      }
    };

    ws.onerror = () => {
      setWsConnected(false);
      setError('强平统计流连接异常');
      setLoading(false);
    };

    ws.onclose = () => {
      setWsConnected(false);
      clearWsTimers();
      wsRef.current = null;
      if (!mountedRef.current || closedByUserRef.current) return;
      reconnectTimerRef.current = setTimeout(() => { connectWs(); }, WS_RECONNECT_MS);
    };
  }, [clearWsTimers, requestSnapshot, sendWs, detectSpike, checkThresholdAlert]);

  useEffect(() => {
    mountedRef.current = true;
    closedByUserRef.current = false;
    connectWs();
    void fetchHistory(true);

    const snapshotTimer = setInterval(() => {
      requestSnapshot();
      void fetchHistory(false);
    }, SNAPSHOT_REFRESH_MS);
    const nowTimer = setInterval(() => { setNowTs(Date.now()); }, 1000);

    return () => {
      mountedRef.current = false;
      closedByUserRef.current = true;
      clearInterval(snapshotTimer);
      clearInterval(nowTimer);
      clearWsTimers();
      if (wsRef.current) { try { wsRef.current.close(); } catch (_) {} wsRef.current = null; }
      LIQ_PANEL_CACHE.lastUpdatedAt = lastUpdatedRef.current;
      LIQ_PANEL_CACHE.lastAlertAt = lastAlertRef.current;
    };
  }, [clearWsTimers, connectWs, fetchHistory, requestSnapshot]);

  const onRefresh = () => {
    requestSnapshot();
    void fetchHistory(true);
  };

  const applyThreshold = useCallback(() => {
    const val = parseFloat(alertInput);
    if (!Number.isFinite(val) || val <= 0) {
      Alert.alert('参数错误', '请输入有效的阈值（百万 USD）');
      return;
    }
    setAlertThreshold(val * 1e6);
  }, [alertInput]);

  const latestStats = useMemo(() => ({
    day: stats.daily?.[0] || null,
    h4: stats.h4?.[0] || null,
    h1: stats.h1?.[0] || null,
  }), [stats]);

  const hero = useMemo(() => {
    const rows = [
      { label: '1H', mode: 'h1', data: latestStats.h1 },
      { label: '4H', mode: 'h4', data: latestStats.h4 },
      { label: '1D(UTC)', mode: 'day', data: latestStats.day },
    ];
    rows.sort((a, b) => toNumber(b.data?.totalNotional) - toNumber(a.data?.totalNotional));
    return rows[0] || { label: '1H', mode: 'h1', data: null };
  }, [latestStats]);

  const historyRows = useMemo(() => {
    if (historyTab === 'daily') return history.daily || [];
    if (historyTab === 'h4') return history.h4 || [];
    return history.h1 || [];
  }, [history, historyTab]);

  // 当前 topSymbols 选择跟随 historyTab
  const currentTopSymbols = useMemo(() => {
    if (historyTab === 'daily') return stats.topSymbols?.day || [];
    if (historyTab === 'h4') return stats.topSymbols?.h4 || [];
    return stats.topSymbols?.h1 || [];
  }, [stats.topSymbols, historyTab]);

  return (
    <View style={styles.card}>
      <View style={styles.header}>
        <Text style={styles.title}>Binance 强平监控</Text>
        <TouchableOpacity onPress={onRefresh} style={styles.refreshBtn}>
          <Text style={styles.refreshText}>刷新</Text>
        </TouchableOpacity>
      </View>

      <Text style={styles.hintText}>
        WS: {wsConnected ? '已连接' : '重连中'} | 更新: {fmtLocalTime(stats.updatedAt)} | 累计: {toNumber(stats.eventCount).toLocaleString('en-US')} 笔
      </Text>
      {error ? <Text style={styles.errorText}>{error}</Text> : null}

      {/* ========== 突增预警横幅 ========== */}
      {spikeDetected && (
        <View style={styles.spikeBanner}>
          <Text style={styles.spikeIcon}>⚠</Text>
          <Text style={styles.spikeText}>
            爆仓突增！当前 1H 金额是前几小时均值的 {spikeDetected.ratio}x（${fmtUsd(spikeDetected.value)}）
          </Text>
        </View>
      )}

      {loading ? (
        <View style={styles.loadingBox}>
          <ActivityIndicator color={colors.gold} />
          <Text style={styles.loadingText}>强平统计加载中...</Text>
        </View>
      ) : (
        <>
          {/* ========== Hero 卡片 ========== */}
          <View style={[styles.heroCard, spikeDetected && styles.heroCardSpike]}>
            <Text style={styles.heroTitle}>当前最强价值（{hero.label}）</Text>
            <Text style={[styles.heroValue, spikeDetected && styles.heroValueSpike]}>
              ${fmtUsd(hero.data?.totalNotional)}
            </Text>
            <Text style={styles.heroSub}>
              {fmtUtcBucketStart(hero.data?.startTime, hero.mode)} UTC
            </Text>
            <RatioBar buyValue={hero.data?.buyNotional} sellValue={hero.data?.sellNotional} height={8} />
            <RatioLabel buyValue={hero.data?.buyNotional} sellValue={hero.data?.sellNotional} />
          </View>

          {/* ========== 三栏概览 ========== */}
          <View style={styles.summaryRow}>
              {[
                { label: '1H', data: latestStats.h1, mode: 'h1' },
                { label: '4H', data: latestStats.h4, mode: 'h4' },
                { label: '1D(UTC)', data: latestStats.day, mode: 'day' },
              ].map((item) => (
              <View key={item.label} style={styles.summaryItem}>
                <Text style={styles.summaryTitle}>{item.label}</Text>
                <Text style={styles.summaryRange}>{fmtUtcBucketStart(item.data?.startTime, item.mode)}</Text>
                <Text style={styles.summaryValue}>${fmtUsd(item.data?.totalNotional)}</Text>
                <Text style={styles.summarySub}>
                  {toNumber(item.data?.totalCount)} 笔
                </Text>
                <RatioBar buyValue={item.data?.buyNotional} sellValue={item.data?.sellNotional} />
                <RatioLabel buyValue={item.data?.buyNotional} sellValue={item.data?.sellNotional} />
              </View>
            ))}
          </View>

          {/* ========== 24H 趋势图 ========== */}
          <View style={styles.sectionHeader}>
            <Text style={styles.sectionTitle}>24H 趋势</Text>
          </View>
          <TrendBarChart data={stats.h1} maxBars={24} />

          {/* ========== Top 爆仓币种 ========== */}
          {currentTopSymbols.length > 0 && (
            <>
              <View style={styles.sectionHeader}>
                <Text style={styles.sectionTitle}>
                  Top 爆仓币种（{historyTab === 'daily' ? '1D' : historyTab === 'h4' ? '4H' : '1H'}）
                </Text>
              </View>
              <View style={styles.topSymbolsCard}>
                {currentTopSymbols.map((item, idx) => {
                  const maxNotional = toNumber(currentTopSymbols[0]?.notional) || 1;
                  const pct = (toNumber(item.notional) / maxNotional) * 100;
                  return (
                    <View key={item.symbol || idx} style={styles.topSymbolRow}>
                      <Text style={styles.topSymbolRank}>#{idx + 1}</Text>
                      <Text style={styles.topSymbolName}>{(item.symbol || '').replace('USDT', '')}</Text>
                      <View style={styles.topSymbolBarTrack}>
                        <View style={[styles.topSymbolBarFill, { width: `${Math.max(pct, 3)}%` }]} />
                      </View>
                      <Text style={styles.topSymbolValue}>${fmtUsd(item.notional)}</Text>
                      <Text style={styles.topSymbolCount}>{toNumber(item.count)}笔</Text>
                    </View>
                  );
                })}
              </View>
            </>
          )}

          {/* ========== 阈值预警设置 ========== */}
          <View style={styles.sectionHeader}>
            <Text style={styles.sectionTitle}>预警设置</Text>
          </View>
          <View style={styles.alertCard}>
            <View style={styles.alertRow}>
              <Text style={styles.alertLabel}>1H 阈值(百万U)</Text>
              <TextInput
                style={styles.alertInput}
                value={alertInput}
                onChangeText={(v) => setAlertInput(v.replace(/[^0-9.]/g, ''))}
                keyboardType="default"
                placeholder="如 50"
                placeholderTextColor={colors.textMuted}
              />
              <TouchableOpacity style={styles.alertApplyBtn} onPress={applyThreshold}>
                <Text style={styles.alertApplyText}>应用</Text>
              </TouchableOpacity>
              <TouchableOpacity
                style={[styles.alertToggle, alertEnabled && styles.alertToggleOn]}
                onPress={() => setAlertEnabled((v) => !v)}
              >
                <Text style={styles.alertToggleText}>{alertEnabled ? '开' : '关'}</Text>
              </TouchableOpacity>
            </View>
            <Text style={styles.alertHint}>
              当前阈值: ${fmtUsdFull(alertThreshold)} | {alertEnabled ? '预警开启' : '预警关闭'}
            </Text>
          </View>

          {/* ========== 历史数据 ========== */}
	          <View style={styles.sectionHeader}>
	            <Text style={styles.sectionTitle}>历史数据</Text>
	            <Text style={styles.sectionCount}>{historyRows.length} 条</Text>
	          </View>
	          <Text style={styles.hintText}>
	            说明: BUY=空头被强平，SELL=多头被强平；1D 为 UTC 自然日统计（非滚动24H）
	          </Text>

          <View style={styles.histTabRow}>
            {[
              { key: 'h1', label: '1H' },
              { key: 'h4', label: '4H' },
              { key: 'daily', label: '1D' },
            ].map((tab) => (
              <TouchableOpacity
                key={tab.key}
                style={[styles.histTabBtn, historyTab === tab.key && styles.histTabBtnActive]}
                onPress={() => setHistoryTab(tab.key)}
              >
                <Text style={[styles.histTabText, historyTab === tab.key && styles.histTabTextActive]}>
                  {tab.label}
                </Text>
              </TouchableOpacity>
            ))}
          </View>

          {historyLoading && historyRows.length === 0 ? (
            <View style={styles.loadingBox}>
              <ActivityIndicator color={colors.gold} />
              <Text style={styles.loadingText}>历史数据加载中...</Text>
            </View>
          ) : (
            <>
              {historyError ? <Text style={styles.errorText}>{historyError}</Text> : null}
              {historyRows.length === 0 ? (
                <View style={styles.emptyBox}>
                  <Text style={styles.emptyText}>暂无历史数据</Text>
                </View>
              ) : (
                <ScrollView style={styles.historyScroll} nestedScrollEnabled>
                  {historyRows.slice(0, 40).map((item, idx) => {
                    const mode = historyTab === 'daily' ? 'day' : historyTab;
                    return (
                      <View key={`${historyTab}-${item.startTime}-${idx}`} style={styles.historyRow}>
                        <View style={styles.historyTop}>
                          <Text style={styles.historyTitle}>{fmtUtcBucketStart(item.startTime, mode)} UTC</Text>
                          <Text style={styles.historyValue}>${fmtUsd(item.totalNotional)}</Text>
                        </View>
                        <Text style={styles.historySub}>
                          {toNumber(item.totalCount)} 笔 | BUY {toNumber(item.buyCount)} / SELL {toNumber(item.sellCount)}
                        </Text>
                        <RatioBar buyValue={item.buyNotional} sellValue={item.sellNotional} />
                      </View>
                    );
                  })}
                </ScrollView>
              )}
            </>
          )}
        </>
      )}
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
    marginBottom: spacing.sm,
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
    fontSize: fontSize.sm,
    color: colors.goldLight,
    fontWeight: '600',
  },
  hintText: {
    color: colors.textMuted,
    fontSize: fontSize.xs,
    marginBottom: spacing.sm,
  },
  errorText: {
    color: colors.redLight,
    fontSize: fontSize.sm,
    marginTop: spacing.xs,
    marginBottom: spacing.sm,
  },
  loadingBox: {
    borderRadius: radius.lg,
    backgroundColor: colors.surface,
    paddingVertical: spacing.xxl,
    alignItems: 'center',
    gap: spacing.sm,
    marginTop: spacing.md,
  },
  loadingText: {
    color: colors.textMuted,
    fontSize: fontSize.sm,
  },

  // ===== 突增预警横幅 =====
  spikeBanner: {
    flexDirection: 'row',
    alignItems: 'center',
    backgroundColor: 'rgba(217,68,82,0.18)',
    borderWidth: 1,
    borderColor: colors.red,
    borderRadius: radius.md,
    padding: spacing.sm,
    marginBottom: spacing.sm,
    gap: spacing.sm,
  },
  spikeIcon: {
    fontSize: fontSize.lg,
  },
  spikeText: {
    fontSize: fontSize.sm,
    color: colors.redLight,
    fontWeight: '700',
    flex: 1,
  },

  // ===== Hero 卡片 =====
  heroCard: {
    borderRadius: radius.lg,
    borderWidth: 1,
    borderColor: colors.gold,
    backgroundColor: colors.goldBg,
    padding: spacing.md,
    marginBottom: spacing.md,
  },
  heroCardSpike: {
    borderColor: colors.red,
    backgroundColor: 'rgba(217,68,82,0.12)',
  },
  heroTitle: {
    color: colors.textSecondary,
    fontSize: fontSize.sm,
  },
  heroValue: {
    color: colors.goldLight,
    fontSize: 34,
    fontWeight: '900',
    marginTop: spacing.xs,
  },
  heroValueSpike: {
    color: colors.redLight,
  },
  heroSub: {
    color: colors.textMuted,
    fontSize: fontSize.xs,
    marginTop: spacing.xs,
  },

  // ===== 三栏概览 =====
  summaryRow: {
    flexDirection: 'row',
    gap: spacing.sm,
    marginBottom: spacing.md,
  },
  summaryItem: {
    flex: 1,
    borderRadius: radius.md,
    borderWidth: 1,
    borderColor: colors.cardBorder,
    backgroundColor: colors.surface,
    paddingVertical: spacing.sm,
    paddingHorizontal: spacing.sm,
  },
  summaryTitle: {
    color: colors.white,
    fontSize: fontSize.xs,
    fontWeight: '700',
  },
  summaryRange: {
    color: colors.textMuted,
    fontSize: fontSize.xs,
    marginTop: 2,
  },
  summaryValue: {
    color: colors.goldLight,
    fontSize: fontSize.lg,
    fontWeight: '800',
    marginTop: spacing.xs,
  },
  summarySub: {
    color: colors.textSecondary,
    fontSize: fontSize.xs,
    marginTop: 2,
  },

  // ===== Top 币种 =====
  topSymbolsCard: {
    backgroundColor: colors.surface,
    borderRadius: radius.md,
    padding: spacing.md,
    marginBottom: spacing.md,
    gap: spacing.sm,
  },
  topSymbolRow: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: spacing.sm,
  },
  topSymbolRank: {
    fontSize: fontSize.xs,
    color: colors.goldLight,
    fontWeight: '800',
    width: 22,
  },
  topSymbolName: {
    fontSize: fontSize.sm,
    color: colors.white,
    fontWeight: '700',
    width: 50,
  },
  topSymbolBarTrack: {
    flex: 1,
    height: 6,
    backgroundColor: colors.cardBorder,
    borderRadius: 3,
    overflow: 'hidden',
  },
  topSymbolBarFill: {
    height: '100%',
    backgroundColor: colors.gold,
    borderRadius: 3,
  },
  topSymbolValue: {
    fontSize: fontSize.xs,
    color: colors.goldLight,
    fontWeight: '700',
    width: 52,
    textAlign: 'right',
  },
  topSymbolCount: {
    fontSize: fontSize.xs,
    color: colors.textMuted,
    width: 32,
    textAlign: 'right',
  },

  // ===== 预警设置 =====
  alertCard: {
    backgroundColor: colors.surface,
    borderRadius: radius.md,
    padding: spacing.md,
    marginBottom: spacing.md,
  },
  alertRow: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: spacing.sm,
  },
  alertLabel: {
    fontSize: fontSize.xs,
    color: colors.textMuted,
  },
  alertInput: {
    flex: 1,
    borderWidth: 1,
    borderColor: colors.cardBorder,
    backgroundColor: colors.card,
    color: colors.text,
    borderRadius: radius.sm,
    paddingHorizontal: spacing.sm,
    paddingVertical: spacing.xs,
    fontSize: fontSize.sm,
  },
  alertApplyBtn: {
    paddingHorizontal: spacing.md,
    paddingVertical: spacing.xs,
    borderRadius: radius.sm,
    backgroundColor: colors.goldBg,
    borderWidth: 1,
    borderColor: colors.gold,
  },
  alertApplyText: {
    fontSize: fontSize.sm,
    color: colors.gold,
    fontWeight: '700',
  },
  alertToggle: {
    paddingHorizontal: spacing.md,
    paddingVertical: spacing.xs,
    borderRadius: radius.sm,
    backgroundColor: colors.surface,
    borderWidth: 1,
    borderColor: colors.cardBorder,
  },
  alertToggleOn: {
    backgroundColor: colors.greenBg,
    borderColor: colors.green,
  },
  alertToggleText: {
    fontSize: fontSize.sm,
    fontWeight: '700',
    color: colors.text,
  },
  alertHint: {
    fontSize: fontSize.xs,
    color: colors.textMuted,
    marginTop: spacing.xs,
  },

  // ===== 区域标题 =====
  sectionHeader: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'center',
    marginTop: spacing.sm,
    marginBottom: spacing.sm,
  },
  sectionTitle: {
    color: colors.white,
    fontSize: fontSize.md,
    fontWeight: '700',
  },
  sectionCount: {
    color: colors.textMuted,
    fontSize: fontSize.sm,
  },

  // ===== 历史 Tab =====
  histTabRow: {
    flexDirection: 'row',
    gap: spacing.sm,
    marginBottom: spacing.sm,
  },
  histTabBtn: {
    flex: 1,
    borderRadius: radius.md,
    backgroundColor: colors.surface,
    borderWidth: 1,
    borderColor: colors.cardBorder,
    alignItems: 'center',
    paddingVertical: spacing.xs,
  },
  histTabBtnActive: {
    backgroundColor: colors.goldBg,
    borderColor: colors.gold,
  },
  histTabText: {
    color: colors.textMuted,
    fontSize: fontSize.sm,
    fontWeight: '600',
  },
  histTabTextActive: {
    color: colors.white,
    fontWeight: '700',
  },
  historyScroll: {
    maxHeight: 520,
  },
  historyRow: {
    backgroundColor: colors.surface,
    borderRadius: radius.md,
    padding: spacing.md,
    marginBottom: spacing.sm,
  },
  historyTop: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'center',
  },
  historyTitle: {
    color: colors.white,
    fontSize: fontSize.sm,
    fontWeight: '700',
  },
  historyValue: {
    color: colors.goldLight,
    fontSize: fontSize.md,
    fontWeight: '800',
  },
  historySub: {
    color: colors.textSecondary,
    fontSize: fontSize.xs,
    marginTop: 2,
  },
  emptyBox: {
    borderRadius: radius.lg,
    backgroundColor: colors.surface,
    paddingVertical: spacing.xl,
    alignItems: 'center',
  },
  emptyText: {
    color: colors.textMuted,
    fontSize: fontSize.sm,
  },
});
