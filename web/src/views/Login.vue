<template>
  <div class="login-page">
    <el-card class="login-card">
      <h2>电商交易系统</h2>
      <p class="tip">题目01：订单交易、发货与售前退款（演示账号密码均为 123456）</p>
      <el-form @submit.prevent="submit">
        <el-form-item>
          <el-input v-model="username" placeholder="用户名" autofocus />
        </el-form-item>
        <el-form-item>
          <el-input v-model="password" type="password" placeholder="密码" show-password />
        </el-form-item>
        <el-button type="primary" class="full" :loading="loading" @click="submit">登录</el-button>
      </el-form>
      <div class="quick">
        <span>快捷选择：</span>
        <el-tag v-for="u in presets" :key="u" class="qk" @click="pick(u)">{{ u }}</el-tag>
      </div>
      <el-alert v-if="error" :title="error" type="error" show-icon :closable="false" class="err" />
    </el-card>
  </div>
</template>

<script setup>
import { ref } from 'vue'
import { api, setSession, getSession } from '../api'

const emit = defineEmits(['logged'])
const presets = ['buyer01', 'buyer02', 'buyer03', 'op01', 'wh01', 'cs01']
const username = ref('')
const password = ref('123456')
const loading = ref(false)
const error = ref('')

function pick(u) {
  username.value = u
  password.value = '123456'
}
async function submit() {
  if (!username.value || !password.value) {
    error.value = '请输入用户名和密码'
    return
  }
  loading.value = true
  error.value = ''
  try {
    const res = await api.login(username.value, password.value)
    setSession(res.token, res.user)
    emit('logged', getSession())
  } catch (e) {
    error.value = e.message
  } finally {
    loading.value = false
  }
}
</script>

<style scoped>
.login-page { display: flex; justify-content: center; padding-top: 12vh; }
.login-card { width: 380px; }
.tip { color: #909399; font-size: 13px; }
.full { width: 100%; }
.quick { margin-top: 14px; display: flex; flex-wrap: wrap; gap: 6px; align-items: center; }
.qk { cursor: pointer; }
.err { margin-top: 12px; }
</style>
