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
    padding: 3,
    gap: 2,
  },
  tab: {
    flex: 1,
    alignItems: 'center',
    paddingVertical: spacing.sm,
    borderRadius: radius.sm,
    position: 'relative',
    borderWidth: 1,
    borderColor: 'transparent',
  },
  tabActive: {
    backgroundColor: colors.goldBg,
    borderColor: 'rgba(212,165,74,0.3)',
  },
  label: {
    fontSize: fontSize.md,
    fontWeight: '600',
    color: colors.textMuted,
  },
  labelActive: {
    color: colors.white,
    fontWeight: '700',
  },
  indicator: {
    position: 'absolute',
    bottom: 2,
    width: 16,
    height: 2,
    borderRadius: 1,
    backgroundColor: colors.gold,
    display: 'none',
  },
  dot: {
    position: 'absolute',
    top: 4,
    right: 8,
    width: 7,
    height: 7,
    borderRadius: 3.5,
    backgroundColor: colors.red,
  },
});
