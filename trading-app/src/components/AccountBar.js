import React, { useState, useEffect, useCallback, useRef } from 'react';
import { View, Text, StyleSheet } from 'react-native';
import api, { WS_PRICE_BASE, AUTH_TOKEN } from '../services/api';
import { colors, spacing, radius, fontSize } from '../services/theme';
import SymbolPicker from './SymbolPicker';

/**
 * 交易 Tab 顶部账户摘要条
 * 左: SymbolPicker  中: 实时价格+涨跌  右: 余额+今日盈亏
 */
export default function AccountBar({ symbol, onChangeSymbol, markPrice }) {
  const [balance, setBalance] = useState(null);
  const [todayPnl, setTodayPnl] = useState(null);

  const fetchAccount = useCallback(async () => {
    try {
      const res = await api.getBalance();
      if (res?.data) {
        const balVal = parseFloat(res.data.crossWalletBalance || res.data.balance || '0');
        setBalance(balVal);
        // todayPnl 可从 positions unrealizedProfit 汇总
        const posRes = await api.getPositions();
        if (posRes?.data) {
          let pnl = 0;
          posRes.data.forEach((p) => {
            pnl += parseFloat(p.unRealizedProfit || '0');
          });
          setTodayPnl(pnl);
        }
      }
    } catch (_) {}
  }, []);

  useEffect(() => {
    fetchAccount();
    const iv = setInterval(fetchAccount, 10000);
    return () => clearInterval(iv);
  }, [fetchAccount]);

  const fmtPrice = (v) => {
    if (v == null) return '--';
    if (v >= 1000) return v.toFixed(2);
    if (v >= 1) return v.toFixed(4);
    return v.toFixed(6);
  };

  const fmtUsd = (v) => {
    if (v == null) return '--';
    return v >= 0 ? `+${v.toFixed(2)}` : v.toFixed(2);
  };

  return (
    <View style={styles.wrap}>
      {/* 第一行：币对 + 实时价格 */}
      <View style={styles.row}>
        <SymbolPicker symbol={symbol} onChangeSymbol={onChangeSymbol} />
        <View style={styles.priceBox}>
          <Text style={styles.price}>{fmtPrice(markPrice)}</Text>
        </View>
      </View>
      {/* 第二行：余额 + 持仓盈亏 */}
      <View style={styles.row2}>
        <View style={styles.statItem}>
          <Text style={styles.statLabel}>余额</Text>
          <Text style={styles.statValue}>
            {balance != null ? balance.toFixed(2) : '--'} U
          </Text>
        </View>
        <View style={styles.divider} />
        <View style={styles.statItem}>
          <Text style={styles.statLabel}>持仓盈亏</Text>
          <Text style={[styles.statValue, todayPnl != null && {
            color: todayPnl >= 0 ? colors.greenLight : colors.redLight,
          }]}>
            {fmtUsd(todayPnl)} U
          </Text>
        </View>
      </View>
    </View>
  );
}

const styles = StyleSheet.create({
  wrap: {
    backgroundColor: colors.card,
    borderRadius: radius.lg,
    borderWidth: 1,
    borderColor: colors.cardBorder,
    padding: spacing.md,
    gap: spacing.sm,
  },
  row: {
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
  },
  priceBox: {
    flexDirection: 'row',
    alignItems: 'baseline',
    gap: spacing.xs,
  },
  price: {
    fontSize: fontSize.xxl,
    fontWeight: '900',
    color: colors.white,
    fontVariant: ['tabular-nums'],
  },
  row2: {
    flexDirection: 'row',
    alignItems: 'center',
    backgroundColor: colors.surface,
    borderRadius: radius.md,
    borderWidth: 1,
    borderColor: colors.cardBorder,
    paddingVertical: spacing.sm,
    paddingHorizontal: spacing.md,
  },
  statItem: {
    flex: 1,
    alignItems: 'center',
  },
  divider: {
    width: 1,
    height: 24,
    backgroundColor: colors.divider,
  },
  statLabel: {
    fontSize: fontSize.xs,
    color: colors.textMuted,
    marginBottom: 2,
  },
  statValue: {
    fontSize: fontSize.lg,
    fontWeight: '700',
    color: colors.text,
    fontVariant: ['tabular-nums'],
  },
});
