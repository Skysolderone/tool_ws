import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import {
  View,
  Text,
  TouchableOpacity,
  StyleSheet,
  ActivityIndicator,
  Alert,
  ScrollView,
  TextInput,
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
// 单币分析可能接近 100s，上层网关有超时限制，这里给前端更长窗口。
const ANALYZE_TIMEOUT_MS = 210000;
const RAW_POSITION_TIMEOUT_MS = 15000;
const ANALYZE_MODE = 'positions';

export default function AIAnalysisPanel() {
  const [loading, setLoading] = useState(false);
  const [data, setData] = useState(null);
  const [error, setError] = useState(null);
  const [rawPositionData, setRawPositionData] = useState(null);
  const [rawPositionError, setRawPositionError] = useState(null);
  const [targetSymbol, setTargetSymbol] = useState('');
  const [analyzedSymbol, setAnalyzedSymbol] = useState('');
  const [policy, setPolicy] = useState(null);
  const [policyError, setPolicyError] = useState(null);
  const [policyLoading, setPolicyLoading] = useState(true);
  const [selectedActions, setSelectedActions] = useState({});
  const analyzeAbortRef = useRef(null);
  const rawPositionAbortRef = useRef(null);
  const requestIdRef = useRef(0);

  const actionItems = useMemo(() => data?.action_items || [], [data]);
  const positionAnalysis = useMemo(() => data?.position_analysis || [], [data]);
  const rawPositionItems = useMemo(() => rawPositionData?.items || [], [rawPositionData]);
  const execution = data?.execution;

  useEffect(() => {
    const next = {};
    actionItems.forEach((item, idx) => {
      next[getActionKey(item, idx)] = false;
    });
    setSelectedActions((prev) => {
      const prevKeys = Object.keys(prev);
      const nextKeys = Object.keys(next);
      if (prevKeys.length !== nextKeys.length) return next;
      for (const k of nextKeys) {
        if (prev[k] !== next[k]) return next;
      }
      return prev;
    });
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
      if (analyzeAbortRef.current) {
        analyzeAbortRef.current.abort();
      }
      if (rawPositionAbortRef.current) {
        rawPositionAbortRef.current.abort();
      }
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

  const runAgent = useCallback(async ({ symbols }) => {
    if (analyzeAbortRef.current) {
      analyzeAbortRef.current.abort();
    }
    if (rawPositionAbortRef.current) {
      rawPositionAbortRef.current.abort();
    }
    const requestId = requestIdRef.current + 1;
    requestIdRef.current = requestId;
    const controller = new AbortController();
    analyzeAbortRef.current = controller;
    const normalizedSymbols = normalizeSymbolList(symbols || []);
    const timeoutMs = normalizedSymbols.length > 1
      ? Math.max(ANALYZE_TIMEOUT_MS, normalizedSymbols.length * 110000)
      : ANALYZE_TIMEOUT_MS;
    let timedOut = false;
    const timeoutId = setTimeout(() => {
      timedOut = true;
      controller.abort();
    }, timeoutMs);

    setLoading(true);
    setError(null);
    setRawPositionError(null);
    setRawPositionData(null);
    try {
      let result;
      if (normalizedSymbols.length <= 1) {
        const body = {
          mode: ANALYZE_MODE,
          symbols: normalizedSymbols,
        };
        const res = await api.analyzeAgent(body, { signal: controller.signal });
        result = res.data || res;
      } else {
        const merged = createEmptyAnalysisResult();
        const summaryParts = [];
        const failures = [];
        for (const symbol of normalizedSymbols) {
          if (controller.signal.aborted) break;
          try {
            const res = await api.analyzeAgent(
              { mode: ANALYZE_MODE, symbols: [symbol] },
              { signal: controller.signal },
            );
            const part = res.data || res;
            if (part?.summary) {
              summaryParts.push(`[${symbol}] ${part.summary}`);
            }
            mergeAnalysisResult(merged, part);
          } catch (e) {
            if (controller.signal.aborted) {
              throw e;
            }
            failures.push(`${symbol}: ${e.message}`);
          }
        }
        if (!hasAnalysisResult(merged)) {
          throw new Error(failures.length ? `全部币种分析失败: ${failures.join('；')}` : '分析失败');
        }
        if (summaryParts.length > 0) {
          merged.summary = summaryParts.join('\n\n');
        }
        if (failures.length > 0) {
          const failureText = `以下币种分析失败：${failures.join('；')}`;
          merged.summary = merged.summary ? `${merged.summary}\n\n${failureText}` : failureText;
        }
        result = merged;
      }

      if (requestId !== requestIdRef.current) return;
      setData(result);
      setAnalyzedSymbol(
        normalizedSymbols.length > 0 ? normalizedSymbols.join(', ') : '全部持仓'
      );
      setError(null);

      const rawController = new AbortController();
      rawPositionAbortRef.current = rawController;
      const rawTimeoutId = setTimeout(() => {
        rawController.abort();
      }, RAW_POSITION_TIMEOUT_MS);
      api
        .getRecommendAnalyze({ signal: rawController.signal })
        .then((rawRes) => {
          if (requestId !== requestIdRef.current) return;
          setRawPositionData(rawRes?.data || rawRes || null);
          setRawPositionError(null);
        })
        .catch((e) => {
          if (requestId !== requestIdRef.current) return;
          if (rawController.signal.aborted) {
            setRawPositionError(`原始仓位分析超时（>${RAW_POSITION_TIMEOUT_MS / 1000}s）`);
            return;
          }
          setRawPositionError(e.message);
        })
        .finally(() => {
          clearTimeout(rawTimeoutId);
          if (rawPositionAbortRef.current === rawController) {
            rawPositionAbortRef.current = null;
          }
        });
    } catch (e) {
      if (requestId !== requestIdRef.current) return;
      if (controller.signal.aborted) {
        setError(timedOut ? `分析超时（>${Math.round(timeoutMs / 1000)}s），请重试。` : '已取消本次分析。');
      } else {
        setError(e.message);
      }
    } finally {
      clearTimeout(timeoutId);
      if (analyzeAbortRef.current === controller) {
        analyzeAbortRef.current = null;
      }
      if (requestId === requestIdRef.current) {
        setLoading(false);
      }
    }
  }, []);

  const onAnalyzePress = () => {
    const raw = String(targetSymbol || '').trim();
    if (!raw) {
      setTargetSymbol('');
      runAgent({ symbols: [] });
      return;
    }

    const symbols = parseSymbolsInput(raw);
    if (!symbols.length) {
      Alert.alert('参数错误', '请输入有效代币，例如 BTCUSDT 或 ETH；留空则分析全部持仓。');
      return;
    }
    setTargetSymbol(symbols.join(', '));
    runAgent({ symbols });
  };

  const onCancelAnalyze = () => {
    if (analyzeAbortRef.current) {
      analyzeAbortRef.current.abort();
    }
  };

  const onExecutePress = async () => {
    if (policyLoading) {
      Alert.alert('策略加载中', '执行策略尚在加载，请稍后重试。');
      return;
    }
    if (!executionEnabled) {
      Alert.alert('执行已禁用', '当前策略不允许自动执行，请仅查看建议。');
      return;
    }
    if (!actionItems.length) {
      Alert.alert('无可执行建议', '请先点击“使用Agent分析”获取建议。');
      return;
    }
    if (!selectedCount) {
      Alert.alert('未选择建议', '请先勾选要执行的建议。');
      return;
    }
    if (!analyzedSymbol) {
      Alert.alert('请先分析', '请先点击“使用Agent分析”生成建议。');
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
          onPress: async () => {
            setLoading(true);
            setError(null);
            try {
              const res = await api.executeAgent({ action_items: selectedActionItems });
              const exec = res.data || res;
              setData((prev) => ({ ...(prev || {}), execution: exec }));
            } catch (e) {
              setError(e.message);
            } finally {
              setLoading(false);
            }
          },
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
        <Text style={s.subtitle}>不会自动分析，仅在点击按钮时分析（可指定代币或全部持仓）</Text>
        <View style={s.symbolRow}>
          <TextInput
            style={s.symbolInput}
            value={targetSymbol}
            onChangeText={setTargetSymbol}
            placeholder="输入代币，如 BTCUSDT,ETH 或 SOL；留空=全部持仓"
            placeholderTextColor={C.textDim}
            autoCapitalize="characters"
            autoCorrect={false}
          />
          <View style={s.symbolHintWrap}>
            <Text style={s.symbolHint}>当前分析: {analyzedSymbol || '未分析'}</Text>
          </View>
        </View>
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
            <Text style={s.primaryBtnText}>{loading ? '分析中...' : '使用Agent分析'}</Text>
          </TouchableOpacity>
          {loading ? (
            <TouchableOpacity style={s.cancelBtn} onPress={onCancelAnalyze}>
              <Text style={s.cancelBtnText}>取消分析</Text>
            </TouchableOpacity>
          ) : (
            <TouchableOpacity
              style={[s.dangerBtn, (!executionEnabled || policyLoading) ? s.btnDisabled : null]}
              onPress={onExecutePress}
              disabled={policyLoading || !executionEnabled}
            >
              <Text style={s.dangerBtnText}>{executionEnabled ? '执行建议下单' : '执行已禁用'}</Text>
            </TouchableOpacity>
          )}
        </View>
      </View>

      {loading && (
        <View style={s.card}>
          <ActivityIndicator size="large" color={C.primary} />
          <Text style={s.loadingText}>正在请求 Agent，请稍候...</Text>
          <Text style={s.loadingHint}>reasoner 模型可能需要 1-3 分钟；若不想等待可点击上方“取消分析”。</Text>
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
            <Text style={s.cardTitle}>仓位情况分析</Text>
            {!!rawPositionError && (
              <Text style={[s.emptyText, { color: C.warn }]}>原始仓位分析加载失败: {rawPositionError}</Text>
            )}
            {!rawPositionError && rawPositionItems.length === 0 && <Text style={s.emptyText}>暂无仓位数据</Text>}
            {rawPositionItems.map((item, idx) => {
              const isLong = (item.side || '').toUpperCase() === 'LONG';
              const pnl = Number(item.unrealizedPnl || 0);
              const pnlPct = Number(item.pnlPercent || 0);
              return (
                <View key={`${item.symbol}-${item.side}-${idx}`} style={s.rawPosRow}>
                  <View style={s.rawPosHead}>
                    <Text style={s.rawPosSymbol}>{item.symbol || '--'}</Text>
                    <Text style={[s.sideTag, isLong ? s.sideLong : s.sideShort]}>{item.side || '--'}</Text>
                  </View>
                  <View style={s.rawPosMeta}>
                    <Text style={[s.rawPosPnl, { color: pnl >= 0 ? C.success : C.danger }]}>
                      浮盈亏: {pnl >= 0 ? '+' : ''}{pnl.toFixed(2)} USDT ({pnlPct >= 0 ? '+' : ''}{pnlPct.toFixed(2)}%)
                    </Text>
                    <Text style={s.rawPosMetaText}>
                      入场: {formatPrice(item.entryPrice)} · 现价: {formatPrice(item.markPrice)} · 数量: {Math.abs(Number(item.amount || 0))} · 杠杆: {Number(item.leverage || 0)}x
                    </Text>
                  </View>
                  <Text style={s.rawPosAdvice}>建议: {item.adviceLabel || '-'}</Text>
                  {(item.reasons || []).slice(0, 3).map((reason, ridx) => (
                    <Text key={`${item.symbol}-reason-${ridx}`} style={s.rawPosReason}>- {reason}</Text>
                  ))}
                </View>
              );
            })}
          </View>

          <View style={s.card}>
            <Text style={s.cardTitle}>Agent仓位分析</Text>
            {positionAnalysis.length === 0 && <Text style={s.emptyText}>暂无 position_analysis</Text>}
            {positionAnalysis.map((item, idx) => {
              const r = (item.risk || '').toLowerCase();
              const riskStyle = r === 'critical'
                ? s.riskCritical
                : r === 'high'
                  ? s.riskHigh
                  : r === 'medium'
                    ? s.riskMedium
                    : s.riskLow;
              return (
                <View key={`${item.symbol}-${idx}`} style={s.positionRow}>
                  <View style={s.positionHeader}>
                    <Text style={s.positionSymbol}>{item.symbol || '--'}</Text>
                    <Text style={[s.riskTag, riskStyle]}>{item.risk || 'low'}</Text>
                  </View>
                  <Text style={s.positionAssessment}>{item.assessment || '-'}</Text>
                  <Text style={s.positionSuggestion}>建议: {item.suggestion || '-'}</Text>
                  {(item.reasons || []).slice(0, 4).map((reason, ridx) => (
                    <Text key={`${item.symbol}-agent-reason-${ridx}`} style={s.positionReason}>- {reason}</Text>
                  ))}
                </View>
              );
            })}
          </View>

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
  symbolRow: {
    gap: spacing.xs,
    marginBottom: spacing.xs,
  },
  symbolInput: {
    borderWidth: 1,
    borderColor: C.border,
    borderRadius: radius.sm,
    backgroundColor: C.bg,
    color: C.text,
    paddingHorizontal: spacing.sm,
    paddingVertical: spacing.sm,
    fontSize: fontSize.sm,
    fontWeight: '700',
  },
  symbolHintWrap: {
    alignItems: 'flex-end',
  },
  symbolHint: {
    color: C.textDim,
    fontSize: fontSize.xs,
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
  cancelBtn: {
    flex: 1,
    backgroundColor: C.warnBg,
    borderColor: C.warn,
    borderWidth: 1,
    borderRadius: radius.sm,
    alignItems: 'center',
    justifyContent: 'center',
    paddingVertical: spacing.sm,
  },
  cancelBtnText: {
    color: C.warn,
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
  loadingHint: {
    color: C.textDim,
    fontSize: fontSize.xs,
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
  rawPosRow: {
    borderWidth: 1,
    borderColor: C.border,
    borderRadius: radius.sm,
    padding: spacing.sm,
    gap: 4,
  },
  rawPosHead: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'center',
  },
  rawPosSymbol: {
    color: C.text,
    fontSize: fontSize.sm,
    fontWeight: '800',
    flex: 1,
    marginRight: spacing.sm,
  },
  sideTag: {
    fontSize: fontSize.xs,
    fontWeight: '800',
    paddingHorizontal: 8,
    paddingVertical: 2,
    borderRadius: 999,
    overflow: 'hidden',
  },
  sideLong: {
    color: C.success,
    backgroundColor: C.successBg,
  },
  sideShort: {
    color: C.danger,
    backgroundColor: C.dangerBg,
  },
  rawPosMeta: {
    gap: 2,
  },
  rawPosPnl: {
    fontSize: fontSize.sm,
    fontWeight: '800',
  },
  rawPosMetaText: {
    color: C.textDim,
    fontSize: fontSize.xs,
  },
  rawPosAdvice: {
    color: C.text,
    fontSize: fontSize.sm,
    fontWeight: '700',
  },
  rawPosReason: {
    color: C.textDim,
    fontSize: fontSize.xs,
    lineHeight: 18,
  },
  positionRow: {
    borderWidth: 1,
    borderColor: C.border,
    borderRadius: radius.sm,
    padding: spacing.sm,
    gap: 4,
  },
  positionHeader: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'center',
  },
  positionSymbol: {
    color: C.text,
    fontWeight: '800',
    fontSize: fontSize.sm,
    flex: 1,
    marginRight: spacing.sm,
  },
  positionAssessment: {
    color: C.text,
    fontSize: fontSize.sm,
  },
  positionSuggestion: {
    color: C.textDim,
    fontSize: fontSize.xs,
  },
  positionReason: {
    color: C.textDim,
    fontSize: fontSize.xs,
    lineHeight: 18,
  },
  riskTag: {
    fontSize: fontSize.xs,
    fontWeight: '800',
    paddingHorizontal: 8,
    paddingVertical: 2,
    borderRadius: 999,
    overflow: 'hidden',
  },
  riskCritical: {
    color: C.danger,
    backgroundColor: C.dangerBg,
  },
  riskHigh: {
    color: C.danger,
    backgroundColor: C.dangerBg,
  },
  riskMedium: {
    color: C.warn,
    backgroundColor: C.warnBg,
  },
  riskLow: {
    color: C.success,
    backgroundColor: C.successBg,
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

function normalizeSymbol(input) {
  const raw = String(input || '').trim().toUpperCase();
  if (!raw) return '';
  if (/^[A-Z0-9]+USDT$/.test(raw)) return raw;
  if (/^[A-Z0-9]+$/.test(raw)) return `${raw}USDT`;
  return '';
}

function normalizeSymbolList(inputSymbols) {
  const seen = new Set();
  const out = [];
  (inputSymbols || []).forEach((item) => {
    const symbol = normalizeSymbol(item);
    if (!symbol || seen.has(symbol)) return;
    seen.add(symbol);
    out.push(symbol);
  });
  return out;
}

function parseSymbolsInput(input) {
  const tokens = String(input || '')
    .split(/[,\s，;；]+/)
    .map((x) => x.trim())
    .filter(Boolean);
  return normalizeSymbolList(tokens);
}

function createEmptyAnalysisResult() {
  return {
    summary: '',
    position_analysis: [],
    signal_evaluation: [],
    journal_review: {
      patterns: [],
      weaknesses: [],
      strengths: [],
      suggestion: '',
    },
    action_items: [],
  };
}

function mergeAnalysisResult(base, part) {
  if (!base || !part) return;
  if (Array.isArray(part.position_analysis)) {
    base.position_analysis = base.position_analysis.concat(part.position_analysis);
  }
  if (Array.isArray(part.signal_evaluation)) {
    base.signal_evaluation = base.signal_evaluation.concat(part.signal_evaluation);
  }
  if (Array.isArray(part.action_items)) {
    base.action_items = base.action_items.concat(part.action_items);
  }
}

function hasAnalysisResult(result) {
  if (!result) return false;
  return (
    (Array.isArray(result.position_analysis) && result.position_analysis.length > 0) ||
    (Array.isArray(result.signal_evaluation) && result.signal_evaluation.length > 0) ||
    (Array.isArray(result.action_items) && result.action_items.length > 0) ||
    !!String(result.summary || '').trim()
  );
}

function formatPrice(value) {
  const p = Number(value || 0);
  if (!Number.isFinite(p) || p <= 0) return '--';
  if (p >= 1000) return p.toFixed(1);
  if (p >= 1) return p.toFixed(2);
  return p.toFixed(4);
}
