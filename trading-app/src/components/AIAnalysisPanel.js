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
  const [policy, setPolicy] = useState(null);
  const [policyError, setPolicyError] = useState(null);
  const [policyLoading, setPolicyLoading] = useState(true);
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

  useEffect(() => {
    let active = true;
    const loadPolicy = async () => {
      setPolicyLoading(true);
      try {
        const res = await api.getAgentPolicy();
        if (!active) return;
        setPolicy(res.data || res);
        setPolicyError(null);
      } catch (e) {
        if (!active) return;
        setPolicyError(e.message);
      } finally {
        if (active) setPolicyLoading(false);
      }
    };
    loadPolicy();
    return () => {
      active = false;
    };
  }, []);

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
  const executionEnabled = !!policy?.enable_execution;

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
    if (policyLoading) {
      Alert.alert('策略加载中', '执行策略尚在加载，请稍后重试。');
      return;
    }
    if (!executionEnabled) {
      Alert.alert('执行已禁用', '当前策略不允许自动执行，请仅查看建议。');
      return;
    }
    if (!actionItems.length) {
      Alert.alert('无可执行建议', '请先点击“开始AI分析”获取建议。');
      return;
    }
    if (!selectedCount) {
      Alert.alert('未选择建议', '请先勾选要执行的建议。');
      return;
    }
    // 风控预检：逐条检查 open/add 类动作
    const openItems = selectedActionItems.filter(
      (it) => it.action === 'open' || it.action === 'add'
    );
    let riskWarnings = [];
    for (const it of openItems) {
      try {
        const res = await api.agentRiskCheck({
          symbol: it.symbol,
          side: it.detail?.includes('做多') || it.detail?.includes('long') ? 'BUY' : 'SELL',
          sizeUSDT: 10,
          leverage: 5,
        });
        if (res?.data && !res.data.allowed) {
          riskWarnings.push(`${it.symbol}: ${(res.data.reasons || []).join(', ')}`);
        }
      } catch (_e) { /* 预检失败不阻断 */ }
    }

    let confirmMsg = `将执行 ${selectedCount} 条已勾选建议。`;
    if (riskWarnings.length > 0) {
      confirmMsg += `\n\n⚠️ 风控预警:\n${riskWarnings.join('\n')}`;
    }
    confirmMsg += '\n\n是否继续？';

    Alert.alert(
      riskWarnings.length > 0 ? '⚠️ 执行确认（有风控预警）' : '执行确认',
      confirmMsg,
      [
        { text: '取消', style: 'cancel' },
        {
          text: riskWarnings.length > 0 ? '强制执行' : '执行',
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
        <View style={s.policyCard}>
          <Text style={s.policyTitle}>执行策略</Text>
          {policyLoading && <Text style={s.policyText}>加载中...</Text>}
          {!policyLoading && !!policyError && (
            <Text style={[s.policyText, { color: C.danger }]}>策略读取失败: {policyError}</Text>
          )}
          {!policyLoading && !policyError && !!policy && (
            <>
              <Text style={s.policyText}>模板: {policy.profile || 'custom'}</Text>
              {!!policy.description && <Text style={s.policyText}>说明: {policy.description}</Text>}
              <Text style={s.policyText}>执行开关: {executionEnabled ? '开启' : '关闭'}</Text>
              <Text style={s.policyText}>单次上限: {policy.max_actions_per_request || 0}</Text>
              <Text style={s.policyText}>
                动作白名单: {(policy.allowed_actions || []).join(', ') || '-'}
              </Text>
              <Text style={s.policyText}>
                币种白名单: {(policy.allowed_symbols || []).join(', ') || '不限制'}
              </Text>
            </>
          )}
        </View>
        <View style={s.btnRow}>
          <TouchableOpacity style={s.primaryBtn} onPress={onAnalyzePress} disabled={loading}>
            <Text style={s.primaryBtnText}>{loading ? '分析中...' : '开始AI分析'}</Text>
          </TouchableOpacity>
          <TouchableOpacity
            style={[s.dangerBtn, (!executionEnabled || policyLoading) ? s.btnDisabled : null]}
            onPress={onExecutePress}
            disabled={loading || policyLoading || !executionEnabled}
          >
            <Text style={s.dangerBtnText}>{executionEnabled ? '执行建议下单' : '执行已禁用'}</Text>
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
  policyCard: {
    borderWidth: 1,
    borderColor: C.border,
    borderRadius: radius.sm,
    padding: spacing.sm,
    gap: 4,
    backgroundColor: C.bg,
  },
  policyTitle: {
    color: C.text,
    fontSize: fontSize.sm,
    fontWeight: '800',
  },
  policyText: {
    color: C.textDim,
    fontSize: fontSize.xs,
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
  btnDisabled: {
    opacity: 0.45,
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
