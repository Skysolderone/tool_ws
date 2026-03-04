import React, { useCallback, useMemo, useState } from 'react';
import { ScrollView, StyleSheet, Text, TextInput, TouchableOpacity, View } from 'react-native';
import { colors, fontSize, radius, spacing } from '../services/theme';

const SOURCE_OPTIONS = [
  { key: 'all', label: '全部' },
  { key: 'hyper', label: 'Hyper' },
  { key: 'liquidation', label: '强平' },
  { key: 'market', label: '市场' },
];

const SEVERITY_OPTIONS = [
  { key: 'all', label: '全部级别' },
  { key: 'critical', label: '危险' },
  { key: 'warn', label: '警告' },
  { key: 'info', label: '提示' },
];

const DEFAULT_NOTIFY_CONFIG = Object.freeze({
  warnPopup: false,
  warnVibrate: false,
  criticalPopup: true,
  criticalVibrate: true,
});

const NOTIFY_TOGGLE_OPTIONS = [
  { key: 'warnPopup', label: 'Warn弹窗' },
  { key: 'warnVibrate', label: 'Warn震动' },
  { key: 'criticalPopup', label: 'Critical弹窗' },
  { key: 'criticalVibrate', label: 'Critical震动' },
];

function toNum(value) {
  const n = Number(value);
  return Number.isFinite(n) ? n : 0;
}

function fmtTime(ms) {
  const t = toNum(ms);
  if (t <= 0) return '--:--:--';
  return new Date(t).toLocaleTimeString('zh-CN', { hour12: false });
}

function severityColor(severity) {
  if (severity === 'critical') return colors.redLight;
  if (severity === 'warn') return colors.goldLight;
  return colors.blueLight;
}

function sourceLabel(source) {
  if (source === 'hyper') return 'HYPER';
  if (source === 'liquidation') return 'LIQ';
  if (source === 'market') return 'MKT';
  return String(source || 'N/A').toUpperCase();
}

function eventMeta(evt = {}) {
  const payload = evt.payload && typeof evt.payload === 'object' ? evt.payload : {};
  const parts = [];
  if (payload.direction) parts.push(String(payload.direction));
  if (payload.windowSec) parts.push(`窗口 ${toNum(payload.windowSec)}s`);
  const triggerPct = toNum(payload.triggerPct || payload.thresholdPct);
  if (triggerPct > 0 && evt.type === 'market_spike') {
    parts.push(`${payload.dynamic ? '动态' : '静态'}阈值 ${triggerPct.toFixed(2)}%`);
  }
  const suppressSec = toNum(payload.suppressSec);
  if (suppressSec > 0) parts.push(`抑制 ${suppressSec}s`);
  return parts.join(' | ');
}

export default function MonitorEventTimelinePanel({ events = [], onClear, notifyConfig, onNotifyConfigChange }) {
  const [sourceFilter, setSourceFilter] = useState('all');
  const [severityFilter, setSeverityFilter] = useState('all');
  const [symbolInput, setSymbolInput] = useState('');
  const effectiveNotify = {
    ...DEFAULT_NOTIFY_CONFIG,
    ...(notifyConfig || {}),
  };

  const symbolFilter = (symbolInput || '').trim().toUpperCase();

  const toggleNotify = useCallback((key) => {
    if (!onNotifyConfigChange) return;
    onNotifyConfigChange((prev) => {
      const current = {
        ...DEFAULT_NOTIFY_CONFIG,
        ...(prev || {}),
      };
      return {
        ...current,
        [key]: !current[key],
      };
    });
  }, [onNotifyConfigChange]);

  const filtered = useMemo(() => {
    return (events || []).filter((evt) => {
      if (sourceFilter !== 'all' && evt.source !== sourceFilter) return false;
      if (severityFilter !== 'all' && evt.severity !== severityFilter) return false;
      if (symbolFilter && String(evt.symbol || '').toUpperCase() !== symbolFilter) return false;
      return true;
    });
  }, [events, sourceFilter, severityFilter, symbolFilter]);

  return (
    <View style={styles.card}>
      <View style={styles.header}>
        <Text style={styles.title}>统一事件时间线</Text>
        <View style={styles.headerRight}>
          <Text style={styles.countText}>{filtered.length}/{events.length}</Text>
          <TouchableOpacity style={styles.clearBtn} onPress={onClear}>
            <Text style={styles.clearText}>清空</Text>
          </TouchableOpacity>
        </View>
      </View>

      <View style={styles.filterRow}>
        {SOURCE_OPTIONS.map((opt) => (
          <TouchableOpacity
            key={opt.key}
            style={[styles.chip, sourceFilter === opt.key && styles.chipActive]}
            onPress={() => setSourceFilter(opt.key)}
          >
            <Text style={[styles.chipText, sourceFilter === opt.key && styles.chipTextActive]}>{opt.label}</Text>
          </TouchableOpacity>
        ))}
      </View>

      <View style={styles.filterRow}>
        {SEVERITY_OPTIONS.map((opt) => (
          <TouchableOpacity
            key={opt.key}
            style={[styles.chip, severityFilter === opt.key && styles.chipActive]}
            onPress={() => setSeverityFilter(opt.key)}
          >
            <Text style={[styles.chipText, severityFilter === opt.key && styles.chipTextActive]}>{opt.label}</Text>
          </TouchableOpacity>
        ))}
      </View>

      {onNotifyConfigChange ? (
        <View style={styles.notifyWrap}>
          <Text style={styles.notifyTitle}>通知路由</Text>
          <View style={styles.filterRow}>
            {NOTIFY_TOGGLE_OPTIONS.map((opt) => {
              const active = !!effectiveNotify[opt.key];
              return (
                <TouchableOpacity
                  key={opt.key}
                  style={[styles.chip, active && styles.chipActive]}
                  onPress={() => toggleNotify(opt.key)}
                >
                  <Text style={[styles.chipText, active && styles.chipTextActive]}>{opt.label}</Text>
                </TouchableOpacity>
              );
            })}
          </View>
        </View>
      ) : null}

      <TextInput
        style={styles.symbolInput}
        value={symbolInput}
        onChangeText={(v) => setSymbolInput(v.toUpperCase())}
        placeholder="按币种过滤，如 BTCUSDT"
        placeholderTextColor={colors.textMuted}
        autoCapitalize="characters"
        autoCorrect={false}
      />

      <ScrollView style={styles.list} nestedScrollEnabled>
        {filtered.length === 0 ? (
          <View style={styles.emptyBox}>
            <Text style={styles.emptyText}>暂无匹配事件</Text>
          </View>
        ) : (
          filtered.slice(0, 120).map((evt) => {
            const meta = eventMeta(evt);
            return (
              <View key={evt.eventId} style={styles.row}>
                <View style={styles.rowTop}>
                  <Text style={styles.ts}>{fmtTime(evt.ts)}</Text>
                  <Text style={styles.source}>{sourceLabel(evt.source)}</Text>
                  <Text style={[styles.severity, { color: severityColor(evt.severity) }]}>{String(evt.severity || 'info').toUpperCase()}</Text>
                </View>
                <Text style={styles.typeLine}>
                  {evt.type || 'event'} {evt.symbol ? `| ${evt.symbol}` : ''} {evt.strategyId ? `| ${evt.strategyId}` : ''}
                </Text>
                <Text style={styles.message}>{evt.message || '-'}</Text>
                {meta ? <Text style={styles.meta}>{meta}</Text> : null}
              </View>
            );
          })
        )}
      </ScrollView>
    </View>
  );
}

const styles = StyleSheet.create({
  card: {
    backgroundColor: colors.card,
    borderRadius: radius.lg,
    borderWidth: 1,
    borderColor: colors.cardBorder,
    padding: spacing.md,
    marginBottom: spacing.md,
  },
  header: {
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
    marginBottom: spacing.sm,
  },
  headerRight: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: spacing.xs,
  },
  title: {
    color: colors.blueLight,
    fontSize: fontSize.md,
    fontWeight: '700',
  },
  countText: {
    color: colors.textMuted,
    fontSize: fontSize.xs,
  },
  clearBtn: {
    borderRadius: radius.pill,
    borderWidth: 1,
    borderColor: colors.cardBorder,
    backgroundColor: colors.surface,
    paddingHorizontal: spacing.sm,
    paddingVertical: 3,
  },
  clearText: {
    color: colors.textSecondary,
    fontSize: fontSize.xs,
    fontWeight: '600',
  },
  filterRow: {
    flexDirection: 'row',
    flexWrap: 'wrap',
    gap: spacing.xs,
    marginBottom: spacing.xs,
  },
  chip: {
    borderRadius: radius.pill,
    borderWidth: 1,
    borderColor: colors.cardBorder,
    backgroundColor: colors.surface,
    paddingHorizontal: spacing.sm,
    paddingVertical: 4,
  },
  chipActive: {
    borderColor: colors.blue,
    backgroundColor: colors.blueBg,
  },
  chipText: {
    color: colors.textSecondary,
    fontSize: fontSize.xs,
    fontWeight: '600',
  },
  chipTextActive: {
    color: colors.blueLight,
  },
  notifyWrap: {
    marginBottom: spacing.xs,
  },
  notifyTitle: {
    color: colors.textMuted,
    fontSize: fontSize.xs,
    marginBottom: 4,
  },
  symbolInput: {
    marginTop: 2,
    marginBottom: spacing.sm,
    backgroundColor: colors.cardAlt,
    borderRadius: radius.sm,
    borderWidth: 1,
    borderColor: colors.cardBorder,
    color: colors.white,
    paddingHorizontal: spacing.sm,
    paddingVertical: spacing.xs,
    fontSize: fontSize.sm,
  },
  list: {
    maxHeight: 260,
  },
  emptyBox: {
    borderRadius: radius.md,
    borderWidth: 1,
    borderColor: colors.divider,
    backgroundColor: colors.surface,
    paddingVertical: spacing.md,
    alignItems: 'center',
  },
  emptyText: {
    color: colors.textMuted,
    fontSize: fontSize.sm,
  },
  row: {
    borderRadius: radius.md,
    borderWidth: 1,
    borderColor: colors.divider,
    backgroundColor: colors.surface,
    padding: spacing.sm,
    marginBottom: spacing.xs,
  },
  rowTop: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: spacing.xs,
    marginBottom: 2,
  },
  ts: {
    color: colors.textMuted,
    fontSize: fontSize.xs,
    width: 56,
  },
  source: {
    color: colors.textSecondary,
    fontSize: fontSize.xs,
    fontWeight: '700',
    width: 42,
  },
  severity: {
    fontSize: fontSize.xs,
    fontWeight: '700',
  },
  typeLine: {
    color: colors.textSecondary,
    fontSize: fontSize.xs,
    marginBottom: 2,
  },
  message: {
    color: colors.white,
    fontSize: fontSize.sm,
  },
  meta: {
    color: colors.textMuted,
    fontSize: fontSize.xs,
    marginTop: 2,
  },
});
