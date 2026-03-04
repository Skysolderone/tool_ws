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
// 思考模型响应较慢，前端等待窗口放宽到 10 分钟。
const ANALYZE_TIMEOUT_MS = 600000;
const RAW_POSITION_TIMEOUT_MS = 15000;
const ANALYZE_MODE = 'positions';
const AGENT_LOG_LIMIT = 30;
const AGENT_LOG_POLL_MS = 1500;
const AGENT_EVAL_DAYS = 7;
const STREAM_PREVIEW_MAX = 320;
const CHAT_TIMEOUT_MS = 120000;

export default function AIAnalysisPanel() {
  const [loading, setLoading] = useState(false);
  const [panelMode, setPanelMode] = useState('analysis');
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
  const [analysisLogs, setAnalysisLogs] = useState([]);
  const [logsLoading, setLogsLoading] = useState(false);
  const [logsError, setLogsError] = useState(null);
  const [evalSummary, setEvalSummary] = useState(null);
  const [evalLoading, setEvalLoading] = useState(false);
  const [evalError, setEvalError] = useState(null);
  const [activeTaskId, setActiveTaskId] = useState(null);
  const [streamProgress, setStreamProgress] = useState(null);
  const [streamText, setStreamText] = useState('');
  const [streamMode, setStreamMode] = useState('');
  const [chatInput, setChatInput] = useState('');
  const [chatSymbols, setChatSymbols] = useState('');
  const [chatMessages, setChatMessages] = useState([]);
  const [chatLoading, setChatLoading] = useState(false);
  const analyzeAbortRef = useRef(null);
  const rawPositionAbortRef = useRef(null);
  const logsAbortRef = useRef(null);
  const evalAbortRef = useRef(null);
  const chatAbortRef = useRef(null);
  const requestIdRef = useRef(0);

  const actionItems = useMemo(() => data?.action_items || [], [data]);
  const positionAnalysis = useMemo(() => data?.position_analysis || [], [data]);
  const rawPositionItems = useMemo(() => rawPositionData?.items || [], [rawPositionData]);
  const execution = data?.execution;
  const currentTaskLabel = activeTaskId ? `#${activeTaskId}` : '';
  const streamPreview = useMemo(() => {
    if (!streamText) return '';
    if (streamText.length <= STREAM_PREVIEW_MAX) return streamText;
    return streamText.slice(streamText.length - STREAM_PREVIEW_MAX);
  }, [streamText]);

  const loadAnalysisLogs = useCallback(async (requestOptions = {}) => {
    setLogsLoading(true);
    try {
      const res = await api.getAgentLogs({ limit: AGENT_LOG_LIMIT }, requestOptions);
      const payload = res?.data ?? res;
      setAnalysisLogs(Array.isArray(payload) ? payload : []);
      setLogsError(null);
    } catch (e) {
      if (requestOptions?.signal?.aborted) return;
      setLogsError(e.message);
    } finally {
      if (!requestOptions?.signal?.aborted) {
        setLogsLoading(false);
      }
    }
  }, []);

  const loadEvaluationSummary = useCallback(async (requestOptions = {}) => {
    setEvalLoading(true);
    try {
      const res = await api.getAgentEvaluation(AGENT_EVAL_DAYS, requestOptions);
      const payload = res?.data ?? res;
      setEvalSummary(payload && typeof payload === 'object' ? payload : null);
      setEvalError(null);
    } catch (e) {
      if (requestOptions?.signal?.aborted) return;
      setEvalError(e.message);
    } finally {
      if (!requestOptions?.signal?.aborted) {
        setEvalLoading(false);
      }
    }
  }, []);

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
    const logController = new AbortController();
    logsAbortRef.current = logController;
    loadAnalysisLogs({ signal: logController.signal });
    const evalController = new AbortController();
    evalAbortRef.current = evalController;
    loadEvaluationSummary({ signal: evalController.signal });
    return () => {
      active = false;
      if (analyzeAbortRef.current) {
        analyzeAbortRef.current.abort();
      }
      if (rawPositionAbortRef.current) {
        rawPositionAbortRef.current.abort();
      }
      if (logsAbortRef.current) {
        logsAbortRef.current.abort();
      }
      if (evalAbortRef.current) {
        evalAbortRef.current.abort();
      }
      if (chatAbortRef.current) {
        chatAbortRef.current.abort();
      }
    };
  }, [loadAnalysisLogs, loadEvaluationSummary]);

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
  const evalView = useMemo(() => {
    const source = evalSummary || {};
    const num = (value, fallback = 0) => {
      const n = Number(value);
      return Number.isFinite(n) ? n : fallback;
    };

    return {
      totalSuggestions: num(source.total_suggestions ?? source.totalAdvices, 0),
      hitRate1H: num(source.hit_rate_1h, 0),
      hitRate4H: num(source.hit_rate_4h, 0),
      hitRate24H: num(source.hit_rate_24h ?? source.hitRate, 0),
      avgPnl1H: num(source.avg_pnl_1h, 0),
      avgPnl24H: num(source.avg_pnl_24h ?? source.avgPnlUsdt, 0),
      bestMode: String(source.best_mode ?? source.bestMode ?? '-').trim() || '-',
      worstSymbols: Array.isArray(source.worst_symbols ?? source.worstSymbols)
        ? (source.worst_symbols ?? source.worstSymbols).filter(Boolean)
        : [],
    };
  }, [evalSummary]);

  const fetchRawPositionSnapshot = useCallback(async (requestId) => {
    if (rawPositionAbortRef.current) {
      rawPositionAbortRef.current.abort();
    }
    const rawController = new AbortController();
    rawPositionAbortRef.current = rawController;
    const rawTimeoutId = setTimeout(() => {
      rawController.abort();
    }, RAW_POSITION_TIMEOUT_MS);
    try {
      const rawRes = await api.getRecommendAnalyze({ signal: rawController.signal });
      if (requestId !== requestIdRef.current) return;
      setRawPositionData(rawRes?.data || rawRes || null);
      setRawPositionError(null);
    } catch (e) {
      if (requestId !== requestIdRef.current) return;
      if (rawController.signal.aborted) {
        setRawPositionError(`原始仓位分析超时（>${RAW_POSITION_TIMEOUT_MS / 1000}s）`);
        return;
      }
      setRawPositionError(e.message);
    } finally {
      clearTimeout(rawTimeoutId);
      if (rawPositionAbortRef.current === rawController) {
        rawPositionAbortRef.current = null;
      }
    }
  }, []);

  const waitForAgentTask = useCallback(async (taskId, { signal, timeoutMs }) => {
    const begin = Date.now();
    let lastRecord = null;
    while (Date.now() - begin < timeoutMs) {
      if (signal?.aborted) {
        throw new Error('已取消本次分析。');
      }
      const res = await api.getAgentLog(taskId, { signal });
      const record = res?.data ?? res;
      lastRecord = record || null;
      const status = String(record?.status || '').toUpperCase();
      if (status === 'SUCCESS' || status === 'FAILED') {
        return record;
      }
      await waitWithAbort(AGENT_LOG_POLL_MS, signal);
    }
    const lastStatus = String(lastRecord?.status || '').toUpperCase();
    throw new Error(`分析超时（>${Math.round(timeoutMs / 1000)}s），当前状态: ${lastStatus || 'UNKNOWN'}`);
  }, []);

  const runAgentByPolling = useCallback(async ({
    normalizedSymbols,
    requestId,
    signal,
    timeoutMs,
  }) => {
    const submitRes = await api.analyzeAgentAsync(
      {
        mode: ANALYZE_MODE,
        symbols: normalizedSymbols,
      },
      { signal },
    );
    const submitData = submitRes?.data ?? submitRes;
    const taskId = Number(submitData?.task_id || 0);
    if (!Number.isFinite(taskId) || taskId <= 0) {
      throw new Error('异步任务创建失败：未返回 task_id');
    }
    if (requestId !== requestIdRef.current) return null;
    setActiveTaskId(taskId);
    await loadAnalysisLogs();

    const taskRecord = await waitForAgentTask(taskId, {
      signal,
      timeoutMs,
    });
    if (requestId !== requestIdRef.current) return null;
    await loadAnalysisLogs();
    setActiveTaskId(null);

    const taskStatus = String(taskRecord?.status || '').toUpperCase();
    if (taskStatus !== 'SUCCESS') {
      throw new Error(taskRecord?.errorMessage || `分析任务失败（${taskStatus || 'UNKNOWN'}）`);
    }
    const result = safeJSONParse(taskRecord?.responseBody, null);
    if (!result || typeof result !== 'object') {
      throw new Error('分析任务完成，但结果为空');
    }
    return result;
  }, [loadAnalysisLogs, waitForAgentTask]);

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
    setActiveTaskId(null);
    setStreamMode('sse');
    setStreamProgress({
      phase: 'connect',
      detail: '正在建立流式连接...',
      step: 0,
      total: 5,
    });
    setStreamText('');
    try {
      let usedFallback = false;
      let result = null;
      try {
        const streamRes = await api.analyzeAgentStream(
          {
            mode: ANALYZE_MODE,
            symbols: normalizedSymbols,
          },
          {
            onProgress: (progress) => {
              if (requestId !== requestIdRef.current) return;
              if (!progress || typeof progress !== 'object') return;
              setStreamProgress({
                phase: String(progress.phase || ''),
                detail: String(progress.detail || ''),
                step: Number(progress.step || 0),
                total: Number(progress.total || 5),
              });
            },
            onToken: (text) => {
              if (requestId !== requestIdRef.current) return;
              if (!text) return;
              setStreamText((prev) => `${prev}${text}`);
            },
            onDone: (payload) => {
              if (requestId !== requestIdRef.current) return;
              const taskId = Number(payload?.task_id || 0);
              if (Number.isFinite(taskId) && taskId > 0) {
                setActiveTaskId(taskId);
              }
            },
          },
          { signal: controller.signal },
        );
        if (requestId !== requestIdRef.current) return;
        result = parseStreamAnalysis(streamRes?.rawText);
        if (!result || typeof result !== 'object') {
          throw new Error('流式分析结果为空');
        }
      } catch (streamErr) {
        if (controller.signal.aborted) {
          throw streamErr;
        }
        usedFallback = true;
        if (requestId !== requestIdRef.current) return;
        setStreamMode('poll');
        setStreamProgress({
          phase: 'fallback',
          detail: `流式连接失败，改用轮询：${streamErr?.message || 'unknown error'}`,
          step: 0,
          total: 5,
        });
        result = await runAgentByPolling({
          normalizedSymbols,
          requestId,
          signal: controller.signal,
          timeoutMs,
        });
      }

      if (requestId !== requestIdRef.current || !result) return;
      if (!usedFallback) {
        await loadAnalysisLogs();
        setActiveTaskId(null);
      }

      setData(result);
      setAnalyzedSymbol(
        normalizedSymbols.length > 0 ? normalizedSymbols.join(', ') : '全部持仓'
      );
      setError(null);
      setStreamProgress(null);
      setStreamMode('');
      await loadEvaluationSummary();
      await fetchRawPositionSnapshot(requestId);
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
        setActiveTaskId(null);
        setStreamMode('');
        setLoading(false);
      }
    }
  }, [fetchRawPositionSnapshot, loadAnalysisLogs, loadEvaluationSummary, runAgentByPolling]);

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

  const onRefreshLogs = useCallback(() => {
    if (logsAbortRef.current) {
      logsAbortRef.current.abort();
    }
    const controller = new AbortController();
    logsAbortRef.current = controller;
    loadAnalysisLogs({ signal: controller.signal });
  }, [loadAnalysisLogs]);

  const onRefreshEval = useCallback(() => {
    if (evalAbortRef.current) {
      evalAbortRef.current.abort();
    }
    const controller = new AbortController();
    evalAbortRef.current = controller;
    loadEvaluationSummary({ signal: controller.signal });
  }, [loadEvaluationSummary]);

  const onChatSend = useCallback(async () => {
    const message = String(chatInput || '').trim();
    if (!message || chatLoading) return;

    const rawSymbols = String(chatSymbols || '').trim();
    let symbols = [];
    if (rawSymbols) {
      symbols = parseSymbolsInput(rawSymbols);
      if (!symbols.length) {
        Alert.alert('参数错误', '对话上下文币种格式无效，请输入如 BTCUSDT,ETH。');
        return;
      }
      setChatSymbols(symbols.join(', '));
    }

    if (chatAbortRef.current) {
      chatAbortRef.current.abort();
    }
    const controller = new AbortController();
    chatAbortRef.current = controller;
    let timedOut = false;
    const timeoutId = setTimeout(() => {
      timedOut = true;
      controller.abort();
    }, CHAT_TIMEOUT_MS);

    const userEntry = {
      id: `u-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`,
      role: 'user',
      text: message,
      actionItems: [],
    };
    setChatMessages((prev) => [...prev, userEntry]);
    setChatInput('');
    setChatLoading(true);
    setError(null);

    try {
      const res = await api.chatAgent(
        {
          message,
          symbols,
        },
        { signal: controller.signal },
      );
      const payload = res?.data ?? res;
      const assistantEntry = {
        id: `a-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`,
        role: 'assistant',
        text: String(payload?.reply || '').trim() || '暂无可用回复。',
        actionItems: Array.isArray(payload?.action_items) ? payload.action_items : [],
      };
      setChatMessages((prev) => [...prev, assistantEntry]);
    } catch (e) {
      if (controller.signal.aborted) {
        setError(timedOut ? `对话超时（>${Math.round(CHAT_TIMEOUT_MS / 1000)}s），请重试。` : '对话已取消。');
      } else {
        setError(e.message);
      }
    } finally {
      clearTimeout(timeoutId);
      if (chatAbortRef.current === controller) {
        chatAbortRef.current = null;
      }
      setChatLoading(false);
    }
  }, [chatInput, chatLoading, chatSymbols]);

  const onClearChat = useCallback(() => {
    if (chatAbortRef.current) {
      chatAbortRef.current.abort();
    }
    setChatMessages([]);
    setChatInput('');
    setError(null);
  }, []);

  return (
    <ScrollView style={s.root} contentContainerStyle={s.content}>
      <View style={s.headerCard}>
        <Text style={s.title}>Agent 智能分析</Text>
        <View style={s.modeRow}>
          <TouchableOpacity
            style={[s.modeBtn, panelMode === 'analysis' ? s.modeBtnActive : null]}
            onPress={() => setPanelMode('analysis')}
            disabled={loading || chatLoading}
          >
            <Text style={[s.modeBtnText, panelMode === 'analysis' ? s.modeBtnTextActive : null]}>分析模式</Text>
          </TouchableOpacity>
          <TouchableOpacity
            style={[s.modeBtn, panelMode === 'chat' ? s.modeBtnActive : null]}
            onPress={() => setPanelMode('chat')}
            disabled={loading || chatLoading}
          >
            <Text style={[s.modeBtnText, panelMode === 'chat' ? s.modeBtnTextActive : null]}>对话模式</Text>
          </TouchableOpacity>
        </View>

        {panelMode === 'analysis' ? (
          <>
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
          </>
        ) : (
          <>
            <Text style={s.subtitle}>输入自然语言问题，Agent 会按需抓取持仓/信号/新闻等数据后回复。</Text>
            <View style={s.symbolRow}>
              <TextInput
                style={s.symbolInput}
                value={chatSymbols}
                onChangeText={setChatSymbols}
                placeholder="可选：对话上下文币种，如 BTCUSDT,ETH"
                placeholderTextColor={C.textDim}
                autoCapitalize="characters"
                autoCorrect={false}
              />
            </View>
            <View style={s.btnRow}>
              <TouchableOpacity
                style={[s.cancelBtn, chatLoading ? s.btnDisabled : null]}
                onPress={onClearChat}
                disabled={chatLoading}
              >
                <Text style={s.cancelBtnText}>清空对话</Text>
              </TouchableOpacity>
            </View>
          </>
        )}
      </View>

      {panelMode === 'analysis' && (
        <>
      {loading && (
        <View style={s.card}>
          <ActivityIndicator size="large" color={C.primary} />
          <Text style={s.loadingText}>
            {streamMode === 'poll'
              ? `正在等待 Agent 异步任务${currentTaskLabel ? ` ${currentTaskLabel}` : ''}，请稍候...`
              : '正在进行流式分析，请稍候...'}
          </Text>
          {!!streamProgress?.detail && (
            <Text style={s.loadingHint}>{streamProgress.detail}</Text>
          )}
          {streamMode !== 'poll' && (
            <View style={s.progressWrap}>
              <View style={s.progressTrack}>
                <View
                  style={[
                    s.progressFill,
                    {
                      width: `${Math.max(
                        0,
                        Math.min(
                          100,
                          (Number(streamProgress?.step || 0) / Math.max(1, Number(streamProgress?.total || 5))) * 100,
                        ),
                      )}%`,
                    },
                  ]}
                />
              </View>
              <Text style={s.progressText}>
                进度 {Number(streamProgress?.step || 0)}/{Number(streamProgress?.total || 5)}
              </Text>
            </View>
          )}
          {!!streamPreview && streamMode !== 'poll' && (
            <Text style={s.streamPreview}>{streamPreview}</Text>
          )}
          <Text style={s.loadingHint}>reasoner 模型可能需要 1-3 分钟；若不想等待可点击上方“取消分析”。</Text>
        </View>
      )}

      {error && !loading && (
        <View style={s.card}>
          <Text style={s.errorTitle}>请求失败</Text>
          <Text style={s.errorText}>{error}</Text>
        </View>
      )}

      <View style={s.card}>
        <View style={s.logHeaderRow}>
          <Text style={s.cardTitle}>Agent分析记录</Text>
          <TouchableOpacity style={s.logRefreshBtn} onPress={onRefreshLogs} disabled={logsLoading}>
            <Text style={[s.logRefreshText, logsLoading ? s.btnDisabled : null]}>{logsLoading ? '刷新中...' : '刷新'}</Text>
          </TouchableOpacity>
        </View>
        {!!logsError && (
          <Text style={[s.emptyText, { color: C.warn }]}>记录加载失败: {logsError}</Text>
        )}
        {!logsError && analysisLogs.length === 0 && (
          <Text style={s.emptyText}>暂无分析记录</Text>
        )}
        {(analysisLogs || []).map((item, idx) => {
          const status = String(item?.status || '').toUpperCase();
          const source = String(item?.source || '').toUpperCase();
          const statusStyle = status === 'SUCCESS'
            ? s.logStatusSuccess
            : status === 'FAILED'
              ? s.logStatusFailed
              : status === 'RUNNING'
                ? s.logStatusRunning
                : s.logStatusPending;
          const symbols = String(item?.symbols || '').trim() || '全部持仓';
          return (
            <View key={`log-${item?.id || idx}`} style={s.logRow}>
              <View style={s.logRowTop}>
                <Text style={s.logTitle}>#{item?.id || '--'} · {item?.mode || '-'}</Text>
                <Text style={[s.logStatusTag, statusStyle]}>{status || 'UNKNOWN'}</Text>
              </View>
              <Text style={s.logMeta}>来源: {formatLogSource(source)}</Text>
              <Text style={s.logMeta}>标的: {symbols}</Text>
              <Text style={s.logMeta}>执行: {item?.execute ? '是' : '否'} · 耗时: {Number(item?.durationMs || 0)}ms</Text>
              <Text style={s.logMeta}>时间: {formatTimestamp(item?.createdAt)}</Text>
              {!!item?.errorMessage && <Text style={s.logError}>{item.errorMessage}</Text>}
            </View>
          );
        })}
      </View>

      <View style={s.card}>
        <View style={s.logHeaderRow}>
          <Text style={s.cardTitle}>Agent评估（近{AGENT_EVAL_DAYS}天）</Text>
          <TouchableOpacity style={s.logRefreshBtn} onPress={onRefreshEval} disabled={evalLoading}>
            <Text style={[s.logRefreshText, evalLoading ? s.btnDisabled : null]}>{evalLoading ? '刷新中...' : '刷新'}</Text>
          </TouchableOpacity>
        </View>
        {!!evalError && (
          <Text style={[s.emptyText, { color: C.warn }]}>评估加载失败: {evalError}</Text>
        )}
        {!evalError && (
          <>
            <View style={s.statsRow}>
              <Text style={s.statText}>建议总数: {evalView.totalSuggestions}</Text>
              <Text style={s.statText}>命中率 1H: {formatPct(evalView.hitRate1H)}</Text>
              <Text style={s.statText}>命中率 4H: {formatPct(evalView.hitRate4H)}</Text>
              <Text style={s.statText}>命中率 24H: {formatPct(evalView.hitRate24H)}</Text>
            </View>
            <View style={s.statsRow}>
              <Text style={s.statText}>平均收益 1H: {formatSignedPct(evalView.avgPnl1H)}</Text>
              <Text style={s.statText}>平均收益 24H: {formatSignedPct(evalView.avgPnl24H)}</Text>
              <Text style={s.statText}>最佳模式: {evalView.bestMode}</Text>
            </View>
            <Text style={s.statText}>
              最差币种: {evalView.worstSymbols.length > 0 ? evalView.worstSymbols.join(', ') : '-'}
            </Text>
          </>
        )}
      </View>

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
        </>
      )}

      {panelMode === 'chat' && (
        <>
          {!!error && !chatLoading && (
            <View style={s.card}>
              <Text style={s.errorTitle}>请求失败</Text>
              <Text style={s.errorText}>{error}</Text>
            </View>
          )}

          <View style={s.card}>
            <View style={s.chatHeaderRow}>
              <Text style={s.cardTitle}>对话记录</Text>
              {chatLoading && (
                <View style={s.chatLoadingWrap}>
                  <ActivityIndicator size="small" color={C.primary} />
                  <Text style={s.chatLoadingText}>Agent 回复中...</Text>
                </View>
              )}
            </View>
            {chatMessages.length === 0 && (
              <Text style={s.emptyText}>暂无对话。示例：当前 BTC 仓位风险大吗？是否要先减仓？</Text>
            )}
            {chatMessages.map((msg) => (
              <View
                key={msg.id}
                style={[
                  s.chatBubble,
                  msg.role === 'user' ? s.chatBubbleUser : s.chatBubbleAssistant,
                ]}
              >
                <Text style={s.chatRole}>{msg.role === 'user' ? '你' : 'Agent'}</Text>
                <Text style={s.chatText}>{msg.text}</Text>
                {(msg.actionItems || []).length > 0 && (
                  <View style={s.chatActionWrap}>
                    {(msg.actionItems || []).map((it, idx) => (
                      <Text key={`${msg.id}-act-${idx}`} style={s.chatActionText}>
                        - {it.symbol || '--'} · {it.action || '--'} · {it.detail || '-'}
                      </Text>
                    ))}
                  </View>
                )}
              </View>
            ))}
          </View>

          <View style={s.card}>
            <Text style={s.cardTitle}>发送问题</Text>
            <TextInput
              style={s.chatInput}
              value={chatInput}
              onChangeText={setChatInput}
              placeholder="例如：结合我的持仓和最近新闻，给出今天的风险控制建议。"
              placeholderTextColor={C.textDim}
              multiline
              editable={!chatLoading}
            />
            <TouchableOpacity
              style={[s.primaryBtn, (!chatInput.trim() || chatLoading) ? s.btnDisabled : null]}
              onPress={onChatSend}
              disabled={!chatInput.trim() || chatLoading}
            >
              <Text style={s.primaryBtnText}>{chatLoading ? '发送中...' : '发送'}</Text>
            </TouchableOpacity>
          </View>
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
  modeRow: {
    flexDirection: 'row',
    gap: spacing.sm,
    marginTop: spacing.xs,
    marginBottom: spacing.xs,
  },
  modeBtn: {
    flex: 1,
    borderWidth: 1,
    borderColor: C.border,
    borderRadius: radius.sm,
    paddingVertical: 8,
    alignItems: 'center',
    backgroundColor: C.bg,
  },
  modeBtnActive: {
    borderColor: C.primary,
    backgroundColor: C.primaryBg,
  },
  modeBtnText: {
    color: C.textDim,
    fontSize: fontSize.sm,
    fontWeight: '700',
  },
  modeBtnTextActive: {
    color: C.primary,
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
  logHeaderRow: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'center',
  },
  logRefreshBtn: {
    borderWidth: 1,
    borderColor: C.border,
    borderRadius: radius.sm,
    paddingHorizontal: spacing.sm,
    paddingVertical: 4,
  },
  logRefreshText: {
    color: C.textDim,
    fontSize: fontSize.xs,
    fontWeight: '700',
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
  progressWrap: {
    gap: 6,
    marginTop: 4,
  },
  progressTrack: {
    height: 8,
    borderRadius: 999,
    backgroundColor: C.bg,
    borderWidth: 1,
    borderColor: C.border,
    overflow: 'hidden',
  },
  progressFill: {
    height: '100%',
    backgroundColor: C.primary,
  },
  progressText: {
    color: C.textDim,
    fontSize: fontSize.xs,
    textAlign: 'center',
  },
  streamPreview: {
    color: C.text,
    fontSize: fontSize.xs,
    lineHeight: 18,
    borderWidth: 1,
    borderColor: C.border,
    borderRadius: radius.sm,
    padding: spacing.sm,
    backgroundColor: C.bg,
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
  logRow: {
    borderWidth: 1,
    borderColor: C.border,
    borderRadius: radius.sm,
    padding: spacing.sm,
    gap: 3,
  },
  logRowTop: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'center',
  },
  logTitle: {
    color: C.text,
    fontSize: fontSize.sm,
    fontWeight: '800',
    flex: 1,
    marginRight: spacing.sm,
  },
  logStatusTag: {
    fontSize: fontSize.xs,
    fontWeight: '800',
    paddingHorizontal: 8,
    paddingVertical: 2,
    borderRadius: 999,
    overflow: 'hidden',
  },
  logStatusSuccess: {
    color: C.success,
    backgroundColor: C.successBg,
  },
  logStatusFailed: {
    color: C.danger,
    backgroundColor: C.dangerBg,
  },
  logStatusRunning: {
    color: C.warn,
    backgroundColor: C.warnBg,
  },
  logStatusPending: {
    color: C.textDim,
    backgroundColor: C.bg,
    borderWidth: 1,
    borderColor: C.border,
  },
  logMeta: {
    color: C.textDim,
    fontSize: fontSize.xs,
  },
  logError: {
    color: C.warn,
    fontSize: fontSize.xs,
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
  chatHeaderRow: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'center',
  },
  chatLoadingWrap: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 6,
  },
  chatLoadingText: {
    color: C.textDim,
    fontSize: fontSize.xs,
  },
  chatBubble: {
    borderWidth: 1,
    borderRadius: radius.sm,
    padding: spacing.sm,
    gap: 4,
  },
  chatBubbleUser: {
    borderColor: C.primary,
    backgroundColor: C.primaryBg,
  },
  chatBubbleAssistant: {
    borderColor: C.border,
    backgroundColor: C.bg,
  },
  chatRole: {
    color: C.textDim,
    fontSize: fontSize.xs,
    fontWeight: '700',
  },
  chatText: {
    color: C.text,
    fontSize: fontSize.sm,
    lineHeight: 21,
  },
  chatActionWrap: {
    marginTop: 2,
    gap: 2,
  },
  chatActionText: {
    color: C.textDim,
    fontSize: fontSize.xs,
    lineHeight: 18,
  },
  chatInput: {
    borderWidth: 1,
    borderColor: C.border,
    borderRadius: radius.sm,
    backgroundColor: C.bg,
    color: C.text,
    minHeight: 88,
    paddingHorizontal: spacing.sm,
    paddingVertical: spacing.sm,
    fontSize: fontSize.sm,
    textAlignVertical: 'top',
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

function waitWithAbort(ms, signal) {
  return new Promise((resolve, reject) => {
    if (signal?.aborted) {
      reject(new Error('aborted'));
      return;
    }
    const timer = setTimeout(() => {
      if (signal) {
        signal.removeEventListener('abort', onAbort);
      }
      resolve();
    }, ms);
    const onAbort = () => {
      clearTimeout(timer);
      signal.removeEventListener('abort', onAbort);
      reject(new Error('aborted'));
    };
    if (signal) {
      signal.addEventListener('abort', onAbort, { once: true });
    }
  });
}

function safeJSONParse(text, fallback) {
  if (typeof text !== 'string' || !text.trim()) return fallback;
  try {
    return JSON.parse(text);
  } catch (_e) {
    return fallback;
  }
}

function parseStreamAnalysis(rawText) {
  const raw = String(rawText || '').trim();
  if (!raw) return null;

  const direct = safeJSONParse(raw, null);
  if (direct && typeof direct === 'object') return direct;

  const cleaned = raw
    .replace(/^```json\s*/i, '')
    .replace(/^```\s*/i, '')
    .replace(/\s*```$/i, '')
    .trim();
  const fenced = safeJSONParse(cleaned, null);
  if (fenced && typeof fenced === 'object') return fenced;

  return {
    summary: raw,
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

function formatTimestamp(value) {
  const ts = Date.parse(String(value || ''));
  if (!Number.isFinite(ts)) return '--';
  return new Date(ts).toLocaleString('zh-CN', { hour12: false });
}

function formatLogSource(source) {
  if (source === 'DAILY_AUTO') return '每日自动';
  if (source === 'APP_MANUAL') return 'App手动';
  return source || '--';
}

function formatPct(value) {
  const n = Number(value);
  if (!Number.isFinite(n)) return '--';
  return `${n.toFixed(2)}%`;
}

function formatSignedPct(value) {
  const n = Number(value);
  if (!Number.isFinite(n)) return '--';
  if (n === 0) return '0.00%';
  return `${n > 0 ? '+' : ''}${n.toFixed(2)}%`;
}

function formatPrice(value) {
  const p = Number(value || 0);
  if (!Number.isFinite(p) || p <= 0) return '--';
  if (p >= 1000) return p.toFixed(1);
  if (p >= 1) return p.toFixed(2);
  return p.toFixed(4);
}
