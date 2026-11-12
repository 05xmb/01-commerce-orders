// API 封装：统一携带 token、解析 {code,data,msg} 信封
const state = {
  token: localStorage.getItem('trade_token') || '',
  user: JSON.parse(localStorage.getItem('trade_user') || 'null'),
}

export function setSession(token, user) {
  state.token = token
  state.user = user
  localStorage.setItem('trade_token', token)
  localStorage.setItem('trade_user', JSON.stringify(user))
}

export function clearSession() {
  state.token = ''
  state.user = null
  localStorage.removeItem('trade_token')
  localStorage.removeItem('trade_user')
}

export function getSession() {
  return state
}

async function call(method, url, body, auth = true) {
  const headers = { 'Content-Type': 'application/json' }
  if (auth && state.token) headers.Authorization = 'Bearer ' + state.token
  const resp = await fetch(url, {
    method,
    headers,
    body: body === undefined ? undefined : JSON.stringify(body),
  })
  let data
  try {
    data = await resp.json()
  } catch {
    throw new Error('服务响应异常（HTTP ' + resp.status + '）')
  }
  if (resp.status === 401) {
    clearSession()
    throw new Error(data.msg || '登录已过期，请重新登录')
  }
  if (data.code !== 0) throw new Error(data.msg || '操作失败（' + resp.status + '）')
  return data.data
}

export const api = {
  // 公共
  login: (username, password) => call('POST', '/api/login', { username, password }, false),
  // 买家
  products: () => call('GET', '/api/products'),
  createOrder: (product_id, qty) => call('POST', '/api/orders/create', { product_id, qty }),
  myOrders: () => call('GET', '/api/orders/mine'),
  getOrder: (order_no) => call('GET', '/api/orders/get?order_no=' + order_no),
  cancelOrder: (order_no) => call('POST', '/api/orders/cancel', { order_no }),
  applyRefund: (order_no, reason) => call('POST', '/api/orders/refund/apply', { order_no, reason }),
  // 仓库
  whOrders: () => call('GET', '/api/wh/orders'),
  ship: (order_no, carrier, tracking_no) => call('POST', '/api/wh/ship', { order_no, carrier, tracking_no }),
  // 客服
  csRefunds: (status) => call('GET', '/api/cs/refunds' + (status ? '?status=' + status : '')),
  csReview: (refund_no, approve, note) => call('POST', '/api/cs/refunds/review', { refund_no, approve, note }),
  csRetry: (refund_no) => call('POST', '/api/cs/refunds/retry', { refund_no }),
  csExceptions: (status) => call('GET', '/api/cs/exceptions' + (status ? '?status=' + status : '')),
  csExceptionRefund: (id) => call('POST', '/api/cs/exceptions/refund', { id }),
  // 运营
  opsProducts: () => call('GET', '/api/ops/products'),
  opsSetPrice: (id, price) => call('POST', '/api/ops/products/price', { id, price }),
  opsSetSale: (id, on_sale) => call('POST', '/api/ops/products/sale', { id, on_sale }),
  opsSummary: () => call('GET', '/api/ops/summary'),
  opsOrderDetail: (order_no) => call('GET', '/api/ops/order-detail?order_no=' + order_no),
  opsNotifyLogs: () => call('GET', '/api/ops/notify-logs'),
  opsExceptions: (status) => call('GET', '/api/ops/exceptions' + (status ? '?status=' + status : '')),
  opsStockFlows: (product_id) => call('GET', '/api/ops/stock-flows?product_id=' + product_id),
  // 模拟支付渠道（免鉴权，直接操作渠道服务）
  channelPay: (order_no, amount, scenario, delay_ms) =>
    call('POST', '/channel/pay', { order_no, amount, scenario, delay_ms }, false),
}
