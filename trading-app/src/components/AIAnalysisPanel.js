import React, { useCallback, useEffect, useRef, useState } from 'react';
import {
  View, Text, TouchableOpacity, StyleSheet, ActivityIndicator,
} from 'react-native';
import { colors, spacing, radius, fontSize } from '../services/theme';
import api from '../services/api';

// 暖金色板（与 RecommendPanel 共享）
const C = {
  neon: colors.gold,
  neonDim: colors.goldDark,
  neonGlow: colors.goldGlow,
  neonBg: colors.goldBg,
  panelBg: colors.bg,
  cardBg: colors.card,
  cardBorder: colors.cardBorder,
  surface: colors.surface,
  text: colors.text,
  textDim: colors.textSecondary,
  long: colors.green,
  longBg: colors.greenBg,
  short: colors.red,
  shortBg: colors.redBg,
  warn: colors.yellow,
  warnBg: colors.yellowBg,
};

export default function AIAnalysisPanel({ onNavigateToTrade }) {
  const [analyzeData, setAnalyzeData] = useState(null);
  const [analyzeLoading, setAnalyzeLoading] = useState(false);
  const [analyzeError, setAnalyzeError] = useState(null);
  const [scanDots, setScanDots] = useState('');
  const analyzeTimerRef = useRef(null);

  // 扫描动画
  useEffect(() => {
    if (!analyzeLoading) return;
    const iv = setInterval(() => {
      setScanDots((d) => (d.length >= 3 ? '' : d + '.'));
    }, 400);
    return () => clearInterval(iv);
  }, [analyzeLoading]);

  const fetchAnalysis = useCallback(async (showLoading = false) => {
    if (showLoading) setAnalyzeLoading(true);
    try {
      const res = await api.getRecommendAnalyze();
      setAnalyzeData(res.data || res);
      setAnalyzeError(null);
    } catch (e) {
      setAnalyzeError(e.message);
    } finally {
      setAnalyzeLoading(false);
    }
  }, []);

  useEffect(() => {
    fetchAnalysis(true);
    analyzeTimerRef.current = setInterval(() => fetchAnalysis(false), 60000);
    return () => clearInterval(analyzeTimerRef.current);
  }, [fetchAnalysis]);

  const items = analyzeData?.items || [];

  return (
    <View style={s.root}>
      {/* 顶部标题 + 刷新 */}
      <View style={s.headerRow}>
        <View style={s.headerLeft}>
          <Text style={s.headerIcon}>◈</Text>
          <View>
            <Text style={s.headerTitle}>持仓分析</Text>
            <Text style={s.headerSub}>AI 多时间框架智能分析</Text>
          </View>
        </View>
        <TouchableOpacity
          style={s.scanBtn}
          onPress={() => fetchAnalysis(true)}
          activeOpacity={0.7}
        >
          <View style={s.scanBtnInner}>
            <Text style={s.scanBtnIcon}>{analyzeLoading ? '◉' : '⟳'}</Text>
            <Text style={s.scanBtnText}>{analyzeLoading ? '分析中' : '刷新'}</Text>
          </View>
        </TouchableOpacity>
      </View>

      {/* 加载 */}
      {analyzeLoading && !analyzeData && (
        <View style={s.loadingBox}>
          <ActivityIndicator color={C.neon} size="large" />
          <Text style={s.loadingText}>正在分析持仓{scanDots}</Text>
          <Text style={s.loadingHint}>扫描多时间框架信号中</Text>
        </View>
      )}
      {analyzeError && !analyzeData && (
        <View style={s.loadingBox}>
          <Text style={s.errorIcon}>⚠</Text>
          <Text style={s.errorText}>连接失败</Text>
          <Text style={s.errorDetail}>{analyzeError}</Text>
          <TouchableOpacity style={s.retryBtn} onPress={() => fetchAnalysis(true)}>
            <Text style={s.retryText}>重试</Text>
          </TouchableOpacity>
        </View>
      )}
      {items.length === 0 && analyzeData && !analyzeLoading && (
        <View style={s.loadingBox}>
          <Text style={s.emptyIcon}>◇</Text>
          <Text style={s.emptyText}>暂无持仓</Text>
          <Text style={s.loadingHint}>开仓后将自动显示 AI 分析</Text>
        </View>
      )}

      {/* 汇总统计 */}
      {items.length > 0 && (
        <View style={s.analysisSummary}>
          <View style={s.summaryCell}>
            <Text style={s.summaryCellLabel}>持仓数</Text>
            <Text style={s.summaryCellVal}>{items.length}</Text>
          </View>
          <View style={s.dividerLine} />
          <View style={s.summaryCell}>
            <Text style={s.summaryCellLabel}>总盈亏</Text>
            <Text style={[s.summaryCellVal, {
              color: items.reduce((a, b) => a + b.unrealizedPnl, 0) >= 0 ? C.long : C.short,
            }]}>
              ${items.reduce((a, b) => a + b.unrealizedPnl, 0).toFixed(2)}
            </Text>
          </View>
          <View style={s.dividerLine} />
          <View style={s.summaryCell}>
            <Text style={s.summaryCellLabel}>警告</Text>
            <Text style={[s.summaryCellVal, {
              color: items.filter(i => ['close', 'stop_loss'].includes(i.advice)).length > 0 ? C.short : C.neon,
            }]}>
              {items.filter(i => ['close', 'stop_loss', 'reduce'].includes(i.advice)).length}
            </Text>
          </View>
        </View>
      )}

      {/* 持仓分析卡片 */}
      {items.map((item, idx) => {
        const isLong = item.side === 'LONG';
        const sc = isLong ? C.long : C.short;
        const pnlColor = item.unrealizedPnl >= 0 ? C.long : C.short;
        const adviceStyle = getAdviceStyle(item.advice);

        return (
          <View key={item.symbol + idx} style={[s.card, { borderLeftColor: adviceStyle.border }]}>
            <View style={[s.cardTopLine, { backgroundColor: adviceStyle.border + '40' }]} />

            {/* 头部 */}
            <View style={s.cardHeader}>
              <View style={s.cardLeft}>
                <Text style={s.cardSymbol}>{item.symbol}</Text>
                <View style={[s.dirBadge, { backgroundColor: isLong ? C.longBg : C.shortBg, borderColor: sc + '55' }]}>
                  <Text style={[s.dirArrow, { color: sc }]}>{isLong ? '▲' : '▼'}</Text>
                  <Text style={[s.dirText, { color: sc }]}>{item.side === 'LONG' ? '做多' : '做空'}</Text>
                </View>
              </View>
              <View style={{ alignItems: 'flex-end' }}>
                <Text style={[s.pnlVal, { color: pnlColor }]}>
                  {item.unrealizedPnl >= 0 ? '+' : ''}{item.unrealizedPnl?.toFixed(2)} USDT
                </Text>
                <Text style={[s.pnlPct, { color: pnlColor }]}>
                  {item.pnlPercent >= 0 ? '+' : ''}{item.pnlPercent?.toFixed(2)}%
                </Text>
              </View>
            </View>

            {/* 持仓信息 */}
            <View style={s.posInfoRow}>
              <View style={s.posInfoCell}>
                <Text style={s.posInfoLabel}>开仓价</Text>
                <Text style={s.posInfoVal}>${formatPrice(item.entryPrice)}</Text>
              </View>
              <View style={s.posInfoCell}>
                <Text style={s.posInfoLabel}>标记价</Text>
                <Text style={s.posInfoVal}>${formatPrice(item.markPrice)}</Text>
              </View>
              <View style={s.posInfoCell}>
                <Text style={s.posInfoLabel}>数量</Text>
                <Text style={s.posInfoVal}>{Math.abs(item.amount)}</Text>
              </View>
              <View style={s.posInfoCell}>
                <Text style={s.posInfoLabel}>杠杆</Text>
                <Text style={s.posInfoVal}>{item.leverage}x</Text>
              </View>
            </View>

            {/* AI 建议 */}
            <View style={[s.adviceBanner, { backgroundColor: adviceStyle.bg, borderColor: adviceStyle.border + '55' }]}>
              <Text style={[s.adviceIcon, { color: adviceStyle.border }]}>{adviceStyle.icon}</Text>
              <View style={{ flex: 1 }}>
                <Text style={[s.adviceTag, { color: adviceStyle.border }]}>{adviceStyle.tag}</Text>
                <Text style={s.adviceDetail}>{item.adviceLabel}</Text>
              </View>
              {item.confidence > 0 && (
                <View style={[s.confRingSmall, { borderColor: adviceStyle.border }]}>
                  <Text style={[s.confValSmall, { color: adviceStyle.border }]}>{item.confidence}%</Text>
                </View>
              )}
            </View>

            {/* TF 矩阵 */}
            {(item.signals || []).length > 0 && (
              <View style={s.tfMatrix}>
                {item.signals.map((sig) => {
                  const tfTag = sig.timeframe === '1d' ? '1D' : sig.timeframe.toUpperCase();
                  const dc = sig.direction === 'LONG' ? C.long :
                    sig.direction === 'SHORT' ? C.short : C.textDim;
                  const arrow = sig.direction === 'LONG' ? '↑' : sig.direction === 'SHORT' ? '↓' : '—';
                  const aligned = sig.direction === item.side;
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
                      {sig.direction && (
                        <Text style={[s.tfAlignTag, {
                          color: aligned ? C.long : C.short,
                        }]}>{aligned ? '一致' : '相反'}</Text>
                      )}
                    </View>
                  );
                })}
              </View>
            )}

            {/* 信号原因 */}
            <View style={s.reasonBox}>
              {(item.reasons || []).slice(0, 4).map((r, i) => (
                <View key={i} style={s.reasonRow}>
                  <Text style={s.reasonBullet}>›</Text>
                  <Text style={s.reasonText}>{r}</Text>
                </View>
              ))}
            </View>

            {/* 止损止盈（基于AI推荐方向） */}
            {item.stopLoss > 0 && item.takeProfit > 0 && (
              <View style={s.priceMatrixWrap}>
                {item.direction && item.direction !== item.side && (
                  <View style={[s.aiDirTag, { backgroundColor: item.direction === 'LONG' ? C.longBg : C.shortBg, borderColor: (item.direction === 'LONG' ? C.long : C.short) + '55' }]}>
                    <Text style={[s.aiDirText, { color: item.direction === 'LONG' ? C.long : C.short }]}>
                      AI {item.direction === 'LONG' ? '▲ 看多' : '▼ 看空'}
                    </Text>
                  </View>
                )}
                <View style={s.priceMatrix}>
                  <View style={s.priceCell}>
                    <Text style={[s.priceCellLabel, { color: C.short }]}>止损</Text>
                    <Text style={[s.priceCellVal, { color: C.short }]}>${formatPrice(item.stopLoss)}</Text>
                  </View>
                  <View style={[s.priceDivider, { backgroundColor: C.cardBorder }]} />
                  <View style={s.priceCell}>
                    <Text style={[s.priceCellLabel, { color: C.long }]}>止盈</Text>
                    <Text style={[s.priceCellVal, { color: C.long }]}>${formatPrice(item.takeProfit)}</Text>
                  </View>
                </View>
              </View>
            )}

            {/* 操作按钮 */}
            <TouchableOpacity
              style={[s.execBtn, { borderColor: C.neon, shadowColor: C.neon }]}
              onPress={() => onNavigateToTrade?.(item.symbol, {
                direction: item.direction || item.side,
                stopLoss: item.stopLoss,
                takeProfit: item.takeProfit,
              })}
              activeOpacity={0.7}
            >
              <Text style={[s.execBtnText, { color: C.neon }]}>管理持仓  ›</Text>
            </TouchableOpacity>
          </View>
        );
      })}

      {analyzeData && (
        <View style={s.footerRow}>
          <View style={s.footerDot} />
          <Text style={s.footerText}>
            分析时间: {analyzeData.analyzedAt ? new Date(analyzeData.analyzedAt).toLocaleTimeString() : '--'}
          </Text>
          <Text style={s.footerText}>  |  自动刷新: 60秒</Text>
        </View>
      )}
    </View>
  );
}

// ==================== 辅助函数 ====================

function formatPrice(price) {
  if (!price) return '--';
  if (price >= 1000) return price.toFixed(1).replace(/\B(?=(\d{3})+(?!\d))/g, ',');
  if (price >= 1) return price.toFixed(2);
  return price.toFixed(4);
}

function getAdviceStyle(advice) {
  switch (advice) {
    case 'add':
      return { icon: '⊕', tag: '建议加仓', border: C.long, bg: C.longBg };
    case 'take_profit':
      return { icon: '◎', tag: '建议止盈', border: C.warn, bg: C.warnBg };
    case 'reduce':
      return { icon: '⊖', tag: '建议减仓', border: C.warn, bg: C.warnBg };
    case 'stop_loss':
      return { icon: '⛔', tag: '建议止损', border: C.short, bg: C.shortBg };
    case 'close':
      return { icon: '✕', tag: '建议平仓', border: C.short, bg: C.shortBg };
    default:
      return { icon: '◆', tag: '继续持有', border: C.neon, bg: C.neonBg };
  }
}

// ==================== 样式 ====================
const s = StyleSheet.create({
  root: {
    gap: spacing.sm,
  },
  headerRow: {
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

  // 加载/空态
  loadingBox: { alignItems: 'center', paddingVertical: spacing.xxl, gap: spacing.sm },
  loadingText: { color: C.neon, fontSize: fontSize.sm, fontWeight: '700', letterSpacing: 1, fontFamily: 'monospace' },
  loadingHint: { color: C.textDim, fontSize: 10, fontFamily: 'monospace' },
  errorIcon: { fontSize: 28, color: C.short },
  errorText: { color: C.short, fontSize: fontSize.sm, fontWeight: '800', letterSpacing: 1, fontFamily: 'monospace' },
  errorDetail: { color: C.textDim, fontSize: 10, fontFamily: 'monospace', textAlign: 'center' },
  retryBtn: { marginTop: spacing.xs, paddingHorizontal: spacing.lg, paddingVertical: spacing.xs + 2, borderRadius: radius.sm, borderWidth: 1, borderColor: C.neon + '55', backgroundColor: C.neonBg },
  retryText: { color: C.neon, fontWeight: '800', fontSize: 11, letterSpacing: 1, fontFamily: 'monospace' },
  emptyIcon: { fontSize: 32, color: C.textDim, fontFamily: 'monospace' },
  emptyText: { color: C.textDim, fontSize: fontSize.sm, fontWeight: '700', letterSpacing: 1, fontFamily: 'monospace' },

  // 汇总统计
  analysisSummary: {
    flexDirection: 'row',
    alignItems: 'center',
    backgroundColor: C.cardBg,
    borderRadius: radius.lg,
    borderWidth: 1,
    borderColor: C.cardBorder,
    padding: spacing.lg,
  },
  summaryCell: { flex: 1, alignItems: 'center' },
  summaryCellLabel: { fontSize: 8, fontWeight: '700', color: C.textDim, letterSpacing: 1, fontFamily: 'monospace', marginBottom: 3 },
  summaryCellVal: { fontSize: fontSize.md, fontWeight: '900', color: C.neon, fontFamily: 'monospace', fontVariant: ['tabular-nums'] },
  dividerLine: { width: 1, height: 24, backgroundColor: C.cardBorder },

  // 卡片
  card: {
    backgroundColor: C.cardBg,
    borderRadius: radius.lg,
    borderWidth: 1,
    borderColor: C.cardBorder,
    borderLeftWidth: 3,
    padding: spacing.lg,
    overflow: 'hidden',
  },
  cardTopLine: { position: 'absolute', top: 0, left: 0, right: 0, height: 1 },
  cardHeader: { flexDirection: 'row', justifyContent: 'space-between', alignItems: 'center', marginBottom: spacing.sm },
  cardLeft: { flexDirection: 'row', alignItems: 'center', gap: spacing.sm },
  cardSymbol: { fontSize: fontSize.lg, fontWeight: '900', color: C.text, letterSpacing: 0.5, fontFamily: 'monospace' },
  dirBadge: { flexDirection: 'row', alignItems: 'center', gap: 3, paddingHorizontal: spacing.sm, paddingVertical: 3, borderRadius: radius.xs, borderWidth: 1 },
  dirArrow: { fontSize: 11, fontWeight: '900' },
  dirText: { fontSize: 10, fontWeight: '900', letterSpacing: 0.5, fontFamily: 'monospace' },

  // PnL
  pnlVal: { fontSize: fontSize.sm, fontWeight: '900', fontFamily: 'monospace', fontVariant: ['tabular-nums'] },
  pnlPct: { fontSize: 10, fontWeight: '700', fontFamily: 'monospace', fontVariant: ['tabular-nums'], marginTop: 1 },

  // 持仓信息
  posInfoRow: { flexDirection: 'row', backgroundColor: C.surface, borderRadius: radius.sm, padding: spacing.sm, marginBottom: spacing.sm, borderWidth: 1, borderColor: C.cardBorder },
  posInfoCell: { flex: 1, alignItems: 'center' },
  posInfoLabel: { fontSize: 7, fontWeight: '700', color: C.textDim, letterSpacing: 1, fontFamily: 'monospace', marginBottom: 3 },
  posInfoVal: { fontSize: 10, fontWeight: '800', color: C.text, fontFamily: 'monospace', fontVariant: ['tabular-nums'] },

  // 建议
  adviceBanner: { flexDirection: 'row', alignItems: 'center', gap: spacing.sm, padding: spacing.sm + 2, borderRadius: radius.sm, borderWidth: 1, marginBottom: spacing.sm },
  adviceIcon: { fontSize: 18 },
  adviceTag: { fontSize: 11, fontWeight: '900', letterSpacing: 1.5, fontFamily: 'monospace' },
  adviceDetail: { fontSize: 10, color: C.textDim, fontFamily: 'monospace', marginTop: 2 },
  confRingSmall: { width: 32, height: 32, borderRadius: 16, borderWidth: 1.5, alignItems: 'center', justifyContent: 'center' },
  confValSmall: { fontSize: 9, fontWeight: '900', fontFamily: 'monospace' },

  // TF 矩阵
  tfMatrix: { flexDirection: 'row', gap: spacing.sm, marginBottom: spacing.sm },
  tfCell: { flex: 1, backgroundColor: C.surface, borderRadius: radius.sm, borderWidth: 1, paddingVertical: spacing.xs + 1, paddingHorizontal: spacing.xs, alignItems: 'center', gap: 2 },
  tfTag: { fontSize: 9, fontWeight: '900', color: C.textDim, letterSpacing: 1, fontFamily: 'monospace' },
  tfArrow: { fontSize: 16, fontWeight: '900', lineHeight: 18 },
  tfDataRow: { flexDirection: 'row', gap: 3, alignItems: 'center' },
  tfDataLabel: { fontSize: 7, fontWeight: '700', color: C.textDim, fontFamily: 'monospace', letterSpacing: 0.5 },
  tfDataVal: { fontSize: 10, fontWeight: '800', fontFamily: 'monospace', fontVariant: ['tabular-nums'] },
  tfAlignTag: { fontSize: 7, fontWeight: '900', letterSpacing: 0.5, fontFamily: 'monospace', marginTop: 1 },

  // 信号原因
  reasonBox: { marginBottom: spacing.sm, gap: 3 },
  reasonRow: { flexDirection: 'row', alignItems: 'flex-start', gap: spacing.xs },
  reasonBullet: { color: C.neon, fontSize: 13, fontWeight: '700', lineHeight: 16, fontFamily: 'monospace' },
  reasonText: { color: C.textDim, fontSize: 11, flex: 1, fontFamily: 'monospace', lineHeight: 16 },

  // 价格矩阵
  priceMatrixWrap: { gap: spacing.xs },
  aiDirTag: { alignSelf: 'flex-start', flexDirection: 'row', alignItems: 'center', paddingHorizontal: spacing.sm, paddingVertical: 3, borderRadius: radius.xs, borderWidth: 1 },
  aiDirText: { fontSize: 10, fontWeight: '900', letterSpacing: 0.5, fontFamily: 'monospace' },
  priceMatrix: { flexDirection: 'row', alignItems: 'center', backgroundColor: C.surface, borderRadius: radius.sm, padding: spacing.sm, borderWidth: 1, borderColor: C.cardBorder },
  priceCell: { flex: 1, alignItems: 'center' },
  priceCellLabel: { fontSize: 7, fontWeight: '700', color: C.textDim, letterSpacing: 1, fontFamily: 'monospace', marginBottom: 3 },
  priceCellVal: { fontSize: fontSize.sm, fontWeight: '900', color: C.text, fontFamily: 'monospace', fontVariant: ['tabular-nums'] },
  priceDivider: { width: 1, height: 26 },

  // 执行按钮
  execBtn: { marginTop: spacing.sm, alignSelf: 'stretch', alignItems: 'center', paddingVertical: spacing.sm + 2, borderRadius: radius.sm, borderWidth: 1, backgroundColor: 'transparent', shadowOffset: { width: 0, height: 0 }, shadowOpacity: 0.3, shadowRadius: 8, elevation: 4 },
  execBtnText: { fontSize: 12, fontWeight: '900', letterSpacing: 2, fontFamily: 'monospace' },

  // 底部
  footerRow: { flexDirection: 'row', alignItems: 'center', justifyContent: 'center', gap: 5, paddingVertical: spacing.md },
  footerDot: { width: 5, height: 5, borderRadius: 2.5, backgroundColor: C.neon, opacity: 0.5 },
  footerText: { fontSize: 9, color: C.textDim, fontFamily: 'monospace', letterSpacing: 0.5 },
});
