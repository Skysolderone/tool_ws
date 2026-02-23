import React, { useState } from 'react';
import {
  View, Text, TextInput, TouchableOpacity, StyleSheet, Modal, FlatList,
} from 'react-native';
import { colors, spacing, radius, fontSize } from '../services/theme';

// 常用合约交易对
const POPULAR_SYMBOLS = [
  'BTCUSDT', 'ETHUSDT', 'BNBUSDT', 'SOLUSDT', 'XRPUSDT',
  'DOGEUSDT', 'ADAUSDT', 'AVAXUSDT', 'DOTUSDT', 'LINKUSDT',
  'MATICUSDT', 'ARBUSDT', 'OPUSDT', 'APTUSDT', 'SUIUSDT',
  'LTCUSDT', 'ETCUSDT', 'FILUSDT', 'ATOMUSDT', 'NEARUSDT',
  'PEPEUSDT', 'WIFUSDT', 'BONKUSDT', 'FLOKIUSDT', 'SHIBUSDT',
  'TRXUSDT', 'UNIUSDT', 'AAVEUSDT', 'MKRUSDT', 'INJUSDT',
];

export default function SymbolPicker({ symbol, onChangeSymbol }) {
  const [showModal, setShowModal] = useState(false);
  const [search, setSearch] = useState('');

  const canChange = typeof onChangeSymbol === 'function';

  const filtered = search.trim()
    ? POPULAR_SYMBOLS.filter((s) => s.includes(search.toUpperCase()))
    : POPULAR_SYMBOLS;

  const handleSelect = (sym) => {
    if (canChange) onChangeSymbol(sym);
    setShowModal(false);
    setSearch('');
  };

  return (
    <View style={styles.container}>
      <TouchableOpacity
        style={[styles.chip, !canChange && styles.chipStatic]}
        onPress={() => canChange && setShowModal(true)}
        activeOpacity={canChange ? 0.7 : 1}
      >
        <Text style={styles.chipText}>{symbol}</Text>
        {canChange && <Text style={styles.arrow}> ▼</Text>}
      </TouchableOpacity>

      <Modal visible={showModal} transparent animationType="fade">
        <TouchableOpacity
          style={styles.overlay}
          activeOpacity={1}
          onPress={() => setShowModal(false)}
        >
          <View style={styles.modal} onStartShouldSetResponder={() => true}>
            <Text style={styles.modalTitle}>选择交易对</Text>

            <TextInput
              style={styles.searchInput}
              placeholder="搜索交易对..."
              placeholderTextColor={colors.textMuted}
              value={search}
              onChangeText={setSearch}
              autoCapitalize="characters"
              autoCorrect={false}
            />

            <FlatList
              data={filtered}
              keyExtractor={(item) => item}
              numColumns={3}
              columnWrapperStyle={styles.gridRow}
              style={styles.list}
              renderItem={({ item }) => (
                <TouchableOpacity
                  style={[styles.symbolBtn, item === symbol && styles.symbolBtnActive]}
                  onPress={() => handleSelect(item)}
                >
                  <Text style={[
                    styles.symbolText,
                    item === symbol && styles.symbolTextActive,
                  ]}>
                    {item.replace('USDT', '')}
                  </Text>
                </TouchableOpacity>
              )}
              ListEmptyComponent={
                <Text style={styles.emptyText}>没有匹配的交易对</Text>
              }
            />

            {/* 自定义输入 */}
            <View style={styles.customRow}>
              <TextInput
                style={styles.customInput}
                placeholder="自定义 如 1000PEPEUSDT"
                placeholderTextColor={colors.textMuted}
                value={search}
                onChangeText={setSearch}
                autoCapitalize="characters"
                autoCorrect={false}
              />
              <TouchableOpacity
                style={styles.customBtn}
                onPress={() => {
                  const v = search.trim().toUpperCase();
                  if (v && v.endsWith('USDT')) {
                    handleSelect(v);
                  }
                }}
              >
                <Text style={styles.customBtnText}>确定</Text>
              </TouchableOpacity>
            </View>
          </View>
        </TouchableOpacity>
      </Modal>
    </View>
  );
}

const styles = StyleSheet.create({
  container: { marginBottom: 0 },
  chip: {
    alignSelf: 'flex-start',
    flexDirection: 'row',
    alignItems: 'center',
    paddingHorizontal: spacing.lg,
    paddingVertical: spacing.sm,
    borderRadius: radius.pill,
    backgroundColor: colors.goldBg,
    borderWidth: 1,
    borderColor: 'rgba(212,165,74,0.3)',
  },
  chipStatic: {
    backgroundColor: colors.surface,
    borderWidth: 1,
    borderColor: colors.cardBorder,
  },
  chipText: { color: colors.white, fontSize: fontSize.lg, fontWeight: '800' },
  arrow: { color: colors.goldLight, fontSize: fontSize.xs, marginLeft: spacing.xs },

  overlay: {
    flex: 1,
    backgroundColor: 'rgba(0,0,0,0.75)',
    justifyContent: 'center',
    padding: spacing.xl,
  },
  modal: {
    backgroundColor: colors.card,
    borderRadius: radius.xxl,
    padding: spacing.xl,
    maxHeight: '80%',
    borderWidth: 1,
    borderColor: colors.cardBorder,
  },
  modalTitle: {
    fontSize: fontSize.xl,
    fontWeight: '800',
    color: colors.white,
    textAlign: 'center',
    marginBottom: spacing.lg,
  },
  searchInput: {
    backgroundColor: colors.surface,
    borderRadius: radius.lg,
    padding: spacing.md,
    color: colors.white,
    fontSize: fontSize.md,
    marginBottom: spacing.md,
  },
  list: {
    maxHeight: 300,
  },
  gridRow: {
    gap: spacing.sm,
    marginBottom: spacing.sm,
  },
  symbolBtn: {
    flex: 1,
    paddingVertical: spacing.md,
    borderRadius: radius.md,
    alignItems: 'center',
    backgroundColor: colors.surface,
  },
  symbolBtnActive: {
    backgroundColor: colors.gold,
  },
  symbolText: {
    color: colors.textSecondary,
    fontSize: fontSize.sm,
    fontWeight: '600',
  },
  symbolTextActive: {
    color: colors.white,
    fontWeight: '700',
  },
  emptyText: {
    color: colors.textMuted,
    textAlign: 'center',
    paddingVertical: spacing.xl,
    fontSize: fontSize.sm,
  },
  customRow: {
    flexDirection: 'row',
    gap: spacing.sm,
    marginTop: spacing.md,
  },
  customInput: {
    flex: 1,
    backgroundColor: colors.surface,
    borderRadius: radius.lg,
    padding: spacing.md,
    color: colors.white,
    fontSize: fontSize.md,
  },
  customBtn: {
    backgroundColor: colors.gold,
    borderRadius: radius.lg,
    paddingHorizontal: spacing.xl,
    justifyContent: 'center',
  },
  customBtnText: {
    color: colors.white,
    fontSize: fontSize.md,
    fontWeight: '700',
  },
});
