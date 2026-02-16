import React, { useState, useEffect, useCallback, useMemo } from 'react';
import {
  View,
  Text,
  StyleSheet,
  TouchableOpacity,
  ActivityIndicator,
  ScrollView,
} from 'react-native';
import api from '../services/api';
import { colors } from '../services/theme';

const WEEKDAYS = ['一', '二', '三', '四', '五', '六', '日'];

// 获取某月的天数
function getDaysInMonth(year, month) {
  return new Date(year, month + 1, 0).getDate();
}

// 获取某月第一天是周几 (0=周日 -> 转为 周一=0)
function getFirstDayOfMonth(year, month) {
  const day = new Date(year, month, 1).getDay();
  return day === 0 ? 6 : day - 1; // 周一开始
}

// 日期key: "2026-02-16"
function dateKey(d) {
  const y = d.getFullYear();
  const m = String(d.getMonth() + 1).padStart(2, '0');
  const dd = String(d.getDate()).padStart(2, '0');
  return `${y}-${m}-${dd}`;
}

function todayKey() {
  return dateKey(new Date());
}

export default function TradeLogPanel({ symbol }) {
  const now = new Date();
  const [year, setYear] = useState(now.getFullYear());
  const [month, setMonth] = useState(now.getMonth()); // 0-indexed
  const [selectedDate, setSelectedDate] = useState(todayKey());
  const [trades, setTrades] = useState([]);
  const [loading, setLoading] = useState(true);

  const fetchTrades = useCallback(async () => {
    try {
      const res = await api.getTrades(symbol, 200);
      setTrades(res?.data || []);
    } catch (e) {
      console.warn('获取交易记录失败:', e.message);
    } finally {
      setLoading(false);
    }
  }, [symbol]);

  useEffect(() => {
    fetchTrades();
    const iv = setInterval(fetchTrades, 30000);
    return () => clearInterval(iv);
  }, [fetchTrades]);

  // 按日期分组 { "2026-02-16": [trade, trade, ...] }
  const groupedByDate = useMemo(() => {
    const map = {};
    trades.forEach((t) => {
      const d = new Date(t.createdAt);
      const key = dateKey(d);
      if (!map[key]) map[key] = [];
      map[key].push(t);
    });
    return map;
  }, [trades]);

  // 每日盈亏汇总 { "2026-02-16": { pnl: 123.45, count: 3 } }
  const dailySummary = useMemo(() => {
    const map = {};
    Object.entries(groupedByDate).forEach(([key, list]) => {
      let pnl = 0;
      list.forEach((t) => {
        pnl += parseFloat(t.realizedPnl || '0');
      });
      map[key] = { pnl, count: list.length };
    });
    return map;
  }, [groupedByDate]);

  // 本月统计
  const monthStats = useMemo(() => {
    const prefix = `${year}-${String(month + 1).padStart(2, '0')}`;
    let totalPnl = 0;
    let tradeCount = 0;
    let winDays = 0;
    let lossDays = 0;
    Object.entries(dailySummary).forEach(([key, s]) => {
      if (key.startsWith(prefix)) {
        totalPnl += s.pnl;
        tradeCount += s.count;
        if (s.pnl > 0) winDays++;
        else if (s.pnl < 0) lossDays++;
      }
    });
    return { totalPnl, tradeCount, winDays, lossDays };
  }, [dailySummary, year, month]);

  // 切月
  const prevMonth = () => {
    if (month === 0) { setYear(year - 1); setMonth(11); }
    else setMonth(month - 1);
  };
  const nextMonth = () => {
    if (month === 11) { setYear(year + 1); setMonth(0); }
    else setMonth(month + 1);
  };

  // 日历网格
  const calendarGrid = useMemo(() => {
    const daysInMonth = getDaysInMonth(year, month);
    const firstDay = getFirstDayOfMonth(year, month);
    const cells = [];
    // 填充前面空白
    for (let i = 0; i < firstDay; i++) cells.push(null);
    for (let d = 1; d <= daysInMonth; d++) cells.push(d);
    return cells;
  }, [year, month]);

  // 选中日期的交易
  const selectedTrades = groupedByDate[selectedDate] || [];
  const selectedDayPnl = dailySummary[selectedDate];

  if (loading) {
    return (
      <View style={styles.card}>
        <Text style={styles.cardTitle}>收益日志</Text>
        <ActivityIndicator color={colors.blue} style={{ marginTop: 20 }} />
      </View>
    );
  }

  return (
    <View style={styles.card}>
      {/* 月份导航 */}
      <View style={styles.monthNav}>
        <TouchableOpacity onPress={prevMonth} style={styles.navBtn}>
          <Text style={styles.navBtnText}>‹</Text>
        </TouchableOpacity>
        <Text style={styles.monthTitle}>{year}年{month + 1}月</Text>
        <TouchableOpacity onPress={nextMonth} style={styles.navBtn}>
          <Text style={styles.navBtnText}>›</Text>
        </TouchableOpacity>
      </View>

      {/* 本月汇总 */}
      <View style={styles.monthStatsRow}>
        <View style={styles.monthStatItem}>
          <Text style={[styles.monthStatValue, {
            color: monthStats.totalPnl >= 0 ? colors.greenLight : colors.redLight,
          }]}>
            {monthStats.totalPnl >= 0 ? '+' : ''}{monthStats.totalPnl.toFixed(2)}
          </Text>
          <Text style={styles.monthStatLabel}>本月收益(U)</Text>
        </View>
        <View style={styles.monthStatItem}>
          <Text style={styles.monthStatValue}>{monthStats.tradeCount}</Text>
          <Text style={styles.monthStatLabel}>交易次数</Text>
        </View>
        <View style={styles.monthStatItem}>
          <Text style={[styles.monthStatValue, { color: colors.greenLight }]}>{monthStats.winDays}</Text>
          <Text style={styles.monthStatLabel}>盈利天</Text>
        </View>
        <View style={styles.monthStatItem}>
          <Text style={[styles.monthStatValue, { color: colors.redLight }]}>{monthStats.lossDays}</Text>
          <Text style={styles.monthStatLabel}>亏损天</Text>
        </View>
      </View>

      {/* 星期标题 */}
      <View style={styles.weekRow}>
        {WEEKDAYS.map((w) => (
          <View key={w} style={styles.weekCell}>
            <Text style={styles.weekText}>{w}</Text>
          </View>
        ))}
      </View>

      {/* 日历网格 */}
      <View style={styles.calendarGrid}>
        {calendarGrid.map((day, i) => {
          if (day === null) {
            return <View key={`empty-${i}`} style={styles.dayCell} />;
          }
          const key = `${year}-${String(month + 1).padStart(2, '0')}-${String(day).padStart(2, '0')}`;
          const summary = dailySummary[key];
          const isSelected = key === selectedDate;
          const isToday = key === todayKey();
          const hasTrades = !!summary;

          return (
            <TouchableOpacity
              key={key}
              style={[
                styles.dayCell,
                isSelected && styles.dayCellSelected,
                isToday && !isSelected && styles.dayCellToday,
              ]}
              onPress={() => setSelectedDate(key)}
              activeOpacity={0.7}
            >
              <Text style={[
                styles.dayText,
                isSelected && styles.dayTextSelected,
                isToday && !isSelected && { color: colors.blue },
              ]}>
                {day}
              </Text>
              {hasTrades && (
                <Text style={[
                  styles.dayPnl,
                  { color: summary.pnl >= 0 ? colors.greenLight : colors.redLight },
                  isSelected && { color: summary.pnl >= 0 ? '#4ade80' : '#fca5a5' },
                ]} numberOfLines={1}>
                  {summary.pnl >= 0 ? '+' : ''}{summary.pnl.toFixed(1)}
                </Text>
              )}
            </TouchableOpacity>
          );
        })}
      </View>

      {/* 选中日期的明细 */}
      <View style={styles.detailSection}>
        <View style={styles.detailHeader}>
          <Text style={styles.detailTitle}>
            {selectedDate.replace(/-/g, '/')} 交易明细
          </Text>
          {selectedDayPnl && (
            <Text style={[styles.detailDayPnl, {
              color: selectedDayPnl.pnl >= 0 ? colors.greenLight : colors.redLight,
            }]}>
              日盈亏: {selectedDayPnl.pnl >= 0 ? '+' : ''}{selectedDayPnl.pnl.toFixed(2)} U
            </Text>
          )}
        </View>

        {selectedTrades.length === 0 ? (
          <Text style={styles.empty}>当天无交易记录</Text>
        ) : (
          selectedTrades.map((item, idx) => {
            const isLong = item.positionSide === 'LONG' || item.side === 'BUY';
            const pnl = parseFloat(item.realizedPnl || '0');
            const hasPnl = item.realizedPnl && item.realizedPnl !== '0' && item.realizedPnl !== '';
            const pnlColor = pnl > 0 ? colors.greenLight : pnl < 0 ? colors.redLight : colors.textSecondary;

            const date = new Date(item.createdAt);
            const timeStr = `${date.getHours().toString().padStart(2, '0')}:${date.getMinutes().toString().padStart(2, '0')}:${date.getSeconds().toString().padStart(2, '0')}`;

            return (
              <View key={item.id || item.orderId || idx} style={styles.tradeItem}>
                <View style={styles.tradeRow}>
                  <View style={styles.tradeLeft}>
                    <Text style={styles.tradeSymbol}>{item.symbol}</Text>
                    <View style={[styles.badge, { backgroundColor: isLong ? colors.greenBg : colors.redBg }]}>
                      <Text style={[styles.badgeText, { color: isLong ? colors.greenLight : colors.redLight }]}>
                        {isLong ? 'LONG' : 'SHORT'}
                      </Text>
                    </View>
                    <View style={[styles.badge, { backgroundColor: colors.blueBg }]}>
                      <Text style={[styles.badgeText, { color: colors.blue }]}>{item.leverage}x</Text>
                    </View>
                    <View style={[styles.badge, {
                      backgroundColor: item.status === 'OPEN' ? colors.greenBg : colors.surface,
                    }]}>
                      <Text style={[styles.badgeText, {
                        color: item.status === 'OPEN' ? colors.greenLight : colors.textMuted,
                      }]}>
                        {item.status === 'OPEN' ? '持仓' : '平仓'}
                      </Text>
                    </View>
                  </View>
                  <Text style={styles.tradeTime}>{timeStr}</Text>
                </View>

                <View style={styles.tradeDetails}>
                  <View style={styles.detailCol}>
                    <Text style={styles.dlabel}>金额</Text>
                    <Text style={styles.dvalue}>{item.quoteQuantity || '-'} U</Text>
                  </View>
                  <View style={styles.detailCol}>
                    <Text style={styles.dlabel}>数量</Text>
                    <Text style={styles.dvalue}>{item.quantity || '-'}</Text>
                  </View>
                  <View style={styles.detailCol}>
                    <Text style={styles.dlabel}>均价</Text>
                    <Text style={styles.dvalue}>{item.price || '-'}</Text>
                  </View>
                  {hasPnl ? (
                    <View style={styles.detailCol}>
                      <Text style={styles.dlabel}>收益</Text>
                      <Text style={[styles.dvalue, { color: pnlColor, fontWeight: '700' }]}>
                        {pnl > 0 ? '+' : ''}{pnl.toFixed(2)}
                      </Text>
                    </View>
                  ) : null}
                </View>

                {(item.stopLossPrice && item.stopLossPrice !== '0') || (item.takeProfitPrice && item.takeProfitPrice !== '0') ? (
                  <View style={styles.tpslRow}>
                    {item.stopLossPrice && item.stopLossPrice !== '0' && (
                      <Text style={styles.tpslText}>
                        止损: <Text style={{ color: colors.redLight }}>{item.stopLossPrice}</Text>
                      </Text>
                    )}
                    {item.takeProfitPrice && item.takeProfitPrice !== '0' && (
                      <Text style={styles.tpslText}>
                        止盈: <Text style={{ color: colors.greenLight }}>{item.takeProfitPrice}</Text>
                      </Text>
                    )}
                  </View>
                ) : null}
              </View>
            );
          })
        )}
      </View>
    </View>
  );
}

const styles = StyleSheet.create({
  card: {
    backgroundColor: colors.card,
    borderRadius: 12,
    borderWidth: 1,
    borderColor: colors.cardBorder,
    padding: 14,
    marginBottom: 16,
  },
  // 月份导航
  monthNav: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'center',
    marginBottom: 10,
  },
  navBtn: {
    width: 36,
    height: 36,
    borderRadius: 8,
    backgroundColor: colors.surface,
    alignItems: 'center',
    justifyContent: 'center',
  },
  navBtnText: {
    fontSize: 22,
    color: colors.white,
    fontWeight: '600',
  },
  monthTitle: {
    fontSize: 17,
    fontWeight: '700',
    color: colors.white,
  },
  // 月统计
  monthStatsRow: {
    flexDirection: 'row',
    backgroundColor: colors.surface,
    borderRadius: 8,
    padding: 10,
    marginBottom: 12,
  },
  monthStatItem: {
    flex: 1,
    alignItems: 'center',
  },
  monthStatValue: {
    fontSize: 15,
    fontWeight: '700',
    color: colors.white,
  },
  monthStatLabel: {
    fontSize: 10,
    color: colors.textSecondary,
    marginTop: 2,
  },
  // 星期
  weekRow: {
    flexDirection: 'row',
    marginBottom: 4,
  },
  weekCell: {
    flex: 1,
    alignItems: 'center',
    paddingVertical: 4,
  },
  weekText: {
    fontSize: 12,
    color: colors.textMuted,
    fontWeight: '600',
  },
  // 日历
  calendarGrid: {
    flexDirection: 'row',
    flexWrap: 'wrap',
  },
  dayCell: {
    width: '14.28%',
    aspectRatio: 0.85,
    alignItems: 'center',
    justifyContent: 'center',
    paddingVertical: 2,
    borderRadius: 6,
  },
  dayCellSelected: {
    backgroundColor: colors.blue,
  },
  dayCellToday: {
    backgroundColor: colors.surface,
    borderWidth: 1,
    borderColor: colors.blue,
  },
  dayText: {
    fontSize: 13,
    color: colors.text,
    fontWeight: '500',
  },
  dayTextSelected: {
    color: colors.white,
    fontWeight: '700',
  },
  dayPnl: {
    fontSize: 8,
    fontWeight: '600',
    marginTop: 1,
  },
  // 明细区
  detailSection: {
    marginTop: 14,
    borderTopWidth: 1,
    borderTopColor: colors.cardBorder,
    paddingTop: 12,
  },
  detailHeader: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'center',
    marginBottom: 10,
  },
  detailTitle: {
    fontSize: 14,
    fontWeight: '700',
    color: colors.white,
  },
  detailDayPnl: {
    fontSize: 13,
    fontWeight: '700',
  },
  empty: {
    color: colors.textSecondary,
    textAlign: 'center',
    paddingVertical: 16,
    fontSize: 13,
  },
  // 交易项
  tradeItem: {
    backgroundColor: colors.surface,
    borderRadius: 8,
    padding: 10,
    marginBottom: 8,
  },
  tradeRow: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'center',
    marginBottom: 8,
  },
  tradeLeft: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 5,
  },
  tradeSymbol: {
    fontSize: 13,
    fontWeight: '700',
    color: colors.white,
  },
  badge: {
    paddingHorizontal: 5,
    paddingVertical: 1,
    borderRadius: 3,
  },
  badgeText: {
    fontSize: 10,
    fontWeight: '600',
  },
  tradeTime: {
    fontSize: 11,
    color: colors.textMuted,
  },
  tradeDetails: {
    flexDirection: 'row',
    gap: 4,
  },
  detailCol: {
    flex: 1,
    alignItems: 'center',
  },
  dlabel: {
    fontSize: 10,
    color: colors.textMuted,
  },
  dvalue: {
    fontSize: 12,
    color: colors.text,
    fontWeight: '500',
    marginTop: 1,
  },
  tpslRow: {
    flexDirection: 'row',
    gap: 12,
    marginTop: 6,
    paddingTop: 6,
    borderTopWidth: 1,
    borderTopColor: colors.cardBorder,
  },
  tpslText: {
    fontSize: 11,
    color: colors.textSecondary,
  },
});
