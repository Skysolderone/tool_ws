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
import { colors } from '../services/theme';

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
      <View style={styles.header}>
        <Text style={styles.title}>浮盈加仓</Text>
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
                  style={[styles.toggleBtn, config.triggerType === t && styles.blueActive]}
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
                trackColor={{ true: colors.blue }}
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
    marginBottom: 8,
  },
  title: { fontSize: 18, fontWeight: 'bold', color: colors.white },
  startBtn: {
    backgroundColor: colors.green,
    paddingHorizontal: 16,
    paddingVertical: 6,
    borderRadius: 6,
  },
  stopBtn: {
    backgroundColor: colors.red,
    paddingHorizontal: 16,
    paddingVertical: 6,
    borderRadius: 6,
  },
  btnText: { color: colors.white, fontWeight: '600', fontSize: 13 },

  statusBox: {
    backgroundColor: colors.bg,
    padding: 10,
    borderRadius: 8,
    marginTop: 4,
  },
  statusText: { color: colors.greenLight, fontSize: 13, marginBottom: 2 },

  // Modal
  overlay: {
    flex: 1,
    backgroundColor: 'rgba(0,0,0,0.7)',
    justifyContent: 'flex-end',
  },
  modal: {
    backgroundColor: colors.card,
    borderTopLeftRadius: 20,
    borderTopRightRadius: 20,
    padding: 20,
    maxHeight: '85%',
  },
  modalTitle: {
    fontSize: 18,
    fontWeight: 'bold',
    color: colors.white,
    marginBottom: 16,
    textAlign: 'center',
  },

  row: { flexDirection: 'row', gap: 8, marginBottom: 12 },
  toggleBtn: {
    flex: 1,
    paddingVertical: 10,
    borderRadius: 8,
    alignItems: 'center',
    backgroundColor: colors.surface,
    borderWidth: 1,
    borderColor: colors.cardBorder,
  },
  buyActive: { backgroundColor: colors.green, borderColor: colors.green },
  sellActive: { backgroundColor: colors.red, borderColor: colors.red },
  blueActive: { backgroundColor: colors.blue, borderColor: colors.blue },
  toggleText: { color: colors.textSecondary, fontWeight: '600' },
  toggleTextActive: { color: colors.white },

  halfInput: { flex: 1 },
  inputRow: { marginBottom: 12 },
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

  switchRow: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'center',
    marginBottom: 12,
  },

  modalBtn: { flex: 1, paddingVertical: 12, borderRadius: 10, alignItems: 'center' },
  cancelBtn: { backgroundColor: colors.surface, borderWidth: 1, borderColor: colors.cardBorder },
  cancelBtnText: { color: colors.textSecondary, fontWeight: '600', fontSize: 15 },
  confirmBtn: { backgroundColor: colors.green },
  confirmBtnText: { color: colors.white, fontWeight: '600', fontSize: 15 },
});
