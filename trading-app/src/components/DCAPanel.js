import React, { useState, useEffect, useCallback } from 'react';
import {
  View, Text, TextInput, TouchableOpacity, StyleSheet, Alert, Modal, Switch,
} from 'react-native';
import api from '../services/api';
import { colors } from '../services/theme';

const INTERVAL_OPTIONS = [
  { label: '30秒', value: 30 },
  { label: '1分钟', value: 60 },
  { label: '5分钟', value: 300 },
  { label: '15分钟', value: 900 },
  { label: '1小时', value: 3600 },
];

export default function DCAPanel({ symbol }) {
  const [showModal, setShowModal] = useState(false);
  const [status, setStatus] = useState(null);
  const [config, setConfig] = useState({
    side: 'BUY',
    leverage: '10',
    amountPerOrder: '5',
    totalOrders: '10',
    intervalSec: 60,
    usePriceDrop: false,
    priceDropPercent: '2',
    stopLossAmount: '',
    takeProfitAmount: '',
  });

  const fetchStatus = useCallback(async () => {
    if (!symbol) return;
    try {
      const data = await api.dcaStatus(symbol);
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
      side: config.side,
      leverage: parseInt(config.leverage, 10),
      amountPerOrder: config.amountPerOrder,
      totalOrders: parseInt(config.totalOrders, 10),
      intervalSec: config.intervalSec,
    };
    if (config.usePriceDrop) {
      req.priceDropPercent = parseFloat(config.priceDropPercent);
    }
    if (config.stopLossAmount) req.stopLossAmount = parseFloat(config.stopLossAmount);
    if (config.takeProfitAmount) req.takeProfitAmount = parseFloat(config.takeProfitAmount);

    try {
      await api.startDCA(req);
      Alert.alert('成功', 'DCA定投已开启');
      setShowModal(false);
      fetchStatus();
    } catch (e) { Alert.alert('失败', e.message); }
  };

  const handleStop = async () => {
    try {
      await api.stopDCA(symbol);
      Alert.alert('成功', 'DCA定投已关闭');
      fetchStatus();
    } catch (e) { Alert.alert('失败', e.message); }
  };

  const u = (key, val) => setConfig((p) => ({ ...p, [key]: val }));

  return (
    <View style={styles.panel}>
      <View style={styles.header}>
        <Text style={styles.title}>DCA 定投</Text>
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
            {status.config.side === 'BUY' ? '做多' : '做空'} | 已投入 {status.orderCount}/{status.config.totalOrders} 次
          </Text>
          <Text style={styles.statusText}>
            累计: {status.totalAmount.toFixed(2)} U | 均价: {status.avgEntry.toFixed(2)}
          </Text>
          <Text style={[styles.statusText, {
            color: status.currentPnl >= 0 ? colors.greenLight : colors.redLight,
          }]}>
            当前浮盈: {status.currentPnl >= 0 ? '+' : ''}{status.currentPnl.toFixed(2)} U
          </Text>
          {status.lastOrderAt && (
            <Text style={[styles.statusText, { color: colors.textSecondary }]}>
              上次下单: {status.lastOrderAt}
            </Text>
          )}
        </View>
      )}

      <Modal visible={showModal} animationType="slide" transparent>
        <View style={styles.overlay}>
          <View style={styles.modal}>
            <Text style={styles.modalTitle}>DCA 定投配置 - {symbol}</Text>

            {/* 方向 */}
            <View style={styles.row}>
              {['BUY', 'SELL'].map((s) => (
                <TouchableOpacity key={s}
                  style={[styles.toggleBtn, config.side === s && (s === 'BUY' ? styles.buyActive : styles.sellActive)]}
                  onPress={() => u('side', s)}>
                  <Text style={[styles.toggleText, config.side === s && styles.toggleTextActive]}>
                    {s === 'BUY' ? '做多' : '做空'}
                  </Text>
                </TouchableOpacity>
              ))}
            </View>

            {/* 金额 + 次数 */}
            <View style={styles.row}>
              <View style={styles.halfInput}>
                <Text style={styles.label}>每次金额 (U)</Text>
                <TextInput style={styles.input} value={config.amountPerOrder}
                  onChangeText={(v) => u('amountPerOrder', v)} keyboardType="decimal-pad"
                  placeholderTextColor={colors.textMuted} />
              </View>
              <View style={styles.halfInput}>
                <Text style={styles.label}>总次数</Text>
                <TextInput style={styles.input} value={config.totalOrders}
                  onChangeText={(v) => u('totalOrders', v)} keyboardType="number-pad"
                  placeholderTextColor={colors.textMuted} />
              </View>
            </View>

            {/* 间隔 */}
            <Text style={styles.label}>投入间隔</Text>
            <View style={styles.chipRow}>
              {INTERVAL_OPTIONS.map((opt) => (
                <TouchableOpacity key={opt.value}
                  style={[styles.chip, config.intervalSec === opt.value && styles.chipActive]}
                  onPress={() => u('intervalSec', opt.value)}>
                  <Text style={[styles.chipText, config.intervalSec === opt.value && styles.chipTextActive]}>
                    {opt.label}
                  </Text>
                </TouchableOpacity>
              ))}
            </View>

            {/* 杠杆 */}
            <View style={styles.inputRow}>
              <Text style={styles.label}>杠杆</Text>
              <TextInput style={styles.input} value={config.leverage}
                onChangeText={(v) => u('leverage', v)} keyboardType="number-pad"
                placeholderTextColor={colors.textMuted} />
            </View>

            {/* 逢跌加仓 */}
            <View style={styles.switchRow}>
              <Text style={styles.label}>逢跌加仓（价格跌X%才买入）</Text>
              <Switch value={config.usePriceDrop} onValueChange={(v) => u('usePriceDrop', v)}
                trackColor={{ true: colors.blue }} />
            </View>
            {config.usePriceDrop && (
              <View style={styles.inputRow}>
                <Text style={styles.label}>跌幅触发 (%)</Text>
                <TextInput style={styles.input} value={config.priceDropPercent}
                  onChangeText={(v) => u('priceDropPercent', v)} keyboardType="decimal-pad"
                  placeholderTextColor={colors.textMuted} />
              </View>
            )}

            {/* 止损止盈 */}
            <View style={styles.row}>
              <View style={styles.halfInput}>
                <Text style={styles.label}>止损 (U, 选填)</Text>
                <TextInput style={styles.input} value={config.stopLossAmount}
                  onChangeText={(v) => u('stopLossAmount', v)} keyboardType="decimal-pad"
                  placeholder="亏X U平仓" placeholderTextColor={colors.textMuted} />
              </View>
              <View style={styles.halfInput}>
                <Text style={styles.label}>止盈 (U, 选填)</Text>
                <TextInput style={styles.input} value={config.takeProfitAmount}
                  onChangeText={(v) => u('takeProfitAmount', v)} keyboardType="decimal-pad"
                  placeholder="赚X U平仓" placeholderTextColor={colors.textMuted} />
              </View>
            </View>

            {/* 预览 */}
            <View style={styles.previewBox}>
              <Text style={styles.previewText}>
                总投入: {(parseFloat(config.amountPerOrder || 0) * parseInt(config.totalOrders || 0, 10)).toFixed(0)} U |
                预计耗时: {formatDuration(config.intervalSec * parseInt(config.totalOrders || 0, 10))}
              </Text>
            </View>

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

function formatDuration(sec) {
  if (sec < 60) return `${sec}秒`;
  if (sec < 3600) return `${Math.round(sec / 60)}分钟`;
  const h = Math.floor(sec / 3600);
  const m = Math.round((sec % 3600) / 60);
  return m > 0 ? `${h}小时${m}分` : `${h}小时`;
}

const styles = StyleSheet.create({
  panel: { backgroundColor: colors.card, borderRadius: 12, padding: 16, marginBottom: 12, borderWidth: 1, borderColor: colors.cardBorder },
  header: { flexDirection: 'row', justifyContent: 'space-between', alignItems: 'center', marginBottom: 8 },
  title: { fontSize: 18, fontWeight: 'bold', color: colors.white },
  startBtn: { backgroundColor: colors.green, paddingHorizontal: 16, paddingVertical: 6, borderRadius: 6 },
  stopBtn: { backgroundColor: colors.red, paddingHorizontal: 16, paddingVertical: 6, borderRadius: 6 },
  btnText: { color: colors.white, fontWeight: '600', fontSize: 13 },
  statusBox: { backgroundColor: colors.bg, padding: 10, borderRadius: 8, marginTop: 4 },
  statusText: { color: colors.greenLight, fontSize: 13, marginBottom: 2 },
  overlay: { flex: 1, backgroundColor: 'rgba(0,0,0,0.7)', justifyContent: 'flex-end' },
  modal: { backgroundColor: colors.card, borderTopLeftRadius: 20, borderTopRightRadius: 20, padding: 20, maxHeight: '90%' },
  modalTitle: { fontSize: 18, fontWeight: 'bold', color: colors.white, marginBottom: 16, textAlign: 'center' },
  row: { flexDirection: 'row', gap: 8, marginBottom: 12 },
  halfInput: { flex: 1 },
  inputRow: { marginBottom: 12 },
  label: { color: colors.textSecondary, fontSize: 12, marginBottom: 4 },
  input: { backgroundColor: colors.bg, borderRadius: 8, padding: 10, color: colors.white, borderWidth: 1, borderColor: colors.cardBorder, fontSize: 15 },
  toggleBtn: { flex: 1, paddingVertical: 10, borderRadius: 8, alignItems: 'center', backgroundColor: colors.surface, borderWidth: 1, borderColor: colors.cardBorder },
  buyActive: { backgroundColor: colors.green, borderColor: colors.green },
  sellActive: { backgroundColor: colors.red, borderColor: colors.red },
  toggleText: { color: colors.textSecondary, fontWeight: '600' },
  toggleTextActive: { color: colors.white },
  chipRow: { flexDirection: 'row', gap: 6, marginBottom: 12, flexWrap: 'wrap' },
  chip: { paddingHorizontal: 12, paddingVertical: 6, borderRadius: 6, backgroundColor: colors.surface },
  chipActive: { backgroundColor: colors.blue },
  chipText: { color: colors.textSecondary, fontSize: 13, fontWeight: '500' },
  chipTextActive: { color: colors.white },
  switchRow: { flexDirection: 'row', justifyContent: 'space-between', alignItems: 'center', marginBottom: 12 },
  previewBox: { backgroundColor: colors.surface, borderRadius: 8, padding: 10, marginBottom: 12 },
  previewText: { color: colors.yellow, fontSize: 12, textAlign: 'center' },
  modalBtn: { flex: 1, paddingVertical: 12, borderRadius: 10, alignItems: 'center' },
  cancelBtn: { backgroundColor: colors.surface, borderWidth: 1, borderColor: colors.cardBorder },
  cancelBtnText: { color: colors.textSecondary, fontWeight: '600', fontSize: 15 },
  confirmBtn: { backgroundColor: colors.green },
  confirmBtnText: { color: colors.white, fontWeight: '600', fontSize: 15 },
});
