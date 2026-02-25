import React, { useState, useEffect, useCallback, useRef } from 'react';
import {
  View, Text, StyleSheet, TouchableOpacity, TextInput, ScrollView, Alert, Switch,
} from 'react-native';
import { colors, spacing, radius, fontSize } from '../services/theme';
import api from '../services/api';

export default function FundingPanel({ symbol }) {
  const [status, setStatus] = useState(null);
  const [loading, setLoading] = useState(false);

  // 配置
  const [threshold, setThreshold] = useState('100');
  const [interval, setInterval_] = useState('300');
  const [topN, setTopN] = useState('10');
  const [autoTrade, setAutoTrade] = useState(false);
  const [amount, setAmount] = useState('10');
  const [leverage, setLeverage] = useState('5');

  // 展开/折叠
  const [showPositive, setShowPositive] = useState(true);
  const [showNegative, setShowNegative] = useState(true);

  const timerRef = useRef(null);

  const fetchStatus = useCallback(async () => {
    try {
      const res = await api.fundingStatus();
      if (res?.data) setStatus(res.data);
    } catch (_) {}
  }, []);

  useEffect(() => {
    fetchStatus();
    timerRef.current = setInterval(fetchStatus, 10000);
    return () => { if (timerRef.current) clearInterval(timerRef.current); };
  }, [fetchStatus]);

  const handleStart = async () => {
    setLoading(true);
    try {
      await api.startFundingMonitor({
        thresholdAnnualized: parseFloat(threshold) || 100,
        intervalSec: parseInt(interval) || 300,
        topN: parseInt(topN) || 10,
        autoTrade,
        amountPerOrder: amount,
        leverage: parseInt(leverage) || 5,
      });
      Alert.alert('成功', '资金费率监控已启动');
      fetchStatus();
    } catch (e) {
      Alert.alert('失败', e.message);
    }
    setLoading(false);
  };

  const handleStop = async () => {
    setLoading(true);
    try {
      await api.stopFundingMonitor();
      Alert.alert('成功', '资金费率监控已停止');
      fetchStatus();
    } catch (e) {
      Alert.alert('失败', e.message);
    }
    setLoading(false);
  };

  const isActive = status?.active;

  const renderRateItem = (item, idx) => {
    const isPos = item.fundingRate > 0;
    const annColor = Math.abs(item.annualizedPct) >= (parseFloat(threshold) || 100)
      ? colors.orange
      : isPos ? colors.redLight : colors.greenLight;

    return (
      <View key={item.symbol + idx} style={styles.rateItem}>
        <View style={styles.rateLeft}>
          <Text style={styles.rateSymbol}>{item.symbol.replace('USDT', '')}</Text>
          <View style={[styles.dirBadge, { backgroundColor: isPos ? colors.redBg : colors.greenBg }]}>
            <Text style={[styles.dirText, { color: isPos ? colors.redLight : colors.greenLight }]}>
              {isPos ? '多付空' : '空付多'}
            </Text>
          </View>
        </View>
        <View style={styles.rateRight}>
          <Text style={[styles.ratePct, { color: annColor }]}>
            {item.fundingRatePct >= 0 ? '+' : ''}{item.fundingRatePct.toFixed(4)}%
          </Text>
          <Text style={[styles.rateAnn, { color: annColor }]}>
            年化 {item.annualizedPct >= 0 ? '+' : ''}{item.annualizedPct.toFixed(0)}%
          </Text>
        </View>
      </View>
    );
  };

  return (
    <View style={styles.card}>
      <View style={styles.header}>
        <View style={styles.headerLeft}>
          <Text style={styles.title}>资金费率</Text>
          <View style={[styles.statusDot, { backgroundColor: isActive ? colors.green : colors.textMuted }]} />
        </View>
        <TouchableOpacity
          style={[styles.actionBtn, isActive ? styles.stopBtn : styles.startBtn]}
          onPress={isActive ? handleStop : handleStart}
          disabled={loading}
        >
          <Text style={[styles.actionBtnText, isActive ? styles.stopBtnText : styles.startBtnText]}>
            {loading ? '...' : isActive ? '停止' : '启动'}
          </Text>
        </TouchableOpacity>
      </View>

      {/* 配置区域（未启动时展示） */}
      {!isActive && (
        <View style={styles.configArea}>
          <View style={styles.configRow}>
            <View style={styles.configItem}>
              <Text style={styles.configLabel}>年化阈值%</Text>
              <TextInput style={styles.configInput} value={threshold} onChangeText={setThreshold} keyboardType="numeric" placeholderTextColor={colors.textMuted} />
            </View>
            <View style={styles.configItem}>
              <Text style={styles.configLabel}>刷新间隔(s)</Text>
              <TextInput style={styles.configInput} value={interval} onChangeText={setInterval_} keyboardType="numeric" placeholderTextColor={colors.textMuted} />
            </View>
            <View style={styles.configItem}>
              <Text style={styles.configLabel}>Top N</Text>
              <TextInput style={styles.configInput} value={topN} onChangeText={setTopN} keyboardType="numeric" placeholderTextColor={colors.textMuted} />
            </View>
          </View>
          <View style={styles.configRow}>
            <View style={[styles.configItem, { flexDirection: 'row', alignItems: 'center', gap: spacing.sm }]}>
              <Text style={styles.configLabel}>自动套利</Text>
              <Switch
                value={autoTrade}
                onValueChange={setAutoTrade}
                trackColor={{ false: colors.surface, true: colors.goldBg }}
                thumbColor={autoTrade ? colors.gold : colors.textMuted}
              />
            </View>
            {autoTrade && (
              <>
                <View style={styles.configItem}>
                  <Text style={styles.configLabel}>金额(U)</Text>
                  <TextInput style={styles.configInput} value={amount} onChangeText={setAmount} keyboardType="numeric" placeholderTextColor={colors.textMuted} />
                </View>
                <View style={styles.configItem}>
                  <Text style={styles.configLabel}>杠杆</Text>
                  <TextInput style={styles.configInput} value={leverage} onChangeText={setLeverage} keyboardType="numeric" placeholderTextColor={colors.textMuted} />
                </View>
              </>
            )}
          </View>
        </View>
      )}

      {/* 告警区 */}
      {status?.alertSymbols?.length > 0 && (
        <View style={styles.alertSection}>
          <Text style={styles.sectionLabel}>超阈值告警 ({status.alertSymbols.length})</Text>
          {status.alertSymbols.map((item, idx) => renderRateItem(item, idx))}
        </View>
      )}

      {/* 正费率排行 */}
      {status?.topPositive?.length > 0 && (
        <View style={styles.section}>
          <TouchableOpacity style={styles.sectionHeader} onPress={() => setShowPositive(!showPositive)}>
            <Text style={styles.sectionLabel}>正费率 Top（多付空）</Text>
            <Text style={styles.expandIcon}>{showPositive ? '▾' : '▸'}</Text>
          </TouchableOpacity>
          {showPositive && status.topPositive.map((item, idx) => renderRateItem(item, idx))}
        </View>
      )}

      {/* 负费率排行 */}
      {status?.topNegative?.length > 0 && (
        <View style={styles.section}>
          <TouchableOpacity style={styles.sectionHeader} onPress={() => setShowNegative(!showNegative)}>
            <Text style={styles.sectionLabel}>负费率 Top（空付多）</Text>
            <Text style={styles.expandIcon}>{showNegative ? '▾' : '▸'}</Text>
          </TouchableOpacity>
          {showNegative && status.topNegative.map((item, idx) => renderRateItem(item, idx))}
        </View>
      )}

      {/* 更新时间 */}
      {status?.updateTime && (
        <Text style={styles.updateTime}>更新: {status.updateTime}</Text>
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
  headerLeft: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: spacing.sm,
  },
  title: {
    fontSize: fontSize.lg,
    fontWeight: '700',
    color: colors.white,
  },
  statusDot: {
    width: 8,
    height: 8,
    borderRadius: 4,
  },
  actionBtn: {
    paddingHorizontal: spacing.lg,
    paddingVertical: spacing.sm,
    borderRadius: radius.md,
    borderWidth: 1,
  },
  startBtn: {
    backgroundColor: colors.goldBg,
    borderColor: colors.gold,
  },
  stopBtn: {
    backgroundColor: colors.redBg,
    borderColor: colors.red,
  },
  actionBtnText: {
    fontSize: fontSize.sm,
    fontWeight: '700',
  },
  startBtnText: {
    color: colors.gold,
  },
  stopBtnText: {
    color: colors.red,
  },
  configArea: {
    gap: spacing.sm,
    marginBottom: spacing.md,
  },
  configRow: {
    flexDirection: 'row',
    gap: spacing.sm,
  },
  configItem: {
    flex: 1,
  },
  configLabel: {
    fontSize: fontSize.xs,
    color: colors.textMuted,
    marginBottom: spacing.xs,
  },
  configInput: {
    backgroundColor: colors.surface,
    borderWidth: 1,
    borderColor: colors.cardBorder,
    borderRadius: radius.sm,
    color: colors.text,
    fontSize: fontSize.sm,
    paddingHorizontal: spacing.sm,
    paddingVertical: spacing.xs,
  },
  alertSection: {
    backgroundColor: colors.orangeBg,
    borderRadius: radius.md,
    padding: spacing.md,
    marginBottom: spacing.sm,
    borderWidth: 1,
    borderColor: 'rgba(212,138,44,0.3)',
  },
  section: {
    marginBottom: spacing.sm,
  },
  sectionHeader: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'center',
    paddingVertical: spacing.xs,
  },
  sectionLabel: {
    fontSize: fontSize.sm,
    fontWeight: '700',
    color: colors.textSecondary,
    marginBottom: spacing.xs,
  },
  expandIcon: {
    fontSize: fontSize.md,
    color: colors.textMuted,
  },
  rateItem: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'center',
    paddingVertical: spacing.xs,
    borderBottomWidth: StyleSheet.hairlineWidth,
    borderBottomColor: colors.divider,
  },
  rateLeft: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: spacing.sm,
  },
  rateSymbol: {
    fontSize: fontSize.sm,
    fontWeight: '700',
    color: colors.white,
    width: 70,
  },
  dirBadge: {
    paddingHorizontal: spacing.sm,
    paddingVertical: 2,
    borderRadius: radius.sm,
  },
  dirText: {
    fontSize: fontSize.xs,
    fontWeight: '600',
  },
  rateRight: {
    alignItems: 'flex-end',
  },
  ratePct: {
    fontSize: fontSize.sm,
    fontWeight: '700',
    fontVariant: ['tabular-nums'],
  },
  rateAnn: {
    fontSize: fontSize.xs,
    fontVariant: ['tabular-nums'],
  },
  updateTime: {
    fontSize: fontSize.xs,
    color: colors.textMuted,
    textAlign: 'right',
    marginTop: spacing.xs,
  },
});
