import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import {
  View, Text, TouchableOpacity, StyleSheet, ActivityIndicator, Modal, TextInput, Alert,
} from 'react-native';
import { colors, spacing, radius, fontSize } from '../services/theme';
import api from '../services/api';

// 暖金色板（与全局 theme 统一，保留终端/扫描仪美感）
const C = {
  neon: colors.gold,
  neonDim: colors.goldDark,
  neonGlow: colors.goldGlow,
  neonBg: colors.goldBg,
  panelBg: colors.bg,
  cardBg: colors.card,
  cardBorder: colors.cardBorder,
  surface: colors.surface,
  gridLine: 'rgba(255,184,77,0.08)',
  text: colors.text,
  textDim: colors.textSecondary,
  accent: colors.purple,
  accentBg: colors.purpleBg,
  long: colors.green,
  longBg: colors.greenBg,
  short: colors.red,
  shortBg: colors.redBg,
  warn: colors.yellow,
  warnBg: colors.yellowBg,
};

const FILTERS = [
  { key: 'ALL', label: '全部' },
  { key: 'LONG', label: '做多' },
  { key: 'SHORT', label: '做空' },
];

const AUTO_REFRESH_MS = 45000;
const STRONG_CONFIDENCE = 70;
const MEDIUM_CONFIDENCE = 45;

export default function RecommendPanel({ onNavigateToTrade }) {
  const [data, setData] = useState(null);
  const [history, setHistory] = useState([]);
  const [loading, setLoading] = useState(true);
  const [historyLoading, setHistoryLoading] = useState(true);
  const [filter, setFilter] = useState('ALL');
  const [error, setError] = useState(null);
  const [historyError, setHistoryError] = useState(null);
  const [execModalVisible, setExecModalVisible] = useState(false);
  const [execSubmitting, setExecSubmitting] = useState(false);
  const [execForm, setExecForm] = useState({
    symbol: '',
    direction: 'LONG',
    entry: '',
    stopLoss: '',
    takeProfit: '',
    quoteQuantity: '5',
    leverage: '10',
  });
  const timerRef = useRef(null);
  const [scanDots, setScanDots] = useState('');

  // 扫描动画
  useEffect(() => {
    if (!loading) return;
    const iv = setInterval(() => {
      setScanDots((d) => (d.length >= 3 ? '' : d + '.'));
    }, 400);
    return () => clearInterval(iv);
  }, [loading]);

  const fetchData = useCallback(async (showLoading = false) => {
    if (showLoading) {
      setLoading(true);
      setHistoryLoading(true);
    }
    const [scanResult, historyResult] = await Promise.allSettled([
      api.getRecommendScan(),
      api.getRecommendHistory({ limit: 100 }),
    ]);

    if (scanResult.status === 'fulfilled') {
      setData(scanResult.value?.data || scanResult.value);
      setError(null);
    } else {
      setError(scanResult.reason?.message || '获取推荐信号失败');
    }

    if (historyResult.status === 'fulfilled') {
      const payload = historyResult.value?.data || historyResult.value || {};
      setHistory(payload.items || []);
      setHistoryError(null);
    } else {
      setHistoryError(historyResult.reason?.message || '获取历史信号失败');
    }

    setLoading(false);
    setHistoryLoading(false);
  }, []);

  useEffect(() => {
    fetchData(true);
    timerRef.current = setInterval(() => fetchData(false), AUTO_REFRESH_MS);
    return () => clearInterval(timerRef.current);
  }, [fetchData]);

  const filteredItems = (data?.items || []).filter(
    (item) => filter === 'ALL' || item.direction === filter,
  );
  const filteredHistory = (history || []).filter(
    (item) => filter === 'ALL' || item.direction === filter,
  );

  const sentiment = data?.sentiment;
  const sentimentColor =
    sentiment?.bias === 'bullish' ? C.long :
    sentiment?.bias === 'bearish' ? C.short : C.neon;
  const sentimentTag =
    sentiment?.bias === 'bullish' ? '看涨' :
    sentiment?.bias === 'bearish' ? '看跌' : '中性';

  const execRiskRewardPreview = useMemo(() => {
    const entry = Number(execForm.entry);
    const stopLoss = Number(execForm.stopLoss);
    const takeProfit = Number(execForm.takeProfit);
    if (!(entry > 0 && stopLoss > 0 && takeProfit > 0)) return null;

    if (execForm.direction === 'LONG' && !(stopLoss < entry && entry < takeProfit)) return null;
    if (execForm.direction === 'SHORT' && !(takeProfit < entry && entry < stopLoss)) return null;

    const risk = Math.abs(entry - stopLoss);
    const reward = Math.abs(takeProfit - entry);
    if (risk <= 0 || reward <= 0) return null;
    return reward / risk;
  }, [execForm.direction, execForm.entry, execForm.stopLoss, execForm.takeProfit]);

  const openExecModal = useCallback((item) => {
    if (!item?.symbol) return;
    const direction = item.direction === 'SHORT' ? 'SHORT' : 'LONG';
    setExecForm({
      symbol: String(item.symbol || '').toUpperCase(),
      direction,
      entry: item.entry ? String(item.entry) : '',
      stopLoss: item.stopLoss ? String(item.stopLoss) : '',
      takeProfit: item.takeProfit ? String(item.takeProfit) : '',
      quoteQuantity: '5',
      leverage: '10',
    });
    setExecModalVisible(true);
  }, []);

  const submitExecOrder = useCallback(async () => {
    const symbol = String(execForm.symbol || '').trim().toUpperCase();
    const direction = execForm.direction === 'SHORT' ? 'SHORT' : 'LONG';
    const entry = Number(execForm.entry);
    const stopLoss = Number(execForm.stopLoss);
    const takeProfit = Number(execForm.takeProfit);
    const quoteQuantity = Number(execForm.quoteQuantity);
    const leverage = parseInt(execForm.leverage, 10);

    if (!symbol) return Alert.alert('提示', '交易对不能为空');
    if (!(entry > 0)) return Alert.alert('提示', '入场价必须大于 0');
    if (!(quoteQuantity > 0)) return Alert.alert('提示', '金额必须大于 0');
    if (!(leverage > 0)) return Alert.alert('提示', '杠杆必须大于 0');

    const hasSL = stopLoss > 0;
    const hasTP = takeProfit > 0;
    let riskReward = 0;
    if (hasSL || hasTP) {
      if (!hasSL || !hasTP) {
        return Alert.alert('提示', '止损和止盈需要同时填写');
      }
      if (direction === 'LONG' && !(stopLoss < entry && entry < takeProfit)) {
        return Alert.alert('提示', '做多时需满足：止损 < 入场 < 止盈');
      }
      if (direction === 'SHORT' && !(takeProfit < entry && entry < stopLoss)) {
        return Alert.alert('提示', '做空时需满足：止盈 < 入场 < 止损');
      }
      const risk = Math.abs(entry - stopLoss);
      const reward = Math.abs(takeProfit - entry);
      if (risk <= 0 || reward <= 0) {
        return Alert.alert('提示', '止损/止盈参数无效');
      }
      riskReward = reward / risk;
    }

    const req = {
      source: 'history_signal',
      symbol,
      side: direction === 'LONG' ? 'BUY' : 'SELL',
      positionSide: direction,
      orderType: 'LIMIT',
      price: String(entry),
      timeInForce: 'GTC',
      quoteQuantity: String(quoteQuantity),
      leverage,
    };

    if (riskReward > 0) {
      req.stopLossPrice = String(stopLoss);
      req.riskReward = Number(riskReward.toFixed(4));
    }

    setExecSubmitting(true);
    try {
      const res = await api.placeOrder(req);
      const orderID = res?.data?.order?.orderId || res?.data?.order?.orderID || 'N/A';
      setExecModalVisible(false);
      Alert.alert('下单成功', `订单ID: ${orderID}`);
    } catch (e) {
      Alert.alert('下单失败', e.message || '未知错误');
    } finally {
      setExecSubmitting(false);
    }
  }, [execForm]);

  return (
    <View style={s.root}>
      {renderScanner()}
      {renderExecModal()}
    </View>
  );

  // ==================== SCANNER TAB ====================
  function renderScanner() {
    return (
      <View style={s.tabContent}>
        {/* 顶部标题栏 */}
        <View style={s.header}>
          <View style={s.headerLeft}>
            <Text style={s.headerIcon}>{'{ }'}</Text>
            <View>
              <Text style={s.headerTitle}>AI 信号扫描</Text>
              <Text style={s.headerSub}>多时间框架分析引擎</Text>
            </View>
          </View>
        </View>

        {/* 市场情绪仪表盘 */}
        <View style={s.sentPanel}>
          <View style={s.sentTopRow}>
            <Text style={s.sentLabel}>市场情绪</Text>
            <View style={[s.sentTagWrap, { borderColor: sentimentColor }]}>
              <View style={[s.sentDot, { backgroundColor: sentimentColor, shadowColor: sentimentColor }]} />
              <Text style={[s.sentTag, { color: sentimentColor }]}>{sentimentTag}</Text>
            </View>
          </View>
          {sentiment && (
            <View style={s.sentGrid}>
              <View style={s.sentCell}>
                <Text style={s.sentCellLabel}>资金费率</Text>
                <Text style={[s.sentCellVal, {
                  color: sentiment.fundingRate > 0 ? C.short : sentiment.fundingRate < 0 ? C.long : C.text,
                }]}>
                  {(sentiment.fundingRate * 100).toFixed(4)}%
                </Text>
              </View>
              <View style={s.sentDivider} />
              <View style={s.sentCell}>
                <Text style={s.sentCellLabel}>多空比</Text>
                <Text style={s.sentCellVal}>{sentiment.longShort?.toFixed(2) || '--'}</Text>
              </View>
              <View style={s.sentDivider} />
              <View style={s.sentCell}>
                <Text style={s.sentCellLabel}>1H爆仓</Text>
                <Text style={s.sentCellVal}>${(sentiment.liqTotal / 1e6).toFixed(1)}M</Text>
              </View>
            </View>
          )}
          <View style={s.scanLine} />
        </View>

        {/* 过滤栏 */}
        <View style={s.filterRow}>
          {FILTERS.map((f) => (
            <TouchableOpacity
              key={f.key}
              style={[s.filterChip, filter === f.key && s.filterActive]}
              onPress={() => setFilter(f.key)}
              activeOpacity={0.7}
            >
              <Text style={[s.filterText, filter === f.key && s.filterTextActive]}>{f.label}</Text>
            </TouchableOpacity>
          ))}
          <View style={s.filterCountWrap}>
            <Text style={s.filterCount}>{filteredItems.length}</Text>
            <Text style={s.filterCountLabel}>个信号</Text>
          </View>
        </View>

        {/* 加载态 */}
        {loading && !data && (
          <View style={s.loadingBox}>
            <ActivityIndicator color={C.neon} size="large" />
            <Text style={s.loadingText}>扫描 24 个交易对{scanDots}</Text>
            <Text style={s.loadingHint}>分析 1D / 4H / 1H 时间框架</Text>
          </View>
        )}
        {error && !data && (
          <View style={s.loadingBox}>
            <Text style={s.errorIcon}>⚠</Text>
            <Text style={s.errorText}>连接失败</Text>
            <Text style={s.errorDetail}>{error}</Text>
            <TouchableOpacity style={s.retryBtn} onPress={() => fetchData(true)}>
              <Text style={s.retryText}>重试</Text>
            </TouchableOpacity>
          </View>
        )}
        {filteredItems.length === 0 && data && !loading && (
          <View style={s.loadingBox}>
            <Text style={s.emptyIcon}>◇</Text>
            <Text style={s.emptyText}>暂无信号</Text>
            <Text style={s.loadingHint}>所有资产处于中性区间</Text>
          </View>
        )}

        {/* 推荐卡片 */}
        {filteredItems.map((item, idx) => {
          const isLong = item.direction === 'LONG';
          const sc = isLong ? C.long : C.short;
          const scBg = isLong ? C.longBg : C.shortBg;
          const conf = item.confidence;
          const levelTag = conf >= STRONG_CONFIDENCE ? '强' : conf >= MEDIUM_CONFIDENCE ? '中' : '弱';
          const levelColor = conf >= STRONG_CONFIDENCE ? sc : conf >= MEDIUM_CONFIDENCE ? C.neon : C.textDim;

          return (
            <View key={item.symbol + idx} style={[s.card, { borderLeftColor: sc }]}>
              <View style={[s.cardTopLine, { backgroundColor: sc + '40' }]} />
              <View style={s.cardHeader}>
                <View style={s.cardLeft}>
                  <Text style={s.cardSymbol}>{item.symbol}</Text>
                  <View style={[s.levelBadge, { borderColor: levelColor, backgroundColor: levelColor + '15' }]}>
                    <Text style={[s.levelText, { color: levelColor }]}>{levelTag}</Text>
                  </View>
                </View>
                <View style={s.cardRight}>
                  <View style={[s.dirBadge, { backgroundColor: scBg, borderColor: sc + '55' }]}>
                    <Text style={[s.dirArrow, { color: sc }]}>{isLong ? '▲' : '▼'}</Text>
                    <Text style={[s.dirText, { color: sc }]}>{item.direction === 'LONG' ? '做多' : '做空'}</Text>
                  </View>
                  <View style={[s.confRing, { borderColor: sc }]}>
                    <Text style={[s.confVal, { color: sc }]}>{conf}</Text>
                    <Text style={[s.confPct, { color: sc }]}>%</Text>
                  </View>
                </View>
              </View>

              {(item.signals || []).length > 0 && (
                <View style={s.tfMatrix}>
                  {item.signals.map((sig) => {
                    const tfTag = sig.timeframe === '1d' ? '1D' : sig.timeframe.toUpperCase();
                    const dc = sig.direction === 'LONG' ? C.long :
                      sig.direction === 'SHORT' ? C.short : C.textDim;
                    const arrow = sig.direction === 'LONG' ? '↑' : sig.direction === 'SHORT' ? '↓' : '—';
                    return (
                      <View key={sig.timeframe} style={[s.tfCell, { borderColor: dc + '33' }]}>
                        <Text style={s.tfTag}>{tfTag}</Text>
                        <Text style={[s.tfArrow, { color: dc }]}>{arrow}</Text>
                        <View style={s.tfDataRow}>
                          <Text style={s.tfDataLabel}>RSI</Text>
                          <Text style={[s.tfDataVal, {
                            color: sig.rsi < 30 ? C.long : sig.rsi > 70 ? C.short : C.text,
                          }]}>{sig.rsi?.toFixed(0) || '--'}</Text>
                        </View>
                        {sig.volRatio > 0 && (
                          <View style={s.tfDataRow}>
                            <Text style={s.tfDataLabel}>VOL</Text>
                            <Text style={[s.tfDataVal, {
                              color: sig.volRatio >= 1.5 ? C.warn : C.textDim,
                            }]}>{sig.volRatio.toFixed(1)}x</Text>
                          </View>
                        )}
                      </View>
                    );
                  })}
                </View>
              )}

              <View style={s.reasonBox}>
                {(item.reasons || []).map((r, i) => (
                  <View key={i} style={s.reasonRow}>
                    <Text style={s.reasonBullet}>›</Text>
                    <Text style={s.reasonText}>{r}</Text>
                  </View>
                ))}
              </View>

              <View style={s.priceMatrix}>
                <View style={s.priceCell}>
                  <Text style={s.priceCellLabel}>入场价</Text>
                  <Text style={s.priceCellVal}>${formatPrice(item.entry)}</Text>
                </View>
                <View style={[s.priceDivider, { backgroundColor: C.short + '44' }]} />
                <View style={s.priceCell}>
                  <Text style={[s.priceCellLabel, { color: C.short }]}>止损</Text>
                  <Text style={[s.priceCellVal, { color: C.short }]}>${formatPrice(item.stopLoss)}</Text>
                </View>
                <View style={[s.priceDivider, { backgroundColor: C.long + '44' }]} />
                <View style={s.priceCell}>
                  <Text style={[s.priceCellLabel, { color: C.long }]}>止盈</Text>
                  <Text style={[s.priceCellVal, { color: C.long }]}>${formatPrice(item.takeProfit)}</Text>
                </View>
              </View>

              <TouchableOpacity
                style={[s.execBtn, { borderColor: sc, shadowColor: sc }]}
                onPress={() => onNavigateToTrade?.(item.symbol, item)}
                activeOpacity={0.7}
              >
                <Text style={[s.execBtnText, { color: sc }]}>执行交易  ›</Text>
              </TouchableOpacity>
            </View>
          );
        })}

        <View style={s.historyPanel}>
          <View style={s.historyHeader}>
            <Text style={s.historyTitle}>历史信号记录</Text>
            <Text style={s.historyCount}>{filteredHistory.length} 条</Text>
          </View>

          {historyLoading && history.length === 0 && (
            <View style={s.historyLoadingWrap}>
              <ActivityIndicator color={C.neon} />
              <Text style={s.historyHint}>加载历史信号中...</Text>
            </View>
          )}

          {historyError && history.length === 0 && (
            <View style={s.historyLoadingWrap}>
              <Text style={s.historyErrorText}>{historyError}</Text>
            </View>
          )}

          {!historyLoading && filteredHistory.length === 0 && (
            <View style={s.historyLoadingWrap}>
              <Text style={s.historyHint}>暂无历史信号</Text>
            </View>
          )}

          {filteredHistory.slice(0, 30).map((item, idx) => {
            const isLong = item.direction === 'LONG';
            const isShort = item.direction === 'SHORT';
            const sideColor = isLong ? C.long : isShort ? C.short : C.textDim;
            const key = `${item.id || 'h'}-${item.symbol}-${item.scannedAt || item.createdAt || idx}`;
            return (
              <View key={key} style={s.historyRow}>
                <View style={s.historyTopRow}>
                  <View style={s.historyLeft}>
                    <Text style={s.historySymbol}>{item.symbol || '--'}</Text>
                    <Text style={[s.historyDirection, { color: sideColor }]}>
                      {isLong ? '做多' : isShort ? '做空' : '未知'}
                    </Text>
                  </View>
                  <View style={s.historyRight}>
                    <Text style={[s.historyConfidence, { color: sideColor }]}>
                      {Number(item.confidence || 0)}%
                    </Text>
                    <Text style={s.historyTime}>{formatHistoryTime(item.scannedAt || item.createdAt)}</Text>
                  </View>
                </View>

                <Text style={s.historyPriceLine}>
                  入场 {formatPrice(Number(item.entry || 0))} · 止损 {formatPrice(Number(item.stopLoss || 0))} · 止盈 {formatPrice(Number(item.takeProfit || 0))}
                </Text>

                <TouchableOpacity
                  style={[s.historyExecBtn, { borderColor: sideColor }]}
                  onPress={() => openExecModal(item)}
                  activeOpacity={0.75}
                >
                  <Text style={[s.historyExecBtnText, { color: sideColor }]}>执行该历史信号 ›</Text>
                </TouchableOpacity>
              </View>
            );
          })}
        </View>

        {data && (
          <View style={s.footerRow}>
            <View style={s.footerDot} />
            <Text style={s.footerText}>
              扫描时间: {data.scannedAt ? new Date(data.scannedAt).toLocaleTimeString() : '--'}
            </Text>
            <Text style={s.footerText}>  |  自动刷新: {AUTO_REFRESH_MS / 1000}秒</Text>
          </View>
        )}
      </View>
    );
  }

  function renderExecModal() {
    return (
      <Modal
        visible={execModalVisible}
        transparent
        animationType="fade"
        onRequestClose={() => !execSubmitting && setExecModalVisible(false)}
      >
        <View style={s.execOverlay}>
          <View style={s.execModal}>
            <Text style={s.execTitle}>执行历史信号</Text>
            <Text style={s.execSub}>{execForm.symbol || '--'}</Text>

            <View style={s.execDirRow}>
              <TouchableOpacity
                style={[s.execDirBtn, execForm.direction === 'LONG' && s.execDirBtnLongActive]}
                onPress={() => setExecForm((prev) => ({ ...prev, direction: 'LONG' }))}
              >
                <Text style={[s.execDirText, execForm.direction === 'LONG' && { color: C.long }]}>做多</Text>
              </TouchableOpacity>
              <TouchableOpacity
                style={[s.execDirBtn, execForm.direction === 'SHORT' && s.execDirBtnShortActive]}
                onPress={() => setExecForm((prev) => ({ ...prev, direction: 'SHORT' }))}
              >
                <Text style={[s.execDirText, execForm.direction === 'SHORT' && { color: C.short }]}>做空</Text>
              </TouchableOpacity>
            </View>

            <View style={s.execFieldGrid}>
              <View style={s.execField}>
                <Text style={s.execFieldLabel}>入场价</Text>
                <TextInput
                  style={s.execInput}
                  value={execForm.entry}
                  onChangeText={(v) => setExecForm((prev) => ({ ...prev, entry: v }))}
                  keyboardType="decimal-pad"
                  placeholder="0"
                  placeholderTextColor={C.textDim}
                />
              </View>
              <View style={s.execField}>
                <Text style={s.execFieldLabel}>止损价</Text>
                <TextInput
                  style={s.execInput}
                  value={execForm.stopLoss}
                  onChangeText={(v) => setExecForm((prev) => ({ ...prev, stopLoss: v }))}
                  keyboardType="decimal-pad"
                  placeholder="可选"
                  placeholderTextColor={C.textDim}
                />
              </View>
              <View style={s.execField}>
                <Text style={s.execFieldLabel}>止盈价</Text>
                <TextInput
                  style={s.execInput}
                  value={execForm.takeProfit}
                  onChangeText={(v) => setExecForm((prev) => ({ ...prev, takeProfit: v }))}
                  keyboardType="decimal-pad"
                  placeholder="可选"
                  placeholderTextColor={C.textDim}
                />
              </View>
              <View style={s.execField}>
                <Text style={s.execFieldLabel}>金额(U)</Text>
                <TextInput
                  style={s.execInput}
                  value={execForm.quoteQuantity}
                  onChangeText={(v) => setExecForm((prev) => ({ ...prev, quoteQuantity: v }))}
                  keyboardType="decimal-pad"
                  placeholder="5"
                  placeholderTextColor={C.textDim}
                />
              </View>
              <View style={s.execField}>
                <Text style={s.execFieldLabel}>杠杆</Text>
                <TextInput
                  style={s.execInput}
                  value={execForm.leverage}
                  onChangeText={(v) => setExecForm((prev) => ({ ...prev, leverage: v }))}
                  keyboardType="number-pad"
                  placeholder="10"
                  placeholderTextColor={C.textDim}
                />
              </View>
            </View>

            <Text style={s.execHint}>
              {execRiskRewardPreview ? `当前盈亏比: 1:${execRiskRewardPreview.toFixed(2)}` : '可选：填写止损+止盈自动挂 TPSL'}
            </Text>

            <View style={s.execActionRow}>
              <TouchableOpacity
                style={[s.execActionBtn, s.execCancelBtn]}
                onPress={() => !execSubmitting && setExecModalVisible(false)}
                disabled={execSubmitting}
              >
                <Text style={s.execCancelText}>取消</Text>
              </TouchableOpacity>
              <TouchableOpacity
                style={[s.execActionBtn, s.execConfirmBtn, execSubmitting && s.execConfirmBtnDisabled]}
                onPress={submitExecOrder}
                disabled={execSubmitting}
              >
                {execSubmitting ? (
                  <ActivityIndicator color={C.text} size="small" />
                ) : (
                  <Text style={s.execConfirmText}>直接下单</Text>
                )}
              </TouchableOpacity>
            </View>
          </View>
        </View>
      </Modal>
    );
  }

}

// ==================== 辅助函数 ====================

function formatPrice(price) {
  if (!price) return '--';
  if (price >= 1000) return price.toFixed(1).replace(/\B(?=(\d{3})+(?!\d))/g, ',');
  if (price >= 1) return price.toFixed(2);
  return price.toFixed(4);
}

function formatHistoryTime(v) {
  if (!v) return '--';
  const d = new Date(v);
  if (Number.isNaN(d.getTime())) return '--';
  return d.toLocaleString();
}


// ==================== 科技风样式 ====================
const s = StyleSheet.create({
  root: {
    gap: spacing.sm,
  },
  tabContent: {
    gap: spacing.sm,
  },

  // ===== 顶部标题 =====
  header: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'center',
    paddingVertical: spacing.sm,
  },
  headerLeft: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: spacing.sm,
  },
  headerIcon: {
    fontSize: 22,
    color: C.neon,
    fontFamily: 'monospace',
    fontWeight: '700',
    textShadowColor: C.neonGlow,
    textShadowOffset: { width: 0, height: 0 },
    textShadowRadius: 10,
  },
  headerTitle: {
    fontSize: fontSize.lg,
    fontWeight: '900',
    color: C.text,
    letterSpacing: 1.5,
    fontFamily: 'monospace',
  },
  headerSub: {
    fontSize: 9,
    color: C.textDim,
    letterSpacing: 0.8,
    fontFamily: 'monospace',
    marginTop: 1,
  },
  scanBtn: {
    borderWidth: 1,
    borderColor: C.neon + '55',
    borderRadius: radius.sm,
    overflow: 'hidden',
  },
  scanBtnInner: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 5,
    paddingHorizontal: spacing.md,
    paddingVertical: spacing.xs + 2,
    backgroundColor: C.neonBg,
  },
  scanBtnIcon: {
    fontSize: 12,
    color: C.neon,
  },
  scanBtnText: {
    fontSize: 10,
    fontWeight: '800',
    color: C.neon,
    letterSpacing: 1,
    fontFamily: 'monospace',
  },

  // ===== 情绪面板 =====
  sentPanel: {
    backgroundColor: C.cardBg,
    borderRadius: radius.lg,
    borderWidth: 1,
    borderColor: C.cardBorder,
    padding: spacing.lg,
    overflow: 'hidden',
  },
  sentTopRow: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'center',
    marginBottom: spacing.sm,
  },
  sentLabel: {
    fontSize: 10,
    fontWeight: '700',
    color: C.textDim,
    letterSpacing: 1.5,
    fontFamily: 'monospace',
  },
  sentTagWrap: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 5,
    borderWidth: 1,
    borderRadius: radius.sm,
    paddingHorizontal: spacing.sm,
    paddingVertical: 3,
  },
  sentDot: {
    width: 7,
    height: 7,
    borderRadius: 3.5,
    shadowOffset: { width: 0, height: 0 },
    shadowOpacity: 0.8,
    shadowRadius: 4,
    elevation: 4,
  },
  sentTag: {
    fontSize: 11,
    fontWeight: '900',
    letterSpacing: 1,
    fontFamily: 'monospace',
  },
  sentGrid: {
    flexDirection: 'row',
    alignItems: 'center',
    backgroundColor: C.surface,
    borderRadius: radius.sm,
    padding: spacing.sm,
  },
  sentCell: {
    flex: 1,
    alignItems: 'center',
  },
  sentCellLabel: {
    fontSize: 8,
    fontWeight: '700',
    color: C.textDim,
    letterSpacing: 1,
    fontFamily: 'monospace',
    marginBottom: 3,
  },
  sentCellVal: {
    fontSize: fontSize.sm,
    fontWeight: '800',
    color: C.text,
    fontFamily: 'monospace',
    fontVariant: ['tabular-nums'],
  },
  sentDivider: {
    width: 1,
    height: 24,
    backgroundColor: C.cardBorder,
  },
  scanLine: {
    position: 'absolute',
    bottom: 0,
    left: 0,
    right: 0,
    height: 1,
    backgroundColor: C.neon,
    opacity: 0.15,
  },

  // ===== 过滤栏 =====
  filterRow: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: spacing.sm,
  },
  filterChip: {
    paddingHorizontal: spacing.md,
    paddingVertical: spacing.xs + 1,
    borderRadius: radius.xs,
    backgroundColor: C.surface,
    borderWidth: 1,
    borderColor: C.cardBorder,
  },
  filterActive: {
    backgroundColor: C.neonBg,
    borderColor: C.neon + '66',
  },
  filterText: {
    color: C.textDim,
    fontSize: 11,
    fontWeight: '700',
    letterSpacing: 1,
    fontFamily: 'monospace',
  },
  filterTextActive: {
    color: C.neon,
  },
  filterCountWrap: {
    marginLeft: 'auto',
    flexDirection: 'row',
    alignItems: 'baseline',
    gap: 4,
  },
  filterCount: {
    fontSize: fontSize.lg,
    fontWeight: '900',
    color: C.neon,
    fontFamily: 'monospace',
    fontVariant: ['tabular-nums'],
  },
  filterCountLabel: {
    fontSize: 8,
    fontWeight: '700',
    color: C.textDim,
    letterSpacing: 0.5,
    fontFamily: 'monospace',
  },

  // ===== 加载/空态 =====
  loadingBox: {
    alignItems: 'center',
    paddingVertical: spacing.xxl,
    gap: spacing.sm,
  },
  loadingText: {
    color: C.neon,
    fontSize: fontSize.sm,
    fontWeight: '700',
    letterSpacing: 1,
    fontFamily: 'monospace',
  },
  loadingHint: {
    color: C.textDim,
    fontSize: 10,
    fontFamily: 'monospace',
  },
  errorIcon: {
    fontSize: 28,
    color: C.short,
  },
  errorText: {
    color: C.short,
    fontSize: fontSize.sm,
    fontWeight: '800',
    letterSpacing: 1,
    fontFamily: 'monospace',
  },
  errorDetail: {
    color: C.textDim,
    fontSize: 10,
    fontFamily: 'monospace',
    textAlign: 'center',
  },
  retryBtn: {
    marginTop: spacing.xs,
    paddingHorizontal: spacing.lg,
    paddingVertical: spacing.xs + 2,
    borderRadius: radius.sm,
    borderWidth: 1,
    borderColor: C.neon + '55',
    backgroundColor: C.neonBg,
  },
  retryText: {
    color: C.neon,
    fontWeight: '800',
    fontSize: 11,
    letterSpacing: 1,
    fontFamily: 'monospace',
  },
  emptyIcon: {
    fontSize: 32,
    color: C.textDim,
    fontFamily: 'monospace',
  },
  emptyText: {
    color: C.textDim,
    fontSize: fontSize.sm,
    fontWeight: '700',
    letterSpacing: 1,
    fontFamily: 'monospace',
  },

  // ===== 推荐/分析卡片 =====
  card: {
    backgroundColor: C.cardBg,
    borderRadius: radius.md,
    borderWidth: 1,
    borderColor: C.cardBorder,
    borderLeftWidth: 3,
    padding: spacing.lg,
    overflow: 'hidden',
  },
  cardTopLine: {
    position: 'absolute',
    top: 0,
    left: 0,
    right: 0,
    height: 1,
  },
  cardHeader: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'center',
    marginBottom: spacing.sm,
  },
  cardLeft: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: spacing.sm,
  },
  cardSymbol: {
    fontSize: fontSize.lg,
    fontWeight: '900',
    color: C.text,
    letterSpacing: 0.5,
    fontFamily: 'monospace',
  },
  levelBadge: {
    paddingHorizontal: spacing.sm,
    paddingVertical: 2,
    borderRadius: radius.xs,
    borderWidth: 1,
  },
  levelText: {
    fontSize: 8,
    fontWeight: '800',
    letterSpacing: 1,
    fontFamily: 'monospace',
  },
  cardRight: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: spacing.sm,
  },
  dirBadge: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 3,
    paddingHorizontal: spacing.sm,
    paddingVertical: 3,
    borderRadius: radius.xs,
    borderWidth: 1,
  },
  dirArrow: {
    fontSize: 11,
    fontWeight: '900',
  },
  dirText: {
    fontSize: 10,
    fontWeight: '900',
    letterSpacing: 0.5,
    fontFamily: 'monospace',
  },
  confRing: {
    width: 38,
    height: 38,
    borderRadius: 19,
    borderWidth: 2,
    alignItems: 'center',
    justifyContent: 'center',
  },
  confVal: {
    fontSize: 13,
    fontWeight: '900',
    fontFamily: 'monospace',
    fontVariant: ['tabular-nums'],
    marginTop: -1,
  },
  confPct: {
    fontSize: 7,
    fontWeight: '700',
    marginTop: -2,
    fontFamily: 'monospace',
  },

  // ===== PnL 显示 =====
  pnlVal: {
    fontSize: fontSize.sm,
    fontWeight: '900',
    fontFamily: 'monospace',
    fontVariant: ['tabular-nums'],
  },
  pnlPct: {
    fontSize: 10,
    fontWeight: '700',
    fontFamily: 'monospace',
    fontVariant: ['tabular-nums'],
    marginTop: 1,
  },

  // ===== 持仓信息行 =====
  posInfoRow: {
    flexDirection: 'row',
    backgroundColor: C.surface,
    borderRadius: radius.sm,
    padding: spacing.sm,
    marginBottom: spacing.sm,
    borderWidth: 1,
    borderColor: C.cardBorder,
  },
  posInfoCell: {
    flex: 1,
    alignItems: 'center',
  },
  posInfoLabel: {
    fontSize: 7,
    fontWeight: '700',
    color: C.textDim,
    letterSpacing: 1,
    fontFamily: 'monospace',
    marginBottom: 3,
  },
  posInfoVal: {
    fontSize: 10,
    fontWeight: '800',
    color: C.text,
    fontFamily: 'monospace',
    fontVariant: ['tabular-nums'],
  },

  // ===== 建议横幅 =====
  adviceBanner: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: spacing.sm,
    padding: spacing.sm + 2,
    borderRadius: radius.sm,
    borderWidth: 1,
    marginBottom: spacing.sm,
  },
  adviceIcon: {
    fontSize: 18,
  },
  adviceTag: {
    fontSize: 11,
    fontWeight: '900',
    letterSpacing: 1.5,
    fontFamily: 'monospace',
  },
  adviceDetail: {
    fontSize: 10,
    color: C.textDim,
    fontFamily: 'monospace',
    marginTop: 2,
  },
  confRingSmall: {
    width: 32,
    height: 32,
    borderRadius: 16,
    borderWidth: 1.5,
    alignItems: 'center',
    justifyContent: 'center',
  },
  confValSmall: {
    fontSize: 9,
    fontWeight: '900',
    fontFamily: 'monospace',
  },

  // ===== 时间框架矩阵 =====
  tfMatrix: {
    flexDirection: 'row',
    gap: spacing.sm,
    marginBottom: spacing.sm,
  },
  tfCell: {
    flex: 1,
    backgroundColor: C.surface,
    borderRadius: radius.sm,
    borderWidth: 1,
    paddingVertical: spacing.xs + 1,
    paddingHorizontal: spacing.xs,
    alignItems: 'center',
    gap: 2,
  },
  tfTag: {
    fontSize: 9,
    fontWeight: '900',
    color: C.textDim,
    letterSpacing: 1,
    fontFamily: 'monospace',
  },
  tfArrow: {
    fontSize: 16,
    fontWeight: '900',
    lineHeight: 18,
  },
  tfDataRow: {
    flexDirection: 'row',
    gap: 3,
    alignItems: 'center',
  },
  tfDataLabel: {
    fontSize: 7,
    fontWeight: '700',
    color: C.textDim,
    fontFamily: 'monospace',
    letterSpacing: 0.5,
  },
  tfDataVal: {
    fontSize: 10,
    fontWeight: '800',
    fontFamily: 'monospace',
    fontVariant: ['tabular-nums'],
  },
  tfAlignTag: {
    fontSize: 7,
    fontWeight: '900',
    letterSpacing: 0.5,
    fontFamily: 'monospace',
    marginTop: 1,
  },

  // ===== 信号原因 =====
  reasonBox: {
    marginBottom: spacing.sm,
    gap: 3,
  },
  reasonRow: {
    flexDirection: 'row',
    alignItems: 'flex-start',
    gap: spacing.xs,
  },
  reasonBullet: {
    color: C.neon,
    fontSize: 13,
    fontWeight: '700',
    lineHeight: 16,
    fontFamily: 'monospace',
  },
  reasonText: {
    color: C.textDim,
    fontSize: 11,
    flex: 1,
    fontFamily: 'monospace',
    lineHeight: 16,
  },

  // ===== 价格矩阵 =====
  priceMatrix: {
    flexDirection: 'row',
    alignItems: 'center',
    backgroundColor: C.surface,
    borderRadius: radius.sm,
    padding: spacing.sm,
    borderWidth: 1,
    borderColor: C.cardBorder,
  },
  priceCell: {
    flex: 1,
    alignItems: 'center',
  },
  priceCellLabel: {
    fontSize: 7,
    fontWeight: '700',
    color: C.textDim,
    letterSpacing: 1,
    fontFamily: 'monospace',
    marginBottom: 3,
  },
  priceCellVal: {
    fontSize: fontSize.sm,
    fontWeight: '900',
    color: C.text,
    fontFamily: 'monospace',
    fontVariant: ['tabular-nums'],
  },
  priceDivider: {
    width: 1,
    height: 26,
  },

  // ===== 执行按钮 =====
  execBtn: {
    marginTop: spacing.sm,
    alignSelf: 'stretch',
    alignItems: 'center',
    paddingVertical: spacing.sm + 2,
    borderRadius: radius.sm,
    borderWidth: 1,
    backgroundColor: 'transparent',
    shadowOffset: { width: 0, height: 0 },
    shadowOpacity: 0.3,
    shadowRadius: 8,
    elevation: 4,
  },
  execBtnText: {
    fontSize: 12,
    fontWeight: '900',
    letterSpacing: 2,
    fontFamily: 'monospace',
  },

  // ===== 汇总统计 =====
  analysisSummary: {
    flexDirection: 'row',
    alignItems: 'center',
    backgroundColor: C.cardBg,
    borderRadius: radius.lg,
    borderWidth: 1,
    borderColor: C.cardBorder,
    padding: spacing.lg,
  },
  summaryCell: {
    flex: 1,
    alignItems: 'center',
  },
  summaryCellLabel: {
    fontSize: 8,
    fontWeight: '700',
    color: C.textDim,
    letterSpacing: 1,
    fontFamily: 'monospace',
    marginBottom: 3,
  },
  summaryCellVal: {
    fontSize: fontSize.md,
    fontWeight: '900',
    color: C.neon,
    fontFamily: 'monospace',
    fontVariant: ['tabular-nums'],
  },

  // ===== 历史信号 =====
  historyPanel: {
    backgroundColor: C.cardBg,
    borderRadius: radius.lg,
    borderWidth: 1,
    borderColor: C.cardBorder,
    padding: spacing.md,
    gap: spacing.sm,
  },
  historyHeader: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'center',
  },
  historyTitle: {
    fontSize: fontSize.md,
    fontWeight: '800',
    color: C.text,
    fontFamily: 'monospace',
    letterSpacing: 0.6,
  },
  historyCount: {
    fontSize: 10,
    color: C.textDim,
    fontFamily: 'monospace',
  },
  historyLoadingWrap: {
    alignItems: 'center',
    justifyContent: 'center',
    paddingVertical: spacing.lg,
    gap: spacing.xs,
  },
  historyHint: {
    color: C.textDim,
    fontSize: 10,
    fontFamily: 'monospace',
  },
  historyErrorText: {
    color: C.short,
    fontSize: 10,
    fontFamily: 'monospace',
  },
  historyRow: {
    backgroundColor: C.surface,
    borderRadius: radius.sm,
    borderWidth: 1,
    borderColor: C.cardBorder,
    paddingHorizontal: spacing.sm,
    paddingVertical: spacing.sm,
    gap: 4,
  },
  historyTopRow: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'center',
  },
  historyLeft: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: spacing.xs,
  },
  historyRight: {
    alignItems: 'flex-end',
  },
  historySymbol: {
    fontSize: fontSize.sm,
    color: C.text,
    fontWeight: '800',
    fontFamily: 'monospace',
  },
  historyDirection: {
    fontSize: 10,
    fontWeight: '800',
    fontFamily: 'monospace',
  },
  historyConfidence: {
    fontSize: 12,
    fontWeight: '800',
    fontFamily: 'monospace',
  },
  historyTime: {
    fontSize: 9,
    color: C.textDim,
    fontFamily: 'monospace',
  },
  historyPriceLine: {
    fontSize: 9,
    color: C.textDim,
    fontFamily: 'monospace',
  },
  historyExecBtn: {
    marginTop: 2,
    alignSelf: 'flex-end',
    borderWidth: 1,
    borderRadius: radius.xs,
    paddingHorizontal: spacing.sm,
    paddingVertical: 4,
    backgroundColor: 'transparent',
  },
  historyExecBtnText: {
    fontSize: 10,
    fontWeight: '800',
    fontFamily: 'monospace',
    letterSpacing: 0.4,
  },

  // ===== 执行弹窗 =====
  execOverlay: {
    flex: 1,
    backgroundColor: 'rgba(0,0,0,0.72)',
    justifyContent: 'center',
    alignItems: 'center',
    padding: spacing.lg,
  },
  execModal: {
    width: '100%',
    maxWidth: 420,
    backgroundColor: C.cardBg,
    borderRadius: radius.lg,
    borderWidth: 1,
    borderColor: C.cardBorder,
    padding: spacing.lg,
    gap: spacing.sm,
  },
  execTitle: {
    fontSize: fontSize.lg,
    color: C.text,
    fontWeight: '900',
    fontFamily: 'monospace',
    textAlign: 'center',
    letterSpacing: 0.4,
  },
  execSub: {
    fontSize: 11,
    color: C.textDim,
    fontFamily: 'monospace',
    textAlign: 'center',
  },
  execDirRow: {
    flexDirection: 'row',
    gap: spacing.sm,
  },
  execDirBtn: {
    flex: 1,
    borderWidth: 1,
    borderColor: C.cardBorder,
    borderRadius: radius.sm,
    alignItems: 'center',
    paddingVertical: spacing.sm,
    backgroundColor: C.surface,
  },
  execDirBtnLongActive: {
    borderColor: C.long,
    backgroundColor: C.longBg,
  },
  execDirBtnShortActive: {
    borderColor: C.short,
    backgroundColor: C.shortBg,
  },
  execDirText: {
    color: C.text,
    fontSize: 12,
    fontWeight: '800',
    fontFamily: 'monospace',
  },
  execFieldGrid: {
    gap: spacing.xs,
  },
  execField: {
    gap: 4,
  },
  execFieldLabel: {
    color: C.textDim,
    fontSize: 10,
    fontFamily: 'monospace',
  },
  execInput: {
    borderWidth: 1,
    borderColor: C.cardBorder,
    borderRadius: radius.sm,
    backgroundColor: C.surface,
    color: C.text,
    paddingHorizontal: spacing.sm,
    paddingVertical: spacing.xs + 2,
    fontSize: 12,
    fontFamily: 'monospace',
  },
  execHint: {
    color: C.textDim,
    fontSize: 10,
    fontFamily: 'monospace',
    textAlign: 'center',
  },
  execActionRow: {
    flexDirection: 'row',
    gap: spacing.sm,
    marginTop: spacing.xs,
  },
  execActionBtn: {
    flex: 1,
    alignItems: 'center',
    justifyContent: 'center',
    borderRadius: radius.sm,
    paddingVertical: spacing.sm,
  },
  execCancelBtn: {
    borderWidth: 1,
    borderColor: C.cardBorder,
    backgroundColor: C.surface,
  },
  execCancelText: {
    color: C.textDim,
    fontSize: 12,
    fontWeight: '700',
    fontFamily: 'monospace',
  },
  execConfirmBtn: {
    backgroundColor: C.neonBg,
    borderWidth: 1,
    borderColor: C.neon,
  },
  execConfirmBtnDisabled: {
    opacity: 0.6,
  },
  execConfirmText: {
    color: C.text,
    fontSize: 12,
    fontWeight: '800',
    fontFamily: 'monospace',
  },

  // ===== 底部时间戳 =====
  footerRow: {
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'center',
    gap: 5,
    paddingVertical: spacing.md,
  },
  footerDot: {
    width: 5,
    height: 5,
    borderRadius: 2.5,
    backgroundColor: C.neon,
    opacity: 0.5,
  },
  footerText: {
    fontSize: 9,
    color: C.textDim,
    fontFamily: 'monospace',
    letterSpacing: 0.5,
  },
});
