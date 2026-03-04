import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import {
  View,
  Text,
  StyleSheet,
  TouchableOpacity,
  ActivityIndicator,
  ScrollView,
  TextInput,
  Alert,
} from 'react-native';
import { colors, spacing, radius, fontSize } from '../services/theme';
import api, { AUTH_TOKEN, WS_HYPER_MONITOR_BASE } from '../services/api';

const DEFAULT_ADDRESS = '0x15a4f009bb324a3fb9e36137136b201e3fe0dfdb';
const FOLLOW_SYMBOL = 'BTCUSDT';
const SNAPSHOT_REFRESH_MS = 30000;
const WS_RECONNECT_MS = 3000;
const WS_PING_MS = 30000;

function toNumber(value) {
  const n = Number(value);
  return Number.isFinite(n) ? n : 0;
}

function fmtAmount(value) {
  return toNumber(value).toLocaleString('en-US', { maximumFractionDigits: 4 });
}

function fmtPrice(value) {
  return toNumber(value).toLocaleString('en-US', { maximumFractionDigits: 6 });
}

function fmtUsd(value) {
  return toNumber(value).toLocaleString('en-US', { maximumFractionDigits: 0 });
}

function fmtTime(ms) {
  const t = toNumber(ms);
  if (t <= 0) return '-';
  return new Date(t).toLocaleString('zh-CN', {
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
    hour12: false,
  });
}

function shortHash(hash = '') {
  if (!hash || hash.length < 12) return hash || '-';
  return `${hash.slice(0, 8)}...${hash.slice(-6)}`;
}

function normalizeOrder(raw = {}) {
  if (raw.order?.order) return raw.order.order;
  if (raw.order) return raw.order;
  return raw;
}

function getOrderPrice(order = {}) {
  return order.px || order.limitPx || order.price || 0;
}

function getOrderSize(order = {}) {
  return order.sz || order.origSz || order.size || 0;
}

function sideLabel(side) {
  if (side === 'B' || side === 'BUY') return '买入';
  if (side === 'A' || side === 'SELL') return '卖出';
  return '-';
}

function fillActionLabel(fill = {}) {
  const dir = String(fill.dir || '').toLowerCase();
  if (dir.includes('open')) return '开仓';
  if (dir.includes('close')) return '平仓';
  if (toNumber(fill.closedPnl) !== 0) return '平仓';
  return '未知';
}

function statusColor(status = '') {
  const s = String(status).toLowerCase();
  if (s.includes('filled') || s.includes('triggered')) return colors.greenLight;
  if (s.includes('canceled') || s.includes('rejected') || s.includes('error')) return colors.redLight;
  return colors.yellow;
}

function inferHyperSeverity(item = {}) {
  const title = String(item.title || '').toLowerCase();
  const detail = String(item.detail || '').toLowerCase();
  if (title.includes('清算') || title.includes('拒') || detail.includes('rejected') || detail.includes('error')) return 'critical';
  if (title.includes('取消') || title.includes('消失') || title.includes('跟单平仓')) return 'warn';
  return 'info';
}

function makeHistoryKey(item = {}) {
  const order = normalizeOrder(item);
  return `${order.oid || order.cloid || order.coin || '-'}::${item.status || '-'}::${item.statusTimestamp || item.timestamp || '-'}`;
}

function makeOpenOrderKey(order = {}) {
  return order.oid || order.cloid || `${order.coin || '-'}-${order.timestamp || '-'}-${getOrderPrice(order)}-${getOrderSize(order)}-${order.side || '-'}`;
}

function makeFillKey(fill = {}) {
  return `${fill.tid || '-'}::${fill.hash || '-'}::${fill.time || '-'}::${fill.coin || '-'}::${fill.side || '-'}`;
}

function mergeHistory(prev = [], incoming = []) {
  const merged = [...incoming, ...prev];
  const seen = new Set();
  const deduped = [];
  merged.forEach((item) => {
    const key = makeHistoryKey(item);
    if (seen.has(key)) return;
    seen.add(key);
    deduped.push(item);
  });
  deduped.sort((a, b) => toNumber(b.statusTimestamp || b.timestamp) - toNumber(a.statusTimestamp || a.timestamp));
  return deduped.slice(0, 300);
}

function mergeFills(prev = [], incoming = []) {
  const merged = [...incoming, ...prev];
  const seen = new Set();
  const deduped = [];
  merged.forEach((item) => {
    const key = makeFillKey(item);
    if (seen.has(key)) return;
    seen.add(key);
    deduped.push(item);
  });
  deduped.sort((a, b) => toNumber(b.time) - toNumber(a.time));
  return deduped.slice(0, 300);
}

export default function HyperMonitorPanel({
  address = DEFAULT_ADDRESS,
  onHasNew,
  onMonitorEvent,
}) {
  const [activeCard, setActiveCard] = useState('orders');
  const [loading, setLoading] = useState(true);
  const [refreshing, setRefreshing] = useState(false);
  const [error, setError] = useState('');
  const [lastUpdated, setLastUpdated] = useState(0);
  const [wsConnected, setWsConnected] = useState(false);

  const [openOrders, setOpenOrders] = useState([]);
  const [historyOrders, setHistoryOrders] = useState([]);
  const [fills, setFills] = useState([]);
  const [activityEvents, setActivityEvents] = useState([]);
  const [followEnabled, setFollowEnabled] = useState(false);
  const [followQuoteQty, setFollowQuoteQty] = useState('10');
  const [followLeverage, setFollowLeverage] = useState('10');
  const [followCount, setFollowCount] = useState(0);

  const wsRef = useRef(null);
  const pingTimerRef = useRef(null);
  const reconnectTimerRef = useRef(null);
  const mountedRef = useRef(false);
  const closedByUserRef = useRef(false);

  const prevOpenMapRef = useRef({});
  const seenHistoryRef = useRef(new Set());
  const seenEventRef = useRef(new Set());
  const onMonitorEventRef = useRef(onMonitorEvent);

  useEffect(() => {
    onMonitorEventRef.current = onMonitorEvent;
  }, [onMonitorEvent]);

  useEffect(() => {
    prevOpenMapRef.current = {};
    seenHistoryRef.current = new Set();
    seenEventRef.current = new Set();
    setLoading(true);
    setRefreshing(false);
    setError('');
    setActivityEvents([]);
    setOpenOrders([]);
    setHistoryOrders([]);
    setFills([]);
    setLastUpdated(0);
    setFollowEnabled(false);
    setFollowCount(0);
  }, [address]);

  const pushEvents = useCallback((items = []) => {
    if (!items.length) return;
    const fresh = [];
    items.forEach((item) => {
      const key = `${item.title}::${item.detail}::${item.time || '-'}`;
      if (seenEventRef.current.has(key)) return;
      seenEventRef.current.add(key);
      fresh.push({
        ...item,
        eventKey: key,
        id: `${key}::${Math.random().toString(36).slice(2, 8)}`,
      });
    });
    if (!fresh.length) return;
    onHasNew?.(true);
    fresh.forEach((item) => {
      onMonitorEventRef.current?.({
        eventId: `hyper::${item.eventKey || item.id}`,
        ts: toNumber(item.time || Date.now()),
        source: 'hyper',
        severity: inferHyperSeverity(item),
        symbol: item.symbol || '',
        strategyId: `hyper:${address}`,
        type: item.type || 'activity',
        message: `${item.title} - ${item.detail}`,
        payload: {
          title: item.title,
          detail: item.detail,
        },
      });
    });
    setActivityEvents((prev) => {
      const merged = [...fresh, ...prev];
      merged.sort((a, b) => toNumber(b.time) - toNumber(a.time));
      return merged.slice(0, 80);
    });
  }, [address, onHasNew]);

  const applyOpenOrders = useCallback((rawList = [], emitEvents = true) => {
    const normalized = rawList.map((raw) => normalizeOrder(raw));
    const nextMap = {};
    normalized.forEach((order) => {
      const key = makeOpenOrderKey(order);
      if (key) nextMap[key] = order;
    });

    if (emitEvents) {
      const prevMap = prevOpenMapRef.current || {};
      const prevKeys = new Set(Object.keys(prevMap));
      const nextKeys = new Set(Object.keys(nextMap));
      const events = [];

      nextKeys.forEach((key) => {
        if (prevKeys.has(key)) return;
        const order = nextMap[key];
        events.push({
          time: Date.now(),
          title: '新增挂单',
          type: 'open_order_new',
          symbol: order.coin || '',
          detail: `${order.coin || '-'} ${sideLabel(order.side)} ${fmtAmount(getOrderSize(order))} @ ${fmtPrice(getOrderPrice(order))}`,
        });
      });

      prevKeys.forEach((key) => {
        if (nextKeys.has(key)) return;
        const order = prevMap[key];
        events.push({
          time: Date.now(),
          title: '挂单消失',
          type: 'open_order_missing',
          symbol: order.coin || '',
          detail: `${order.coin || '-'} ${sideLabel(order.side)} ${fmtAmount(getOrderSize(order))} @ ${fmtPrice(getOrderPrice(order))}（可能成交或撤单）`,
        });
      });

      pushEvents(events);
    }

    prevOpenMapRef.current = nextMap;
    setOpenOrders(normalized);
  }, [pushEvents]);

  const applyOrderUpdates = useCallback((updates = []) => {
    if (!updates.length) return;
    const fresh = [];
    const events = [];

    updates.forEach((item) => {
      const key = makeHistoryKey(item);
      if (seenHistoryRef.current.has(key)) return;
      seenHistoryRef.current.add(key);
      fresh.push(item);
      const order = normalizeOrder(item);
      events.push({
        time: toNumber(item.statusTimestamp || item.timestamp || Date.now()),
        title: `订单状态: ${item.status || '-'}`,
        type: 'order_status',
        symbol: order.coin || '',
        detail: `${order.coin || '-'} ${sideLabel(order.side)} ${fmtAmount(getOrderSize(order))} @ ${fmtPrice(getOrderPrice(order))}`,
      });
    });

    if (fresh.length) {
      setHistoryOrders((prev) => mergeHistory(prev, fresh));
      pushEvents(events);
    }
  }, [pushEvents]);

  const applyFills = useCallback((incoming = [], isSnapshot = false) => {
    if (isSnapshot) {
      setFills(mergeFills([], incoming));
      return;
    }
    if (!incoming.length) return;
    setFills((prev) => mergeFills(prev, incoming));
    pushEvents(incoming.map((fill) => ({
      time: toNumber(fill.time || Date.now()),
      title: '新成交',
      type: 'fill',
      symbol: fill.coin || '',
      detail: `${fill.coin || '-'} ${fillActionLabel(fill)} ${sideLabel(fill.side)} ${fill.sz || '-'} @ ${fill.px || '-'}`,
    })));
  }, [pushEvents]);

  const clearWsTimers = useCallback(() => {
    if (pingTimerRef.current) {
      clearInterval(pingTimerRef.current);
      pingTimerRef.current = null;
    }
    if (reconnectTimerRef.current) {
      clearTimeout(reconnectTimerRef.current);
      reconnectTimerRef.current = null;
    }
  }, []);

  const sendWs = useCallback((payload) => {
    if (!wsRef.current || wsRef.current.readyState !== 1) return;
    wsRef.current.send(JSON.stringify(payload));
  }, []);

  const requestSnapshot = useCallback(() => {
    sendWs({ action: 'snapshot' });
  }, [sendWs]);

  const fetchFollowStatus = useCallback(async () => {
    try {
      const res = await api.hyperFollowStatus(address);
      const status = res?.data || null;
      if (!status || !status.enabled) {
        setFollowEnabled(false);
        setFollowCount(0);
        return;
      }

      setFollowEnabled(true);
      if (status.quoteQuantity !== undefined && status.quoteQuantity !== null) {
        setFollowQuoteQty(String(status.quoteQuantity));
      }
      if (status.leverage !== undefined && status.leverage !== null) {
        setFollowLeverage(String(status.leverage));
      }
      setFollowCount(Number(status.executedCount || 0));
    } catch (_) {
      // 状态查询失败不阻塞监控数据渲染
    }
  }, [address]);

  const connectWs = useCallback(() => {
    if (!mountedRef.current) return;
    if (wsRef.current && (wsRef.current.readyState === 0 || wsRef.current.readyState === 1)) return;

    const ws = new WebSocket(`${WS_HYPER_MONITOR_BASE}?address=${encodeURIComponent(address)}&token=${AUTH_TOKEN}`);
    wsRef.current = ws;

    ws.onopen = () => {
      setWsConnected(true);
      clearWsTimers();
      requestSnapshot();
      pingTimerRef.current = setInterval(() => {
        sendWs({ action: 'ping' });
      }, WS_PING_MS);
    };

    ws.onmessage = (event) => {
      let msg;
      try {
        msg = JSON.parse(event.data);
      } catch (_) {
        return;
      }

      if (!msg || typeof msg !== 'object') return;
      if (msg.channel === 'pong' || msg.method === 'pong' || msg.action === 'pong') return;
      if (msg.channel === 'subscriptionResponse') return;

      if (msg.channel === 'snapshotError' || msg.channel === 'proxyError') {
        setError(`监控拉取失败：${msg.error || '未知错误'}`);
        setLoading(false);
        setRefreshing(false);
        return;
      }

      if (msg.channel === 'openOrders') {
        const list = Array.isArray(msg.data?.orders)
          ? msg.data.orders
          : (Array.isArray(msg.data) ? msg.data : []);
        applyOpenOrders(list, !msg.isSnapshot);
        setLastUpdated(Date.now());
        setLoading(false);
        setRefreshing(false);
        return;
      }

      if (msg.channel === 'orderUpdates') {
        const updates = Array.isArray(msg.data) ? msg.data : [];
        if (msg.isSnapshot) {
          setHistoryOrders([...updates]);
          seenHistoryRef.current = new Set(updates.map((item) => makeHistoryKey(item)));
        } else {
          applyOrderUpdates(updates);
        }
        setLastUpdated(Date.now());
        setLoading(false);
        setRefreshing(false);
        return;
      }

      if (msg.channel === 'userFills') {
        const data = msg.data || {};
        const list = Array.isArray(data.fills) ? data.fills : [];
        applyFills(list, !!data.isSnapshot);
        setLastUpdated(Date.now());
        setLoading(false);
        setRefreshing(false);
        return;
      }

      if (msg.channel === 'userEvents') {
        const data = msg.data || {};
        if (Array.isArray(data.fills)) {
          applyFills(data.fills, false);
        }
        if (Array.isArray(data.nonUserCancel) && data.nonUserCancel.length > 0) {
          pushEvents(data.nonUserCancel.map((evt) => ({
            time: toNumber(evt.time || Date.now()),
            title: '系统取消订单',
            type: 'system_cancel',
            symbol: evt.coin || '',
            detail: `${evt.coin || '-'} OID ${evt.oid || '-'}`,
          })));
        }
        if (Array.isArray(data.liquidation) && data.liquidation.length > 0) {
          pushEvents(data.liquidation.map((evt) => ({
            time: toNumber(evt.time || Date.now()),
            title: '清算事件',
            type: 'liquidation_event',
            symbol: evt.coin || '',
            detail: `${evt.coin || '-'} ${evt.side || '-'} ${evt.sz || '-'}`,
          })));
        }
        setLastUpdated(Date.now());
        setLoading(false);
        setRefreshing(false);
      }

      // 跟单执行事件（后端自动跟单后推送）
      if (msg.channel === 'followEvent') {
        const action = msg.action === 'open' ? '跟单开仓' : '跟单平仓';
        const side = msg.side || '';
        const ps = msg.positionSide || '';
        const qty = msg.quoteQty || '';
        const lev = msg.leverage || '';
        const oid = msg.orderId || '';
        const detail = msg.action === 'open'
          ? `${msg.symbol} ${ps} ${side} ${qty}U ${lev}x OID:${oid}`
          : `${msg.symbol} ${ps}`;
        pushEvents([{
          time: toNumber(msg.time || Date.now()),
          title: action,
          type: msg.action === 'open' ? 'follow_open' : 'follow_close',
          symbol: msg.symbol || '',
          detail,
        }]);
        // 刷新跟单计数
        setFollowCount((c) => c + 1);
      }
    };

    ws.onerror = () => {
      setWsConnected(false);
    };

    ws.onclose = () => {
      setWsConnected(false);
      clearWsTimers();
      wsRef.current = null;
      if (!mountedRef.current || closedByUserRef.current) return;
      reconnectTimerRef.current = setTimeout(() => {
        connectWs();
      }, WS_RECONNECT_MS);
    };
  }, [
    address,
    applyFills,
    applyOpenOrders,
    applyOrderUpdates,
    clearWsTimers,
    pushEvents,
    sendWs,
    requestSnapshot,
  ]);

  useEffect(() => {
    mountedRef.current = true;
    closedByUserRef.current = false;
    connectWs();
    void fetchFollowStatus();

    const snapshotTimer = setInterval(() => {
      requestSnapshot();
    }, SNAPSHOT_REFRESH_MS);
    const followStatusTimer = setInterval(() => {
      void fetchFollowStatus();
    }, SNAPSHOT_REFRESH_MS);

    return () => {
      mountedRef.current = false;
      closedByUserRef.current = true;
      clearInterval(snapshotTimer);
      clearInterval(followStatusTimer);
      clearWsTimers();
      if (wsRef.current) {
        try {
          wsRef.current.close();
        } catch (_) {}
        wsRef.current = null;
      }
    };
  }, [
    clearWsTimers,
    connectWs,
    requestSnapshot,
    fetchFollowStatus,
  ]);

  const onRefresh = () => {
    setRefreshing(true);
    requestSnapshot();
    void fetchFollowStatus();
  };

  const toggleFollow = async () => {
    if (!followEnabled) {
      const quoteQty = Number(followQuoteQty);
      const leverage = parseInt(followLeverage, 10);
      if (!Number.isFinite(quoteQty) || quoteQty <= 0) {
        Alert.alert('参数错误', '请输入有效的跟单金额(U)');
        return;
      }
      if (!Number.isFinite(leverage) || leverage <= 0) {
        Alert.alert('参数错误', '请输入有效的杠杆');
        return;
      }

      try {
        await api.startHyperFollow({
          address,
          symbol: FOLLOW_SYMBOL,
          quoteQuantity: String(quoteQty),
          leverage,
        });
        await fetchFollowStatus();
        Alert.alert('跟单已开启', `服务端已接管，仅跟 ${FOLLOW_SYMBOL}，包含开仓与平仓`);
      } catch (e) {
        Alert.alert('开启失败', e.message || '服务端启动跟单失败');
      }
      return;
    }

    try {
      await api.stopHyperFollow(address);
      await fetchFollowStatus();
      Alert.alert('跟单已关闭', '服务端已停止该地址的自动跟单');
    } catch (e) {
      Alert.alert('关闭失败', e.message || '服务端停止跟单失败');
    }
  };

  const sortedOpenOrders = useMemo(() => (
    [...openOrders]
      .sort((a, b) => toNumber(b.timestamp) - toNumber(a.timestamp))
      .slice(0, 20)
  ), [openOrders]);

  const sortedHistoryOrders = useMemo(() => (
    [...historyOrders]
      .sort((a, b) => toNumber(b.statusTimestamp || b.timestamp) - toNumber(a.statusTimestamp || a.timestamp))
      .slice(0, 40)
  ), [historyOrders]);

  const sortedFills = useMemo(() => (
    [...fills]
      .sort((a, b) => toNumber(b.time) - toNumber(a.time))
      .slice(0, 40)
  ), [fills]);

  const orderStats = useMemo(() => {
    const recent = sortedHistoryOrders.slice(0, 80);
    let filled = 0;
    let canceled = 0;
    let rejected = 0;
    recent.forEach((item) => {
      const s = String(item.status || '').toLowerCase();
      if (s.includes('filled') || s.includes('triggered')) filled += 1;
      else if (s.includes('canceled')) canceled += 1;
      else if (s.includes('rejected') || s.includes('error')) rejected += 1;
    });
    return { filled, canceled, rejected };
  }, [sortedHistoryOrders]);

  return (
    <View style={styles.card}>
      <View style={styles.header}>
        <Text style={styles.title}>Hyperliquid 合约下单监控</Text>
        <TouchableOpacity onPress={onRefresh} style={styles.refreshBtn}>
          <Text style={styles.refreshText}>{refreshing ? '刷新中...' : '刷新'}</Text>
        </TouchableOpacity>
      </View>

      <Text style={styles.addrText}>{address}</Text>
      <Text style={styles.hintText}>
        WS: {wsConnected ? '已连接' : '重连中'} | 服务端快照: 30s | 最近更新: {fmtTime(lastUpdated)}
      </Text>

      <View style={styles.tabRow}>
        <TouchableOpacity
          style={[styles.tabBtn, activeCard === 'orders' && styles.tabBtnActive]}
          onPress={() => setActiveCard('orders')}
        >
          <Text style={[styles.tabText, activeCard === 'orders' && styles.tabTextActive]}>下单行为</Text>
        </TouchableOpacity>
        <TouchableOpacity
          style={[styles.tabBtn, activeCard === 'fills' && styles.tabBtnActive]}
          onPress={() => setActiveCard('fills')}
        >
          <Text style={[styles.tabText, activeCard === 'fills' && styles.tabTextActive]}>成交行为</Text>
        </TouchableOpacity>
      </View>

      {loading ? (
        <View style={styles.loadingBox}>
          <ActivityIndicator color={colors.gold} />
          <Text style={styles.loadingText}>监控数据加载中...</Text>
        </View>
      ) : (
        <>
          {error ? <Text style={styles.errorText}>{error}</Text> : null}

          {activeCard === 'orders' ? (
            <>
              <View style={styles.followCard}>
                <View style={styles.followTopRow}>
                  <Text style={styles.followTitle}>自动跟单（仅{FOLLOW_SYMBOL}）</Text>
                  <TouchableOpacity
                    style={[styles.followBtn, followEnabled ? styles.followBtnStop : styles.followBtnStart]}
                    onPress={toggleFollow}
                  >
                    <Text style={styles.followBtnText}>{followEnabled ? '停止' : '启动'}</Text>
                  </TouchableOpacity>
                </View>
                <View style={styles.followInputRow}>
                  <View style={styles.followInputWrap}>
                    <Text style={styles.followInputLabel}>金额(U)</Text>
                    <TextInput
                      style={styles.followInput}
                      value={followQuoteQty}
                      onChangeText={(v) => setFollowQuoteQty(v.replace(/[^0-9.]/g, ''))}
                      keyboardType="default"
                      editable={!followEnabled}
                      placeholder="如 10.5"
                      placeholderTextColor={colors.textMuted}
                    />
                  </View>
                  <View style={styles.followInputWrap}>
                    <Text style={styles.followInputLabel}>杠杆</Text>
                    <TextInput
                      style={styles.followInput}
                      value={followLeverage}
                      onChangeText={(v) => setFollowLeverage(v.replace(/[^0-9]/g, ''))}
                      keyboardType="number-pad"
                      editable={!followEnabled}
                    />
                  </View>
                </View>
                <Text style={styles.followHint}>
                  状态: {followEnabled ? '运行中(服务端)' : '未启动'} | 跟开仓+平仓 | 已执行 {followCount} 次
                </Text>
              </View>

              <View style={styles.summaryRow}>
                <View style={styles.summaryItem}>
                  <Text style={styles.summaryValue}>{sortedOpenOrders.length}</Text>
                  <Text style={styles.summaryLabel}>当前挂单</Text>
                </View>
                <View style={styles.summaryItem}>
                  <Text style={styles.summaryValue}>{orderStats.filled}/{orderStats.canceled}/{orderStats.rejected}</Text>
                  <Text style={styles.summaryLabel}>成/撤/拒</Text>
                </View>
                <View style={styles.summaryItem}>
                  <Text style={styles.summaryValue}>{activityEvents.length}</Text>
                  <Text style={styles.summaryLabel}>实时事件</Text>
                </View>
              </View>

              <View style={styles.sectionHeader}>
                <Text style={styles.sectionTitle}>挂单列表</Text>
                <Text style={styles.sectionCount}>{sortedOpenOrders.length} 条</Text>
              </View>
              {sortedOpenOrders.length === 0 ? (
                <View style={styles.emptyBox}>
                  <Text style={styles.emptyText}>暂无挂单</Text>
                </View>
              ) : (
                sortedOpenOrders.map((order, idx) => (
                  <View key={`${makeOpenOrderKey(order)}-${idx}`} style={styles.rowCard}>
                    <View style={styles.rowTop}>
                      <Text style={styles.rowTitle}>{order.coin || '-'}</Text>
                      <Text style={[
                        styles.sideTag,
                        { color: order.side === 'B' || order.side === 'BUY' ? colors.greenLight : colors.redLight },
                      ]}>
                        {sideLabel(order.side)}
                      </Text>
                    </View>
                    <Text style={styles.rowSub}>价格: {fmtPrice(getOrderPrice(order))}</Text>
                    <Text style={styles.rowSub}>数量: {fmtAmount(getOrderSize(order))}</Text>
                    <Text style={styles.rowSub}>类型: {order.orderType || '-'}</Text>
                    <Text style={styles.rowSub}>时间: {fmtTime(order.timestamp)}</Text>
                    <Text style={styles.rowSub}>OID: {order.oid || '-'}</Text>
                  </View>
                ))
              )}

              <View style={styles.sectionHeader}>
                <Text style={styles.sectionTitle}>订单状态变化</Text>
                <Text style={styles.sectionCount}>{sortedHistoryOrders.length} 条</Text>
              </View>
              {sortedHistoryOrders.length === 0 ? (
                <View style={styles.emptyBox}>
                  <Text style={styles.emptyText}>暂无历史订单</Text>
                </View>
              ) : (
                sortedHistoryOrders.map((item, idx) => {
                  const order = normalizeOrder(item);
                  return (
                    <View key={`${makeHistoryKey(item)}-${idx}`} style={styles.rowCard}>
                      <View style={styles.rowTop}>
                        <Text style={styles.rowTitle}>{order.coin || '-'}</Text>
                        <Text style={[styles.statusTag, { color: statusColor(item.status) }]}>
                          {item.status || '-'}
                        </Text>
                      </View>
                      <Text style={styles.rowSub}>
                        {sideLabel(order.side)} {fmtAmount(getOrderSize(order))} @ {fmtPrice(getOrderPrice(order))}
                      </Text>
                      <Text style={styles.rowSub}>时间: {fmtTime(item.statusTimestamp || order.timestamp)}</Text>
                      <Text style={styles.rowSub}>OID: {order.oid || '-'}</Text>
                    </View>
                  );
                })
              )}

              <View style={styles.sectionHeader}>
                <Text style={styles.sectionTitle}>实时事件</Text>
                <Text style={styles.sectionCount}>{activityEvents.length} 条</Text>
              </View>
              {activityEvents.length === 0 ? (
                <View style={styles.emptyBox}>
                  <Text style={styles.emptyText}>暂无实时变化</Text>
                </View>
              ) : (
                activityEvents.slice(0, 25).map((item) => (
                  <View key={item.id} style={styles.rowCard}>
                    <Text style={styles.rowTitle}>{item.title}</Text>
                    <Text style={styles.rowSub}>{item.detail}</Text>
                    <Text style={styles.rowSub}>时间: {fmtTime(item.time)}</Text>
                  </View>
                ))
              )}
            </>
          ) : activeCard === 'fills' ? (
            <>
              <View style={styles.sectionHeader}>
                <Text style={styles.sectionTitle}>最近成交</Text>
                <Text style={styles.sectionCount}>{sortedFills.length} 条</Text>
              </View>
              {sortedFills.length === 0 ? (
                <View style={styles.emptyBox}>
                  <Text style={styles.emptyText}>暂无成交记录</Text>
                </View>
              ) : (
                <ScrollView style={{ maxHeight: 560 }} nestedScrollEnabled>
                  {sortedFills.map((item, idx) => (
                    <View key={`${makeFillKey(item)}-${idx}`} style={styles.rowCard}>
                      <View style={styles.rowTop}>
                        <Text style={styles.rowTitle}>{item.coin || '-'}</Text>
                        <Text style={[
                          styles.sideTag,
                          { color: item.side === 'B' || item.side === 'BUY' ? colors.greenLight : colors.redLight },
                        ]}>
                          {sideLabel(item.side)}
                        </Text>
                      </View>
                      <Text style={styles.rowSub}>价格: {item.px || '-'}</Text>
                      <Text style={styles.rowSub}>数量: {item.sz || '-'}</Text>
                      <Text style={styles.rowSub}>行为: {fillActionLabel(item)} ({item.dir || '-'})</Text>
                      <Text style={styles.rowSub}>已实现盈亏: {item.closedPnl || '0'}</Text>
                      <Text style={styles.rowSub}>手续费: {item.fee || '-'}</Text>
                      <Text style={styles.rowSub}>时间: {fmtTime(item.time)}</Text>
                      <Text style={styles.rowSub}>交易哈希: {shortHash(item.hash)}</Text>
                    </View>
                  ))}
                </ScrollView>
              )}
            </>
          ) : null}
        </>
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
  },
  header: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'center',
    marginBottom: spacing.sm,
  },
  title: {
    fontSize: fontSize.lg,
    fontWeight: '700',
    color: colors.white,
  },
  refreshBtn: {
    borderRadius: radius.pill,
    paddingHorizontal: spacing.md,
    paddingVertical: spacing.xs,
    backgroundColor: colors.goldBg,
  },
  refreshText: {
    fontSize: fontSize.sm,
    color: colors.goldLight,
    fontWeight: '600',
  },
  addrText: {
    color: colors.textMuted,
    fontSize: fontSize.xs,
    marginBottom: spacing.xs,
    fontFamily: 'monospace',
  },
  hintText: {
    color: colors.textMuted,
    fontSize: fontSize.xs,
    marginBottom: spacing.md,
  },
  tabRow: {
    flexDirection: 'row',
    backgroundColor: colors.surface,
    borderRadius: radius.md,
    padding: 3,
    gap: 2,
    marginBottom: spacing.md,
  },
  tabBtn: {
    flex: 1,
    backgroundColor: 'transparent',
    borderRadius: radius.sm,
    alignItems: 'center',
    paddingVertical: spacing.sm,
  },
  tabBtnActive: {
    backgroundColor: colors.goldBg,
  },
  tabText: {
    color: colors.textMuted,
    fontSize: fontSize.sm,
    fontWeight: '600',
  },
  tabTextActive: {
    color: colors.white,
    fontWeight: '700',
  },
  loadingBox: {
    borderRadius: radius.lg,
    backgroundColor: colors.surface,
    paddingVertical: spacing.xxl,
    alignItems: 'center',
    gap: spacing.sm,
    marginBottom: spacing.md,
  },
  loadingText: {
    color: colors.textMuted,
    fontSize: fontSize.sm,
  },
  errorText: {
    color: colors.redLight,
    fontSize: fontSize.sm,
    marginBottom: spacing.md,
  },
  followCard: {
    borderRadius: radius.lg,
    borderWidth: 1,
    borderColor: colors.cardBorder,
    backgroundColor: colors.surface,
    padding: spacing.md,
    marginBottom: spacing.md,
  },
  followTopRow: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'center',
    marginBottom: spacing.sm,
  },
  followTitle: {
    color: colors.white,
    fontSize: fontSize.sm,
    fontWeight: '700',
  },
  followBtn: {
    borderRadius: radius.pill,
    paddingHorizontal: spacing.lg,
    paddingVertical: spacing.sm,
    borderWidth: 1,
  },
  followBtnStart: {
    backgroundColor: colors.goldBg,
    borderColor: colors.gold,
  },
  followBtnStop: {
    backgroundColor: colors.redBg,
    borderColor: colors.red,
  },
  followBtnText: {
    color: colors.white,
    fontSize: fontSize.sm,
    fontWeight: '700',
  },
  followInputRow: {
    flexDirection: 'row',
    gap: spacing.sm,
  },
  followInputWrap: {
    flex: 1,
  },
  followInputLabel: {
    color: colors.textMuted,
    fontSize: fontSize.xs,
    marginBottom: spacing.xs,
  },
  followInput: {
    borderWidth: 1,
    borderColor: colors.cardBorder,
    backgroundColor: colors.card,
    color: colors.text,
    borderRadius: radius.md,
    paddingHorizontal: spacing.md,
    paddingVertical: spacing.sm,
    fontSize: fontSize.sm,
  },
  followHint: {
    color: colors.textMuted,
    fontSize: fontSize.xs,
    marginTop: spacing.sm,
  },
  summaryRow: {
    flexDirection: 'row',
    gap: spacing.sm,
    marginBottom: spacing.md,
  },
  summaryItem: {
    flex: 1,
    borderRadius: radius.lg,
    backgroundColor: colors.surface,
    paddingVertical: spacing.md,
    paddingHorizontal: spacing.sm,
    alignItems: 'center',
  },
  summaryValue: {
    color: colors.white,
    fontSize: fontSize.lg,
    fontWeight: '800',
    fontVariant: ['tabular-nums'],
  },
  summaryLabel: {
    color: colors.textMuted,
    fontSize: fontSize.xs,
    marginTop: spacing.xs,
  },
  sectionHeader: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'center',
    marginBottom: spacing.sm,
    marginTop: spacing.xs,
  },
  sectionTitle: {
    color: colors.white,
    fontSize: fontSize.md,
    fontWeight: '700',
  },
  sectionCount: {
    color: colors.textMuted,
    fontSize: fontSize.sm,
  },
  rowCard: {
    backgroundColor: colors.surface,
    borderRadius: radius.lg,
    padding: spacing.md,
    marginBottom: spacing.sm,
  },
  rowTop: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'center',
    marginBottom: spacing.xs,
  },
  rowTitle: {
    color: colors.white,
    fontSize: fontSize.md,
    fontWeight: '700',
  },
  rowSub: {
    color: colors.textSecondary,
    fontSize: fontSize.sm,
    marginTop: 2,
  },
  sideTag: {
    fontSize: fontSize.sm,
    fontWeight: '700',
  },
  statusTag: {
    fontSize: fontSize.sm,
    fontWeight: '700',
  },
  emptyBox: {
    borderRadius: radius.lg,
    backgroundColor: colors.surface,
    paddingVertical: spacing.xl,
    alignItems: 'center',
    marginBottom: spacing.md,
  },
  emptyText: {
    color: colors.textMuted,
    fontSize: fontSize.sm,
  },
});
