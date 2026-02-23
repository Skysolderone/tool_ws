import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import {
  ScrollView, Text, StyleSheet, View, TouchableOpacity,
  TextInput, Alert,
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
import HyperMonitorPanel from './src/components/HyperMonitorPanel';
import LiquidationMonitorPanel from './src/components/LiquidationMonitorPanel';
import AnalyticsPanel from './src/components/AnalyticsPanel';
import WhaleAggregatePanel from './src/components/WhaleAggregatePanel';
import MarketMonitorPanel from './src/components/MarketMonitorPanel';
import { colors, spacing, radius, fontSize } from './src/services/theme';
import api, { WS_PRICE_BASE, AUTH_TOKEN } from './src/services/api';

const DEFAULT_SYMBOL = 'ETHUSDT';
const DEFAULT_WATCH_ADDRESSES = [
  { address: '0x15a4f009bb324a3fb9e36137136b201e3fe0dfdb', label: '主监控' },
];

// ==================== 底部主 Tab ====================
const MAIN_TABS = [
  { key: 'trade', label: '交易', icon: '⇅' },
  { key: 'strategy', label: '策略', icon: '⚙' },
  { key: 'monitor', label: '监控', icon: '◉' },
  { key: 'info', label: '资讯', icon: '◎' },
  { key: 'me', label: '我的', icon: '◈' },
];

// 交易 Tab 子页签
const TRADE_SUB_TABS = [
  { key: 'order', label: '下单' },
  { key: 'position', label: '持仓' },
  { key: 'book', label: '盘口' },
  { key: 'log', label: '日志' },
];

// 监控 Tab 子页签
const MONITOR_SUB_TABS = [
  { key: 'hyper', label: 'Hyper监控' },
  { key: 'liquidation', label: '强平监控' },
  { key: 'market', label: '市场监控' },
];

export default function App() {
  // ===== 主Tab状态 =====
  const [activeTab, setActiveTab] = useState('trade');
  const [tradeSubTab, setTradeSubTab] = useState('order');
  const [monitorSubTab, setMonitorSubTab] = useState('hyper');

  // ===== 全局交易币对 =====
  const [tradeSymbol, setTradeSymbol] = useState(DEFAULT_SYMBOL);
  const [strategySymbol, setStrategySymbol] = useState(DEFAULT_SYMBOL);

  // ===== 新闻/监控 =====
  const [newsHasNew, setNewsHasNew] = useState(false);
  const [hyperHasNew, setHyperHasNew] = useState(false);
  const [liqHasNew, setLiqHasNew] = useState(false);
  const [marketHasNew, setMarketHasNew] = useState(false);
  const [watchAddresses, setWatchAddresses] = useState(DEFAULT_WATCH_ADDRESSES);
  const [activeAddrIdx, setActiveAddrIdx] = useState(0);
  const [newAddrInput, setNewAddrInput] = useState('');
  const [newAddrLabel, setNewAddrLabel] = useState('');

  // ===== 懒加载标记 =====
  const [newsActivated, setNewsActivated] = useState(false);
  const [hyperActivated, setHyperActivated] = useState(false);
  const [liqActivated, setLiqActivated] = useState(false);
  const [marketActivated, setMarketActivated] = useState(false);

  // ===== 实时价格（交易Tab顶栏共享） =====
  const [markPrice, setMarkPrice] = useState(null);
  const lastUpdateRef = useRef(0);
  const pendingPriceRef = useRef(null);
  const rafRef = useRef(null);

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
    if (activeTab === 'monitor' && monitorSubTab === 'hyper') setHyperHasNew(false);
    if (activeTab === 'monitor' && monitorSubTab === 'liquidation') setLiqHasNew(false);
    if (activeTab === 'monitor' && monitorSubTab === 'market') setMarketHasNew(false);
  }, [activeTab, monitorSubTab]);

  const handleNewsHasNew = useCallback((hasNew) => {
    if (hasNew && activeTab !== 'info') setNewsHasNew(true);
  }, [activeTab]);

  const handleHyperHasNew = useCallback((hasNew) => {
    if (hasNew && !(activeTab === 'monitor' && monitorSubTab === 'hyper')) setHyperHasNew(true);
  }, [activeTab, monitorSubTab]);

  const handleLiqHasNew = useCallback((hasNew) => {
    if (hasNew && !(activeTab === 'monitor' && monitorSubTab === 'liquidation')) setLiqHasNew(true);
  }, [activeTab, monitorSubTab]);

  const handleMarketHasNew = useCallback((hasNew) => {
    if (hasNew && !(activeTab === 'monitor' && monitorSubTab === 'market')) setMarketHasNew(true);
  }, [activeTab, monitorSubTab]);

  const switchMainTab = useCallback((key) => {
    setActiveTab(key);
    if (key === 'info') {
      setNewsActivated(true);
    }
    if (key === 'monitor') {
      if (monitorSubTab === 'hyper') setHyperActivated(true);
      else if (monitorSubTab === 'liquidation') setLiqActivated(true);
      else setMarketActivated(true);
    }
  }, [monitorSubTab]);

  const switchMonitorSub = useCallback((key) => {
    setMonitorSubTab(key);
    if (key === 'hyper') {
      setHyperActivated(true);
      setHyperHasNew(false);
    }
    if (key === 'liquidation') {
      setLiqActivated(true);
      setLiqHasNew(false);
    }
    if (key === 'market') {
      setMarketActivated(true);
      setMarketHasNew(false);
    }
  }, []);

  const addWatchAddress = useCallback(() => {
    if (watchAddresses.length >= 3) {
      Alert.alert('数量限制', '最多同时监控 3 个地址');
      return;
    }
    const addr = newAddrInput.trim();
    if (!/^0x[a-fA-F0-9]{40}$/.test(addr)) {
      Alert.alert('地址格式错误', '请输入正确的 0x 开头地址');
      return;
    }
    if (watchAddresses.some((w) => w.address.toLowerCase() === addr.toLowerCase())) {
      Alert.alert('重复地址', '该地址已在监控列表中');
      return;
    }
    const label = newAddrLabel.trim() || `监控${watchAddresses.length + 1}`;
    setWatchAddresses((prev) => [...prev, { address: addr, label }]);
    setActiveAddrIdx(watchAddresses.length);
    setNewAddrInput('');
    setNewAddrLabel('');
  }, [newAddrInput, newAddrLabel, watchAddresses]);

  const removeWatchAddress = useCallback((idx) => {
    Alert.alert('删除地址', `确定移除 "${watchAddresses[idx].label}" ?`, [
      { text: '取消', style: 'cancel' },
      {
        text: '删除', style: 'destructive', onPress: () => {
          setWatchAddresses((prev) => prev.filter((_, i) => i !== idx));
          setActiveAddrIdx((prev) => {
            if (prev >= idx && prev > 0) return prev - 1;
            return Math.min(prev, watchAddresses.length - 2);
          });
        },
      },
    ]);
  }, [watchAddresses]);

  // ===== Tab 红点/懒加载 =====
  const monitorBadge = hyperHasNew || liqHasNew || marketHasNew;
  const infoBadge = newsHasNew;
  const newsPanelMounted = newsActivated || activeTab === 'info';
  const hyperPanelMounted = hyperActivated || (activeTab === 'monitor' && monitorSubTab === 'hyper');
  const liqPanelMounted = liqActivated || (activeTab === 'monitor' && monitorSubTab === 'liquidation');
  const marketPanelMounted = marketActivated || (activeTab === 'monitor' && monitorSubTab === 'market');

  // ======================== 渲染 ========================
  return (
    <View style={styles.container}>
      <StatusBar style="light" />

      {/* 顶部安全区 */}
      <View style={styles.safeTop} />

      {/* 内容区 */}
      <ScrollView
        style={styles.scroll}
        contentContainerStyle={styles.content}
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
            />
            <SubTabBar
              tabs={TRADE_SUB_TABS}
              activeKey={tradeSubTab}
              onChangeTab={setTradeSubTab}
              style={{ marginTop: spacing.sm }}
            />
            {tradeSubTab === 'order' && (
              <OrderPanel symbol={tradeSymbol} externalMarkPrice={markPrice} />
            )}
            {tradeSubTab === 'position' && (
              <PositionPanel symbol={tradeSymbol} />
            )}
            {tradeSubTab === 'book' && (
              <OrderBookPanel symbol={tradeSymbol} />
            )}
            {tradeSubTab === 'log' && (
              <TradeLogPanel symbol={tradeSymbol} />
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
            <SignalPanel symbol={strategySymbol} />
            <DojiPanel symbol={strategySymbol} />
            <AutoScalePanel symbol={strategySymbol} />
            <GridPanel symbol={strategySymbol} />
            <DCAPanel symbol={strategySymbol} />
          </>
        )}

        {/* ==================== 监控 Tab ==================== */}
        {activeTab === 'monitor' && (
          <>
            <SubTabBar
              tabs={MONITOR_SUB_TABS}
              activeKey={monitorSubTab}
              onChangeTab={switchMonitorSub}
              badge={{ hyper: hyperHasNew, liquidation: liqHasNew, market: marketHasNew }}
            />
            {monitorSubTab === 'hyper' && hyperPanelMounted && (
              <>
                <WhaleAggregatePanel
                  watchAddresses={watchAddresses}
                  onHasNew={handleHyperHasNew}
                />
                {/* 地址 Chip 列表 */}
                <View style={styles.hyperAddrCard}>
                  <ScrollView horizontal showsHorizontalScrollIndicator={false} style={styles.addrChipScroll}>
                    <View style={styles.addrChipRow}>
                      {watchAddresses.map((item, idx) => (
                        <TouchableOpacity
                          key={item.address}
                          style={[styles.addrChip, idx === activeAddrIdx && styles.addrChipActive]}
                          onPress={() => setActiveAddrIdx(idx)}
                          onLongPress={() => removeWatchAddress(idx)}
                        >
                          <Text style={[styles.addrChipText, idx === activeAddrIdx && styles.addrChipTextActive]} numberOfLines={1}>
                            {item.label}
                          </Text>
                        </TouchableOpacity>
                      ))}
                    </View>
                  </ScrollView>
                  <Text style={styles.addrHint}>
                    {watchAddresses[activeAddrIdx]?.address ? `${watchAddresses[activeAddrIdx].address.slice(0, 10)}...${watchAddresses[activeAddrIdx].address.slice(-6)}` : '暂无监控地址，请添加'}
                    {'  '}| 长按标签删除 | 最多 3 个
                  </Text>
                  {/* 添加新地址 */}
                  <View style={styles.hyperAddrRow}>
                    <TextInput
                      style={[styles.hyperAddrInput, { flex: 2 }]}
                      value={newAddrInput}
                      onChangeText={setNewAddrInput}
                      placeholder="新地址 0x..."
                      placeholderTextColor={colors.textMuted}
                      autoCapitalize="none"
                      autoCorrect={false}
                    />
                    <TextInput
                      style={[styles.hyperAddrInput, { flex: 1 }]}
                      value={newAddrLabel}
                      onChangeText={setNewAddrLabel}
                      placeholder="备注名"
                      placeholderTextColor={colors.textMuted}
                    />
                    <TouchableOpacity style={styles.hyperAddrBtn} onPress={addWatchAddress}>
                      <Text style={styles.hyperAddrBtnText}>添加</Text>
                    </TouchableOpacity>
                  </View>
                </View>
                {/* 为每个地址渲染面板，仅显示当前选中的 */}
                {watchAddresses.map((item, idx) => (
                  <View key={item.address} style={idx !== activeAddrIdx ? styles.hidden : undefined}>
                    <HyperMonitorPanel
                      address={item.address}
                      onHasNew={handleHyperHasNew}
                      withLiquidationTab={false}
                    />
                  </View>
                ))}
              </>
            )}
            {monitorSubTab === 'liquidation' && liqPanelMounted && (
              <LiquidationMonitorPanel onHasNew={handleLiqHasNew} />
            )}
            {monitorSubTab === 'market' && marketPanelMounted && (
              <MarketMonitorPanel onHasNew={handleMarketHasNew} />
            )}
          </>
        )}

        {/* ==================== 资讯 Tab ==================== */}
        {activeTab === 'info' && (
          <>
            {newsPanelMounted && (
              <NewsPanel onHasNew={handleNewsHasNew} />
            )}
          </>
        )}

        {/* ==================== 我的 Tab ==================== */}
        {activeTab === 'me' && (
          <DashboardContent tradeSymbol={tradeSymbol} />
        )}
      </ScrollView>

      {/* 懒挂载的后台面板（保持 WS 连接） */}
      {newsPanelMounted && activeTab !== 'info' && (
        <View style={styles.hidden}><NewsPanel onHasNew={handleNewsHasNew} /></View>
      )}
      {hyperPanelMounted && !(activeTab === 'monitor' && monitorSubTab === 'hyper') && (
        <View style={styles.hidden}>
          {watchAddresses.map((item) => (
            <HyperMonitorPanel key={item.address} address={item.address} onHasNew={handleHyperHasNew} withLiquidationTab={false} />
          ))}
        </View>
      )}
      {liqPanelMounted && !(activeTab === 'monitor' && monitorSubTab === 'liquidation') && (
        <View style={styles.hidden}>
          <LiquidationMonitorPanel onHasNew={handleLiqHasNew} />
        </View>
      )}
      {marketPanelMounted && !(activeTab === 'monitor' && monitorSubTab === 'market') && (
        <View style={styles.hidden}>
          <MarketMonitorPanel onHasNew={handleMarketHasNew} />
        </View>
      )}

      {/* ==================== 底部 Tab Bar ==================== */}
      <View style={styles.tabBarWrap}>
        <View style={styles.tabBar}>
          {MAIN_TABS.map((tab) => {
            const isActive = activeTab === tab.key;
            const showBadge = (tab.key === 'info' && infoBadge) || (tab.key === 'monitor' && monitorBadge);
            return (
              <TouchableOpacity
                key={tab.key}
                style={styles.tabItem}
                onPress={() => switchMainTab(tab.key)}
                activeOpacity={0.7}
              >
                <Text style={[styles.tabIcon, isActive && styles.tabIconActive]}>
                  {tab.icon}
                </Text>
                <Text style={[styles.tabLabel, isActive && styles.tabLabelActive]}>
                  {tab.label}
                </Text>
                {isActive && <View style={styles.tabIndicator} />}
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

  const handleUnlock = useCallback(async () => {
    try {
      await api.unlockRisk();
      Alert.alert('成功', '风控已解锁');
      fetchAll();
    } catch (e) {
      Alert.alert('失败', e.message);
    }
  }, [fetchAll]);

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
              <View key={pos.symbol + pos.positionSide} style={[styles.dashPosItem, { borderLeftColor: isLong ? colors.green : colors.red }]}>
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
              <View style={[styles.riskDot, { backgroundColor: riskStatus.locked ? colors.red : colors.green }]} />
              <Text style={styles.riskText}>
                {riskStatus.locked ? '已锁定' : '正常'}
              </Text>
            </View>
            <View style={styles.riskItem}>
              <Text style={styles.riskLabel}>今日亏损</Text>
              <Text style={styles.riskText}>
                {riskStatus.dailyLossCount || 0}/{riskStatus.dailyMaxLosses || 3}
              </Text>
            </View>
            {riskStatus.locked && (
              <TouchableOpacity style={styles.riskUnlockBtn} onPress={handleUnlock}>
                <Text style={styles.riskUnlockText}>解锁</Text>
              </TouchableOpacity>
            )}
          </View>
        ) : (
          <Text style={styles.dashEmpty}>加载中...</Text>
        )}
      </View>

      {/* 数据分析 */}
      <AnalyticsPanel sentimentSymbol={tradeSymbol || 'BTCUSDT'} />
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
    height: 54,
    backgroundColor: colors.bg,
  },
  scroll: {
    flex: 1,
  },
  content: {
    paddingHorizontal: spacing.md,
    paddingTop: spacing.xs,
    paddingBottom: 100,
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
    paddingVertical: spacing.sm,
  },
  sectionTitle: {
    fontSize: fontSize.xl,
    fontWeight: '800',
    color: colors.white,
  },

  // ===== Hyper地址输入 =====
  hyperAddrCard: {
    backgroundColor: colors.card,
    borderRadius: radius.lg,
    borderWidth: 1,
    borderColor: colors.cardBorder,
    padding: spacing.md,
    marginTop: spacing.sm,
  },
  addrChipScroll: {
    marginBottom: spacing.sm,
  },
  addrChipRow: {
    flexDirection: 'row',
    gap: spacing.sm,
  },
  addrChip: {
    paddingHorizontal: spacing.md,
    paddingVertical: spacing.sm,
    borderRadius: radius.pill,
    backgroundColor: colors.surface,
    borderWidth: 1,
    borderColor: colors.cardBorder,
  },
  addrChipActive: {
    backgroundColor: colors.goldBg,
    borderColor: colors.gold,
  },
  addrChipText: {
    fontSize: fontSize.sm,
    fontWeight: '600',
    color: colors.textMuted,
    maxWidth: 100,
  },
  addrChipTextActive: {
    color: colors.gold,
    fontWeight: '700',
  },
  addrHint: {
    fontSize: fontSize.xs,
    color: colors.textMuted,
    marginBottom: spacing.sm,
    fontFamily: 'monospace',
  },
  hyperAddrRow: {
    flexDirection: 'row',
    gap: spacing.sm,
    alignItems: 'center',
  },
  hyperAddrInput: {
    flex: 1,
    backgroundColor: colors.surface,
    borderWidth: 1,
    borderColor: colors.cardBorder,
    color: colors.text,
    borderRadius: radius.md,
    paddingHorizontal: spacing.md,
    paddingVertical: spacing.sm,
    fontSize: fontSize.sm,
  },
  hyperAddrBtn: {
    paddingHorizontal: spacing.lg,
    paddingVertical: spacing.sm,
    borderRadius: radius.md,
    backgroundColor: colors.gold,
  },
  hyperAddrBtnText: {
    color: colors.white,
    fontWeight: '700',
    fontSize: fontSize.sm,
  },

  // ===== 底部 Tab Bar =====
  tabBarWrap: {
    position: 'absolute',
    left: 0,
    right: 0,
    bottom: 0,
    backgroundColor: 'rgba(18, 14, 10, 0.95)',
    borderTopWidth: 1,
    borderTopColor: colors.divider,
    paddingBottom: 20, // 安全区
  },
  tabBar: {
    flexDirection: 'row',
    paddingTop: spacing.md,
    paddingHorizontal: spacing.lg,
  },
  tabItem: {
    flex: 1,
    alignItems: 'center',
    paddingVertical: spacing.xs,
    position: 'relative',
  },
  tabIcon: {
    fontSize: 22,
    color: colors.textMuted,
    marginBottom: 2,
  },
  tabIconActive: {
    color: colors.gold,
    textShadowColor: colors.goldGlow,
    textShadowOffset: { width: 0, height: 0 },
    textShadowRadius: 8,
  },
  tabLabel: {
    fontSize: fontSize.xs,
    color: colors.textMuted,
    fontWeight: '600',
  },
  tabLabelActive: {
    color: colors.gold,
    fontWeight: '700',
  },
  tabIndicator: {
    position: 'absolute',
    top: -2,
    width: 28,
    height: 2.5,
    borderRadius: 1.25,
    backgroundColor: colors.gold,
  },
  tabBadgeDot: {
    position: 'absolute',
    top: 0,
    right: '25%',
    width: 6,
    height: 6,
    borderRadius: 3,
    backgroundColor: colors.red,
  },

  // ===== Dashboard 我的 =====
  dashCard: {
    backgroundColor: colors.card,
    borderRadius: radius.lg,
    borderWidth: 1,
    borderColor: colors.cardBorder,
    padding: spacing.lg,
    borderTopWidth: 2,
    borderTopColor: colors.gold,
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
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'center',
    backgroundColor: colors.surface,
    borderRadius: radius.md,
    borderLeftWidth: 3,
    padding: spacing.md,
    marginBottom: spacing.xs,
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
});
