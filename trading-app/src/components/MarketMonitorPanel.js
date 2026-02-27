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

export default function MarketMonitorPanel({ onHasNew }) {
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
  const [alerts, setAlerts] = useState([]);
  const [alertLogs, setAlertLogs] = useState([]);
  const [alertWsConnected, setAlertWsConnected] = useState(false);
  const [alertWsError, setAlertWsError] = useState('');
  const [spikeSymbolInput, setSpikeSymbolInput] = useState(DEFAULT_SYMBOL);
  const [spikeThresholdInput, setSpikeThresholdInput] = useState(String(DEFAULT_SPIKE_THRESHOLD_PCT));
  const [spikeWindowInput, setSpikeWindowInput] = useState(String(DEFAULT_SPIKE_WINDOW_SEC));
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

  useEffect(() => {
    onHasNewRef.current = onHasNew;
  }, [onHasNew]);

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
  }, [symbol, threshold]);

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
          onHasNewRef.current?.(true);
          Vibration.vibrate(180);
          setAlertLogs((prev) => [event, ...prev].slice(0, 60));
          setAlerts((prev) => prev.map((r) => (
            r.id === event.ruleId
              ? { ...r, lastPrice: toNum(event.price), lastInside: false, lastTriggerAt: toNum(event.time) }
              : r
          )));
          Alert.alert(
            '价格预警触发',
            `${event.symbol} ${event.direction}\n区间 ${event.lower} - ${event.upper}\n当前 ${toNum(event.price).toFixed(4)}`,
          );
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
  }, [sendAlertWs]);

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
          onHasNewRef.current?.(true);
          Vibration.vibrate(220);
          setSpikeLogs((prev) => [event, ...prev].slice(0, 80));
          setSpikeRules((prev) => prev.map((r) => (
            r.id === event.ruleId
              ? { ...r, lastPrice: toNum(event.price), lastMovePct: toNum(event.movePct), lastTriggerAt: toNum(event.time) }
              : r
          )));
          Alert.alert(
            '突发波动预警',
            `${event.symbol} ${event.direction} ${fmtPct(event.movePct)}\n窗口 ${event.windowSec}s 阈值 ${event.thresholdPct}%\n基准 ${toNum(event.basePrice).toFixed(4)} 当前 ${toNum(event.price).toFixed(4)}`,
          );
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
  }, [sendSpikeWs]);

  const addAlert = useCallback(() => {
    const symbolUpper = alertSymbolInput.trim().toUpperCase();
    const lower = toNum(alertLowerInput);
    const upper = toNum(alertUpperInput);
    if (!symbolUpper) {
      Alert.alert('参数错误', '请输入预警交易对');
      return;
    }
    if (!Number.isFinite(lower) || !Number.isFinite(upper) || lower <= 0 || upper <= 0 || lower >= upper) {
      Alert.alert('参数错误', '请填写有效区间，且下限 < 上限');
      return;
    }
    const ok = sendAlertWs({
      action: 'addRule',
      symbol: symbolUpper,
      lower,
      upper,
      cooldownSec: 10,
      enabled: true,
    });
    if (!ok) {
      Alert.alert('连接异常', '后端区间预警未连接，请稍后重试');
      return;
    }
    setAlertSymbolInput(symbolUpper);
    setAlertLowerInput('');
    setAlertUpperInput('');
  }, [alertLowerInput, alertSymbolInput, alertUpperInput, sendAlertWs]);

  const addSpikeRule = useCallback(() => {
    const symbolUpper = spikeSymbolInput.trim().toUpperCase();
    const thresholdPct = toNum(spikeThresholdInput);
    const windowSec = toNum(spikeWindowInput);
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
    const ok = sendSpikeWs({
      action: 'addRule',
      symbol: symbolUpper,
      thresholdPct,
      windowSec: Math.round(windowSec),
      cooldownSec: 15,
      enabled: true,
    });
    if (!ok) {
      Alert.alert('连接异常', '后端突发监控未连接，请稍后重试');
      return;
    }
  }, [sendSpikeWs, spikeSymbolInput, spikeThresholdInput, spikeWindowInput]);

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
                {fmtTime(log.time)} {log.symbol} {log.direction} {log.lower}-{log.upper} 当前 {log.price.toFixed(4)}
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
              <Text style={styles.rowSub}>条件: {rule.windowSec}s 内 {rule.thresholdPct}% 以上波动</Text>
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
                {fmtTime(log.time)} {log.symbol} {log.direction} {fmtPct(log.movePct)} | {log.windowSec}s 阈值 {log.thresholdPct}%
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
