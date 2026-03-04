import React, { useCallback, useEffect, useMemo, useState } from 'react';
import { ActivityIndicator, StyleSheet, Text, View } from 'react-native';
import api from '../services/api';
import { colors, fontSize, radius, spacing } from '../services/theme';

const REFRESH_MS = 10000;
const DEFAULT_DAYS = 30;

const EMPTY_OVERVIEW = Object.freeze({
  netValue: 0,
  dayPnl: 0,
  drawdown: 0,
  drawdownPct: 0,
  marginRatio: 0,
  var95: 0,
  slippageP95Bps: 0,
  rejectRate: 0,
  riskTriggerCount: 0,
  windowDays: DEFAULT_DAYS,
  updatedAt: 0,
  warnings: [],
});

function toNum(value) {
  const n = Number(value);
  return Number.isFinite(n) ? n : 0;
}

function fmtCompactUsd(value) {
  const n = toNum(value);
  const abs = Math.abs(n);
  if (abs >= 1e9) return `${(n / 1e9).toFixed(2)}B`;
  if (abs >= 1e6) return `${(n / 1e6).toFixed(2)}M`;
  if (abs >= 1e3) return `${(n / 1e3).toFixed(1)}K`;
  return n.toFixed(2);
}

function fmtTime(ms) {
  const t = toNum(ms);
  if (t <= 0) return '--:--:--';
  return new Date(t).toLocaleTimeString('zh-CN', { hour12: false });
}

function toneColor(tone) {
  if (tone === 'good') return colors.greenLight;
  if (tone === 'bad') return colors.redLight;
  if (tone === 'warn') return colors.goldLight;
  return colors.white;
}

export default function MonitorOverviewPanel() {
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [overview, setOverview] = useState(EMPTY_OVERVIEW);

  const fetchOverview = useCallback(async () => {
    try {
      const res = await api.getMonitorOverview(DEFAULT_DAYS);
      const data = res?.data || {};
      setOverview({
        ...EMPTY_OVERVIEW,
        ...data,
        warnings: Array.isArray(data.warnings) ? data.warnings : [],
      });
      setError('');
    } catch (e) {
      setError(e?.message || '监控总览拉取失败');
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    let cancelled = false;
    const run = async () => {
      if (cancelled) return;
      await fetchOverview();
    };
    run();
    const timer = setInterval(run, REFRESH_MS);
    return () => {
      cancelled = true;
      clearInterval(timer);
    };
  }, [fetchOverview]);

  const metricCards = useMemo(() => {
    const dayPnl = toNum(overview.dayPnl);
    const drawdownPct = toNum(overview.drawdownPct);
    const marginRatio = toNum(overview.marginRatio);
    const rejectRate = toNum(overview.rejectRate);
    const riskTriggers = Math.round(toNum(overview.riskTriggerCount));

    return [
      { key: 'netValue', label: '净值', value: `$${fmtCompactUsd(overview.netValue)}`, tone: 'normal' },
      {
        key: 'dayPnl',
        label: '当日PnL',
        value: `${dayPnl >= 0 ? '+' : ''}$${fmtCompactUsd(dayPnl)}`,
        tone: dayPnl >= 0 ? 'good' : 'bad',
      },
      {
        key: 'drawdown',
        label: `回撤(${overview.windowDays || DEFAULT_DAYS}d)`,
        value: `${drawdownPct.toFixed(2)}%`,
        tone: drawdownPct >= 20 ? 'bad' : drawdownPct >= 10 ? 'warn' : 'good',
      },
      {
        key: 'marginRatio',
        label: '保证金率',
        value: `${marginRatio.toFixed(1)}%`,
        tone: marginRatio >= 600 ? 'bad' : marginRatio >= 300 ? 'warn' : 'good',
      },
      { key: 'var95', label: 'VaR95', value: `$${fmtCompactUsd(overview.var95)}`, tone: 'normal' },
      {
        key: 'slippageP95',
        label: '滑点P95',
        value: `${toNum(overview.slippageP95Bps).toFixed(2)} bps`,
        tone: toNum(overview.slippageP95Bps) >= 30 ? 'bad' : toNum(overview.slippageP95Bps) >= 10 ? 'warn' : 'good',
      },
      {
        key: 'rejectRate',
        label: '拒单率',
        value: `${rejectRate.toFixed(2)}%`,
        tone: rejectRate >= 10 ? 'bad' : rejectRate >= 3 ? 'warn' : 'good',
      },
      {
        key: 'riskTriggers',
        label: '风控触发',
        value: `${riskTriggers}`,
        tone: riskTriggers > 0 ? 'warn' : 'good',
      },
    ];
  }, [overview]);

  return (
    <View style={styles.card}>
      <View style={styles.header}>
        <Text style={styles.title}>监控总览</Text>
        <Text style={styles.timeText}>更新: {fmtTime(overview.updatedAt)}</Text>
      </View>

      {loading ? (
        <View style={styles.loadingWrap}>
          <ActivityIndicator color={colors.gold} />
        </View>
      ) : (
        <View style={styles.grid}>
          {metricCards.map((item) => (
            <View key={item.key} style={styles.metricItem}>
              <Text style={styles.metricLabel}>{item.label}</Text>
              <Text style={[styles.metricValue, { color: toneColor(item.tone) }]}>{item.value}</Text>
            </View>
          ))}
        </View>
      )}

      {error ? <Text style={styles.errorText}>{error}</Text> : null}
      {overview.warnings?.length > 0 ? (
        <Text style={styles.warnText} numberOfLines={2}>
          数据降级: {overview.warnings.join(' | ')}
        </Text>
      ) : null}
    </View>
  );
}

const styles = StyleSheet.create({
  card: {
    backgroundColor: colors.card,
    borderRadius: radius.lg,
    borderWidth: 1,
    borderColor: colors.cardBorder,
    padding: spacing.md,
    marginBottom: spacing.md,
  },
  header: {
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
    marginBottom: spacing.sm,
  },
  title: {
    color: colors.goldLight,
    fontSize: fontSize.md,
    fontWeight: '700',
  },
  timeText: {
    color: colors.textMuted,
    fontSize: fontSize.xs,
  },
  loadingWrap: {
    alignItems: 'center',
    justifyContent: 'center',
    paddingVertical: spacing.md,
  },
  grid: {
    flexDirection: 'row',
    flexWrap: 'wrap',
    justifyContent: 'space-between',
    gap: spacing.sm,
  },
  metricItem: {
    width: '48%',
    backgroundColor: colors.surface,
    borderRadius: radius.md,
    borderWidth: 1,
    borderColor: colors.divider,
    padding: spacing.sm,
  },
  metricLabel: {
    color: colors.textMuted,
    fontSize: fontSize.xs,
    marginBottom: 4,
  },
  metricValue: {
    color: colors.white,
    fontSize: fontSize.md,
    fontWeight: '700',
    fontVariant: ['tabular-nums'],
  },
  errorText: {
    marginTop: spacing.sm,
    color: colors.redLight,
    fontSize: fontSize.xs,
  },
  warnText: {
    marginTop: spacing.xs,
    color: colors.goldLight,
    fontSize: fontSize.xs,
  },
});
