import React, { useState, useEffect, useCallback } from 'react';
import {
  View, Text, TextInput, TouchableOpacity, StyleSheet, Alert, Modal,
} from 'react-native';
import api from '../services/api';
import { colors } from '../services/theme';

const INTERVAL_OPTIONS = [
  { label: '5m', value: '5m' },
  { label: '15m', value: '15m' },
  { label: '30m', value: '30m' },
  { label: '1h', value: '1h' },
  { label: '4h', value: '4h' },
];

export default function SignalPanel({ symbol }) {
  const [showModal, setShowModal] = useState(false);
  const [status, setStatus] = useState(null);
  const [config, setConfig] = useState({
    leverage: '10',
    interval: '15m',
    rsiPeriod: '14',
    rsiOverbought: '70',
    rsiOversold: '30',
    volumePeriod: '20',
    volumeMulti: '1.5',
    amountPerOrder: '5',
    maxPositions: '1',
    stopLossPercent: '2',
    takeProfitPercent: '6',
    rsiExitOverbought: '65',
    rsiExitOversold: '35',
  });

  const fetchStatus = useCallback(async () => {
    try {
      const res = await api.signalStatus(symbol);
      setStatus(res.data);
    } catch (_) {}
  }, [symbol]);

  useEffect(() => {
    fetchStatus();
    const t = setInterval(fetchStatus, 5000);
    return () => clearInterval(t);
  }, [fetchStatus]);

  const handleStart = async () => {
    try {
      await api.startSignal({
        symbol,
        leverage: parseInt(config.leverage, 10),
        interval: config.interval,
        rsiPeriod: parseInt(config.rsiPeriod, 10),
        rsiOverbought: parseFloat(config.rsiOverbought),
        rsiOversold: parseFloat(config.rsiOversold),
        volumePeriod: parseInt(config.volumePeriod, 10),
        volumeMulti: parseFloat(config.volumeMulti),
        amountPerOrder: config.amountPerOrder,
        maxPositions: parseInt(config.maxPositions, 10),
        stopLossPercent: parseFloat(config.stopLossPercent) || 0,
        takeProfitPercent: parseFloat(config.takeProfitPercent) || 0,
        rsiExitOverbought: parseFloat(config.rsiExitOverbought) || 0,
        rsiExitOversold: parseFloat(config.rsiExitOversold) || 0,
      });
      setShowModal(false);
      fetchStatus();
      Alert.alert('成功', '信号策略已启动');
    } catch (e) {
      Alert.alert('失败', e.message);
    }
  };

  const handleStop = async () => {
    Alert.alert('确认', '停止信号策略？', [
      { text: '取消' },
      {
        text: '停止', style: 'destructive', onPress: async () => {
          try {
            await api.stopSignal(symbol);
            fetchStatus();
          } catch (e) {
            Alert.alert('失败', e.message);
          }
        },
      },
    ]);
  };

  const isActive = status?.active;

  const rsiColor = (rsi) => {
    if (!rsi) return colors.textSecondary;
    if (rsi >= 70) return colors.red;
    if (rsi <= 30) return colors.green;
    return colors.yellow || '#f0b90b';
  };

  const signalColor = (sig) => {
    if (sig === 'BUY') return colors.green;
    if (sig === 'SELL') return colors.red;
    return colors.textMuted;
  };

  return (
    <View style={styles.panel}>
      <View style={styles.titleRow}>
        <Text style={styles.title}>RSI + 量能信号</Text>
        {isActive ? (
          <TouchableOpacity style={styles.stopBtn} onPress={handleStop}>
            <Text style={styles.stopBtnText}>停止</Text>
          </TouchableOpacity>
        ) : (
          <TouchableOpacity style={styles.startBtn} onPress={() => setShowModal(true)}>
            <Text style={styles.startBtnText}>配置启动</Text>
          </TouchableOpacity>
        )}
      </View>

      {/* 实时状态 */}
      {status && isActive && (
        <View style={styles.statusBox}>
          <View style={styles.statusRow}>
            <View style={styles.statusItem}>
              <Text style={styles.statusLabel}>RSI</Text>
              <Text style={[styles.statusValue, { color: rsiColor(status.currentRsi) }]}>
                {status.currentRsi || '--'}
              </Text>
            </View>
            <View style={styles.statusItem}>
              <Text style={styles.statusLabel}>量比</Text>
              <Text style={[styles.statusValue, {
                color: status.volRatio >= (status.config?.volumeMulti || 1.5) ? colors.green : colors.textSecondary,
              }]}>
                {status.volRatio || '--'}x
              </Text>
            </View>
            <View style={styles.statusItem}>
              <Text style={styles.statusLabel}>信号</Text>
              <Text style={[styles.statusValue, { color: signalColor(status.lastSignal) }]}>
                {status.lastSignal === 'BUY' ? '做多' : status.lastSignal === 'SELL' ? '做空' : '无'}
              </Text>
            </View>
          </View>

          <View style={styles.statusRow}>
            <View style={styles.statusItem}>
              <Text style={styles.statusLabel}>持仓</Text>
              <Text style={styles.statusValue}>{status.openTrades}/{status.config?.maxPositions}</Text>
            </View>
            <View style={styles.statusItem}>
              <Text style={styles.statusLabel}>交易</Text>
              <Text style={styles.statusValue}>{status.totalTrades}次</Text>
            </View>
            <View style={styles.statusItem}>
              <Text style={styles.statusLabel}>总盈亏</Text>
              <Text style={[styles.statusValue, {
                color: status.totalPnl >= 0 ? colors.green : colors.red,
              }]}>
                {status.totalPnl >= 0 ? '+' : ''}{status.totalPnl} U
              </Text>
            </View>
          </View>

          <View style={styles.infoRow}>
            <Text style={styles.infoText}>
              {status.config?.interval} | RSI({status.config?.rsiPeriod}) {status.config?.rsiOversold}/{status.config?.rsiOverbought} | 量×{status.config?.volumeMulti}
            </Text>
          </View>

          {status.signalTime ? (
            <Text style={styles.timeText}>最近信号: {status.signalTime}</Text>
          ) : null}
          {status.lastCheckAt ? (
            <Text style={styles.timeText}>最近检查: {status.lastCheckAt}</Text>
          ) : null}
          {status.lastError ? (
            <Text style={styles.errorText}>{status.lastError}</Text>
          ) : null}
        </View>
      )}

      {/* 未运行提示 */}
      {!isActive && (
        <Text style={styles.inactiveText}>
          RSI 超卖回升 + 成交量放大 → 做多{'\n'}
          RSI 超买回落 + 成交量放大 → 做空{'\n'}
          适合中长线趋势判断
        </Text>
      )}

      {/* 配置弹窗 */}
      <Modal visible={showModal} transparent animationType="slide">
        <View style={styles.modalOverlay}>
          <View style={styles.modalContent}>
            <Text style={styles.modalTitle}>信号策略配置</Text>

            {/* K 线周期 */}
            <Text style={styles.sectionLabel}>K线周期</Text>
            <View style={styles.chipRow}>
              {INTERVAL_OPTIONS.map((opt) => (
                <TouchableOpacity
                  key={opt.value}
                  style={[styles.chip, config.interval === opt.value && styles.chipActive]}
                  onPress={() => setConfig({ ...config, interval: opt.value })}
                >
                  <Text style={[styles.chipText, config.interval === opt.value && styles.chipTextActive]}>
                    {opt.label}
                  </Text>
                </TouchableOpacity>
              ))}
            </View>

            {/* RSI 参数 */}
            <Text style={styles.sectionLabel}>RSI 参数</Text>
            <View style={styles.inputRow}>
              <View style={styles.inputGroup}>
                <Text style={styles.inputLabel}>周期</Text>
                <TextInput style={styles.input} value={config.rsiPeriod}
                  onChangeText={(v) => setConfig({ ...config, rsiPeriod: v })}
                  keyboardType="number-pad" />
              </View>
              <View style={styles.inputGroup}>
                <Text style={styles.inputLabel}>超卖</Text>
                <TextInput style={styles.input} value={config.rsiOversold}
                  onChangeText={(v) => setConfig({ ...config, rsiOversold: v })}
                  keyboardType="decimal-pad" />
              </View>
              <View style={styles.inputGroup}>
                <Text style={styles.inputLabel}>超买</Text>
                <TextInput style={styles.input} value={config.rsiOverbought}
                  onChangeText={(v) => setConfig({ ...config, rsiOverbought: v })}
                  keyboardType="decimal-pad" />
              </View>
            </View>

            {/* 成交量参数 */}
            <Text style={styles.sectionLabel}>成交量确认</Text>
            <View style={styles.inputRow}>
              <View style={styles.inputGroup}>
                <Text style={styles.inputLabel}>均量周期</Text>
                <TextInput style={styles.input} value={config.volumePeriod}
                  onChangeText={(v) => setConfig({ ...config, volumePeriod: v })}
                  keyboardType="number-pad" />
              </View>
              <View style={styles.inputGroup}>
                <Text style={styles.inputLabel}>放量倍数</Text>
                <TextInput style={styles.input} value={config.volumeMulti}
                  onChangeText={(v) => setConfig({ ...config, volumeMulti: v })}
                  keyboardType="decimal-pad" />
              </View>
            </View>

            {/* 下单参数 */}
            <Text style={styles.sectionLabel}>下单参数</Text>
            <View style={styles.inputRow}>
              <View style={styles.inputGroup}>
                <Text style={styles.inputLabel}>金额(U)</Text>
                <TextInput style={styles.input} value={config.amountPerOrder}
                  onChangeText={(v) => setConfig({ ...config, amountPerOrder: v })}
                  keyboardType="decimal-pad" />
              </View>
              <View style={styles.inputGroup}>
                <Text style={styles.inputLabel}>杠杆</Text>
                <TextInput style={styles.input} value={config.leverage}
                  onChangeText={(v) => setConfig({ ...config, leverage: v })}
                  keyboardType="number-pad" />
              </View>
              <View style={styles.inputGroup}>
                <Text style={styles.inputLabel}>最大持仓</Text>
                <TextInput style={styles.input} value={config.maxPositions}
                  onChangeText={(v) => setConfig({ ...config, maxPositions: v })}
                  keyboardType="number-pad" />
              </View>
            </View>

            {/* 止盈止损 */}
            <Text style={styles.sectionLabel}>止盈止损 (%)</Text>
            <View style={styles.inputRow}>
              <View style={styles.inputGroup}>
                <Text style={styles.inputLabel}>止损%</Text>
                <TextInput style={styles.input} value={config.stopLossPercent}
                  onChangeText={(v) => setConfig({ ...config, stopLossPercent: v })}
                  keyboardType="decimal-pad" />
              </View>
              <View style={styles.inputGroup}>
                <Text style={styles.inputLabel}>止盈%</Text>
                <TextInput style={styles.input} value={config.takeProfitPercent}
                  onChangeText={(v) => setConfig({ ...config, takeProfitPercent: v })}
                  keyboardType="decimal-pad" />
              </View>
            </View>

            {/* RSI 平仓 */}
            <Text style={styles.sectionLabel}>RSI 平仓条件（可选）</Text>
            <View style={styles.inputRow}>
              <View style={styles.inputGroup}>
                <Text style={styles.inputLabel}>多单平仓RSI</Text>
                <TextInput style={styles.input} value={config.rsiExitOverbought}
                  onChangeText={(v) => setConfig({ ...config, rsiExitOverbought: v })}
                  keyboardType="decimal-pad" placeholder="如 65" placeholderTextColor={colors.textMuted} />
              </View>
              <View style={styles.inputGroup}>
                <Text style={styles.inputLabel}>空单平仓RSI</Text>
                <TextInput style={styles.input} value={config.rsiExitOversold}
                  onChangeText={(v) => setConfig({ ...config, rsiExitOversold: v })}
                  keyboardType="decimal-pad" placeholder="如 35" placeholderTextColor={colors.textMuted} />
              </View>
            </View>

            {/* 预览 */}
            <View style={styles.previewBox}>
              <Text style={styles.previewText}>
                每次 {config.amountPerOrder} U × {config.leverage}x = {(parseFloat(config.amountPerOrder || 0) * parseInt(config.leverage || 1, 10)).toFixed(0)} U 名义价值
              </Text>
              <Text style={styles.previewText}>
                止损 {config.stopLossPercent}% / 止盈 {config.takeProfitPercent}% (盈亏比 {(parseFloat(config.takeProfitPercent || 0) / parseFloat(config.stopLossPercent || 1)).toFixed(1)})
              </Text>
            </View>

            {/* 按钮 */}
            <View style={styles.btnRow}>
              <TouchableOpacity style={styles.cancelBtn} onPress={() => setShowModal(false)}>
                <Text style={styles.cancelBtnText}>取消</Text>
              </TouchableOpacity>
              <TouchableOpacity style={styles.confirmBtn} onPress={handleStart}>
                <Text style={styles.confirmBtnText}>启动策略</Text>
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
  titleRow: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'center',
    marginBottom: 10,
  },
  title: { fontSize: 16, fontWeight: 'bold', color: colors.white },
  startBtn: {
    backgroundColor: colors.blue,
    paddingHorizontal: 14,
    paddingVertical: 6,
    borderRadius: 6,
  },
  startBtnText: { color: colors.white, fontSize: 13, fontWeight: '600' },
  stopBtn: {
    backgroundColor: colors.red,
    paddingHorizontal: 14,
    paddingVertical: 6,
    borderRadius: 6,
  },
  stopBtnText: { color: colors.white, fontSize: 13, fontWeight: '600' },

  // Status
  statusBox: {
    backgroundColor: colors.bg,
    borderRadius: 8,
    padding: 10,
    borderWidth: 1,
    borderColor: colors.cardBorder,
  },
  statusRow: {
    flexDirection: 'row',
    marginBottom: 8,
  },
  statusItem: { flex: 1, alignItems: 'center' },
  statusLabel: { color: colors.textMuted, fontSize: 11, marginBottom: 2 },
  statusValue: { color: colors.white, fontSize: 16, fontWeight: 'bold' },
  infoRow: { alignItems: 'center', marginBottom: 4 },
  infoText: { color: colors.textSecondary, fontSize: 11 },
  timeText: { color: colors.textMuted, fontSize: 11, textAlign: 'center', marginTop: 2 },
  errorText: { color: colors.red, fontSize: 11, textAlign: 'center', marginTop: 4 },
  inactiveText: { color: colors.textMuted, fontSize: 13, lineHeight: 20 },

  // Modal
  modalOverlay: {
    flex: 1,
    backgroundColor: 'rgba(0,0,0,0.7)',
    justifyContent: 'center',
    padding: 16,
  },
  modalContent: {
    backgroundColor: colors.card,
    borderRadius: 16,
    padding: 20,
    maxHeight: '90%',
  },
  modalTitle: { fontSize: 18, fontWeight: 'bold', color: colors.white, marginBottom: 16, textAlign: 'center' },
  sectionLabel: { color: colors.textSecondary, fontSize: 13, fontWeight: '600', marginBottom: 6, marginTop: 8 },

  chipRow: { flexDirection: 'row', gap: 6, marginBottom: 4 },
  chip: {
    flex: 1,
    paddingVertical: 7,
    borderRadius: 6,
    alignItems: 'center',
    backgroundColor: colors.surface,
    borderWidth: 1,
    borderColor: colors.cardBorder,
  },
  chipActive: { backgroundColor: colors.blue, borderColor: colors.blue },
  chipText: { color: colors.textSecondary, fontSize: 12, fontWeight: '600' },
  chipTextActive: { color: colors.white },

  inputRow: { flexDirection: 'row', gap: 8, marginBottom: 4 },
  inputGroup: { flex: 1 },
  inputLabel: { color: colors.textMuted, fontSize: 11, marginBottom: 3 },
  input: {
    backgroundColor: colors.bg,
    borderRadius: 6,
    padding: 8,
    color: colors.white,
    borderWidth: 1,
    borderColor: colors.cardBorder,
    fontSize: 14,
    textAlign: 'center',
  },

  previewBox: {
    backgroundColor: colors.bg,
    borderRadius: 8,
    padding: 10,
    marginTop: 12,
    borderWidth: 1,
    borderColor: colors.cardBorder,
  },
  previewText: { color: colors.textSecondary, fontSize: 12, textAlign: 'center', lineHeight: 18 },

  btnRow: { flexDirection: 'row', gap: 10, marginTop: 16 },
  cancelBtn: {
    flex: 1,
    paddingVertical: 12,
    borderRadius: 8,
    alignItems: 'center',
    backgroundColor: colors.surface,
  },
  cancelBtnText: { color: colors.textSecondary, fontSize: 15, fontWeight: '600' },
  confirmBtn: {
    flex: 2,
    paddingVertical: 12,
    borderRadius: 8,
    alignItems: 'center',
    backgroundColor: colors.blue,
  },
  confirmBtnText: { color: colors.white, fontSize: 15, fontWeight: 'bold' },
});
