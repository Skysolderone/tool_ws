import React, { useEffect, useMemo, useState } from 'react';
import { View, Text, StyleSheet, TouchableOpacity, Dimensions } from 'react-native';
import api, { AUTH_TOKEN, WS_BOOK_BASE } from '../services/api';
import { colors } from '../services/theme';

const PRICE_STEP_OPTIONS = [0.1, 1, 5, 10];
const ROW_OPTIONS = [15, 20, 30];
const BOOK_SNAPSHOT_LIMIT = 1000;
const BOOK_WS_LEVELS = 1000;

function toNum(v) {
  const n = parseFloat(v);
  return Number.isFinite(n) ? n : 0;
}

function buildBookRows(levels, step, side, count) {
  if (!levels || levels.length === 0) return [];

  const grouped = new Map();
  const safeStep = Math.max(step, 0.01);

  levels.forEach((lv) => {
    const price = toNum(lv.p);
    const qty = toNum(lv.q);
    if (!price || qty <= 0) return;

    const bucket = side === 'bid'
      ? Math.floor(price / safeStep) * safeStep
      : Math.ceil(price / safeStep) * safeStep;

    const key = bucket.toFixed(8);
    grouped.set(key, (grouped.get(key) || 0) + qty);
  });

  const sorted = Array.from(grouped.entries())
    .map(([price, qty]) => ({ price: Number(price), qty }))
    .sort((a, b) => (side === 'bid' ? b.price - a.price : a.price - b.price))
    .slice(0, count);

  return sorted;
}

function mergeLevels(snapshot, live, side) {
  const map = new Map();

  (snapshot || []).forEach((lv) => {
    const price = toNum(lv.p);
    const qty = toNum(lv.q);
    if (!price || qty <= 0) return;
    map.set(price.toFixed(8), qty);
  });

  (live || []).forEach((lv) => {
    const price = toNum(lv.p);
    const qty = toNum(lv.q);
    if (!price) return;
    const key = price.toFixed(8);
    if (qty <= 0) {
      map.delete(key);
    } else {
      map.set(key, qty);
    }
  });

  return Array.from(map.entries())
    .map(([price, qty]) => ({ p: Number(price), q: qty }))
    .sort((a, b) => (side === 'bid' ? b.p - a.p : a.p - b.p))
    .slice(0, BOOK_SNAPSHOT_LIMIT)
    .map((lv) => ({ p: lv.p.toFixed(2), q: lv.q.toFixed(3) }));
}

function formatQty(q) {
  if (q >= 1000) return (q / 1000).toFixed(1) + 'K';
  if (q >= 1) return q.toFixed(2);
  return q.toFixed(3);
}

function formatPrice(p, step) {
  if (step >= 1) return p.toFixed(2);
  return p.toFixed(2);
}

export default function OrderBookPanel({ symbol }) {
  const [priceStep, setPriceStep] = useState(1);
  const [rowCount, setRowCount] = useState(20);
  const [snapshotBids, setSnapshotBids] = useState([]);
  const [snapshotAsks, setSnapshotAsks] = useState([]);
  const [liveBids, setLiveBids] = useState([]);
  const [liveAsks, setLiveAsks] = useState([]);
  const [connected, setConnected] = useState(false);

  useEffect(() => {
    if (!symbol) return undefined;

    let alive = true;
    let ws = null;
    let reconnectTimer = null;
    let backoff = 1000;
    setSnapshotBids([]);
    setSnapshotAsks([]);
    setLiveBids([]);
    setLiveAsks([]);
    setConnected(false);

    const connectWs = () => {
      if (!alive) return;
      const url = `${WS_BOOK_BASE}?symbol=${symbol}&levels=${BOOK_WS_LEVELS}&token=${AUTH_TOKEN}`;
      ws = new WebSocket(url);

      ws.onopen = () => {
        setConnected(true);
        backoff = 1000;
      };

      ws.onmessage = (evt) => {
        try {
          const data = JSON.parse(evt.data);
          if (!data || data.type !== 'book') return;
          setLiveBids((data.b || []).slice(0, BOOK_WS_LEVELS));
          setLiveAsks((data.a || []).slice(0, BOOK_WS_LEVELS));
        } catch (_) {}
      };

      ws.onerror = () => {};
      ws.onclose = () => {
        setConnected(false);
        if (!alive) return;
        reconnectTimer = setTimeout(() => {
          backoff = Math.min(backoff * 2, 30000);
          connectWs();
        }, backoff);
      };
    };

    const start = async () => {
      try {
        const res = await api.getOrderBook(symbol, BOOK_SNAPSHOT_LIMIT);
        if (!alive) return;
        const data = res?.data || {};
        setSnapshotBids((data.b || []).slice(0, BOOK_SNAPSHOT_LIMIT));
        setSnapshotAsks((data.a || []).slice(0, BOOK_SNAPSHOT_LIMIT));
      } catch (_) {}
      connectWs();
    };
    start();

    return () => {
      alive = false;
      if (reconnectTimer) clearTimeout(reconnectTimer);
      if (ws) {
        ws.onclose = null;
        ws.close();
      }
    };
  }, [symbol]);

  const mergedBids = useMemo(
    () => mergeLevels(snapshotBids, liveBids, 'bid'),
    [snapshotBids, liveBids],
  );
  const mergedAsks = useMemo(
    () => mergeLevels(snapshotAsks, liveAsks, 'ask'),
    [snapshotAsks, liveAsks],
  );

  const bids = useMemo(() => buildBookRows(mergedBids, priceStep, 'bid', rowCount), [mergedBids, priceStep, rowCount]);
  const asks = useMemo(() => buildBookRows(mergedAsks, priceStep, 'ask', rowCount), [mergedAsks, priceStep, rowCount]);

  // 卖盘倒序显示（价格高的在上，低的在下靠近spread）
  const asksReversed = useMemo(() => [...asks].reverse(), [asks]);

  // 计算最大量用于深度条宽度
  const maxQty = useMemo(() => {
    let m = 0;
    bids.forEach((r) => { if (r.qty > m) m = r.qty; });
    asks.forEach((r) => { if (r.qty > m) m = r.qty; });
    return m || 1;
  }, [bids, asks]);

  // 累计量
  const bidsCumQty = useMemo(() => {
    let cum = 0;
    return bids.map((r) => { cum += r.qty; return cum; });
  }, [bids]);

  const asksCumQty = useMemo(() => {
    let cum = 0;
    return asks.map((r) => { cum += r.qty; return cum; });
  }, [asks]);

  const maxCum = useMemo(() => {
    const bc = bidsCumQty.length > 0 ? bidsCumQty[bidsCumQty.length - 1] : 0;
    const ac = asksCumQty.length > 0 ? asksCumQty[asksCumQty.length - 1] : 0;
    return Math.max(bc, ac, 1);
  }, [bidsCumQty, asksCumQty]);

  // reversed asks 的累计量也要倒过来
  const asksCumReversed = useMemo(() => [...asksCumQty].reverse(), [asksCumQty]);

  const spreadInfo = useMemo(() => {
    const askSrc = liveAsks.length > 0 ? liveAsks : mergedAsks;
    const bidSrc = liveBids.length > 0 ? liveBids : mergedBids;
    if (askSrc.length === 0 || bidSrc.length === 0) return { spread: '--', mid: '--' };
    const bestAsk = toNum(askSrc[0].p);
    const bestBid = toNum(bidSrc[0].p);
    if (!bestAsk || !bestBid) return { spread: '--', mid: '--' };
    return {
      spread: (bestAsk - bestBid).toFixed(2),
      mid: ((bestAsk + bestBid) / 2).toFixed(2),
    };
  }, [liveAsks, liveBids, mergedAsks, mergedBids]);

  const totalBidQty = useMemo(() => bids.reduce((s, r) => s + r.qty, 0), [bids]);
  const totalAskQty = useMemo(() => asks.reduce((s, r) => s + r.qty, 0), [asks]);
  const bidPercent = totalBidQty + totalAskQty > 0
    ? Math.round(totalBidQty / (totalBidQty + totalAskQty) * 100) : 50;

  return (
    <View style={styles.panel}>
      {/* Header */}
      <View style={styles.header}>
        <Text style={styles.title}>订单簿</Text>
        <View style={styles.headerRight}>
          <View style={[styles.statusDot, connected ? styles.online : styles.offline]} />
        </View>
      </View>

      {/* 控制行：档位 + 行数 */}
      <View style={styles.controlRow}>
        <View style={styles.chipGroup}>
          {PRICE_STEP_OPTIONS.map((step) => (
            <TouchableOpacity
              key={step}
              style={[styles.chip, priceStep === step && styles.chipActive]}
              onPress={() => setPriceStep(step)}
            >
              <Text style={[styles.chipText, priceStep === step && styles.chipTextActive]}>
                {step}
              </Text>
            </TouchableOpacity>
          ))}
        </View>
        <View style={styles.chipGroup}>
          {ROW_OPTIONS.map((n) => (
            <TouchableOpacity
              key={n}
              style={[styles.chip, rowCount === n && styles.chipActive]}
              onPress={() => setRowCount(n)}
            >
              <Text style={[styles.chipText, rowCount === n && styles.chipTextActive]}>
                {n}档
              </Text>
            </TouchableOpacity>
          ))}
        </View>
      </View>

      {/* 表头 */}
      <View style={styles.tableHeader}>
        <Text style={[styles.hCell, styles.hLeft]}>累计</Text>
        <Text style={[styles.hCell, styles.hRight]}>数量</Text>
        <Text style={[styles.hCell, styles.hRight]}>价格</Text>
      </View>

      {/* === 卖盘（上半部分）=== */}
      {asksReversed.map((row, i) => {
        const barW = maxQty > 0 ? (row.qty / maxQty) * 100 : 0;
        const cumW = maxCum > 0 ? ((asksCumReversed[i] || 0) / maxCum) * 100 : 0;
        return (
          <View key={'a' + i} style={styles.row}>
            {/* 深度条背景 */}
            <View style={[styles.depthBar, styles.askBar, { width: barW + '%' }]} />
            <View style={[styles.cumBar, styles.askCumBar, { width: cumW + '%' }]} />
            {/* 内容 */}
            <Text style={[styles.cell, styles.cellLeft, styles.dimText]}>{formatQty(asksCumReversed[i] || 0)}</Text>
            <Text style={[styles.cell, styles.cellRight]}>{formatQty(row.qty)}</Text>
            <Text style={[styles.cell, styles.cellRight, styles.askPrice]}>{formatPrice(row.price, priceStep)}</Text>
          </View>
        );
      })}

      {/* === Spread 中间栏 === */}
      <View style={styles.spreadRow}>
        <Text style={styles.spreadPrice}>{spreadInfo.mid}</Text>
        <Text style={styles.spreadLabel}>Spread {spreadInfo.spread}</Text>
      </View>

      {/* === 买盘（下半部分）=== */}
      {bids.map((row, i) => {
        const barW = maxQty > 0 ? (row.qty / maxQty) * 100 : 0;
        const cumW = maxCum > 0 ? ((bidsCumQty[i] || 0) / maxCum) * 100 : 0;
        return (
          <View key={'b' + i} style={styles.row}>
            <View style={[styles.depthBar, styles.bidBar, { width: barW + '%' }]} />
            <View style={[styles.cumBar, styles.bidCumBar, { width: cumW + '%' }]} />
            <Text style={[styles.cell, styles.cellLeft, styles.dimText]}>{formatQty(bidsCumQty[i] || 0)}</Text>
            <Text style={[styles.cell, styles.cellRight]}>{formatQty(row.qty)}</Text>
            <Text style={[styles.cell, styles.cellRight, styles.bidPrice]}>{formatPrice(row.price, priceStep)}</Text>
          </View>
        );
      })}

      {/* 买卖力量对比 */}
      <View style={styles.ratioRow}>
        <View style={[styles.ratioBar, styles.ratioBid, { flex: bidPercent }]} />
        <View style={[styles.ratioBar, styles.ratioAsk, { flex: 100 - bidPercent }]} />
      </View>
      <View style={styles.ratioLabelRow}>
        <Text style={[styles.ratioText, { color: colors.greenLight }]}>
          买 {bidPercent}%  ({formatQty(totalBidQty)})
        </Text>
        <Text style={[styles.ratioText, { color: colors.redLight }]}>
          ({formatQty(totalAskQty)})  卖 {100 - bidPercent}%
        </Text>
      </View>
    </View>
  );
}

const styles = StyleSheet.create({
  panel: {
    backgroundColor: colors.card,
    borderRadius: 12,
    padding: 12,
    marginBottom: 12,
    borderWidth: 1,
    borderColor: colors.cardBorder,
  },
  header: {
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
    marginBottom: 6,
  },
  headerRight: { flexDirection: 'row', alignItems: 'center', gap: 8 },
  title: { color: colors.white, fontSize: 16, fontWeight: '700' },
  statusDot: { width: 8, height: 8, borderRadius: 4 },
  online: { backgroundColor: colors.greenLight },
  offline: { backgroundColor: colors.redLight },

  // Controls
  controlRow: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    marginBottom: 8,
    gap: 8,
  },
  chipGroup: { flexDirection: 'row', gap: 4 },
  chip: {
    paddingHorizontal: 8,
    paddingVertical: 3,
    borderRadius: 4,
    backgroundColor: colors.surface,
    borderWidth: 1,
    borderColor: colors.cardBorder,
  },
  chipActive: { backgroundColor: colors.blueBg, borderColor: colors.blue },
  chipText: { color: colors.textSecondary, fontSize: 11, fontWeight: '600' },
  chipTextActive: { color: colors.blue },

  // Table header
  tableHeader: {
    flexDirection: 'row',
    borderBottomWidth: 1,
    borderBottomColor: colors.cardBorder,
    paddingBottom: 4,
    marginBottom: 2,
  },
  hCell: { flex: 1, color: colors.textMuted, fontSize: 10 },
  hLeft: { textAlign: 'left' },
  hRight: { textAlign: 'right' },

  // Rows
  row: {
    flexDirection: 'row',
    height: 20,
    alignItems: 'center',
    position: 'relative',
    overflow: 'hidden',
  },
  depthBar: {
    position: 'absolute',
    right: 0,
    top: 0,
    bottom: 0,
  },
  cumBar: {
    position: 'absolute',
    right: 0,
    top: 0,
    bottom: 0,
  },
  askBar: { backgroundColor: 'rgba(248,81,73,0.15)' },
  bidBar: { backgroundColor: 'rgba(63,185,80,0.15)' },
  askCumBar: { backgroundColor: 'rgba(248,81,73,0.06)' },
  bidCumBar: { backgroundColor: 'rgba(63,185,80,0.06)' },

  cell: {
    flex: 1,
    color: colors.text,
    fontSize: 11,
    fontVariant: ['tabular-nums'],
    zIndex: 1,
  },
  cellLeft: { textAlign: 'left' },
  cellRight: { textAlign: 'right' },
  dimText: { color: colors.textMuted, fontSize: 10 },
  askPrice: { color: colors.redLight, fontWeight: '600' },
  bidPrice: { color: colors.greenLight, fontWeight: '600' },

  // Spread
  spreadRow: {
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'center',
    paddingVertical: 6,
    borderTopWidth: 1,
    borderBottomWidth: 1,
    borderColor: colors.cardBorder,
    marginVertical: 2,
    gap: 10,
  },
  spreadPrice: {
    color: colors.white,
    fontSize: 16,
    fontWeight: 'bold',
    fontVariant: ['tabular-nums'],
  },
  spreadLabel: {
    color: colors.textMuted,
    fontSize: 11,
  },

  // Buy/Sell ratio bar
  ratioRow: {
    flexDirection: 'row',
    height: 4,
    borderRadius: 2,
    overflow: 'hidden',
    marginTop: 10,
  },
  ratioBar: { height: '100%' },
  ratioBid: { backgroundColor: colors.greenLight },
  ratioAsk: { backgroundColor: colors.redLight },
  ratioLabelRow: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    marginTop: 4,
  },
  ratioText: { fontSize: 11, fontWeight: '600' },
});
