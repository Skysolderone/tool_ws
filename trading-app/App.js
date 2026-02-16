import React, { useState } from 'react';
import { ScrollView, Text, StyleSheet, View, TouchableOpacity } from 'react-native';
import { StatusBar } from 'expo-status-bar';

import SymbolPicker from './src/components/SymbolPicker';
import OrderPanel from './src/components/OrderPanel';
import PositionPanel from './src/components/PositionPanel';
import AutoScalePanel from './src/components/AutoScalePanel';
import GridPanel from './src/components/GridPanel';
import DCAPanel from './src/components/DCAPanel';
import SignalPanel from './src/components/SignalPanel';
import DojiPanel from './src/components/DojiPanel';
import TradeLogPanel from './src/components/TradeLogPanel';
import { colors } from './src/services/theme';

const SYMBOL = 'ETHUSDT';

const TABS = [
  { key: 'trade', label: '交易' },
  { key: 'strategy', label: '策略' },
  { key: 'log', label: '日志' },
];

export default function App() {
  const [activeTab, setActiveTab] = useState('trade');

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
              <Text style={[styles.tabText, activeTab === tab.key && styles.tabTextActive]}>
                {tab.label}
              </Text>
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
            <SymbolPicker />
            <OrderPanel symbol={SYMBOL} />
            <PositionPanel symbol={SYMBOL} />
          </>
        )}

        {activeTab === 'strategy' && (
          <>
            <SignalPanel symbol={SYMBOL} />
            <DojiPanel symbol={SYMBOL} />
            <AutoScalePanel symbol={SYMBOL} />
            <GridPanel symbol={SYMBOL} />
            <DCAPanel symbol={SYMBOL} />
          </>
        )}

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
  scroll: {
    flex: 1,
  },
  content: {
    padding: 16,
    paddingTop: 8,
  },
});
