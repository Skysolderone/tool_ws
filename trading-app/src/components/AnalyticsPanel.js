import React, { useCallback, useEffect, useMemo, useState } from 'react';
import {
  ActivityIndicator,
  StyleSheet,
  Text,
  TouchableOpacity,
  View,
} from 'react-native';
import api from '../services/api';
import { colors, fontSize, radius, spacing } from '../services/theme';

const RANGE_OPTIONS = [7, 30, 90];

const EMPTY_JOURNAL = Object.freeze({
  from: 0,
  to: 0,
  period: 'daily',
  overall: {
    totalTrades: 0,
    wins: 0,
    losses: 0,
    breakeven: 0,
    winRate: 0,
    grossProfit: 0,
    grossLoss: 0,
    netPnl: 0,
    avgWin: 0,
    avgLoss: 0,
    pnlRatio: 0,
    profitFactor: 0,
    maxDrawdown: 0,
    maxDrawdownPct: 0,
    avgHoldMinutes: 0,
    holdDurationBuckets: {
      '<=15m': 0,
      '15-60m': 0,
      '1-4h': 0,
      '4-24h': 0,
      '>24h': 0,
    },
  },
  buckets: [],
});

const EMPTY_ATTRIBUTION = Object.freeze({
  source: [],
  symbol: [],
  hour: [],
});

const EMPTY_SENTIMENT = Object.freeze({
  symbol: 'BTCUSDT',
  time: 0,
  timezone: 'UTC',
  index: 50,
  regime: 'neutral',
  liquidation: {
    score: 50,
    direction: 0,
    intensity: 0,
    totalNotional: 0,
    buyNotional: 0,
    sellNotional: 0,
  },
  funding: { score: 50, value: 0 },
  longShort: { score: 50, longAccount: 0.5, shortAccount: 0.5, timestamp: 0 },
  warnings: [],
});

function toNum(v) {
  const n = Number(v);
  return Number.isFinite(n) ? n : 0;
}

function formatUsd(v) {
  const n = toNum(v);
  if (Math.abs(n) >= 1e9) return `${(n / 1e9).toFixed(2)}B`;
  if (Math.abs(n) >= 1e6) return `${(n / 1e6).toFixed(2)}M`;
  if (Math.abs(n) >= 1e3) return `${(n / 1e3).toFixed(1)}K`;
  return n.toFixed(2);
}

function formatPct(v, digits = 1) {
  return `${(toNum(v) * 100).toFixed(digits)}%`;
}

function formatUtc(ms) {
  const t = toNum(ms);
  if (t <= 0) return '--';
  const d = new Date(t);
  const pad = (x) => String(x).padStart(2, '0');
  return `${d.getUTCFullYear()}-${pad(d.getUTCMonth() + 1)}-${pad(d.getUTCDate())} ${pad(d.getUTCHours())}:${pad(d.getUTCMinutes())}:${pad(d.getUTCSeconds())} UTC`;
}

function StatCell({ label, value, positiveNegative = false }) {
  const n = toNum(value);
  const color = positiveNegative ? (n >= 0 ? colors.greenLight : colors.redLight) : colors.white;
  return (
    <View style={styles.statCell}>
      <Text style={styles.statLabel}>{label}</Text>
      <Text style={[styles.statValue, { color }]}>{value}</Text>
    </View>
  );
}

function AttributionList({ title, rows }) {
  const list = (rows || []).slice(0, 5);
  return (
    <View style={styles.attrCard}>
      <Text style={styles.subTitle}>{title}</Text>
      {list.length === 0 ? (
        <Text style={styles.emptyText}>暂无数据</Text>
      ) : (
        list.map((item, idx) => {
          const pnl = toNum(item.netPnl);
          return (
            <View key={`${title}-${item.key}-${idx}`} style={styles.attrRow}>
              <Text style={styles.attrKey} numberOfLines={1}>{item.key}</Text>
              <Text style={styles.attrTrades}>{item.trades} 笔</Text>
              <Text style={[styles.attrPnl, { color: pnl >= 0 ? colors.greenLight : colors.redLight }]}>
                {pnl >= 0 ? '+' : ''}{formatUsd(pnl)}
              </Text>
            </View>
          );
        })
      )}
    </View>
  );
}

export default function AnalyticsPanel({ sentimentSymbol = 'BTCUSDT' }) {
  const [period, setPeriod] = useState('daily');
  const [rangeDays, setRangeDays] = useState(30);
  const [loading, setLoading] = useState(true);
  const [refreshing, setRefreshing] = useState(false);
  const [error, setError] = useState('');

  const [journal, setJournal] = useState(EMPTY_JOURNAL);
  const [attribution, setAttribution] = useState(EMPTY_ATTRIBUTION);
  const [sentiment, setSentiment] = useState(EMPTY_SENTIMENT);

  const bestHour = useMemo(() => {
    if (!attribution.hour || attribution.hour.length === 0) return null;
    const sorted = [...attribution.hour].sort((a, b) => toNum(b.netPnl) - toNum(a.netPnl));
    return sorted[0] || null;
  }, [attribution.hour]);

  const fetchAll = useCallback(async (isRefresh = false) => {
    if (isRefresh) setRefreshing(true);
    else setLoading(true);
    setError('');

    const now = Date.now();
    const from = new Date(now - rangeDays * 24 * 3600 * 1000).toISOString();
    const to = new Date(now).toISOString();

    const [jRes, aRes, sRes] = await Promise.allSettled([
      api.getAnalyticsJournal({ period, from, to }),
      api.getAnalyticsAttribution({ from, to }),
      api.getAnalyticsSentiment({ symbol: sentimentSymbol || 'BTCUSDT', period: '5m' }),
    ]);

    const errs = [];
    if (jRes.status === 'fulfilled') setJournal(jRes.value?.data || EMPTY_JOURNAL);
    else errs.push(`交易日记加载失败: ${jRes.reason?.message || 'unknown error'}`);

    if (aRes.status === 'fulfilled') setAttribution(aRes.value?.data || EMPTY_ATTRIBUTION);
    else errs.push(`归因加载失败: ${aRes.reason?.message || 'unknown error'}`);

    if (sRes.status === 'fulfilled') setSentiment(sRes.value?.data || EMPTY_SENTIMENT);
    else errs.push(`情绪指标加载失败: ${sRes.reason?.message || 'unknown error'}`);

    if (errs.length > 0) setError(errs.join('\n'));
    setLoading(false);
    setRefreshing(false);
  }, [period, rangeDays, sentimentSymbol]);

  useEffect(() => {
    fetchAll(false);
  }, [fetchAll]);

  const m = journal.overall || EMPTY_JOURNAL.overall;
  const pnlColor = toNum(m.netPnl) >= 0 ? colors.greenLight : colors.redLight;
  const sentimentColor = sentiment.index >= 65
    ? colors.greenLight
    : sentiment.index <= 35 ? colors.redLight : colors.goldLight;

  if (loading) {
    return (
      <View style={styles.card}>
        <View style={styles.loadingWrap}>
          <ActivityIndicator color={colors.gold} />
          <Text style={styles.loadingText}>分析中...</Text>
        </View>
      </View>
    );
  }

  return (
    <View style={styles.card}>
      <View style={styles.headerRow}>
        <Text style={styles.title}>数据分析</Text>
        <TouchableOpacity style={styles.refreshBtn} onPress={() => fetchAll(true)} disabled={refreshing}>
          <Text style={styles.refreshBtnText}>{refreshing ? '刷新中' : '刷新'}</Text>
        </TouchableOpacity>
      </View>

      <View style={styles.filterRow}>
        <View style={styles.filterGroup}>
          {['daily', 'weekly'].map((p) => (
            <TouchableOpacity
              key={p}
              onPress={() => setPeriod(p)}
              style={[styles.pill, period === p && styles.pillActive]}
            >
              <Text style={[styles.pillText, period === p && styles.pillTextActive]}>
                {p === 'daily' ? '按天' : '按周'}
              </Text>
            </TouchableOpacity>
          ))}
        </View>
        <View style={styles.filterGroup}>
          {RANGE_OPTIONS.map((days) => (
            <TouchableOpacity
              key={days}
              onPress={() => setRangeDays(days)}
              style={[styles.pill, rangeDays === days && styles.pillActive]}
            >
              <Text style={[styles.pillText, rangeDays === days && styles.pillTextActive]}>{days}D</Text>
            </TouchableOpacity>
          ))}
        </View>
      </View>

      {error ? <Text style={styles.errorText}>{error}</Text> : null}

      <View style={styles.heroWrap}>
        <Text style={styles.heroLabel}>净盈亏 ({period === 'daily' ? '日汇总' : '周汇总'})</Text>
        <Text style={[styles.heroValue, { color: pnlColor }]}>
          {toNum(m.netPnl) >= 0 ? '+' : ''}{formatUsd(m.netPnl)} U
        </Text>
        <Text style={styles.heroSub}>胜率 {formatPct(m.winRate)} · 交易 {m.totalTrades} 笔</Text>
      </View>

      <View style={styles.statGrid}>
        <StatCell label="盈亏比" value={toNum(m.pnlRatio).toFixed(2)} />
        <StatCell label="Profit Factor" value={toNum(m.profitFactor).toFixed(2)} />
        <StatCell label="最大回撤" value={`${formatUsd(m.maxDrawdown)} U`} positiveNegative />
        <StatCell label="回撤占比" value={formatPct(m.maxDrawdownPct)} />
        <StatCell label="平均持仓时长" value={`${toNum(m.avgHoldMinutes).toFixed(1)}m`} />
        <StatCell label="平均单笔盈亏" value={`${formatUsd(toNum(m.totalTrades) > 0 ? m.netPnl / m.totalTrades : 0)} U`} positiveNegative />
      </View>

      <View style={styles.bucketCard}>
        <Text style={styles.subTitle}>持仓时长分布</Text>
        {Object.entries(m.holdDurationBuckets || {}).map(([k, v]) => (
          <View key={k} style={styles.bucketRow}>
            <Text style={styles.bucketKey}>{k}</Text>
            <Text style={styles.bucketVal}>{v} 笔</Text>
          </View>
        ))}
      </View>

      <View style={styles.bucketCard}>
        <Text style={styles.subTitle}>最近汇总</Text>
        {(journal.buckets || []).slice(0, 6).map((b) => {
          const pnl = toNum(b?.metrics?.netPnl);
          return (
            <View key={b.key} style={styles.bucketRow}>
              <Text style={styles.bucketKey}>{b.key}</Text>
              <Text style={[styles.bucketVal, { color: pnl >= 0 ? colors.greenLight : colors.redLight }]}>
                {pnl >= 0 ? '+' : ''}{formatUsd(pnl)} U
              </Text>
            </View>
          );
        })}
      </View>

      <AttributionList title="按策略来源归因" rows={attribution.source} />
      <AttributionList title="按币种归因" rows={attribution.symbol} />
      <AttributionList title="按时段归因 (UTC)" rows={(attribution.hour || []).map((h) => ({ ...h, key: `${String(h.hourUTC).padStart(2, '0')}:00` }))} />

      <View style={styles.sentimentCard}>
        <Text style={styles.subTitle}>市场情绪指标</Text>
        <View style={styles.sentimentTop}>
          <View>
            <Text style={styles.sentimentLabel}>综合指数</Text>
            <Text style={[styles.sentimentValue, { color: sentimentColor }]}>{toNum(sentiment.index).toFixed(1)}</Text>
            <Text style={styles.sentimentRegime}>{String(sentiment.regime || 'neutral').toUpperCase()}</Text>
          </View>
          <View style={styles.sentimentMeta}>
            <Text style={styles.sentimentMetaText}>标的: {sentiment.symbol || '--'}</Text>
            <Text style={styles.sentimentMetaText}>更新: {formatUtc(sentiment.time)}</Text>
            {bestHour ? (
              <Text style={styles.sentimentMetaText}>
                最佳时段(UTC): {String(bestHour.hourUTC).padStart(2, '0')}:00
              </Text>
            ) : null}
          </View>
        </View>
        <View style={styles.componentRow}>
          <Text style={styles.componentLabel}>爆仓情绪</Text>
          <Text style={styles.componentVal}>{toNum(sentiment.liquidation?.score).toFixed(1)}</Text>
        </View>
        <View style={styles.componentRow}>
          <Text style={styles.componentLabel}>资金费率</Text>
          <Text style={styles.componentVal}>
            {toNum(sentiment.funding?.value).toFixed(6)} / {toNum(sentiment.funding?.score).toFixed(1)}
          </Text>
        </View>
        <View style={styles.componentRow}>
          <Text style={styles.componentLabel}>多仓人数占比</Text>
          <Text style={styles.componentVal}>
            {formatPct(sentiment.longShort?.longAccount || 0, 2)}
          </Text>
        </View>
        <View style={styles.componentRow}>
          <Text style={styles.componentLabel}>空仓人数占比</Text>
          <Text style={styles.componentVal}>
            {formatPct(sentiment.longShort?.shortAccount || 0, 2)}
          </Text>
        </View>
      </View>
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
    borderTopWidth: 2,
    borderTopColor: colors.blue,
  },
  loadingWrap: {
    minHeight: 120,
    justifyContent: 'center',
    alignItems: 'center',
    gap: spacing.sm,
  },
  loadingText: {
    color: colors.textMuted,
    fontSize: fontSize.sm,
  },
  headerRow: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'center',
    marginBottom: spacing.sm,
  },
  title: {
    color: colors.white,
    fontSize: fontSize.lg,
    fontWeight: '800',
  },
  refreshBtn: {
    borderWidth: 1,
    borderColor: colors.blue,
    backgroundColor: colors.blueBg,
    paddingHorizontal: spacing.md,
    paddingVertical: spacing.xs,
    borderRadius: radius.sm,
  },
  refreshBtnText: {
    color: colors.blueLight,
    fontSize: fontSize.sm,
    fontWeight: '700',
  },
  filterRow: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    gap: spacing.sm,
    marginBottom: spacing.sm,
  },
  filterGroup: {
    flexDirection: 'row',
    gap: spacing.xs,
  },
  pill: {
    borderWidth: 1,
    borderColor: colors.cardBorder,
    backgroundColor: colors.surface,
    paddingHorizontal: spacing.md,
    paddingVertical: spacing.xs,
    borderRadius: radius.pill,
  },
  pillActive: {
    borderColor: colors.gold,
    backgroundColor: colors.goldBg,
  },
  pillText: {
    color: colors.textSecondary,
    fontSize: fontSize.sm,
    fontWeight: '600',
  },
  pillTextActive: {
    color: colors.gold,
  },
  errorText: {
    color: colors.redLight,
    fontSize: fontSize.xs,
    marginBottom: spacing.sm,
  },
  heroWrap: {
    backgroundColor: colors.surface,
    borderRadius: radius.md,
    borderWidth: 1,
    borderColor: colors.cardBorder,
    padding: spacing.md,
    marginBottom: spacing.sm,
    alignItems: 'center',
  },
  heroLabel: {
    fontSize: fontSize.sm,
    color: colors.textMuted,
  },
  heroValue: {
    marginTop: spacing.xs,
    fontSize: fontSize.xxl,
    fontWeight: '800',
    fontVariant: ['tabular-nums'],
  },
  heroSub: {
    marginTop: spacing.xs,
    fontSize: fontSize.xs,
    color: colors.textSecondary,
  },
  statGrid: {
    flexDirection: 'row',
    flexWrap: 'wrap',
    gap: spacing.sm,
    marginBottom: spacing.sm,
  },
  statCell: {
    width: '48%',
    backgroundColor: colors.surface,
    borderRadius: radius.md,
    borderWidth: 1,
    borderColor: colors.cardBorder,
    padding: spacing.sm,
  },
  statLabel: {
    color: colors.textMuted,
    fontSize: fontSize.xs,
  },
  statValue: {
    marginTop: spacing.xs,
    color: colors.white,
    fontWeight: '700',
    fontSize: fontSize.md,
    fontVariant: ['tabular-nums'],
  },
  bucketCard: {
    backgroundColor: colors.surface,
    borderRadius: radius.md,
    borderWidth: 1,
    borderColor: colors.cardBorder,
    padding: spacing.sm,
    marginBottom: spacing.sm,
  },
  subTitle: {
    color: colors.text,
    fontSize: fontSize.md,
    fontWeight: '700',
    marginBottom: spacing.xs,
  },
  bucketRow: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    paddingVertical: 3,
    borderBottomWidth: 1,
    borderBottomColor: colors.divider,
  },
  bucketKey: {
    color: colors.textSecondary,
    fontSize: fontSize.sm,
  },
  bucketVal: {
    color: colors.white,
    fontSize: fontSize.sm,
    fontWeight: '600',
    fontVariant: ['tabular-nums'],
  },
  attrCard: {
    backgroundColor: colors.surface,
    borderRadius: radius.md,
    borderWidth: 1,
    borderColor: colors.cardBorder,
    padding: spacing.sm,
    marginBottom: spacing.sm,
  },
  attrRow: {
    flexDirection: 'row',
    alignItems: 'center',
    paddingVertical: 4,
    borderBottomWidth: 1,
    borderBottomColor: colors.divider,
    gap: spacing.xs,
  },
  attrKey: {
    flex: 1.2,
    color: colors.white,
    fontSize: fontSize.sm,
    fontWeight: '600',
  },
  attrTrades: {
    flex: 0.6,
    color: colors.textMuted,
    fontSize: fontSize.xs,
    textAlign: 'right',
  },
  attrPnl: {
    flex: 1,
    textAlign: 'right',
    fontVariant: ['tabular-nums'],
    fontSize: fontSize.sm,
    fontWeight: '700',
  },
  emptyText: {
    color: colors.textMuted,
    fontSize: fontSize.sm,
    paddingVertical: spacing.sm,
  },
  sentimentCard: {
    backgroundColor: colors.surface,
    borderRadius: radius.md,
    borderWidth: 1,
    borderColor: colors.cardBorder,
    padding: spacing.sm,
  },
  sentimentTop: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    gap: spacing.sm,
    marginBottom: spacing.sm,
  },
  sentimentLabel: {
    color: colors.textMuted,
    fontSize: fontSize.xs,
  },
  sentimentValue: {
    fontSize: fontSize.xxl,
    fontWeight: '800',
    fontVariant: ['tabular-nums'],
  },
  sentimentRegime: {
    color: colors.textSecondary,
    fontSize: fontSize.xs,
    fontWeight: '700',
  },
  sentimentMeta: {
    flex: 1,
    alignItems: 'flex-end',
    justifyContent: 'center',
    gap: 2,
  },
  sentimentMetaText: {
    color: colors.textSecondary,
    fontSize: fontSize.xs,
  },
  componentRow: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'center',
    paddingVertical: 4,
    borderTopWidth: 1,
    borderTopColor: colors.divider,
  },
  componentLabel: {
    color: colors.textSecondary,
    fontSize: fontSize.sm,
  },
  componentVal: {
    color: colors.white,
    fontSize: fontSize.sm,
    fontWeight: '700',
    fontVariant: ['tabular-nums'],
  },
});
