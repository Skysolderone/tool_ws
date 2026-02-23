import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { ActivityIndicator, StyleSheet, Text, TouchableOpacity, View } from 'react-native';
import api from '../services/api';
import { colors, fontSize, radius, spacing } from '../services/theme';

const POLL_MS = 8000;
const FLOW_EPSILON = 2000;
const DIR_EPSILON = 1000;

function toNum(v) {
  const n = Number(v);
  return Number.isFinite(n) ? n : 0;
}

function fmtUsd(v) {
  const n = toNum(v);
  if (Math.abs(n) >= 1e9) return `${(n / 1e9).toFixed(2)}B`;
  if (Math.abs(n) >= 1e6) return `${(n / 1e6).toFixed(2)}M`;
  if (Math.abs(n) >= 1e3) return `${(n / 1e3).toFixed(1)}K`;
  return n.toFixed(0);
}

function fmtTime(ms) {
  if (!ms) return '--';
  return new Date(ms).toLocaleTimeString('zh-CN', { hour12: false });
}

function flowMeta(deltaAbs) {
  const v = toNum(deltaAbs);
  if (Math.abs(v) < FLOW_EPSILON) return { text: '持平', color: colors.textMuted };
  if (v > 0) return { text: '加仓', color: colors.greenLight };
  return { text: '减仓', color: colors.redLight };
}

function dirMeta(netExposure) {
  const n = toNum(netExposure);
  if (n > DIR_EPSILON) return { text: '偏多', color: colors.greenLight };
  if (n < -DIR_EPSILON) return { text: '偏空', color: colors.redLight };
  return { text: '中性', color: colors.textMuted };
}

export default function WhaleAggregatePanel({ watchAddresses = [], onHasNew }) {
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [rows, setRows] = useState([]);
  const [updatedAt, setUpdatedAt] = useState(0);
  const prevRef = useRef({});

  const fetchData = useCallback(async () => {
    const targets = (watchAddresses || []).slice(0, 3).filter((x) => !!x?.address);
    if (targets.length === 0) {
      setRows([]);
      setLoading(false);
      return;
    }

    const results = await Promise.allSettled(
      targets.map((t) => api.getHyperPositions(t.address)),
    );

    let hasChange = false;
    const nextRows = targets.map((item, idx) => {
      const res = results[idx];
      if (res.status !== 'fulfilled' || !res.value?.data) {
        return {
          ok: false,
          label: item.label || `监控${idx + 1}`,
          address: item.address,
          error: res.reason?.message || '获取失败',
        };
      }

      const data = res.value.data;
      const prev = prevRef.current[item.address];
      const totalNotional = toNum(data.totalNotional);
      const netExposure = toNum(data.netExposure);
      const deltaAbs = prev ? totalNotional - toNum(prev.totalNotional) : 0;
      const deltaNet = prev ? netExposure - toNum(prev.netExposure) : 0;
      if (prev && Math.abs(deltaAbs) >= FLOW_EPSILON*2) {
        hasChange = true;
      }
      prevRef.current[item.address] = { totalNotional, netExposure };

      return {
        ok: true,
        label: item.label || `监控${idx + 1}`,
        address: item.address,
        totalNotional,
        netExposure,
        deltaAbs,
        deltaNet,
        longNotional: toNum(data.longNotional),
        shortNotional: toNum(data.shortNotional),
        positions: Array.isArray(data.positions) ? data.positions : [],
      };
    });

    if (hasChange) onHasNew?.(true);

    const errs = nextRows.filter((r) => !r.ok);
    setError(errs.length > 0 ? errs.map((e) => `${e.label}: ${e.error}`).join(' | ') : '');
    setRows(nextRows);
    setUpdatedAt(Date.now());
    setLoading(false);
  }, [watchAddresses, onHasNew]);

  useEffect(() => {
    setLoading(true);
    fetchData();
    const timer = setInterval(fetchData, POLL_MS);
    return () => clearInterval(timer);
  }, [fetchData]);

  const consistency = useMemo(() => {
    const active = rows.filter((r) => r.ok && Math.abs(toNum(r.netExposure)) >= DIR_EPSILON);
    if (active.length < 2) {
      return { text: '方向一致性: 样本不足', color: colors.textMuted };
    }
    const longCount = active.filter((r) => toNum(r.netExposure) > 0).length;
    const shortCount = active.length - longCount;
    const major = Math.max(longCount, shortCount);
    const pct = Math.round((major / active.length) * 100);
    const dir = longCount >= shortCount ? '偏多' : '偏空';
    const color = pct >= 80 ? colors.greenLight : pct >= 60 ? colors.goldLight : colors.textSecondary;
    return { text: `方向一致性: ${pct}% (${dir})`, color };
  }, [rows]);

  return (
    <View style={styles.card}>
      <View style={styles.headerRow}>
        <Text style={styles.title}>鲸鱼钱包聚合看板</Text>
        <TouchableOpacity onPress={fetchData} style={styles.refreshBtn}>
          <Text style={styles.refreshText}>刷新</Text>
        </TouchableOpacity>
      </View>

      <Text style={[styles.consistency, { color: consistency.color }]}>{consistency.text}</Text>
      <Text style={styles.metaText}>最近更新: {fmtTime(updatedAt)} | 地址数: {rows.length}</Text>
      {error ? <Text style={styles.errorText}>{error}</Text> : null}

      {loading ? (
        <View style={styles.loadingBox}>
          <ActivityIndicator color={colors.gold} />
          <Text style={styles.loadingText}>加载持仓聚合...</Text>
        </View>
      ) : (
        rows.map((row) => {
          if (!row.ok) {
            return (
              <View key={row.address} style={styles.rowCard}>
                <Text style={styles.rowTitle}>{row.label}</Text>
                <Text style={styles.rowSub}>{row.address}</Text>
                <Text style={styles.errorText}>{row.error}</Text>
              </View>
            );
          }
          const flow = flowMeta(row.deltaAbs);
          const dir = dirMeta(row.netExposure);
          const top = row.positions[0];
          return (
            <View key={row.address} style={styles.rowCard}>
              <View style={styles.rowTop}>
                <Text style={styles.rowTitle}>{row.label}</Text>
                <Text style={[styles.badgeText, { color: flow.color }]}>{flow.text}</Text>
              </View>
              <Text style={styles.rowSub}>{row.address}</Text>
              <View style={styles.kpiRow}>
                <Text style={styles.kpi}>总持仓: ${fmtUsd(row.totalNotional)}</Text>
                <Text style={[styles.kpi, { color: dir.color }]}>方向: {dir.text}</Text>
              </View>
              <View style={styles.kpiRow}>
                <Text style={[styles.kpi, { color: colors.greenLight }]}>多: ${fmtUsd(row.longNotional)}</Text>
                <Text style={[styles.kpi, { color: colors.redLight }]}>空: ${fmtUsd(row.shortNotional)}</Text>
              </View>
              <View style={styles.kpiRow}>
                <Text style={styles.kpi}>Δ总持仓: {row.deltaAbs >= 0 ? '+' : ''}${fmtUsd(row.deltaAbs)}</Text>
                <Text style={styles.kpi}>Δ净敞口: {row.deltaNet >= 0 ? '+' : ''}${fmtUsd(row.deltaNet)}</Text>
              </View>
              <Text style={styles.kpi}>主仓: {top?.coin || '-'} {top ? `(${top.side || '-'})` : ''}</Text>
            </View>
          );
        })
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
    padding: spacing.md,
    marginTop: spacing.sm,
  },
  headerRow: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'center',
    marginBottom: spacing.xs,
  },
  title: {
    color: colors.white,
    fontSize: fontSize.md,
    fontWeight: '700',
  },
  refreshBtn: {
    borderRadius: radius.pill,
    paddingHorizontal: spacing.md,
    paddingVertical: spacing.xs,
    backgroundColor: colors.blueBg,
  },
  refreshText: {
    color: colors.blueLight,
    fontSize: fontSize.xs,
    fontWeight: '700',
  },
  consistency: {
    fontSize: fontSize.sm,
    fontWeight: '700',
  },
  metaText: {
    color: colors.textMuted,
    fontSize: fontSize.xs,
    marginTop: 2,
    marginBottom: spacing.xs,
  },
  rowCard: {
    backgroundColor: colors.surface,
    borderWidth: 1,
    borderColor: colors.cardBorder,
    borderRadius: radius.md,
    padding: spacing.sm,
    marginTop: spacing.xs,
  },
  rowTop: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'center',
    marginBottom: 2,
  },
  rowTitle: {
    color: colors.white,
    fontSize: fontSize.sm,
    fontWeight: '700',
  },
  badgeText: {
    fontSize: fontSize.xs,
    fontWeight: '700',
  },
  rowSub: {
    color: colors.textMuted,
    fontSize: fontSize.xs,
    marginBottom: spacing.xs,
  },
  kpiRow: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    gap: spacing.sm,
  },
  kpi: {
    color: colors.textSecondary,
    fontSize: fontSize.xs,
    marginTop: 2,
    fontVariant: ['tabular-nums'],
  },
  loadingBox: {
    paddingVertical: spacing.lg,
    alignItems: 'center',
    gap: spacing.xs,
  },
  loadingText: {
    color: colors.textMuted,
    fontSize: fontSize.sm,
  },
  errorText: {
    color: colors.redLight,
    fontSize: fontSize.xs,
    marginTop: 2,
  },
});

