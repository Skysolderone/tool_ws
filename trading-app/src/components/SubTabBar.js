import React from 'react';
import { View, Text, TouchableOpacity, StyleSheet } from 'react-native';
import { colors, spacing, radius, fontSize } from '../services/theme';

/**
 * 通用子 Tab 切换条
 * @param {Array<{key:string, label:string}>} tabs
 * @param {string} activeKey
 * @param {function} onChangeTab
 * @param {object} [style]       容器自定义样式
 * @param {boolean} [badge]      key→boolean map, 显示红点
 */
export default function SubTabBar({ tabs, activeKey, onChangeTab, style, badge = {} }) {
  return (
    <View style={[styles.wrap, style]}>
      {tabs.map((tab) => {
        const isActive = tab.key === activeKey;
        return (
          <TouchableOpacity
            key={tab.key}
            style={[styles.tab, isActive && styles.tabActive]}
            onPress={() => onChangeTab(tab.key)}
            activeOpacity={0.7}
          >
            <Text style={[styles.label, isActive && styles.labelActive]}>
              {tab.label}
            </Text>
            {isActive && <View style={styles.indicator} />}
            {badge[tab.key] && <View style={styles.dot} />}
          </TouchableOpacity>
        );
      })}
    </View>
  );
}

const styles = StyleSheet.create({
  wrap: {
    flexDirection: 'row',
    backgroundColor: colors.cardAlt,
    borderRadius: radius.md,
    borderWidth: 1,
    borderColor: colors.cardBorder,
    padding: 3,
    gap: 3,
  },
  tab: {
    flex: 1,
    alignItems: 'center',
    justifyContent: 'center',
    minHeight: 34,
    borderRadius: radius.sm,
    position: 'relative',
  },
  tabActive: {
    backgroundColor: colors.surface,
  },
  label: {
    fontSize: fontSize.sm,
    fontWeight: '600',
    color: colors.textMuted,
  },
  labelActive: {
    color: colors.text,
    fontWeight: '700',
  },
  indicator: {
    position: 'absolute',
    top: 0,
    width: 16,
    height: 2,
    borderRadius: 1,
    backgroundColor: colors.gold,
  },
  dot: {
    position: 'absolute',
    top: 6,
    right: 10,
    width: 7,
    height: 7,
    borderRadius: 3.5,
    backgroundColor: colors.red,
  },
});
