import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import {
  ScrollView, Text, StyleSheet, View, TouchableOpacity,
  TextInput, Alert, Modal, Platform, StatusBar as RNStatusBar,
} from 'react-native';
import { StatusBar } from 'expo-status-bar';

import SubTabBar from './src/components/SubTabBar';
import AccountBar from './src/components/AccountBar';
import SymbolPicker from './src/components/SymbolPicker';
import OrderPanel from './src/components/OrderPanel';
import OrderBookPanel from './src/components/OrderBookPanel';
import PositionPanel from './src/components/PositionPanel';
import AutoScalePanel from './src/components/AutoScalePanel';
import GridPanel from './src/components/GridPanel';
import DCAPanel from './src/components/DCAPanel';
import SignalPanel from './src/components/SignalPanel';
import DojiPanel from './src/components/DojiPanel';
import TradeLogPanel from './src/components/TradeLogPanel';
import NewsPanel from './src/components/NewsPanel';
import AnalyticsPanel from './src/components/AnalyticsPanel';
import FundingPanel from './src/components/FundingPanel';
import AaveMonitorPanel from './src/components/AaveMonitorPanel';
import StrategyLinkPanel from './src/components/StrategyLinkPanel';
import SupportResistancePanel from './src/components/SupportResistancePanel';
import RecommendPanel from './src/components/RecommendPanel';
import ScalpPanel from './src/components/ScalpPanel';
import { colors, spacing, radius, fontSize } from './src/services/theme';
import api, { WS_PRICE_BASE, AUTH_TOKEN } from './src/services/api';

const DEFAULT_SYMBOL = 'ETHUSDT';

// ==================== 底部主 Tab ====================
const MAIN_TABS = [
  { key: 'trade', label: '交易' },
  { key: 'strategy', label: '策略' },
  { key: 'aave', label: '监控' },
  { key: 'recommend', label: 'AI推荐' },
  { key: 'info', label: '资讯' },
  { key: 'me', label: '账户' },
];

// 交易 Tab 子页签
const TRADE_SUB_TABS = [
  { key: 'order', label: '下单' },
  { key: 'position', label: '持仓' },
  { key: 'sr', label: '支撑阻力' },
  { key: 'book', label: '盘口' },
  { key: 'log', label: '日志' },
];


export default function App() {
  const androidStatusBarHeight = Platform.OS === 'android' ? (RNStatusBar.currentHeight || 0) : 0;

  // ===== 主Tab状态 =====
  const [activeTab, setActiveTab] = useState('trade');
  const [tradeSubTab, setTradeSubTab] = useState('order');

  // ===== 全局交易币对 =====
  const [tradeSymbol, setTradeSymbol] = useState(DEFAULT_SYMBOL);
  const [orderPreset, setOrderPreset] = useState(null); // 推荐预设参数
  const [strategySymbol, setStrategySymbol] = useState(DEFAULT_SYMBOL);
  const [accountSnapshot, setAccountSnapshot] = useState({
    balance: null,
    positions: [],
  });

  // ===== 新闻 =====
  const [newsHasNew, setNewsHasNew] = useState(false);

  // ===== 懒加载标记 =====
  // 启动即激活资讯后台连接，保证非资讯页也能收到本地新资讯通知
  const [newsActivated, setNewsActivated] = useState(true);

  // ===== 实时价格（交易Tab顶栏共享） =====
  const [markPrice, setMarkPrice] = useState(null);
  const lastUpdateRef = useRef(0);
  const pendingPriceRef = useRef(null);
  const rafRef = useRef(null);
  const activeMainTabLabel = useMemo(
    () => MAIN_TABS.find((tab) => tab.key === activeTab)?.label || '',
    [activeTab],
  );
  const topPriceText = useMemo(() => {
    if (markPrice == null) return '--';
    if (markPrice >= 1000) return markPrice.toFixed(2);
    if (markPrice >= 1) return markPrice.toFixed(4);
    return markPrice.toFixed(6);
  }, [markPrice]);
  const openPositionCount = useMemo(
    () => (accountSnapshot.positions || []).filter((p) => Math.abs(parseFloat(p.positionAmt || '0')) > 0).length,
    [accountSnapshot.positions],
  );
  const floatingPnl = useMemo(
    () => (accountSnapshot.positions || []).reduce((sum, p) => sum + parseFloat(p.unRealizedProfit || '0'), 0),
    [accountSnapshot.positions],
  );

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

  // ===== 统一账户轮询（余额 + 持仓）=====
  const refreshAccountSnapshot = useCallback(async () => {
    try {
      const [balRes, posRes] = await Promise.all([
        api.getBalance(),
        api.getPositions(),
      ]);
      const balance = parseFloat(balRes?.data?.crossWalletBalance || balRes?.data?.balance || '0');
      setAccountSnapshot({
        balance: Number.isFinite(balance) ? balance : 0,
        positions: posRes?.data || [],
      });
    } catch (_) {}
  }, []);

  useEffect(() => {
    let active = true;
    const run = async () => {
      if (!active) return;
      await refreshAccountSnapshot();
    };
    run();
    const timer = setInterval(run, 2000);
    return () => {
      active = false;
      clearInterval(timer);
    };
  }, [refreshAccountSnapshot]);

  // 全局 WS 价格连接
  useEffect(() => {
    if (!tradeSymbol) return;
    const url = `${WS_PRICE_BASE}?symbol=${tradeSymbol}&token=${AUTH_TOKEN}`;
    let ws = null;
    let reconnectTimer = null;
    let backoff = 1000;

    const connect = () => {
      ws = new WebSocket(url);
      ws.onopen = () => { backoff = 1000; };
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
  }, [tradeSymbol, throttledSetPrice]);

  // ===== Tab 切换逻辑 =====
  useEffect(() => {
    if (activeTab === 'info') setNewsHasNew(false);
  }, [activeTab]);

  const handleNewsHasNew = useCallback((hasNew) => {
    if (hasNew && activeTab !== 'info') setNewsHasNew(true);
  }, [activeTab]);

  const switchMainTab = useCallback((key) => {
    setActiveTab(key);
    if (key === 'info') {
      setNewsActivated(true);
    }
  }, []);

  // ===== Tab 红点/懒加载 =====
  const infoBadge = newsHasNew;
  const newsPanelMounted = newsActivated || activeTab === 'info';

  // ======================== 渲染 ========================
  return (
    <View style={styles.container}>
      <StatusBar style="light" translucent={false} backgroundColor={colors.bg} />

      {/* 顶部安全区 + 标题条 */}
      <View style={[styles.safeTop, { paddingTop: androidStatusBarHeight + 10 }]}>
        <View style={styles.topBar}>
          <View style={styles.topLeft}>
            <Text style={styles.topTitle}>USDT 永续</Text>
            <Text style={styles.topSubtitle}>{tradeSymbol} · {activeMainTabLabel}</Text>
          </View>
          <View style={styles.topRight}>
            <View style={styles.topMetaRow}>
              <Text style={styles.topMetaLabel}>标记价</Text>
              <Text style={styles.topMetaValue}>{topPriceText}</Text>
            </View>
            <View style={styles.topMetaRow}>
              <Text style={styles.topMetaLabel}>持仓</Text>
              <Text style={styles.topMetaValue}>{openPositionCount}</Text>
              <Text style={[
                styles.topPnlText,
                { color: floatingPnl >= 0 ? colors.greenLight : colors.redLight },
              ]}
              >
                {floatingPnl >= 0 ? '+' : ''}{floatingPnl.toFixed(2)}U
              </Text>
            </View>
          </View>
        </View>
      </View>

      {/* 内容区 */}
      <ScrollView
        style={styles.scroll}
        contentContainerStyle={styles.content}
        contentInsetAdjustmentBehavior="automatic"
        keyboardShouldPersistTaps="handled"
        showsVerticalScrollIndicator={false}
      >
        {/* ==================== 交易 Tab ==================== */}
        {activeTab === 'trade' && (
          <>
            <AccountBar
              symbol={tradeSymbol}
              onChangeSymbol={setTradeSymbol}
              markPrice={markPrice}
              balance={accountSnapshot.balance}
              positions={accountSnapshot.positions}
            />
            <SubTabBar
              tabs={TRADE_SUB_TABS}
              activeKey={tradeSubTab}
              onChangeTab={setTradeSubTab}
              style={{ marginTop: spacing.sm }}
            />
            {tradeSubTab === 'order' && (
              <OrderPanel
                symbol={tradeSymbol}
                externalMarkPrice={markPrice}
                walletBalance={accountSnapshot.balance}
                positions={accountSnapshot.positions}
                preset={orderPreset}
              />
            )}
            {tradeSubTab === 'position' && (
              <PositionPanel
                symbol={tradeSymbol}
                positions={accountSnapshot.positions}
                liveMarkPrice={markPrice}
                onRefreshPositions={refreshAccountSnapshot}
              />
            )}
            {tradeSubTab === 'sr' && (
              <SupportResistancePanel symbol={tradeSymbol} externalMarkPrice={markPrice} />
            )}
            {tradeSubTab === 'book' && (
              <OrderBookPanel symbol={tradeSymbol} />
            )}
            {tradeSubTab === 'log' && (
              <TradeLogPanel />
            )}
          </>
        )}

        {/* ==================== 策略 Tab ==================== */}
        {activeTab === 'strategy' && (
          <>
            <View style={styles.sectionHeader}>
              <Text style={styles.sectionTitle}>策略控制台</Text>
              <SymbolPicker symbol={strategySymbol} onChangeSymbol={setStrategySymbol} />
            </View>
            <ScalpPanel symbol={strategySymbol} />
            <StrategyLinkPanel symbol={strategySymbol} />
            <FundingPanel symbol={strategySymbol} />
            <SignalPanel symbol={strategySymbol} />
            <DojiPanel symbol={strategySymbol} />
            <AutoScalePanel symbol={strategySymbol} />
            <GridPanel symbol={strategySymbol} />
            <DCAPanel symbol={strategySymbol} />
          </>
        )}

        {activeTab === 'aave' && (
          <>
            <View style={styles.sectionHeader}>
              <Text style={styles.sectionTitle}>Aave 监控</Text>
            </View>
            <AaveMonitorPanel />
          </>
        )}

        {/* ==================== 推荐 Tab ==================== */}
        {activeTab === 'recommend' && (
          <RecommendPanel onNavigateToTrade={(symbol, recommendation) => {
            setTradeSymbol(symbol);
            setOrderPreset(recommendation ? {
              direction: recommendation.direction,
              stopLoss: recommendation.stopLoss,
              takeProfit: recommendation.takeProfit,
            } : null);
            setActiveTab('trade');
            setTradeSubTab('order');
          }} />
        )}

        {/* ==================== 资讯面板（常驻，按 Tab 显隐） ==================== */}
        {newsPanelMounted && (
          <View style={activeTab === 'info' ? undefined : styles.hidden}>
            <NewsPanel onHasNew={handleNewsHasNew} />
          </View>
        )}

        {/* ==================== 我的 Tab ==================== */}
        {activeTab === 'me' && (
          <DashboardContent tradeSymbol={tradeSymbol} />
        )}
      </ScrollView>

      {/* ==================== 底部 Tab Bar ==================== */}
      <View style={styles.tabBarWrap}>
        <View style={styles.tabBar}>
          {MAIN_TABS.map((tab) => {
            const isActive = activeTab === tab.key;
            const showBadge = (
              (tab.key === 'info' && infoBadge)
            );
            return (
              <TouchableOpacity
                key={tab.key}
                style={[styles.tabItem, isActive && styles.tabItemActive]}
                onPress={() => switchMainTab(tab.key)}
                activeOpacity={0.75}
              >
                {isActive && <View style={styles.tabIndicator} />}
                <Text style={[styles.tabLabel, isActive && styles.tabLabelActive]}>{tab.label}</Text>
                {showBadge && <View style={styles.tabBadgeDot} />}
              </TouchableOpacity>
            );
          })}
        </View>
      </View>
    </View>
  );
}

// ==================== 我的 Tab 内嵌仪表盘 ====================
function DashboardContent({ tradeSymbol }) {
  const [balance, setBalance] = useState(null);
  const [equity, setEquity] = useState(null);
  const [positions, setPositions] = useState([]);
  const [riskStatus, setRiskStatus] = useState(null);
  const [loading, setLoading] = useState(true);
  const [actionLoading, setActionLoading] = useState(false);
  const [reduceModal, setReduceModal] = useState(null);
  const [reducePercent, setReducePercent] = useState('50');

  const fetchAll = useCallback(async () => {
    try {
      const [balRes, posRes, riskRes] = await Promise.allSettled([
        api.getBalance(),
        api.getPositions(),
        api.getRiskStatus(),
      ]);
      if (balRes.status === 'fulfilled' && balRes.value?.data) {
        const wb = parseFloat(balRes.value.data.crossWalletBalance || balRes.value.data.balance || '0');
        const upnl = parseFloat(balRes.value.data.crossUnPnl || '0');
        setBalance(wb);
        setEquity(wb + upnl); // 总权益 = 钱包余额 + 未实现盈亏
      }
      if (posRes.status === 'fulfilled') {
        setPositions(posRes.value?.data || []);
      }
      if (riskRes.status === 'fulfilled') {
        setRiskStatus(riskRes.value?.data || riskRes.value);
      }
    } catch (_) {}
    setLoading(false);
  }, []);

  useEffect(() => {
    fetchAll();
    const iv = setInterval(fetchAll, 8000);
    return () => clearInterval(iv);
  }, [fetchAll]);

  const totalPnl = useMemo(() => {
    return positions.reduce((sum, p) => sum + parseFloat(p.unRealizedProfit || '0'), 0);
  }, [positions]);

  const activePositions = useMemo(() => {
    return positions.filter((p) => Math.abs(parseFloat(p.positionAmt || '0')) > 0);
  }, [positions]);

  const riskDailyLosses = Number(riskStatus?.dailyLosses || 0);
  const riskDailyMaxLosses = Number(riskStatus?.dailyMaxLosses || 0);
  const riskDailyLossAmount = Number(riskStatus?.dailyLossAmount || 0);
  const riskMaxDailyLossAmount = Number(riskStatus?.maxDailyLossAmount || 0);
  const riskConditionLines = riskStatus?.enabled
    ? [
      riskDailyMaxLosses > 0 ? `亏损次数 >= ${riskDailyMaxLosses}` : '亏损次数：不限制',
      riskMaxDailyLossAmount > 0
        ? `累计亏损金额 >= ${riskMaxDailyLossAmount.toFixed(2)} U`
        : '累计亏损金额：不限制',
    ]
    : ['风控未启用'];

  const handleUnlock = useCallback(async () => {
    try {
      await api.unlockRisk();
      Alert.alert('成功', '风控已解锁');
      fetchAll();
    } catch (e) {
      Alert.alert('失败', e.message);
    }
  }, [fetchAll]);

  const handleReduce = useCallback(async () => {
    if (!reduceModal) return;
    const pct = parseFloat(reducePercent);
    if (!pct || pct <= 0 || pct > 100) {
      Alert.alert('提示', '请输入 1-100 的百分比');
      return;
    }
    setActionLoading(true);
    try {
      await api.reducePosition({
        symbol: reduceModal.symbol,
        positionSide: reduceModal.positionSide || '',
        percent: pct,
      });
      Alert.alert('成功', `减仓 ${pct}% 成功`);
      setReduceModal(null);
      fetchAll();
    } catch (e) {
      Alert.alert('减仓失败', e.message);
    } finally {
      setActionLoading(false);
    }
  }, [reduceModal, reducePercent, fetchAll]);

  const handleClose = useCallback((pos) => {
    const amt = parseFloat(pos.positionAmt || '0');
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
            setActionLoading(true);
            try {
              await api.closePosition({
                symbol: pos.symbol,
                positionSide: pos.positionSide || '',
              });
              Alert.alert('成功', '平仓成功');
              fetchAll();
            } catch (e) {
              Alert.alert('平仓失败', e.message);
            } finally {
              setActionLoading(false);
            }
          },
        },
      ],
    );
  }, [fetchAll]);

  const showPositionActions = useCallback((pos) => {
    if (actionLoading) return;
    const amt = parseFloat(pos.positionAmt || '0');
    const sideText = amt > 0 ? '多' : '空';
    Alert.alert(
      `${pos.symbol} ${sideText} 持仓`,
      '请选择操作',
      [
        {
          text: '减仓',
          onPress: () => {
            setReducePercent('50');
            setReduceModal(pos);
          },
        },
        {
          text: '平仓',
          style: 'destructive',
          onPress: () => handleClose(pos),
        },
        { text: '取消', style: 'cancel' },
      ],
    );
  }, [actionLoading, handleClose]);

  return (
    <>
      {/* 账户总览卡 */}
      <View style={styles.dashCard}>
        <Text style={styles.dashCardTitle}>账户总览</Text>
        {/* 总权益 - 英雄数字 */}
        <View style={styles.dashEquitySection}>
          <Text style={styles.dashEquityLabel}>总权益</Text>
          <Text style={styles.dashEquityValue}>{equity != null ? equity.toFixed(2) : '--'}</Text>
        </View>
        {/* 钱包余额 + 持仓盈亏 - 并排 */}
        <View style={styles.dashStatsRow}>
          <View style={styles.dashStat}>
            <Text style={styles.dashStatLabel}>钱包余额</Text>
            <Text style={styles.dashStatValue}>{balance != null ? balance.toFixed(2) : '--'}</Text>
          </View>
          <View style={styles.dashStatDivider} />
          <View style={styles.dashStat}>
            <Text style={styles.dashStatLabel}>持仓盈亏</Text>
            <Text style={[styles.dashStatValue, {
              color: totalPnl >= 0 ? colors.greenLight : colors.redLight,
            }]}>
              {totalPnl >= 0 ? '+' : ''}{totalPnl.toFixed(2)}
            </Text>
          </View>
        </View>
      </View>

      {/* 当前持仓 */}
      <View style={styles.dashCard}>
        <View style={styles.dashCardHeader}>
          <Text style={styles.dashCardTitle}>当前持仓</Text>
          <Text style={styles.dashCardBadge}>{activePositions.length}</Text>
        </View>
        {activePositions.length === 0 ? (
          <Text style={styles.dashEmpty}>暂无持仓</Text>
        ) : (
          activePositions.map((pos) => {
            const amt = parseFloat(pos.positionAmt || '0');
            const pnl = parseFloat(pos.unRealizedProfit || '0');
            const isLong = amt > 0;
            return (
              <TouchableOpacity
                key={pos.symbol + pos.positionSide}
                style={[styles.dashPosItem, { borderLeftColor: isLong ? colors.green : colors.red }]}
                onPress={() => showPositionActions(pos)}
                activeOpacity={0.78}
                disabled={actionLoading}
              >
                <View style={styles.dashPosMainRow}>
                  <View style={styles.dashPosLeft}>
                    <Text style={styles.dashPosSymbol}>{pos.symbol}</Text>
                    <View style={[styles.dashPosSide, { backgroundColor: isLong ? colors.greenBg : colors.redBg }]}>
                      <Text style={{ fontSize: 10, fontWeight: '700', color: isLong ? colors.greenLight : colors.redLight }}>
                        {isLong ? 'LONG' : 'SHORT'}
                      </Text>
                    </View>
                    <Text style={styles.dashPosLev}>{pos.leverage}x</Text>
                  </View>
                  <Text style={[styles.dashPosPnl, { color: pnl >= 0 ? colors.greenLight : colors.redLight }]}>
                    {pnl >= 0 ? '+' : ''}{pnl.toFixed(2)} U
                  </Text>
                </View>
                <Text style={styles.dashPosHint}>点击查看操作</Text>
              </TouchableOpacity>
            );
          })
        )}
      </View>

      {/* 风控状态 */}
      <View style={styles.dashCard}>
        <Text style={styles.dashCardTitle}>风控状态</Text>
        {riskStatus ? (
          <View style={styles.riskRow}>
            <View style={styles.riskItem}>
              <View style={[styles.riskDot, {
                backgroundColor: riskStatus.enabled === false ? colors.textMuted : (riskStatus.locked ? colors.red : colors.green),
              }]}
              />
              <Text style={styles.riskText}>
                {riskStatus.enabled === false ? '未启用' : (riskStatus.locked ? '已锁定' : '正常')}
              </Text>
            </View>
            <View style={styles.riskItem}>
              <Text style={styles.riskLabel}>亏损次数</Text>
              <Text style={styles.riskText}>
                {riskDailyLosses}/{riskDailyMaxLosses > 0 ? riskDailyMaxLosses : '∞'}
              </Text>
            </View>
            <View style={styles.riskItem}>
              <Text style={styles.riskLabel}>亏损金额</Text>
              <Text style={styles.riskText}>
                {riskDailyLossAmount.toFixed(2)}/{riskMaxDailyLossAmount > 0 ? riskMaxDailyLossAmount.toFixed(2) : '∞'} U
              </Text>
            </View>
            {riskStatus.locked && (
              <TouchableOpacity style={styles.riskUnlockBtn} onPress={handleUnlock}>
                <Text style={styles.riskUnlockText}>解锁</Text>
              </TouchableOpacity>
            )}
            {riskStatus.locked && riskStatus.lockReason ? (
              <Text style={styles.riskReason}>原因: {riskStatus.lockReason}</Text>
            ) : null}
            <View style={styles.riskConditions}>
              <Text style={styles.riskLabel}>风控条件</Text>
              {riskConditionLines.map((line, idx) => (
                <Text key={`risk-condition-${idx}`} style={styles.riskConditionText}>{line}</Text>
              ))}
            </View>
          </View>
        ) : (
          <Text style={styles.dashEmpty}>加载中...</Text>
        )}
      </View>

      {/* 数据分析 */}
      <AnalyticsPanel sentimentSymbol={tradeSymbol || 'BTCUSDT'} />

      <Modal visible={!!reduceModal} animationType="fade" transparent>
        <View style={styles.dashModalOverlay}>
          <View style={styles.dashModal}>
            <Text style={styles.dashModalTitle}>减仓 {reduceModal?.symbol}</Text>
            <Text style={styles.dashModalSub}>
              当前: {reduceModal ? Math.abs(parseFloat(reduceModal.positionAmt || '0')) : 0} 个
            </Text>
            <View style={styles.dashPctRow}>
              {['25', '50', '75', '100'].map((pct) => (
                <TouchableOpacity
                  key={pct}
                  style={[styles.dashPctChip, reducePercent === pct && styles.dashPctChipActive]}
                  onPress={() => setReducePercent(pct)}
                >
                  <Text style={[styles.dashPctChipText, reducePercent === pct && styles.dashPctChipTextActive]}>
                    {pct}%
                  </Text>
                </TouchableOpacity>
              ))}
            </View>
            <View style={styles.dashCustomInput}>
              <Text style={styles.dashCustomLabel}>自定义比例 (%)</Text>
              <TextInput
                style={styles.dashInput}
                value={reducePercent}
                onChangeText={setReducePercent}
                keyboardType="decimal-pad"
                placeholder="1-100"
                placeholderTextColor={colors.textMuted}
              />
            </View>
            <View style={styles.dashModalActions}>
              <TouchableOpacity style={styles.dashCancelBtn} onPress={() => setReduceModal(null)}>
                <Text style={styles.dashCancelText}>取消</Text>
              </TouchableOpacity>
              <TouchableOpacity style={styles.dashConfirmBtn} onPress={handleReduce} disabled={actionLoading}>
                <Text style={styles.dashConfirmText}>确认减仓 {reducePercent}%</Text>
              </TouchableOpacity>
            </View>
          </View>
        </View>
      </Modal>
    </>
  );
}

// ==================== 样式 ====================
const styles = StyleSheet.create({
  container: {
    flex: 1,
    backgroundColor: colors.bg,
  },
  safeTop: {
    paddingTop: 10,
    paddingHorizontal: spacing.md,
    paddingBottom: spacing.sm,
    backgroundColor: colors.bg,
    borderBottomWidth: 1,
    borderBottomColor: colors.divider,
  },
  topBar: {
    minHeight: 64,
    borderRadius: radius.md,
    backgroundColor: colors.card,
    borderWidth: 1,
    borderColor: colors.cardBorder,
    flexDirection: 'row',
    alignItems: 'flex-start',
    justifyContent: 'space-between',
    paddingHorizontal: spacing.md,
    paddingVertical: spacing.sm,
  },
  topLeft: {
    flex: 1,
    gap: 2,
  },
  topRight: {
    alignItems: 'flex-end',
    gap: 2,
  },
  topTitle: {
    color: colors.text,
    fontSize: fontSize.md,
    fontWeight: '700',
  },
  topSubtitle: {
    color: colors.textSecondary,
    fontSize: fontSize.xs,
    fontWeight: '500',
  },
  topMetaRow: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: spacing.xs,
  },
  topMetaLabel: {
    color: colors.textMuted,
    fontSize: fontSize.xs,
    fontWeight: '500',
  },
  topMetaValue: {
    color: colors.text,
    fontSize: fontSize.sm,
    fontWeight: '700',
    fontVariant: ['tabular-nums'],
  },
  topPnlText: {
    fontSize: fontSize.xs,
    fontWeight: '700',
    fontVariant: ['tabular-nums'],
  },
  scroll: {
    flex: 1,
  },
  content: {
    paddingHorizontal: spacing.md,
    paddingTop: spacing.sm,
    paddingBottom: 110,
    gap: spacing.sm,
  },
  hidden: {
    position: 'absolute',
    width: 0,
    height: 0,
    overflow: 'hidden',
    opacity: 0,
  },

  // ===== 区域标题 =====
  sectionHeader: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'center',
    backgroundColor: colors.card,
    borderRadius: radius.md,
    borderWidth: 1,
    borderColor: colors.cardBorder,
    paddingVertical: spacing.sm,
    paddingHorizontal: spacing.md,
  },
  sectionTitle: {
    fontSize: fontSize.lg,
    fontWeight: '700',
    color: colors.text,
  },

  // ===== 底部 Tab Bar =====
  tabBarWrap: {
    position: 'absolute',
    left: 0,
    right: 0,
    bottom: 0,
    backgroundColor: colors.bg,
    borderTopWidth: 1,
    borderTopColor: colors.divider,
    paddingBottom: 12,
  },
  tabBar: {
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
    backgroundColor: colors.bg,
    paddingHorizontal: spacing.sm,
    paddingTop: spacing.sm,
  },
  tabItem: {
    flex: 1,
    alignItems: 'center',
    justifyContent: 'center',
    minHeight: 42,
    marginHorizontal: 1,
    borderRadius: radius.sm,
    position: 'relative',
  },
  tabItemActive: {
    backgroundColor: colors.card,
  },
  tabLabel: {
    fontSize: 11,
    color: colors.textMuted,
    fontWeight: '600',
  },
  tabLabelActive: {
    color: colors.text,
    fontWeight: '700',
  },
  tabIndicator: {
    position: 'absolute',
    top: 0,
    width: 28,
    height: 2,
    borderRadius: radius.pill,
    backgroundColor: colors.gold,
  },
  tabBadgeDot: {
    position: 'absolute',
    top: 8,
    right: '30%',
    width: 6,
    height: 6,
    borderRadius: 3,
    backgroundColor: colors.red,
  },

  // ===== Dashboard 我的 =====
  dashCard: {
    backgroundColor: colors.card,
    borderRadius: radius.md,
    borderWidth: 1,
    borderColor: colors.cardBorder,
    padding: spacing.lg,
  },
  dashCardHeader: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'center',
    marginBottom: spacing.sm,
  },
  dashCardTitle: {
    fontSize: fontSize.lg,
    fontWeight: '700',
    color: colors.white,
    marginBottom: spacing.sm,
  },
  dashCardBadge: {
    fontSize: fontSize.sm,
    fontWeight: '700',
    color: colors.gold,
    backgroundColor: colors.goldBg,
    paddingHorizontal: spacing.sm,
    paddingVertical: 2,
    borderRadius: radius.pill,
    overflow: 'hidden',
  },
  dashEquitySection: {
    paddingVertical: spacing.md,
    marginBottom: spacing.md,
    alignItems: 'center',
  },
  dashEquityLabel: {
    fontSize: fontSize.sm,
    color: colors.textMuted,
    marginBottom: spacing.xs,
  },
  dashEquityValue: {
    fontSize: fontSize.hero,
    fontWeight: '800',
    color: colors.white,
    fontVariant: ['tabular-nums'],
  },
  dashStatsRow: {
    flexDirection: 'row',
    alignItems: 'center',
    backgroundColor: colors.surface,
    borderRadius: radius.md,
    padding: spacing.md,
  },
  dashStat: {
    flex: 1,
    alignItems: 'center',
  },
  dashStatDivider: {
    width: 1,
    height: 28,
    backgroundColor: colors.divider,
  },
  dashStatLabel: {
    fontSize: fontSize.xs,
    color: colors.textMuted,
    marginBottom: spacing.xs,
  },
  dashStatValue: {
    fontSize: fontSize.lg,
    fontWeight: '700',
    color: colors.white,
    fontVariant: ['tabular-nums'],
  },
  dashEmpty: {
    color: colors.textMuted,
    textAlign: 'center',
    paddingVertical: spacing.xl,
    fontSize: fontSize.sm,
  },
  dashPosItem: {
    backgroundColor: colors.surface,
    borderRadius: radius.md,
    borderLeftWidth: 3,
    padding: spacing.md,
    marginBottom: spacing.xs,
  },
  dashPosMainRow: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'center',
  },
  dashPosLeft: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: spacing.sm,
  },
  dashPosSymbol: {
    fontSize: fontSize.md,
    fontWeight: '700',
    color: colors.white,
  },
  dashPosSide: {
    paddingHorizontal: spacing.sm,
    paddingVertical: 2,
    borderRadius: radius.sm,
  },
  dashPosLev: {
    fontSize: fontSize.xs,
    color: colors.textMuted,
    fontWeight: '600',
  },
  dashPosPnl: {
    fontSize: fontSize.md,
    fontWeight: '700',
    fontVariant: ['tabular-nums'],
  },
  dashPosHint: {
    marginTop: spacing.xs,
    color: colors.textMuted,
    fontSize: fontSize.xs,
  },
  dashModalOverlay: {
    flex: 1,
    backgroundColor: 'rgba(0,0,0,0.55)',
    justifyContent: 'center',
    alignItems: 'center',
    padding: spacing.xl,
  },
  dashModal: {
    width: '100%',
    maxWidth: 420,
    backgroundColor: colors.card,
    borderRadius: radius.lg,
    borderWidth: 1,
    borderColor: colors.cardBorder,
    padding: spacing.lg,
  },
  dashModalTitle: {
    color: colors.white,
    fontSize: fontSize.lg,
    fontWeight: '700',
  },
  dashModalSub: {
    marginTop: spacing.xs,
    color: colors.textMuted,
    fontSize: fontSize.sm,
  },
  dashPctRow: {
    flexDirection: 'row',
    gap: spacing.sm,
    marginTop: spacing.md,
  },
  dashPctChip: {
    flex: 1,
    borderWidth: 1,
    borderColor: colors.cardBorder,
    backgroundColor: colors.surface,
    borderRadius: radius.md,
    alignItems: 'center',
    paddingVertical: spacing.sm,
  },
  dashPctChipActive: {
    borderColor: colors.gold,
    backgroundColor: colors.goldBg,
  },
  dashPctChipText: {
    color: colors.textMuted,
    fontSize: fontSize.sm,
    fontWeight: '600',
  },
  dashPctChipTextActive: {
    color: colors.goldLight,
    fontWeight: '700',
  },
  dashCustomInput: {
    marginTop: spacing.md,
    gap: spacing.xs,
  },
  dashCustomLabel: {
    color: colors.textSecondary,
    fontSize: fontSize.xs,
  },
  dashInput: {
    borderWidth: 1,
    borderColor: colors.cardBorder,
    backgroundColor: colors.surface,
    color: colors.text,
    borderRadius: radius.md,
    paddingHorizontal: spacing.md,
    paddingVertical: spacing.sm,
    fontSize: fontSize.sm,
  },
  dashModalActions: {
    marginTop: spacing.lg,
    flexDirection: 'row',
    gap: spacing.sm,
  },
  dashCancelBtn: {
    flex: 1,
    borderRadius: radius.md,
    borderWidth: 1,
    borderColor: colors.cardBorder,
    backgroundColor: colors.surface,
    alignItems: 'center',
    paddingVertical: spacing.sm,
  },
  dashCancelText: {
    color: colors.textMuted,
    fontWeight: '600',
    fontSize: fontSize.sm,
  },
  dashConfirmBtn: {
    flex: 1.6,
    borderRadius: radius.md,
    borderWidth: 1,
    borderColor: colors.gold,
    backgroundColor: colors.goldBg,
    alignItems: 'center',
    paddingVertical: spacing.sm,
  },
  dashConfirmText: {
    color: colors.goldLight,
    fontWeight: '700',
    fontSize: fontSize.sm,
  },
  riskRow: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: spacing.lg,
    flexWrap: 'wrap',
  },
  riskItem: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: spacing.xs,
  },
  riskDot: {
    width: 10,
    height: 10,
    borderRadius: 5,
  },
  riskLabel: {
    fontSize: fontSize.sm,
    color: colors.textMuted,
  },
  riskText: {
    fontSize: fontSize.sm,
    fontWeight: '600',
    color: colors.text,
  },
  riskUnlockBtn: {
    paddingHorizontal: spacing.md,
    paddingVertical: spacing.xs,
    borderRadius: radius.sm,
    backgroundColor: colors.orangeBg,
    borderWidth: 1,
    borderColor: colors.orange,
  },
  riskUnlockText: {
    fontSize: fontSize.sm,
    fontWeight: '700',
    color: colors.orange,
  },
  riskReason: {
    width: '100%',
    fontSize: fontSize.xs,
    color: colors.textMuted,
  },
  riskConditions: {
    width: '100%',
    gap: 2,
  },
  riskConditionText: {
    fontSize: fontSize.xs,
    color: colors.textSecondary,
  },
});
