import React, { useState, useEffect, useMemo, useCallback } from 'react';
import {
  View, Text, TextInput, TouchableOpacity,
  StyleSheet, Alert, ActivityIndicator, ScrollView, Modal,
} from 'react-native';
import AsyncStorage from '@react-native-async-storage/async-storage';
import api from '../services/api';
import { colors, spacing, radius, fontSize } from '../services/theme';

const TEMPLATES_KEY = '@order_templates';

/**
 * 下单面板（专业交易所风格）
 * @param {string} symbol
 * @param {number|null} externalMarkPrice - 从 App.js 传入的实时价格
 */
export default function OrderPanel({ symbol, externalMarkPrice, walletBalance = null, positions = [], preset = null }) {
  const [side, setSide] = useState('BUY');
  const [quoteQty, setQuoteQty] = useState('5');
  const [leverage, setLeverage] = useState('10');
  const [stopLossAmount, setStopLossAmount] = useState('');
  const [riskReward, setRiskReward] = useState('');
  const [loading, setLoading] = useState(false);
  const [result, setResult] = useState(null);
  const [showTpsl, setShowTpsl] = useState(false);

  // 推荐预设止损止盈价格
  const [presetSLPrice, setPresetSLPrice] = useState('');
  const [presetTPPrice, setPresetTPPrice] = useState('');

  // 接收推荐面板传入的预设参数
  useEffect(() => {
    if (!preset) return;
    if (preset.direction === 'LONG') setSide('BUY');
    else if (preset.direction === 'SHORT') setSide('SELL');
    if (preset.stopLoss) setPresetSLPrice(String(preset.stopLoss));
    if (preset.takeProfit) setPresetTPPrice(String(preset.takeProfit));
    setStopLossAmount('');
    setRiskReward('');
    setShowTpsl(true);
  }, [preset]);

  // 阶梯止盈
  const [useComboTP, setUseComboTP] = useState(false);
  const [tpLevels, setTpLevels] = useState([
    { percent: '50', riskReward: '2' },
    { percent: '50', riskReward: '5' },
  ]);

  // 模板
  const [templates, setTemplates] = useState([]);
  const [showTemplates, setShowTemplates] = useState(false);
  const [savingTemplate, setSavingTemplate] = useState(false);
  const [templateName, setTemplateName] = useState('');

  const markPrice = externalMarkPrice;

  // 加载模板
  useEffect(() => {
    AsyncStorage.getItem(TEMPLATES_KEY).then((data) => {
      if (data) setTemplates(JSON.parse(data));
    }).catch(() => {});
  }, []);

  const saveTemplates = async (list) => {
    setTemplates(list);
    await AsyncStorage.setItem(TEMPLATES_KEY, JSON.stringify(list));
  };

  // 实时计算
  const tpslPreview = useMemo(() => {
    if (!markPrice) return null;
    const qty = parseFloat(quoteQty);
    const lev = parseInt(leverage, 10);
    if (!qty || !lev) return null;

    const newQuantity = (qty * lev) / markPrice;
    const mmRate = 0.005;
    const res = {};

    // TP/SL 预览
    const sl = parseFloat(stopLossAmount);
    if (sl) {
      const slDist = sl / newQuantity;
      if (side === 'BUY') {
        res.slPrice = (markPrice - slDist).toFixed(2);
      } else {
        res.slPrice = (markPrice + slDist).toFixed(2);
      }

      if (useComboTP && tpLevels.length > 0) {
        // 阶梯止盈预览
        res.tpLevelPreviews = tpLevels.map((lv) => {
          const rr = parseFloat(lv.riskReward) || 0;
          const pct = parseFloat(lv.percent) || 0;
          let tp;
          if (side === 'BUY') {
            tp = markPrice + slDist * rr;
          } else {
            tp = markPrice - slDist * rr;
          }
          return {
            percent: pct,
            riskReward: rr,
            tpPrice: tp.toFixed(2),
            profit: (sl * rr * pct / 100).toFixed(2),
          };
        });
        res.tpProfit = res.tpLevelPreviews.reduce((s, l) => s + parseFloat(l.profit), 0).toFixed(2);
      } else {
        const rr = parseFloat(riskReward);
        if (rr) {
          if (side === 'BUY') {
            res.tpPrice = (markPrice + slDist * rr).toFixed(2);
          } else {
            res.tpPrice = (markPrice - slDist * rr).toFixed(2);
          }
          res.tpProfit = (sl * rr).toFixed(2);
        }
      }
    }

    // 合并已有持仓 → 强平价
    const newPosSide = side === 'BUY' ? 'LONG' : 'SHORT';
    let existingQty = 0;
    let existingEntry = 0;
    let existingLiqPrice = null;

    for (const pos of positions) {
      const posAmt = parseFloat(pos.positionAmt || 0);
      const posEntry = parseFloat(pos.entryPrice || 0);
      const posSym = pos.symbol || '';
      const posSide = pos.positionSide || 'BOTH';
      const posLiq = parseFloat(pos.liquidationPrice || 0);
      if (posAmt === 0) continue;
      if (posSym === symbol && (posSide === newPosSide || posSide === 'BOTH')) {
        existingQty = Math.abs(posAmt);
        existingEntry = posEntry;
        if (posLiq > 0) existingLiqPrice = posLiq;
      }
    }

    const totalQty = existingQty + newQuantity;
    const avgEntry = totalQty > 0
      ? (existingQty * existingEntry + newQuantity * markPrice) / totalQty
      : markPrice;

    let liqPrice;
    const wb = walletBalance;
    const posNotional = avgEntry * totalQty;

    if (wb != null && wb > 0) {
      const maintMargin = posNotional * mmRate;
      if (side === 'BUY') {
        liqPrice = avgEntry - (wb - maintMargin) / totalQty;
      } else {
        liqPrice = avgEntry + (wb - maintMargin) / totalQty;
      }
      res.walletBalance = wb.toFixed(2);
      res.effectiveLev = (posNotional / wb).toFixed(1);
    } else {
      if (side === 'BUY') {
        liqPrice = avgEntry * (1 - 1 / lev + mmRate);
      } else {
        liqPrice = avgEntry * (1 + 1 / lev - mmRate);
      }
    }

    if (liqPrice < 0) liqPrice = 0;
    res.liqPrice = liqPrice.toFixed(2);
    if (existingLiqPrice) res.binanceLiqPrice = existingLiqPrice.toFixed(2);
    res.existingQty = existingQty > 0 ? existingQty.toFixed(4) : null;
    res.totalQty = totalQty.toFixed(4);
    res.avgEntry = avgEntry.toFixed(2);

    return res;
  }, [markPrice, stopLossAmount, riskReward, quoteQty, leverage, side, walletBalance, positions, symbol, useComboTP, tpLevels]);

  const handleOrder = async () => {
    if (!symbol) return Alert.alert('提示', '请选择交易对');
    if (!quoteQty) return Alert.alert('提示', '请输入下单金额');
    if (!leverage) return Alert.alert('提示', '请输入杠杆倍数');

    if (!markPrice) return Alert.alert('提示', '价格加载中，请稍后');

    const req = {
      symbol,
      side,
      positionSide: side === 'BUY' ? 'LONG' : 'SHORT',
      orderType: 'LIMIT',
      price: String(markPrice),
      timeInForce: 'GTC',
      quoteQuantity: quoteQty,
      leverage: parseInt(leverage, 10),
    };

    if (presetSLPrice && presetTPPrice && markPrice) {
      // 推荐预设模式：直接用止损价 + 盈亏比
      const slP = parseFloat(presetSLPrice);
      const tpP = parseFloat(presetTPPrice);
      if (slP > 0 && tpP > 0) {
        req.stopLossPrice = presetSLPrice;
        const slDist = Math.abs(markPrice - slP);
        const tpDist = Math.abs(tpP - markPrice);
        if (slDist > 0) {
          req.riskReward = parseFloat((tpDist / slDist).toFixed(2));
        }
      }
    } else if (stopLossAmount) {
      req.stopLossAmount = parseFloat(stopLossAmount);

      if (useComboTP && tpLevels.length > 0) {
        // 阶梯止盈
        req.tpLevels = tpLevels.map((lv) => ({
          percent: parseFloat(lv.percent) || 0,
          riskReward: parseFloat(lv.riskReward) || 0,
        }));
        // 需要一个 riskReward 给 SL 计算用（取第一级的）
        req.riskReward = req.tpLevels[0].riskReward;
      } else if (riskReward) {
        req.riskReward = parseFloat(riskReward);
      }
    }

    setLoading(true);
    setResult(null);
    try {
      const data = await api.placeOrder(req);
      setResult(data.data);
      Alert.alert('下单成功', `订单ID: ${data.data?.order?.orderId || 'N/A'}`);
    } catch (e) {
      Alert.alert('下单失败', e.message);
    } finally {
      setLoading(false);
    }
  };

  // 阶梯止盈管理
  const addTPLevel = () => {
    if (tpLevels.length >= 5) return;
    setTpLevels([...tpLevels, { percent: '', riskReward: '' }]);
  };
  const removeTPLevel = (idx) => {
    setTpLevels(tpLevels.filter((_, i) => i !== idx));
  };
  const updateTPLevel = (idx, field, value) => {
    const next = [...tpLevels];
    next[idx] = { ...next[idx], [field]: value };
    setTpLevels(next);
  };

  // 模板操作
  const handleSaveTemplate = async () => {
    if (!templateName.trim()) return Alert.alert('提示', '请输入模板名称');
    const tpl = {
      id: Date.now().toString(),
      name: templateName.trim(),
      symbol,
      side,
      quoteQty,
      leverage,
      stopLossAmount,
      riskReward,
      useComboTP,
      tpLevels: useComboTP ? tpLevels : [],
    };
    const next = [tpl, ...templates].slice(0, 10); // 最多10个
    await saveTemplates(next);
    setTemplateName('');
    setSavingTemplate(false);
    Alert.alert('已保存', `模板 "${tpl.name}" 已保存`);
  };

  const applyTemplate = (tpl) => {
    setSide(tpl.side || 'BUY');
    setQuoteQty(tpl.quoteQty || '5');
    setLeverage(tpl.leverage || '10');
    setStopLossAmount(tpl.stopLossAmount || '');
    setRiskReward(tpl.riskReward || '');
    setUseComboTP(tpl.useComboTP || false);
    if (tpl.tpLevels && tpl.tpLevels.length > 0) {
      setTpLevels(tpl.tpLevels);
    }
    if (tpl.stopLossAmount) setShowTpsl(true);
    setShowTemplates(false);
  };

  const deleteTemplate = async (id) => {
    const next = templates.filter((t) => t.id !== id);
    await saveTemplates(next);
  };

  const isBuy = side === 'BUY';

  return (
    <View style={styles.panel}>
      {/* === 模板快捷栏 === */}
      <View style={styles.tplBar}>
        <TouchableOpacity
          style={styles.tplBtn}
          onPress={() => setShowTemplates(!showTemplates)}
        >
          <Text style={styles.tplBtnText}>▤ 模板</Text>
        </TouchableOpacity>
        <TouchableOpacity
          style={styles.tplBtn}
          onPress={() => setSavingTemplate(true)}
        >
          <Text style={styles.tplBtnText}>◆ 存为模板</Text>
        </TouchableOpacity>
      </View>

      {/* 模板列表 */}
      {showTemplates && templates.length > 0 && (
        <ScrollView horizontal style={styles.tplList} showsHorizontalScrollIndicator={false}>
          {templates.map((tpl) => (
            <TouchableOpacity
              key={tpl.id}
              style={styles.tplChip}
              onPress={() => applyTemplate(tpl)}
              onLongPress={() => {
                Alert.alert('删除模板', `确认删除 "${tpl.name}" ?`, [
                  { text: '取消', style: 'cancel' },
                  { text: '删除', style: 'destructive', onPress: () => deleteTemplate(tpl.id) },
                ]);
              }}
            >
              <Text style={styles.tplChipName}>{tpl.name}</Text>
              <Text style={styles.tplChipSub}>
                {tpl.side === 'BUY' ? '多' : '空'} {tpl.quoteQty}U {tpl.leverage}×
              </Text>
            </TouchableOpacity>
          ))}
        </ScrollView>
      )}
      {showTemplates && templates.length === 0 && (
        <Text style={styles.tplEmpty}>暂无模板，下单前点 "存为模板" 保存</Text>
      )}

      {/* === 方向选择 === */}
      <View style={styles.sideRow}>
        <TouchableOpacity
          style={[styles.sideBtn, styles.sideBtnBuy, isBuy && styles.buyActive]}
          onPress={() => setSide('BUY')}
          activeOpacity={0.8}
        >
          <Text style={[styles.sideText, isBuy && styles.sideTextActive]}>做多</Text>
        </TouchableOpacity>
        <TouchableOpacity
          style={[styles.sideBtn, styles.sideBtnSell, !isBuy && styles.sellActive]}
          onPress={() => setSide('SELL')}
          activeOpacity={0.8}
        >
          <Text style={[styles.sideText, !isBuy && styles.sideTextActive]}>做空</Text>
        </TouchableOpacity>
      </View>

      {/* === 金额输入 === */}
      <View style={styles.fieldRow}>
        <Text style={styles.fieldLabel}>金额</Text>
        <View style={styles.fieldInputWrap}>
          <TextInput
            style={styles.fieldInput}
            value={quoteQty}
            onChangeText={setQuoteQty}
            keyboardType="decimal-pad"
            placeholder="0.00"
            placeholderTextColor={colors.textMuted}
          />
          <Text style={styles.fieldUnit}>USDT</Text>
        </View>
      </View>

      {/* === 杠杆 === */}
      <View style={styles.fieldRow}>
        <Text style={styles.fieldLabel}>杠杆</Text>
        <View style={styles.fieldInputWrap}>
          <TextInput
            style={styles.fieldInput}
            value={leverage}
            onChangeText={setLeverage}
            keyboardType="number-pad"
            placeholder="10"
            placeholderTextColor={colors.textMuted}
          />
          <Text style={styles.fieldUnit}>×</Text>
        </View>
      </View>

      {/* 快捷杠杆 */}
      <View style={styles.levRow}>
        {['5', '10', '20', '50', '100'].map((lev) => (
          <TouchableOpacity
            key={lev}
            style={[styles.levChip, leverage === lev && styles.levChipActive]}
            onPress={() => setLeverage(lev)}
          >
            <Text style={[styles.levChipText, leverage === lev && styles.levChipTextActive]}>
              {lev}×
            </Text>
          </TouchableOpacity>
        ))}
      </View>

      {/* === 止盈止损折叠 === */}
      <TouchableOpacity
        style={styles.tpslToggle}
        onPress={() => setShowTpsl(!showTpsl)}
        activeOpacity={0.7}
      >
        <View style={{ flexDirection: 'row', alignItems: 'center', gap: spacing.xs }}>
          <Text style={styles.tpslToggleIcon}>{showTpsl ? '⊖' : '⊕'}</Text>
          <Text style={styles.tpslToggleText}>止盈止损</Text>
        </View>
        <Text style={styles.tpslToggleArrow}>{showTpsl ? '▾' : '▸'}</Text>
      </TouchableOpacity>

      {showTpsl && (
        <View style={styles.tpslBox}>
          {/* 推荐预设止损止盈价格 */}
          {(presetSLPrice || presetTPPrice) ? (
            <View style={styles.presetTpslRow}>
              <View style={styles.presetTpslItem}>
                <Text style={styles.presetTpslLabel}>止损价</Text>
                <TextInput
                  style={[styles.tpslInput, { borderColor: colors.red + '66' }]}
                  value={presetSLPrice}
                  onChangeText={setPresetSLPrice}
                  keyboardType="decimal-pad"
                  placeholderTextColor={colors.textMuted}
                />
              </View>
              <View style={styles.presetTpslItem}>
                <Text style={styles.presetTpslLabel}>止盈价</Text>
                <TextInput
                  style={[styles.tpslInput, { borderColor: colors.green + '66' }]}
                  value={presetTPPrice}
                  onChangeText={setPresetTPPrice}
                  keyboardType="decimal-pad"
                  placeholderTextColor={colors.textMuted}
                />
              </View>
              <TouchableOpacity
                style={styles.presetClearBtn}
                onPress={() => { setPresetSLPrice(''); setPresetTPPrice(''); }}
              >
                <Text style={styles.presetClearText}>清除</Text>
              </TouchableOpacity>
            </View>
          ) : null}
          {/* 止损金额（无预设价格时使用） */}
          {!presetSLPrice && (
          <View style={styles.tpslInputRow}>
            <View style={styles.tpslInputGroup}>
              <Text style={styles.tpslLabel}>止损金额(U)</Text>
              <TextInput
                style={styles.tpslInput}
                value={stopLossAmount}
                onChangeText={setStopLossAmount}
                keyboardType="decimal-pad"
                placeholder="1"
                placeholderTextColor={colors.textMuted}
              />
            </View>
            {!useComboTP && (
              <View style={styles.tpslInputGroup}>
                <Text style={styles.tpslLabel}>盈亏比</Text>
                <TextInput
                  style={styles.tpslInput}
                  value={riskReward}
                  onChangeText={setRiskReward}
                  keyboardType="decimal-pad"
                  placeholder="3"
                  placeholderTextColor={colors.textMuted}
                />
              </View>
            )}
          </View>
          )}

          {/* 阶梯止盈开关 */}
          <TouchableOpacity
            style={styles.comboToggle}
            onPress={() => setUseComboTP(!useComboTP)}
            activeOpacity={0.7}
          >
            <View style={[styles.comboCheck, useComboTP && styles.comboCheckActive]}>
              {useComboTP && <Text style={styles.comboCheckMark}>✓</Text>}
            </View>
            <Text style={styles.comboToggleText}>阶梯止盈</Text>
            <Text style={styles.comboHint}>分批止盈，锁定利润</Text>
          </TouchableOpacity>

          {/* 阶梯止盈列表 */}
          {useComboTP && (
            <View style={styles.comboBox}>
              {tpLevels.map((lv, idx) => (
                <View key={idx} style={styles.comboRow}>
                  <Text style={styles.comboIdx}>#{idx + 1}</Text>
                  <View style={styles.comboField}>
                    <Text style={styles.comboFieldLabel}>比例%</Text>
                    <TextInput
                      style={styles.comboInput}
                      value={lv.percent}
                      onChangeText={(v) => updateTPLevel(idx, 'percent', v)}
                      keyboardType="decimal-pad"
                      placeholder="50"
                      placeholderTextColor={colors.textMuted}
                    />
                  </View>
                  <View style={styles.comboField}>
                    <Text style={styles.comboFieldLabel}>盈亏比</Text>
                    <TextInput
                      style={styles.comboInput}
                      value={lv.riskReward}
                      onChangeText={(v) => updateTPLevel(idx, 'riskReward', v)}
                      keyboardType="decimal-pad"
                      placeholder="3"
                      placeholderTextColor={colors.textMuted}
                    />
                  </View>
                  {tpLevels.length > 1 && (
                    <TouchableOpacity
                      style={styles.comboRemove}
                      onPress={() => removeTPLevel(idx)}
                    >
                      <Text style={styles.comboRemoveText}>✕</Text>
                    </TouchableOpacity>
                  )}
                </View>
              ))}
              {tpLevels.length < 5 && (
                <TouchableOpacity style={styles.comboAdd} onPress={addTPLevel}>
                  <Text style={styles.comboAddText}>+ 添加级别</Text>
                </TouchableOpacity>
              )}
            </View>
          )}
        </View>
      )}

      {/* === 预估信息网格 === */}
      {tpslPreview && (
        <View style={styles.previewGrid}>
          {tpslPreview.slPrice && (
            <View style={styles.previewItem}>
              <Text style={styles.previewLabel}>止损价</Text>
              <Text style={[styles.previewValue, { color: colors.redLight }]}>{tpslPreview.slPrice}</Text>
            </View>
          )}
          {/* 单级止盈 */}
          {tpslPreview.tpPrice && !tpslPreview.tpLevelPreviews && (
            <View style={styles.previewItem}>
              <Text style={styles.previewLabel}>止盈价</Text>
              <Text style={[styles.previewValue, { color: colors.greenLight }]}>{tpslPreview.tpPrice}</Text>
            </View>
          )}
          {/* 阶梯止盈 */}
          {tpslPreview.tpLevelPreviews && tpslPreview.tpLevelPreviews.map((lv, idx) => (
            <View key={idx} style={styles.previewItem}>
              <Text style={styles.previewLabel}>TP{idx + 1} ({lv.percent}%)</Text>
              <Text style={[styles.previewValue, { color: colors.greenLight }]}>
                {lv.tpPrice} (+{lv.profit}U)
              </Text>
            </View>
          ))}
          <View style={styles.previewItem}>
            <Text style={styles.previewLabel}>强平价</Text>
            <Text style={[styles.previewValue, { color: colors.orange }]}>{tpslPreview.liqPrice}</Text>
          </View>
          {tpslPreview.tpProfit && (
            <View style={styles.previewItem}>
              <Text style={styles.previewLabel}>预计总盈利</Text>
              <Text style={[styles.previewValue, { color: colors.greenLight }]}>+{tpslPreview.tpProfit}</Text>
            </View>
          )}
          <View style={styles.previewItem}>
            <Text style={styles.previewLabel}>数量</Text>
            <Text style={styles.previewValue}>{tpslPreview.totalQty}</Text>
          </View>
          <View style={styles.previewItem}>
            <Text style={styles.previewLabel}>均价</Text>
            <Text style={styles.previewValue}>{tpslPreview.avgEntry}</Text>
          </View>
          {tpslPreview.effectiveLev && (
            <View style={styles.previewItem}>
              <Text style={styles.previewLabel}>实际杠杆</Text>
              <Text style={styles.previewValue}>{tpslPreview.effectiveLev}×</Text>
            </View>
          )}
          {tpslPreview.binanceLiqPrice && (
            <View style={styles.previewItem}>
              <Text style={styles.previewLabel}>当前强平</Text>
              <Text style={[styles.previewValue, { color: colors.orange }]}>{tpslPreview.binanceLiqPrice}</Text>
            </View>
          )}
        </View>
      )}

      {/* === 下单按钮 === */}
      <TouchableOpacity
        style={[styles.orderBtn, isBuy ? styles.orderBuy : styles.orderSell, loading && styles.disabled]}
        onPress={handleOrder}
        disabled={loading}
        activeOpacity={0.8}
      >
        {loading ? (
          <ActivityIndicator color="#fff" />
        ) : (
          <Text style={styles.orderBtnText}>
            {isBuy ? '买入做多' : '卖出做空'} {markPrice ? `@ ${markPrice}` : ''}
          </Text>
        )}
      </TouchableOpacity>

      {/* === 下单结果 === */}
      {result && (
        <View style={styles.resultBox}>
          <Text style={styles.resultText}>
            {result.order?.status} | 均价 {result.order?.avgPrice || result.order?.price}
          </Text>
          {result.takeProfits && result.takeProfits.length > 0 ? (
            result.takeProfits.map((tp, i) => (
              <Text key={i} style={[styles.resultText, { color: colors.greenLight }]}>
                TP{i + 1} {tp.triggerPrice}
              </Text>
            ))
          ) : result.takeProfit ? (
            <Text style={[styles.resultText, { color: colors.greenLight }]}>
              止盈 {result.takeProfit.triggerPrice}
            </Text>
          ) : null}
          {result.stopLoss && (
            <Text style={[styles.resultText, { color: colors.redLight }]}>
              止损 {result.stopLoss.triggerPrice}
            </Text>
          )}
        </View>
      )}

      {/* === 保存模板弹窗 === */}
      <Modal visible={savingTemplate} animationType="fade" transparent>
        <View style={styles.overlay}>
          <View style={styles.modal}>
            <Text style={styles.modalTitle}>保存下单模板</Text>
            <Text style={styles.modalSub}>
              {side === 'BUY' ? '做多' : '做空'} {quoteQty}U {leverage}×
              {stopLossAmount ? ` | SL ${stopLossAmount}U` : ''}
            </Text>
            <TextInput
              style={styles.tplNameInput}
              value={templateName}
              onChangeText={setTemplateName}
              placeholder="输入模板名称"
              placeholderTextColor={colors.textMuted}
              autoFocus
            />
            <View style={styles.modalActions}>
              <TouchableOpacity
                style={styles.cancelBtn}
                onPress={() => { setSavingTemplate(false); setTemplateName(''); }}
              >
                <Text style={styles.cancelText}>取消</Text>
              </TouchableOpacity>
              <TouchableOpacity style={styles.confirmBtn} onPress={handleSaveTemplate}>
                <Text style={styles.confirmText}>保存</Text>
              </TouchableOpacity>
            </View>
          </View>
        </View>
      </Modal>
    </View>
  );
}

const styles = StyleSheet.create({
  panel: {
    backgroundColor: colors.card,
    borderRadius: radius.lg,
    padding: spacing.lg,
    borderWidth: 1,
    borderColor: colors.cardBorder,
  },

  // 模板栏
  tplBar: {
    flexDirection: 'row',
    gap: spacing.sm,
    marginBottom: spacing.md,
  },
  tplBtn: {
    paddingHorizontal: spacing.md,
    paddingVertical: spacing.xs,
    borderRadius: radius.pill,
    backgroundColor: colors.surface,
    borderWidth: 1,
    borderColor: colors.cardBorder,
  },
  tplBtnText: {
    color: colors.textSecondary,
    fontSize: fontSize.xs,
    fontWeight: '600',
  },
  tplList: {
    marginBottom: spacing.md,
    maxHeight: 56,
  },
  tplChip: {
    backgroundColor: colors.goldBg,
    borderRadius: radius.md,
    paddingHorizontal: spacing.md,
    paddingVertical: spacing.sm,
    marginRight: spacing.sm,
    borderWidth: 1,
    borderColor: 'rgba(0,229,255,0.3)',
    minWidth: 80,
  },
  tplChipName: {
    color: colors.gold,
    fontSize: fontSize.sm,
    fontWeight: '700',
  },
  tplChipSub: {
    color: colors.textMuted,
    fontSize: fontSize.xs,
    marginTop: 2,
  },
  tplEmpty: {
    color: colors.textMuted,
    fontSize: fontSize.xs,
    textAlign: 'center',
    marginBottom: spacing.md,
  },

  // 方向
  sideRow: {
    flexDirection: 'row',
    marginBottom: spacing.lg,
  },
  sideBtn: {
    flex: 1,
    paddingVertical: spacing.md,
    alignItems: 'center',
    backgroundColor: 'rgba(15,25,35,0.6)',
    borderWidth: 1,
    borderColor: colors.cardBorder,
  },
  sideBtnBuy: {
    borderTopLeftRadius: radius.md,
    borderBottomLeftRadius: radius.md,
    borderTopRightRadius: 0,
    borderBottomRightRadius: 0,
  },
  sideBtnSell: {
    borderTopLeftRadius: 0,
    borderBottomLeftRadius: 0,
    borderTopRightRadius: radius.md,
    borderBottomRightRadius: radius.md,
    borderLeftWidth: 0,
  },
  buyActive: {
    backgroundColor: colors.green,
    borderColor: colors.green,
    shadowColor: colors.greenGlow,
    shadowRadius: 8,
    shadowOpacity: 1,
    elevation: 4,
  },
  sellActive: {
    backgroundColor: colors.red,
    borderColor: colors.red,
    shadowColor: colors.redGlow,
    shadowRadius: 8,
    shadowOpacity: 1,
    elevation: 4,
  },
  sideText: {
    color: colors.textMuted,
    fontWeight: '700',
    fontSize: fontSize.md,
  },
  sideTextActive: {
    color: colors.white,
  },

  // 字段输入
  fieldRow: {
    flexDirection: 'row',
    alignItems: 'center',
    backgroundColor: colors.surface,
    borderRadius: radius.lg,
    marginBottom: spacing.sm,
    paddingHorizontal: spacing.md,
  },
  fieldLabel: {
    color: colors.textSecondary,
    fontSize: fontSize.sm,
    fontWeight: '500',
    width: 36,
  },
  fieldInputWrap: {
    flex: 1,
    flexDirection: 'row',
    alignItems: 'center',
  },
  fieldInput: {
    flex: 1,
    color: colors.white,
    fontSize: fontSize.xl,
    fontWeight: '700',
    paddingVertical: spacing.md,
    fontVariant: ['tabular-nums'],
  },
  fieldUnit: {
    color: colors.textMuted,
    fontSize: fontSize.sm,
    fontWeight: '600',
    marginLeft: spacing.sm,
  },

  // 杠杆快选
  levRow: {
    flexDirection: 'row',
    gap: spacing.xs,
    marginBottom: spacing.lg,
  },
  levChip: {
    flex: 1,
    paddingVertical: 6,
    borderRadius: radius.pill,
    alignItems: 'center',
    backgroundColor: colors.surface,
  },
  levChipActive: {
    backgroundColor: colors.goldBg,
    borderWidth: 1,
    borderColor: colors.gold,
  },
  levChipText: {
    color: colors.textMuted,
    fontSize: fontSize.xs,
    fontWeight: '600',
  },
  levChipTextActive: {
    color: colors.gold,
    fontWeight: '700',
  },

  // TP/SL 折叠
  tpslToggle: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'center',
    paddingVertical: spacing.sm,
    borderTopWidth: 1,
    borderTopColor: colors.divider,
    marginBottom: spacing.sm,
  },
  tpslToggleText: {
    color: colors.textSecondary,
    fontSize: fontSize.sm,
    fontWeight: '600',
  },
  tpslToggleIcon: {
    color: colors.gold,
    fontSize: fontSize.lg,
    fontWeight: '600',
  },
  tpslToggleArrow: {
    color: colors.textMuted,
    fontSize: fontSize.sm,
  },
  tpslBox: {
    marginBottom: spacing.md,
  },
  presetTpslRow: {
    flexDirection: 'row',
    alignItems: 'flex-end',
    gap: spacing.sm,
    marginBottom: spacing.sm,
    backgroundColor: colors.surface,
    borderRadius: radius.md,
    padding: spacing.sm,
    borderWidth: 1,
    borderColor: colors.gold + '33',
  },
  presetTpslItem: {
    flex: 1,
  },
  presetTpslLabel: {
    fontSize: fontSize.xs,
    color: colors.gold,
    fontWeight: '600',
    marginBottom: 3,
  },
  presetClearBtn: {
    paddingHorizontal: spacing.sm,
    paddingVertical: spacing.xs,
    borderRadius: radius.sm,
    borderWidth: 1,
    borderColor: colors.cardBorder,
  },
  presetClearText: {
    fontSize: fontSize.xs,
    color: colors.textMuted,
    fontWeight: '600',
  },
  tpslInputRow: {
    flexDirection: 'row',
    gap: spacing.sm,
  },
  tpslInputGroup: {
    flex: 1,
  },
  tpslLabel: {
    color: colors.textMuted,
    fontSize: fontSize.xs,
    marginBottom: spacing.xs,
  },
  tpslInput: {
    backgroundColor: colors.surface,
    borderRadius: radius.sm,
    padding: spacing.sm,
    color: colors.white,
    fontSize: fontSize.md,
    borderWidth: 1,
    borderColor: colors.cardBorder,
  },

  // 阶梯止盈
  comboToggle: {
    flexDirection: 'row',
    alignItems: 'center',
    marginTop: spacing.md,
    gap: spacing.sm,
  },
  comboCheck: {
    width: 20,
    height: 20,
    borderRadius: 4,
    borderWidth: 1.5,
    borderColor: colors.textMuted,
    alignItems: 'center',
    justifyContent: 'center',
  },
  comboCheckActive: {
    borderColor: colors.gold,
    backgroundColor: colors.goldBg,
  },
  comboCheckMark: {
    color: colors.gold,
    fontSize: 12,
    fontWeight: '800',
  },
  comboToggleText: {
    color: colors.textSecondary,
    fontSize: fontSize.sm,
    fontWeight: '600',
  },
  comboHint: {
    color: colors.textMuted,
    fontSize: fontSize.xs,
    marginLeft: 'auto',
  },
  comboBox: {
    marginTop: spacing.sm,
    padding: spacing.sm,
    backgroundColor: colors.surface,
    borderRadius: radius.md,
    borderWidth: 1,
    borderColor: colors.cardBorder,
  },
  comboRow: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: spacing.xs,
    marginBottom: spacing.xs,
  },
  comboIdx: {
    color: colors.gold,
    fontSize: fontSize.xs,
    fontWeight: '700',
    width: 22,
  },
  comboField: {
    flex: 1,
  },
  comboFieldLabel: {
    color: colors.textMuted,
    fontSize: 10,
    marginBottom: 2,
  },
  comboInput: {
    backgroundColor: colors.card,
    borderRadius: radius.sm,
    padding: spacing.xs,
    color: colors.white,
    fontSize: fontSize.sm,
    borderWidth: 1,
    borderColor: colors.cardBorder,
    textAlign: 'center',
  },
  comboRemove: {
    width: 24,
    height: 24,
    borderRadius: 12,
    backgroundColor: 'rgba(255,59,92,0.2)',
    alignItems: 'center',
    justifyContent: 'center',
    marginTop: 14,
  },
  comboRemoveText: {
    color: colors.redLight,
    fontSize: 12,
    fontWeight: '700',
  },
  comboAdd: {
    paddingVertical: spacing.xs,
    alignItems: 'center',
    marginTop: spacing.xs,
  },
  comboAddText: {
    color: colors.gold,
    fontSize: fontSize.xs,
    fontWeight: '600',
  },

  // 预估信息
  previewGrid: {
    flexDirection: 'row',
    flexWrap: 'wrap',
    backgroundColor: colors.surface,
    borderRadius: radius.md,
    padding: spacing.md,
    marginBottom: spacing.md,
    gap: spacing.xs,
    borderWidth: 1,
    borderColor: colors.cardBorder,
  },
  previewItem: {
    width: '48%',
    paddingVertical: spacing.xs,
  },
  previewLabel: {
    color: colors.textMuted,
    fontSize: fontSize.xs,
    marginBottom: 2,
  },
  previewValue: {
    color: colors.text,
    fontSize: fontSize.md,
    fontWeight: '700',
    fontVariant: ['tabular-nums'],
  },

  // 下单按钮
  orderBtn: {
    paddingVertical: spacing.xl,
    borderRadius: radius.lg,
    alignItems: 'center',
  },
  orderBuy: {
    backgroundColor: colors.green,
    shadowColor: colors.greenGlow,
    shadowRadius: 8,
    shadowOpacity: 0.6,
    elevation: 4,
  },
  orderSell: {
    backgroundColor: colors.red,
    shadowColor: colors.redGlow,
    shadowRadius: 8,
    shadowOpacity: 0.6,
    elevation: 4,
  },
  disabled: { opacity: 0.5 },
  orderBtnText: {
    color: colors.white,
    fontSize: fontSize.xl,
    fontWeight: '800',
  },

  // 结果
  resultBox: {
    marginTop: spacing.sm,
    padding: spacing.md,
    backgroundColor: colors.surface,
    borderRadius: radius.md,
    gap: spacing.xs,
  },
  resultText: {
    color: colors.text,
    fontSize: fontSize.sm,
  },

  // 弹窗
  overlay: {
    flex: 1,
    backgroundColor: 'rgba(0,0,0,0.75)',
    justifyContent: 'center',
    alignItems: 'center',
    padding: spacing.xl,
  },
  modal: {
    backgroundColor: colors.card,
    borderRadius: radius.xxl,
    borderWidth: 1,
    borderColor: colors.cardBorder,
    padding: spacing.xl,
    width: '100%',
    maxWidth: 400,
  },
  modalTitle: {
    fontSize: fontSize.lg,
    fontWeight: '700',
    color: colors.white,
    textAlign: 'center',
    marginBottom: spacing.xs,
  },
  modalSub: {
    fontSize: fontSize.sm,
    color: colors.textSecondary,
    textAlign: 'center',
    marginBottom: spacing.lg,
  },
  tplNameInput: {
    backgroundColor: colors.surface,
    borderRadius: radius.md,
    padding: spacing.md,
    color: colors.white,
    fontSize: fontSize.md,
    marginBottom: spacing.lg,
    borderWidth: 1,
    borderColor: colors.cardBorder,
  },
  modalActions: {
    flexDirection: 'row',
    gap: spacing.md,
  },
  cancelBtn: {
    flex: 1,
    paddingVertical: spacing.md,
    borderRadius: radius.md,
    alignItems: 'center',
    backgroundColor: colors.surface,
  },
  cancelText: {
    color: colors.textSecondary,
    fontWeight: '600',
    fontSize: fontSize.md,
  },
  confirmBtn: {
    flex: 1,
    paddingVertical: spacing.md,
    borderRadius: radius.md,
    alignItems: 'center',
    backgroundColor: colors.gold,
  },
  confirmText: {
    color: colors.bg,
    fontWeight: '700',
    fontSize: fontSize.md,
  },
});
