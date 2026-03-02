import React, { useState, useEffect, useCallback } from 'react';
import { View, Text, ScrollView, StyleSheet, ActivityIndicator } from 'react-native';
import { colors, spacing, radius, fontSize } from '../services/theme';
import api from '../services/api';

export default function PortfolioHealthPanel() {
  const [varData, setVarData] = useState(null);
  const [opsData, setOpsData] = useState(null);
  const [regimeData, setRegimeData] = useState(null);
  const [allocData, setAllocData] = useState(null);
  const [ksData, setKsData] = useState(null);
  const [loading, setLoading] = useState(true);

  const fetchAll = useCallback(async () => {
    try {
      const [varRes, opsRes, regimeRes, allocRes, ksRes] = await Promise.allSettled([
        api.getVarStatus(),
        api.getOpsMetrics(),
        api.getRegimeStatus(),
        api.getAllocationStatus(),
        api.getKillSwitchStatus(),
      ]);
      if (varRes.status === 'fulfilled') setVarData(varRes.value?.data);
      if (opsRes.status === 'fulfilled') setOpsData(opsRes.value?.data);
      if (regimeRes.status === 'fulfilled') setRegimeData(regimeRes.value?.data);
      if (allocRes.status === 'fulfilled') setAllocData(allocRes.value?.data);
      if (ksRes.status === 'fulfilled') setKsData(ksRes.value?.data);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    fetchAll();
    const timer = setInterval(fetchAll, 15000);
    return () => clearInterval(timer);
  }, [fetchAll]);

  if (loading) {
    return (
      <View style={s.center}>
        <ActivityIndicator color={colors.gold} />
      </View>
    );
  }

  const budgetPct = varData?.riskBudgetUsedPct || 0;
  const budgetColor = budgetPct > 80 ? colors.red : budgetPct > 50 ? colors.gold : colors.green;

  return (
    <ScrollView style={s.container} contentContainerStyle={s.content}>
      {/* VaR 风控 */}
      <View style={s.section}>
        <Text style={s.sectionTitle}>风险预算</Text>
        <View style={s.row}>
          <MetricCard label="VaR 95%" value={`$${(varData?.totalVar95 || 0).toFixed(0)}`} />
          <MetricCard label="CVaR 95%" value={`$${(varData?.totalCVar95 || 0).toFixed(0)}`} />
          <MetricCard label="预算使用" value={`${budgetPct.toFixed(1)}%`} color={budgetColor} />
        </View>
        {varData?.breached && (
          <View style={s.alertBanner}>
            <Text style={s.alertText}>⚠️ VaR 超出风险预算</Text>
          </View>
        )}
      </View>

      {/* Regime */}
      <View style={s.section}>
        <Text style={s.sectionTitle}>市场状态</Text>
        <View style={s.row}>
          <RegimeTag regime={regimeData?.regime} confidence={regimeData?.confidence} />
          {regimeData?.indicators && (
            <>
              <MetricCard label="ADX" value={(regimeData.indicators.adx14 || 0).toFixed(1)} small />
              <MetricCard label="ATR%" value={((regimeData.indicators.atrPct || 0) * 100).toFixed(2) + '%'} small />
              <MetricCard label="BB宽" value={(regimeData.indicators.bollingerWidth || 0).toFixed(4)} small />
            </>
          )}
        </View>
      </View>

      {/* Kill-Switch */}
      {ksData?.accountLocked && (
        <View style={s.alertBanner}>
          <Text style={s.alertText}>🚨 账户级 Kill-Switch 已激活</Text>
        </View>
      )}

      {/* 运营指标 */}
      <View style={s.section}>
        <Text style={s.sectionTitle}>运营指标</Text>
        <View style={s.row}>
          <MetricCard label="下单成功率" value={`${(opsData?.successRate || 0).toFixed(1)}%`} />
          <MetricCard label="平均延迟" value={`${(opsData?.avgLatencyMs || 0).toFixed(0)}ms`} />
          <MetricCard label="P95延迟" value={`${(opsData?.p95LatencyMs || 0).toFixed(0)}ms`} />
          <MetricCard label="风控触发" value={`${opsData?.riskTriggerCount || 0}`} />
        </View>
      </View>

      {/* 策略分配 */}
      {allocData?.allocations?.length > 0 && (
        <View style={s.section}>
          <Text style={s.sectionTitle}>策略资金分配</Text>
          {allocData.allocations.map((a, i) => (
            <View key={i} style={s.allocRow}>
              <Text style={s.allocName}>{a.strategyType}</Text>
              <View style={s.allocBarOuter}>
                <View style={[s.allocBarInner, { width: `${(a.weight * 100).toFixed(0)}%` }]} />
              </View>
              <Text style={s.allocPct}>{(a.weight * 100).toFixed(0)}%</Text>
              <Text style={s.allocUSDT}>${a.allocatedUSDT?.toFixed(0)}</Text>
            </View>
          ))}
        </View>
      )}
    </ScrollView>
  );
}

function MetricCard({ label, value, color, small }) {
  return (
    <View style={[s.metricCard, small && s.metricCardSmall]}>
      <Text style={s.metricLabel}>{label}</Text>
      <Text style={[s.metricValue, color && { color }]}>{value}</Text>
    </View>
  );
}

function RegimeTag({ regime, confidence }) {
  const regimeColors = { trend: colors.gold, range: colors.blue || colors.textSecondary, high_volatility: colors.red };
  const regimeLabels = { trend: '趋势', range: '震荡', high_volatility: '高波动' };
  const c = regimeColors[regime] || colors.textSecondary;
  return (
    <View style={[s.regimeTag, { borderColor: c }]}>
      <Text style={[s.regimeText, { color: c }]}>
        {regimeLabels[regime] || regime || '未知'} ({((confidence || 0) * 100).toFixed(0)}%)
      </Text>
    </View>
  );
}

const s = StyleSheet.create({
  container: { flex: 1, backgroundColor: colors.bg },
  content: { padding: spacing.md },
  center: { flex: 1, justifyContent: 'center', alignItems: 'center' },
  section: { marginBottom: spacing.lg },
  sectionTitle: { color: colors.gold, fontSize: fontSize.md, fontWeight: '700', marginBottom: spacing.sm },
  row: { flexDirection: 'row', flexWrap: 'wrap', gap: spacing.sm },
  metricCard: {
    backgroundColor: colors.card, borderRadius: radius.md, padding: spacing.sm,
    minWidth: 80, flex: 1, alignItems: 'center',
  },
  metricCardSmall: { minWidth: 60 },
  metricLabel: { color: colors.textSecondary, fontSize: fontSize.xs, marginBottom: 2 },
  metricValue: { color: colors.text, fontSize: fontSize.md, fontWeight: '700' },
  alertBanner: {
    backgroundColor: 'rgba(217,68,82,0.15)', borderRadius: radius.sm,
    padding: spacing.sm, marginTop: spacing.xs,
  },
  alertText: { color: colors.red, fontSize: fontSize.sm, fontWeight: '600', textAlign: 'center' },
  regimeTag: {
    borderWidth: 1, borderRadius: radius.sm, paddingHorizontal: spacing.sm,
    paddingVertical: spacing.xs, alignSelf: 'flex-start',
  },
  regimeText: { fontSize: fontSize.sm, fontWeight: '600' },
  allocRow: {
    flexDirection: 'row', alignItems: 'center', marginBottom: spacing.xs,
    backgroundColor: colors.card, borderRadius: radius.sm, padding: spacing.xs,
  },
  allocName: { color: colors.text, fontSize: fontSize.sm, width: 70 },
  allocBarOuter: { flex: 1, height: 8, backgroundColor: colors.surface, borderRadius: 4, marginHorizontal: spacing.xs },
  allocBarInner: { height: 8, backgroundColor: colors.gold, borderRadius: 4 },
  allocPct: { color: colors.gold, fontSize: fontSize.xs, width: 35, textAlign: 'right' },
  allocUSDT: { color: colors.textSecondary, fontSize: fontSize.xs, width: 50, textAlign: 'right' },
});
