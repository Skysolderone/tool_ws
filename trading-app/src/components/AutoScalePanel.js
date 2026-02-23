import React, { useState, useEffect, useCallback } from 'react';
import {
  View,
  Text,
  TextInput,
  TouchableOpacity,
  StyleSheet,
  Alert,
  Modal,
  Switch,
} from 'react-native';
import api from '../services/api';
import { colors, spacing, radius, fontSize } from '../services/theme';

export default function AutoScalePanel({ symbol }) {
  const [showModal, setShowModal] = useState(false);
  const [status, setStatus] = useState(null);
  const [config, setConfig] = useState({
    side: 'BUY',
    leverage: '10',
    triggerType: 'amount',
    triggerAmount: '2',
    triggerPercent: '2',
    addQuantity: '5',
    maxScaleCount: '3',
    updateTPSL: false,
    stopLossAmount: '1',
    riskReward: '3',
  });

  const fetchStatus = useCallback(async () => {
    if (!symbol) return;
    try {
      const data = await api.autoScaleStatus(symbol);
      setStatus(data.data);
    } catch {
      setStatus(null);
    }
  }, [symbol]);

  useEffect(() => {
    fetchStatus();
    const timer = setInterval(fetchStatus, 5000);
    return () => clearInterval(timer);
  }, [fetchStatus]);

  const handleStart = async () => {
    const req = {
      symbol,
      side: config.side,
      leverage: parseInt(config.leverage, 10),
      addQuantity: config.addQuantity,
      maxScaleCount: parseInt(config.maxScaleCount, 10),
      updateTPSL: config.updateTPSL,
    };
    if (config.triggerType === 'amount') {
      req.triggerAmount = parseFloat(config.triggerAmount);
    } else {
      req.triggerPercent = parseFloat(config.triggerPercent);
    }
    if (config.updateTPSL) {
      req.stopLossAmount = parseFloat(config.stopLossAmount);
      req.riskReward = parseFloat(config.riskReward);
    }

    try {
      await api.startAutoScale(req);
      Alert.alert('成功', '浮盈加仓已开启');
      setShowModal(false);
      fetchStatus();
    } catch (e) {
      Alert.alert('开启失败', e.message);
    }
  };

  const handleStop = async () => {
    try {
      await api.stopAutoScale(symbol);
      Alert.alert('成功', '浮盈加仓已关闭');
      fetchStatus();
    } catch (e) {
      Alert.alert('关闭失败', e.message);
    }
  };

  const updateConfig = (key, value) => setConfig((prev) => ({ ...prev, [key]: value }));

  return (
    <View style={styles.panel}>
      <View style={styles.titleRow}>
        <View style={styles.titleContent}>
          <View style={[styles.statusDot, { backgroundColor: status?.active ? colors.green : colors.textMuted }]} />
          <Text style={styles.title}>浮盈加仓</Text>
        </View>
        {status?.active ? (
          <TouchableOpacity style={styles.stopBtn} onPress={handleStop}>
            <Text style={styles.btnText}>关闭</Text>
          </TouchableOpacity>
        ) : (
          <TouchableOpacity
            style={styles.startBtn}
            onPress={() => {
              if (!symbol) return Alert.alert('提示', '请先选择交易对');
              setShowModal(true);
            }}
          >
            <Text style={styles.btnText}>开启</Text>
          </TouchableOpacity>
        )}
      </View>

      {/* 运行状态 */}
      {status?.active && (
        <View style={styles.statusBox}>
          <Text style={styles.statusText}>
            {status.config.symbol} | 已加仓 {status.scaleCount}/{status.config.maxScaleCount} 次
          </Text>
          <Text style={styles.statusText}>
            累计加仓 {status.totalAdded.toFixed(2)} USDT
          </Text>
        </View>
      )}

      {/* 配置弹窗 */}
      <Modal visible={showModal} animationType="slide" transparent>
        <View style={styles.overlay}>
          <View style={styles.modal}>
            <Text style={styles.modalTitle}>浮盈加仓配置 - {symbol}</Text>

            {/* 方向 */}
            <View style={styles.row}>
              {['BUY', 'SELL'].map((s) => (
                <TouchableOpacity
                  key={s}
                  style={[
                    styles.toggleBtn,
                    config.side === s && (s === 'BUY' ? styles.buyActive : styles.sellActive),
                  ]}
                  onPress={() => updateConfig('side', s)}
                >
                  <Text style={[styles.toggleText, config.side === s && styles.toggleTextActive]}>
                    {s === 'BUY' ? '多' : '空'}
                  </Text>
                </TouchableOpacity>
              ))}
            </View>

            {/* 触发方式 */}
            <View style={styles.row}>
              {['amount', 'percent'].map((t) => (
                <TouchableOpacity
                  key={t}
                  style={[styles.toggleBtn, config.triggerType === t && styles.goldActive]}
                  onPress={() => updateConfig('triggerType', t)}
                >
                  <Text style={[styles.toggleText, config.triggerType === t && styles.toggleTextActive]}>
                    {t === 'amount' ? '按金额' : '按百分比'}
                  </Text>
                </TouchableOpacity>
              ))}
            </View>

            {/* 触发阈值 */}
            <View style={styles.inputRow}>
              <Text style={styles.inputLabel}>
                {config.triggerType === 'amount' ? '每浮盈 (USDT)' : '每浮盈 (%)'}
              </Text>
              <TextInput
                style={styles.input}
                value={config.triggerType === 'amount' ? config.triggerAmount : config.triggerPercent}
                onChangeText={(v) =>
                  updateConfig(config.triggerType === 'amount' ? 'triggerAmount' : 'triggerPercent', v)
                }
                keyboardType="decimal-pad"
                placeholderTextColor={colors.textMuted}
              />
            </View>

            {/* 加仓金额 + 最大次数 */}
            <View style={styles.row}>
              <View style={styles.halfInput}>
                <Text style={styles.inputLabel}>加仓金额 (U)</Text>
                <TextInput
                  style={styles.input}
                  value={config.addQuantity}
                  onChangeText={(v) => updateConfig('addQuantity', v)}
                  keyboardType="decimal-pad"
                  placeholderTextColor={colors.textMuted}
                />
              </View>
              <View style={styles.halfInput}>
                <Text style={styles.inputLabel}>最大次数</Text>
                <TextInput
                  style={styles.input}
                  value={config.maxScaleCount}
                  onChangeText={(v) => updateConfig('maxScaleCount', v)}
                  keyboardType="number-pad"
                  placeholderTextColor={colors.textMuted}
                />
              </View>
            </View>

            {/* 杠杆 */}
            <View style={styles.inputRow}>
              <Text style={styles.inputLabel}>杠杆</Text>
              <TextInput
                style={styles.input}
                value={config.leverage}
                onChangeText={(v) => updateConfig('leverage', v)}
                keyboardType="number-pad"
                placeholderTextColor={colors.textMuted}
              />
            </View>

            {/* 更新止盈止损 */}
            <View style={styles.switchRow}>
              <Text style={styles.inputLabel}>加仓后更新止盈止损</Text>
              <Switch
                value={config.updateTPSL}
                onValueChange={(v) => updateConfig('updateTPSL', v)}
                trackColor={{ true: colors.gold }}
              />
            </View>

            {config.updateTPSL && (
              <View style={styles.row}>
                <View style={styles.halfInput}>
                  <Text style={styles.inputLabel}>止损 (U)</Text>
                  <TextInput
                    style={styles.input}
                    value={config.stopLossAmount}
                    onChangeText={(v) => updateConfig('stopLossAmount', v)}
                    keyboardType="decimal-pad"
                    placeholderTextColor={colors.textMuted}
                  />
                </View>
                <View style={styles.halfInput}>
                  <Text style={styles.inputLabel}>盈亏比</Text>
                  <TextInput
                    style={styles.input}
                    value={config.riskReward}
                    onChangeText={(v) => updateConfig('riskReward', v)}
                    keyboardType="decimal-pad"
                    placeholderTextColor={colors.textMuted}
                  />
                </View>
              </View>
            )}

            {/* 操作按钮 */}
            <View style={styles.row}>
              <TouchableOpacity
                style={[styles.modalBtn, styles.cancelBtn]}
                onPress={() => setShowModal(false)}
              >
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
  titleRow: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'center',
    marginBottom: spacing.lg,
  },
  titleContent: {
    flexDirection: 'row',
    alignItems: 'center',
  },
  statusDot: {
    width: 8,
    height: 8,
    borderRadius: 4,
    marginRight: spacing.sm,
  },
  title: { fontSize: fontSize.lg, fontWeight: '700', color: colors.white },
  startBtn: {
    backgroundColor: colors.green,
    paddingHorizontal: spacing.lg,
    paddingVertical: spacing.sm,
    borderRadius: radius.pill,
  },
  stopBtn: {
    backgroundColor: colors.red,
    paddingHorizontal: spacing.lg,
    paddingVertical: spacing.sm,
    borderRadius: radius.pill,
  },
  btnText: { color: colors.white, fontWeight: '700', fontSize: fontSize.sm },

  statusBox: {
    backgroundColor: colors.surface,
    padding: spacing.md,
    borderRadius: radius.lg,
    marginTop: spacing.sm,
    borderWidth: 1,
    borderColor: colors.cardBorder,
  },
  statusText: { color: colors.textSecondary, fontSize: fontSize.sm, marginBottom: spacing.xs },

  // Modal
  overlay: {
    flex: 1,
    backgroundColor: 'rgba(0,0,0,0.75)',
    justifyContent: 'flex-end',
    padding: spacing.lg,
  },
  modal: {
    backgroundColor: colors.card,
    borderTopLeftRadius: radius.xxl,
    borderTopRightRadius: radius.xxl,
    padding: spacing.xl,
    maxHeight: '85%',
    borderWidth: 1,
    borderColor: colors.cardBorder,
  },
  modalTitle: {
    fontSize: fontSize.xl,
    fontWeight: '800',
    color: colors.white,
    marginBottom: spacing.xl,
    textAlign: 'center',
  },

  row: { flexDirection: 'row', gap: spacing.md, marginBottom: spacing.md },
  toggleBtn: {
    flex: 1,
    paddingVertical: spacing.md,
    borderRadius: radius.lg,
    alignItems: 'center',
    backgroundColor: colors.surface,
    borderWidth: 1,
    borderColor: colors.cardBorder,
  },
  buyActive: { backgroundColor: colors.green, borderColor: colors.green },
  sellActive: { backgroundColor: colors.red, borderColor: colors.red },
  goldActive: { backgroundColor: colors.gold, borderColor: colors.gold },
  toggleText: { color: colors.textSecondary, fontWeight: '600' },
  toggleTextActive: { color: colors.white },

  halfInput: { flex: 1 },
  inputRow: { marginBottom: spacing.md },
  inputLabel: { color: colors.textSecondary, fontSize: fontSize.sm, marginBottom: spacing.xs },
  input: {
    backgroundColor: colors.surface,
    borderRadius: radius.md,
    padding: spacing.md,
    color: colors.white,
    borderWidth: 1,
    borderColor: colors.cardBorder,
    fontSize: fontSize.md,
  },

  switchRow: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'center',
    marginBottom: spacing.md,
  },

  modalBtn: { flex: 1, paddingVertical: spacing.md, borderRadius: radius.lg, alignItems: 'center' },
  cancelBtn: { backgroundColor: colors.surface, borderWidth: 1, borderColor: colors.cardBorder },
  cancelBtnText: { color: colors.textSecondary, fontWeight: '600', fontSize: fontSize.md },
  confirmBtn: { backgroundColor: colors.green },
  confirmBtnText: { color: colors.white, fontWeight: '800', fontSize: fontSize.md },
});
