import React, { useState, useEffect, useCallback } from 'react';
import {
  View, Text, TextInput, TouchableOpacity,
  StyleSheet, Alert, Modal,
} from 'react-native';
import api from '../services/api';
import { colors, spacing, radius, fontSize } from '../services/theme';

export default function PositionPanel({ symbol, positions: externalPositions, onRefreshPositions }) {
  const [localPositions, setLocalPositions] = useState([]);
  const [loading, setLoading] = useState(false);
  const [reduceModal, setReduceModal] = useState(null);
  const [reducePercent, setReducePercent] = useState('50');
  const positions = Array.isArray(externalPositions) ? externalPositions : localPositions;

  const fetchPositions = useCallback(async () => {
    if (onRefreshPositions) {
      await onRefreshPositions();
      return;
    }
    try {
      const data = await api.getPositions();
      setLocalPositions(data.data || []);
    } catch (_) {}
  }, [onRefreshPositions]);

  useEffect(() => {
    if (onRefreshPositions) return undefined;
    fetchPositions();
    const timer = setInterval(fetchPositions, 5000);
    return () => clearInterval(timer);
  }, [fetchPositions, onRefreshPositions]);

  const handleReverse = (pos) => {
    const amt = parseFloat(pos.positionAmt);
    const direction = amt > 0 ? '多' : '空';
    const newDirection = amt > 0 ? '空' : '多';
    Alert.alert(
      '一键反手',
      `${pos.symbol} ${direction} → ${newDirection}\n将平仓并反向开仓（等值保证金）`,
      [
        { text: '取消', style: 'cancel' },
        {
          text: '确认反手',
          style: 'destructive',
          onPress: async () => {
            setLoading(true);
            try {
              const resp = await api.reversePosition({
                symbol: pos.symbol,
                positionSide: pos.positionSide || '',
              });
              const openId = resp.data?.openOrder?.orderId || 'N/A';
              Alert.alert('反手成功', `已反向开仓，订单ID: ${openId}`);
              fetchPositions();
            } catch (e) {
              Alert.alert('反手失败', e.message);
            } finally {
              setLoading(false);
            }
          },
        },
      ],
    );
  };

  const handleClose = (pos) => {
    const amt = parseFloat(pos.positionAmt);
    const direction = amt > 0 ? '多' : '空';
    Alert.alert(
      '确认平仓',
      `${pos.symbol} ${direction} ${Math.abs(amt)} 个\n将全部市价平仓`,
      [
        { text: '取消', style: 'cancel' },
        {
          text: '确认平仓',
          style: 'destructive',
          onPress: async () => {
            setLoading(true);
            try {
              await api.closePosition({
                symbol: pos.symbol,
                positionSide: pos.positionSide || '',
              });
              Alert.alert('成功', '平仓成功');
              fetchPositions();
            } catch (e) {
              Alert.alert('平仓失败', e.message);
            } finally {
              setLoading(false);
            }
          },
        },
      ],
    );
  };

  const handleReduce = async () => {
    if (!reduceModal) return;
    const pct = parseFloat(reducePercent);
    if (!pct || pct <= 0 || pct > 100) {
      return Alert.alert('提示', '请输入 1-100 的百分比');
    }
    setLoading(true);
    try {
      await api.reducePosition({
        symbol: reduceModal.symbol,
        positionSide: reduceModal.positionSide || '',
        percent: pct,
      });
      Alert.alert('成功', `减仓 ${pct}% 成功`);
      setReduceModal(null);
      fetchPositions();
    } catch (e) {
      Alert.alert('减仓失败', e.message);
    } finally {
      setLoading(false);
    }
  };

  const filtered = symbol
    ? positions.filter((p) => p.symbol === symbol)
    : positions;

  return (
    <View style={styles.panel}>
      <View style={styles.header}>
        <Text style={styles.title}>当前持仓</Text>
        <TouchableOpacity onPress={fetchPositions} style={styles.refreshBtn} activeOpacity={0.7}>
          <Text style={styles.refreshText}>🔄 刷新</Text>
        </TouchableOpacity>
      </View>

      {filtered.length === 0 ? (
        <View style={styles.emptyWrap}>
          <Text style={styles.emptyIcon}>📊</Text>
          <Text style={styles.emptyText}>暂无持仓</Text>
        </View>
      ) : (
        filtered.map((pos, idx) => {
          const amt = parseFloat(pos.positionAmt);
          const pnl = parseFloat(pos.unRealizedProfit);
          const isLong = amt > 0;
          const entry = parseFloat(pos.entryPrice);
          const mark = parseFloat(pos.markPrice);
          const liq = parseFloat(pos.liquidationPrice);
          const pnlPct = entry > 0 ? ((mark - entry) / entry * 100 * (isLong ? 1 : -1)).toFixed(2) : '0.00';

          return (
            <View key={`${pos.symbol}-${pos.positionSide}-${idx}`} style={[styles.card, { borderLeftWidth: 3, borderLeftColor: isLong ? colors.green : colors.red }]}>
              {/* 顶部：币对 + 方向 | 盈亏 */}
              <View style={styles.cardTop}>
                <View style={styles.cardTopLeft}>
                  <Text style={styles.posSymbol}>{pos.symbol}</Text>
                  <View style={[styles.sideBadge, { backgroundColor: isLong ? colors.greenBg : colors.redBg }]}>
                    <Text style={[styles.sideBadgeText, { color: isLong ? colors.greenLight : colors.redLight }]}>
                      {isLong ? 'LONG' : 'SHORT'}
                    </Text>
                  </View>
                  <Text style={styles.levText}>{pos.leverage}×</Text>
                </View>
                <View style={styles.cardTopRight}>
                  <Text style={[styles.pnlValue, { color: pnl >= 0 ? colors.greenLight : colors.redLight }]}>
                    {pnl >= 0 ? '+' : ''}{pnl.toFixed(2)} U
                  </Text>
                  <Text style={[styles.pnlPct, { color: pnl >= 0 ? colors.greenLight : colors.redLight }]}>
                    {pnl >= 0 ? '+' : ''}{pnlPct}%
                  </Text>
                </View>
              </View>

              {/* 中间：数据网格 */}
              <View style={styles.dataGrid}>
                <View style={styles.dataCell}>
                  <Text style={styles.dataLabel}>数量</Text>
                  <Text style={styles.dataValue}>{Math.abs(amt)}</Text>
                </View>
                <View style={styles.dataCell}>
                  <Text style={styles.dataLabel}>开仓价</Text>
                  <Text style={styles.dataValue}>{entry.toFixed(2)}</Text>
                </View>
                <View style={styles.dataCell}>
                  <Text style={styles.dataLabel}>标记价</Text>
                  <Text style={styles.dataValue}>{mark.toFixed(2)}</Text>
                </View>
                {liq > 0 && (
                  <View style={styles.dataCell}>
                    <Text style={styles.dataLabel}>强平价</Text>
                    <Text style={[styles.dataValue, { color: colors.orange }]}>{liq.toFixed(2)}</Text>
                  </View>
                )}
              </View>

              {/* 底部操作 */}
              <View style={styles.actionRow}>
                <TouchableOpacity
                  style={styles.reduceBtn}
                  onPress={() => { setReducePercent('50'); setReduceModal(pos); }}
                  disabled={loading}
                >
                  <Text style={styles.reduceBtnText}>减仓</Text>
                </TouchableOpacity>
                <TouchableOpacity
                  style={styles.reverseBtn}
                  onPress={() => handleReverse(pos)}
                  disabled={loading}
                >
                  <Text style={styles.reverseBtnText}>反手</Text>
                </TouchableOpacity>
                <TouchableOpacity
                  style={styles.closeBtn}
                  onPress={() => handleClose(pos)}
                  disabled={loading}
                >
                  <Text style={styles.closeBtnText}>平仓</Text>
                </TouchableOpacity>
              </View>
            </View>
          );
        })
      )}

      {/* 减仓弹窗 */}
      <Modal visible={!!reduceModal} animationType="fade" transparent>
        <View style={styles.overlay}>
          <View style={styles.modal}>
            <Text style={styles.modalTitle}>减仓 {reduceModal?.symbol}</Text>
            <Text style={styles.modalSub}>
              当前: {reduceModal ? Math.abs(parseFloat(reduceModal.positionAmt)) : 0} 个
            </Text>

            <View style={styles.pctRow}>
              {['25', '50', '75', '100'].map((pct) => (
                <TouchableOpacity
                  key={pct}
                  style={[styles.pctChip, reducePercent === pct && styles.pctChipActive]}
                  onPress={() => setReducePercent(pct)}
                >
                  <Text style={[styles.pctChipText, reducePercent === pct && styles.pctChipTextActive]}>
                    {pct}%
                  </Text>
                </TouchableOpacity>
              ))}
            </View>

            <View style={styles.customInput}>
              <Text style={styles.customLabel}>自定义比例 (%)</Text>
              <TextInput
                style={styles.input}
                value={reducePercent}
                onChangeText={setReducePercent}
                keyboardType="decimal-pad"
                placeholder="1-100"
                placeholderTextColor={colors.textMuted}
              />
            </View>

            <View style={styles.modalActions}>
              <TouchableOpacity style={styles.cancelBtn} onPress={() => setReduceModal(null)}>
                <Text style={styles.cancelText}>取消</Text>
              </TouchableOpacity>
              <TouchableOpacity style={styles.confirmBtn} onPress={handleReduce} disabled={loading}>
                <Text style={styles.confirmText}>确认减仓 {reducePercent}%</Text>
              </TouchableOpacity>
            </View>
          </View>
        </View>
      </Modal>
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
    justifyContent: 'space-between',
    alignItems: 'center',
    marginBottom: spacing.md,
  },
  title: {
    fontSize: fontSize.md,
    fontWeight: '700',
    color: colors.white,
  },
  refreshBtn: {
    paddingHorizontal: spacing.md,
    paddingVertical: spacing.xs,
    borderRadius: radius.pill,
    backgroundColor: colors.goldBg,
    borderWidth: 1,
    borderColor: 'rgba(212,165,74,0.3)',
  },
  refreshText: {
    color: colors.goldLight,
    fontSize: fontSize.sm,
    fontWeight: '600',
  },

  // 空状态
  emptyWrap: {
    alignItems: 'center',
    paddingVertical: 40,
  },
  emptyIcon: {
    fontSize: 36,
    marginBottom: spacing.sm,
  },
  emptyText: {
    color: colors.textMuted,
    fontSize: fontSize.sm,
  },

  // 持仓卡片
  card: {
    backgroundColor: colors.surface,
    borderRadius: radius.md,
    padding: spacing.md,
    marginBottom: spacing.sm,
  },
  cardTop: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'flex-start',
    marginBottom: spacing.md,
  },
  cardTopLeft: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: spacing.sm,
  },
  posSymbol: {
    color: colors.white,
    fontSize: fontSize.lg,
    fontWeight: '700',
  },
  sideBadge: {
    paddingHorizontal: spacing.sm,
    paddingVertical: 2,
    borderRadius: radius.sm,
  },
  sideBadgeText: {
    fontSize: fontSize.xs,
    fontWeight: '700',
  },
  levText: {
    fontSize: fontSize.xs,
    color: colors.textMuted,
    fontWeight: '600',
  },
  cardTopRight: {
    alignItems: 'flex-end',
  },
  pnlValue: {
    fontSize: fontSize.xl,
    fontWeight: '800',
    fontVariant: ['tabular-nums'],
  },
  pnlPct: {
    fontSize: fontSize.xs,
    fontWeight: '600',
    marginTop: 2,
  },

  // 数据网格
  dataGrid: {
    flexDirection: 'row',
    flexWrap: 'wrap',
    marginBottom: spacing.md,
    gap: spacing.xs,
    borderTopWidth: 1,
    borderTopColor: colors.cardBorder,
    paddingTop: spacing.md,
  },
  dataCell: {
    width: '48%',
    paddingVertical: spacing.xs,
  },
  dataLabel: {
    color: colors.textMuted,
    fontSize: fontSize.xs,
    marginBottom: 2,
  },
  dataValue: {
    color: colors.text,
    fontSize: fontSize.sm,
    fontWeight: '600',
    fontVariant: ['tabular-nums'],
  },

  // 操作按钮
  actionRow: {
    flexDirection: 'row',
    gap: spacing.sm,
  },
  reduceBtn: {
    flex: 1,
    paddingVertical: spacing.md,
    borderRadius: radius.sm,
    alignItems: 'center',
    backgroundColor: 'transparent',
    borderWidth: 1.5,
    borderColor: colors.yellow,
  },
  reduceBtnText: {
    color: colors.yellow,
    fontWeight: '700',
    fontSize: fontSize.sm,
  },
  reverseBtn: {
    flex: 1,
    paddingVertical: spacing.md,
    borderRadius: radius.sm,
    alignItems: 'center',
    backgroundColor: 'transparent',
    borderWidth: 1.5,
    borderColor: colors.gold,
  },
  reverseBtnText: {
    color: colors.gold,
    fontWeight: '700',
    fontSize: fontSize.sm,
  },
  closeBtn: {
    flex: 1,
    paddingVertical: spacing.md,
    borderRadius: radius.sm,
    alignItems: 'center',
    backgroundColor: colors.red,
    shadowColor: colors.redGlow,
    shadowRadius: 6,
    shadowOpacity: 0.5,
    elevation: 3,
  },
  closeBtnText: {
    color: colors.white,
    fontWeight: '700',
    fontSize: fontSize.sm,
  },

  // 减仓弹窗
  overlay: {
    flex: 1,
    backgroundColor: 'rgba(0,0,0,0.75)',
    justifyContent: 'center',
    alignItems: 'center',
    padding: spacing.xl,
  },
  modal: {
    backgroundColor: colors.card,
    borderRadius: radius.xxl,
    borderWidth: 1,
    borderColor: colors.cardBorder,
    padding: spacing.xl,
    width: '100%',
    maxWidth: 400,
    shadowColor: colors.shadow,
    shadowRadius: 12,
    shadowOpacity: 0.8,
    elevation: 6,
  },
  modalTitle: {
    fontSize: fontSize.lg,
    fontWeight: '700',
    color: colors.white,
    textAlign: 'center',
    marginBottom: spacing.xs,
  },
  modalSub: {
    fontSize: fontSize.sm,
    color: colors.textSecondary,
    textAlign: 'center',
    marginBottom: spacing.lg,
  },
  pctRow: {
    flexDirection: 'row',
    gap: spacing.sm,
    marginBottom: spacing.md,
  },
  pctChip: {
    flex: 1,
    paddingVertical: spacing.sm,
    borderRadius: radius.md,
    alignItems: 'center',
    backgroundColor: colors.surface,
  },
  pctChipActive: {
    backgroundColor: colors.goldBg,
  },
  pctChipText: {
    color: colors.textMuted,
    fontWeight: '600',
    fontSize: fontSize.sm,
  },
  pctChipTextActive: {
    color: colors.gold,
    fontWeight: '700',
  },
  customInput: {
    marginBottom: spacing.lg,
  },
  customLabel: {
    color: colors.textMuted,
    fontSize: fontSize.xs,
    marginBottom: spacing.xs,
  },
  input: {
    backgroundColor: colors.surface,
    borderRadius: radius.md,
    padding: spacing.md,
    color: colors.white,
    fontSize: fontSize.md,
  },
  modalActions: {
    flexDirection: 'row',
    gap: spacing.md,
  },
  cancelBtn: {
    flex: 1,
    paddingVertical: spacing.md,
    borderRadius: radius.md,
    alignItems: 'center',
    backgroundColor: colors.surface,
  },
  cancelText: {
    color: colors.textSecondary,
    fontWeight: '600',
    fontSize: fontSize.md,
  },
  confirmBtn: {
    flex: 1,
    paddingVertical: spacing.md,
    borderRadius: radius.md,
    alignItems: 'center',
    backgroundColor: colors.yellow,
  },
  confirmText: {
    color: colors.bg,
    fontWeight: '700',
    fontSize: fontSize.md,
  },
});
