<template>
  <div>
    <div class="bar">
      <h3>我的订单</h3>
      <el-button :loading="loading" @click="load">刷新</el-button>
    </div>
    <el-table :data="orders" v-loading="loading" stripe>
      <el-table-column prop="order_no" label="订单号" width="180" />
      <el-table-column prop="product_name" label="商品" />
      <el-table-column label="数量" width="70" prop="qty" />
      <el-table-column label="金额" width="110">
        <template #default="{ row }">¥{{ (row.amount / 100).toFixed(2) }}</template>
      </el-table-column>
      <el-table-column label="状态" width="110">
        <template #default="{ row }">
          <el-tag :type="statusType(row.status)">{{ statusName(row.status) }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column label="支付倒计时" width="120">
        <template #default="{ row }">
          <span v-if="row.status === 'pending'" class="countdown">{{ left(row) }}</span>
          <span v-else>-</span>
        </template>
      </el-table-column>
      <el-table-column label="创建时间" width="170">
        <template #default="{ row }">{{ fmt(row.created_at) }}</template>
      </el-table-column>
      <el-table-column label="操作" width="260">
        <template #default="{ row }">
          <el-button v-if="row.status === 'pending'" size="small" type="primary" @click="openPay(row)">支付</el-button>
          <el-button v-if="row.status === 'pending'" size="small" @click="cancel(row)">取消</el-button>
          <el-button v-if="row.status === 'to_ship'" size="small" type="warning" @click="openRefund(row)">申请退款</el-button>
          <el-button v-if="row.status === 'shipped'" size="small" type="success" @click="showShip(row)">查看物流</el-button>
        </template>
      </el-table-column>
    </el-table>

    <!-- 模拟支付弹窗：按钮操作渠道服务，渠道异步通知交易系统 -->
    <el-dialog v-model="pay.visible" title="模拟支付渠道" width="460px">
      <p>订单 <b>{{ pay.order?.order_no }}</b>，金额 <b>¥{{ ((pay.order?.amount || 0) / 100).toFixed(2) }}</b></p>
      <p class="dim">选择支付结果；通知由渠道异步发送，状态变化请稍后刷新。</p>
      <div class="scenes">
        <el-button type="success" @click="payScene('success')">支付成功</el-button>
        <el-button type="danger" @click="payScene('fail')">支付失败</el-button>
        <el-button @click="payScene('duplicate')">重复通知</el-button>
        <el-button @click="payScene('late')">延迟通知（迟到支付）</el-button>
        <el-button @click="payScene('amount_mismatch')">金额不符</el-button>
        <el-button @click="payScene('unknown_order')">未知订单</el-button>
        <el-button @click="payScene('bad_sign')">验签失败</el-button>
      </div>
    </el-dialog>

    <!-- 申请退款 -->
    <el-dialog v-model="refund.visible" title="申请退款（售前）" width="420px">
      <p>订单 {{ refund.order?.order_no }}，金额 ¥{{ ((refund.order?.amount || 0) / 100).toFixed(2) }}</p>
      <el-input v-model="refund.reason" type="textarea" placeholder="退款原因" :rows="3" />
      <template #footer>
        <el-button @click="refund.visible = false">取消</el-button>
        <el-button type="primary" :loading="refund.loading" @click="submitRefund">提交申请</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { computed, onMounted, onUnmounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { api } from '../api'

const orders = ref([])
const loading = ref(false)
const now = ref(Date.now())
let timer

const pay = reactive({ visible: false, order: null })
const refund = reactive({ visible: false, order: null, reason: '', loading: false })

async function load() {
  loading.value = true
  try {
    orders.value = await api.myOrders()
  } catch (e) {
    ElMessage.error(e.message)
  } finally {
    loading.value = false
  }
}

function left(row) {
  const sec = Math.max(0, Math.floor((new Date(row.pay_deadline) - now.value) / 1000))
  return `${Math.floor(sec / 60)}:${String(sec % 60).padStart(2, '0')}`
}

function fmt(t) {
  return new Date(t).toLocaleString('zh-CN', { hour12: false })
}

const statusMap = {
  pending: ['待付款', 'info'],
  to_ship: ['待发货', 'success'],
  shipped: ['已发货', 'primary'],
  closed: ['已关闭', 'info'],
  refund_review: ['退款审核中', 'warning'],
  refunding: ['退款中', 'warning'],
  refunded: ['已退款', 'danger'],
}
function statusName(s) { return statusMap[s]?.[0] || s }
function statusType(s) { return statusMap[s]?.[1] || 'info' }

function openPay(row) {
  pay.order = row
  pay.visible = true
}
async function payScene(scenario) {
  try {
    const res = await api.channelPay(pay.order.order_no, pay.order.amount, scenario, 0)
    ElMessage.success(`渠道已受理（交易号 ${res.trade_no}），稍后刷新查看状态`)
    pay.visible = false
    setTimeout(load, 800)
    setTimeout(load, 2500) // 延迟/重复场景兜底再刷一次
  } catch (e) {
    ElMessage.error(e.message)
  }
}

async function cancel(row) {
  try {
    await ElMessageBox.confirm(`确定取消订单 ${row.order_no}？库存将立即释放。`, '取消订单', { type: 'warning' })
    await api.cancelOrder(row.order_no)
    ElMessage.success('已取消')
    load()
  } catch (e) {
    if (e !== 'cancel') ElMessage.error(e.message)
  }
}

function openRefund(row) {
  refund.order = row
  refund.reason = ''
  refund.visible = true
}
async function submitRefund() {
  if (!refund.reason.trim()) {
    ElMessage.warning('请填写退款原因')
    return
  }
  refund.loading = true
  try {
    await api.applyRefund(refund.order.order_no, refund.reason)
    ElMessage.success('已提交，等待客服审核（审核期间暂停发货）')
    refund.visible = false
    load()
  } catch (e) {
    ElMessage.error(e.message)
  } finally {
    refund.loading = false
  }
}

async function showShip(row) {
  try {
    const o = await api.getOrder(row.order_no)
    ElMessageBox.alert(
      o.shipment
        ? `承运方：${o.shipment.carrier}\n运单号：${o.shipment.tracking_no}\n发货时间：${fmt(o.shipment.created_at)}`
        : '仓库已标记发货，物流信息同步中',
      '物流信息',
    )
  } catch (e) {
    ElMessage.error(e.message)
  }
}

onMounted(() => {
  load()
  timer = setInterval(() => { now.value = Date.now() }, 1000)
})
onUnmounted(() => clearInterval(timer))
</script>

<style scoped>
.bar { display: flex; justify-content: space-between; align-items: center; }
.countdown { color: #f56c6c; font-weight: 600; font-variant-numeric: tabular-nums; }
.dim { color: #909399; font-size: 13px; }
.scenes { display: flex; flex-wrap: wrap; gap: 8px; }
.scenes .el-button { margin-left: 0; }
</style>
