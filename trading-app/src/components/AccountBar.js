import React, { useMemo } from 'react';
import { View, Text, StyleSheet } from 'react-native';
import { colors, spacing, radius, fontSize } from '../services/theme';
import SymbolPicker from './SymbolPicker';

/**
 * 交易 Tab 顶部账户摘要条
 * 左: SymbolPicker  中: 实时价格+涨跌  右: 余额+今日盈亏
 */
export default function AccountBar({ symbol, onChangeSymbol, markPrice, balance, positions = [] }) {
  const { displayPnl, pnlLabel } = useMemo(() => {
    let totalPnl = 0;
    let symbolPnl = 0;
    let hasSymbolPosition = false;

    positions.forEach((p) => {
      const pnl = parseFloat(p.unRealizedProfit || '0');
      totalPnl += pnl;
      if (p.symbol === symbol) {
        symbolPnl += pnl;
        hasSymbolPosition = true;
      }
    });

    return {
      displayPnl: hasSymbolPosition ? symbolPnl : totalPnl,
      pnlLabel: hasSymbolPosition ? '当前币对盈亏' : '总持仓盈亏',
    };
  }, [positions, symbol]);

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
          <Text style={styles.priceLabel}>标记价格</Text>
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
          <Text style={styles.statLabel}>{pnlLabel}</Text>
          <Text style={[styles.statValue, displayPnl != null && {
            color: displayPnl >= 0 ? colors.greenLight : colors.redLight,
          }]}>
            {fmtUsd(displayPnl)} U
          </Text>
        </View>
      </View>
    </View>
  );
}

const styles = StyleSheet.create({
  wrap: {
    backgroundColor: colors.card,
    borderRadius: radius.md,
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
    alignItems: 'flex-end',
    gap: 2,
  },
  priceLabel: {
    fontSize: fontSize.xs,
    color: colors.textMuted,
    fontWeight: '500',
  },
  price: {
    fontSize: fontSize.xl,
    fontWeight: '700',
    color: colors.text,
    fontVariant: ['tabular-nums'],
  },
  row2: {
    flexDirection: 'row',
    alignItems: 'center',
    backgroundColor: colors.surface,
    borderRadius: radius.sm,
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
    marginBottom: 3,
  },
  statValue: {
    fontSize: fontSize.md,
    fontWeight: '700',
    color: colors.text,
    fontVariant: ['tabular-nums'],
  },
});
