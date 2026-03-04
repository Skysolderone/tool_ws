import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import {
  Alert,
  ScrollView,
  StyleSheet,
  Text,
  TextInput,
  TouchableOpacity,
  Vibration,
  View,
} from 'react-native';
import {
  AUTH_TOKEN,
  WS_BIG_TRADE_BASE,
  WS_MARKET_RANGE_BASE,
  WS_MARKET_SPIKE_BASE,
} from '../services/api';
import { colors, fontSize, radius, spacing } from '../services/theme';

const RECONNECT_MS = 3000;
const DEFAULT_SYMBOL = 'BTCUSDT';
const DEFAULT_THRESHOLD = 100000;
const DEFAULT_SPIKE_THRESHOLD_PCT = 1.5;
const DEFAULT_SPIKE_WINDOW_SEC = 30;
const DEFAULT_RANGE_COOLDOWN_SEC = 10;
const DEFAULT_RANGE_SUPPRESS_SEC = 60;
const DEFAULT_SPIKE_COOLDOWN_SEC = 15;
const DEFAULT_SPIKE_SUPPRESS_SEC = 60;
const DEFAULT_SPIKE_WARN_Q = 95;
const DEFAULT_SPIKE_CRITICAL_Q = 99;
const DEFAULT_SPIKE_MIN_SAMPLES = 40;
const DEFAULT_NOTIFY_CONFIG = Object.freeze({
  warnPopup: false,
  warnVibrate: false,
  criticalPopup: true,
  criticalVibrate: true,
});

function toNum(v) {
  const n = Number(v);
  return Number.isFinite(n) ? n : 0;
}

function fmtUsd(v) {
  const n = toNum(v);
  if (Math.abs(n) >= 1e9) return `${(n / 1e9).toFixed(2)}B`;
  if (Math.abs(n) >= 1e6) return `${(n / 1e6).toFixed(2)}M`;
  if (Math.abs(n) >= 1e3) return `${(n / 1e3).toFixed(1)}K`;
  return n.toFixed(0);
}

function fmtTime(ms) {
  const t = toNum(ms);
  if (t <= 0) return '--';
  return new Date(t).toLocaleTimeString('zh-CN', { hour12: false });
}

function fmtPct(v) {
  const n = toNum(v);
  return `${n >= 0 ? '+' : ''}${n.toFixed(2)}%`;
}

export default function MarketMonitorPanel({ onHasNew, onMonitorEvent, notifyConfig }) {
  const [symbolInput, setSymbolInput] = useState(DEFAULT_SYMBOL);
  const [thresholdInput, setThresholdInput] = useState(String(DEFAULT_THRESHOLD));
  const [symbol, setSymbol] = useState(DEFAULT_SYMBOL);
  const [threshold, setThreshold] = useState(DEFAULT_THRESHOLD);
  const [wsConnected, setWsConnected] = useState(false);
  const [bigEvents, setBigEvents] = useState([]);
  const [wsError, setWsError] = useState('');

  const [alertSymbolInput, setAlertSymbolInput] = useState(DEFAULT_SYMBOL);
  const [alertLowerInput, setAlertLowerInput] = useState('');
  const [alertUpperInput, setAlertUpperInput] = useState('');
  const [alertCooldownInput, setAlertCooldownInput] = useState(String(DEFAULT_RANGE_COOLDOWN_SEC));
  const [alertSuppressInput, setAlertSuppressInput] = useState(String(DEFAULT_RANGE_SUPPRESS_SEC));
  const [alerts, setAlerts] = useState([]);
  const [alertLogs, setAlertLogs] = useState([]);
  const [alertWsConnected, setAlertWsConnected] = useState(false);
  const [alertWsError, setAlertWsError] = useState('');
  const [spikeSymbolInput, setSpikeSymbolInput] = useState(DEFAULT_SYMBOL);
  const [spikeThresholdInput, setSpikeThresholdInput] = useState(String(DEFAULT_SPIKE_THRESHOLD_PCT));
  const [spikeWindowInput, setSpikeWindowInput] = useState(String(DEFAULT_SPIKE_WINDOW_SEC));
  const [spikeCooldownInput, setSpikeCooldownInput] = useState(String(DEFAULT_SPIKE_COOLDOWN_SEC));
  const [spikeSuppressInput, setSpikeSuppressInput] = useState(String(DEFAULT_SPIKE_SUPPRESS_SEC));
  const [spikeDynamicEnabled, setSpikeDynamicEnabled] = useState(true);
  const [spikeWarnQInput, setSpikeWarnQInput] = useState(String(DEFAULT_SPIKE_WARN_Q));
  const [spikeCriticalQInput, setSpikeCriticalQInput] = useState(String(DEFAULT_SPIKE_CRITICAL_Q));
  const [spikeMinSamplesInput, setSpikeMinSamplesInput] = useState(String(DEFAULT_SPIKE_MIN_SAMPLES));
  const [spikeRules, setSpikeRules] = useState([]);
  const [spikeLogs, setSpikeLogs] = useState([]);
  const [spikeWsConnected, setSpikeWsConnected] = useState(false);
  const [spikeWsError, setSpikeWsError] = useState('');

  const bigWsRef = useRef(null);
  const bigReconnectRef = useRef(null);
  const alertWsRef = useRef(null);
  const alertReconnectRef = useRef(null);
  const alertSnapshotTimerRef = useRef(null);
  const spikeWsRef = useRef(null);
  const spikeReconnectRef = useRef(null);
  const spikeSnapshotTimerRef = useRef(null);
  const mountedRef = useRef(false);
  const onHasNewRef = useRef(onHasNew);
  const onMonitorEventRef = useRef(onMonitorEvent);
  const notifyConfigRef = useRef(notifyConfig);

  useEffect(() => {
    onHasNewRef.current = onHasNew;
  }, [onHasNew]);

  useEffect(() => {
    onMonitorEventRef.current = onMonitorEvent;
  }, [onMonitorEvent]);

  useEffect(() => {
    notifyConfigRef.current = notifyConfig;
  }, [notifyConfig]);

  const resolveNotifyPolicy = useCallback((severity) => {
    const config = {
      ...DEFAULT_NOTIFY_CONFIG,
      ...(notifyConfigRef.current || {}),
    };
    if (severity === 'critical') {
      return {
        popup: !!config.criticalPopup,
        vibrate: !!config.criticalVibrate,
      };
    }
    if (severity === 'warn') {
      return {
        popup: !!config.warnPopup,
        vibrate: !!config.warnVibrate,
      };
    }
    return { popup: false, vibrate: false };
  }, []);

  const emitMonitorEvent = useCallback((evt = {}) => {
    onMonitorEventRef.current?.({
      eventId: evt.eventId || `market::${evt.type || 'event'}::${evt.symbol || '-'}::${evt.ts || Date.now()}`,
      ts: toNum(evt.ts || Date.now()),
      source: 'market',
      severity: evt.severity || 'info',
      symbol: evt.symbol || '',
      strategyId: evt.strategyId || '',
      type: evt.type || 'event',
      message: evt.message || '',
      payload: evt.payload || {},
    });
  }, []);

  const applyBigConfig = useCallback(() => {
    const sym = symbolInput.trim().toUpperCase();
    if (!sym) {
      Alert.alert('参数错误', '请输入交易对，如 BTCUSDT');
      return;
    }
    const val = toNum(thresholdInput);
    if (!Number.isFinite(val) || val < 1000) {
      Alert.alert('参数错误', '大单阈值至少为 1000 U');
      return;
    }
    setSymbol(sym);
    setThreshold(val);
  }, [symbolInput, thresholdInput]);

  useEffect(() => {
    mountedRef.current = true;
    return () => {
      mountedRef.current = false;
      if (bigReconnectRef.current) {
        clearTimeout(bigReconnectRef.current);
      }
      if (bigWsRef.current) {
        try { bigWsRef.current.close(); } catch (_) {}
        bigWsRef.current = null;
      }
      if (alertReconnectRef.current) {
        clearTimeout(alertReconnectRef.current);
      }
      if (alertSnapshotTimerRef.current) {
        clearInterval(alertSnapshotTimerRef.current);
      }
      if (alertWsRef.current) {
        try { alertWsRef.current.close(); } catch (_) {}
        alertWsRef.current = null;
      }
      if (spikeReconnectRef.current) {
        clearTimeout(spikeReconnectRef.current);
      }
      if (spikeSnapshotTimerRef.current) {
        clearInterval(spikeSnapshotTimerRef.current);
      }
      if (spikeWsRef.current) {
        try { spikeWsRef.current.close(); } catch (_) {}
        spikeWsRef.current = null;
      }
    };
  }, []);

  useEffect(() => {
    if (!mountedRef.current) return undefined;

    const connect = () => {
      const url = `${WS_BIG_TRADE_BASE}?symbol=${encodeURIComponent(symbol)}&minNotional=${threshold}&token=${AUTH_TOKEN}`;
      const ws = new WebSocket(url);
      bigWsRef.current = ws;

      ws.onopen = () => {
        setWsConnected(true);
        setWsError('');
      };

      ws.onmessage = (event) => {
        let msg;
        try {
          msg = JSON.parse(event.data);
        } catch (_) {
          return;
        }
        if (!msg || msg.channel !== 'bigTrade') return;

        const evt = {
          id: `${msg.tradeId || '-'}-${msg.t || Date.now()}`,
          symbol: msg.s || symbol,
          side: msg.side || 'BUY',
          price: toNum(msg.p),
          qty: toNum(msg.q),
          notional: toNum(msg.n),
          time: toNum(msg.t || Date.now()),
        };
        onHasNewRef.current?.(true);
        emitMonitorEvent({
          eventId: `market::big_trade::${evt.id}`,
          ts: evt.time,
          severity: evt.notional >= threshold * 2 ? 'warn' : 'info',
          symbol: evt.symbol,
          type: 'big_trade',
          message: `${evt.symbol} ${evt.side} 大单 $${fmtUsd(evt.notional)}`,
          payload: evt,
        });
        setBigEvents((prev) => [evt, ...prev].slice(0, 150));
      };

      ws.onerror = () => {
        setWsConnected(false);
        setWsError('大单流连接异常');
      };

      ws.onclose = () => {
        setWsConnected(false);
        if (!mountedRef.current) return;
        bigReconnectRef.current = setTimeout(connect, RECONNECT_MS);
      };
    };

    setBigEvents([]);
    connect();

    return () => {
      if (bigReconnectRef.current) {
        clearTimeout(bigReconnectRef.current);
        bigReconnectRef.current = null;
      }
      if (bigWsRef.current) {
        try { bigWsRef.current.close(); } catch (_) {}
        bigWsRef.current = null;
      }
    };
  }, [emitMonitorEvent, symbol, threshold]);

  const bigSummary = useMemo(() => {
    const recent = bigEvents.slice(0, 80);
    let buyNotional = 0;
    let sellNotional = 0;
    recent.forEach((x) => {
      if (x.side === 'SELL') sellNotional += x.notional;
      else buyNotional += x.notional;
    });
    const total = buyNotional + sellNotional;
    const buyPct = total > 0 ? (buyNotional / total) * 100 : 0;
    return {
      count: recent.length,
      buyNotional,
      sellNotional,
      bias: buyPct >= 55 ? '主动买偏强' : buyPct <= 45 ? '主动卖偏强' : '相对均衡',
      biasColor: buyPct >= 55 ? colors.greenLight : buyPct <= 45 ? colors.redLight : colors.goldLight,
    };
  }, [bigEvents]);

  const sendAlertWs = useCallback((payload) => {
    const ws = alertWsRef.current;
    if (!ws || ws.readyState !== 1) return false;
    try {
      ws.send(JSON.stringify(payload));
      return true;
    } catch (_) {
      return false;
    }
  }, []);

  useEffect(() => {
    if (!mountedRef.current) return undefined;
    let stopped = false;

    const connect = () => {
      if (stopped) return;
      const ws = new WebSocket(`${WS_MARKET_RANGE_BASE}?token=${AUTH_TOKEN}`);
      alertWsRef.current = ws;

      ws.onopen = () => {
        setAlertWsConnected(true);
        setAlertWsError('');
        sendAlertWs({ action: 'snapshot' });
        if (alertSnapshotTimerRef.current) clearInterval(alertSnapshotTimerRef.current);
        alertSnapshotTimerRef.current = setInterval(() => {
          sendAlertWs({ action: 'snapshot' });
        }, 30000);
      };

      ws.onmessage = (evt) => {
        let msg;
        try {
          msg = JSON.parse(evt.data);
        } catch (_) {
          return;
        }
        if (!msg || msg.channel !== 'marketRange') return;
        if (msg.type === 'snapshot') {
          setAlerts(Array.isArray(msg.rules) ? msg.rules : []);
          return;
        }
        if (msg.type === 'event' && msg.event) {
          const event = msg.event;
          const eventSeverityRaw = String(event.severity || 'warn').toLowerCase();
          const eventSeverity = eventSeverityRaw === 'critical' ? 'critical' : eventSeverityRaw === 'warn' ? 'warn' : 'info';
          onHasNewRef.current?.(true);
          emitMonitorEvent({
            eventId: `market::range_break::${event.id || `${event.ruleId}-${event.time}`}`,
            ts: toNum(event.time || Date.now()),
            severity: eventSeverity,
            symbol: String(event.symbol || '').toUpperCase(),
            strategyId: event.ruleId ? `range:${event.ruleId}` : 'range',
            type: 'range_break',
            message: `${event.symbol} ${event.direction} 区间 ${event.lower}-${event.upper} 当前 ${toNum(event.price).toFixed(4)}`,
            payload: {
              ...event,
              suppressSec: toNum(event.suppressSec || DEFAULT_RANGE_SUPPRESS_SEC),
            },
          });
          const notify = resolveNotifyPolicy(eventSeverity);
          if (notify.vibrate) {
            Vibration.vibrate(eventSeverity === 'critical' ? [0, 220, 120, 220] : 180);
          }
          setAlertLogs((prev) => [event, ...prev].slice(0, 60));
          setAlerts((prev) => prev.map((r) => (
            r.id === event.ruleId
              ? { ...r, lastPrice: toNum(event.price), lastInside: false, lastTriggerAt: toNum(event.time) }
              : r
          )));
          if (notify.popup) {
            Alert.alert(
              `价格预警触发（${eventSeverity.toUpperCase()}）`,
              `${event.symbol} ${event.direction}\n区间 ${event.lower} - ${event.upper}\n当前 ${toNum(event.price).toFixed(4)}\n抑制 ${toNum(event.suppressSec || DEFAULT_RANGE_SUPPRESS_SEC)}s`,
            );
          }
          return;
        }
        if (msg.type === 'error') {
          setAlertWsError(msg.error || '区间预警服务异常');
        }
      };

      ws.onerror = () => {
        setAlertWsConnected(false);
        setAlertWsError('后端区间预警连接异常');
      };

      ws.onclose = () => {
        setAlertWsConnected(false);
        if (alertSnapshotTimerRef.current) {
          clearInterval(alertSnapshotTimerRef.current);
          alertSnapshotTimerRef.current = null;
        }
        if (stopped || !mountedRef.current) return;
        alertReconnectRef.current = setTimeout(connect, RECONNECT_MS);
      };
    };

    connect();
    return () => {
      stopped = true;
      if (alertReconnectRef.current) {
        clearTimeout(alertReconnectRef.current);
        alertReconnectRef.current = null;
      }
      if (alertSnapshotTimerRef.current) {
        clearInterval(alertSnapshotTimerRef.current);
        alertSnapshotTimerRef.current = null;
      }
      if (alertWsRef.current) {
        try { alertWsRef.current.close(); } catch (_) {}
        alertWsRef.current = null;
      }
    };
  }, [emitMonitorEvent, resolveNotifyPolicy, sendAlertWs]);

  const sendSpikeWs = useCallback((payload) => {
    const ws = spikeWsRef.current;
    if (!ws || ws.readyState !== 1) return false;
    try {
      ws.send(JSON.stringify(payload));
      return true;
    } catch (_) {
      return false;
    }
  }, []);

  useEffect(() => {
    if (!mountedRef.current) return undefined;
    let stopped = false;

    const connect = () => {
      if (stopped) return;
      const ws = new WebSocket(`${WS_MARKET_SPIKE_BASE}?token=${AUTH_TOKEN}`);
      spikeWsRef.current = ws;

      ws.onopen = () => {
        setSpikeWsConnected(true);
        setSpikeWsError('');
        sendSpikeWs({ action: 'snapshot' });
        if (spikeSnapshotTimerRef.current) clearInterval(spikeSnapshotTimerRef.current);
        spikeSnapshotTimerRef.current = setInterval(() => {
          sendSpikeWs({ action: 'snapshot' });
        }, 30000);
      };

      ws.onmessage = (evt) => {
        let msg;
        try {
          msg = JSON.parse(evt.data);
        } catch (_) {
          return;
        }
        if (!msg || msg.channel !== 'marketSpike') return;
        if (msg.type === 'snapshot') {
          setSpikeRules(Array.isArray(msg.rules) ? msg.rules : []);
          return;
        }
        if (msg.type === 'event' && msg.event) {
          const event = msg.event;
          const eventSeverityRaw = String(event.severity || 'warn').toLowerCase();
          const eventSeverity = eventSeverityRaw === 'critical' ? 'critical' : eventSeverityRaw === 'warn' ? 'warn' : 'info';
          const triggerPct = toNum(event.triggerPct || event.thresholdPct);
          const dynamicTag = event.dynamic ? '动态' : '静态';
          onHasNewRef.current?.(true);
          emitMonitorEvent({
            eventId: `market::spike::${event.id || `${event.ruleId}-${event.time}`}`,
            ts: toNum(event.time || Date.now()),
            severity: eventSeverity,
            symbol: String(event.symbol || '').toUpperCase(),
            strategyId: event.ruleId ? `spike:${event.ruleId}` : 'spike',
            type: 'market_spike',
            message: `${event.symbol} ${event.direction} ${fmtPct(event.movePct)} | ${event.windowSec}s | ${dynamicTag}阈值 ${triggerPct.toFixed(2)}%`,
            payload: {
              ...event,
              suppressSec: toNum(event.suppressSec || DEFAULT_SPIKE_SUPPRESS_SEC),
            },
          });
          const notify = resolveNotifyPolicy(eventSeverity);
          if (notify.vibrate) {
            Vibration.vibrate(eventSeverity === 'critical' ? [0, 220, 120, 220] : 180);
          }
          setSpikeLogs((prev) => [event, ...prev].slice(0, 80));
          setSpikeRules((prev) => prev.map((r) => (
            r.id === event.ruleId
              ? { ...r, lastPrice: toNum(event.price), lastMovePct: toNum(event.movePct), lastTriggerAt: toNum(event.time) }
              : r
          )));
          if (notify.popup) {
            Alert.alert(
              `突发波动预警（${eventSeverity.toUpperCase()}）`,
              `${event.symbol} ${event.direction} ${fmtPct(event.movePct)}\n窗口 ${event.windowSec}s ${dynamicTag}阈值 ${triggerPct.toFixed(2)}%\n基准 ${toNum(event.basePrice).toFixed(4)} 当前 ${toNum(event.price).toFixed(4)}\n抑制 ${toNum(event.suppressSec || DEFAULT_SPIKE_SUPPRESS_SEC)}s`,
            );
          }
          return;
        }
        if (msg.type === 'error') {
          setSpikeWsError(msg.error || '突发预警服务异常');
        }
      };

      ws.onerror = () => {
        setSpikeWsConnected(false);
        setSpikeWsError('后端突发监控连接异常');
      };

      ws.onclose = () => {
        setSpikeWsConnected(false);
        if (spikeSnapshotTimerRef.current) {
          clearInterval(spikeSnapshotTimerRef.current);
          spikeSnapshotTimerRef.current = null;
        }
        if (stopped || !mountedRef.current) return;
        spikeReconnectRef.current = setTimeout(connect, RECONNECT_MS);
      };
    };

    connect();
    return () => {
      stopped = true;
      if (spikeReconnectRef.current) {
        clearTimeout(spikeReconnectRef.current);
        spikeReconnectRef.current = null;
      }
      if (spikeSnapshotTimerRef.current) {
        clearInterval(spikeSnapshotTimerRef.current);
        spikeSnapshotTimerRef.current = null;
      }
      if (spikeWsRef.current) {
        try { spikeWsRef.current.close(); } catch (_) {}
        spikeWsRef.current = null;
      }
    };
  }, [emitMonitorEvent, resolveNotifyPolicy, sendSpikeWs]);

  const addAlert = useCallback(() => {
    const symbolUpper = alertSymbolInput.trim().toUpperCase();
    const lower = toNum(alertLowerInput);
    const upper = toNum(alertUpperInput);
    const cooldownSec = Math.round(toNum(alertCooldownInput));
    const suppressSec = Math.round(toNum(alertSuppressInput));
    if (!symbolUpper) {
      Alert.alert('参数错误', '请输入预警交易对');
      return;
    }
    if (!Number.isFinite(lower) || !Number.isFinite(upper) || lower <= 0 || upper <= 0 || lower >= upper) {
      Alert.alert('参数错误', '请填写有效区间，且下限 < 上限');
      return;
    }
    if (!Number.isFinite(cooldownSec) || cooldownSec < 5 || cooldownSec > 600) {
      Alert.alert('参数错误', '冷却时间请填写 5~600 秒');
      return;
    }
    if (!Number.isFinite(suppressSec) || suppressSec < 10 || suppressSec > 1800) {
      Alert.alert('参数错误', '抑制时间请填写 10~1800 秒');
      return;
    }
    const ok = sendAlertWs({
      action: 'addRule',
      symbol: symbolUpper,
      lower,
      upper,
      cooldownSec,
      suppressSec,
      enabled: true,
    });
    if (!ok) {
      Alert.alert('连接异常', '后端区间预警未连接，请稍后重试');
      return;
    }
    setAlertSymbolInput(symbolUpper);
    setAlertLowerInput('');
    setAlertUpperInput('');
  }, [alertCooldownInput, alertLowerInput, alertSuppressInput, alertSymbolInput, alertUpperInput, sendAlertWs]);

  const addSpikeRule = useCallback(() => {
    const symbolUpper = spikeSymbolInput.trim().toUpperCase();
    const thresholdPct = toNum(spikeThresholdInput);
    const windowSec = toNum(spikeWindowInput);
    const cooldownSec = Math.round(toNum(spikeCooldownInput));
    const suppressSec = Math.round(toNum(spikeSuppressInput));
    const warnQuantile = toNum(spikeWarnQInput);
    const criticalQuantile = toNum(spikeCriticalQInput);
    const minSamples = Math.round(toNum(spikeMinSamplesInput));
    if (!symbolUpper) {
      Alert.alert('参数错误', '请输入预警交易对');
      return;
    }
    if (!Number.isFinite(thresholdPct) || thresholdPct <= 0 || thresholdPct > 30) {
      Alert.alert('参数错误', '阈值请填写 0~30 的百分比');
      return;
    }
    if (!Number.isFinite(windowSec) || windowSec < 5 || windowSec > 3600) {
      Alert.alert('参数错误', '窗口请填写 5~3600 秒');
      return;
    }
    if (!Number.isFinite(cooldownSec) || cooldownSec < 5 || cooldownSec > 600) {
      Alert.alert('参数错误', '冷却时间请填写 5~600 秒');
      return;
    }
    if (!Number.isFinite(suppressSec) || suppressSec < 10 || suppressSec > 1800) {
      Alert.alert('参数错误', '抑制时间请填写 10~1800 秒');
      return;
    }
    if (spikeDynamicEnabled) {
      if (!Number.isFinite(warnQuantile) || warnQuantile < 50 || warnQuantile > 99.9) {
        Alert.alert('参数错误', '动态警告分位请填写 50~99.9');
        return;
      }
      if (!Number.isFinite(criticalQuantile) || criticalQuantile < warnQuantile || criticalQuantile > 99.9) {
        Alert.alert('参数错误', '动态危险分位需 >= 警告分位，且不超过 99.9');
        return;
      }
      if (!Number.isFinite(minSamples) || minSamples < 10 || minSamples > 500) {
        Alert.alert('参数错误', '最小样本请填写 10~500');
        return;
      }
    }
    const ok = sendSpikeWs({
      action: 'addRule',
      symbol: symbolUpper,
      thresholdPct,
      windowSec: Math.round(windowSec),
      cooldownSec,
      suppressSec,
      dynamic: spikeDynamicEnabled,
      warnQuantile,
      criticalQuantile,
      minSamples,
      enabled: true,
    });
    if (!ok) {
      Alert.alert('连接异常', '后端突发监控未连接，请稍后重试');
      return;
    }
  }, [
    sendSpikeWs,
    spikeCooldownInput,
    spikeCriticalQInput,
    spikeDynamicEnabled,
    spikeMinSamplesInput,
    spikeSuppressInput,
    spikeSymbolInput,
    spikeThresholdInput,
    spikeWarnQInput,
    spikeWindowInput,
  ]);

  const toggleSpikeRule = useCallback((id) => {
    if (!sendSpikeWs({ action: 'toggleRule', id })) {
      Alert.alert('连接异常', '后端突发监控未连接，请稍后重试');
    }
  }, [sendSpikeWs]);

  const removeSpikeRule = useCallback((id) => {
    if (!sendSpikeWs({ action: 'removeRule', id })) {
      Alert.alert('连接异常', '后端突发监控未连接，请稍后重试');
    }
  }, [sendSpikeWs]);

  const toggleAlert = useCallback((id) => {
    if (!sendAlertWs({ action: 'toggleRule', id })) {
      Alert.alert('连接异常', '后端区间预警未连接，请稍后重试');
    }
  }, [sendAlertWs]);

  const removeAlert = useCallback((id) => {
    if (!sendAlertWs({ action: 'removeRule', id })) {
      Alert.alert('连接异常', '后端区间预警未连接，请稍后重试');
    }
  }, [sendAlertWs]);

  return (
    <View style={styles.card}>
      <Text style={styles.title}>市场监控增强</Text>

      <View style={styles.section}>
        <Text style={styles.sectionTitle}>Binance 大单监控 (aggTrade)</Text>
        <View style={styles.inputRow}>
          <TextInput
            style={[styles.input, { flex: 1.2 }]}
            value={symbolInput}
            onChangeText={(v) => setSymbolInput(v.toUpperCase())}
            placeholder="BTCUSDT"
            placeholderTextColor={colors.textMuted}
            autoCapitalize="characters"
            autoCorrect={false}
          />
          <TextInput
            style={[styles.input, { flex: 1 }]}
            value={thresholdInput}
            onChangeText={(v) => setThresholdInput(v.replace(/[^0-9]/g, ''))}
            placeholder="100000"
            placeholderTextColor={colors.textMuted}
            keyboardType="number-pad"
          />
          <TouchableOpacity style={styles.applyBtn} onPress={applyBigConfig}>
            <Text style={styles.applyText}>应用</Text>
          </TouchableOpacity>
        </View>
        <Text style={styles.hint}>
          状态: {wsConnected ? '在线' : '重连中'} | {symbol} | 阈值: ${fmtUsd(threshold)}
        </Text>
        {wsError ? <Text style={styles.errorText}>{wsError}</Text> : null}
        <View style={styles.summaryRow}>
          <Text style={styles.summaryText}>最近 {bigSummary.count} 笔</Text>
          <Text style={[styles.summaryText, { color: colors.greenLight }]}>主动买: ${fmtUsd(bigSummary.buyNotional)}</Text>
          <Text style={[styles.summaryText, { color: colors.redLight }]}>主动卖: ${fmtUsd(bigSummary.sellNotional)}</Text>
        </View>
        <Text style={[styles.biasText, { color: bigSummary.biasColor }]}>{bigSummary.bias}</Text>
        <ScrollView style={{ maxHeight: 260 }} nestedScrollEnabled>
          {bigEvents.length === 0 ? (
            <View style={styles.emptyBox}><Text style={styles.emptyText}>暂无大单事件</Text></View>
          ) : (
            bigEvents.slice(0, 80).map((evt) => (
              <View key={evt.id} style={styles.rowCard}>
                <View style={styles.rowTop}>
                  <Text style={styles.rowTitle}>{evt.symbol}</Text>
                  <Text style={[styles.sideText, { color: evt.side === 'BUY' ? colors.greenLight : colors.redLight }]}>
                    {evt.side === 'BUY' ? '主动买' : '主动卖'}
                  </Text>
                </View>
                <Text style={styles.rowSub}>价格: {evt.price}</Text>
                <Text style={styles.rowSub}>数量: {evt.qty}</Text>
                <Text style={styles.rowSub}>名义价值: ${fmtUsd(evt.notional)}</Text>
                <Text style={styles.rowSub}>时间: {fmtTime(evt.time)}</Text>
              </View>
            ))
          )}
        </ScrollView>
      </View>

      <View style={styles.section}>
        <Text style={styles.sectionTitle}>价格区间预警</Text>
        <View style={styles.inputRow}>
          <TextInput
            style={[styles.input, { flex: 1.1 }]}
            value={alertSymbolInput}
            onChangeText={(v) => setAlertSymbolInput(v.toUpperCase())}
            placeholder="BTCUSDT"
            placeholderTextColor={colors.textMuted}
            autoCapitalize="characters"
            autoCorrect={false}
          />
          <TextInput
            style={styles.input}
            value={alertLowerInput}
            onChangeText={(v) => setAlertLowerInput(v.replace(/[^0-9.]/g, ''))}
            placeholder="下限"
            placeholderTextColor={colors.textMuted}
          />
          <TextInput
            style={styles.input}
            value={alertUpperInput}
            onChangeText={(v) => setAlertUpperInput(v.replace(/[^0-9.]/g, ''))}
            placeholder="上限"
            placeholderTextColor={colors.textMuted}
          />
          <TouchableOpacity style={styles.applyBtn} onPress={addAlert}>
            <Text style={styles.applyText}>添加</Text>
          </TouchableOpacity>
        </View>
        <View style={[styles.inputRow, styles.subInputRow]}>
          <TextInput
            style={styles.input}
            value={alertCooldownInput}
            onChangeText={(v) => setAlertCooldownInput(v.replace(/[^0-9]/g, ''))}
            placeholder="冷却秒 5-600"
            placeholderTextColor={colors.textMuted}
            keyboardType="number-pad"
          />
          <TextInput
            style={styles.input}
            value={alertSuppressInput}
            onChangeText={(v) => setAlertSuppressInput(v.replace(/[^0-9]/g, ''))}
            placeholder="抑制秒 10-1800"
            placeholderTextColor={colors.textMuted}
            keyboardType="number-pad"
          />
        </View>
        <Text style={styles.hint}>
          后端监控: {alertWsConnected ? '在线' : '重连中'} | 突破区间触发提醒，可同时监控多个币种。
        </Text>
        {alertWsError ? <Text style={styles.errorText}>{alertWsError}</Text> : null}
        {alerts.length === 0 ? (
          <View style={styles.emptyBox}><Text style={styles.emptyText}>暂无预警规则</Text></View>
        ) : (
          alerts.map((rule) => (
            <View key={rule.id} style={styles.rowCard}>
              <View style={styles.rowTop}>
                <Text style={styles.rowTitle}>{rule.symbol}</Text>
                <Text style={styles.rowSub}>最新价: {rule.lastPrice ? rule.lastPrice.toFixed(4) : '--'}</Text>
              </View>
              <Text style={styles.rowSub}>区间: {rule.lower} - {rule.upper}</Text>
              <Text style={styles.rowSub}>
                冷却 {toNum(rule.cooldownSec || DEFAULT_RANGE_COOLDOWN_SEC)}s | 抑制 {toNum(rule.suppressSec || DEFAULT_RANGE_SUPPRESS_SEC)}s
              </Text>
              <View style={styles.opsRow}>
                <TouchableOpacity
                  style={[styles.smallBtn, rule.enabled ? styles.enableBtn : styles.disableBtn]}
                  onPress={() => toggleAlert(rule.id)}
                >
                  <Text style={styles.smallBtnText}>{rule.enabled ? '已开启' : '已关闭'}</Text>
                </TouchableOpacity>
                <TouchableOpacity style={[styles.smallBtn, styles.removeBtn]} onPress={() => removeAlert(rule.id)}>
                  <Text style={styles.smallBtnText}>删除</Text>
                </TouchableOpacity>
              </View>
            </View>
          ))
        )}

        <View style={styles.logWrap}>
          <Text style={styles.logTitle}>触发记录</Text>
          {alertLogs.length === 0 ? (
            <Text style={styles.emptyText}>暂无触发</Text>
          ) : (
            alertLogs.slice(0, 20).map((log) => (
              <Text key={log.id} style={styles.logText}>
                {fmtTime(log.time)} {String(log.severity || 'warn').toUpperCase()} {log.symbol} {log.direction} {log.lower}-{log.upper} 当前 {log.price.toFixed(4)}
              </Text>
            ))
          )}
        </View>
      </View>

      <View style={styles.section}>
        <Text style={styles.sectionTitle}>突发波动预警（拉升/下跌）</Text>
        <View style={styles.inputRow}>
          <TextInput
            style={[styles.input, { flex: 1.1 }]}
            value={spikeSymbolInput}
            onChangeText={(v) => setSpikeSymbolInput(v.toUpperCase())}
            placeholder="BTCUSDT"
            placeholderTextColor={colors.textMuted}
            autoCapitalize="characters"
            autoCorrect={false}
          />
          <TextInput
            style={styles.input}
            value={spikeThresholdInput}
            onChangeText={(v) => setSpikeThresholdInput(v.replace(/[^0-9.]/g, ''))}
            placeholder="阈值%"
            placeholderTextColor={colors.textMuted}
          />
          <TextInput
            style={styles.input}
            value={spikeWindowInput}
            onChangeText={(v) => setSpikeWindowInput(v.replace(/[^0-9]/g, ''))}
            placeholder="窗口秒"
            placeholderTextColor={colors.textMuted}
            keyboardType="number-pad"
          />
          <TouchableOpacity style={styles.applyBtn} onPress={addSpikeRule}>
            <Text style={styles.applyText}>添加</Text>
          </TouchableOpacity>
        </View>
        <View style={[styles.inputRow, styles.subInputRow]}>
          <TextInput
            style={styles.input}
            value={spikeCooldownInput}
            onChangeText={(v) => setSpikeCooldownInput(v.replace(/[^0-9]/g, ''))}
            placeholder="冷却秒 5-600"
            placeholderTextColor={colors.textMuted}
            keyboardType="number-pad"
          />
          <TextInput
            style={styles.input}
            value={spikeSuppressInput}
            onChangeText={(v) => setSpikeSuppressInput(v.replace(/[^0-9]/g, ''))}
            placeholder="抑制秒 10-1800"
            placeholderTextColor={colors.textMuted}
            keyboardType="number-pad"
          />
        </View>
        <View style={styles.modeRow}>
          <Text style={styles.modeLabel}>阈值模式</Text>
          <TouchableOpacity
            style={[styles.modeBtn, spikeDynamicEnabled && styles.modeBtnActive]}
            onPress={() => setSpikeDynamicEnabled(true)}
          >
            <Text style={[styles.modeText, spikeDynamicEnabled && styles.modeTextActive]}>动态</Text>
          </TouchableOpacity>
          <TouchableOpacity
            style={[styles.modeBtn, !spikeDynamicEnabled && styles.modeBtnActive]}
            onPress={() => setSpikeDynamicEnabled(false)}
          >
            <Text style={[styles.modeText, !spikeDynamicEnabled && styles.modeTextActive]}>静态</Text>
          </TouchableOpacity>
        </View>
        {spikeDynamicEnabled ? (
          <View style={[styles.inputRow, styles.subInputRow]}>
            <TextInput
              style={styles.input}
              value={spikeWarnQInput}
              onChangeText={(v) => setSpikeWarnQInput(v.replace(/[^0-9.]/g, ''))}
              placeholder="警告分位 50-99.9"
              placeholderTextColor={colors.textMuted}
            />
            <TextInput
              style={styles.input}
              value={spikeCriticalQInput}
              onChangeText={(v) => setSpikeCriticalQInput(v.replace(/[^0-9.]/g, ''))}
              placeholder="危险分位 50-99.9"
              placeholderTextColor={colors.textMuted}
            />
            <TextInput
              style={styles.input}
              value={spikeMinSamplesInput}
              onChangeText={(v) => setSpikeMinSamplesInput(v.replace(/[^0-9]/g, ''))}
              placeholder="最小样本 10-500"
              placeholderTextColor={colors.textMuted}
              keyboardType="number-pad"
            />
          </View>
        ) : null}
        <Text style={styles.hint}>
          后端监控: {spikeWsConnected ? '在线' : '重连中'} | 在指定秒数窗口内涨跌幅超过阈值时触发预警。
        </Text>
        {spikeWsError ? <Text style={styles.errorText}>{spikeWsError}</Text> : null}
        {spikeRules.length === 0 ? (
          <View style={styles.emptyBox}><Text style={styles.emptyText}>暂无突发预警规则</Text></View>
        ) : (
          spikeRules.map((rule) => (
            <View key={rule.id} style={styles.rowCard}>
              <View style={styles.rowTop}>
                <Text style={styles.rowTitle}>{rule.symbol}</Text>
                <Text style={styles.rowSub}>
                  最新价: {rule.lastPrice ? rule.lastPrice.toFixed(4) : '--'} | 窗口变动: {fmtPct(rule.lastMovePct)}
                </Text>
              </View>
              <Text style={styles.rowSub}>
                条件: {rule.windowSec}s 内 {rule.dynamic ? '动态阈值' : `${rule.thresholdPct}%`} 以上波动
              </Text>
              <Text style={styles.rowSub}>
                模式: {rule.dynamic ? '动态分位' : '静态阈值'} | 冷却 {toNum(rule.cooldownSec || DEFAULT_SPIKE_COOLDOWN_SEC)}s | 抑制 {toNum(rule.suppressSec || DEFAULT_SPIKE_SUPPRESS_SEC)}s
              </Text>
              {rule.dynamic ? (
                <Text style={styles.rowSub}>
                  分位: warn {toNum(rule.warnQuantile || DEFAULT_SPIKE_WARN_Q).toFixed(1)} / critical {toNum(rule.criticalQuantile || DEFAULT_SPIKE_CRITICAL_Q).toFixed(1)} | 最小样本 {toNum(rule.minSamples || DEFAULT_SPIKE_MIN_SAMPLES)}
                </Text>
              ) : null}
              <View style={styles.opsRow}>
                <TouchableOpacity
                  style={[styles.smallBtn, rule.enabled ? styles.enableBtn : styles.disableBtn]}
                  onPress={() => toggleSpikeRule(rule.id)}
                >
                  <Text style={styles.smallBtnText}>{rule.enabled ? '已开启' : '已关闭'}</Text>
                </TouchableOpacity>
                <TouchableOpacity style={[styles.smallBtn, styles.removeBtn]} onPress={() => removeSpikeRule(rule.id)}>
                  <Text style={styles.smallBtnText}>删除</Text>
                </TouchableOpacity>
              </View>
            </View>
          ))
        )}
        <View style={styles.logWrap}>
          <Text style={styles.logTitle}>突发触发记录</Text>
          {spikeLogs.length === 0 ? (
            <Text style={styles.emptyText}>暂无触发</Text>
          ) : (
            spikeLogs.slice(0, 20).map((log) => (
              <Text key={log.id} style={styles.logText}>
                {fmtTime(log.time)} {String(log.severity || 'warn').toUpperCase()} {log.symbol} {log.direction} {fmtPct(log.movePct)} | {log.windowSec}s {log.dynamic ? '动态' : '静态'}阈值 {toNum(log.triggerPct || log.thresholdPct).toFixed(2)}%
              </Text>
            ))
          )}
        </View>
      </View>
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
    marginTop: spacing.sm,
  },
  title: {
    color: colors.white,
    fontSize: fontSize.lg,
    fontWeight: '700',
    marginBottom: spacing.sm,
  },
  section: {
    backgroundColor: colors.surface,
    borderRadius: radius.md,
    borderWidth: 1,
    borderColor: colors.cardBorder,
    padding: spacing.sm,
    marginBottom: spacing.sm,
  },
  sectionTitle: {
    color: colors.text,
    fontSize: fontSize.md,
    fontWeight: '700',
    marginBottom: spacing.sm,
  },
  inputRow: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: spacing.xs,
  },
  subInputRow: {
    marginTop: spacing.xs,
  },
  input: {
    flex: 1,
    backgroundColor: colors.cardAlt,
    color: colors.white,
    borderWidth: 1,
    borderColor: colors.cardBorder,
    borderRadius: radius.sm,
    paddingHorizontal: spacing.sm,
    paddingVertical: spacing.xs,
    fontSize: fontSize.sm,
  },
  applyBtn: {
    backgroundColor: colors.goldBg,
    borderRadius: radius.sm,
    borderWidth: 1,
    borderColor: colors.gold,
    paddingHorizontal: spacing.md,
    paddingVertical: spacing.xs,
  },
  applyText: {
    color: colors.goldLight,
    fontSize: fontSize.sm,
    fontWeight: '700',
  },
  modeRow: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: spacing.xs,
    marginTop: spacing.xs,
  },
  modeLabel: {
    color: colors.textMuted,
    fontSize: fontSize.xs,
    minWidth: 52,
  },
  modeBtn: {
    borderWidth: 1,
    borderColor: colors.cardBorder,
    borderRadius: radius.pill,
    backgroundColor: colors.cardAlt,
    paddingHorizontal: spacing.sm,
    paddingVertical: 4,
  },
  modeBtnActive: {
    borderColor: colors.blue,
    backgroundColor: colors.blueBg,
  },
  modeText: {
    color: colors.textSecondary,
    fontSize: fontSize.xs,
    fontWeight: '700',
  },
  modeTextActive: {
    color: colors.blueLight,
  },
  hint: {
    color: colors.textMuted,
    fontSize: fontSize.xs,
    marginTop: spacing.xs,
  },
  errorText: {
    color: colors.redLight,
    fontSize: fontSize.xs,
    marginTop: spacing.xs,
  },
  summaryRow: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    flexWrap: 'wrap',
    gap: spacing.xs,
    marginTop: spacing.sm,
  },
  summaryText: {
    color: colors.textSecondary,
    fontSize: fontSize.xs,
    fontVariant: ['tabular-nums'],
  },
  biasText: {
    fontSize: fontSize.sm,
    fontWeight: '700',
    marginTop: spacing.xs,
    marginBottom: spacing.xs,
  },
  emptyBox: {
    paddingVertical: spacing.md,
    alignItems: 'center',
  },
  emptyText: {
    color: colors.textMuted,
    fontSize: fontSize.sm,
  },
  rowCard: {
    backgroundColor: colors.cardAlt,
    borderWidth: 1,
    borderColor: colors.cardBorder,
    borderRadius: radius.sm,
    padding: spacing.sm,
    marginTop: spacing.xs,
  },
  rowTop: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'center',
    gap: spacing.sm,
  },
  rowTitle: {
    color: colors.white,
    fontSize: fontSize.sm,
    fontWeight: '700',
  },
  sideText: {
    fontSize: fontSize.xs,
    fontWeight: '700',
  },
  rowSub: {
    color: colors.textSecondary,
    fontSize: fontSize.xs,
    marginTop: 2,
    fontVariant: ['tabular-nums'],
  },
  opsRow: {
    flexDirection: 'row',
    gap: spacing.xs,
    marginTop: spacing.xs,
  },
  smallBtn: {
    borderRadius: radius.sm,
    paddingHorizontal: spacing.sm,
    paddingVertical: 4,
    borderWidth: 1,
  },
  enableBtn: {
    borderColor: colors.green,
    backgroundColor: colors.greenBg,
  },
  disableBtn: {
    borderColor: colors.textMuted,
    backgroundColor: colors.surface,
  },
  removeBtn: {
    borderColor: colors.red,
    backgroundColor: colors.redBg,
  },
  smallBtnText: {
    color: colors.white,
    fontSize: fontSize.xs,
    fontWeight: '700',
  },
  logWrap: {
    marginTop: spacing.sm,
    borderTopWidth: 1,
    borderTopColor: colors.cardBorder,
    paddingTop: spacing.sm,
  },
  logTitle: {
    color: colors.text,
    fontSize: fontSize.sm,
    fontWeight: '700',
    marginBottom: spacing.xs,
  },
  logText: {
    color: colors.textSecondary,
    fontSize: fontSize.xs,
    marginBottom: 3,
  },
});
