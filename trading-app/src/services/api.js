// ========== API 配置 ==========
// 修改为你的服务器地址（手机需要用局域网 IP，不能用 localhost）
const API_BASE = 'https://wws741.top/tool';
const AUTH_TOKEN = 'wws2026tool'; // 与 config.json 中 auth.token 保持一致

// WebSocket 价格转发地址（后端代理，不直连币安）
const WS_PRICE_BASE = 'wss://wws741.top/ws/price';
const WS_BOOK_BASE = 'wss://wws741.top/ws/book';
const WS_NEWS_BASE = 'wss://wws741.top/ws/news';
const WS_HYPER_MONITOR_BASE = 'wss://wws741.top/ws/hyper-monitor';

async function apiCall(method, path, body = null) {
  const options = {
    method,
    headers: {
      'Content-Type': 'application/json',
      'X-Auth-Token': AUTH_TOKEN,
    },
  };
  if (body) options.body = JSON.stringify(body);

  const res = await fetch(`${API_BASE}${path}`, options);
  const data = await res.json();
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

  // 查询未成交订单
  getOrders: (symbol) => apiCall('GET', `/orders?symbol=${symbol || ''}`),
  getOrderBook: (symbol, limit = 100) =>
    apiCall('GET', `/orderbook?symbol=${symbol || ''}&limit=${limit}`),

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

  // Hyper 跟单（服务端执行）
  startHyperFollow: (config) => apiCall('POST', '/hyper/follow/start', config),
  stopHyperFollow: (address) => apiCall('POST', '/hyper/follow/stop', { address }),
  hyperFollowStatus: (address = '') =>
    apiCall('GET', `/hyper/follow/status?address=${encodeURIComponent(address || '')}`),

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
};

export { API_BASE, WS_PRICE_BASE, WS_BOOK_BASE, WS_NEWS_BASE, WS_HYPER_MONITOR_BASE, AUTH_TOKEN };
