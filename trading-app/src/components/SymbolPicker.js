import React, { useState } from 'react';
import {
  View, Text, TextInput, TouchableOpacity, StyleSheet, Modal, FlatList,
} from 'react-native';
import { colors } from '../services/theme';

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
  container: { marginBottom: 12 },
  chip: {
    alignSelf: 'flex-start',
    flexDirection: 'row',
    alignItems: 'center',
    paddingHorizontal: 16,
    paddingVertical: 8,
    borderRadius: 8,
    backgroundColor: colors.blue,
  },
  chipText: { color: colors.white, fontSize: 15, fontWeight: 'bold' },
  arrow: { color: colors.white, fontSize: 11 },

  overlay: {
    flex: 1,
    backgroundColor: 'rgba(0,0,0,0.7)',
    justifyContent: 'center',
    padding: 20,
  },
  modal: {
    backgroundColor: colors.card,
    borderRadius: 16,
    padding: 16,
    maxHeight: '80%',
  },
  modalTitle: {
    fontSize: 18,
    fontWeight: 'bold',
    color: colors.white,
    textAlign: 'center',
    marginBottom: 12,
  },
  searchInput: {
    backgroundColor: colors.bg,
    borderRadius: 8,
    padding: 10,
    color: colors.white,
    borderWidth: 1,
    borderColor: colors.cardBorder,
    fontSize: 14,
    marginBottom: 12,
  },
  list: {
    maxHeight: 300,
  },
  gridRow: {
    gap: 8,
    marginBottom: 8,
  },
  symbolBtn: {
    flex: 1,
    paddingVertical: 10,
    borderRadius: 8,
    alignItems: 'center',
    backgroundColor: colors.surface,
    borderWidth: 1,
    borderColor: colors.cardBorder,
  },
  symbolBtnActive: {
    backgroundColor: colors.blue,
    borderColor: colors.blue,
  },
  symbolText: {
    color: colors.textSecondary,
    fontSize: 13,
    fontWeight: '600',
  },
  symbolTextActive: {
    color: colors.white,
  },
  emptyText: {
    color: colors.textMuted,
    textAlign: 'center',
    paddingVertical: 20,
  },
  customRow: {
    flexDirection: 'row',
    gap: 8,
    marginTop: 12,
  },
  customInput: {
    flex: 1,
    backgroundColor: colors.bg,
    borderRadius: 8,
    padding: 10,
    color: colors.white,
    borderWidth: 1,
    borderColor: colors.cardBorder,
    fontSize: 14,
  },
  customBtn: {
    backgroundColor: colors.blue,
    borderRadius: 8,
    paddingHorizontal: 16,
    justifyContent: 'center',
  },
  customBtnText: {
    color: colors.white,
    fontSize: 14,
    fontWeight: '600',
  },
});
