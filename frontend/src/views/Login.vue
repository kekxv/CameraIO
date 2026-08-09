<template>
  <div class="login-page">
    <div class="w-full max-w-sm">
      <!-- Logo -->
      <div class="text-center mb-8">
        <h1 class="text-3xl font-bold tracking-tight text-slate-800">
          <span class="text-primary-600">Camera</span><span>IO</span>
        </h1>
        <p class="text-slate-500 text-sm mt-2">轻量级无延迟网络视频录像系统</p>
      </div>

      <!-- 登录卡片 -->
      <el-card class="login-card">
        <h2 class="text-lg font-semibold text-slate-800 mb-4">登录</h2>

        <el-form @submit.prevent="handleLogin" class="login-form">
          <el-form-item label="用户名">
            <el-input
              v-model="username"
              type="text"
              required
              autocomplete="username"
              placeholder="admin"
            />
          </el-form-item>

          <el-form-item label="密码">
            <el-input
              v-model="password"
              type="password"
              required
              autocomplete="current-password"
              placeholder="••••••••"
              show-password
            />
          </el-form-item>

          <el-alert v-if="error" :title="error" type="error" :closable="false" show-icon />

          <el-button
            native-type="submit"
            type="primary"
            :disabled="loading"
            class="w-full"
          >
            <span v-if="loading">登录中...</span>
            <span v-else>登录</span>
          </el-button>
        </el-form>
      </el-card>

      <p class="text-center text-xs text-slate-500 mt-4">
        默认账户: admin / admin
      </p>
    </div>
  </div>
</template>

<script setup>
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import { login } from '../api'

const router = useRouter()
const username = ref('')
const password = ref('')
const error = ref('')
const loading = ref(false)

const handleLogin = async () => {
  error.value = ''
  loading.value = true
  try {
    await login(username.value, password.value)
    router.push('/cameras')
  } catch (err) {
    error.value = err.response?.data?.message || '登录失败，请检查用户名和密码'
  } finally {
    loading.value = false
  }
}
</script>
