import React, { useState, useEffect, useCallback } from 'react';
import {
  View, Text, TextInput, TouchableOpacity, StyleSheet, Alert, Modal, Switch, ScrollView,
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

const PATTERN_NAMES = {
  NONE: '无',
  DOJI: '十字星',
  HAMMER: '锤子线',
  SHOOTING_STAR: '射击之星',
  ENGULF_BULL: '看涨吞没',
  ENGULF_BEAR: '看跌吞没',
};

const TREND_NAMES = {
  UP: '上涨 ↑',
  DOWN: '下跌 ↓',
  FLAT: '横盘 →',
};

export default function DojiPanel({ symbol }) {
  const [showModal, setShowModal] = useState(false);
  const [status, setStatus] = useState(null);
  const [config, setConfig] = useState({
    leverage: '10',
    interval: '15m',
    bodyRatio: '0.1',
    shadowRatio: '2.0',
    enableDoji: true,
    enableHammer: true,
    enableEngulf: true,
    trendBars: '5',
    trendStrength: '0.3',
    enableRsi: false,
    rsiPeriod: '14',
    rsiOverbought: '65',
    rsiOversold: '35',
    enableVolume: false,
    volumePeriod: '20',
    volumeMulti: '1.2',
    amountPerOrder: '5',
    maxPositions: '1',
    stopLossPercent: '2',
    takeProfitPercent: '6',
  });

  const fetchStatus = useCallback(async () => {
    try {
      const res = await api.dojiStatus(symbol);
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
      await api.startDoji({
        symbol,
        leverage: parseInt(config.leverage, 10),
        interval: config.interval,
        bodyRatio: parseFloat(config.bodyRatio),
        shadowRatio: parseFloat(config.shadowRatio),
        enableDoji: config.enableDoji,
        enableHammer: config.enableHammer,
        enableEngulf: config.enableEngulf,
        trendBars: parseInt(config.trendBars, 10),
        trendStrength: parseFloat(config.trendStrength),
        enableRsi: config.enableRsi,
        rsiPeriod: parseInt(config.rsiPeriod, 10),
        rsiOverbought: parseFloat(config.rsiOverbought),
        rsiOversold: parseFloat(config.rsiOversold),
        enableVolume: config.enableVolume,
        volumePeriod: parseInt(config.volumePeriod, 10),
        volumeMulti: parseFloat(config.volumeMulti),
        amountPerOrder: config.amountPerOrder,
        maxPositions: parseInt(config.maxPositions, 10),
        stopLossPercent: parseFloat(config.stopLossPercent) || 0,
        takeProfitPercent: parseFloat(config.takeProfitPercent) || 0,
      });
      setShowModal(false);
      fetchStatus();
      Alert.alert('成功', 'K线形态策略已启动');
    } catch (e) {
      Alert.alert('失败', e.message);
    }
  };

  const handleStop = async () => {
    Alert.alert('确认', '停止K线形态策略？', [
      { text: '取消' },
      {
        text: '停止', style: 'destructive', onPress: async () => {
          try {
            await api.stopDoji(symbol);
            fetchStatus();
          } catch (e) {
            Alert.alert('失败', e.message);
          }
        },
      },
    ]);
  };

  const isActive = status?.active;

  const patternColor = (p) => {
    if (!p || p === 'NONE') return colors.textMuted;
    if (p === 'HAMMER' || p === 'ENGULF_BULL') return colors.green;
    if (p === 'SHOOTING_STAR' || p === 'ENGULF_BEAR') return colors.red;
    return colors.yellow || '#f0b90b';
  };

  const trendColor = (t) => {
    if (t === 'UP') return colors.green;
    if (t === 'DOWN') return colors.red;
    return colors.textMuted;
  };

  const signalColor = (sig) => {
    if (sig === 'BUY') return colors.green;
    if (sig === 'SELL') return colors.red;
    return colors.textMuted;
  };

  return (
    <View style={styles.panel}>
      <View style={styles.titleRow}>
        <Text style={styles.title}>K线形态策略</Text>
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
              <Text style={styles.statusLabel}>形态</Text>
              <Text style={[styles.statusValue, { color: patternColor(status.lastPattern), fontSize: 13 }]}>
                {PATTERN_NAMES[status.lastPattern] || '无'}
              </Text>
            </View>
            <View style={styles.statusItem}>
              <Text style={styles.statusLabel}>趋势</Text>
              <Text style={[styles.statusValue, { color: trendColor(status.trendDir), fontSize: 13 }]}>
                {TREND_NAMES[status.trendDir] || '--'}
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
            {status.config?.enableRsi && (
              <View style={styles.statusItem}>
                <Text style={styles.statusLabel}>RSI</Text>
                <Text style={styles.statusValue}>{status.currentRsi || '--'}</Text>
              </View>
            )}
            {status.config?.enableVolume && (
              <View style={styles.statusItem}>
                <Text style={styles.statusLabel}>量比</Text>
                <Text style={styles.statusValue}>{status.volRatio || '--'}x</Text>
              </View>
            )}
            <View style={styles.statusItem}>
              <Text style={styles.statusLabel}>持仓</Text>
              <Text style={styles.statusValue}>{status.openTrades}/{status.config?.maxPositions}</Text>
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
              {status.config?.interval} | 趋势{status.config?.trendBars}根 |
              {status.config?.enableDoji ? ' 十字星' : ''}
              {status.config?.enableHammer ? ' 锤子' : ''}
              {status.config?.enableEngulf ? ' 吞没' : ''}
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
          识别十字星、锤子线、射击之星、吞没等K线形态{'\n'}
          结合趋势方向判断反转信号开仓{'\n'}
          可选叠加 RSI + 成交量过滤
        </Text>
      )}

      {/* 配置弹窗 */}
      <Modal visible={showModal} transparent animationType="slide">
        <View style={styles.modalOverlay}>
          <ScrollView style={{ maxHeight: '90%' }}>
            <View style={styles.modalContent}>
              <Text style={styles.modalTitle}>K线形态策略配置</Text>

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

              {/* 形态选择 */}
              <Text style={styles.sectionLabel}>识别形态</Text>
              <View style={styles.switchRow}>
                <View style={styles.switchItem}>
                  <Text style={styles.switchLabel}>十字星</Text>
                  <Switch value={config.enableDoji}
                    onValueChange={(v) => setConfig({ ...config, enableDoji: v })}
                    trackColor={{ true: colors.blue }} />
                </View>
                <View style={styles.switchItem}>
                  <Text style={styles.switchLabel}>锤子/射击</Text>
                  <Switch value={config.enableHammer}
                    onValueChange={(v) => setConfig({ ...config, enableHammer: v })}
                    trackColor={{ true: colors.blue }} />
                </View>
                <View style={styles.switchItem}>
                  <Text style={styles.switchLabel}>吞没</Text>
                  <Switch value={config.enableEngulf}
                    onValueChange={(v) => setConfig({ ...config, enableEngulf: v })}
                    trackColor={{ true: colors.blue }} />
                </View>
              </View>

              {/* 形态参数 */}
              <Text style={styles.sectionLabel}>形态参数</Text>
              <View style={styles.inputRow}>
                <View style={styles.inputGroup}>
                  <Text style={styles.inputLabel}>实体比</Text>
                  <TextInput style={styles.input} value={config.bodyRatio}
                    onChangeText={(v) => setConfig({ ...config, bodyRatio: v })}
                    keyboardType="decimal-pad" />
                </View>
                <View style={styles.inputGroup}>
                  <Text style={styles.inputLabel}>影线比</Text>
                  <TextInput style={styles.input} value={config.shadowRatio}
                    onChangeText={(v) => setConfig({ ...config, shadowRatio: v })}
                    keyboardType="decimal-pad" />
                </View>
              </View>

              {/* 趋势参数 */}
              <Text style={styles.sectionLabel}>趋势确认</Text>
              <View style={styles.inputRow}>
                <View style={styles.inputGroup}>
                  <Text style={styles.inputLabel}>回溯K线</Text>
                  <TextInput style={styles.input} value={config.trendBars}
                    onChangeText={(v) => setConfig({ ...config, trendBars: v })}
                    keyboardType="number-pad" />
                </View>
                <View style={styles.inputGroup}>
                  <Text style={styles.inputLabel}>最小涨跌%</Text>
                  <TextInput style={styles.input} value={config.trendStrength}
                    onChangeText={(v) => setConfig({ ...config, trendStrength: v })}
                    keyboardType="decimal-pad" />
                </View>
              </View>

              {/* RSI 过滤 */}
              <View style={styles.filterHeader}>
                <Text style={styles.sectionLabel}>RSI 过滤（可选）</Text>
                <Switch value={config.enableRsi}
                  onValueChange={(v) => setConfig({ ...config, enableRsi: v })}
                  trackColor={{ true: colors.blue }} />
              </View>
              {config.enableRsi && (
                <View style={styles.inputRow}>
                  <View style={styles.inputGroup}>
                    <Text style={styles.inputLabel}>周期</Text>
                    <TextInput style={styles.input} value={config.rsiPeriod}
                      onChangeText={(v) => setConfig({ ...config, rsiPeriod: v })}
                      keyboardType="number-pad" />
                  </View>
                  <View style={styles.inputGroup}>
                    <Text style={styles.inputLabel}>多≤</Text>
                    <TextInput style={styles.input} value={config.rsiOversold}
                      onChangeText={(v) => setConfig({ ...config, rsiOversold: v })}
                      keyboardType="decimal-pad" />
                  </View>
                  <View style={styles.inputGroup}>
                    <Text style={styles.inputLabel}>空≥</Text>
                    <TextInput style={styles.input} value={config.rsiOverbought}
                      onChangeText={(v) => setConfig({ ...config, rsiOverbought: v })}
                      keyboardType="decimal-pad" />
                  </View>
                </View>
              )}

              {/* 成交量过滤 */}
              <View style={styles.filterHeader}>
                <Text style={styles.sectionLabel}>成交量过滤（可选）</Text>
                <Switch value={config.enableVolume}
                  onValueChange={(v) => setConfig({ ...config, enableVolume: v })}
                  trackColor={{ true: colors.blue }} />
              </View>
              {config.enableVolume && (
                <View style={styles.inputRow}>
                  <View style={styles.inputGroup}>
                    <Text style={styles.inputLabel}>均量周期</Text>
                    <TextInput style={styles.input} value={config.volumePeriod}
                      onChangeText={(v) => setConfig({ ...config, volumePeriod: v })}
                      keyboardType="number-pad" />
                  </View>
                  <View style={styles.inputGroup}>
                    <Text style={styles.inputLabel}>量比≥</Text>
                    <TextInput style={styles.input} value={config.volumeMulti}
                      onChangeText={(v) => setConfig({ ...config, volumeMulti: v })}
                      keyboardType="decimal-pad" />
                  </View>
                </View>
              )}

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
          </ScrollView>
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
  },
  modalTitle: { fontSize: 18, fontWeight: 'bold', color: colors.white, marginBottom: 16, textAlign: 'center' },
  sectionLabel: { color: colors.textSecondary, fontSize: 13, fontWeight: '600', marginBottom: 6, marginTop: 8, flex: 1 },

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

  switchRow: { flexDirection: 'row', gap: 8, marginBottom: 4 },
  switchItem: { flex: 1, flexDirection: 'row', alignItems: 'center', justifyContent: 'space-between', backgroundColor: colors.surface, borderRadius: 6, paddingHorizontal: 8, paddingVertical: 6 },
  switchLabel: { color: colors.textSecondary, fontSize: 12 },

  filterHeader: { flexDirection: 'row', alignItems: 'center', justifyContent: 'space-between', marginTop: 8 },

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
