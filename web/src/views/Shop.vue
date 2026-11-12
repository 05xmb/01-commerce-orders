<template>
  <div>
    <div class="bar">
      <h3>商品列表</h3>
      <el-button :loading="loading" @click="load">刷新</el-button>
    </div>
    <el-alert type="info" :closable="false" class="tip"
      title="活动商品库存有限，下单后须在倒计时内完成支付，超时自动关单释放库存" />
    <el-row :gutter="16">
      <el-col v-for="p in products" :key="p.id" :span="8">
        <el-card class="prod">
          <h4>{{ p.name }}</h4>
          <div class="price">¥{{ (p.price / 100).toFixed(2) }}</div>
          <div class="stock">
            可售
            <span :class="{ zero: p.stock === 0 }">{{ p.stock }}</span>
            件
          </div>
          <div class="buy">
            <el-input-number v-model="qty[p.id]" :min="1" :max="2" size="small" />
            <el-button type="primary" :disabled="p.stock === 0" :loading="buying === p.id" @click="buy(p)">
              {{ p.stock === 0 ? '已抢完' : '立即抢购' }}
            </el-button>
          </div>
        </el-card>
      </el-col>
    </el-row>
    <el-empty v-if="!loading && products.length === 0" description="暂无上架商品" />
  </div>
</template>

<script setup>
import { onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { api } from '../api'

const emit = defineEmits(['ordered'])
const products = ref([])
const qty = reactive({})
const loading = ref(false)
const buying = ref('')

async function load() {
  loading.value = true
  try {
    products.value = await api.products()
    products.value.forEach((p) => { if (!qty[p.id]) qty[p.id] = 1 })
  } catch (e) {
    ElMessage.error(e.message)
  } finally {
    loading.value = false
  }
}

async function buy(p) {
  buying.value = p.id
  try {
    const order = await api.createOrder(p.id, qty[p.id])
    await ElMessageBox.confirm(
      `订单 ${order.order_no} 已创建，金额 ¥${(order.amount / 100).toFixed(2)}，请在倒计时内完成支付。`,
      '下单成功',
      { confirmButtonText: '去支付', cancelButtonText: '继续逛逛', type: 'success' },
    ).then(() => emit('ordered')).catch(() => {})
    load()
  } catch (e) {
    ElMessage.error(e.message)
    load() // 抢购失败（如售罄）后刷新可售数
  } finally {
    buying.value = ''
  }
}

onMounted(load)
</script>

<style scoped>
.bar { display: flex; justify-content: space-between; align-items: center; }
.tip { margin-bottom: 16px; }
.prod { margin-bottom: 16px; }
.price { font-size: 22px; color: #f56c6c; font-weight: 600; margin: 8px 0; }
.stock { color: #909399; margin-bottom: 12px; }
.stock .zero { color: #f56c6c; font-weight: 600; }
.buy { display: flex; gap: 10px; align-items: center; }
</style>
