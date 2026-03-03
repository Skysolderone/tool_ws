import React, { useState, useEffect, useCallback } from 'react';
import {
  View, Text, TextInput, TouchableOpacity,
  StyleSheet, Alert, Modal, LayoutAnimation, Platform, UIManager,
} from 'react-native';
import api from '../services/api';
import { colors, spacing, radius, fontSize } from '../services/theme';

// Android 需要开启 LayoutAnimation
if (Platform.OS === 'android' && UIManager.setLayoutAnimationEnabledExperimental) {
  UIManager.setLayoutAnimationEnabledExperimental(true);
}

const DETAIL_TABS = [
  { key: 'info', label: '详情' },
  { key: 'tpsl', label: '止盈止损' },
  { key: 'trades', label: '交易记录' },
];

export default function PositionPanel({
  symbol,
  positions: externalPositions,
  liveMarkPrice = null,
  onRefreshPositions,
}) {
  const [localPositions, setLocalPositions] = useState([]);
  const [loading, setLoading] = useState(false);
  const [reduceModal, setReduceModal] = useState(null);
  const [reducePercent, setReducePercent] = useState('50');
  // 展开状态: posKey -> true/false
  const [expandedKey, setExpandedKey] = useState(null);
  // 每个展开卡的子 tab
  const [detailTab, setDetailTab] = useState('info');
  // TPSL 数据缓存
  const [tpslData, setTpslData] = useState({});
  // 交易记录缓存
  const [tradesData, setTradesData] = useState({});

  const positions = Array.isArray(externalPositions) ? externalPositions : localPositions;

  const fetchPositions = useCallback(async () => {
    if (onRefreshPositions) {
      await onRefreshPositions();
      return;
    }
    try {
      const data = await api.getPositions();
      setLocalPositions(data.data || []);
    } catch (_) {}
  }, [onRefreshPositions]);

  useEffect(() => {
    if (onRefreshPositions) return undefined;
    fetchPositions();
    const timer = setInterval(fetchPositions, 5000);
    return () => clearInterval(timer);
  }, [fetchPositions, onRefreshPositions]);

  const posKey = (pos, idx) => `${pos.symbol}-${pos.positionSide}-${idx}`;

  const toggleExpand = (key) => {
    LayoutAnimation.configureNext(LayoutAnimation.Presets.easeInEaseOut);
    if (expandedKey === key) {
      setExpandedKey(null);
    } else {
      setExpandedKey(key);
      setDetailTab('info');
    }
  };

  const handleDetailTabChange = (tab, pos) => {
    setDetailTab(tab);
    if (tab === 'tpsl' && !tpslData[pos.symbol]) {
      fetchTPSL(pos.symbol);
    }
    if (tab === 'trades' && !tradesData[pos.symbol]) {
      fetchTrades(pos.symbol);
    }
  };

  const fetchTPSL = async (sym) => {
    try {
      const resp = await api.getTPSLList(sym);
      setTpslData((prev) => ({ ...prev, [sym]: resp.data || [] }));
    } catch (_) {
      setTpslData((prev) => ({ ...prev, [sym]: [] }));
    }
  };

  const fetchTrades = async (sym) => {
    try {
      const resp = await api.getTrades(sym, 20);
      setTradesData((prev) => ({ ...prev, [sym]: resp.data || [] }));
    } catch (_) {
      setTradesData((prev) => ({ ...prev, [sym]: [] }));
    }
  };

  const handleCancelTPSL = (item) => {
    Alert.alert(
      '取消止盈止损',
      `确定取消 ${item.conditionType === 'TAKE_PROFIT' ? '止盈' : '止损'} (触发价 ${item.triggerPrice.toFixed(2)})？`,
      [
        { text: '取消', style: 'cancel' },
        {
          text: '确定',
          style: 'destructive',
          onPress: async () => {
            try {
              await api.cancelTPSL({ id: item.id });
              Alert.alert('成功', '已取消');
              fetchTPSL(item.symbol);
            } catch (e) {
              Alert.alert('失败', e.message);
            }
          },
        },
      ],
    );
  };

  const handleCancelTPSLGroup = (groupId, sym) => {
    Alert.alert('取消整组', '确定取消该笔订单的所有止盈止损？', [
      { text: '取消', style: 'cancel' },
      {
        text: '确定',
        style: 'destructive',
        onPress: async () => {
          try {
            await api.cancelTPSL({ groupId });
            Alert.alert('成功', '整组已取消');
            fetchTPSL(sym);
          } catch (e) {
            Alert.alert('失败', e.message);
          }
        },
      },
    ]);
  };

  const handleReverse = (pos) => {
    const amt = parseFloat(pos.positionAmt);
    const direction = amt > 0 ? '多' : '空';
    const newDirection = amt > 0 ? '空' : '多';
    Alert.alert(
      '一键反手',
      `${pos.symbol} ${direction} → ${newDirection}\n将平仓并反向开仓（等值保证金）`,
      [
        { text: '取消', style: 'cancel' },
        {
          text: '确认反手',
          style: 'destructive',
          onPress: async () => {
            setLoading(true);
            try {
              const resp = await api.reversePosition({
                symbol: pos.symbol,
                positionSide: pos.positionSide || '',
              });
              const openId = resp.data?.openOrder?.orderId || 'N/A';
              Alert.alert('反手成功', `已反向开仓，订单ID: ${openId}`);
              fetchPositions();
            } catch (e) {
              Alert.alert('反手失败', e.message);
            } finally {
              setLoading(false);
            }
          },
        },
      ],
    );
  };

  const handleClose = (pos) => {
    const amt = parseFloat(pos.positionAmt);
    const direction = amt > 0 ? '多' : '空';
    Alert.alert(
      '确认平仓',
      `${pos.symbol} ${direction} ${Math.abs(amt)} 个\n将全部市价平仓`,
      [
        { text: '取消', style: 'cancel' },
        {
          text: '确认平仓',
          style: 'destructive',
          onPress: async () => {
            setLoading(true);
            try {
              await api.closePosition({
                symbol: pos.symbol,
                positionSide: pos.positionSide || '',
              });
              Alert.alert('成功', '平仓成功');
              fetchPositions();
            } catch (e) {
              Alert.alert('平仓失败', e.message);
            } finally {
              setLoading(false);
            }
          },
        },
      ],
    );
  };

  const handleReduce = async () => {
    if (!reduceModal) return;
    const pct = parseFloat(reducePercent);
    if (!pct || pct <= 0 || pct > 100) {
      return Alert.alert('提示', '请输入 1-100 的百分比');
    }
    setLoading(true);
    try {
      await api.reducePosition({
        symbol: reduceModal.symbol,
        positionSide: reduceModal.positionSide || '',
        percent: pct,
      });
      Alert.alert('成功', `减仓 ${pct}% 成功`);
      setReduceModal(null);
      fetchPositions();
    } catch (e) {
      Alert.alert('减仓失败', e.message);
    } finally {
      setLoading(false);
    }
  };

  const filtered = symbol
    ? positions.filter((p) => p.symbol === symbol)
    : positions;

  // 渲染展开详情区域
  const renderDetailContent = (pos) => {
    const amt = parseFloat(pos.positionAmt);
    const isLong = amt > 0;
    const entry = parseFloat(pos.entryPrice);
    const mark = (
      liveMarkPrice != null
      && symbol
      && pos.symbol === symbol
      && Number.isFinite(liveMarkPrice)
    ) ? liveMarkPrice : parseFloat(pos.markPrice);
    const liq = parseFloat(pos.liquidationPrice);
    const breakEven = parseFloat(pos.breakEvenPrice || '0');
    const notional = Number.isFinite(mark) ? Math.abs(amt) * mark : 0;
    const lev = Math.max(parseFloat(pos.leverage || '1') || 1, 1);
    const margin = notional / lev;

    switch (detailTab) {
      case 'info':
        return (
          <View style={styles.detailSection}>
            <View style={styles.detailGrid}>
              <View style={styles.detailCell}>
                <Text style={styles.detailLabel}>数量</Text>
                <Text style={styles.detailValue}>{Math.abs(amt)}</Text>
              </View>
              <View style={styles.detailCell}>
                <Text style={styles.detailLabel}>开仓价</Text>
                <Text style={styles.detailValue}>{entry.toFixed(2)}</Text>
              </View>
              <View style={styles.detailCell}>
                <Text style={styles.detailLabel}>标记价</Text>
                <Text style={styles.detailValue}>{mark.toFixed(2)}</Text>
              </View>
              <View style={styles.detailCell}>
                <Text style={styles.detailLabel}>杠杆</Text>
                <Text style={styles.detailValue}>{pos.leverage}x</Text>
              </View>
              {breakEven > 0 && (
                <View style={styles.detailCell}>
                  <Text style={styles.detailLabel}>盈亏平衡</Text>
                  <Text style={styles.detailValue}>{breakEven.toFixed(2)}</Text>
                </View>
              )}
              {liq > 0 && (
                <View style={styles.detailCell}>
                  <Text style={styles.detailLabel}>强平价</Text>
                  <Text style={[styles.detailValue, { color: colors.orange }]}>{liq.toFixed(2)}</Text>
                </View>
              )}
              <View style={styles.detailCell}>
                <Text style={styles.detailLabel}>名义价值</Text>
                <Text style={styles.detailValue}>{notional.toFixed(2)} U</Text>
              </View>
              <View style={styles.detailCell}>
                <Text style={styles.detailLabel}>保证金</Text>
                <Text style={styles.detailValue}>{margin.toFixed(2)} U</Text>
              </View>
            </View>
          </View>
        );

      case 'tpsl': {
        const items = tpslData[pos.symbol] || [];
        // 按 groupId 分组
        const groups = {};
        items.forEach((item) => {
          if (!groups[item.groupId]) groups[item.groupId] = [];
          groups[item.groupId].push(item);
        });
        const groupKeys = Object.keys(groups);

        return (
          <View style={styles.detailSection}>
            <View style={styles.tpslHeader}>
              <Text style={styles.tpslTitle}>活跃条件 ({items.length})</Text>
              <TouchableOpacity onPress={() => fetchTPSL(pos.symbol)} style={styles.tpslRefresh}>
                <Text style={styles.tpslRefreshText}>↻</Text>
              </TouchableOpacity>
            </View>
            {items.length === 0 ? (
              <Text style={styles.tpslEmpty}>暂无止盈止损条件</Text>
            ) : (
              groupKeys.map((gid) => {
                const group = groups[gid];
                return (
                  <View key={gid} style={styles.tpslGroup}>
                    <View style={styles.tpslGroupHeader}>
                      <Text style={styles.tpslGroupLabel}>
                        入场 {group[0].entryPrice.toFixed(2)} | 数量 {group[0].quantity}
                      </Text>
                      <TouchableOpacity
                        onPress={() => handleCancelTPSLGroup(gid, pos.symbol)}
                        style={styles.tpslCancelGroupBtn}
                      >
                        <Text style={styles.tpslCancelGroupText}>取消整组</Text>
                      </TouchableOpacity>
                    </View>
                    {group.map((item) => (
                      <View key={item.id} style={styles.tpslItem}>
                        <View style={styles.tpslItemLeft}>
                          <View style={[
                            styles.tpslTypeBadge,
                            { backgroundColor: item.conditionType === 'TAKE_PROFIT' ? colors.greenBg : colors.redBg },
                          ]}>
                            <Text style={[
                              styles.tpslTypeBadgeText,
                              { color: item.conditionType === 'TAKE_PROFIT' ? colors.greenLight : colors.redLight },
                            ]}>
                              {item.conditionType === 'TAKE_PROFIT' ? 'TP' : 'SL'}
                              {item.levelIndex >= 0 ? ` L${item.levelIndex + 1}` : ''}
                            </Text>
                          </View>
                          <View>
                            <Text style={styles.tpslPrice}>
                              {item.triggerPrice.toFixed(2)}
                            </Text>
                            <Text style={styles.tpslQty}>数量: {item.quantity}</Text>
                          </View>
                        </View>
                        <TouchableOpacity
                          onPress={() => handleCancelTPSL(item)}
                          style={styles.tpslCancelBtn}
                        >
                          <Text style={styles.tpslCancelText}>×</Text>
                        </TouchableOpacity>
                      </View>
                    ))}
                  </View>
                );
              })
            )}
          </View>
        );
      }

      case 'trades': {
        const trades = tradesData[pos.symbol] || [];
        return (
          <View style={styles.detailSection}>
            <View style={styles.tpslHeader}>
              <Text style={styles.tpslTitle}>最近交易 ({trades.length})</Text>
              <TouchableOpacity onPress={() => fetchTrades(pos.symbol)} style={styles.tpslRefresh}>
                <Text style={styles.tpslRefreshText}>↻</Text>
              </TouchableOpacity>
            </View>
            {trades.length === 0 ? (
              <Text style={styles.tpslEmpty}>暂无交易记录</Text>
            ) : (
              trades.slice(0, 10).map((t) => {
                const pnl = t.realizedPnl || 0;
                const isBuy = t.side === 'BUY';
                return (
                  <View key={t.id} style={styles.tradeItem}>
                    <View style={styles.tradeLeft}>
                      <View style={styles.tradeTopRow}>
                        <View style={[
                          styles.tradeSideBadge,
                          { backgroundColor: isBuy ? colors.greenBg : colors.redBg },
                        ]}>
                          <Text style={[
                            styles.tradeSideText,
                            { color: isBuy ? colors.greenLight : colors.redLight },
                          ]}>
                            {t.side}
                          </Text>
                        </View>
                        <Text style={styles.tradeType}>{t.orderType}</Text>
                        <Text style={styles.tradeSource}>{t.source}</Text>
                      </View>
                      <Text style={styles.tradeInfo}>
                        价格 {(t.price || 0).toFixed(2)} | 数量 {t.quantity}
                      </Text>
                      <Text style={styles.tradeTime}>
                        {t.createdAt ? new Date(t.createdAt).toLocaleString('zh-CN', { hour12: false }) : ''}
                      </Text>
                    </View>
                    {t.status === 'CLOSED' && (
                      <Text style={[styles.tradePnl, { color: pnl >= 0 ? colors.greenLight : colors.redLight }]}>
                        {pnl >= 0 ? '+' : ''}{pnl.toFixed(2)} U
                      </Text>
                    )}
                    {t.status === 'OPEN' && (
                      <View style={styles.tradeStatusBadge}>
                        <Text style={styles.tradeStatusText}>持仓中</Text>
                      </View>
                    )}
                  </View>
                );
              })
            )}
          </View>
        );
      }

      default:
        return null;
    }
  };

  return (
    <View style={styles.panel}>
      <View style={styles.header}>
        <Text style={styles.title}>当前持仓</Text>
        <TouchableOpacity onPress={fetchPositions} style={styles.refreshBtn} activeOpacity={0.7}>
          <Text style={styles.refreshText}>↻ 刷新</Text>
        </TouchableOpacity>
      </View>

      {filtered.length === 0 ? (
        <View style={styles.emptyWrap}>
          <Text style={styles.emptyIcon}>◇</Text>
          <Text style={styles.emptyText}>暂无持仓</Text>
        </View>
      ) : (
        filtered.map((pos, idx) => {
          const amt = parseFloat(pos.positionAmt);
          const fallbackPnl = parseFloat(pos.unRealizedProfit);
          const isLong = amt > 0;
          const entry = parseFloat(pos.entryPrice);
          const mark = (
            liveMarkPrice != null
            && symbol
            && pos.symbol === symbol
            && Number.isFinite(liveMarkPrice)
          ) ? liveMarkPrice : parseFloat(pos.markPrice);
          const pnl = (Number.isFinite(entry) && entry > 0 && Number.isFinite(mark))
            ? (mark - entry) * amt
            : fallbackPnl;
          const leverage = Math.max(parseFloat(pos.leverage || '1') || 1, 1);
          const isolatedWallet = parseFloat(pos.isolatedWallet || '0');
          const initialMargin = (Math.abs(amt) * entry) / leverage;
          const marginBase = isolatedWallet > 0 ? isolatedWallet : initialMargin;
          const pnlPct = marginBase > 0 ? ((pnl / marginBase) * 100).toFixed(2) : '0.00';
          const key = posKey(pos, idx);
          const isExpanded = expandedKey === key;

          return (
            <View key={key} style={[styles.card, { borderLeftWidth: 3, borderLeftColor: isLong ? colors.green : colors.red }]}>
              {/* 顶部：点击展开/收起 */}
              <TouchableOpacity
                activeOpacity={0.7}
                onPress={() => toggleExpand(key)}
              >
                <View style={styles.cardTop}>
                  <View style={styles.cardTopLeft}>
                    <Text style={styles.posSymbol}>{pos.symbol}</Text>
                    <View style={[styles.sideBadge, { backgroundColor: isLong ? colors.greenBg : colors.redBg }]}>
                      <Text style={[styles.sideBadgeText, { color: isLong ? colors.greenLight : colors.redLight }]}>
                        {isLong ? 'LONG' : 'SHORT'}
                      </Text>
                    </View>
                    <Text style={styles.levText}>{pos.leverage}×</Text>
                    <Text style={styles.expandArrow}>{isExpanded ? '▲' : '▼'}</Text>
                  </View>
                  <View style={styles.cardTopRight}>
                    <Text style={[styles.pnlValue, { color: pnl >= 0 ? colors.greenLight : colors.redLight }]}>
                      {pnl >= 0 ? '+' : ''}{pnl.toFixed(2)} U
                    </Text>
                    <Text style={[styles.pnlPct, { color: pnl >= 0 ? colors.greenLight : colors.redLight }]}>
                      {pnl >= 0 ? '+' : ''}{pnlPct}%
                    </Text>
                  </View>
                </View>
              </TouchableOpacity>

              {/* 展开后的详情区域 */}
              {isExpanded && (
                <View style={styles.expandedArea}>
                  {/* 子 Tab 切换条 */}
                  <View style={styles.detailTabBar}>
                    {DETAIL_TABS.map((tab) => {
                      const isActive = tab.key === detailTab;
                      return (
                        <TouchableOpacity
                          key={tab.key}
                          style={[styles.detailTab, isActive && styles.detailTabActive]}
                          onPress={() => handleDetailTabChange(tab.key, pos)}
                          activeOpacity={0.7}
                        >
                          <Text style={[styles.detailTabLabel, isActive && styles.detailTabLabelActive]}>
                            {tab.label}
                          </Text>
                        </TouchableOpacity>
                      );
                    })}
                  </View>

                  {/* Tab 内容 */}
                  {renderDetailContent(pos)}

                  {/* 底部操作按钮 */}
                  <View style={styles.actionRow}>
                    <TouchableOpacity
                      style={styles.reduceBtn}
                      onPress={() => { setReducePercent('50'); setReduceModal(pos); }}
                      disabled={loading}
                    >
                      <Text style={styles.reduceBtnText}>减仓</Text>
                    </TouchableOpacity>
                    <TouchableOpacity
                      style={styles.reverseBtn}
                      onPress={() => handleReverse(pos)}
                      disabled={loading}
                    >
                      <Text style={styles.reverseBtnText}>反手</Text>
                    </TouchableOpacity>
                    <TouchableOpacity
                      style={styles.closeBtn}
                      onPress={() => handleClose(pos)}
                      disabled={loading}
                    >
                      <Text style={styles.closeBtnText}>平仓</Text>
                    </TouchableOpacity>
                  </View>
                </View>
              )}

              {/* 未展开时显示简要数据 + 操作 */}
              {!isExpanded && (
                <>
                  <View style={styles.summaryRow}>
                    <Text style={styles.summaryItem}>数量 {Math.abs(amt)}</Text>
                    <Text style={styles.summaryItem}>开仓 {entry.toFixed(2)}</Text>
                    <Text style={styles.summaryItem}>标记 {Number.isFinite(mark) ? mark.toFixed(2) : '--'}</Text>
                  </View>
                  <View style={styles.actionRow}>
                    <TouchableOpacity
                      style={styles.reduceBtn}
                      onPress={() => { setReducePercent('50'); setReduceModal(pos); }}
                      disabled={loading}
                    >
                      <Text style={styles.reduceBtnText}>减仓</Text>
                    </TouchableOpacity>
                    <TouchableOpacity
                      style={styles.reverseBtn}
                      onPress={() => handleReverse(pos)}
                      disabled={loading}
                    >
                      <Text style={styles.reverseBtnText}>反手</Text>
                    </TouchableOpacity>
                    <TouchableOpacity
                      style={styles.closeBtn}
                      onPress={() => handleClose(pos)}
                      disabled={loading}
                    >
                      <Text style={styles.closeBtnText}>平仓</Text>
                    </TouchableOpacity>
                  </View>
                </>
              )}
            </View>
          );
        })
      )}

      {/* 减仓弹窗 */}
      <Modal visible={!!reduceModal} animationType="fade" transparent>
        <View style={styles.overlay}>
          <View style={styles.modal}>
            <Text style={styles.modalTitle}>减仓 {reduceModal?.symbol}</Text>
            <Text style={styles.modalSub}>
              当前: {reduceModal ? Math.abs(parseFloat(reduceModal.positionAmt)) : 0} 个
            </Text>

            <View style={styles.pctRow}>
              {['25', '50', '75', '100'].map((pct) => (
                <TouchableOpacity
                  key={pct}
                  style={[styles.pctChip, reducePercent === pct && styles.pctChipActive]}
                  onPress={() => setReducePercent(pct)}
                >
                  <Text style={[styles.pctChipText, reducePercent === pct && styles.pctChipTextActive]}>
                    {pct}%
                  </Text>
                </TouchableOpacity>
              ))}
            </View>

            <View style={styles.customInput}>
              <Text style={styles.customLabel}>自定义比例 (%)</Text>
              <TextInput
                style={styles.input}
                value={reducePercent}
                onChangeText={setReducePercent}
                keyboardType="decimal-pad"
                placeholder="1-100"
                placeholderTextColor={colors.textMuted}
              />
            </View>

            <View style={styles.modalActions}>
              <TouchableOpacity style={styles.cancelBtn} onPress={() => setReduceModal(null)}>
                <Text style={styles.cancelText}>取消</Text>
              </TouchableOpacity>
              <TouchableOpacity style={styles.confirmBtn} onPress={handleReduce} disabled={loading}>
                <Text style={styles.confirmText}>确认减仓 {reducePercent}%</Text>
              </TouchableOpacity>
            </View>
          </View>
        </View>
      </Modal>
    </View>
  );
}

const styles = StyleSheet.create({
  panel: {
    backgroundColor: colors.card,
    borderRadius: radius.lg,
    padding: spacing.lg,
    borderWidth: 1,
    borderColor: colors.cardBorder,
  },
  header: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'center',
    marginBottom: spacing.md,
  },
  title: {
    fontSize: fontSize.lg,
    fontWeight: '700',
    color: colors.white,
  },
  refreshBtn: {
    paddingHorizontal: spacing.md,
    paddingVertical: spacing.xs,
    borderRadius: radius.pill,
    backgroundColor: colors.goldBg,
    borderWidth: 1,
    borderColor: colors.gold,
  },
  refreshText: {
    color: colors.goldLight,
    fontSize: fontSize.sm,
    fontWeight: '600',
  },

  // 空状态
  emptyWrap: {
    alignItems: 'center',
    paddingVertical: 40,
  },
  emptyIcon: {
    fontSize: 36,
    marginBottom: spacing.sm,
  },
  emptyText: {
    color: colors.textMuted,
    fontSize: fontSize.sm,
  },

  // 持仓卡片
  card: {
    backgroundColor: colors.surface,
    borderRadius: radius.md,
    padding: spacing.md,
    marginBottom: spacing.sm,
  },
  cardTop: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'flex-start',
    marginBottom: spacing.sm,
  },
  cardTopLeft: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: spacing.sm,
  },
  posSymbol: {
    color: colors.white,
    fontSize: fontSize.lg,
    fontWeight: '700',
  },
  sideBadge: {
    paddingHorizontal: spacing.sm,
    paddingVertical: 2,
    borderRadius: radius.sm,
  },
  sideBadgeText: {
    fontSize: fontSize.xs,
    fontWeight: '700',
  },
  levText: {
    fontSize: fontSize.xs,
    color: colors.textMuted,
    fontWeight: '600',
  },
  expandArrow: {
    fontSize: fontSize.xs,
    color: colors.textMuted,
    marginLeft: 2,
  },
  cardTopRight: {
    alignItems: 'flex-end',
  },
  pnlValue: {
    fontSize: fontSize.xl,
    fontWeight: '800',
    fontVariant: ['tabular-nums'],
  },
  pnlPct: {
    fontSize: fontSize.xs,
    fontWeight: '600',
    marginTop: 2,
  },

  // 收起时的摘要行
  summaryRow: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    marginBottom: spacing.sm,
    paddingTop: spacing.xs,
    borderTopWidth: 1,
    borderTopColor: colors.cardBorder,
  },
  summaryItem: {
    color: colors.textSecondary,
    fontSize: fontSize.xs,
    fontVariant: ['tabular-nums'],
  },

  // 展开区域
  expandedArea: {
    borderTopWidth: 1,
    borderTopColor: colors.cardBorder,
    paddingTop: spacing.sm,
  },

  // 详情子 Tab
  detailTabBar: {
    flexDirection: 'row',
    backgroundColor: colors.cardAlt,
    borderRadius: radius.sm,
    padding: 2,
    marginBottom: spacing.md,
  },
  detailTab: {
    flex: 1,
    alignItems: 'center',
    paddingVertical: spacing.sm,
    borderRadius: radius.xs,
  },
  detailTabActive: {
    backgroundColor: colors.goldBg,
    borderWidth: 1,
    borderColor: colors.gold,
  },
  detailTabLabel: {
    fontSize: fontSize.sm,
    fontWeight: '600',
    color: colors.textMuted,
  },
  detailTabLabelActive: {
    color: colors.white,
    fontWeight: '700',
  },

  // 详情内容
  detailSection: {
    marginBottom: spacing.md,
  },
  detailGrid: {
    flexDirection: 'row',
    flexWrap: 'wrap',
    gap: spacing.xs,
  },
  detailCell: {
    width: '48%',
    paddingVertical: spacing.xs,
  },
  detailLabel: {
    color: colors.textMuted,
    fontSize: fontSize.xs,
    marginBottom: 2,
  },
  detailValue: {
    color: colors.text,
    fontSize: fontSize.sm,
    fontWeight: '600',
    fontVariant: ['tabular-nums'],
  },

  // TPSL
  tpslHeader: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'center',
    marginBottom: spacing.sm,
  },
  tpslTitle: {
    color: colors.textSecondary,
    fontSize: fontSize.sm,
    fontWeight: '600',
  },
  tpslRefresh: {
    padding: spacing.xs,
  },
  tpslRefreshText: {
    color: colors.gold,
    fontSize: fontSize.md,
  },
  tpslEmpty: {
    color: colors.textMuted,
    fontSize: fontSize.sm,
    textAlign: 'center',
    paddingVertical: spacing.lg,
  },
  tpslGroup: {
    backgroundColor: colors.cardAlt,
    borderRadius: radius.sm,
    padding: spacing.sm,
    marginBottom: spacing.sm,
  },
  tpslGroupHeader: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'center',
    marginBottom: spacing.sm,
    paddingBottom: spacing.xs,
    borderBottomWidth: 1,
    borderBottomColor: colors.cardBorder,
  },
  tpslGroupLabel: {
    color: colors.textSecondary,
    fontSize: fontSize.xs,
  },
  tpslCancelGroupBtn: {
    paddingHorizontal: spacing.sm,
    paddingVertical: 2,
    borderRadius: radius.xs,
    backgroundColor: colors.redBg,
  },
  tpslCancelGroupText: {
    color: colors.redLight,
    fontSize: fontSize.xs,
    fontWeight: '600',
  },
  tpslItem: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'center',
    paddingVertical: spacing.xs,
  },
  tpslItemLeft: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: spacing.sm,
  },
  tpslTypeBadge: {
    paddingHorizontal: spacing.sm,
    paddingVertical: 2,
    borderRadius: radius.xs,
    minWidth: 36,
    alignItems: 'center',
  },
  tpslTypeBadgeText: {
    fontSize: fontSize.xs,
    fontWeight: '700',
  },
  tpslPrice: {
    color: colors.text,
    fontSize: fontSize.sm,
    fontWeight: '600',
    fontVariant: ['tabular-nums'],
  },
  tpslQty: {
    color: colors.textMuted,
    fontSize: fontSize.xs,
  },
  tpslCancelBtn: {
    width: 24,
    height: 24,
    borderRadius: 12,
    backgroundColor: colors.redBg,
    alignItems: 'center',
    justifyContent: 'center',
  },
  tpslCancelText: {
    color: colors.redLight,
    fontSize: fontSize.md,
    fontWeight: '700',
    lineHeight: 18,
  },

  // 交易记录
  tradeItem: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'center',
    paddingVertical: spacing.sm,
    borderBottomWidth: 1,
    borderBottomColor: colors.cardBorder,
  },
  tradeLeft: {
    flex: 1,
  },
  tradeTopRow: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: spacing.xs,
    marginBottom: 2,
  },
  tradeSideBadge: {
    paddingHorizontal: spacing.xs,
    paddingVertical: 1,
    borderRadius: radius.xs,
  },
  tradeSideText: {
    fontSize: fontSize.xs,
    fontWeight: '700',
  },
  tradeType: {
    color: colors.textSecondary,
    fontSize: fontSize.xs,
  },
  tradeSource: {
    color: colors.textMuted,
    fontSize: fontSize.xs,
  },
  tradeInfo: {
    color: colors.textSecondary,
    fontSize: fontSize.xs,
    fontVariant: ['tabular-nums'],
  },
  tradeTime: {
    color: colors.textMuted,
    fontSize: fontSize.xs,
    marginTop: 1,
  },
  tradePnl: {
    fontSize: fontSize.sm,
    fontWeight: '700',
    fontVariant: ['tabular-nums'],
  },
  tradeStatusBadge: {
    paddingHorizontal: spacing.sm,
    paddingVertical: 2,
    borderRadius: radius.xs,
    backgroundColor: colors.goldBg,
  },
  tradeStatusText: {
    color: colors.gold,
    fontSize: fontSize.xs,
    fontWeight: '600',
  },

  // 操作按钮
  actionRow: {
    flexDirection: 'row',
    gap: spacing.sm,
  },
  reduceBtn: {
    flex: 1,
    paddingVertical: spacing.md,
    borderRadius: radius.sm,
    alignItems: 'center',
    backgroundColor: 'transparent',
    borderWidth: 1.5,
    borderColor: colors.gold,
  },
  reduceBtnText: {
    color: colors.gold,
    fontWeight: '700',
    fontSize: fontSize.sm,
  },
  reverseBtn: {
    flex: 1,
    paddingVertical: spacing.md,
    borderRadius: radius.sm,
    alignItems: 'center',
    backgroundColor: 'transparent',
    borderWidth: 1.5,
    borderColor: colors.gold,
  },
  reverseBtnText: {
    color: colors.gold,
    fontWeight: '700',
    fontSize: fontSize.sm,
  },
  closeBtn: {
    flex: 1,
    paddingVertical: spacing.md,
    borderRadius: radius.sm,
    alignItems: 'center',
    backgroundColor: colors.red,
    shadowColor: colors.redGlow,
    shadowRadius: 6,
    shadowOpacity: 0.5,
    elevation: 3,
  },
  closeBtnText: {
    color: colors.white,
    fontWeight: '700',
    fontSize: fontSize.sm,
  },

  // 减仓弹窗
  overlay: {
    flex: 1,
    backgroundColor: 'rgba(0,0,0,0.75)',
    justifyContent: 'center',
    alignItems: 'center',
    padding: spacing.xl,
  },
  modal: {
    backgroundColor: colors.card,
    borderRadius: radius.xxl,
    borderWidth: 1,
    borderColor: colors.cardBorder,
    padding: spacing.xl,
    width: '100%',
    maxWidth: 400,
    shadowColor: colors.shadow,
    shadowRadius: 12,
    shadowOpacity: 0.8,
    elevation: 6,
  },
  modalTitle: {
    fontSize: fontSize.lg,
    fontWeight: '700',
    color: colors.white,
    textAlign: 'center',
    marginBottom: spacing.xs,
  },
  modalSub: {
    fontSize: fontSize.sm,
    color: colors.textSecondary,
    textAlign: 'center',
    marginBottom: spacing.lg,
  },
  pctRow: {
    flexDirection: 'row',
    gap: spacing.sm,
    marginBottom: spacing.md,
  },
  pctChip: {
    flex: 1,
    paddingVertical: spacing.sm,
    borderRadius: radius.md,
    alignItems: 'center',
    backgroundColor: colors.surface,
  },
  pctChipActive: {
    backgroundColor: colors.goldBg,
  },
  pctChipText: {
    color: colors.textMuted,
    fontWeight: '600',
    fontSize: fontSize.sm,
  },
  pctChipTextActive: {
    color: colors.gold,
    fontWeight: '700',
  },
  customInput: {
    marginBottom: spacing.lg,
  },
  customLabel: {
    color: colors.textMuted,
    fontSize: fontSize.xs,
    marginBottom: spacing.xs,
  },
  input: {
    backgroundColor: colors.surface,
    borderRadius: radius.md,
    padding: spacing.md,
    color: colors.white,
    fontSize: fontSize.md,
  },
  modalActions: {
    flexDirection: 'row',
    gap: spacing.md,
  },
  cancelBtn: {
    flex: 1,
    paddingVertical: spacing.md,
    borderRadius: radius.md,
    alignItems: 'center',
    backgroundColor: colors.surface,
  },
  cancelText: {
    color: colors.textSecondary,
    fontWeight: '600',
    fontSize: fontSize.md,
  },
  confirmBtn: {
    flex: 1,
    paddingVertical: spacing.md,
    borderRadius: radius.md,
    alignItems: 'center',
    backgroundColor: colors.yellow,
  },
  confirmText: {
    color: colors.bg,
    fontWeight: '700',
    fontSize: fontSize.md,
  },
});
