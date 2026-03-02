import React, { useCallback, useEffect, useState } from 'react';
import { View, Text, StyleSheet, TouchableOpacity, ScrollView, ActivityIndicator } from 'react-native';
import api from '../services/api';
import { colors, spacing, radius, fontSize } from '../services/theme';

function fmtUSD(n) {
  if (n == null || !Number.isFinite(n)) return '--';
  const sign = n >= 0 ? '+' : '';
  return sign + n.toFixed(2);
}

function fmtPct(n) {
  if (n == null || !Number.isFinite(n)) return '--';
  return n.toFixed(1) + '%';
}

export default function StrategyComparePanel() {
  const [loading, setLoading] = useState(false);
  const [data, setData] = useState(null);
  const [error, setError] = useState(null);

  const fetchData = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const res = await api.getAnalyticsAttribution();
      setData(res);
    } catch (e) {
      setError(e.message || '加载失败');
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    fetchData();
  }, [fetchData]);

  const strategies = data?.data?.bySource || data?.bySource || [];
  const summary = data?.data?.summary || data?.summary || {};

  return (
    <View style={styles.panel}>
      <View style={styles.header}>
        <Text style={styles.title}>策略对比</Text>
        <TouchableOpacity onPress={fetchData} style={styles.refreshBtn}>
          <Text style={styles.refreshText}>刷新</Text>
        </TouchableOpacity>
      </View>

      {loading && <ActivityIndicator color={colors.gold} style={{ marginVertical: spacing.lg }} />}
      {error && <Text style={styles.errorText}>{error}</Text>}

      {!loading && !error && (
        <>
          {/* 整体汇总 */}
          <View style={styles.summaryRow}>
            <View style={styles.summaryCard}>
              <Text style={styles.summaryLabel}>总盈亏</Text>
              <Text style={[styles.summaryValue, (summary.totalPnl || 0) >= 0 ? styles.profit : styles.loss]}>
                {fmtUSD(summary.totalPnl || 0)}
              </Text>
            </View>
            <View style={styles.summaryCard}>
              <Text style={styles.summaryLabel}>总交易</Text>
              <Text style={styles.summaryValue}>{summary.totalTrades || 0}</Text>
            </View>
            <View style={styles.summaryCard}>
              <Text style={styles.summaryLabel}>总胜率</Text>
              <Text style={styles.summaryValue}>{fmtPct(summary.winRate || 0)}</Text>
            </View>
          </View>

          {/* 表头 */}
          <View style={styles.tableHeader}>
            <Text style={[styles.hCell, { flex: 1.5 }]}>策略</Text>
            <Text style={[styles.hCell, { flex: 1, textAlign: 'right' }]}>盈亏</Text>
            <Text style={[styles.hCell, { flex: 0.7, textAlign: 'right' }]}>胜率</Text>
            <Text style={[styles.hCell, { flex: 0.6, textAlign: 'right' }]}>次数</Text>
          </View>

          {strategies.length === 0 && (
            <Text style={styles.emptyText}>暂无策略数据</Text>
          )}

          <ScrollView style={styles.listScroll} nestedScrollEnabled>
            {strategies.map((s, i) => {
              const pnl = s.totalPnl || s.pnl || 0;
              const winRate = s.winRate || 0;
              const trades = s.totalTrades || s.trades || 0;
              const name = s.source || s.strategy || '未知';
              return (
                <View key={i} style={[styles.row, i % 2 === 0 && styles.rowAlt]}>
                  <Text style={[styles.cell, { flex: 1.5 }]} numberOfLines={1}>{name}</Text>
                  <Text style={[styles.cell, { flex: 1, textAlign: 'right' }, pnl >= 0 ? styles.profit : styles.loss]}>
                    {fmtUSD(pnl)}
                  </Text>
                  <Text style={[styles.cell, { flex: 0.7, textAlign: 'right' }]}>{fmtPct(winRate)}</Text>
                  <Text style={[styles.cell, { flex: 0.6, textAlign: 'right' }]}>{trades}</Text>
                </View>
              );
            })}
          </ScrollView>

          {/* 盈亏柱状条 */}
          {strategies.length > 0 && (
            <View style={styles.barSection}>
              <Text style={styles.barTitle}>盈亏对比</Text>
              {strategies.map((s, i) => {
                const pnl = s.totalPnl || s.pnl || 0;
                const maxAbs = Math.max(...strategies.map(x => Math.abs(x.totalPnl || x.pnl || 0)), 1);
                const width = Math.abs(pnl) / maxAbs * 100;
                const name = s.source || s.strategy || '未知';
                return (
                  <View key={i} style={styles.barRow}>
                    <Text style={styles.barLabel} numberOfLines={1}>{name}</Text>
                    <View style={styles.barTrack}>
                      <View style={[
                        styles.barFill,
                        { width: `${Math.max(width, 2)}%` },
                        pnl >= 0 ? styles.profitBar : styles.lossBar,
                      ]} />
                    </View>
                    <Text style={[styles.barValue, pnl >= 0 ? styles.profit : styles.loss]}>
                      {fmtUSD(pnl)}
                    </Text>
                  </View>
                );
              })}
            </View>
          )}
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
    marginBottom: spacing.md,
  },
  title: { color: colors.white, fontSize: fontSize.lg, fontWeight: '700' },
  refreshBtn: {
    paddingHorizontal: spacing.md,
    paddingVertical: spacing.xs,
    borderRadius: radius.sm,
    backgroundColor: colors.goldBg,
  },
  refreshText: { color: colors.gold, fontSize: fontSize.sm, fontWeight: '600' },

  errorText: { color: colors.red, fontSize: fontSize.sm, textAlign: 'center', marginVertical: spacing.md },
  emptyText: { color: colors.textMuted, fontSize: fontSize.sm, textAlign: 'center', marginVertical: spacing.xl },

  profit: { color: colors.green },
  loss: { color: colors.red },

  summaryRow: { flexDirection: 'row', gap: spacing.sm, marginBottom: spacing.md },
  summaryCard: {
    flex: 1,
    backgroundColor: colors.surface,
    borderRadius: radius.md,
    padding: spacing.md,
    alignItems: 'center',
  },
  summaryLabel: { color: colors.textMuted, fontSize: fontSize.xs, marginBottom: 4 },
  summaryValue: { color: colors.white, fontSize: fontSize.md, fontWeight: '700', fontVariant: ['tabular-nums'] },

  tableHeader: {
    flexDirection: 'row',
    borderBottomWidth: 1,
    borderBottomColor: colors.cardBorder,
    paddingBottom: 4,
    marginBottom: 2,
  },
  hCell: { color: colors.textMuted, fontSize: fontSize.xs },
  listScroll: { maxHeight: 280 },
  row: { flexDirection: 'row', paddingVertical: 6, paddingHorizontal: 2 },
  rowAlt: { backgroundColor: colors.surface },
  cell: { color: colors.text, fontSize: 11, fontVariant: ['tabular-nums'] },

  barSection: {
    marginTop: spacing.md,
    backgroundColor: colors.surface,
    borderRadius: radius.md,
    padding: spacing.md,
  },
  barTitle: { color: colors.white, fontSize: fontSize.md, fontWeight: '700', marginBottom: spacing.sm },
  barRow: {
    flexDirection: 'row',
    alignItems: 'center',
    marginBottom: 6,
  },
  barLabel: { width: 70, color: colors.textSecondary, fontSize: 10 },
  barTrack: {
    flex: 1,
    height: 10,
    backgroundColor: colors.card,
    borderRadius: 5,
    overflow: 'hidden',
    marginHorizontal: 6,
  },
  barFill: { height: '100%', borderRadius: 5 },
  profitBar: { backgroundColor: colors.green },
  lossBar: { backgroundColor: colors.red },
  barValue: { width: 60, fontSize: 10, fontWeight: '600', textAlign: 'right', fontVariant: ['tabular-nums'] },
});
