import React, { useState, useEffect, useCallback } from 'react';
import {
  View,
  Text,
  TextInput,
  TouchableOpacity,
  ScrollView,
  StyleSheet,
  Alert,
  ActivityIndicator,
  RefreshControl,
  Modal,
  Switch,
} from 'react-native';

// ========== 配置 ==========
const API_BASE = 'http://127.0.0.1:10088/tool';

// 常用交易对
const SYMBOLS = [
  'BTCUSDT', 'ETHUSDT', 'BNBUSDT', 'SOLUSDT', 'XRPUSDT',
  'DOGEUSDT', 'ADAUSDT', 'AVAXUSDT', 'DOTUSDT', 'LINKUSDT',
];

// ========== API 调用 ==========
async function apiCall(method, path, body = null) {
  const options = {
    method,
    headers: { 'Content-Type': 'application/json' },
  };
  if (body) options.body = JSON.stringify(body);

  const res = await fetch(`${API_BASE}${path}`, options);
  const data = await res.json();
  if (data.error) throw new Error(data.error);
  return data;
}

const api = {
  getPositions: () => apiCall('GET', '/positions'),
  placeOrder: (req) => apiCall('POST', '/order', req),
  closePosition: (req) => apiCall('POST', '/close', req),
  getOrders: (symbol) => apiCall('GET', `/orders?symbol=${symbol || ''}`),
  cancelOrder: (symbol, orderId) => apiCall('DELETE', `/order?symbol=${symbol}&orderId=${orderId}`),
  startAutoScale: (config) => apiCall('POST', '/autoscale/start', config),
  stopAutoScale: (symbol) => apiCall('POST', '/autoscale/stop', { symbol }),
  autoScaleStatus: (symbol) => apiCall('GET', `/autoscale/status?symbol=${symbol}`),
};

// ========== 交易对选择器 ==========
function SymbolPicker({ selected, onSelect }) {
  const [custom, setCustom] = useState('');

  return (
    <View style={styles.symbolPicker}>
      <Text style={styles.label}>交易对</Text>
      <ScrollView horizontal showsHorizontalScrollIndicator={false} style={styles.symbolScroll}>
        {SYMBOLS.map((s) => (
          <TouchableOpacity
            key={s}
            style={[styles.symbolChip, selected === s && styles.symbolChipActive]}
            onPress={() => onSelect(s)}
          >
            <Text style={[styles.symbolChipText, selected === s && styles.symbolChipTextActive]}>
              {s.replace('USDT', '')}
            </Text>
          </TouchableOpacity>
        ))}
      </ScrollView>
      <View style={styles.customSymbolRow}>
        <TextInput
          style={styles.customSymbolInput}
          placeholder="自定义交易对 如 PEPEUSDT"
          placeholderTextColor="#666"
          value={custom}
          onChangeText={setCustom}
          autoCapitalize="characters"
        />
        <TouchableOpacity
          style={styles.customSymbolBtn}
          onPress={() => {
            if (custom.trim()) onSelect(custom.trim().toUpperCase());
          }}
        >
          <Text style={styles.customSymbolBtnText}>确定</Text>
        </TouchableOpacity>
      </View>
    </View>
  );
}

// ========== 下单面板 ==========
function OrderPanel({ symbol }) {
  const [side, setSide] = useState('BUY');
  const [quoteQty, setQuoteQty] = useState('5');
  const [leverage, setLeverage] = useState('10');
  const [stopLossAmount, setStopLossAmount] = useState('');
  const [riskReward, setRiskReward] = useState('');
  const [loading, setLoading] = useState(false);
  const [result, setResult] = useState(null);

  const handleOrder = async () => {
    if (!symbol) return Alert.alert('请选择交易对');
    if (!quoteQty) return Alert.alert('请输入下单金额');
    if (!leverage) return Alert.alert('请输入杠杆倍数');

    const req = {
      symbol,
      side,
      orderType: 'MARKET',
      quoteQuantity: quoteQty,
      leverage: parseInt(leverage, 10),
    };

    // 止盈止损（可选）
    if (stopLossAmount && riskReward) {
      req.stopLossAmount = parseFloat(stopLossAmount);
      req.riskReward = parseFloat(riskReward);
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

  return (
    <View style={styles.panel}>
      <Text style={styles.panelTitle}>下单</Text>

      {/* 方向 */}
      <View style={styles.sideRow}>
        <TouchableOpacity
          style={[styles.sideBtn, styles.buyBtn, side === 'BUY' && styles.buyBtnActive]}
          onPress={() => setSide('BUY')}
        >
          <Text style={[styles.sideBtnText, side === 'BUY' && styles.sideBtnTextActive]}>
            做多 BUY
          </Text>
        </TouchableOpacity>
        <TouchableOpacity
          style={[styles.sideBtn, styles.sellBtn, side === 'SELL' && styles.sellBtnActive]}
          onPress={() => setSide('SELL')}
        >
          <Text style={[styles.sideBtnText, side === 'SELL' && styles.sideBtnTextActive]}>
            做空 SELL
          </Text>
        </TouchableOpacity>
      </View>

      {/* 金额和杠杆 */}
      <View style={styles.inputRow}>
        <View style={styles.inputGroup}>
          <Text style={styles.inputLabel}>金额 (USDT)</Text>
          <TextInput
            style={styles.input}
            value={quoteQty}
            onChangeText={setQuoteQty}
            keyboardType="decimal-pad"
            placeholder="5"
            placeholderTextColor="#666"
          />
        </View>
        <View style={styles.inputGroup}>
          <Text style={styles.inputLabel}>杠杆</Text>
          <TextInput
            style={styles.input}
            value={leverage}
            onChangeText={setLeverage}
            keyboardType="number-pad"
            placeholder="10"
            placeholderTextColor="#666"
          />
        </View>
      </View>

      {/* 快捷杠杆 */}
      <View style={styles.leverageRow}>
        {['5', '10', '20', '50', '100'].map((lev) => (
          <TouchableOpacity
            key={lev}
            style={[styles.leverageChip, leverage === lev && styles.leverageChipActive]}
            onPress={() => setLeverage(lev)}
          >
            <Text style={[styles.leverageChipText, leverage === lev && styles.leverageChipTextActive]}>
              {lev}x
            </Text>
          </TouchableOpacity>
        ))}
      </View>

      {/* 止盈止损 */}
      <Text style={styles.subTitle}>止盈止损（可选）</Text>
      <View style={styles.inputRow}>
        <View style={styles.inputGroup}>
          <Text style={styles.inputLabel}>止损金额 (U)</Text>
          <TextInput
            style={styles.input}
            value={stopLossAmount}
            onChangeText={setStopLossAmount}
            keyboardType="decimal-pad"
            placeholder="如 1"
            placeholderTextColor="#666"
          />
        </View>
        <View style={styles.inputGroup}>
          <Text style={styles.inputLabel}>盈亏比</Text>
          <TextInput
            style={styles.input}
            value={riskReward}
            onChangeText={setRiskReward}
            keyboardType="decimal-pad"
            placeholder="如 3 (1:3)"
            placeholderTextColor="#666"
          />
        </View>
      </View>

      {/* 下单按钮 */}
      <TouchableOpacity
        style={[
          styles.orderBtn,
          side === 'BUY' ? styles.orderBtnBuy : styles.orderBtnSell,
          loading && styles.orderBtnDisabled,
        ]}
        onPress={handleOrder}
        disabled={loading}
      >
        {loading ? (
          <ActivityIndicator color="#fff" />
        ) : (
          <Text style={styles.orderBtnText}>
            {side === 'BUY' ? '做多' : '做空'} {symbol || '---'} / 市价
          </Text>
        )}
      </TouchableOpacity>

      {/* 结果 */}
      {result && (
        <View style={styles.resultBox}>
          <Text style={styles.resultText}>
            主单: {result.order?.status} | 均价: {result.order?.avgPrice || result.order?.price}
          </Text>
          {result.takeProfit && (
            <Text style={styles.resultTextGreen}>
              止盈: {result.takeProfit.triggerPrice} (algoId: {result.takeProfit.algoId})
            </Text>
          )}
          {result.stopLoss && (
            <Text style={styles.resultTextRed}>
              止损: {result.stopLoss.triggerPrice} (algoId: {result.stopLoss.algoId})
            </Text>
          )}
        </View>
      )}
    </View>
  );
}

// ========== 持仓列表 + 平仓 ==========
function PositionPanel({ symbol }) {
  const [positions, setPositions] = useState([]);
  const [loading, setLoading] = useState(false);
  const [refreshing, setRefreshing] = useState(false);

  const fetchPositions = useCallback(async () => {
    try {
      const data = await api.getPositions();
      setPositions(data.data || []);
    } catch (e) {
      console.log('fetch positions error:', e.message);
    }
  }, []);

  useEffect(() => {
    fetchPositions();
    const timer = setInterval(fetchPositions, 5000);
    return () => clearInterval(timer);
  }, [fetchPositions]);

  const onRefresh = async () => {
    setRefreshing(true);
    await fetchPositions();
    setRefreshing(false);
  };

  const handleClose = async (pos) => {
    Alert.alert(
      '确认平仓',
      `${pos.symbol} ${parseFloat(pos.positionAmt) > 0 ? '多' : '空'} ${Math.abs(parseFloat(pos.positionAmt))} 个`,
      [
        { text: '取消', style: 'cancel' },
        {
          text: '确认平仓',
          style: 'destructive',
          onPress: async () => {
            setLoading(true);
            try {
              await api.closePosition({
                symbol: pos.symbol,
                positionSide: pos.positionSide || '',
              });
              Alert.alert('平仓成功');
              fetchPositions();
            } catch (e) {
              Alert.alert('平仓失败', e.message);
            } finally {
              setLoading(false);
            }
          },
        },
      ]
    );
  };

  const filteredPositions = symbol
    ? positions.filter((p) => p.symbol === symbol)
    : positions;

  return (
    <View style={styles.panel}>
      <View style={styles.panelHeader}>
        <Text style={styles.panelTitle}>持仓 ({filteredPositions.length})</Text>
        <TouchableOpacity onPress={fetchPositions}>
          <Text style={styles.refreshText}>刷新</Text>
        </TouchableOpacity>
      </View>

      {filteredPositions.length === 0 ? (
        <Text style={styles.emptyText}>暂无持仓</Text>
      ) : (
        filteredPositions.map((pos, idx) => {
          const amt = parseFloat(pos.positionAmt);
          const pnl = parseFloat(pos.unRealizedProfit);
          const isLong = amt > 0;

          return (
            <View key={`${pos.symbol}-${pos.positionSide}-${idx}`} style={styles.positionCard}>
              <View style={styles.positionHeader}>
                <View style={styles.positionInfo}>
                  <Text style={styles.positionSymbol}>{pos.symbol}</Text>
                  <View style={[styles.directionBadge, isLong ? styles.badgeLong : styles.badgeShort]}>
                    <Text style={styles.badgeText}>{isLong ? '多' : '空'} {pos.leverage}x</Text>
                  </View>
                </View>
                <Text style={[styles.pnlText, pnl >= 0 ? styles.pnlGreen : styles.pnlRed]}>
                  {pnl >= 0 ? '+' : ''}{pnl.toFixed(4)} U
                </Text>
              </View>

              <View style={styles.positionDetails}>
                <Text style={styles.detailText}>数量: {Math.abs(amt)}</Text>
                <Text style={styles.detailText}>开仓价: {parseFloat(pos.entryPrice).toFixed(2)}</Text>
                <Text style={styles.detailText}>标记价: {parseFloat(pos.markPrice).toFixed(2)}</Text>
              </View>

              <TouchableOpacity
                style={styles.closeBtn}
                onPress={() => handleClose(pos)}
                disabled={loading}
              >
                <Text style={styles.closeBtnText}>平仓</Text>
              </TouchableOpacity>
            </View>
          );
        })
      )}
    </View>
  );
}

// ========== 浮盈加仓面板 ==========
function AutoScalePanel({ symbol }) {
  const [showModal, setShowModal] = useState(false);
  const [status, setStatus] = useState(null);
  const [config, setConfig] = useState({
    side: 'BUY',
    leverage: '10',
    triggerType: 'amount',
    triggerAmount: '2',
    triggerPercent: '2',
    addQuantity: '5',
    maxScaleCount: '3',
    updateTPSL: false,
    stopLossAmount: '1',
    riskReward: '3',
  });

  const fetchStatus = useCallback(async () => {
    if (!symbol) return;
    try {
      const data = await api.autoScaleStatus(symbol);
      setStatus(data.data);
    } catch (e) {
      setStatus(null);
    }
  }, [symbol]);

  useEffect(() => {
    fetchStatus();
    const timer = setInterval(fetchStatus, 5000);
    return () => clearInterval(timer);
  }, [fetchStatus]);

  const handleStart = async () => {
    const req = {
      symbol,
      side: config.side,
      leverage: parseInt(config.leverage, 10),
      addQuantity: config.addQuantity,
      maxScaleCount: parseInt(config.maxScaleCount, 10),
      updateTPSL: config.updateTPSL,
    };
    if (config.triggerType === 'amount') {
      req.triggerAmount = parseFloat(config.triggerAmount);
    } else {
      req.triggerPercent = parseFloat(config.triggerPercent);
    }
    if (config.updateTPSL) {
      req.stopLossAmount = parseFloat(config.stopLossAmount);
      req.riskReward = parseFloat(config.riskReward);
    }

    try {
      await api.startAutoScale(req);
      Alert.alert('浮盈加仓已开启');
      setShowModal(false);
      fetchStatus();
    } catch (e) {
      Alert.alert('开启失败', e.message);
    }
  };

  const handleStop = async () => {
    try {
      await api.stopAutoScale(symbol);
      Alert.alert('浮盈加仓已关闭');
      fetchStatus();
    } catch (e) {
      Alert.alert('关闭失败', e.message);
    }
  };

  return (
    <View style={styles.panel}>
      <View style={styles.panelHeader}>
        <Text style={styles.panelTitle}>浮盈加仓</Text>
        {status?.active ? (
          <TouchableOpacity style={styles.stopScaleBtn} onPress={handleStop}>
            <Text style={styles.stopScaleBtnText}>关闭</Text>
          </TouchableOpacity>
        ) : (
          <TouchableOpacity
            style={styles.startScaleBtn}
            onPress={() => {
              if (!symbol) return Alert.alert('请先选择交易对');
              setShowModal(true);
            }}
          >
            <Text style={styles.startScaleBtnText}>开启</Text>
          </TouchableOpacity>
        )}
      </View>

      {status?.active && (
        <View style={styles.scaleStatus}>
          <Text style={styles.scaleStatusText}>
            {status.config.symbol} | 已加仓 {status.scaleCount}/{status.config.maxScaleCount} 次 | 累计 {status.totalAdded.toFixed(2)} U
          </Text>
        </View>
      )}

      {/* 配置弹窗 */}
      <Modal visible={showModal} animationType="slide" transparent>
        <View style={styles.modalOverlay}>
          <View style={styles.modalContent}>
            <Text style={styles.modalTitle}>浮盈加仓配置 - {symbol}</Text>

            {/* 方向 */}
            <View style={styles.sideRow}>
              <TouchableOpacity
                style={[styles.sideBtn, styles.buyBtn, config.side === 'BUY' && styles.buyBtnActive]}
                onPress={() => setConfig({ ...config, side: 'BUY' })}
              >
                <Text style={[styles.sideBtnText, config.side === 'BUY' && styles.sideBtnTextActive]}>多</Text>
              </TouchableOpacity>
              <TouchableOpacity
                style={[styles.sideBtn, styles.sellBtn, config.side === 'SELL' && styles.sellBtnActive]}
                onPress={() => setConfig({ ...config, side: 'SELL' })}
              >
                <Text style={[styles.sideBtnText, config.side === 'SELL' && styles.sideBtnTextActive]}>空</Text>
              </TouchableOpacity>
            </View>

            {/* 触发条件 */}
            <View style={styles.sideRow}>
              <TouchableOpacity
                style={[styles.sideBtn, config.triggerType === 'amount' && styles.leverageChipActive]}
                onPress={() => setConfig({ ...config, triggerType: 'amount' })}
              >
                <Text style={[styles.sideBtnText, config.triggerType === 'amount' && styles.sideBtnTextActive]}>
                  按金额
                </Text>
              </TouchableOpacity>
              <TouchableOpacity
                style={[styles.sideBtn, config.triggerType === 'percent' && styles.leverageChipActive]}
                onPress={() => setConfig({ ...config, triggerType: 'percent' })}
              >
                <Text style={[styles.sideBtnText, config.triggerType === 'percent' && styles.sideBtnTextActive]}>
                  按百分比
                </Text>
              </TouchableOpacity>
            </View>

            {config.triggerType === 'amount' ? (
              <View style={styles.modalInputRow}>
                <Text style={styles.inputLabel}>每浮盈 (USDT)</Text>
                <TextInput
                  style={styles.input}
                  value={config.triggerAmount}
                  onChangeText={(v) => setConfig({ ...config, triggerAmount: v })}
                  keyboardType="decimal-pad"
                  placeholderTextColor="#666"
                />
              </View>
            ) : (
              <View style={styles.modalInputRow}>
                <Text style={styles.inputLabel}>每浮盈 (%)</Text>
                <TextInput
                  style={styles.input}
                  value={config.triggerPercent}
                  onChangeText={(v) => setConfig({ ...config, triggerPercent: v })}
                  keyboardType="decimal-pad"
                  placeholderTextColor="#666"
                />
              </View>
            )}

            <View style={styles.inputRow}>
              <View style={styles.inputGroup}>
                <Text style={styles.inputLabel}>加仓金额 (U)</Text>
                <TextInput
                  style={styles.input}
                  value={config.addQuantity}
                  onChangeText={(v) => setConfig({ ...config, addQuantity: v })}
                  keyboardType="decimal-pad"
                  placeholderTextColor="#666"
                />
              </View>
              <View style={styles.inputGroup}>
                <Text style={styles.inputLabel}>最大次数</Text>
                <TextInput
                  style={styles.input}
                  value={config.maxScaleCount}
                  onChangeText={(v) => setConfig({ ...config, maxScaleCount: v })}
                  keyboardType="number-pad"
                  placeholderTextColor="#666"
                />
              </View>
            </View>

            <View style={styles.modalInputRow}>
              <Text style={styles.inputLabel}>杠杆</Text>
              <TextInput
                style={styles.input}
                value={config.leverage}
                onChangeText={(v) => setConfig({ ...config, leverage: v })}
                keyboardType="number-pad"
                placeholderTextColor="#666"
              />
            </View>

            {/* 更新止盈止损 */}
            <View style={styles.switchRow}>
              <Text style={styles.inputLabel}>加仓后更新止盈止损</Text>
              <Switch
                value={config.updateTPSL}
                onValueChange={(v) => setConfig({ ...config, updateTPSL: v })}
              />
            </View>

            {config.updateTPSL && (
              <View style={styles.inputRow}>
                <View style={styles.inputGroup}>
                  <Text style={styles.inputLabel}>止损 (U)</Text>
                  <TextInput
                    style={styles.input}
                    value={config.stopLossAmount}
                    onChangeText={(v) => setConfig({ ...config, stopLossAmount: v })}
                    keyboardType="decimal-pad"
                    placeholderTextColor="#666"
                  />
                </View>
                <View style={styles.inputGroup}>
                  <Text style={styles.inputLabel}>盈亏比</Text>
                  <TextInput
                    style={styles.input}
                    value={config.riskReward}
                    onChangeText={(v) => setConfig({ ...config, riskReward: v })}
                    keyboardType="decimal-pad"
                    placeholderTextColor="#666"
                  />
                </View>
              </View>
            )}

            {/* 按钮 */}
            <View style={styles.modalBtnRow}>
              <TouchableOpacity
                style={styles.modalCancelBtn}
                onPress={() => setShowModal(false)}
              >
                <Text style={styles.modalCancelBtnText}>取消</Text>
              </TouchableOpacity>
              <TouchableOpacity style={styles.modalConfirmBtn} onPress={handleStart}>
                <Text style={styles.modalConfirmBtnText}>开启</Text>
              </TouchableOpacity>
            </View>
          </View>
        </View>
      </Modal>
    </View>
  );
}

// ========== 主页面 ==========
export default function TradingApp() {
  const [symbol, setSymbol] = useState('ETHUSDT');

  return (
    <ScrollView style={styles.container} contentContainerStyle={styles.contentContainer}>
      <Text style={styles.title}>合约交易</Text>

      <SymbolPicker selected={symbol} onSelect={setSymbol} />
      <OrderPanel symbol={symbol} />
      <PositionPanel symbol={symbol} />
      <AutoScalePanel symbol={symbol} />

      <View style={{ height: 50 }} />
    </ScrollView>
  );
}

// ========== 样式 ==========
const styles = StyleSheet.create({
  container: {
    flex: 1,
    backgroundColor: '#0d1117',
  },
  contentContainer: {
    padding: 16,
  },
  title: {
    fontSize: 24,
    fontWeight: 'bold',
    color: '#fff',
    marginBottom: 16,
    marginTop: 48,
  },

  // Symbol Picker
  symbolPicker: {
    marginBottom: 16,
  },
  label: {
    color: '#8b949e',
    fontSize: 14,
    marginBottom: 8,
  },
  symbolScroll: {
    marginBottom: 8,
  },
  symbolChip: {
    paddingHorizontal: 14,
    paddingVertical: 8,
    borderRadius: 8,
    backgroundColor: '#161b22',
    marginRight: 8,
    borderWidth: 1,
    borderColor: '#30363d',
  },
  symbolChipActive: {
    backgroundColor: '#1f6feb',
    borderColor: '#1f6feb',
  },
  symbolChipText: {
    color: '#8b949e',
    fontSize: 13,
    fontWeight: '600',
  },
  symbolChipTextActive: {
    color: '#fff',
  },
  customSymbolRow: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 8,
  },
  customSymbolInput: {
    flex: 1,
    backgroundColor: '#161b22',
    borderRadius: 8,
    padding: 10,
    color: '#fff',
    borderWidth: 1,
    borderColor: '#30363d',
  },
  customSymbolBtn: {
    backgroundColor: '#30363d',
    paddingHorizontal: 16,
    paddingVertical: 10,
    borderRadius: 8,
  },
  customSymbolBtnText: {
    color: '#fff',
    fontWeight: '600',
  },

  // Panel
  panel: {
    backgroundColor: '#161b22',
    borderRadius: 12,
    padding: 16,
    marginBottom: 16,
    borderWidth: 1,
    borderColor: '#30363d',
  },
  panelHeader: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'center',
    marginBottom: 12,
  },
  panelTitle: {
    fontSize: 18,
    fontWeight: 'bold',
    color: '#fff',
    marginBottom: 12,
  },
  subTitle: {
    fontSize: 14,
    color: '#8b949e',
    marginBottom: 8,
    marginTop: 4,
  },

  // Side buttons
  sideRow: {
    flexDirection: 'row',
    gap: 8,
    marginBottom: 12,
  },
  sideBtn: {
    flex: 1,
    paddingVertical: 10,
    borderRadius: 8,
    alignItems: 'center',
    backgroundColor: '#21262d',
    borderWidth: 1,
    borderColor: '#30363d',
  },
  buyBtn: {},
  buyBtnActive: {
    backgroundColor: '#238636',
    borderColor: '#238636',
  },
  sellBtn: {},
  sellBtnActive: {
    backgroundColor: '#da3633',
    borderColor: '#da3633',
  },
  sideBtnText: {
    color: '#8b949e',
    fontWeight: '600',
    fontSize: 15,
  },
  sideBtnTextActive: {
    color: '#fff',
  },

  // Input
  inputRow: {
    flexDirection: 'row',
    gap: 8,
    marginBottom: 12,
  },
  inputGroup: {
    flex: 1,
  },
  inputLabel: {
    color: '#8b949e',
    fontSize: 12,
    marginBottom: 4,
  },
  input: {
    backgroundColor: '#0d1117',
    borderRadius: 8,
    padding: 10,
    color: '#fff',
    borderWidth: 1,
    borderColor: '#30363d',
    fontSize: 15,
  },

  // Leverage chips
  leverageRow: {
    flexDirection: 'row',
    gap: 6,
    marginBottom: 12,
  },
  leverageChip: {
    flex: 1,
    paddingVertical: 6,
    borderRadius: 6,
    alignItems: 'center',
    backgroundColor: '#21262d',
    borderWidth: 1,
    borderColor: '#30363d',
  },
  leverageChipActive: {
    backgroundColor: '#1f6feb',
    borderColor: '#1f6feb',
  },
  leverageChipText: {
    color: '#8b949e',
    fontSize: 12,
    fontWeight: '600',
  },
  leverageChipTextActive: {
    color: '#fff',
  },

  // Order button
  orderBtn: {
    paddingVertical: 14,
    borderRadius: 10,
    alignItems: 'center',
    marginTop: 4,
  },
  orderBtnBuy: {
    backgroundColor: '#238636',
  },
  orderBtnSell: {
    backgroundColor: '#da3633',
  },
  orderBtnDisabled: {
    opacity: 0.6,
  },
  orderBtnText: {
    color: '#fff',
    fontSize: 16,
    fontWeight: 'bold',
  },

  // Result
  resultBox: {
    marginTop: 12,
    padding: 10,
    backgroundColor: '#0d1117',
    borderRadius: 8,
  },
  resultText: {
    color: '#c9d1d9',
    fontSize: 13,
  },
  resultTextGreen: {
    color: '#3fb950',
    fontSize: 13,
    marginTop: 4,
  },
  resultTextRed: {
    color: '#f85149',
    fontSize: 13,
    marginTop: 4,
  },

  // Position
  positionCard: {
    backgroundColor: '#0d1117',
    borderRadius: 10,
    padding: 12,
    marginBottom: 10,
    borderWidth: 1,
    borderColor: '#30363d',
  },
  positionHeader: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'center',
    marginBottom: 8,
  },
  positionInfo: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 8,
  },
  positionSymbol: {
    color: '#fff',
    fontSize: 16,
    fontWeight: 'bold',
  },
  directionBadge: {
    paddingHorizontal: 8,
    paddingVertical: 2,
    borderRadius: 4,
  },
  badgeLong: {
    backgroundColor: 'rgba(35,134,54,0.3)',
  },
  badgeShort: {
    backgroundColor: 'rgba(218,54,51,0.3)',
  },
  badgeText: {
    color: '#fff',
    fontSize: 11,
    fontWeight: '600',
  },
  pnlText: {
    fontSize: 16,
    fontWeight: 'bold',
  },
  pnlGreen: {
    color: '#3fb950',
  },
  pnlRed: {
    color: '#f85149',
  },
  positionDetails: {
    flexDirection: 'row',
    gap: 12,
    marginBottom: 10,
  },
  detailText: {
    color: '#8b949e',
    fontSize: 12,
  },
  closeBtn: {
    backgroundColor: '#da3633',
    paddingVertical: 8,
    borderRadius: 8,
    alignItems: 'center',
  },
  closeBtnText: {
    color: '#fff',
    fontWeight: 'bold',
    fontSize: 14,
  },

  // Refresh
  refreshText: {
    color: '#1f6feb',
    fontSize: 14,
  },
  emptyText: {
    color: '#484f58',
    textAlign: 'center',
    paddingVertical: 20,
  },

  // AutoScale
  startScaleBtn: {
    backgroundColor: '#238636',
    paddingHorizontal: 16,
    paddingVertical: 6,
    borderRadius: 6,
  },
  startScaleBtnText: {
    color: '#fff',
    fontWeight: '600',
    fontSize: 13,
  },
  stopScaleBtn: {
    backgroundColor: '#da3633',
    paddingHorizontal: 16,
    paddingVertical: 6,
    borderRadius: 6,
  },
  stopScaleBtnText: {
    color: '#fff',
    fontWeight: '600',
    fontSize: 13,
  },
  scaleStatus: {
    backgroundColor: '#0d1117',
    padding: 10,
    borderRadius: 8,
  },
  scaleStatusText: {
    color: '#3fb950',
    fontSize: 13,
  },

  // Modal
  modalOverlay: {
    flex: 1,
    backgroundColor: 'rgba(0,0,0,0.7)',
    justifyContent: 'flex-end',
  },
  modalContent: {
    backgroundColor: '#161b22',
    borderTopLeftRadius: 20,
    borderTopRightRadius: 20,
    padding: 20,
    maxHeight: '85%',
  },
  modalTitle: {
    fontSize: 18,
    fontWeight: 'bold',
    color: '#fff',
    marginBottom: 16,
    textAlign: 'center',
  },
  modalInputRow: {
    marginBottom: 12,
  },
  switchRow: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'center',
    marginBottom: 12,
  },
  modalBtnRow: {
    flexDirection: 'row',
    gap: 12,
    marginTop: 8,
  },
  modalCancelBtn: {
    flex: 1,
    paddingVertical: 12,
    borderRadius: 10,
    alignItems: 'center',
    backgroundColor: '#21262d',
    borderWidth: 1,
    borderColor: '#30363d',
  },
  modalCancelBtnText: {
    color: '#8b949e',
    fontWeight: '600',
    fontSize: 15,
  },
  modalConfirmBtn: {
    flex: 1,
    paddingVertical: 12,
    borderRadius: 10,
    alignItems: 'center',
    backgroundColor: '#238636',
  },
  modalConfirmBtnText: {
    color: '#fff',
    fontWeight: '600',
    fontSize: 15,
  },
});
