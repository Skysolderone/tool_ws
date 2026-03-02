import React, { useCallback, useEffect, useState } from 'react';
import { View, Text, StyleSheet, TouchableOpacity, ScrollView, ActivityIndicator } from 'react-native';
import api from '../services/api';
import { colors, spacing, radius, fontSize } from '../services/theme';

function fmtNum(n, decimals = 2) {
  if (n == null || !Number.isFinite(n)) return '--';
  return n.toFixed(decimals);
}

function fmtPct(n) {
  if (n == null || !Number.isFinite(n)) return '--';
  const sign = n >= 0 ? '+' : '';
  return sign + n.toFixed(2) + '%';
}

function fmtUSD(n) {
  if (n == null || !Number.isFinite(n)) return '--';
  const sign = n >= 0 ? '+' : '';
  return sign + n.toFixed(2);
}

export default function EquityCurvePanel() {
  const [loading, setLoading] = useState(false);
  const [data, setData] = useState(null);
  const [error, setError] = useState(null);
  const [period, setPeriod] = useState('daily');

  const fetchData = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const res = await api.getAnalyticsJournal({ period });
      setData(res);
    } catch (e) {
      setError(e.message || '加载失败');
    } finally {
      setLoading(false);
    }
  }, [period]);

  useEffect(() => {
    fetchData();
  }, [fetchData]);

  const entries = data?.entries || data?.data || [];
  const summary = data?.summary || {};

  // 计算统计
  const profitDays = entries.filter((e) => (e.pnl || 0) > 0).length;
  const lossDays = entries.filter((e) => (e.pnl || 0) < 0).length;
  const maxProfit = entries.reduce((m, e) => Math.max(m, e.pnl || 0), 0);
  const maxLoss = entries.reduce((m, e) => Math.min(m, e.pnl || 0), 0);

  // 计算最大回撤
  let peak = 0;
  let maxDrawdown = 0;
  entries.forEach((e) => {
    const bal = e.balance || e.equity || 0;
    if (bal > peak) peak = bal;
    if (peak > 0) {
      const dd = (peak - bal) / peak * 100;
      if (dd > maxDrawdown) maxDrawdown = dd;
    }
  });

  const currentBalance = entries.length > 0
    ? (entries[entries.length - 1].balance || entries[entries.length - 1].equity || 0)
    : (summary.balance || 0);

  const totalPnl = summary.totalPnl || entries.reduce((s, e) => s + (e.pnl || 0), 0);
  const totalPnlPct = summary.totalPnlPct || (currentBalance > 0 && entries.length > 0
    ? (totalPnl / (currentBalance - totalPnl)) * 100 : 0);

  return (
    <View style={styles.panel}>
      <View style={styles.header}>
        <Text style={styles.title}>权益曲线</Text>
        <TouchableOpacity onPress={fetchData} style={styles.refreshBtn}>
          <Text style={styles.refreshText}>刷新</Text>
        </TouchableOpacity>
      </View>

      {/* 周期切换 */}
      <View style={styles.periodRow}>
        {['daily', 'weekly'].map((p) => (
          <TouchableOpacity
            key={p}
            style={[styles.chip, period === p && styles.chipActive]}
            onPress={() => setPeriod(p)}
          >
            <Text style={[styles.chipText, period === p && styles.chipTextActive]}>
              {p === 'daily' ? '日' : '周'}
            </Text>
          </TouchableOpacity>
        ))}
      </View>

      {loading && <ActivityIndicator color={colors.gold} style={{ marginVertical: spacing.lg }} />}
      {error && <Text style={styles.errorText}>{error}</Text>}

      {!loading && !error && (
        <>
          {/* 概览卡片 */}
          <View style={styles.overviewRow}>
            <View style={styles.overviewCard}>
              <Text style={styles.overviewLabel}>当前余额</Text>
              <Text style={styles.overviewValue}>{fmtNum(currentBalance)}</Text>
            </View>
            <View style={styles.overviewCard}>
              <Text style={styles.overviewLabel}>总收益率</Text>
              <Text style={[styles.overviewValue, totalPnlPct >= 0 ? styles.profitText : styles.lossText]}>
                {fmtPct(totalPnlPct)}
              </Text>
            </View>
            <View style={styles.overviewCard}>
              <Text style={styles.overviewLabel}>最大回撤</Text>
              <Text style={[styles.overviewValue, styles.lossText]}>
                {maxDrawdown > 0 ? '-' + maxDrawdown.toFixed(2) + '%' : '0%'}
              </Text>
            </View>
          </View>

          {/* 每日净值列表 */}
          <View style={styles.tableHeader}>
            <Text style={[styles.hCell, { flex: 1.2 }]}>日期</Text>
            <Text style={[styles.hCell, { flex: 1, textAlign: 'right' }]}>余额</Text>
            <Text style={[styles.hCell, { flex: 1, textAlign: 'right' }]}>日收益</Text>
            <Text style={[styles.hCell, { flex: 1, textAlign: 'right' }]}>累计%</Text>
          </View>

          {entries.length === 0 && (
            <Text style={styles.emptyText}>暂无数据</Text>
          )}

          <ScrollView style={styles.listScroll} nestedScrollEnabled>
            {[...entries].reverse().map((entry, i) => {
              const pnl = entry.pnl || 0;
              const cumPct = entry.cumPnlPct || entry.totalPnlPct || 0;
              const date = entry.date || entry.period || '--';
              const bal = entry.balance || entry.equity || 0;
              return (
                <View key={i} style={[styles.row, i % 2 === 0 && styles.rowAlt]}>
                  <Text style={[styles.cell, { flex: 1.2 }]}>{date}</Text>
                  <Text style={[styles.cell, { flex: 1, textAlign: 'right' }]}>{fmtNum(bal)}</Text>
                  <Text style={[styles.cell, { flex: 1, textAlign: 'right' }, pnl >= 0 ? styles.profitText : styles.lossText]}>
                    {fmtUSD(pnl)}
                  </Text>
                  <Text style={[styles.cell, { flex: 1, textAlign: 'right' }, cumPct >= 0 ? styles.profitText : styles.lossText]}>
                    {fmtPct(cumPct)}
                  </Text>
                </View>
              );
            })}
          </ScrollView>

          {/* 收益分布 */}
          <View style={styles.statsCard}>
            <Text style={styles.statsTitle}>收益分布</Text>
            <View style={styles.statsGrid}>
              <View style={styles.statItem}>
                <Text style={styles.statLabel}>盈利天数</Text>
                <Text style={[styles.statValue, styles.profitText]}>{profitDays}</Text>
              </View>
              <View style={styles.statItem}>
                <Text style={styles.statLabel}>亏损天数</Text>
                <Text style={[styles.statValue, styles.lossText]}>{lossDays}</Text>
              </View>
              <View style={styles.statItem}>
                <Text style={styles.statLabel}>最大日盈</Text>
                <Text style={[styles.statValue, styles.profitText]}>{fmtUSD(maxProfit)}</Text>
              </View>
              <View style={styles.statItem}>
                <Text style={styles.statLabel}>最大日亏</Text>
                <Text style={[styles.statValue, styles.lossText]}>{fmtUSD(maxLoss)}</Text>
              </View>
            </View>

            {/* 盈亏天数比例条 */}
            {(profitDays + lossDays) > 0 && (
              <View style={styles.ratioRow}>
                <View style={[styles.ratioBar, styles.profitBar, { flex: profitDays || 0.1 }]} />
                <View style={[styles.ratioBar, styles.lossBar, { flex: lossDays || 0.1 }]} />
              </View>
            )}
          </View>
        </>
      )}
    </View>
  );
}

const styles = StyleSheet.create({
  panel: {
    backgroundColor: colors.card,
    borderRadius: radius.lg,
    padding: spacing.lg,
    borderWidth: 1,
    borderColor: colors.cardBorder,
  },
  header: {
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
    marginBottom: spacing.sm,
  },
  title: { color: colors.white, fontSize: fontSize.lg, fontWeight: '700' },
  refreshBtn: {
    paddingHorizontal: spacing.md,
    paddingVertical: spacing.xs,
    borderRadius: radius.sm,
    backgroundColor: colors.goldBg,
  },
  refreshText: { color: colors.gold, fontSize: fontSize.sm, fontWeight: '600' },

  periodRow: {
    flexDirection: 'row',
    gap: 6,
    marginBottom: spacing.md,
  },
  chip: {
    paddingHorizontal: spacing.md,
    paddingVertical: spacing.xs,
    borderRadius: radius.sm,
    backgroundColor: colors.surface,
    borderWidth: 1,
    borderColor: colors.cardBorder,
  },
  chipActive: { backgroundColor: colors.goldBg, borderColor: colors.gold },
  chipText: { color: colors.textSecondary, fontSize: fontSize.sm, fontWeight: '600' },
  chipTextActive: { color: colors.gold, fontWeight: '700' },

  errorText: { color: colors.red, fontSize: fontSize.sm, textAlign: 'center', marginVertical: spacing.md },
  emptyText: { color: colors.textMuted, fontSize: fontSize.sm, textAlign: 'center', marginVertical: spacing.xl },

  // Overview
  overviewRow: {
    flexDirection: 'row',
    gap: spacing.sm,
    marginBottom: spacing.md,
  },
  overviewCard: {
    flex: 1,
    backgroundColor: colors.surface,
    borderRadius: radius.md,
    padding: spacing.md,
    alignItems: 'center',
  },
  overviewLabel: { color: colors.textMuted, fontSize: fontSize.xs, marginBottom: 4 },
  overviewValue: { color: colors.white, fontSize: fontSize.md, fontWeight: '700', fontVariant: ['tabular-nums'] },

  // Table
  tableHeader: {
    flexDirection: 'row',
    borderBottomWidth: 1,
    borderBottomColor: colors.cardBorder,
    paddingBottom: 4,
    marginBottom: 2,
  },
  hCell: { color: colors.textMuted, fontSize: fontSize.xs },
  listScroll: { maxHeight: 300 },
  row: {
    flexDirection: 'row',
    paddingVertical: 6,
    paddingHorizontal: 2,
  },
  rowAlt: { backgroundColor: colors.surface },
  cell: {
    color: colors.text,
    fontSize: 11,
    fontVariant: ['tabular-nums'],
  },

  profitText: { color: colors.green },
  lossText: { color: colors.red },

  // Stats
  statsCard: {
    marginTop: spacing.md,
    backgroundColor: colors.surface,
    borderRadius: radius.md,
    padding: spacing.md,
  },
  statsTitle: { color: colors.white, fontSize: fontSize.md, fontWeight: '700', marginBottom: spacing.sm },
  statsGrid: {
    flexDirection: 'row',
    flexWrap: 'wrap',
    gap: spacing.sm,
  },
  statItem: {
    width: '45%',
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'center',
    paddingVertical: 4,
  },
  statLabel: { color: colors.textSecondary, fontSize: fontSize.sm },
  statValue: { fontSize: fontSize.md, fontWeight: '700', fontVariant: ['tabular-nums'] },

  ratioRow: {
    flexDirection: 'row',
    height: 5,
    borderRadius: 3,
    overflow: 'hidden',
    marginTop: spacing.md,
  },
  ratioBar: { height: '100%' },
  profitBar: { backgroundColor: colors.green },
  lossBar: { backgroundColor: colors.red },
});
