import React, { useState, useEffect, useRef, useCallback, useMemo } from 'react';
import {
  View, Text, StyleSheet, TouchableOpacity, TextInput, Alert,
} from 'react-native';
import { colors, spacing, radius, fontSize } from '../services/theme';
import api from '../services/api';

const DEFAULT_CHAIN_IDS = '1';
const DEFAULT_SYMBOLS = '';
const DEFAULT_INTERVAL = '60';
const DEFAULT_MAX_RESERVES = '200';
const DEFAULT_LP_THRESHOLD = '5';
const DEFAULT_ALERT_COOLDOWN = '600';
const AUTO_START_PAYLOAD = {
  lpFeeRateOnly: true,
  intervalSec: 60,
  maxReserves: 200,
  lpFeeRateThresholdPct: 5,
  alertCooldownSec: 600,
};

function parsePositiveInt(input, fallback) {
  const n = parseInt(String(input || '').trim(), 10);
  if (!Number.isFinite(n) || n <= 0) return fallback;
  return n;
}

function parsePositiveNumber(input, fallback) {
  const n = Number(String(input || '').trim());
  if (!Number.isFinite(n) || n <= 0) return fallback;
  return n;
}

function parseChainIds(input) {
  const seen = new Set();
  return String(input || '')
    .split(',')
    .map((s) => parseInt(s.trim(), 10))
    .filter((n) => Number.isFinite(n) && n > 0)
    .filter((n) => {
      if (seen.has(n)) return false;
      seen.add(n);
      return true;
    });
}

function parseSymbols(input) {
  const seen = new Set();
  return String(input || '')
    .split(',')
    .map((s) => s.trim().toUpperCase())
    .filter(Boolean)
    .filter((s) => {
      if (seen.has(s)) return false;
      seen.add(s);
      return true;
    });
}

function truncateAddress(addr, left = 6, right = 4) {
  const v = String(addr || '');
  if (v.length <= left + right + 1) return v;
  return `${v.slice(0, left)}...${v.slice(-right)}`;
}

function fmtPct(v) {
  const n = Number(v || 0);
  return `${n.toFixed(2)}%`;
}

function fmtNum(v, digits = 2) {
  const n = Number(v || 0);
  if (!Number.isFinite(n)) return '--';
  return n.toFixed(digits);
}

export default function AaveMonitorPanel() {
  const [status, setStatus] = useState(null);
  const [loading, setLoading] = useState(false);

  const [chainIds, setChainIds] = useState(DEFAULT_CHAIN_IDS);
  const [symbols, setSymbols] = useState(DEFAULT_SYMBOLS);
  const [intervalSec, setIntervalSec] = useState(DEFAULT_INTERVAL);
  const [maxReserves, setMaxReserves] = useState(DEFAULT_MAX_RESERVES);
  const [lpThreshold, setLpThreshold] = useState(DEFAULT_LP_THRESHOLD);
  const [alertCooldownSec, setAlertCooldownSec] = useState(DEFAULT_ALERT_COOLDOWN);
  const [showAdvanced, setShowAdvanced] = useState(false);

  const timerRef = useRef(null);
  const autoStartOnceRef = useRef(false);

  const fetchStatus = useCallback(async () => {
    try {
      const res = await api.aaveMonitorStatus();
      if (res?.data) setStatus(res.data);
    } catch (_) {}
  }, []);

  useEffect(() => {
    let cancelled = false;

    const bootstrap = async () => {
      try {
        const res = await api.aaveMonitorStatus();
        const snap = res?.data || null;
        if (!cancelled && snap) setStatus(snap);
        if (snap?.active || autoStartOnceRef.current) return;

        autoStartOnceRef.current = true;
        await api.startAaveMonitor(AUTO_START_PAYLOAD);
      } catch (_) {}

      if (!cancelled) {
        fetchStatus();
      }
    };

    bootstrap();
    timerRef.current = setInterval(fetchStatus, 10000);
    return () => {
      cancelled = true;
      if (timerRef.current) clearInterval(timerRef.current);
    };
  }, [fetchStatus]);

  const isActive = !!status?.active;
  const reserves = status?.reserves || [];
  const alerts = status?.alerts || [];
  const configThreshold = Number(status?.config?.lpFeeRateThresholdPct || lpThreshold || 5);

  const topReserves = useMemo(() => reserves.slice(0, 12), [reserves]);
  const topAlerts = useMemo(() => alerts.slice(0, 12), [alerts]);

  const handleStart = async () => {
    setLoading(true);
    try {
      const parsedChainIDs = parseChainIds(chainIds);
      const parsedSymbols = parseSymbols(symbols);
      const payload = {
        intervalSec: parsePositiveInt(intervalSec, 60),
        maxReserves: parsePositiveInt(maxReserves, 200),
        lpFeeRateOnly: true,
        lpFeeRateThresholdPct: parsePositiveNumber(lpThreshold, 5),
        alertCooldownSec: parsePositiveInt(alertCooldownSec, 600),
      };
      if (parsedChainIDs.length > 0) payload.chainIds = parsedChainIDs;
      if (parsedSymbols.length > 0) payload.symbols = parsedSymbols;

      await api.startAaveMonitor(payload);
      Alert.alert('成功', 'LP费率监控已启动');
      fetchStatus();
    } catch (e) {
      Alert.alert('启动失败', e.message || '未知错误');
    } finally {
      setLoading(false);
    }
  };

  const handleStop = async () => {
    setLoading(true);
    try {
      await api.stopAaveMonitor();
      Alert.alert('成功', 'LP费率监控已停止');
      fetchStatus();
    } catch (e) {
      Alert.alert('停止失败', e.message || '未知错误');
    } finally {
      setLoading(false);
    }
  };

  return (
    <View style={styles.card}>
      <View style={styles.header}>
        <View style={styles.headerLeft}>
          <Text style={styles.title}>LP费率监控</Text>
          <View style={[styles.statusDot, { backgroundColor: isActive ? colors.green : colors.textMuted }]} />
        </View>
        <TouchableOpacity
          style={[styles.actionBtn, isActive ? styles.stopBtn : styles.startBtn]}
          onPress={isActive ? handleStop : handleStart}
          disabled={loading}
        >
          <Text style={[styles.actionBtnText, isActive ? styles.stopBtnText : styles.startBtnText]}>
            {loading ? '处理中...' : isActive ? '停止' : '启动'}
          </Text>
        </TouchableOpacity>
      </View>

      {!isActive && (
        <>
          <Text style={styles.quickHint}>默认监控所有代币，直接点“启动”即可。</Text>
          <TouchableOpacity
            style={styles.advancedToggle}
            onPress={() => setShowAdvanced((v) => !v)}
          >
            <Text style={styles.advancedToggleText}>
              {showAdvanced ? '收起高级设置' : '展开高级设置'}
            </Text>
          </TouchableOpacity>

          {showAdvanced && (
            <View style={styles.configArea}>
              <View style={styles.configRow}>
                <View style={styles.configItem}>
                  <Text style={styles.configLabel}>链ID(逗号分隔)</Text>
                  <TextInput
                    style={styles.configInput}
                    value={chainIds}
                    onChangeText={setChainIds}
                    placeholder="1,42161,137"
                    placeholderTextColor={colors.textMuted}
                  />
                </View>
                <View style={styles.configItem}>
                  <Text style={styles.configLabel}>资产符号(可选)</Text>
                  <TextInput
                    style={styles.configInput}
                    value={symbols}
                    onChangeText={setSymbols}
                    placeholder="留空=所有代币"
                    placeholderTextColor={colors.textMuted}
                  />
                </View>
              </View>

              <View style={styles.configRow}>
                <View style={styles.configItem}>
                  <Text style={styles.configLabel}>轮询间隔(s)</Text>
                  <TextInput
                    style={styles.configInput}
                    value={intervalSec}
                    onChangeText={setIntervalSec}
                    keyboardType="numeric"
                    placeholderTextColor={colors.textMuted}
                  />
                </View>
                <View style={styles.configItem}>
                  <Text style={styles.configLabel}>最大资产数</Text>
                  <TextInput
                    style={styles.configInput}
                    value={maxReserves}
                    onChangeText={setMaxReserves}
                    keyboardType="numeric"
                    placeholderTextColor={colors.textMuted}
                  />
                </View>
              </View>

              <View style={styles.configRow}>
                <View style={styles.configItem}>
                  <Text style={styles.configLabel}>LP费率阈值%</Text>
                  <TextInput
                    style={styles.configInput}
                    value={lpThreshold}
                    onChangeText={setLpThreshold}
                    keyboardType="numeric"
                    placeholderTextColor={colors.textMuted}
                  />
                </View>
                <View style={styles.configItem}>
                  <Text style={styles.configLabel}>告警冷却(s)</Text>
                  <TextInput
                    style={styles.configInput}
                    value={alertCooldownSec}
                    onChangeText={setAlertCooldownSec}
                    keyboardType="numeric"
                    placeholderTextColor={colors.textMuted}
                  />
                </View>
              </View>
            </View>
          )}
        </>
      )}

      <View style={styles.summaryGrid}>
        <View style={styles.summaryItem}>
          <Text style={styles.summaryLabel}>资产</Text>
          <Text style={styles.summaryValue}>{status?.reserveCount ?? 0}</Text>
        </View>
        <View style={styles.summaryItem}>
          <Text style={styles.summaryLabel}>阈值</Text>
          <Text style={styles.summaryValue}>{fmtNum(configThreshold, 2)}%</Text>
        </View>
        <View style={styles.summaryItem}>
          <Text style={styles.summaryLabel}>告警</Text>
          <Text style={[styles.summaryValue, { color: alerts.length > 0 ? colors.orange : colors.text }]}>
            {alerts.length}
          </Text>
        </View>
        <View style={styles.summaryItem}>
          <Text style={styles.summaryLabel}>耗时(ms)</Text>
          <Text style={styles.summaryValue}>{status?.lastRoundDurationMs ?? 0}</Text>
        </View>
      </View>

      {!!status?.lastError && (
        <View style={styles.errorBox}>
          <Text style={styles.errorTitle}>最近错误</Text>
          <Text style={styles.errorText}>{status.lastError}</Text>
          <Text style={styles.errorMeta}>连续失败: {status?.consecutiveErrors ?? 0}</Text>
        </View>
      )}

      {topAlerts.length > 0 && (
        <View style={styles.section}>
          <Text style={styles.sectionLabel}>最新告警</Text>
          {topAlerts.map((a, idx) => (
            <View key={`${a.key || a.type || 'a'}-${idx}`} style={styles.row}>
              <View style={styles.rowLeft}>
                <Text style={styles.rowTitle}>{a.symbol || a.type}</Text>
                <Text style={styles.rowSub}>{a.message}</Text>
              </View>
              <View style={styles.rowRight}>
                <Text style={[
                  styles.badge,
                  {
                    color: a.level === 'critical' ? colors.redLight : colors.orange,
                    backgroundColor: a.level === 'critical' ? colors.redBg : colors.orangeBg,
                  },
                ]}
                >
                  {a.level}
                </Text>
                <Text style={styles.rowHint}>{a.time || '--'}</Text>
              </View>
            </View>
          ))}
        </View>
      )}

      {topReserves.length > 0 && (
        <View style={styles.section}>
          <Text style={styles.sectionLabel}>LP费率 Top</Text>
          {topReserves.map((r) => {
            const isHigh = Number(r.supplyApyPct || 0) >= configThreshold;
            return (
              <View key={`${r.chainId}:${r.market}:${r.symbol}`} style={styles.row}>
                <View style={styles.rowLeft}>
                  <Text style={styles.rowTitle}>{r.symbol} · {r.chainName}</Text>
                  <Text style={styles.rowSub}>{r.marketName}</Text>
                  <Text style={styles.rowSub}>{truncateAddress(r.market)}</Text>
                </View>
                <View style={styles.rowRight}>
                  <Text style={[styles.rowValue, { color: isHigh ? colors.orange : colors.text }]}>
                    {fmtPct(r.supplyApyPct)}
                  </Text>
                  <Text style={styles.rowHint}>LP费率</Text>
                </View>
              </View>
            );
          })}
        </View>
      )}

      {status?.updateTime && (
        <Text style={styles.updateTime}>更新: {status.updateTime}</Text>
      )}
    </View>
  );
}

const styles = StyleSheet.create({
  card: {
    backgroundColor: colors.card,
    borderRadius: radius.lg,
    borderWidth: 1,
    borderColor: colors.cardBorder,
    padding: spacing.lg,
    marginBottom: spacing.lg,
  },
  header: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'center',
    marginBottom: spacing.md,
  },
  headerLeft: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: spacing.sm,
  },
  title: {
    fontSize: fontSize.lg,
    fontWeight: '700',
    color: colors.white,
  },
  statusDot: {
    width: 8,
    height: 8,
    borderRadius: 4,
  },
  actionBtn: {
    paddingHorizontal: spacing.lg,
    paddingVertical: spacing.sm,
    borderRadius: radius.md,
    borderWidth: 1,
  },
  startBtn: {
    backgroundColor: colors.blueBg,
    borderColor: colors.blue,
  },
  stopBtn: {
    backgroundColor: colors.redBg,
    borderColor: colors.red,
  },
  actionBtnText: {
    fontSize: fontSize.sm,
    fontWeight: '700',
  },
  startBtnText: {
    color: colors.blueLight,
  },
  stopBtnText: {
    color: colors.redLight,
  },
  configArea: {
    gap: spacing.sm,
    marginBottom: spacing.md,
    marginTop: spacing.sm,
  },
  quickHint: {
    fontSize: fontSize.sm,
    color: colors.textSecondary,
    marginBottom: spacing.xs,
  },
  advancedToggle: {
    alignSelf: 'flex-start',
    borderWidth: 1,
    borderColor: colors.cardBorder,
    backgroundColor: colors.surface,
    borderRadius: radius.sm,
    paddingHorizontal: spacing.sm,
    paddingVertical: spacing.xs,
    marginBottom: spacing.xs,
  },
  advancedToggleText: {
    fontSize: fontSize.xs,
    color: colors.textSecondary,
    fontWeight: '700',
  },
  configRow: {
    flexDirection: 'row',
    gap: spacing.sm,
  },
  configItem: {
    flex: 1,
  },
  configLabel: {
    fontSize: fontSize.xs,
    color: colors.textMuted,
    marginBottom: spacing.xs,
  },
  configInput: {
    backgroundColor: colors.surface,
    borderWidth: 1,
    borderColor: colors.cardBorder,
    borderRadius: radius.sm,
    color: colors.text,
    fontSize: fontSize.sm,
    paddingHorizontal: spacing.sm,
    paddingVertical: spacing.xs,
  },
  summaryGrid: {
    flexDirection: 'row',
    flexWrap: 'wrap',
    gap: spacing.sm,
    marginBottom: spacing.sm,
  },
  summaryItem: {
    flexGrow: 1,
    minWidth: 76,
    backgroundColor: colors.surface,
    borderRadius: radius.sm,
    borderWidth: 1,
    borderColor: colors.cardBorder,
    paddingVertical: spacing.sm,
    paddingHorizontal: spacing.sm,
  },
  summaryLabel: {
    fontSize: fontSize.xs,
    color: colors.textMuted,
  },
  summaryValue: {
    marginTop: spacing.xs,
    fontSize: fontSize.md,
    fontWeight: '700',
    color: colors.text,
  },
  errorBox: {
    backgroundColor: colors.redBg,
    borderWidth: 1,
    borderColor: 'rgba(246,70,93,0.35)',
    borderRadius: radius.md,
    padding: spacing.sm,
    marginBottom: spacing.sm,
  },
  errorTitle: {
    fontSize: fontSize.sm,
    fontWeight: '700',
    color: colors.redLight,
    marginBottom: spacing.xs,
  },
  errorText: {
    fontSize: fontSize.sm,
    color: colors.text,
  },
  errorMeta: {
    marginTop: spacing.xs,
    fontSize: fontSize.xs,
    color: colors.textMuted,
  },
  section: {
    marginTop: spacing.sm,
  },
  sectionLabel: {
    fontSize: fontSize.sm,
    fontWeight: '700',
    color: colors.textSecondary,
    marginBottom: spacing.xs,
  },
  row: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'center',
    borderWidth: 1,
    borderColor: colors.cardBorder,
    backgroundColor: colors.surface,
    borderRadius: radius.sm,
    paddingHorizontal: spacing.sm,
    paddingVertical: spacing.sm,
    marginTop: spacing.xs,
  },
  rowLeft: {
    flex: 1,
    paddingRight: spacing.sm,
  },
  rowRight: {
    alignItems: 'flex-end',
    justifyContent: 'center',
    minWidth: 88,
  },
  rowTitle: {
    fontSize: fontSize.sm,
    fontWeight: '700',
    color: colors.text,
  },
  rowSub: {
    marginTop: 2,
    fontSize: fontSize.xs,
    color: colors.textMuted,
  },
  rowValue: {
    fontSize: fontSize.sm,
    fontWeight: '700',
    color: colors.text,
  },
  rowHint: {
    marginTop: 2,
    fontSize: fontSize.xs,
    color: colors.textMuted,
  },
  badge: {
    fontSize: fontSize.xs,
    fontWeight: '700',
    paddingHorizontal: spacing.xs,
    paddingVertical: 2,
    borderRadius: radius.xs,
    overflow: 'hidden',
  },
  updateTime: {
    marginTop: spacing.sm,
    fontSize: fontSize.xs,
    color: colors.textMuted,
    textAlign: 'right',
  },
});
