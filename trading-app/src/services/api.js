// ========== API 配置 ==========
// 修改为你的服务器地址（手机需要用局域网 IP，不能用 localhost）
const API_BASE = 'https://wws741.top/tool';
const AUTH_TOKEN = 'wws2026tool'; // 与 config.json 中 auth.token 保持一致

// WebSocket 价格转发地址（后端代理，不直连币安）
const WS_PRICE_BASE = 'wss://wws741.top/ws/price';
const WS_BOOK_BASE = 'wss://wws741.top/ws/book';
const WS_BIG_TRADE_BASE = 'wss://wws741.top/ws/big-trade';
const WS_NEWS_BASE = 'wss://wws741.top/ws/news';
const WS_HYPER_MONITOR_BASE = 'wss://wws741.top/ws/hyper-monitor';
const WS_LIQUIDATION_BASE = 'wss://wws741.top/ws/liquidation-stats';
const WS_MARKET_SPIKE_BASE = 'wss://wws741.top/ws/market-spike';
const WS_MARKET_RANGE_BASE = 'wss://wws741.top/ws/market-range';

async function apiCall(method, path, body = null, requestOptions = {}) {
  const { signal } = requestOptions;
  const options = {
    method,
    headers: {
      'Content-Type': 'application/json',
      'X-Auth-Token': AUTH_TOKEN,
    },
  };
  if (body) options.body = JSON.stringify(body);
  if (signal) options.signal = signal;

  const res = await fetch(`${API_BASE}${path}`, options);
  const text = await res.text();

  // 尝试解析 JSON，失败时显示实际响应内容（方便排查 nginx 错误页等）
  let data;
  try {
    data = JSON.parse(text);
  } catch (_e) {
    throw new Error(`请求失败 (HTTP ${res.status}): ${text.substring(0, 200)}`);
  }

  if (!res.ok) {
    throw new Error(data.error || `HTTP ${res.status}`);
  }
  if (data.error) throw new Error(data.error);
  return data;
}

export default {
  // 余额
  getBalance: () => apiCall('GET', '/balance'),

  // 持仓
  getPositions: () => apiCall('GET', '/positions'),

  // 下单
  placeOrder: (req) => apiCall('POST', '/order', req),

  // 平仓
  closePosition: (req) => apiCall('POST', '/close', req),

  // 减仓
  reducePosition: (req) => apiCall('POST', '/reduce', req),

  // 一键反手
  reversePosition: (req) => apiCall('POST', '/reverse', req),

  // 查询未成交订单
  getOrders: (symbol) => apiCall('GET', `/orders?symbol=${symbol || ''}`),
  getOrderBook: (symbol, limit = 100) =>
    apiCall('GET', `/orderbook?symbol=${symbol || ''}&limit=${limit}`),
  getOrderBookWhale: ({
    symbol,
    limit = 500,
    minNotional = 100000,
    maxRows = 20,
    side = 'BOTH',
  }) =>
    apiCall(
      'GET',
      `/orderbook/whale?symbol=${encodeURIComponent(symbol || '')}&limit=${limit}&minNotional=${minNotional}&maxRows=${maxRows}&side=${encodeURIComponent(side)}`,
    ),

  // 撤单
  cancelOrder: (symbol, orderId) =>
    apiCall('DELETE', `/order?symbol=${symbol}&orderId=${orderId}`),

  // 杠杆
  changeLeverage: (symbol, leverage) =>
    apiCall('POST', '/leverage', { symbol, leverage }),

  // 交易记录
  getTrades: (symbol, limit = 50) =>
    apiCall('GET', `/trades?symbol=${symbol || ''}&limit=${limit}`),
  getOperations: (symbol, status = 'FAILED', limit = 50) =>
    apiCall('GET', `/operations?symbol=${symbol || ''}&status=${status || ''}&limit=${limit}`),
  getLiquidationHistory: (limit = 120) =>
    apiCall('GET', `/liquidation/history?limit=${limit}`),
  getAnalyticsJournal: ({ period = 'daily', from = '', to = '' } = {}) =>
    apiCall(
      'GET',
      `/analytics/journal?period=${encodeURIComponent(period)}&from=${encodeURIComponent(from)}&to=${encodeURIComponent(to)}`,
    ),
  getAnalyticsAttribution: ({ from = '', to = '' } = {}) =>
    apiCall(
      'GET',
      `/analytics/attribution?from=${encodeURIComponent(from)}&to=${encodeURIComponent(to)}`,
    ),
  getAnalyticsSentiment: ({ symbol = 'BTCUSDT', period = '5m' } = {}) =>
    apiCall(
      'GET',
      `/analytics/sentiment?symbol=${encodeURIComponent(symbol)}&period=${encodeURIComponent(period)}`,
    ),
  getNewsSourceStatus: () => apiCall('GET', '/news/sources/status'),
  getNewsPage: ({ source, page = 1, pageSize = 20 } = {}) =>
    apiCall(
      'GET',
      `/news/page?source=${encodeURIComponent(source || '')}&page=${page}&pageSize=${pageSize}`,
    ),

  // Hyper 跟单（服务端执行）
  startHyperFollow: (config) => apiCall('POST', '/hyper/follow/start', config),
  stopHyperFollow: (address) => apiCall('POST', '/hyper/follow/stop', { address }),
  hyperFollowStatus: (address = '') =>
    apiCall('GET', `/hyper/follow/status?address=${encodeURIComponent(address || '')}`),
  getHyperPositions: (address) =>
    apiCall('GET', `/hyper/positions?address=${encodeURIComponent(address || '')}`),

  // 浮盈加仓
  startAutoScale: (config) => apiCall('POST', '/autoscale/start', config),
  stopAutoScale: (symbol) => apiCall('POST', '/autoscale/stop', { symbol }),
  autoScaleStatus: (symbol) => apiCall('GET', `/autoscale/status?symbol=${symbol}`),

  // 风控
  getRiskStatus: () => apiCall('GET', '/risk/status'),
  unlockRisk: () => apiCall('POST', '/risk/unlock'),

  // 网格交易
  startGrid: (config) => apiCall('POST', '/grid/start', config),
  stopGrid: (symbol) => apiCall('POST', '/grid/stop', { symbol }),
  gridStatus: (symbol) => apiCall('GET', `/grid/status?symbol=${symbol}`),

  // DCA 定投
  startDCA: (config) => apiCall('POST', '/dca/start', config),
  stopDCA: (symbol) => apiCall('POST', '/dca/stop', { symbol }),
  dcaStatus: (symbol) => apiCall('GET', `/dca/status?symbol=${symbol}`),

  // RSI+成交量 信号策略
  startSignal: (config) => apiCall('POST', '/signal/start', config),
  stopSignal: (symbol) => apiCall('POST', '/signal/stop', { symbol }),
  signalStatus: (symbol) => apiCall('GET', `/signal/status?symbol=${symbol}`),

  // K线形态（十字星）策略
  startDoji: (config) => apiCall('POST', '/doji/start', config),
  stopDoji: (symbol) => apiCall('POST', '/doji/stop', { symbol }),
  dojiStatus: (symbol) => apiCall('GET', `/doji/status?symbol=${symbol}`),

  // 资金费率监控
  startFundingMonitor: (config) => apiCall('POST', '/funding/start', config),
  stopFundingMonitor: () => apiCall('POST', '/funding/stop'),
  fundingStatus: () => apiCall('GET', '/funding/status'),

  // 多策略联动
  startStrategyLink: (rules) => apiCall('POST', '/link/start', { rules }),
  stopStrategyLink: () => apiCall('POST', '/link/stop'),
  strategyLinkStatus: () => apiCall('GET', '/link/status'),
  updateStrategyLinkRules: (rules) => apiCall('POST', '/link/rules', { rules }),

  // 支撑/阻力位
  getSRLevels: (symbol) => apiCall('GET', `/sr/levels?symbol=${symbol}`),

  // 推荐交易扫描
  getRecommendScan: (symbols) => apiCall('GET', `/recommend/scan${symbols ? `?symbols=${encodeURIComponent(symbols)}` : ''}`),

  // 持仓分析
  getRecommendAnalyze: (requestOptions = {}) => apiCall('GET', '/recommend/analyze', null, requestOptions),
  analyzeAgent: (req, requestOptions = {}) => apiCall('POST', '/agent/analyze', req, requestOptions),
  analyzeAgentAsync: (req, requestOptions = {}) =>
    apiCall('POST', '/agent/analyze', { ...(req || {}), async: true }, requestOptions),
  executeAgent: (req) => apiCall('POST', '/agent/execute', req),
  getAgentLog: (id, requestOptions = {}) =>
    apiCall('GET', `/agent/log?id=${encodeURIComponent(String(id || ''))}`, null, requestOptions),
  getAgentLogs: ({ limit = 50, status = '', execute } = {}, requestOptions = {}) => {
    const parts = [`limit=${Number(limit) > 0 ? Number(limit) : 50}`];
    if (status) parts.push(`status=${encodeURIComponent(String(status))}`);
    if (typeof execute === 'boolean') {
      parts.push(`execute=${execute ? 'true' : 'false'}`);
    }
    return apiCall('GET', `/agent/logs?${parts.join('&')}`, null, requestOptions);
  },
  getAgentPolicy: () => apiCall('GET', '/agent/policy'),

  // 本地止盈止损
  getTPSLList: (symbol) => apiCall('GET', `/tpsl/list?symbol=${symbol || ''}`),
  cancelTPSL: (req) => apiCall('POST', '/tpsl/cancel', req),
  getTPSLHistory: (symbol, limit = 50) => apiCall('GET', `/tpsl/history?symbol=${symbol || ''}&limit=${limit}`),

  // 1分钟 Scalp 策略
  startScalp: (config) => apiCall('POST', '/scalp/start', config),
  stopScalp: (symbol) => apiCall('POST', '/scalp/stop', { symbol }),
  scalpStatus: (symbol) => apiCall('GET', `/scalp/status?symbol=${symbol}`),

  // Paper Trading
  getPaperStatus: () => apiCall('GET', '/paper/status'),
  resetPaper: () => apiCall('POST', '/paper/reset'),

  // 回测
  runBacktest: (config) => apiCall('POST', '/backtest/run', config),

  // 订单流
  getOrderFlow: (symbol, depth = 500) =>
    apiCall('GET', `/orderflow?symbol=${encodeURIComponent(symbol)}&depth=${depth}`),

  // 滑点统计
  getSlippageStats: (symbol) =>
    apiCall('GET', `/slippage/stats?symbol=${encodeURIComponent(symbol || '')}`),

  // 组合风控
  getPortfolioRisk: () => apiCall('GET', '/risk/portfolio'),

  // 第九章新增 API
  getVarStatus: () => apiCall('GET', '/risk/var'),
  getKillSwitchStatus: () => apiCall('GET', '/risk/kill-switch'),
  runStressTest: (scenarios) => apiCall('POST', '/risk/stress-test', { scenarios }),
  getOpsMetrics: () => apiCall('GET', '/ops/metrics'),
  getMonitorOverview: (days = 30) =>
    apiCall('GET', `/monitor/overview?days=${Number(days) > 0 ? Number(days) : 30}`),
  getDataQuality: () => apiCall('GET', '/data-quality'),
  getExecutionQuality: (source, days) => apiCall('GET', `/execution/quality?source=${source || ''}&days=${days || 30}`),
  getAllocationStatus: () => apiCall('GET', '/allocator/status'),
  getRegimeStatus: () => apiCall('GET', '/regime/status'),
  getParamStability: () => apiCall('GET', '/param-stability/status'),
  getAgentEvaluation: (days) => apiCall('GET', `/agent/evaluation?days=${days || 30}`),
  agentRiskCheck: (data) => apiCall('POST', '/agent/risk-check', data),
  strategyAdmin: (data) => apiCall('POST', '/strategy/admin', data),
  getFallbackStatus: () => apiCall('GET', '/data-fallback/status'),
};

export {
  API_BASE,
  WS_PRICE_BASE,
  WS_BOOK_BASE,
  WS_BIG_TRADE_BASE,
  WS_NEWS_BASE,
  WS_HYPER_MONITOR_BASE,
  WS_LIQUIDATION_BASE,
  WS_MARKET_SPIKE_BASE,
  WS_MARKET_RANGE_BASE,
  AUTH_TOKEN,
};
