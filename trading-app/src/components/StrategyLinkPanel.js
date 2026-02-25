import React, { useState, useEffect, useCallback, useRef } from 'react';
import {
  View, Text, StyleSheet, TouchableOpacity, TextInput, ScrollView,
  Alert, Switch, Modal,
} from 'react-native';
import { colors, spacing, radius, fontSize } from '../services/theme';
import api from '../services/api';

const TRIGGER_OPTIONS = [
  { value: 'rsi_buy', label: 'RSI 超卖' },
  { value: 'rsi_sell', label: 'RSI 超买' },
  { value: 'liq_spike', label: '爆仓突增' },
  { value: 'funding_high', label: '费率异常' },
];

const ACTION_OPTIONS = [
  { value: 'start_grid', label: '启动网格' },
  { value: 'close_position', label: '平仓' },
  { value: 'reduce_position', label: '减仓' },
  { value: 'reduce_leverage', label: '降杠杆' },
  { value: 'place_order', label: '开仓' },
];

const DEFAULT_RULE = {
  id: '',
  name: '',
  enabled: true,
  trigger: 'rsi_buy',
  triggerSymbol: '',
  action: 'place_order',
  actionSymbol: '',
  actionParams: {},
  cooldownSec: 300,
};

export default function StrategyLinkPanel({ symbol }) {
  const [status, setStatus] = useState(null);
  const [loading, setLoading] = useState(false);
  const [rules, setRules] = useState([]);
  const [showEditor, setShowEditor] = useState(false);
  const [editRule, setEditRule] = useState({ ...DEFAULT_RULE });
  const [editIndex, setEditIndex] = useState(-1); // -1 = 新增
  const [showLogs, setShowLogs] = useState(false);

  const timerRef = useRef(null);

  const fetchStatus = useCallback(async () => {
    try {
      const res = await api.strategyLinkStatus();
      if (res?.data) {
        setStatus(res.data);
        if (res.data.rules?.length > 0 && rules.length === 0) {
          setRules(res.data.rules);
        }
      }
    } catch (_) {}
  }, [rules.length]);

  useEffect(() => {
    fetchStatus();
    timerRef.current = setInterval(fetchStatus, 10000);
    return () => { if (timerRef.current) clearInterval(timerRef.current); };
  }, [fetchStatus]);

  const isActive = status?.active;

  const handleStart = async () => {
    if (rules.length === 0) {
      Alert.alert('请先添加规则', '至少需要一条联动规则');
      return;
    }
    setLoading(true);
    try {
      await api.startStrategyLink(rules);
      Alert.alert('成功', '策略联动已启动');
      fetchStatus();
    } catch (e) {
      Alert.alert('失败', e.message);
    }
    setLoading(false);
  };

  const handleStop = async () => {
    setLoading(true);
    try {
      await api.stopStrategyLink();
      Alert.alert('成功', '策略联动已停止');
      fetchStatus();
    } catch (e) {
      Alert.alert('失败', e.message);
    }
    setLoading(false);
  };

  const openAddRule = () => {
    setEditRule({
      ...DEFAULT_RULE,
      id: `rule_${Date.now()}`,
      triggerSymbol: symbol || 'ETHUSDT',
      actionSymbol: symbol || 'ETHUSDT',
    });
    setEditIndex(-1);
    setShowEditor(true);
  };

  const openEditRule = (idx) => {
    setEditRule({ ...rules[idx] });
    setEditIndex(idx);
    setShowEditor(true);
  };

  const saveRule = () => {
    if (!editRule.name) {
      Alert.alert('请输入规则名称');
      return;
    }
    const newRules = [...rules];
    if (editIndex >= 0) {
      newRules[editIndex] = editRule;
    } else {
      newRules.push(editRule);
    }
    setRules(newRules);
    setShowEditor(false);

    // 如果正在运行，同步更新到后端
    if (isActive) {
      api.updateStrategyLinkRules(newRules).catch(() => {});
    }
  };

  const deleteRule = (idx) => {
    Alert.alert('删除规则', `确定删除"${rules[idx].name}"？`, [
      { text: '取消', style: 'cancel' },
      {
        text: '删除', style: 'destructive', onPress: () => {
          const newRules = rules.filter((_, i) => i !== idx);
          setRules(newRules);
          if (isActive) {
            api.updateStrategyLinkRules(newRules).catch(() => {});
          }
        },
      },
    ]);
  };

  const toggleRuleEnabled = (idx) => {
    const newRules = [...rules];
    newRules[idx] = { ...newRules[idx], enabled: !newRules[idx].enabled };
    setRules(newRules);
    if (isActive) {
      api.updateStrategyLinkRules(newRules).catch(() => {});
    }
  };

  const getTriggerLabel = (t) => TRIGGER_OPTIONS.find((o) => o.value === t)?.label || t;
  const getActionLabel = (a) => ACTION_OPTIONS.find((o) => o.value === a)?.label || a;

  const updateParam = (key, val) => {
    setEditRule((prev) => ({
      ...prev,
      actionParams: { ...prev.actionParams, [key]: val },
    }));
  };

  return (
    <View style={styles.card}>
      <View style={styles.header}>
        <View style={styles.headerLeft}>
          <Text style={styles.title}>策略联动</Text>
          <View style={[styles.statusDot, { backgroundColor: isActive ? colors.green : colors.textMuted }]} />
          {rules.length > 0 && (
            <Text style={styles.ruleCount}>{rules.length}条</Text>
          )}
        </View>
        <View style={styles.headerRight}>
          <TouchableOpacity style={styles.addBtn} onPress={openAddRule}>
            <Text style={styles.addBtnText}>+ 规则</Text>
          </TouchableOpacity>
          <TouchableOpacity
            style={[styles.actionBtn, isActive ? styles.stopBtn : styles.startBtn]}
            onPress={isActive ? handleStop : handleStart}
            disabled={loading}
          >
            <Text style={[styles.actionBtnText, isActive ? styles.stopBtnText : styles.startBtnText]}>
              {loading ? '...' : isActive ? '停止' : '启动'}
            </Text>
          </TouchableOpacity>
        </View>
      </View>

      {/* 规则列表 */}
      {rules.map((rule, idx) => (
        <TouchableOpacity key={rule.id || idx} style={styles.ruleCard} onPress={() => openEditRule(idx)} onLongPress={() => deleteRule(idx)}>
          <View style={styles.ruleTop}>
            <View style={styles.ruleNameRow}>
              <Switch
                value={rule.enabled}
                onValueChange={() => toggleRuleEnabled(idx)}
                trackColor={{ false: colors.surface, true: colors.goldBg }}
                thumbColor={rule.enabled ? colors.gold : colors.textMuted}
                style={{ transform: [{ scaleX: 0.7 }, { scaleY: 0.7 }] }}
              />
              <Text style={[styles.ruleName, !rule.enabled && styles.ruleDisabled]}>{rule.name}</Text>
            </View>
            <Text style={styles.ruleCooldown}>{rule.cooldownSec}s</Text>
          </View>
          <View style={styles.ruleFlow}>
            <View style={[styles.flowBadge, styles.triggerBadge]}>
              <Text style={styles.flowBadgeText}>{getTriggerLabel(rule.trigger)}</Text>
            </View>
            {rule.triggerSymbol ? (
              <Text style={styles.flowSymbol}>{rule.triggerSymbol.replace('USDT', '')}</Text>
            ) : null}
            <Text style={styles.flowArrow}>→</Text>
            <View style={[styles.flowBadge, styles.actionBadge]}>
              <Text style={styles.flowBadgeText}>{getActionLabel(rule.action)}</Text>
            </View>
            {rule.actionSymbol ? (
              <Text style={styles.flowSymbol}>{rule.actionSymbol.replace('USDT', '')}</Text>
            ) : null}
          </View>
        </TouchableOpacity>
      ))}

      {rules.length === 0 && (
        <Text style={styles.emptyText}>暂无联动规则，点击"+ 规则"添加</Text>
      )}

      {/* 触发日志 */}
      {status?.lastChecks?.length > 0 && (
        <View style={styles.logSection}>
          <TouchableOpacity style={styles.logHeader} onPress={() => setShowLogs(!showLogs)}>
            <Text style={styles.logTitle}>触发日志 ({status.lastChecks.length})</Text>
            <Text style={styles.expandIcon}>{showLogs ? '▾' : '▸'}</Text>
          </TouchableOpacity>
          {showLogs && status.lastChecks.slice(-10).reverse().map((log, idx) => (
            <View key={idx} style={styles.logItem}>
              <Text style={styles.logTime}>{log.time}</Text>
              <Text style={styles.logName}>{log.ruleName}</Text>
              <Text style={[styles.logDetail, log.triggered && styles.logTriggered]} numberOfLines={2}>{log.detail}</Text>
            </View>
          ))}
        </View>
      )}

      {/* 规则编辑弹窗 */}
      <Modal visible={showEditor} transparent animationType="slide">
        <View style={styles.modalOverlay}>
          <View style={styles.modalContent}>
            <ScrollView showsVerticalScrollIndicator={false}>
              <Text style={styles.modalTitle}>{editIndex >= 0 ? '编辑规则' : '新增规则'}</Text>

              <Text style={styles.fieldLabel}>规则名称</Text>
              <TextInput
                style={styles.fieldInput}
                value={editRule.name}
                onChangeText={(v) => setEditRule((p) => ({ ...p, name: v }))}
                placeholder="如：RSI超卖自动开多"
                placeholderTextColor={colors.textMuted}
              />

              <Text style={styles.fieldLabel}>触发条件</Text>
              <View style={styles.optionRow}>
                {TRIGGER_OPTIONS.map((opt) => (
                  <TouchableOpacity
                    key={opt.value}
                    style={[styles.optionChip, editRule.trigger === opt.value && styles.optionChipActive]}
                    onPress={() => setEditRule((p) => ({ ...p, trigger: opt.value }))}
                  >
                    <Text style={[styles.optionChipText, editRule.trigger === opt.value && styles.optionChipTextActive]}>
                      {opt.label}
                    </Text>
                  </TouchableOpacity>
                ))}
              </View>

              {(editRule.trigger === 'rsi_buy' || editRule.trigger === 'rsi_sell' || editRule.trigger === 'funding_high') && (
                <>
                  <Text style={styles.fieldLabel}>触发币种</Text>
                  <TextInput
                    style={styles.fieldInput}
                    value={editRule.triggerSymbol}
                    onChangeText={(v) => setEditRule((p) => ({ ...p, triggerSymbol: v.toUpperCase() }))}
                    placeholder="ETHUSDT"
                    placeholderTextColor={colors.textMuted}
                    autoCapitalize="characters"
                  />
                </>
              )}

              {editRule.trigger === 'liq_spike' && (
                <>
                  <Text style={styles.fieldLabel}>爆仓阈值 (百万USD)</Text>
                  <TextInput
                    style={styles.fieldInput}
                    value={editRule.actionParams?.liqThresholdM || '50'}
                    onChangeText={(v) => updateParam('liqThresholdM', v)}
                    keyboardType="numeric"
                    placeholderTextColor={colors.textMuted}
                  />
                </>
              )}

              <Text style={styles.fieldLabel}>执行动作</Text>
              <View style={styles.optionRow}>
                {ACTION_OPTIONS.map((opt) => (
                  <TouchableOpacity
                    key={opt.value}
                    style={[styles.optionChip, editRule.action === opt.value && styles.optionChipActive]}
                    onPress={() => setEditRule((p) => ({ ...p, action: opt.value }))}
                  >
                    <Text style={[styles.optionChipText, editRule.action === opt.value && styles.optionChipTextActive]}>
                      {opt.label}
                    </Text>
                  </TouchableOpacity>
                ))}
              </View>

              <Text style={styles.fieldLabel}>动作目标币种</Text>
              <TextInput
                style={styles.fieldInput}
                value={editRule.actionSymbol}
                onChangeText={(v) => setEditRule((p) => ({ ...p, actionSymbol: v.toUpperCase() }))}
                placeholder="ETHUSDT"
                placeholderTextColor={colors.textMuted}
                autoCapitalize="characters"
              />

              {/* 动作参数 */}
              {editRule.action === 'place_order' && (
                <>
                  <View style={styles.paramRow}>
                    <View style={styles.paramItem}>
                      <Text style={styles.fieldLabel}>方向</Text>
                      <View style={styles.sideRow}>
                        {['BUY', 'SELL'].map((s) => (
                          <TouchableOpacity
                            key={s}
                            style={[styles.sideChip, (editRule.actionParams?.side || '') === s && (s === 'BUY' ? styles.buyChipActive : styles.sellChipActive)]}
                            onPress={() => updateParam('side', s)}
                          >
                            <Text style={[styles.sideChipText, (editRule.actionParams?.side || '') === s && styles.sideChipTextActive]}>
                              {s === 'BUY' ? '做多' : '做空'}
                            </Text>
                          </TouchableOpacity>
                        ))}
                        <TouchableOpacity
                          style={[styles.sideChip, !editRule.actionParams?.side && styles.autoChipActive]}
                          onPress={() => updateParam('side', '')}
                        >
                          <Text style={[styles.sideChipText, !editRule.actionParams?.side && styles.sideChipTextActive]}>自动</Text>
                        </TouchableOpacity>
                      </View>
                    </View>
                  </View>
                  <View style={styles.paramRow}>
                    <View style={styles.paramItem}>
                      <Text style={styles.fieldLabel}>金额(U)</Text>
                      <TextInput style={styles.fieldInput} value={editRule.actionParams?.amountPerOrder || '5'} onChangeText={(v) => updateParam('amountPerOrder', v)} keyboardType="numeric" placeholderTextColor={colors.textMuted} />
                    </View>
                    <View style={styles.paramItem}>
                      <Text style={styles.fieldLabel}>杠杆</Text>
                      <TextInput style={styles.fieldInput} value={editRule.actionParams?.leverage || '10'} onChangeText={(v) => updateParam('leverage', v)} keyboardType="numeric" placeholderTextColor={colors.textMuted} />
                    </View>
                  </View>
                </>
              )}

              {editRule.action === 'reduce_position' && (
                <View style={styles.paramRow}>
                  <View style={styles.paramItem}>
                    <Text style={styles.fieldLabel}>减仓比例%</Text>
                    <TextInput style={styles.fieldInput} value={editRule.actionParams?.reducePercent || '50'} onChangeText={(v) => updateParam('reducePercent', v)} keyboardType="numeric" placeholderTextColor={colors.textMuted} />
                  </View>
                </View>
              )}

              {editRule.action === 'reduce_leverage' && (
                <View style={styles.paramRow}>
                  <View style={styles.paramItem}>
                    <Text style={styles.fieldLabel}>目标杠杆</Text>
                    <TextInput style={styles.fieldInput} value={editRule.actionParams?.targetLeverage || '3'} onChangeText={(v) => updateParam('targetLeverage', v)} keyboardType="numeric" placeholderTextColor={colors.textMuted} />
                  </View>
                </View>
              )}

              {editRule.action === 'start_grid' && (
                <View style={styles.paramRow}>
                  <View style={styles.paramItem}>
                    <Text style={styles.fieldLabel}>格数</Text>
                    <TextInput style={styles.fieldInput} value={editRule.actionParams?.gridCount || '5'} onChangeText={(v) => updateParam('gridCount', v)} keyboardType="numeric" placeholderTextColor={colors.textMuted} />
                  </View>
                  <View style={styles.paramItem}>
                    <Text style={styles.fieldLabel}>每格金额(U)</Text>
                    <TextInput style={styles.fieldInput} value={editRule.actionParams?.amountPerGrid || '5'} onChangeText={(v) => updateParam('amountPerGrid', v)} keyboardType="numeric" placeholderTextColor={colors.textMuted} />
                  </View>
                </View>
              )}

              <Text style={styles.fieldLabel}>冷却时间(秒)</Text>
              <TextInput
                style={styles.fieldInput}
                value={String(editRule.cooldownSec || 300)}
                onChangeText={(v) => setEditRule((p) => ({ ...p, cooldownSec: parseInt(v) || 300 }))}
                keyboardType="numeric"
                placeholderTextColor={colors.textMuted}
              />

              <View style={styles.modalBtns}>
                <TouchableOpacity style={styles.cancelBtn} onPress={() => setShowEditor(false)}>
                  <Text style={styles.cancelBtnText}>取消</Text>
                </TouchableOpacity>
                <TouchableOpacity style={styles.saveBtn} onPress={saveRule}>
                  <Text style={styles.saveBtnText}>保存</Text>
                </TouchableOpacity>
              </View>
            </ScrollView>
          </View>
        </View>
      </Modal>
    </View>
  );
}

const styles = StyleSheet.create({
  card: {
    backgroundColor: colors.card,
    borderRadius: radius.lg,
    borderWidth: 1,
    borderColor: colors.cardBorder,
    padding: spacing.lg,
  },
  header: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'center',
    marginBottom: spacing.md,
  },
  headerLeft: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: spacing.sm,
  },
  headerRight: {
    flexDirection: 'row',
    gap: spacing.sm,
  },
  title: {
    fontSize: fontSize.lg,
    fontWeight: '700',
    color: colors.white,
  },
  statusDot: {
    width: 8,
    height: 8,
    borderRadius: 4,
  },
  ruleCount: {
    fontSize: fontSize.xs,
    color: colors.textMuted,
  },
  addBtn: {
    paddingHorizontal: spacing.md,
    paddingVertical: spacing.sm,
    borderRadius: radius.md,
    borderWidth: 1,
    borderColor: colors.blue,
    backgroundColor: colors.blueBg,
  },
  addBtnText: {
    fontSize: fontSize.sm,
    fontWeight: '700',
    color: colors.blue,
  },
  actionBtn: {
    paddingHorizontal: spacing.lg,
    paddingVertical: spacing.sm,
    borderRadius: radius.md,
    borderWidth: 1,
  },
  startBtn: {
    backgroundColor: colors.goldBg,
    borderColor: colors.gold,
  },
  stopBtn: {
    backgroundColor: colors.redBg,
    borderColor: colors.red,
  },
  actionBtnText: {
    fontSize: fontSize.sm,
    fontWeight: '700',
  },
  startBtnText: { color: colors.gold },
  stopBtnText: { color: colors.red },

  // 规则卡片
  ruleCard: {
    backgroundColor: colors.surface,
    borderRadius: radius.md,
    padding: spacing.md,
    marginBottom: spacing.sm,
  },
  ruleTop: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'center',
    marginBottom: spacing.xs,
  },
  ruleNameRow: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: spacing.xs,
  },
  ruleName: {
    fontSize: fontSize.sm,
    fontWeight: '700',
    color: colors.white,
  },
  ruleDisabled: {
    color: colors.textMuted,
  },
  ruleCooldown: {
    fontSize: fontSize.xs,
    color: colors.textMuted,
  },
  ruleFlow: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: spacing.xs,
    flexWrap: 'wrap',
  },
  flowBadge: {
    paddingHorizontal: spacing.sm,
    paddingVertical: 2,
    borderRadius: radius.sm,
  },
  triggerBadge: {
    backgroundColor: colors.purpleBg,
  },
  actionBadge: {
    backgroundColor: colors.blueBg,
  },
  flowBadgeText: {
    fontSize: fontSize.xs,
    fontWeight: '600',
    color: colors.text,
  },
  flowSymbol: {
    fontSize: fontSize.xs,
    fontWeight: '600',
    color: colors.gold,
  },
  flowArrow: {
    fontSize: fontSize.md,
    color: colors.textMuted,
  },
  emptyText: {
    color: colors.textMuted,
    textAlign: 'center',
    paddingVertical: spacing.xl,
    fontSize: fontSize.sm,
  },

  // 日志
  logSection: {
    marginTop: spacing.sm,
  },
  logHeader: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'center',
  },
  logTitle: {
    fontSize: fontSize.sm,
    fontWeight: '700',
    color: colors.textSecondary,
  },
  expandIcon: {
    fontSize: fontSize.md,
    color: colors.textMuted,
  },
  logItem: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: spacing.sm,
    paddingVertical: spacing.xs,
    borderBottomWidth: StyleSheet.hairlineWidth,
    borderBottomColor: colors.divider,
  },
  logTime: {
    fontSize: fontSize.xs,
    color: colors.textMuted,
    fontFamily: 'monospace',
    width: 55,
  },
  logName: {
    fontSize: fontSize.xs,
    fontWeight: '600',
    color: colors.text,
    width: 70,
  },
  logDetail: {
    fontSize: fontSize.xs,
    color: colors.textSecondary,
    flex: 1,
  },
  logTriggered: {
    color: colors.gold,
  },

  // 弹窗
  modalOverlay: {
    flex: 1,
    backgroundColor: 'rgba(0,0,0,0.7)',
    justifyContent: 'flex-end',
  },
  modalContent: {
    backgroundColor: colors.card,
    borderTopLeftRadius: radius.xl,
    borderTopRightRadius: radius.xl,
    padding: spacing.lg,
    maxHeight: '85%',
  },
  modalTitle: {
    fontSize: fontSize.xl,
    fontWeight: '800',
    color: colors.white,
    marginBottom: spacing.lg,
    textAlign: 'center',
  },
  fieldLabel: {
    fontSize: fontSize.xs,
    color: colors.textMuted,
    marginBottom: spacing.xs,
    marginTop: spacing.sm,
  },
  fieldInput: {
    backgroundColor: colors.surface,
    borderWidth: 1,
    borderColor: colors.cardBorder,
    borderRadius: radius.sm,
    color: colors.text,
    fontSize: fontSize.sm,
    paddingHorizontal: spacing.md,
    paddingVertical: spacing.sm,
  },
  optionRow: {
    flexDirection: 'row',
    flexWrap: 'wrap',
    gap: spacing.sm,
  },
  optionChip: {
    paddingHorizontal: spacing.md,
    paddingVertical: spacing.sm,
    borderRadius: radius.pill,
    borderWidth: 1,
    borderColor: colors.cardBorder,
    backgroundColor: colors.surface,
  },
  optionChipActive: {
    borderColor: colors.gold,
    backgroundColor: colors.goldBg,
  },
  optionChipText: {
    fontSize: fontSize.sm,
    color: colors.textMuted,
    fontWeight: '600',
  },
  optionChipTextActive: {
    color: colors.gold,
  },
  paramRow: {
    flexDirection: 'row',
    gap: spacing.sm,
  },
  paramItem: {
    flex: 1,
  },
  sideRow: {
    flexDirection: 'row',
    gap: spacing.sm,
  },
  sideChip: {
    paddingHorizontal: spacing.md,
    paddingVertical: spacing.sm,
    borderRadius: radius.sm,
    borderWidth: 1,
    borderColor: colors.cardBorder,
    backgroundColor: colors.surface,
  },
  buyChipActive: {
    borderColor: colors.green,
    backgroundColor: colors.greenBg,
  },
  sellChipActive: {
    borderColor: colors.red,
    backgroundColor: colors.redBg,
  },
  autoChipActive: {
    borderColor: colors.gold,
    backgroundColor: colors.goldBg,
  },
  sideChipText: {
    fontSize: fontSize.sm,
    color: colors.textMuted,
    fontWeight: '600',
  },
  sideChipTextActive: {
    color: colors.white,
  },
  modalBtns: {
    flexDirection: 'row',
    gap: spacing.md,
    marginTop: spacing.xl,
    marginBottom: spacing.lg,
  },
  cancelBtn: {
    flex: 1,
    paddingVertical: spacing.md,
    borderRadius: radius.md,
    borderWidth: 1,
    borderColor: colors.cardBorder,
    alignItems: 'center',
  },
  cancelBtnText: {
    fontSize: fontSize.md,
    fontWeight: '700',
    color: colors.textMuted,
  },
  saveBtn: {
    flex: 1,
    paddingVertical: spacing.md,
    borderRadius: radius.md,
    backgroundColor: colors.gold,
    alignItems: 'center',
  },
  saveBtnText: {
    fontSize: fontSize.md,
    fontWeight: '700',
    color: colors.white,
  },
});
