import React, { useState, useEffect, useCallback } from 'react';
import {
  View, Text, TextInput, TouchableOpacity,
  StyleSheet, Alert,
} from 'react-native';
import api from '../services/api';
import { colors, spacing, radius, fontSize } from '../services/theme';

export default function ScalpPanel({ symbol }) {
  const [config, setConfig] = useState({
    leverage: '10',
    amountPerOrder: '5',
    maxLossPerTrade: '1',
    maxDailyLoss: '0',
    cooldownSec: '60',
  });
  const [status, setStatus] = useState(null);
  const [loading, setLoading] = useState(false);

  const fetchStatus = useCallback(async () => {
    if (!symbol) return;
    try {
      const resp = await api.scalpStatus(symbol);
      setStatus(resp.data || null);
    } catch (_) {}
  }, [symbol]);

  useEffect(() => {
    fetchStatus();
    const timer = setInterval(fetchStatus, 5000);
    return () => clearInterval(timer);
  }, [fetchStatus]);

  const handleStart = async () => {
    if (!symbol) return Alert.alert('提示', '请选择交易对');
    setLoading(true);
    try {
      await api.startScalp({
        symbol,
        leverage: parseInt(config.leverage, 10) || 10,
        amountPerOrder: config.amountPerOrder || '5',
        maxLossPerTrade: parseFloat(config.maxLossPerTrade) || 0,
        maxDailyLoss: parseFloat(config.maxDailyLoss) || 0,
        cooldownSec: parseInt(config.cooldownSec, 10) || 60,
      });
      Alert.alert('已启动', `Scalp 策略已启动 ${symbol}`);
      fetchStatus();
    } catch (e) {
      Alert.alert('启动失败', e.message);
    } finally {
      setLoading(false);
    }
  };

  const handleStop = async () => {
    Alert.alert('停止策略', `确定停止 ${symbol} Scalp 策略？`, [
      { text: '取消', style: 'cancel' },
      {
        text: '确定',
        style: 'destructive',
        onPress: async () => {
          setLoading(true);
          try {
            await api.stopScalp(symbol);
            Alert.alert('已停止', 'Scalp 策略已停止');
            fetchStatus();
          } catch (e) {
            Alert.alert('停止失败', e.message);
          } finally {
            setLoading(false);
          }
        },
      },
    ]);
  };

  const isActive = status?.active;

  const dirColor = status?.direction === 'LONG' ? colors.greenLight
    : status?.direction === 'SHORT' ? colors.redLight
    : colors.textMuted;

  const signalColor = status?.signal === 'BUY' ? colors.greenLight
    : status?.signal === 'SELL' ? colors.redLight
    : colors.textMuted;

  return (
    <View style={styles.panel}>
      <View style={styles.header}>
        <Text style={styles.title}>Scalp 策略</Text>
        {isActive && (
          <View style={styles.activeBadge}>
            <View style={styles.activeDot} />
            <Text style={styles.activeText}>运行中</Text>
          </View>
        )}
      </View>

      <Text style={styles.desc}>
        每分钟分析 EMA交叉+RSI+量能，自动判断买卖方向
      </Text>

      {/* 配置区 */}
      {!isActive && (
        <View style={styles.configBox}>
          <View style={styles.configRow}>
            <View style={styles.configField}>
              <Text style={styles.configLabel}>金额(U)</Text>
              <TextInput
                style={styles.configInput}
                value={config.amountPerOrder}
                onChangeText={(v) => setConfig({ ...config, amountPerOrder: v })}
                keyboardType="decimal-pad"
                placeholderTextColor={colors.textMuted}
              />
            </View>
            <View style={styles.configField}>
              <Text style={styles.configLabel}>杠杆</Text>
              <TextInput
                style={styles.configInput}
                value={config.leverage}
                onChangeText={(v) => setConfig({ ...config, leverage: v })}
                keyboardType="number-pad"
                placeholderTextColor={colors.textMuted}
              />
            </View>
          </View>
          <View style={styles.configRow}>
            <View style={styles.configField}>
              <Text style={styles.configLabel}>单笔止损(U)</Text>
              <TextInput
                style={styles.configInput}
                value={config.maxLossPerTrade}
                onChangeText={(v) => setConfig({ ...config, maxLossPerTrade: v })}
                keyboardType="decimal-pad"
                placeholderTextColor={colors.textMuted}
              />
            </View>
            <View style={styles.configField}>
              <Text style={styles.configLabel}>日止损(U,0=10%)</Text>
              <TextInput
                style={styles.configInput}
                value={config.maxDailyLoss}
                onChangeText={(v) => setConfig({ ...config, maxDailyLoss: v })}
                keyboardType="decimal-pad"
                placeholderTextColor={colors.textMuted}
              />
            </View>
          </View>
          <View style={styles.configRow}>
            <View style={styles.configField}>
              <Text style={styles.configLabel}>冷却(秒)</Text>
              <TextInput
                style={styles.configInput}
                value={config.cooldownSec}
                onChangeText={(v) => setConfig({ ...config, cooldownSec: v })}
                keyboardType="number-pad"
                placeholderTextColor={colors.textMuted}
              />
            </View>
            <View style={styles.configField} />
          </View>
        </View>
      )}

      {/* 状态展示 */}
      {isActive && status && (
        <View style={styles.statusBox}>
          {/* 方向 + 信号 */}
          <View style={styles.statusTopRow}>
            <View style={styles.statusBig}>
              <Text style={styles.statusBigLabel}>持仓</Text>
              <Text style={[styles.statusBigValue, { color: dirColor }]}>
                {status.direction}
              </Text>
            </View>
            <View style={styles.statusBig}>
              <Text style={styles.statusBigLabel}>信号</Text>
              <Text style={[styles.statusBigValue, { color: signalColor }]}>
                {status.signal}
              </Text>
            </View>
          </View>

          {/* 信号原因 */}
          {status.signalReason ? (
            <View style={styles.reasonBox}>
              <Text style={styles.reasonLabel}>信号原因</Text>
              <Text style={[styles.reasonText, { color: signalColor }]}>{status.signalReason}</Text>
            </View>
          ) : null}

          {/* 开仓/平仓原因 */}
          {status.openReason ? (
            <View style={styles.reasonBox}>
              <Text style={styles.reasonLabel}>开仓原因</Text>
              <Text style={[styles.reasonText, { color: colors.gold }]}>{status.openReason}</Text>
            </View>
          ) : null}
          {status.closeReason ? (
            <View style={styles.reasonBox}>
              <Text style={styles.reasonLabel}>平仓原因</Text>
              <Text style={[styles.reasonText, {
                color: status.closeReason.startsWith('止盈') ? colors.greenLight : colors.redLight,
              }]}>{status.closeReason}</Text>
            </View>
          ) : null}

          {/* 指标 */}
          <View style={styles.indicatorRow}>
            <View style={styles.indicatorItem}>
              <Text style={styles.indicatorLabel}>EMA快</Text>
              <Text style={styles.indicatorValue}>{status.emaFast}</Text>
            </View>
            <View style={styles.indicatorItem}>
              <Text style={styles.indicatorLabel}>EMA慢</Text>
              <Text style={styles.indicatorValue}>{status.emaSlow}</Text>
            </View>
            <View style={styles.indicatorItem}>
              <Text style={styles.indicatorLabel}>趋势</Text>
              <Text style={styles.indicatorValue}>{status.emaTrend}</Text>
            </View>
          </View>
          <View style={styles.indicatorRow}>
            <View style={styles.indicatorItem}>
              <Text style={styles.indicatorLabel}>RSI</Text>
              <Text style={[styles.indicatorValue, {
                color: status.rsi > 70 ? colors.redLight : status.rsi < 30 ? colors.greenLight : colors.text,
              }]}>{status.rsi}</Text>
            </View>
            <View style={styles.indicatorItem}>
              <Text style={styles.indicatorLabel}>量比</Text>
              <Text style={[styles.indicatorValue, {
                color: status.volRatio >= 1.2 ? colors.gold : colors.text,
              }]}>{status.volRatio}x</Text>
            </View>
            <View style={styles.indicatorItem}>
              <Text style={styles.indicatorLabel}>信号时间</Text>
              <Text style={styles.indicatorValue}>{status.signalTime || '-'}</Text>
            </View>
          </View>

          {/* 统计 */}
          <View style={styles.statsGrid}>
            <View style={styles.statsCell}>
              <Text style={styles.statsLabel}>今日交易</Text>
              <Text style={styles.statsValue}>{status.dailyTrades}</Text>
            </View>
            <View style={styles.statsCell}>
              <Text style={styles.statsLabel}>今日盈亏</Text>
              <Text style={[styles.statsValue, {
                color: status.dailyPnl >= 0 ? colors.greenLight : colors.redLight,
              }]}>
                {status.dailyPnl >= 0 ? '+' : ''}{status.dailyPnl} U
              </Text>
            </View>
            <View style={styles.statsCell}>
              <Text style={styles.statsLabel}>总交易</Text>
              <Text style={styles.statsValue}>{status.totalTrades}</Text>
            </View>
            <View style={styles.statsCell}>
              <Text style={styles.statsLabel}>总盈亏</Text>
              <Text style={[styles.statsValue, {
                color: status.totalPnl >= 0 ? colors.greenLight : colors.redLight,
              }]}>
                {status.totalPnl >= 0 ? '+' : ''}{status.totalPnl} U
              </Text>
            </View>
            <View style={styles.statsCell}>
              <Text style={styles.statsLabel}>胜率</Text>
              <Text style={styles.statsValue}>
                {status.winCount + status.lossCount > 0
                  ? Math.round(status.winCount / (status.winCount + status.lossCount) * 100) + '%'
                  : '-'}
              </Text>
            </View>
            <View style={styles.statsCell}>
              <Text style={styles.statsLabel}>胜/负</Text>
              <Text style={styles.statsValue}>
                <Text style={{ color: colors.greenLight }}>{status.winCount}</Text>
                {' / '}
                <Text style={{ color: colors.redLight }}>{status.lossCount}</Text>
              </Text>
            </View>
          </View>

          {status.lastError ? (
            <Text style={styles.errorText}>{status.lastError}</Text>
          ) : null}
          {status.lastCheckAt ? (
            <Text style={styles.checkTime}>最近检查: {status.lastCheckAt}</Text>
          ) : null}
        </View>
      )}

      {/* 操作按钮 */}
      <TouchableOpacity
        style={[styles.actionBtn, isActive ? styles.stopBtn : styles.startBtn]}
        onPress={isActive ? handleStop : handleStart}
        disabled={loading}
        activeOpacity={0.8}
      >
        <Text style={[styles.actionBtnText, isActive ? styles.stopBtnText : styles.startBtnText]}>
          {isActive ? '停止策略' : '启动 Scalp'}
        </Text>
      </TouchableOpacity>
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
    marginBottom: spacing.xs,
  },
  title: {
    fontSize: fontSize.lg,
    fontWeight: '700',
    color: colors.white,
  },
  activeBadge: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: spacing.xs,
    backgroundColor: colors.greenBg,
    paddingHorizontal: spacing.sm,
    paddingVertical: 3,
    borderRadius: radius.pill,
  },
  activeDot: {
    width: 6,
    height: 6,
    borderRadius: 3,
    backgroundColor: colors.green,
  },
  activeText: {
    color: colors.greenLight,
    fontSize: fontSize.xs,
    fontWeight: '600',
  },
  desc: {
    color: colors.textMuted,
    fontSize: fontSize.xs,
    marginBottom: spacing.md,
  },

  // 配置
  configBox: {
    marginBottom: spacing.md,
  },
  configRow: {
    flexDirection: 'row',
    gap: spacing.sm,
    marginBottom: spacing.sm,
  },
  configField: {
    flex: 1,
  },
  configLabel: {
    color: colors.textMuted,
    fontSize: fontSize.xs,
    marginBottom: 3,
  },
  configInput: {
    backgroundColor: colors.surface,
    borderRadius: radius.sm,
    padding: spacing.sm,
    color: colors.white,
    fontSize: fontSize.md,
    borderWidth: 1,
    borderColor: colors.cardBorder,
    textAlign: 'center',
  },

  // 状态
  statusBox: {
    marginBottom: spacing.md,
  },
  statusTopRow: {
    flexDirection: 'row',
    gap: spacing.md,
    marginBottom: spacing.md,
  },
  statusBig: {
    flex: 1,
    backgroundColor: colors.surface,
    borderRadius: radius.md,
    padding: spacing.md,
    alignItems: 'center',
  },
  statusBigLabel: {
    color: colors.textMuted,
    fontSize: fontSize.xs,
    marginBottom: 2,
  },
  statusBigValue: {
    fontSize: fontSize.xl,
    fontWeight: '800',
  },

  indicatorRow: {
    flexDirection: 'row',
    gap: spacing.xs,
    marginBottom: spacing.xs,
  },
  indicatorItem: {
    flex: 1,
    backgroundColor: colors.surface,
    borderRadius: radius.sm,
    padding: spacing.sm,
    alignItems: 'center',
  },
  indicatorLabel: {
    color: colors.textMuted,
    fontSize: 10,
    marginBottom: 1,
  },
  indicatorValue: {
    color: colors.text,
    fontSize: fontSize.sm,
    fontWeight: '600',
    fontVariant: ['tabular-nums'],
  },

  statsGrid: {
    flexDirection: 'row',
    flexWrap: 'wrap',
    gap: spacing.xs,
    marginTop: spacing.sm,
  },
  statsCell: {
    width: '31%',
    backgroundColor: colors.surface,
    borderRadius: radius.sm,
    padding: spacing.sm,
    alignItems: 'center',
  },
  statsLabel: {
    color: colors.textMuted,
    fontSize: 10,
    marginBottom: 1,
  },
  statsValue: {
    color: colors.text,
    fontSize: fontSize.sm,
    fontWeight: '700',
    fontVariant: ['tabular-nums'],
  },

  reasonBox: {
    backgroundColor: colors.surface,
    borderRadius: radius.sm,
    padding: spacing.sm,
    marginBottom: spacing.xs,
  },
  reasonLabel: {
    color: colors.textMuted,
    fontSize: 10,
    marginBottom: 2,
  },
  reasonText: {
    fontSize: fontSize.xs,
    fontWeight: '600',
    lineHeight: 16,
  },
  errorText: {
    color: colors.redLight,
    fontSize: fontSize.xs,
    marginTop: spacing.sm,
    backgroundColor: colors.redBg,
    borderRadius: radius.sm,
    padding: spacing.sm,
  },
  checkTime: {
    color: colors.textMuted,
    fontSize: fontSize.xs,
    marginTop: spacing.xs,
    textAlign: 'right',
  },

  // 按钮
  actionBtn: {
    paddingVertical: spacing.md,
    borderRadius: radius.md,
    alignItems: 'center',
  },
  startBtn: {
    backgroundColor: colors.gold,
  },
  stopBtn: {
    backgroundColor: colors.red,
  },
  startBtnText: {
    color: colors.bg,
    fontWeight: '700',
    fontSize: fontSize.md,
  },
  stopBtnText: {
    color: colors.white,
    fontWeight: '700',
    fontSize: fontSize.md,
  },
});
