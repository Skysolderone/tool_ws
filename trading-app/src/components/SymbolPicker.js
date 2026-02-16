import React from 'react';
import { View, Text, StyleSheet } from 'react-native';
import { colors } from '../services/theme';

export default function SymbolPicker() {
  return (
    <View style={styles.container}>
      <View style={styles.chip}>
        <Text style={styles.chipText}>ETHUSDT</Text>
      </View>
    </View>
  );
}

const styles = StyleSheet.create({
  container: { marginBottom: 12 },
  chip: {
    alignSelf: 'flex-start',
    paddingHorizontal: 16,
    paddingVertical: 8,
    borderRadius: 8,
    backgroundColor: colors.blue,
  },
  chipText: { color: colors.white, fontSize: 15, fontWeight: 'bold' },
});
