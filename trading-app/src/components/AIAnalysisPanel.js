import React, { useEffect, useMemo, useState } from 'react';
import {
  View,
  Text,
  TouchableOpacity,
  StyleSheet,
  ActivityIndicator,
  Alert,
  ScrollView,
} from 'react-native';
import { colors, spacing, radius, fontSize } from '../services/theme';
import api from '../services/api';

const C = {
  primary: colors.gold,
  primaryBg: colors.goldBg,
  border: colors.cardBorder,
  card: colors.card,
  bg: colors.bg,
  text: colors.text,
  textDim: colors.textSecondary,
  success: colors.green,
  successBg: colors.greenBg,
  danger: colors.red,
  dangerBg: colors.redBg,
  warn: colors.yellow,
  warnBg: colors.yellowBg,
};

export default function AIAnalysisPanel() {
  const [loading, setLoading] = useState(false);
  const [data, setData] = useState(null);
  const [error, setError] = useState(null);
  const [selectedActions, setSelectedActions] = useState({});

  const actionItems = data?.action_items || [];
  const execution = data?.execution;

  useEffect(() => {
    const next = {};
    actionItems.forEach((item, idx) => {
      next[getActionKey(item, idx)] = false;
    });
    setSelectedActions(next);
  }, [actionItems]);

  const actionStats = useMemo(() => {
    const total = actionItems.length;
    const high = actionItems.filter((x) => x.priority === 'high').length;
    return { total, high };
  }, [actionItems]);

  const selectedActionItems = useMemo(
    () => actionItems.filter((item, idx) => !!selectedActions[getActionKey(item, idx)]),
    [actionItems, selectedActions],
  );
  const selectedCount = selectedActionItems.length;

  const runAgent = async ({ execute, items = [] }) => {
    setLoading(true);
    try {
      const body = {
        mode: 'full',
        execute,
      };
      if (execute) {
        body.action_items = items;
      }
      const res = await api.analyzeAgent(body);
      setData(res.data || res);
      setError(null);
    } catch (e) {
      setError(e.message);
    } finally {
      setLoading(false);
    }
  };

  const onAnalyzePress = () => {
    runAgent({ execute: false, items: [] });
  };

  const onExecutePress = () => {
    if (!actionItems.length) {
      Alert.alert('无可执行建议', '请先点击“开始AI分析”获取建议。');
      return;
    }
    if (!selectedCount) {
      Alert.alert('未选择建议', '请先勾选要执行的建议。');
      return;
    }
    Alert.alert(
      '执行确认',
      `将执行 ${selectedCount} 条已勾选建议（可执行动作由后端校验）。是否继续？`,
      [
        { text: '取消', style: 'cancel' },
        {
          text: '执行',
          style: 'destructive',
          onPress: () => runAgent({ execute: true, items: selectedActionItems }),
        },
      ],
    );
  };

  const onToggleAction = (item, idx) => {
    const k = getActionKey(item, idx);
    setSelectedActions((prev) => ({ ...prev, [k]: !prev[k] }));
  };

  const onSelectAll = () => {
    const next = {};
    actionItems.forEach((item, idx) => {
      next[getActionKey(item, idx)] = true;
    });
    setSelectedActions(next);
  };

  const onClearAll = () => {
    const next = {};
    actionItems.forEach((item, idx) => {
      next[getActionKey(item, idx)] = false;
    });
    setSelectedActions(next);
  };

  return (
    <ScrollView style={s.root} contentContainerStyle={s.content}>
      <View style={s.headerCard}>
        <Text style={s.title}>Agent 智能分析</Text>
        <Text style={s.subtitle}>点击按钮后才会触发分析，不再自动轮询</Text>
        <View style={s.btnRow}>
          <TouchableOpacity style={s.primaryBtn} onPress={onAnalyzePress} disabled={loading}>
            <Text style={s.primaryBtnText}>{loading ? '分析中...' : '开始AI分析'}</Text>
          </TouchableOpacity>
          <TouchableOpacity style={s.dangerBtn} onPress={onExecutePress} disabled={loading}>
            <Text style={s.dangerBtnText}>执行建议下单</Text>
          </TouchableOpacity>
        </View>
      </View>

      {loading && (
        <View style={s.card}>
          <ActivityIndicator size="large" color={C.primary} />
          <Text style={s.loadingText}>正在请求 Agent，请稍候...</Text>
        </View>
      )}

      {error && !loading && (
        <View style={s.card}>
          <Text style={s.errorTitle}>请求失败</Text>
          <Text style={s.errorText}>{error}</Text>
        </View>
      )}

      {!!data && !loading && (
        <>
          <View style={s.card}>
            <Text style={s.cardTitle}>分析总结</Text>
            <Text style={s.summaryText}>{data.summary || '无总结'}</Text>
          </View>

          <View style={s.card}>
            <Text style={s.cardTitle}>操作建议</Text>
            <View style={s.statsRow}>
              <Text style={s.statText}>总建议: {actionStats.total}</Text>
              <Text style={s.statText}>高优先级: {actionStats.high}</Text>
              <Text style={s.statText}>已勾选: {selectedCount}</Text>
            </View>
            {actionItems.length > 0 && (
              <View style={s.selectRow}>
                <TouchableOpacity style={s.selectBtn} onPress={onSelectAll}>
                  <Text style={s.selectBtnText}>全选</Text>
                </TouchableOpacity>
                <TouchableOpacity style={s.selectBtn} onPress={onClearAll}>
                  <Text style={s.selectBtnText}>清空</Text>
                </TouchableOpacity>
              </View>
            )}
            {actionItems.length === 0 && <Text style={s.emptyText}>暂无 action_items</Text>}
            {actionItems.map((item, idx) => {
              const p = (item.priority || '').toLowerCase();
              const priorityStyle = p === 'high' ? s.priHigh : p === 'medium' ? s.priMedium : s.priLow;
              const checked = !!selectedActions[getActionKey(item, idx)];
              return (
                <View key={`${item.symbol}-${item.action}-${idx}`} style={s.actionRow}>
                  <View style={s.actionHeader}>
                    <View style={s.actionMainWrap}>
                      <TouchableOpacity style={s.checkboxBtn} onPress={() => onToggleAction(item, idx)}>
                        <Text style={[s.checkboxText, checked ? s.checkboxChecked : null]}>{checked ? '☑' : '☐'}</Text>
                      </TouchableOpacity>
                      <Text style={s.actionMain}>{item.symbol} · {item.action}</Text>
                    </View>
                    <Text style={[s.priorityTag, priorityStyle]}>{item.priority || 'low'}</Text>
                  </View>
                  <Text style={s.actionDetail}>{item.detail || '-'}</Text>
                  <Text style={s.actionRisk}>风险: {item.risk || '-'}</Text>
                </View>
              );
            })}
          </View>

          {!!execution && (
            <View style={s.card}>
              <Text style={s.cardTitle}>执行结果</Text>
              <View style={s.statsRow}>
                <Text style={s.statText}>请求: {execution.requested}</Text>
                <Text style={s.statText}>执行: {execution.executed}</Text>
                <Text style={[s.statText, { color: C.success }]}>成功: {execution.success}</Text>
                <Text style={[s.statText, { color: C.danger }]}>失败: {execution.failed}</Text>
                <Text style={[s.statText, { color: C.warn }]}>跳过: {execution.skipped}</Text>
              </View>
              {(execution.results || []).map((r, idx) => {
                const status = (r.status || '').toLowerCase();
                const color = status === 'success' ? C.success : status === 'failed' ? C.danger : C.warn;
                return (
                  <View key={`${r.symbol}-${r.action}-${idx}`} style={s.execRow}>
                    <Text style={[s.execStatus, { color }]}>{r.status}</Text>
                    <Text style={s.execText}>{r.symbol} · {r.action}</Text>
                    <Text style={s.execMsg}>{r.message}</Text>
                  </View>
                );
              })}
            </View>
          )}
        </>
      )}
    </ScrollView>
  );
}

const s = StyleSheet.create({
  root: {
    flex: 1,
    backgroundColor: C.bg,
  },
  content: {
    padding: spacing.md,
    gap: spacing.md,
  },
  headerCard: {
    backgroundColor: C.card,
    borderRadius: radius.lg,
    borderWidth: 1,
    borderColor: C.border,
    padding: spacing.lg,
    gap: spacing.sm,
  },
  title: {
    color: C.primary,
    fontSize: fontSize.lg,
    fontWeight: '900',
  },
  subtitle: {
    color: C.textDim,
    fontSize: fontSize.sm,
  },
  btnRow: {
    flexDirection: 'row',
    gap: spacing.sm,
    marginTop: spacing.sm,
  },
  primaryBtn: {
    flex: 1,
    backgroundColor: C.primaryBg,
    borderColor: C.primary,
    borderWidth: 1,
    borderRadius: radius.sm,
    alignItems: 'center',
    justifyContent: 'center',
    paddingVertical: spacing.sm,
  },
  primaryBtnText: {
    color: C.primary,
    fontWeight: '900',
    fontSize: fontSize.sm,
  },
  dangerBtn: {
    flex: 1,
    backgroundColor: C.dangerBg,
    borderColor: C.danger,
    borderWidth: 1,
    borderRadius: radius.sm,
    alignItems: 'center',
    justifyContent: 'center',
    paddingVertical: spacing.sm,
  },
  dangerBtnText: {
    color: C.danger,
    fontWeight: '900',
    fontSize: fontSize.sm,
  },
  card: {
    backgroundColor: C.card,
    borderRadius: radius.lg,
    borderWidth: 1,
    borderColor: C.border,
    padding: spacing.lg,
    gap: spacing.sm,
  },
  cardTitle: {
    color: C.text,
    fontSize: fontSize.md,
    fontWeight: '800',
  },
  loadingText: {
    color: C.textDim,
    fontSize: fontSize.sm,
    textAlign: 'center',
  },
  errorTitle: {
    color: C.danger,
    fontWeight: '800',
    fontSize: fontSize.md,
  },
  errorText: {
    color: C.text,
    fontSize: fontSize.sm,
  },
  summaryText: {
    color: C.text,
    fontSize: fontSize.sm,
    lineHeight: 22,
  },
  statsRow: {
    flexDirection: 'row',
    flexWrap: 'wrap',
    gap: spacing.sm,
  },
  statText: {
    color: C.textDim,
    fontSize: fontSize.xs,
  },
  emptyText: {
    color: C.textDim,
    fontSize: fontSize.sm,
  },
  actionRow: {
    borderWidth: 1,
    borderColor: C.border,
    borderRadius: radius.sm,
    padding: spacing.sm,
    gap: 4,
  },
  actionHeader: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'center',
  },
  actionMainWrap: {
    flexDirection: 'row',
    alignItems: 'center',
    flex: 1,
    marginRight: spacing.sm,
  },
  checkboxBtn: {
    marginRight: spacing.xs,
    paddingHorizontal: 2,
  },
  checkboxText: {
    color: C.textDim,
    fontSize: fontSize.md,
    fontWeight: '700',
  },
  checkboxChecked: {
    color: C.primary,
  },
  actionMain: {
    color: C.text,
    fontWeight: '700',
    fontSize: fontSize.sm,
    flexShrink: 1,
  },
  priorityTag: {
    fontSize: fontSize.xs,
    fontWeight: '800',
    paddingHorizontal: 8,
    paddingVertical: 2,
    borderRadius: 999,
    overflow: 'hidden',
  },
  priHigh: {
    color: C.danger,
    backgroundColor: C.dangerBg,
  },
  priMedium: {
    color: C.warn,
    backgroundColor: C.warnBg,
  },
  priLow: {
    color: C.success,
    backgroundColor: C.successBg,
  },
  actionDetail: {
    color: C.text,
    fontSize: fontSize.sm,
  },
  actionRisk: {
    color: C.textDim,
    fontSize: fontSize.xs,
  },
  execRow: {
    borderWidth: 1,
    borderColor: C.border,
    borderRadius: radius.sm,
    padding: spacing.sm,
    gap: 3,
  },
  execStatus: {
    fontSize: fontSize.xs,
    fontWeight: '900',
    textTransform: 'uppercase',
  },
  execText: {
    color: C.text,
    fontWeight: '700',
    fontSize: fontSize.sm,
  },
  execMsg: {
    color: C.textDim,
    fontSize: fontSize.xs,
  },
  selectRow: {
    flexDirection: 'row',
    gap: spacing.sm,
  },
  selectBtn: {
    borderWidth: 1,
    borderColor: C.border,
    borderRadius: radius.sm,
    paddingHorizontal: spacing.sm,
    paddingVertical: 6,
  },
  selectBtnText: {
    color: C.textDim,
    fontSize: fontSize.xs,
    fontWeight: '700',
  },
});

function getActionKey(item, idx) {
  return `${item.symbol || ''}|${item.action || ''}|${item.priority || ''}|${item.detail || ''}|${idx}`;
}
