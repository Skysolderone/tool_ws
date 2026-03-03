import React, { useState, useEffect, useCallback, useRef, useMemo } from 'react';
import {
  View, Text, StyleSheet, TouchableOpacity, ActivityIndicator,
} from 'react-native';
import { colors, spacing, radius, fontSize } from '../services/theme';
import api from '../services/api';

const STARS = ['', '★', '★★', '★★★', '★★★★'];
const SR_TF_OPTIONS = [
  { key: 'all', label: '全部' },
  { key: '1h', label: '1H' },
  { key: '4h', label: '4H' },
  { key: '1d', label: '1D' },
  { key: '1w', label: '1W' },
];

export default function SupportResistancePanel({ symbol, externalMarkPrice }) {
  const [data, setData] = useState(null);
  const [loading, setLoading] = useState(false);
  const [showPivot, setShowPivot] = useState(false);
  const [selectedTf, setSelectedTf] = useState('all');
  const [expandedReasons, setExpandedReasons] = useState({});
  const timerRef = useRef(null);

  const toggleReason = useCallback((key) => {
    setExpandedReasons((prev) => ({ ...prev, [key]: !prev[key] }));
  }, []);

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

  // 动态重算点位距离（兼容旧展示与回退）
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

  // 根据价格查找最近的 level reason
  const findReason = useCallback((levels, midPrice) => {
    if (!levels?.length || !midPrice) return '';
    let best = null;
    let bestDist = Infinity;
    for (const lv of levels) {
      const d = Math.abs(lv.price - midPrice);
      if (d < bestDist) { bestDist = d; best = lv; }
    }
    return best?.reason || '';
  }, []);

  const supportZones = useMemo(() => {
    const raw = (data?.strongSupportZones?.length ? data.strongSupportZones : supports.map((lv) => ({
      lower: lv.zoneLow || lv.price,
      upper: lv.zoneHigh || lv.price,
      mid: lv.price,
      type: 'SUPPORT',
      strength: lv.strength,
      timeframes: lv.timeframes,
      touchCount: lv.touchCount,
      reason: lv.reason || '',
    })));
    return raw.map((z) => {
      const lower = Number(z.lower) || 0;
      const upper = Number(z.upper) || 0;
      const mid = Number(z.mid) || ((lower + upper) / 2);
      const distance = currentPrice
        ? Math.round(((mid - currentPrice) / currentPrice) * 10000) / 100
        : (Number(z.distance) || 0);
      const reason = z.reason || findReason(supports, mid);
      return { ...z, lower, upper, mid, distance, reason };
    });
  }, [data?.strongSupportZones, supports, currentPrice, findReason]);

  const resistanceZones = useMemo(() => {
    const raw = (data?.strongResistanceZones?.length ? data.strongResistanceZones : resistances.map((lv) => ({
      lower: lv.zoneLow || lv.price,
      upper: lv.zoneHigh || lv.price,
      mid: lv.price,
      type: 'RESISTANCE',
      strength: lv.strength,
      timeframes: lv.timeframes,
      touchCount: lv.touchCount,
      reason: lv.reason || '',
    })));
    return raw.map((z) => {
      const lower = Number(z.lower) || 0;
      const upper = Number(z.upper) || 0;
      const mid = Number(z.mid) || ((lower + upper) / 2);
      const distance = currentPrice
        ? Math.round(((mid - currentPrice) / currentPrice) * 10000) / 100
        : (Number(z.distance) || 0);
      const reason = z.reason || findReason(resistances, mid);
      return { ...z, lower, upper, mid, distance, reason };
    });
  }, [data?.strongResistanceZones, resistances, currentPrice, findReason]);

  const matchTf = useCallback((zone) => {
    if (selectedTf === 'all') return true;
    const tfs = Array.isArray(zone?.timeframes) ? zone.timeframes : [];
    return tfs.some((tf) => String(tf).toLowerCase() === selectedTf);
  }, [selectedTf]);

  const filteredSupportZones = useMemo(
    () => supportZones.filter((z) => matchTf(z)),
    [supportZones, matchTf],
  );
  const filteredResistanceZones = useMemo(
    () => resistanceZones.filter((z) => matchTf(z)),
    [resistanceZones, matchTf],
  );

  const closestSupportZone = filteredSupportZones.length > 0 ? filteredSupportZones[0] : null;
  const closestResistZone = filteredResistanceZones.length > 0 ? filteredResistanceZones[0] : null;
  const hasFilteredZones = filteredSupportZones.length > 0 || filteredResistanceZones.length > 0;

  const renderZone = (zone, isClosest, type) => {
    const isResist = type === 'RESISTANCE';
    const bgColor = isClosest
      ? (isResist ? colors.redBg : colors.greenBg)
      : colors.surface;
    const borderColor = isClosest
      ? (isResist ? colors.red : colors.green)
      : 'transparent';
    const rangeText = `${zone.lower.toFixed(2)} - ${zone.upper.toFixed(2)}`;
    const zoneKey = `${zone.lower}-${zone.upper}-${zone.mid}`;
    const isExpanded = !!expandedReasons[zoneKey];
    const reason = zone.reason || '';

    const strengthPct = Math.min((zone.strength || 1) / 4, 1) * 100;
    return (
      <TouchableOpacity
        key={zoneKey}
        style={[styles.levelRow, { backgroundColor: bgColor, borderLeftColor: borderColor, borderLeftWidth: isClosest ? 3 : 0 }]}
        onPress={() => reason && toggleReason(zoneKey)}
        activeOpacity={reason ? 0.7 : 1}
      >
        <View style={styles.levelMain}>
          <View style={styles.levelPriceRow}>
            <Text style={[styles.zoneRange, { color: isResist ? colors.redLight : colors.greenLight }]}>
              {rangeText}
            </Text>
            <View style={{ flexDirection: 'row', alignItems: 'center', gap: 4 }}>
              <Text style={styles.levelStars}>{STARS[zone.strength] || STARS[4]}</Text>
              {reason ? <Text style={styles.reasonToggle}>{isExpanded ? '▾' : '▸'}</Text> : null}
            </View>
          </View>
          <Text style={styles.zoneMid}>中位 {zone.mid.toFixed(2)}</Text>
          <View style={styles.strengthTrack}>
            <View style={[styles.strengthFill, {
              width: `${strengthPct}%`,
              backgroundColor: isResist ? colors.red : colors.green,
            }]} />
          </View>
          <View style={styles.levelMeta}>
            <Text style={styles.levelTF}>{zone.timeframes?.join(' / ')}</Text>
            <Text style={styles.levelTouch}>触及 {zone.touchCount}x</Text>
          </View>
          {isExpanded && reason ? (
            <View style={styles.reasonBox}>
              <Text style={styles.reasonTitle}>
                {isResist ? '为什么是阻力位？' : '为什么是支撑位？'}
              </Text>
              <Text style={styles.reasonDetail}>{reason}</Text>
            </View>
          ) : null}
        </View>
        <View style={styles.levelRight}>
          <Text style={[styles.levelDist, {
            color: isResist ? colors.redLight : colors.greenLight,
          }]}>
            {zone.distance >= 0 ? '+' : ''}{zone.distance.toFixed(2)}%
          </Text>
        </View>
      </TouchableOpacity>
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

      <View style={styles.tfRow}>
        {SR_TF_OPTIONS.map((opt) => (
          <TouchableOpacity
            key={opt.key}
            style={[styles.tfChip, selectedTf === opt.key && styles.tfChipActive]}
            onPress={() => setSelectedTf(opt.key)}
          >
            <Text style={[styles.tfChipText, selectedTf === opt.key && styles.tfChipTextActive]}>
              {opt.label}
            </Text>
          </TouchableOpacity>
        ))}
      </View>

      {/* 最近支撑/阻力概览 */}
      {(closestResistZone || closestSupportZone) && (
        <View style={styles.overviewRow}>
          {closestResistZone && (
            <View style={[styles.overviewItem, styles.overviewResist]}>
              <Text style={styles.overviewLabel}>最近阻力区间</Text>
              <Text style={[styles.overviewPrice, { color: colors.redLight }]}>
                {closestResistZone.lower.toFixed(2)} - {closestResistZone.upper.toFixed(2)}
              </Text>
              <Text style={[styles.overviewDist, { color: colors.redLight }]}>
                {closestResistZone.distance >= 0 ? '+' : ''}{closestResistZone.distance.toFixed(2)}%
              </Text>
            </View>
          )}
          {closestSupportZone && (
            <View style={[styles.overviewItem, styles.overviewSupport]}>
              <Text style={styles.overviewLabel}>最近支撑区间</Text>
              <Text style={[styles.overviewPrice, { color: colors.greenLight }]}>
                {closestSupportZone.lower.toFixed(2)} - {closestSupportZone.upper.toFixed(2)}
              </Text>
              <Text style={[styles.overviewDist, { color: colors.greenLight }]}>
                {closestSupportZone.distance >= 0 ? '+' : ''}{closestSupportZone.distance.toFixed(2)}%
              </Text>
            </View>
          )}
        </View>
      )}

      {/* 强阻力区间 */}
      {filteredResistanceZones.length > 0 && (
        <View style={styles.section}>
          <Text style={[styles.sectionTitle, { color: colors.red }]}>强阻力区间</Text>
          {filteredResistanceZones.slice(0, 4).map((z, idx) => renderZone(z, idx === 0, 'RESISTANCE'))}
        </View>
      )}

      {/* 强支撑区间 */}
      {filteredSupportZones.length > 0 && (
        <View style={styles.section}>
          <Text style={[styles.sectionTitle, { color: colors.green }]}>强支撑区间</Text>
          {filteredSupportZones.slice(0, 4).map((z, idx) => renderZone(z, idx === 0, 'SUPPORT'))}
        </View>
      )}

      {!data && !loading && (
        <Text style={styles.emptyText}>暂无数据，点击刷新</Text>
      )}
      {data && !loading && !hasFilteredZones && (
        <Text style={styles.emptyText}>该时间区间暂无支撑/阻力数据</Text>
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
  tfRow: {
    flexDirection: 'row',
    flexWrap: 'wrap',
    gap: spacing.xs,
    marginBottom: spacing.md,
  },
  tfChip: {
    paddingHorizontal: spacing.md,
    paddingVertical: spacing.xs,
    borderRadius: radius.pill,
    backgroundColor: colors.surface,
    borderWidth: 1,
    borderColor: colors.cardBorder,
  },
  tfChipActive: {
    backgroundColor: colors.goldBg,
    borderColor: colors.gold,
  },
  tfChipText: {
    fontSize: fontSize.xs,
    color: colors.textMuted,
    fontWeight: '600',
  },
  tfChipTextActive: {
    color: colors.goldLight,
    fontWeight: '700',
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
    borderColor: 'rgba(255,59,92,0.2)',
  },
  overviewSupport: {
    backgroundColor: colors.greenBg,
    borderWidth: 1,
    borderColor: 'rgba(0,230,118,0.2)',
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
  zoneRange: {
    fontSize: fontSize.md,
    fontWeight: '700',
    fontVariant: ['tabular-nums'],
  },
  zoneMid: {
    marginTop: 2,
    fontSize: fontSize.xs,
    color: colors.textMuted,
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

  reasonToggle: {
    fontSize: fontSize.xs,
    color: colors.textMuted,
  },
  reasonBox: {
    marginTop: spacing.sm,
    backgroundColor: 'rgba(255,184,77,0.08)',
    borderRadius: radius.sm,
    padding: spacing.sm,
    borderLeftWidth: 2,
    borderLeftColor: colors.gold,
  },
  reasonTitle: {
    fontSize: fontSize.xs,
    fontWeight: '700',
    color: colors.gold,
    marginBottom: spacing.xs,
  },
  reasonDetail: {
    fontSize: fontSize.xs,
    color: colors.textSecondary,
    lineHeight: 18,
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
