<template>
  <el-config-provider>
    <div class="app">
      <Login v-if="!session.user" @logged="onLogged" />
      <template v-else>
        <el-header class="topbar">
          <span class="brand">电商交易系统（题目01）</span>
          <el-menu mode="horizontal" :default-active="tab" @select="tab = $event" class="nav">
            <template v-if="session.user.role === 'buyer'">
              <el-menu-item index="shop">商品列表</el-menu-item>
              <el-menu-item index="orders">我的订单</el-menu-item>
            </template>
            <template v-else>
              <el-menu-item index="admin">工作台</el-menu-item>
            </template>
          </el-menu>
          <div class="who">
            <el-tag size="small" effect="plain">{{ roleName(session.user.role) }}</el-tag>
            <span>{{ session.user.name || session.user.username }}</span>
            <el-button size="small" text @click="logout">退出</el-button>
          </div>
        </el-header>
        <el-main>
          <Shop v-if="tab === 'shop'" @ordered="tab = 'orders'" />
          <Orders v-else-if="tab === 'orders'" />
          <Admin v-else-if="tab === 'admin'" :role="session.user.role" />
        </el-main>
      </template>
    </div>
  </el-config-provider>
</template>

<script setup>
import { ref, reactive } from 'vue'
import { getSession, clearSession } from './api'
import Login from './views/Login.vue'
import Shop from './views/Shop.vue'
import Orders from './views/Orders.vue'
import Admin from './views/Admin.vue'

const session = reactive(getSession())
const tab = ref(session.user?.role === 'buyer' ? 'shop' : 'admin')

function onLogged() {
  tab.value = session.user.role === 'buyer' ? 'shop' : 'admin'
}
function logout() {
  clearSession()
  session.user = null
  session.token = ''
}
function roleName(r) {
  return { buyer: '买家', op: '运营', wh: '仓库', cs: '客服' }[r] || r
}
</script>

<style>
body { margin: 0; background: #f5f7fa; font-family: 'Helvetica Neue', Arial, sans-serif; }
.topbar { display: flex; align-items: center; background: #fff; border-bottom: 1px solid #e4e7ed; padding: 0 20px; }
.brand { font-weight: 600; margin-right: 24px; white-space: nowrap; }
.nav { flex: 1; border-bottom: none; }
.who { display: flex; align-items: center; gap: 8px; }
</style>
