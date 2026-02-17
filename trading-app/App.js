import React, { useCallback, useEffect, useState } from 'react';
import { ScrollView, Text, StyleSheet, View, TouchableOpacity, TextInput, Alert } from 'react-native';
import { StatusBar } from 'expo-status-bar';

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
import { colors } from './src/services/theme';

const SYMBOL = 'ETHUSDT';
const DEFAULT_WATCH_ADDRESS = '0x15a4f009bb324a3fb9e36137136b201e3fe0dfdb';

const TABS = [
  { key: 'trade', label: '交易' },
  { key: 'book', label: '订单簿' },
  { key: 'strategy', label: '策略' },
  { key: 'news', label: '新闻' },
  { key: 'watch', label: '监控' },
  { key: 'log', label: '日志' },
];

export default function App() {
  const [activeTab, setActiveTab] = useState('trade');
  const [strategySymbol, setStrategySymbol] = useState(SYMBOL);
  const [newsHasNew, setNewsHasNew] = useState(false);
  const [watchHasNew, setWatchHasNew] = useState(false);
  const [watchAddress, setWatchAddress] = useState(DEFAULT_WATCH_ADDRESS);
  const [watchAddressInput, setWatchAddressInput] = useState(DEFAULT_WATCH_ADDRESS);

  useEffect(() => {
    if (activeTab === 'news') setNewsHasNew(false);
    if (activeTab === 'watch') setWatchHasNew(false);
  }, [activeTab]);

  const handleNewsHasNew = useCallback((hasNew) => {
    if (hasNew && activeTab !== 'news') {
      setNewsHasNew(true);
    }
  }, [activeTab]);

  const handleWatchHasNew = useCallback((hasNew) => {
    if (hasNew && activeTab !== 'watch') {
      setWatchHasNew(true);
    }
  }, [activeTab]);

  const applyWatchAddress = useCallback(() => {
    const next = watchAddressInput.trim();
    if (!/^0x[a-fA-F0-9]{40}$/.test(next)) {
      Alert.alert('地址格式错误', '请输入正确的 0x 开头地址');
      return;
    }
    setWatchAddress(next);
    Alert.alert('已更新', `监控地址已切换为\n${next}`);
  }, [watchAddressInput]);

  return (
    <View style={styles.container}>
      <StatusBar style="light" />

      {/* 顶部标题 + Tab 切换 */}
      <View style={styles.header}>
        <Text style={styles.title}>合约交易</Text>
        <View style={styles.tabRow}>
          {TABS.map((tab) => (
            <TouchableOpacity
              key={tab.key}
              style={[styles.tab, activeTab === tab.key && styles.tabActive]}
              onPress={() => setActiveTab(tab.key)}
            >
              <View style={styles.tabLabelRow}>
                <Text style={[styles.tabText, activeTab === tab.key && styles.tabTextActive]}>
                  {tab.label}
                </Text>
                {(tab.key === 'news' && newsHasNew) || (tab.key === 'watch' && watchHasNew) ? (
                  <View style={styles.badgeDot} />
                ) : null}
              </View>
            </TouchableOpacity>
          ))}
        </View>
      </View>

      {/* 内容区域 */}
      <ScrollView
        style={styles.scroll}
        contentContainerStyle={styles.content}
        keyboardShouldPersistTaps="handled"
      >
        {activeTab === 'trade' && (
          <>
            <SymbolPicker symbol={SYMBOL} />
            <OrderPanel symbol={SYMBOL} />
            <PositionPanel symbol={SYMBOL} />
          </>
        )}

        {activeTab === 'book' && (
          <>
            <SymbolPicker symbol={SYMBOL} />
            <OrderBookPanel symbol={SYMBOL} />
          </>
        )}

        {activeTab === 'strategy' && (
          <>
            <SymbolPicker symbol={strategySymbol} onChangeSymbol={setStrategySymbol} />
            <SignalPanel symbol={strategySymbol} />
            <DojiPanel symbol={strategySymbol} />
            <AutoScalePanel symbol={strategySymbol} />
            <GridPanel symbol={strategySymbol} />
            <DCAPanel symbol={strategySymbol} />
          </>
        )}

        <View style={activeTab === 'news' ? styles.panelVisible : styles.panelHidden}>
          <NewsPanel onHasNew={handleNewsHasNew} />
        </View>

        <View style={activeTab === 'watch' ? styles.panelVisible : styles.panelHidden}>
          <View style={styles.watchAddrCard}>
            <Text style={styles.watchAddrTitle}>监控地址</Text>
            <View style={styles.watchAddrRow}>
              <TextInput
                style={styles.watchAddrInput}
                value={watchAddressInput}
                onChangeText={setWatchAddressInput}
                placeholder="输入 0x 地址"
                placeholderTextColor={colors.textMuted}
                autoCapitalize="none"
                autoCorrect={false}
              />
              <TouchableOpacity style={styles.watchAddrBtn} onPress={applyWatchAddress}>
                <Text style={styles.watchAddrBtnText}>应用</Text>
              </TouchableOpacity>
            </View>
            <Text style={styles.watchAddrCurrent}>当前: {watchAddress}</Text>
          </View>
          <HyperMonitorPanel
            address={watchAddress}
            onHasNew={handleWatchHasNew}
          />
        </View>

        {activeTab === 'log' && (
          <TradeLogPanel symbol={SYMBOL} />
        )}

        <View style={{ height: 50 }} />
      </ScrollView>
    </View>
  );
}

const styles = StyleSheet.create({
  container: {
    flex: 1,
    backgroundColor: colors.bg,
  },
  header: {
    paddingHorizontal: 16,
    paddingTop: 56,
    paddingBottom: 8,
    backgroundColor: colors.bg,
  },
  title: {
    fontSize: 26,
    fontWeight: 'bold',
    color: colors.white,
    marginBottom: 12,
  },
  tabRow: {
    flexDirection: 'row',
    gap: 4,
    backgroundColor: colors.surface,
    borderRadius: 8,
    padding: 3,
  },
  tab: {
    flex: 1,
    paddingVertical: 8,
    alignItems: 'center',
    borderRadius: 6,
  },
  tabActive: {
    backgroundColor: colors.blue,
  },
  tabText: {
    fontSize: 14,
    color: colors.textSecondary,
    fontWeight: '500',
  },
  tabTextActive: {
    color: colors.white,
    fontWeight: '700',
  },
  tabLabelRow: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 4,
  },
  badgeDot: {
    width: 7,
    height: 7,
    borderRadius: 999,
    backgroundColor: '#ff4d4f',
  },
  panelVisible: {
    display: 'flex',
  },
  panelHidden: {
    display: 'none',
  },
  watchAddrCard: {
    backgroundColor: colors.card,
    borderRadius: 12,
    borderWidth: 1,
    borderColor: colors.cardBorder,
    padding: 12,
    marginBottom: 10,
  },
  watchAddrTitle: {
    color: colors.white,
    fontSize: 14,
    fontWeight: '700',
    marginBottom: 8,
  },
  watchAddrRow: {
    flexDirection: 'row',
    gap: 8,
    alignItems: 'center',
  },
  watchAddrInput: {
    flex: 1,
    backgroundColor: colors.surface,
    borderWidth: 1,
    borderColor: colors.cardBorder,
    color: colors.text,
    borderRadius: 8,
    paddingHorizontal: 10,
    paddingVertical: 9,
    fontSize: 12,
  },
  watchAddrBtn: {
    paddingHorizontal: 14,
    paddingVertical: 9,
    borderRadius: 8,
    backgroundColor: colors.blueBg,
    borderWidth: 1,
    borderColor: colors.blue,
  },
  watchAddrBtnText: {
    color: colors.white,
    fontWeight: '700',
    fontSize: 12,
  },
  watchAddrCurrent: {
    color: colors.textSecondary,
    fontSize: 11,
    marginTop: 8,
  },
  scroll: {
    flex: 1,
  },
  content: {
    padding: 16,
    paddingTop: 8,
  },
});
