import React, { useState, useEffect, useMemo, useRef, useCallback } from 'react';
import {
  View,
  Text,
  TextInput,
  TouchableOpacity,
  StyleSheet,
  Alert,
  ActivityIndicator,
} from 'react-native';
import api, { WS_PRICE_BASE, AUTH_TOKEN } from '../services/api';
import { colors } from '../services/theme';

export default function OrderPanel({ symbol }) {
  const [side, setSide] = useState('BUY');
  const [quoteQty, setQuoteQty] = useState('5');
  const [leverage, setLeverage] = useState('10');
  const [stopLossAmount, setStopLossAmount] = useState('');
  const [riskReward, setRiskReward] = useState('');
  const [loading, setLoading] = useState(false);
  const [result, setResult] = useState(null);
  const [markPrice, setMarkPrice] = useState(null);
  const [walletBalance, setWalletBalance] = useState(null);
  const lastUpdateRef = useRef(0);
  const pendingPriceRef = useRef(null);
  const rafRef = useRef(null);

  // 节流更新价格，最多 200ms 一次，避免过度渲染
  const throttledSetPrice = useCallback((price) => {
    pendingPriceRef.current = price;
    const now = Date.now();
    if (now - lastUpdateRef.current >= 200) {
      lastUpdateRef.current = now;
      setMarkPrice(price);
    } else if (!rafRef.current) {
      rafRef.current = setTimeout(() => {
        lastUpdateRef.current = Date.now();
        setMarkPrice(pendingPriceRef.current);
        rafRef.current = null;
      }, 200 - (now - lastUpdateRef.current));
    }
  }, []);

  // 通过后端 WebSocket 代理获取实时价格（后端连币安，转发给 app）
  useEffect(() => {
    if (!symbol) return;
    const url = `${WS_PRICE_BASE}?symbol=${symbol}&token=${AUTH_TOKEN}`;
    let ws = null;
    let reconnectTimer = null;
    let backoff = 1000;

    const connect = () => {
      ws = new WebSocket(url);
      ws.onopen = () => {
        backoff = 1000; // 连接成功重置
      };
      ws.onmessage = (evt) => {
        try {
          const data = JSON.parse(evt.data);
          if (data.p) throttledSetPrice(parseFloat(data.p));
        } catch (_) {}
      };
      ws.onerror = () => {};
      ws.onclose = () => {
        reconnectTimer = setTimeout(() => {
          backoff = Math.min(backoff * 2, 30000);
          connect();
        }, backoff);
      };
    };

    connect();
    return () => {
      if (reconnectTimer) clearTimeout(reconnectTimer);
      if (rafRef.current) clearTimeout(rafRef.current);
      if (ws) { ws.onclose = null; ws.close(); }
    };
  }, [symbol, throttledSetPrice]);

  const [positions, setPositions] = useState([]);

  // 定期获取钱包余额 + 当前持仓（全仓模式计算强平价需要）
  useEffect(() => {
    let alive = true;
    const fetchAccountData = async () => {
      try {
        const [balRes, posRes] = await Promise.all([
          api.getBalance(),
          api.getPositions(),
        ]);
        if (!alive) return;
        if (balRes.data) {
          setWalletBalance(parseFloat(balRes.data.crossWalletBalance || balRes.data.balance || '0'));
        }
        if (posRes.data) {
          setPositions(posRes.data);
        }
      } catch (_) {}
    };
    fetchAccountData();
    const timer = setInterval(fetchAccountData, 10000);
    return () => { alive = false; clearInterval(timer); };
  }, []);

  // 实时计算止损价、止盈价、强平价
  const tpslPreview = useMemo(() => {
    if (!markPrice) return null;
    const qty = parseFloat(quoteQty);
    const lev = parseInt(leverage, 10);
    if (!qty || !lev) return null;

    // 新订单的仓位数量
    const newQuantity = (qty * lev) / markPrice;
    // 币安 ETHUSDT 维持保证金率
    const mmRate = 0.005;

    const result = {};

    // ========== 止盈止损预览 ==========
    const sl = parseFloat(stopLossAmount);
    const rr = parseFloat(riskReward);
    if (sl && rr) {
      const slDistance = sl / newQuantity;
      if (side === 'BUY') {
        result.slPrice = (markPrice - slDistance).toFixed(2);
        result.tpPrice = (markPrice + slDistance * rr).toFixed(2);
      } else {
        result.slPrice = (markPrice + slDistance).toFixed(2);
        result.tpPrice = (markPrice - slDistance * rr).toFixed(2);
      }
      result.tpProfit = (sl * rr).toFixed(2);
    }

    // ========== 强平价（合并已有持仓）==========
    const newPosSide = side === 'BUY' ? 'LONG' : 'SHORT';
    let existingQty = 0;
    let existingEntry = 0;
    let existingLiqPrice = null;

    for (const pos of positions) {
      const posAmt = parseFloat(pos.positionAmt || pos.PositionAmt || 0);
      const posEntry = parseFloat(pos.entryPrice || pos.EntryPrice || 0);
      const posSym = pos.symbol || pos.Symbol || '';
      const posSide = pos.positionSide || pos.PositionSide || 'BOTH';
      const posLiq = parseFloat(pos.liquidationPrice || pos.LiquidationPrice || 0);

      if (posAmt === 0) continue;

      if (posSym === symbol && (posSide === newPosSide || posSide === 'BOTH')) {
        existingQty = Math.abs(posAmt);
        existingEntry = posEntry;
        if (posLiq > 0) existingLiqPrice = posLiq;
      }
    }

    // 合并后的总仓位
    const totalQty = existingQty + newQuantity;
    const avgEntry = totalQty > 0
      ? (existingQty * existingEntry + newQuantity * markPrice) / totalQty
      : markPrice;

    // 全仓强平价公式（币安官方）:
    // 全仓下，强平触发条件: 保证金余额 = 维持保证金
    // 即: WB + UPNL = posNotional × MMR
    //
    // 用全仓总保证金(WB)推导:
    // LONG:  LiqPrice = avgEntry - (WB - posNotional × MMR) / totalQty
    // SHORT: LiqPrice = avgEntry + (WB - posNotional × MMR) / totalQty
    //
    // 如果没有余额数据，用杠杆估算:
    //   margin ≈ posNotional / leverage
    //   LONG:  LiqPrice = avgEntry × (1 - 1/lev + MMR)
    //   SHORT: LiqPrice = avgEntry × (1 + 1/lev - MMR)

    let liqPrice;
    const wb = walletBalance;
    const posNotional = avgEntry * totalQty;

    if (wb != null && wb > 0) {
      // 有真实余额数据
      const maintMargin = posNotional * mmRate;
      if (side === 'BUY') {
        liqPrice = avgEntry - (wb - maintMargin) / totalQty;
      } else {
        liqPrice = avgEntry + (wb - maintMargin) / totalQty;
      }
      result.walletBalance = wb.toFixed(2);
      const effectiveLev = posNotional / wb;
      result.effectiveLev = effectiveLev.toFixed(1);
    } else {
      // 无余额数据，按杠杆估算
      if (side === 'BUY') {
        liqPrice = avgEntry * (1 - 1 / lev + mmRate);
      } else {
        liqPrice = avgEntry * (1 + 1 / lev - mmRate);
      }
    }

    if (liqPrice < 0) liqPrice = 0;
    result.liqPrice = liqPrice.toFixed(2);

    // 如果已有持仓且币安返回了强平价，也一并显示
    if (existingLiqPrice) {
      result.binanceLiqPrice = existingLiqPrice.toFixed(2);
    }

    result.existingQty = existingQty > 0 ? existingQty.toFixed(4) : null;
    result.totalQty = totalQty.toFixed(4);
    result.avgEntry = avgEntry.toFixed(2);

    return result;
  }, [markPrice, stopLossAmount, riskReward, quoteQty, leverage, side, walletBalance, positions, symbol]);

  const handleOrder = async () => {
    if (!symbol) return Alert.alert('提示', '请选择交易对');
    if (!quoteQty) return Alert.alert('提示', '请输入下单金额');
    if (!leverage) return Alert.alert('提示', '请输入杠杆倍数');

    const req = {
      symbol,
      side,
      positionSide: side === 'BUY' ? 'LONG' : 'SHORT',
      orderType: 'MARKET',
      quoteQuantity: quoteQty,
      leverage: parseInt(leverage, 10),
    };

    if (stopLossAmount && riskReward) {
      req.stopLossAmount = parseFloat(stopLossAmount);
      req.riskReward = parseFloat(riskReward);
    }

    setLoading(true);
    setResult(null);
    try {
      const data = await api.placeOrder(req);
      setResult(data.data);
      Alert.alert('下单成功', `订单ID: ${data.data?.order?.orderId || 'N/A'}`);
    } catch (e) {
      Alert.alert('下单失败', e.message);
    } finally {
      setLoading(false);
    }
  };

  return (
    <View style={styles.panel}>
      <View style={styles.titleRow}>
        <Text style={styles.title}>下单</Text>
        {markPrice && (
          <Text style={styles.priceTag}>{symbol} {markPrice.toFixed(2)}</Text>
        )}
      </View>

      {/* 方向选择 */}
      <View style={styles.sideRow}>
        <TouchableOpacity
          style={[styles.sideBtn, side === 'BUY' && styles.buyActive]}
          onPress={() => setSide('BUY')}
        >
          <Text style={[styles.sideText, side === 'BUY' && styles.sideTextActive]}>
            做多 BUY
          </Text>
        </TouchableOpacity>
        <TouchableOpacity
          style={[styles.sideBtn, side === 'SELL' && styles.sellActive]}
          onPress={() => setSide('SELL')}
        >
          <Text style={[styles.sideText, side === 'SELL' && styles.sideTextActive]}>
            做空 SELL
          </Text>
        </TouchableOpacity>
      </View>

      {/* 金额和杠杆 */}
      <View style={styles.inputRow}>
        <View style={styles.inputGroup}>
          <Text style={styles.inputLabel}>金额 (USDT)</Text>
          <TextInput
            style={styles.input}
            value={quoteQty}
            onChangeText={setQuoteQty}
            keyboardType="decimal-pad"
            placeholder="5"
            placeholderTextColor={colors.textMuted}
          />
        </View>
        <View style={styles.inputGroup}>
          <Text style={styles.inputLabel}>杠杆</Text>
          <TextInput
            style={styles.input}
            value={leverage}
            onChangeText={setLeverage}
            keyboardType="number-pad"
            placeholder="10"
            placeholderTextColor={colors.textMuted}
          />
        </View>
      </View>

      {/* 快捷杠杆 */}
      <View style={styles.leverageRow}>
        {['5', '10', '20', '50', '100'].map((lev) => (
          <TouchableOpacity
            key={lev}
            style={[styles.levChip, leverage === lev && styles.levChipActive]}
            onPress={() => setLeverage(lev)}
          >
            <Text style={[styles.levChipText, leverage === lev && styles.levChipTextActive]}>
              {lev}x
            </Text>
          </TouchableOpacity>
        ))}
      </View>

      {/* 止盈止损 */}
      <Text style={styles.subTitle}>止盈止损（可选）</Text>
      <View style={styles.inputRow}>
        <View style={styles.inputGroup}>
          <Text style={styles.inputLabel}>止损金额 (U)</Text>
          <TextInput
            style={styles.input}
            value={stopLossAmount}
            onChangeText={setStopLossAmount}
            keyboardType="decimal-pad"
            placeholder="如 1"
            placeholderTextColor={colors.textMuted}
          />
        </View>
        <View style={styles.inputGroup}>
          <Text style={styles.inputLabel}>盈亏比</Text>
          <TextInput
            style={styles.input}
            value={riskReward}
            onChangeText={setRiskReward}
            keyboardType="decimal-pad"
            placeholder="如 3 (1:3)"
            placeholderTextColor={colors.textMuted}
          />
        </View>
      </View>

      {/* 实时价格预览 */}
      {tpslPreview && (
        <View style={styles.previewBox}>
          {tpslPreview.slPrice && (
            <View style={styles.previewRow}>
              <Text style={styles.previewLabelRed}>止损价</Text>
              <Text style={styles.previewValueRed}>{tpslPreview.slPrice}</Text>
              <Text style={styles.previewLoss}>-{stopLossAmount} U</Text>
            </View>
          )}
          {tpslPreview.tpPrice && (
            <View style={styles.previewRow}>
              <Text style={styles.previewLabelGreen}>止盈价</Text>
              <Text style={styles.previewValueGreen}>{tpslPreview.tpPrice}</Text>
              <Text style={styles.previewProfit}>+{tpslPreview.tpProfit} U</Text>
            </View>
          )}
          <View style={styles.previewRow}>
            <Text style={styles.previewLabelOrange}>强平价</Text>
            <Text style={styles.previewValueOrange}>{tpslPreview.liqPrice}</Text>
            {tpslPreview.effectiveLev && (
              <Text style={styles.previewWallet}>实际 {tpslPreview.effectiveLev}x</Text>
            )}
          </View>
          {tpslPreview.binanceLiqPrice && (
            <View style={styles.previewRow}>
              <Text style={styles.previewLabelOrange}>当前强平</Text>
              <Text style={styles.previewValueOrange}>{tpslPreview.binanceLiqPrice}</Text>
            </View>
          )}
          <View style={styles.previewRow}>
            <Text style={styles.previewDetail}>
              {tpslPreview.walletBalance ? `余额 ${tpslPreview.walletBalance} U | ` : ''}
              {tpslPreview.existingQty
                ? `已有 ${tpslPreview.existingQty} → 合计 ${tpslPreview.totalQty} | 均价 ${tpslPreview.avgEntry}`
                : `数量 ${tpslPreview.totalQty} | 均价 ${tpslPreview.avgEntry}`
              }
            </Text>
          </View>
        </View>
      )}

      {/* 下单按钮 */}
      <TouchableOpacity
        style={[
          styles.orderBtn,
          side === 'BUY' ? styles.orderBtnBuy : styles.orderBtnSell,
          loading && styles.disabled,
        ]}
        onPress={handleOrder}
        disabled={loading}
      >
        {loading ? (
          <ActivityIndicator color="#fff" />
        ) : (
          <Text style={styles.orderBtnText}>
            {side === 'BUY' ? '做多' : '做空'} {symbol || '---'} / 市价
          </Text>
        )}
      </TouchableOpacity>

      {/* 下单结果 */}
      {result && (
        <View style={styles.resultBox}>
          <Text style={styles.resultText}>
            主单: {result.order?.status} | 均价: {result.order?.avgPrice || result.order?.price}
          </Text>
          {result.takeProfit && (
            <Text style={styles.resultGreen}>
              止盈: {result.takeProfit.triggerPrice} (algoId: {result.takeProfit.algoId})
            </Text>
          )}
          {result.stopLoss && (
            <Text style={styles.resultRed}>
              止损: {result.stopLoss.triggerPrice} (algoId: {result.stopLoss.algoId})
            </Text>
          )}
        </View>
      )}
    </View>
  );
}

const styles = StyleSheet.create({
  panel: {
    backgroundColor: colors.card,
    borderRadius: 12,
    padding: 16,
    marginBottom: 12,
    borderWidth: 1,
    borderColor: colors.cardBorder,
  },
  titleRow: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'center',
    marginBottom: 12,
  },
  title: { fontSize: 18, fontWeight: 'bold', color: colors.white },
  priceTag: {
    fontSize: 13,
    color: colors.yellow || '#f0b90b',
    fontWeight: '600',
  },
  subTitle: { fontSize: 14, color: colors.textSecondary, marginBottom: 8, marginTop: 4 },

  sideRow: { flexDirection: 'row', gap: 8, marginBottom: 12 },
  sideBtn: {
    flex: 1,
    paddingVertical: 10,
    borderRadius: 8,
    alignItems: 'center',
    backgroundColor: colors.surface,
    borderWidth: 1,
    borderColor: colors.cardBorder,
  },
  buyActive: { backgroundColor: colors.green, borderColor: colors.green },
  sellActive: { backgroundColor: colors.red, borderColor: colors.red },
  sideText: { color: colors.textSecondary, fontWeight: '600', fontSize: 15 },
  sideTextActive: { color: colors.white },

  inputRow: { flexDirection: 'row', gap: 8, marginBottom: 12 },
  inputGroup: { flex: 1 },
  inputLabel: { color: colors.textSecondary, fontSize: 12, marginBottom: 4 },
  input: {
    backgroundColor: colors.bg,
    borderRadius: 8,
    padding: 10,
    color: colors.white,
    borderWidth: 1,
    borderColor: colors.cardBorder,
    fontSize: 15,
  },

  leverageRow: { flexDirection: 'row', gap: 6, marginBottom: 12 },
  levChip: {
    flex: 1,
    paddingVertical: 6,
    borderRadius: 6,
    alignItems: 'center',
    backgroundColor: colors.surface,
    borderWidth: 1,
    borderColor: colors.cardBorder,
  },
  levChipActive: { backgroundColor: colors.blue, borderColor: colors.blue },
  levChipText: { color: colors.textSecondary, fontSize: 12, fontWeight: '600' },
  levChipTextActive: { color: colors.white },

  previewBox: {
    backgroundColor: colors.bg,
    borderRadius: 8,
    padding: 10,
    marginBottom: 12,
    borderWidth: 1,
    borderColor: colors.cardBorder,
  },
  previewRow: {
    flexDirection: 'row',
    alignItems: 'center',
    paddingVertical: 4,
  },
  previewLabelRed: {
    color: colors.redLight || '#ef5350',
    fontSize: 13,
    fontWeight: '600',
    width: 52,
  },
  previewValueRed: {
    color: colors.redLight || '#ef5350',
    fontSize: 15,
    fontWeight: 'bold',
    flex: 1,
  },
  previewLoss: {
    color: colors.redLight || '#ef5350',
    fontSize: 13,
    fontWeight: '600',
  },
  previewLabelGreen: {
    color: colors.greenLight || '#66bb6a',
    fontSize: 13,
    fontWeight: '600',
    width: 52,
  },
  previewValueGreen: {
    color: colors.greenLight || '#66bb6a',
    fontSize: 15,
    fontWeight: 'bold',
    flex: 1,
  },
  previewProfit: {
    color: colors.greenLight || '#66bb6a',
    fontSize: 13,
    fontWeight: '600',
  },
  previewLabelOrange: {
    color: '#ffa726',
    fontSize: 13,
    fontWeight: '600',
    width: 52,
  },
  previewValueOrange: {
    color: '#ffa726',
    fontSize: 15,
    fontWeight: 'bold',
    flex: 1,
  },
  previewWallet: {
    color: colors.textSecondary,
    fontSize: 12,
  },
  previewDetail: {
    color: colors.textMuted || colors.textSecondary,
    fontSize: 11,
    flex: 1,
  },

  orderBtn: { paddingVertical: 14, borderRadius: 10, alignItems: 'center', marginTop: 4 },
  orderBtnBuy: { backgroundColor: colors.green },
  orderBtnSell: { backgroundColor: colors.red },
  disabled: { opacity: 0.6 },
  orderBtnText: { color: colors.white, fontSize: 16, fontWeight: 'bold' },

  resultBox: {
    marginTop: 12,
    padding: 10,
    backgroundColor: colors.bg,
    borderRadius: 8,
  },
  resultText: { color: colors.text, fontSize: 13 },
  resultGreen: { color: colors.greenLight, fontSize: 13, marginTop: 4 },
  resultRed: { color: colors.redLight, fontSize: 13, marginTop: 4 },
});
