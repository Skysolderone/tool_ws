import React, { useState, useEffect, useCallback } from 'react';
import {
  View, Text, TextInput, TouchableOpacity, StyleSheet, Alert, Modal,
} from 'react-native';
import api from '../services/api';
import { colors, spacing, radius, fontSize } from '../services/theme';

export default function GridPanel({ symbol }) {
  const [showModal, setShowModal] = useState(false);
  const [status, setStatus] = useState(null);
  const [config, setConfig] = useState({
    leverage: '10',
    upperPrice: '',
    lowerPrice: '',
    gridCount: '10',
    amountPerGrid: '5',
    stopLossPrice: '',
    takeProfitPrice: '',
  });

  const fetchStatus = useCallback(async () => {
    if (!symbol) return;
    try {
      const data = await api.gridStatus(symbol);
      setStatus(data.data);
    } catch { setStatus(null); }
  }, [symbol]);

  useEffect(() => {
    fetchStatus();
    const iv = setInterval(fetchStatus, 5000);
    return () => clearInterval(iv);
  }, [fetchStatus]);

  const handleStart = async () => {
    const req = {
      symbol,
      leverage: parseInt(config.leverage, 10),
      upperPrice: parseFloat(config.upperPrice),
      lowerPrice: parseFloat(config.lowerPrice),
      gridCount: parseInt(config.gridCount, 10),
      amountPerGrid: config.amountPerGrid,
    };
    if (config.stopLossPrice) req.stopLossPrice = parseFloat(config.stopLossPrice);
    if (config.takeProfitPrice) req.takeProfitPrice = parseFloat(config.takeProfitPrice);

    try {
      await api.startGrid(req);
      Alert.alert('成功', '网格交易已开启');
      setShowModal(false);
      fetchStatus();
    } catch (e) { Alert.alert('失败', e.message); }
  };

  const handleStop = async () => {
    try {
      await api.stopGrid(symbol);
      Alert.alert('成功', '网格交易已关闭');
      fetchStatus();
    } catch (e) { Alert.alert('失败', e.message); }
  };

  const u = (key, val) => setConfig((p) => ({ ...p, [key]: val }));

  return (
    <View style={styles.panel}>
      <View style={styles.titleRow}>
        <View style={styles.titleContent}>
          <View style={[styles.statusDot, { backgroundColor: status?.active ? colors.green : colors.textMuted }]} />
          <Text style={styles.title}>网格交易</Text>
        </View>
        {status?.active ? (
          <TouchableOpacity style={styles.stopBtn} onPress={handleStop}>
            <Text style={styles.btnText}>关闭</Text>
          </TouchableOpacity>
        ) : (
          <TouchableOpacity style={styles.startBtn} onPress={() => {
            if (!symbol) return Alert.alert('提示', '请先选择交易对');
            setShowModal(true);
          }}>
            <Text style={styles.btnText}>开启</Text>
          </TouchableOpacity>
        )}
      </View>

      {status?.active && (
        <View style={styles.statusBox}>
          <Text style={styles.statusText}>
            价格区间: {status.config.lowerPrice.toFixed(2)} ~ {status.config.upperPrice.toFixed(2)}
          </Text>
          <Text style={styles.statusText}>
            网格数: {status.config.gridCount} | 每格: {status.config.amountPerGrid} U
          </Text>
          <Text style={styles.statusText}>
            买入: {status.filledBuys} 次 | 卖出: {status.filledSells} 次
          </Text>
          <Text style={[styles.statusText, {
            color: status.totalProfit >= 0 ? colors.greenLight : colors.redLight,
          }]}>
            网格利润: {status.totalProfit >= 0 ? '+' : ''}{status.totalProfit.toFixed(4)} U
          </Text>
          {status.currentPrice > 0 && (
            <Text style={[styles.statusText, { color: colors.yellow }]}>
              当前价: {status.currentPrice.toFixed(2)}
            </Text>
          )}
        </View>
      )}

      <Modal visible={showModal} animationType="slide" transparent>
        <View style={styles.overlay}>
          <View style={styles.modal}>
            <Text style={styles.modalTitle}>网格交易配置 - {symbol}</Text>

            <View style={styles.row}>
              <View style={styles.halfInput}>
                <Text style={styles.label}>价格下界</Text>
                <TextInput style={styles.input} value={config.lowerPrice}
                  onChangeText={(v) => u('lowerPrice', v)} keyboardType="decimal-pad"
                  placeholder="如 2500" placeholderTextColor={colors.textMuted} />
              </View>
              <View style={styles.halfInput}>
                <Text style={styles.label}>价格上界</Text>
                <TextInput style={styles.input} value={config.upperPrice}
                  onChangeText={(v) => u('upperPrice', v)} keyboardType="decimal-pad"
                  placeholder="如 2800" placeholderTextColor={colors.textMuted} />
              </View>
            </View>

            <View style={styles.row}>
              <View style={styles.halfInput}>
                <Text style={styles.label}>网格数量</Text>
                <TextInput style={styles.input} value={config.gridCount}
                  onChangeText={(v) => u('gridCount', v)} keyboardType="number-pad"
                  placeholderTextColor={colors.textMuted} />
              </View>
              <View style={styles.halfInput}>
                <Text style={styles.label}>每格金额 (U)</Text>
                <TextInput style={styles.input} value={config.amountPerGrid}
                  onChangeText={(v) => u('amountPerGrid', v)} keyboardType="decimal-pad"
                  placeholderTextColor={colors.textMuted} />
              </View>
            </View>

            <View style={styles.inputRow}>
              <Text style={styles.label}>杠杆</Text>
              <TextInput style={styles.input} value={config.leverage}
                onChangeText={(v) => u('leverage', v)} keyboardType="number-pad"
                placeholderTextColor={colors.textMuted} />
            </View>

            <View style={styles.row}>
              <View style={styles.halfInput}>
                <Text style={styles.label}>止损价 (选填)</Text>
                <TextInput style={styles.input} value={config.stopLossPrice}
                  onChangeText={(v) => u('stopLossPrice', v)} keyboardType="decimal-pad"
                  placeholder="整体止损" placeholderTextColor={colors.textMuted} />
              </View>
              <View style={styles.halfInput}>
                <Text style={styles.label}>止盈价 (选填)</Text>
                <TextInput style={styles.input} value={config.takeProfitPrice}
                  onChangeText={(v) => u('takeProfitPrice', v)} keyboardType="decimal-pad"
                  placeholder="整体止盈" placeholderTextColor={colors.textMuted} />
              </View>
            </View>

            {config.upperPrice && config.lowerPrice && config.gridCount && (
              <View style={styles.previewBox}>
                <Text style={styles.previewText}>
                  每格间距: {((parseFloat(config.upperPrice) - parseFloat(config.lowerPrice)) / (parseInt(config.gridCount, 10) - 1 || 1)).toFixed(2)} |
                  总投入: {(parseFloat(config.amountPerGrid || 0) * parseInt(config.gridCount, 10)).toFixed(0)} U
                </Text>
              </View>
            )}

            <View style={styles.row}>
              <TouchableOpacity style={[styles.modalBtn, styles.cancelBtn]} onPress={() => setShowModal(false)}>
                <Text style={styles.cancelBtnText}>取消</Text>
              </TouchableOpacity>
              <TouchableOpacity style={[styles.modalBtn, styles.confirmBtn]} onPress={handleStart}>
                <Text style={styles.confirmBtnText}>开启</Text>
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
  titleRow: { flexDirection: 'row', justifyContent: 'space-between', alignItems: 'center', marginBottom: spacing.lg },
  titleContent: { flexDirection: 'row', alignItems: 'center' },
  statusDot: { width: 8, height: 8, borderRadius: 4, marginRight: spacing.sm },
  title: { fontSize: fontSize.lg, fontWeight: '700', color: colors.white },
  startBtn: { backgroundColor: colors.gold, paddingHorizontal: spacing.lg, paddingVertical: spacing.sm, borderRadius: radius.pill },
  stopBtn: { backgroundColor: colors.red, paddingHorizontal: spacing.lg, paddingVertical: spacing.sm, borderRadius: radius.pill },
  btnText: { color: colors.white, fontWeight: '700', fontSize: fontSize.sm },
  statusBox: { backgroundColor: colors.surface, padding: spacing.md, borderRadius: radius.lg, marginTop: spacing.sm, borderWidth: 1, borderColor: colors.cardBorder },
  statusText: { color: colors.textSecondary, fontSize: fontSize.sm, marginBottom: spacing.xs },
  overlay: { flex: 1, backgroundColor: 'rgba(0,0,0,0.75)', justifyContent: 'flex-end', padding: spacing.lg },
  modal: { backgroundColor: colors.card, borderTopLeftRadius: radius.xxl, borderTopRightRadius: radius.xxl, padding: spacing.xl, maxHeight: '85%', borderWidth: 1, borderColor: colors.cardBorder },
  modalTitle: { fontSize: fontSize.xl, fontWeight: '800', color: colors.white, marginBottom: spacing.xl, textAlign: 'center' },
  row: { flexDirection: 'row', gap: spacing.md, marginBottom: spacing.md },
  halfInput: { flex: 1 },
  inputRow: { marginBottom: spacing.md },
  label: { color: colors.textSecondary, fontSize: fontSize.sm, marginBottom: spacing.xs },
  input: { backgroundColor: colors.surface, borderRadius: radius.md, padding: spacing.md, color: colors.white, borderWidth: 1, borderColor: colors.cardBorder, fontSize: fontSize.md },
  previewBox: { backgroundColor: colors.surface, borderRadius: radius.lg, padding: spacing.md, marginBottom: spacing.md, borderWidth: 1, borderColor: colors.cardBorder },
  previewText: { color: colors.textSecondary, fontSize: fontSize.sm, textAlign: 'center' },
  modalBtn: { flex: 1, paddingVertical: spacing.md, borderRadius: radius.lg, alignItems: 'center' },
  cancelBtn: { backgroundColor: colors.surface, borderWidth: 1, borderColor: colors.cardBorder },
  cancelBtnText: { color: colors.textSecondary, fontWeight: '600', fontSize: fontSize.md },
  confirmBtn: { backgroundColor: colors.gold },
  confirmBtnText: { color: colors.white, fontWeight: '800', fontSize: fontSize.md },
});
