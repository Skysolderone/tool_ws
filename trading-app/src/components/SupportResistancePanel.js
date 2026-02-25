import React, { useState, useEffect, useCallback, useRef, useMemo } from 'react';
import {
  View, Text, StyleSheet, TouchableOpacity, ActivityIndicator,
} from 'react-native';
import { colors, spacing, radius, fontSize } from '../services/theme';
import api from '../services/api';

const STARS = ['', '★', '★★', '★★★', '★★★★'];

export default function SupportResistancePanel({ symbol, externalMarkPrice }) {
  const [data, setData] = useState(null);
  const [loading, setLoading] = useState(false);
  const [showPivot, setShowPivot] = useState(false);
  const timerRef = useRef(null);

  const fetchLevels = useCallback(async () => {
    if (!symbol) return;
    try {
      setLoading(true);
      const res = await api.getSRLevels(symbol);
      if (res?.data) setData(res.data);
    } catch (_) {}
    setLoading(false);
  }, [symbol]);

  useEffect(() => {
    fetchLevels();
    timerRef.current = setInterval(fetchLevels, 60000); // 60s 自动刷新
    return () => { if (timerRef.current) clearInterval(timerRef.current); };
  }, [fetchLevels]);

  // 用 externalMarkPrice 实时更新距离
  const currentPrice = externalMarkPrice || data?.currentPrice || 0;

  // 动态重算距离
  const supports = useMemo(() => {
    if (!data?.supports || !currentPrice) return data?.supports || [];
    return data.supports.map((lv) => ({
      ...lv,
      distance: Math.round(((lv.price - currentPrice) / currentPrice) * 10000) / 100,
    }));
  }, [data?.supports, currentPrice]);

  const resistances = useMemo(() => {
    if (!data?.resistances || !currentPrice) return data?.resistances || [];
    return data.resistances.map((lv) => ({
      ...lv,
      distance: Math.round(((lv.price - currentPrice) / currentPrice) * 10000) / 100,
    }));
  }, [data?.resistances, currentPrice]);

  const closestSupport = supports.length > 0 ? supports[0] : null;
  const closestResist = resistances.length > 0 ? resistances[0] : null;

  const renderLevel = (level, isClosest, type) => {
    const isResist = type === 'RESISTANCE';
    const bgColor = isClosest
      ? (isResist ? colors.redBg : colors.greenBg)
      : colors.surface;
    const borderColor = isClosest
      ? (isResist ? colors.red : colors.green)
      : 'transparent';
    const priceColor = isClosest
      ? (isResist ? colors.redLight : colors.greenLight)
      : colors.text;

    // 强度条宽度
    const strengthPct = Math.min(level.strength / 4, 1) * 100;

    return (
      <View key={level.price} style={[styles.levelRow, { backgroundColor: bgColor, borderLeftColor: borderColor, borderLeftWidth: isClosest ? 3 : 0 }]}>
        <View style={styles.levelMain}>
          <View style={styles.levelPriceRow}>
            <Text style={[styles.levelPrice, { color: priceColor }]}>
              {level.price.toFixed(2)}
            </Text>
            <Text style={styles.levelStars}>{STARS[level.strength] || STARS[4]}</Text>
          </View>
          {/* 强度条 */}
          <View style={styles.strengthTrack}>
            <View style={[styles.strengthFill, {
              width: `${strengthPct}%`,
              backgroundColor: isResist ? colors.red : colors.green,
            }]} />
          </View>
          <View style={styles.levelMeta}>
            <Text style={styles.levelTF}>{level.timeframes?.join(' / ')}</Text>
            <Text style={styles.levelTouch}>触及 {level.touchCount}x</Text>
          </View>
        </View>
        <View style={styles.levelRight}>
          <Text style={[styles.levelDist, {
            color: isResist ? colors.redLight : colors.greenLight,
          }]}>
            {level.distance >= 0 ? '+' : ''}{level.distance.toFixed(2)}%
          </Text>
        </View>
      </View>
    );
  };

  const renderPivotRow = (label, value, type) => {
    if (!value) return null;
    const dist = currentPrice ? ((value - currentPrice) / currentPrice * 100) : 0;
    const isPivot = type === 'pp';
    const isR = type === 'r';
    const color = isPivot ? colors.gold : isR ? colors.redLight : colors.greenLight;

    return (
      <View style={styles.pivotRow} key={label}>
        <Text style={[styles.pivotLabel, { color }]}>{label}</Text>
        <Text style={[styles.pivotPrice, { color }]}>{value.toFixed(2)}</Text>
        <Text style={styles.pivotDist}>{dist >= 0 ? '+' : ''}{dist.toFixed(2)}%</Text>
      </View>
    );
  };

  return (
    <View style={styles.card}>
      {/* Header */}
      <View style={styles.header}>
        <Text style={styles.title}>支撑 / 阻力位</Text>
        <TouchableOpacity style={styles.refreshBtn} onPress={fetchLevels} disabled={loading}>
          {loading ? (
            <ActivityIndicator size="small" color={colors.gold} />
          ) : (
            <Text style={styles.refreshText}>刷新</Text>
          )}
        </TouchableOpacity>
      </View>

      {/* 当前价 */}
      {currentPrice > 0 && (
        <View style={styles.priceBar}>
          <Text style={styles.priceLabel}>当前价格</Text>
          <Text style={styles.priceValue}>{currentPrice.toFixed(2)}</Text>
        </View>
      )}

      {/* 最近支撑/阻力概览 */}
      {(closestResist || closestSupport) && (
        <View style={styles.overviewRow}>
          {closestResist && (
            <View style={[styles.overviewItem, styles.overviewResist]}>
              <Text style={styles.overviewLabel}>最近阻力</Text>
              <Text style={[styles.overviewPrice, { color: colors.redLight }]}>
                {closestResist.price.toFixed(2)}
              </Text>
              <Text style={[styles.overviewDist, { color: colors.redLight }]}>
                +{Math.abs(closestResist.distance).toFixed(2)}%
              </Text>
            </View>
          )}
          {closestSupport && (
            <View style={[styles.overviewItem, styles.overviewSupport]}>
              <Text style={styles.overviewLabel}>最近支撑</Text>
              <Text style={[styles.overviewPrice, { color: colors.greenLight }]}>
                {closestSupport.price.toFixed(2)}
              </Text>
              <Text style={[styles.overviewDist, { color: colors.greenLight }]}>
                -{Math.abs(closestSupport.distance).toFixed(2)}%
              </Text>
            </View>
          )}
        </View>
      )}

      {/* 阻力位列表 */}
      {resistances.length > 0 && (
        <View style={styles.section}>
          <Text style={[styles.sectionTitle, { color: colors.red }]}>阻力位</Text>
          {resistances.slice(0, 5).map((lv, idx) => renderLevel(lv, idx === 0, 'RESISTANCE'))}
        </View>
      )}

      {/* 支撑位列表 */}
      {supports.length > 0 && (
        <View style={styles.section}>
          <Text style={[styles.sectionTitle, { color: colors.green }]}>支撑位</Text>
          {supports.slice(0, 5).map((lv, idx) => renderLevel(lv, idx === 0, 'SUPPORT'))}
        </View>
      )}

      {!data && !loading && (
        <Text style={styles.emptyText}>暂无数据，点击刷新</Text>
      )}

      {/* Pivot Points */}
      {data?.pivotPoints && (
        <View style={styles.pivotSection}>
          <TouchableOpacity style={styles.pivotHeader} onPress={() => setShowPivot(!showPivot)}>
            <Text style={styles.pivotTitle}>Pivot Points（日线）</Text>
            <Text style={styles.expandIcon}>{showPivot ? '▾' : '▸'}</Text>
          </TouchableOpacity>
          {showPivot && (
            <View style={styles.pivotGrid}>
              {renderPivotRow('R3', data.pivotPoints.r3, 'r')}
              {renderPivotRow('R2', data.pivotPoints.r2, 'r')}
              {renderPivotRow('R1', data.pivotPoints.r1, 'r')}
              {renderPivotRow('PP', data.pivotPoints.pp, 'pp')}
              {renderPivotRow('S1', data.pivotPoints.s1, 's')}
              {renderPivotRow('S2', data.pivotPoints.s2, 's')}
              {renderPivotRow('S3', data.pivotPoints.s3, 's')}
            </View>
          )}
        </View>
      )}

      {/* 更新时间 */}
      {data?.calculatedAt && (
        <Text style={styles.updateTime}>计算于 {data.calculatedAt}</Text>
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
    marginBottom: spacing.md,
  },
  title: {
    fontSize: fontSize.lg,
    fontWeight: '700',
    color: colors.white,
  },
  refreshBtn: {
    paddingHorizontal: spacing.md,
    paddingVertical: spacing.sm,
    borderRadius: radius.md,
    backgroundColor: colors.surface,
    borderWidth: 1,
    borderColor: colors.cardBorder,
    minWidth: 50,
    alignItems: 'center',
  },
  refreshText: {
    fontSize: fontSize.sm,
    fontWeight: '600',
    color: colors.gold,
  },

  // 当前价格
  priceBar: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'center',
    backgroundColor: colors.goldBg,
    borderRadius: radius.md,
    paddingHorizontal: spacing.md,
    paddingVertical: spacing.sm,
    marginBottom: spacing.md,
    borderLeftWidth: 3,
    borderLeftColor: colors.gold,
  },
  priceLabel: {
    fontSize: fontSize.sm,
    color: colors.textSecondary,
  },
  priceValue: {
    fontSize: fontSize.xl,
    fontWeight: '800',
    color: colors.gold,
    fontVariant: ['tabular-nums'],
  },

  // 概览卡
  overviewRow: {
    flexDirection: 'row',
    gap: spacing.sm,
    marginBottom: spacing.md,
  },
  overviewItem: {
    flex: 1,
    borderRadius: radius.md,
    padding: spacing.md,
    alignItems: 'center',
  },
  overviewResist: {
    backgroundColor: colors.redBg,
    borderWidth: 1,
    borderColor: 'rgba(217,68,82,0.2)',
  },
  overviewSupport: {
    backgroundColor: colors.greenBg,
    borderWidth: 1,
    borderColor: 'rgba(46,189,110,0.2)',
  },
  overviewLabel: {
    fontSize: fontSize.xs,
    color: colors.textMuted,
    marginBottom: spacing.xs,
  },
  overviewPrice: {
    fontSize: fontSize.lg,
    fontWeight: '800',
    fontVariant: ['tabular-nums'],
  },
  overviewDist: {
    fontSize: fontSize.sm,
    fontWeight: '600',
    marginTop: 2,
    fontVariant: ['tabular-nums'],
  },

  // 区域标题
  section: {
    marginBottom: spacing.sm,
  },
  sectionTitle: {
    fontSize: fontSize.sm,
    fontWeight: '700',
    marginBottom: spacing.xs,
  },

  // 级别行
  levelRow: {
    flexDirection: 'row',
    alignItems: 'center',
    borderRadius: radius.sm,
    padding: spacing.sm,
    marginBottom: spacing.xs,
  },
  levelMain: {
    flex: 1,
  },
  levelPriceRow: {
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
  },
  levelPrice: {
    fontSize: fontSize.md,
    fontWeight: '700',
    fontVariant: ['tabular-nums'],
  },
  levelStars: {
    fontSize: fontSize.xs,
    color: colors.gold,
  },
  strengthTrack: {
    height: 3,
    backgroundColor: colors.divider,
    borderRadius: 1.5,
    marginVertical: spacing.xs,
    overflow: 'hidden',
  },
  strengthFill: {
    height: '100%',
    borderRadius: 1.5,
  },
  levelMeta: {
    flexDirection: 'row',
    justifyContent: 'space-between',
  },
  levelTF: {
    fontSize: fontSize.xs,
    color: colors.textMuted,
  },
  levelTouch: {
    fontSize: fontSize.xs,
    color: colors.textMuted,
  },
  levelRight: {
    marginLeft: spacing.md,
    alignItems: 'flex-end',
    minWidth: 55,
  },
  levelDist: {
    fontSize: fontSize.sm,
    fontWeight: '700',
    fontVariant: ['tabular-nums'],
  },

  emptyText: {
    color: colors.textMuted,
    textAlign: 'center',
    paddingVertical: spacing.xl,
    fontSize: fontSize.sm,
  },

  // Pivot Points
  pivotSection: {
    marginTop: spacing.sm,
    borderTopWidth: 1,
    borderTopColor: colors.divider,
    paddingTop: spacing.sm,
  },
  pivotHeader: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'center',
  },
  pivotTitle: {
    fontSize: fontSize.sm,
    fontWeight: '700',
    color: colors.textSecondary,
  },
  expandIcon: {
    fontSize: fontSize.md,
    color: colors.textMuted,
  },
  pivotGrid: {
    marginTop: spacing.sm,
  },
  pivotRow: {
    flexDirection: 'row',
    alignItems: 'center',
    paddingVertical: spacing.xs,
    borderBottomWidth: StyleSheet.hairlineWidth,
    borderBottomColor: colors.divider,
  },
  pivotLabel: {
    fontSize: fontSize.sm,
    fontWeight: '700',
    width: 30,
  },
  pivotPrice: {
    fontSize: fontSize.sm,
    fontWeight: '600',
    fontVariant: ['tabular-nums'],
    flex: 1,
    textAlign: 'center',
  },
  pivotDist: {
    fontSize: fontSize.xs,
    color: colors.textMuted,
    fontVariant: ['tabular-nums'],
    width: 55,
    textAlign: 'right',
  },

  // 更新时间
  updateTime: {
    fontSize: fontSize.xs,
    color: colors.textMuted,
    textAlign: 'right',
    marginTop: spacing.sm,
  },
});
