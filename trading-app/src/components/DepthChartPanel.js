import React, { useCallback, useEffect, useState } from 'react';
import { View, Text, StyleSheet, TouchableOpacity, ActivityIndicator } from 'react-native';
import api from '../services/api';
import { colors, spacing, radius, fontSize } from '../services/theme';

function fmtPrice(n) {
  if (n == null || !Number.isFinite(n)) return '--';
  if (n >= 1000) return n.toFixed(2);
  if (n >= 1) return n.toFixed(4);
  return n.toFixed(6);
}

function fmtQty(n) {
  if (n == null || !Number.isFinite(n)) return '--';
  if (n >= 1000) return (n / 1000).toFixed(1) + 'K';
  return n.toFixed(4);
}

export default function DepthChartPanel({ symbol }) {
  const [loading, setLoading] = useState(false);
  const [data, setData] = useState(null);
  const [error, setError] = useState(null);

  const fetchData = useCallback(async () => {
    if (!symbol) return;
    setLoading(true);
    setError(null);
    try {
      const res = await api.getOrderBook(symbol, 20);
      setData(res);
    } catch (e) {
      setError(e.message || '加载失败');
    } finally {
      setLoading(false);
    }
  }, [symbol]);

  useEffect(() => {
    fetchData();
    const timer = setInterval(fetchData, 5000);
    return () => clearInterval(timer);
  }, [fetchData]);

  const bids = data?.data?.bids || data?.bids || [];
  const asks = data?.data?.asks || data?.asks || [];

  // 计算累计量和最大值用于条形比例
  let cumBids = [];
  let cumAsks = [];
  let cumB = 0;
  let cumA = 0;

  bids.forEach((b) => {
    const qty = parseFloat(b.quantity || b[1] || 0);
    cumB += qty;
    cumBids.push({ price: parseFloat(b.price || b[0] || 0), qty, cum: cumB });
  });

  asks.forEach((a) => {
    const qty = parseFloat(a.quantity || a[1] || 0);
    cumA += qty;
    cumAsks.push({ price: parseFloat(a.price || a[0] || 0), qty, cum: cumA });
  });

  const maxCum = Math.max(cumB, cumA, 1);

  return (
    <View style={styles.panel}>
      <View style={styles.header}>
        <Text style={styles.title}>深度图</Text>
        <TouchableOpacity onPress={fetchData} style={styles.refreshBtn}>
          <Text style={styles.refreshText}>刷新</Text>
        </TouchableOpacity>
      </View>

      {!symbol && <Text style={styles.emptyText}>请选择交易对</Text>}
      {loading && !data && <ActivityIndicator color={colors.gold} style={{ marginVertical: spacing.lg }} />}
      {error && <Text style={styles.errorText}>{error}</Text>}

      {data && (
        <>
          {/* 买卖压力概览 */}
          <View style={styles.pressureRow}>
            <View style={[styles.pressureBar, styles.bidBar, { flex: cumB || 0.1 }]} />
            <View style={[styles.pressureBar, styles.askBar, { flex: cumA || 0.1 }]} />
          </View>
          <View style={styles.pressureLabelRow}>
            <Text style={styles.bidText}>买 {fmtQty(cumB)}</Text>
            <Text style={styles.askText}>卖 {fmtQty(cumA)}</Text>
          </View>

          {/* 列标题 */}
          <View style={styles.colHeader}>
            <Text style={[styles.colLabel, { textAlign: 'left' }]}>买单 (Bids)</Text>
            <Text style={[styles.colLabel, { textAlign: 'right' }]}>卖单 (Asks)</Text>
          </View>

          {/* 深度条形图 */}
          <View style={styles.depthContainer}>
            {/* 买方 */}
            <View style={styles.depthSide}>
              {cumBids.slice(0, 15).map((b, i) => (
                <View key={i} style={styles.depthRow}>
                  <Text style={[styles.depthPrice, styles.bidText]}>{fmtPrice(b.price)}</Text>
                  <View style={styles.barTrack}>
                    <View style={[styles.bidFill, { width: `${(b.cum / maxCum) * 100}%` }]} />
                  </View>
                  <Text style={styles.depthQty}>{fmtQty(b.qty)}</Text>
                </View>
              ))}
            </View>

            {/* 分隔线 */}
            <View style={styles.separator} />

            {/* 卖方 */}
            <View style={styles.depthSide}>
              {cumAsks.slice(0, 15).map((a, i) => (
                <View key={i} style={styles.depthRow}>
                  <Text style={[styles.depthPrice, styles.askText]}>{fmtPrice(a.price)}</Text>
                  <View style={styles.barTrack}>
                    <View style={[styles.askFill, { width: `${(a.cum / maxCum) * 100}%` }]} />
                  </View>
                  <Text style={styles.depthQty}>{fmtQty(a.qty)}</Text>
                </View>
              ))}
            </View>
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

  bidText: { color: colors.green },
  askText: { color: colors.red },

  pressureRow: {
    flexDirection: 'row',
    height: 8,
    borderRadius: 4,
    overflow: 'hidden',
    marginBottom: 4,
  },
  pressureBar: { height: '100%' },
  bidBar: { backgroundColor: colors.green },
  askBar: { backgroundColor: colors.red },
  pressureLabelRow: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    marginBottom: spacing.md,
  },

  colHeader: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    marginBottom: spacing.xs,
  },
  colLabel: { color: colors.textMuted, fontSize: fontSize.xs },

  depthContainer: { gap: spacing.sm },
  depthSide: { gap: 2 },
  separator: {
    height: 1,
    backgroundColor: colors.cardBorder,
    marginVertical: spacing.xs,
  },
  depthRow: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 4,
  },
  depthPrice: { width: 80, fontSize: 10, fontVariant: ['tabular-nums'] },
  barTrack: {
    flex: 1,
    height: 8,
    backgroundColor: colors.surface,
    borderRadius: 4,
    overflow: 'hidden',
  },
  bidFill: { height: '100%', backgroundColor: colors.green + '60', borderRadius: 4 },
  askFill: { height: '100%', backgroundColor: colors.red + '60', borderRadius: 4 },
  depthQty: {
    width: 55,
    color: colors.textSecondary,
    fontSize: 10,
    textAlign: 'right',
    fontVariant: ['tabular-nums'],
  },
});
