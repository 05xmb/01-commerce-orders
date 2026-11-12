<template>
  <div>
    <!-- 仓库工作台 -->
    <template v-if="role === 'wh'">
      <div class="bar">
        <h3>仓库工作台 · 待发货</h3>
        <el-button :loading="loading" @click="load">刷新</el-button>
      </div>
      <el-alert type="warning" :closable="false" class="tip"
        title="退款审核中的订单已暂停发货；审核拒绝后恢复可发货" />
      <el-table :data="whList" v-loading="loading" stripe>
        <el-table-column prop="order_no" label="订单号" width="180" />
        <el-table-column prop="product_name" label="商品" />
        <el-table-column prop="qty" label="数量" width="70" />
        <el-table-column label="金额" width="100">
          <template #default="{ row }">¥{{ (row.amount / 100).toFixed(2) }}</template>
        </el-table-column>
        <el-table-column label="状态" width="120">
          <template #default="{ row }">
            <el-tag :type="row.status === 'refund_review' ? 'warning' : row.status === 'shipped' ? 'primary' : 'success'">
              {{ { to_ship: '待发货', refund_review: '退款审核中（停发）', shipped: '已发货' }[row.status] || row.status }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="支付时间" width="170">
          <template #default="{ row }">{{ row.paid_at ? fmt(row.paid_at) : '-' }}</template>
        </el-table-column>
        <el-table-column label="操作" width="120">
          <template #default="{ row }">
            <el-button v-if="row.status === 'to_ship'" size="small" type="primary" @click="openShip(row)">登记发货</el-button>
          </template>
        </el-table-column>
      </el-table>

      <el-dialog v-model="ship.visible" title="登记发货" width="420px">
        <p>订单 {{ ship.order?.order_no }}</p>
        <el-form label-width="80px">
          <el-form-item label="承运方">
            <el-select v-model="ship.carrier" placeholder="选择承运方">
              <el-option v-for="c in carriers" :key="c" :label="c" :value="c" />
            </el-select>
          </el-form-item>
          <el-form-item label="运单号">
            <el-input v-model="ship.tracking" placeholder="运单号" />
          </el-form-item>
        </el-form>
        <template #footer>
          <el-button @click="ship.visible = false">取消</el-button>
          <el-button type="primary" :loading="ship.loading" @click="submitShip">确认发货</el-button>
        </template>
      </el-dialog>
    </template>

    <!-- 客服工作台 -->
    <template v-else-if="role === 'cs'">
      <div class="bar">
        <h3>客服工作台</h3>
        <el-button :loading="loading" @click="load">刷新</el-button>
      </div>
      <el-tabs v-model="csTab">
        <el-tab-pane label="退款审核" name="refunds">
          <el-table :data="refunds" v-loading="loading" stripe>
            <el-table-column prop="refund_no" label="退款单号" width="180" />
            <el-table-column prop="order_no" label="订单号" width="180" />
            <el-table-column label="金额" width="100">
              <template #default="{ row }">¥{{ (row.amount / 100).toFixed(2) }}</template>
            </el-table-column>
            <el-table-column prop="reason" label="原因" />
            <el-table-column label="状态" width="110">
              <template #default="{ row }">
                <el-tag :type="refundTag(row.status)">{{ refundName(row.status) }}</el-tag>
              </template>
            </el-table-column>
            <el-table-column label="尝试" width="60" prop="attempt" />
            <el-table-column label="操作" width="210">
              <template #default="{ row }">
                <template v-if="row.status === 'review'">
                  <el-button size="small" type="success" @click="review(row, true)">同意</el-button>
                  <el-button size="small" type="danger" @click="review(row, false)">拒绝</el-button>
                </template>
                <el-button v-if="row.status === 'failed_pending'" size="small" type="warning" @click="retry(row)">重试退款</el-button>
              </template>
            </el-table-column>
          </el-table>
        </el-tab-pane>
        <el-tab-pane label="异常支付（迟到）" name="exceptions">
          <el-table :data="exceptions" v-loading="loading" stripe>
            <el-table-column prop="id" label="ID" width="70" />
            <el-table-column prop="order_no" label="订单号" width="180" />
            <el-table-column prop="trade_no" label="渠道交易号" width="170" />
            <el-table-column label="金额" width="100">
              <template #default="{ row }">¥{{ (row.amount / 100).toFixed(2) }}</template>
            </el-table-column>
            <el-table-column label="状态" width="100">
              <template #default="{ row }">
                <el-tag :type="{ pending: 'danger', refunding: 'warning', refunded: 'success' }[row.status] || 'info'">
                  {{ { pending: '待处理', refunding: '退款中', refunded: '已退款' }[row.status] || row.status }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column prop="refund_no" label="退款单号" width="180">
              <template #default="{ row }">{{ row.refund_no || '-' }}</template>
            </el-table-column>
            <el-table-column label="操作" width="130">
              <template #default="{ row }">
                <el-button v-if="row.status === 'pending'" size="small" type="primary" @click="exceptionRefund(row)">核实并退款</el-button>
              </template>
            </el-table-column>
          </el-table>
        </el-tab-pane>
      </el-tabs>
    </template>

    <!-- 运营工作台 -->
    <template v-else-if="role === 'op'">
      <div class="bar">
        <h3>运营工作台</h3>
        <el-button :loading="loading" @click="load">刷新</el-button>
      </div>
      <el-row :gutter="16" class="sum">
        <el-col :span="6"><el-card><div class="num">¥{{ (summary.paid_amount / 100 || 0).toFixed(2) }}</div><div class="lbl">实收（成功支付）</div></el-card></el-col>
        <el-col :span="6"><el-card><div class="num">¥{{ (summary.refunded_amount / 100 || 0).toFixed(2) }}</div><div class="lbl">实退（成功退款）</div></el-card></el-col>
        <el-col :span="12"><el-card><div class="num small">净额 ¥{{ (((summary.paid_amount || 0) - (summary.refunded_amount || 0)) / 100).toFixed(2) }}</div><div class="lbl">实收 - 实退</div></el-card></el-col>
      </el-row>
      <el-tabs v-model="opTab">
        <el-tab-pane label="商品管理" name="products">
          <el-table :data="products" v-loading="loading" stripe>
            <el-table-column prop="id" label="ID" width="80" />
            <el-table-column prop="name" label="商品" />
            <el-table-column label="价格" width="150">
              <template #default="{ row }">
                <el-input-number v-model="priceEdit[row.id]" :min="1" size="small" :controls="false" style="width: 110px" /> 分
              </template>
            </el-table-column>
            <el-table-column prop="stock" label="库存" width="80" />
            <el-table-column label="上架" width="90">
              <template #default="{ row }">
                <el-switch :model-value="row.on_sale" @change="toggleSale(row, $event)" />
              </template>
            </el-table-column>
            <el-table-column label="操作" width="170">
              <template #default="{ row }">
                <el-button size="small" type="primary" @click="savePrice(row)">保存价格</el-button>
                <el-button size="small" @click="showFlows(row)">库存流水</el-button>
              </template>
            </el-table-column>
          </el-table>
        </el-tab-pane>
        <el-tab-pane label="订单穿透核查" name="audit">
          <div class="audit-bar">
            <el-input v-model="auditNo" placeholder="输入订单号" style="width: 240px" />
            <el-button type="primary" :loading="auditing" @click="runAudit">穿透查询</el-button>
          </div>
          <template v-if="detail">
            <el-descriptions title="订单" :column="3" border class="blk">
              <el-descriptions-item label="订单号">{{ detail.order.order_no }}</el-descriptions-item>
              <el-descriptions-item label="买家">{{ detail.order.buyer_id }}</el-descriptions-item>
              <el-descriptions-item label="状态">{{ detail.order.status }}</el-descriptions-item>
              <el-descriptions-item label="商品">{{ detail.order.product_name }} × {{ detail.order.qty }}</el-descriptions-item>
              <el-descriptions-item label="金额">¥{{ (detail.order.amount / 100).toFixed(2) }}</el-descriptions-item>
              <el-descriptions-item label="创建">{{ fmt(detail.order.created_at) }}</el-descriptions-item>
            </el-descriptions>
            <el-descriptions title="收款" :column="3" border class="blk">
              <template v-if="detail.payment">
                <el-descriptions-item label="渠道交易号">{{ detail.payment.trade_no }}</el-descriptions-item>
                <el-descriptions-item label="金额">¥{{ (detail.payment.amount / 100).toFixed(2) }}</el-descriptions-item>
                <el-descriptions-item label="状态">{{ detail.payment.status }}</el-descriptions-item>
              </template>
              <el-descriptions-item v-else label="收款">无</el-descriptions-item>
            </el-descriptions>
            <el-table v-if="detail.refunds?.length" :data="detail.refunds" size="small" class="blk" title="退款">
              <el-table-column prop="refund_no" label="退款单" width="170" />
              <el-table-column prop="type" label="类型" width="90" />
              <el-table-column prop="status" label="状态" width="110" />
              <el-table-column label="金额" width="90">
                <template #default="{ row }">¥{{ (row.amount / 100).toFixed(2) }}</template>
              </el-table-column>
              <el-table-column prop="attempt" label="尝试" width="60" />
            </el-table>
            <el-descriptions title="发货" :column="3" border class="blk">
              <template v-if="detail.shipment">
                <el-descriptions-item label="承运方">{{ detail.shipment.carrier }}</el-descriptions-item>
                <el-descriptions-item label="运单号">{{ detail.shipment.tracking_no }}</el-descriptions-item>
                <el-descriptions-item label="时间">{{ fmt(detail.shipment.created_at) }}</el-descriptions-item>
              </template>
              <el-descriptions-item v-else label="发货">无</el-descriptions-item>
            </el-descriptions>
            <el-table v-if="detail.exceptions?.length" :data="detail.exceptions" size="small" class="blk">
              <el-table-column prop="id" label="异常ID" width="80" />
              <el-table-column prop="trade_no" label="渠道交易号" width="170" />
              <el-table-column prop="status" label="状态" width="90" />
              <el-table-column label="金额">
                <template #default="{ row }">¥{{ (row.amount / 100).toFixed(2) }}</template>
              </el-table-column>
            </el-table>
            <el-table v-if="detail.stock_flows?.length" :data="detail.stock_flows" size="small" class="blk">
              <el-table-column prop="change_type" label="库存变动" width="100" />
              <el-table-column prop="qty" label="数量" width="70" />
              <el-table-column prop="stock_after" label="变动后库存" width="110" />
              <el-table-column label="时间">
                <template #default="{ row }">{{ fmt(row.created_at) }}</template>
              </el-table-column>
            </el-table>
            <el-table v-if="detail.notify_logs?.length" :data="detail.notify_logs" size="small" class="blk">
              <el-table-column prop="notify_id" label="通知ID" width="170" />
              <el-table-column prop="trade_no" label="渠道交易号" width="170" />
              <el-table-column label="验签" width="70">
                <template #default="{ row }">{{ row.verify_ok ? '✓' : '✗' }}</template>
              </el-table-column>
              <el-table-column prop="result" label="结果" width="130" />
              <el-table-column label="时间">
                <template #default="{ row }">{{ fmt(row.created_at) }}</template>
              </el-table-column>
            </el-table>
          </template>
        </el-tab-pane>
        <el-tab-pane label="通知日志" name="logs">
          <el-table :data="logs" v-loading="loading" stripe size="small">
            <el-table-column prop="notify_id" label="通知ID" width="170" />
            <el-table-column prop="order_no" label="订单号" width="170" />
            <el-table-column prop="trade_no" label="渠道交易号" width="160" />
            <el-table-column label="验签" width="70">
              <template #default="{ row }">{{ row.verify_ok ? '✓' : '✗' }}</template>
            </el-table-column>
            <el-table-column prop="result" label="结果" width="130" />
            <el-table-column label="时间">
              <template #default="{ row }">{{ fmt(row.created_at) }}</template>
            </el-table-column>
          </el-table>
        </el-tab-pane>
        <el-tab-pane label="异常支付台账" name="exceptions">
          <el-table :data="exceptions" v-loading="loading" stripe>
            <el-table-column prop="id" label="ID" width="70" />
            <el-table-column prop="order_no" label="订单号" width="180" />
            <el-table-column prop="trade_no" label="渠道交易号" width="170" />
            <el-table-column label="金额" width="100">
              <template #default="{ row }">¥{{ (row.amount / 100).toFixed(2) }}</template>
            </el-table-column>
            <el-table-column prop="status" label="状态" width="90" />
            <el-table-column prop="handler" label="处理人" width="100">
              <template #default="{ row }">{{ row.handler || '-' }}</template>
            </el-table-column>
            <el-table-column prop="refund_no" label="退款单号" />
          </el-table>
        </el-tab-pane>
      </el-tabs>
    </template>

    <el-dialog v-model="flows.visible" :title="'库存流水 · ' + (flows.product?.name || '')" width="640px">
      <el-table :data="flows.list" size="small">
        <el-table-column prop="order_no" label="订单号" width="170" />
        <el-table-column prop="change_type" label="类型" width="90" />
        <el-table-column prop="qty" label="数量" width="70" />
        <el-table-column prop="stock_after" label="变动后" width="80" />
        <el-table-column label="时间">
          <template #default="{ row }">{{ fmt(row.created_at) }}</template>
        </el-table-column>
      </el-table>
    </el-dialog>
  </div>
</template>

<script setup>
import { onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { api } from '../api'

const props = defineProps({ role: { type: String, required: true } })

const loading = ref(false)
const whList = ref([])
const refunds = ref([])
const exceptions = ref([])
const products = ref([])
const logs = ref([])
const summary = reactive({ paid_amount: 0, refunded_amount: 0 })
const priceEdit = reactive({})
const csTab = ref('refunds')
const opTab = ref('products')
const auditNo = ref('')
const auditing = ref(false)
const detail = ref(null)
const carriers = ['顺丰速运', '中通快递', '圆通速递', '韵达快递']
const ship = reactive({ visible: false, order: null, carrier: '顺丰速运', tracking: '', loading: false })
const flows = reactive({ visible: false, product: null, list: [] })

const refundStatusMap = {
  review: ['审核中', 'warning'],
  refunding: ['退款中', 'warning'],
  failed_pending: ['失败待处理', 'danger'],
  success: ['已退款', 'success'],
  rejected: ['已拒绝', 'info'],
}
function refundName(s) { return refundStatusMap[s]?.[0] || s }
function refundTag(s) { return refundStatusMap[s]?.[1] || 'info' }
function fmt(t) { return new Date(t).toLocaleString('zh-CN', { hour12: false }) }

async function load() {
  loading.value = true
  try {
    if (props.role === 'wh') {
      whList.value = await api.whOrders()
    } else if (props.role === 'cs') {
      refunds.value = await api.csRefunds()
      exceptions.value = await api.csExceptions()
    } else if (props.role === 'op') {
      const [prods, sum, ls, exs] = await Promise.all([
        api.opsProducts(), api.opsSummary(), api.opsNotifyLogs(), api.opsExceptions(),
      ])
      products.value = prods
      prods.forEach((p) => { priceEdit[p.id] = p.price })
      Object.assign(summary, sum)
      logs.value = ls
      exceptions.value = exs
    }
  } catch (e) {
    ElMessage.error(e.message)
  } finally {
    loading.value = false
  }
}

// 仓库
function openShip(row) {
  ship.order = row
  ship.tracking = ''
  ship.visible = true
}
async function submitShip() {
  if (!ship.tracking.trim()) {
    ElMessage.warning('请填写运单号')
    return
  }
  ship.loading = true
  try {
    await api.ship(ship.order.order_no, ship.carrier, ship.tracking.trim())
    ElMessage.success('已发货')
    ship.visible = false
    load()
  } catch (e) {
    ElMessage.error(e.message)
  } finally {
    ship.loading = false
  }
}

// 客服
async function review(row, approve) {
  try {
    const { value } = await ElMessageBox.prompt(
      approve ? '同意退款后将请求渠道原路退回' : '拒绝后订单恢复可发货',
      approve ? '同意退款' : '拒绝退款',
      { inputPlaceholder: '审核备注（必填）', inputValidator: (v) => !!v?.trim() || '备注必填' },
    )
    await api.csReview(row.refund_no, approve, value.trim())
    ElMessage.success('已处理')
    load()
  } catch (e) {
    if (e !== 'cancel') ElMessage.error(e.message)
  }
}
async function retry(row) {
  try {
    await api.csRetry(row.refund_no)
    ElMessage.success('已重新请求渠道退款')
    load()
  } catch (e) {
    ElMessage.error(e.message)
  }
}
async function exceptionRefund(row) {
  try {
    await ElMessageBox.confirm(
      `确认订单 ${row.order_no} 的迟到支付 ¥${(row.amount / 100).toFixed(2)} 原路退款？（不恢复库存）`,
      '核实异常支付',
      { type: 'warning' },
    )
    await api.csExceptionRefund(row.id)
    ElMessage.success('已发起退款')
    load()
  } catch (e) {
    if (e !== 'cancel') ElMessage.error(e.message)
  }
}

// 运营
async function savePrice(row) {
  try {
    await api.opsSetPrice(row.id, Number(priceEdit[row.id]))
    ElMessage.success('已改价（不影响已创建订单的快照价格）')
    load()
  } catch (e) {
    ElMessage.error(e.message)
  }
}
async function toggleSale(row, val) {
  try {
    await api.opsSetSale(row.id, val)
    ElMessage.success(val ? '已上架' : '已下架（不影响已创建订单）')
    load()
  } catch (e) {
    ElMessage.error(e.message)
  }
}
async function showFlows(row) {
  try {
    flows.list = await api.opsStockFlows(row.id)
    flows.product = row
    flows.visible = true
  } catch (e) {
    ElMessage.error(e.message)
  }
}
async function runAudit() {
  if (!auditNo.value.trim()) {
    ElMessage.warning('请输入订单号')
    return
  }
  auditing.value = true
  try {
    detail.value = await api.opsOrderDetail(auditNo.value.trim())
  } catch (e) {
    detail.value = null
    ElMessage.error(e.message)
  } finally {
    auditing.value = false
  }
}

onMounted(load)
</script>

<style scoped>
.bar { display: flex; justify-content: space-between; align-items: center; }
.tip { margin-bottom: 12px; }
.sum { margin-bottom: 16px; }
.sum .num { font-size: 24px; font-weight: 600; color: #303133; }
.sum .num.small { font-size: 18px; }
.sum .lbl { color: #909399; font-size: 13px; margin-top: 4px; }
.audit-bar { display: flex; gap: 10px; margin-bottom: 16px; }
.blk { margin-bottom: 16px; }
</style>
