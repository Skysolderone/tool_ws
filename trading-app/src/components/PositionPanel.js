import React, { useState, useEffect, useCallback } from 'react';
import {
  View,
  Text,
  TextInput,
  TouchableOpacity,
  StyleSheet,
  Alert,
  Modal,
} from 'react-native';
import api from '../services/api';
import { colors } from '../services/theme';

export default function PositionPanel({ symbol }) {
  const [positions, setPositions] = useState([]);
  const [loading, setLoading] = useState(false);
  const [reduceModal, setReduceModal] = useState(null); // 当前正在减仓的 position
  const [reducePercent, setReducePercent] = useState('50');

  const fetchPositions = useCallback(async () => {
    try {
      const data = await api.getPositions();
      setPositions(data.data || []);
    } catch (e) {
      // 静默失败
    }
  }, []);

  useEffect(() => {
    fetchPositions();
    const timer = setInterval(fetchPositions, 5000);
    return () => clearInterval(timer);
  }, [fetchPositions]);

  // 平仓
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

  // 减仓
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
        <Text style={styles.title}>持仓 ({filtered.length})</Text>
        <TouchableOpacity onPress={fetchPositions}>
          <Text style={styles.refreshText}>刷新</Text>
        </TouchableOpacity>
      </View>

      {filtered.length === 0 ? (
        <Text style={styles.emptyText}>暂无持仓</Text>
      ) : (
        filtered.map((pos, idx) => {
          const amt = parseFloat(pos.positionAmt);
          const pnl = parseFloat(pos.unRealizedProfit);
          const isLong = amt > 0;
          const entry = parseFloat(pos.entryPrice);
          const mark = parseFloat(pos.markPrice);
          const liq = parseFloat(pos.liquidationPrice);

          return (
            <View key={`${pos.symbol}-${pos.positionSide}-${idx}`} style={styles.card}>
              {/* 头部 */}
              <View style={styles.cardHeader}>
                <View style={styles.symbolRow}>
                  <Text style={styles.symbolText}>{pos.symbol}</Text>
                  <View style={[styles.badge, isLong ? styles.badgeLong : styles.badgeShort]}>
                    <Text style={styles.badgeText}>
                      {isLong ? '多' : '空'} {pos.leverage}x
                    </Text>
                  </View>
                </View>
                <Text style={[styles.pnl, pnl >= 0 ? styles.pnlGreen : styles.pnlRed]}>
                  {pnl >= 0 ? '+' : ''}{pnl.toFixed(4)} U
                </Text>
              </View>

              {/* 详情 */}
              <View style={styles.details}>
                <View style={styles.detailItem}>
                  <Text style={styles.detailLabel}>数量</Text>
                  <Text style={styles.detailValue}>{Math.abs(amt)}</Text>
                </View>
                <View style={styles.detailItem}>
                  <Text style={styles.detailLabel}>开仓价</Text>
                  <Text style={styles.detailValue}>{entry.toFixed(2)}</Text>
                </View>
                <View style={styles.detailItem}>
                  <Text style={styles.detailLabel}>标记价</Text>
                  <Text style={styles.detailValue}>{mark.toFixed(2)}</Text>
                </View>
                {liq > 0 && (
                  <View style={styles.detailItem}>
                    <Text style={styles.detailLabel}>强平价</Text>
                    <Text style={[styles.detailValue, styles.liqPrice]}>{liq.toFixed(2)}</Text>
                  </View>
                )}
              </View>

              {/* 操作按钮：减仓 + 平仓 */}
              <View style={styles.btnRow}>
                <TouchableOpacity
                  style={styles.reduceBtn}
                  onPress={() => {
                    setReducePercent('50');
                    setReduceModal(pos);
                  }}
                  disabled={loading}
                >
                  <Text style={styles.reduceBtnText}>减仓</Text>
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
            <Text style={styles.modalTitle}>
              减仓 {reduceModal?.symbol}
            </Text>
            <Text style={styles.modalSubtitle}>
              当前持仓: {reduceModal ? Math.abs(parseFloat(reduceModal.positionAmt)) : 0} 个
            </Text>

            {/* 快捷百分比 */}
            <View style={styles.percentRow}>
              {['25', '50', '75', '100'].map((pct) => (
                <TouchableOpacity
                  key={pct}
                  style={[styles.percentChip, reducePercent === pct && styles.percentChipActive]}
                  onPress={() => setReducePercent(pct)}
                >
                  <Text
                    style={[
                      styles.percentChipText,
                      reducePercent === pct && styles.percentChipTextActive,
                    ]}
                  >
                    {pct}%
                  </Text>
                </TouchableOpacity>
              ))}
            </View>

            {/* 自定义百分比 */}
            <View style={styles.customPercentRow}>
              <Text style={styles.inputLabel}>减仓比例 (%)</Text>
              <TextInput
                style={styles.input}
                value={reducePercent}
                onChangeText={setReducePercent}
                keyboardType="decimal-pad"
                placeholder="1-100"
                placeholderTextColor={colors.textMuted}
              />
            </View>

            {/* 操作 */}
            <View style={styles.modalBtnRow}>
              <TouchableOpacity
                style={styles.modalCancelBtn}
                onPress={() => setReduceModal(null)}
              >
                <Text style={styles.modalCancelText}>取消</Text>
              </TouchableOpacity>
              <TouchableOpacity
                style={styles.modalConfirmBtn}
                onPress={handleReduce}
                disabled={loading}
              >
                <Text style={styles.modalConfirmText}>
                  确认减仓 {reducePercent}%
                </Text>
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
    borderRadius: 12,
    padding: 16,
    marginBottom: 12,
    borderWidth: 1,
    borderColor: colors.cardBorder,
  },
  header: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'center',
    marginBottom: 12,
  },
  title: { fontSize: 18, fontWeight: 'bold', color: colors.white },
  refreshText: { color: colors.blue, fontSize: 14 },
  emptyText: { color: colors.textMuted, textAlign: 'center', paddingVertical: 20 },

  card: {
    backgroundColor: colors.bg,
    borderRadius: 10,
    padding: 12,
    marginBottom: 10,
    borderWidth: 1,
    borderColor: colors.cardBorder,
  },
  cardHeader: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'center',
    marginBottom: 10,
  },
  symbolRow: { flexDirection: 'row', alignItems: 'center', gap: 8 },
  symbolText: { color: colors.white, fontSize: 16, fontWeight: 'bold' },
  badge: { paddingHorizontal: 8, paddingVertical: 2, borderRadius: 4 },
  badgeLong: { backgroundColor: colors.greenBg },
  badgeShort: { backgroundColor: colors.redBg },
  badgeText: { color: colors.white, fontSize: 11, fontWeight: '600' },
  pnl: { fontSize: 16, fontWeight: 'bold' },
  pnlGreen: { color: colors.greenLight },
  pnlRed: { color: colors.redLight },

  details: { flexDirection: 'row', marginBottom: 10, gap: 16, flexWrap: 'wrap' },
  detailItem: {},
  detailLabel: { color: colors.textMuted, fontSize: 11 },
  detailValue: { color: colors.textSecondary, fontSize: 13, marginTop: 2 },
  liqPrice: { color: colors.yellow },

  // 按钮行
  btnRow: {
    flexDirection: 'row',
    gap: 8,
  },
  reduceBtn: {
    flex: 1,
    backgroundColor: colors.surface,
    paddingVertical: 8,
    borderRadius: 8,
    alignItems: 'center',
    borderWidth: 1,
    borderColor: colors.yellow,
  },
  reduceBtnText: { color: colors.yellow, fontWeight: 'bold', fontSize: 14 },
  closeBtn: {
    flex: 1,
    backgroundColor: colors.red,
    paddingVertical: 8,
    borderRadius: 8,
    alignItems: 'center',
  },
  closeBtnText: { color: colors.white, fontWeight: 'bold', fontSize: 14 },

  // 减仓弹窗
  overlay: {
    flex: 1,
    backgroundColor: 'rgba(0,0,0,0.7)',
    justifyContent: 'center',
    alignItems: 'center',
    padding: 20,
  },
  modal: {
    backgroundColor: colors.card,
    borderRadius: 16,
    padding: 20,
    width: '100%',
    maxWidth: 400,
  },
  modalTitle: {
    fontSize: 18,
    fontWeight: 'bold',
    color: colors.white,
    textAlign: 'center',
    marginBottom: 4,
  },
  modalSubtitle: {
    fontSize: 13,
    color: colors.textSecondary,
    textAlign: 'center',
    marginBottom: 16,
  },

  percentRow: {
    flexDirection: 'row',
    gap: 8,
    marginBottom: 12,
  },
  percentChip: {
    flex: 1,
    paddingVertical: 8,
    borderRadius: 8,
    alignItems: 'center',
    backgroundColor: colors.surface,
    borderWidth: 1,
    borderColor: colors.cardBorder,
  },
  percentChipActive: {
    backgroundColor: colors.blue,
    borderColor: colors.blue,
  },
  percentChipText: { color: colors.textSecondary, fontWeight: '600', fontSize: 14 },
  percentChipTextActive: { color: colors.white },

  customPercentRow: { marginBottom: 16 },
  inputLabel: { color: colors.textSecondary, fontSize: 12, marginBottom: 4 },
  input: {
    backgroundColor: colors.bg,
    borderRadius: 8,
    padding: 10,
    color: colors.white,
    borderWidth: 1,
    borderColor: colors.cardBorder,
    fontSize: 15,
  },

  modalBtnRow: { flexDirection: 'row', gap: 12 },
  modalCancelBtn: {
    flex: 1,
    paddingVertical: 12,
    borderRadius: 10,
    alignItems: 'center',
    backgroundColor: colors.surface,
    borderWidth: 1,
    borderColor: colors.cardBorder,
  },
  modalCancelText: { color: colors.textSecondary, fontWeight: '600', fontSize: 15 },
  modalConfirmBtn: {
    flex: 1,
    paddingVertical: 12,
    borderRadius: 10,
    alignItems: 'center',
    backgroundColor: colors.yellow,
  },
  modalConfirmText: { color: colors.bg, fontWeight: '600', fontSize: 15 },
});
