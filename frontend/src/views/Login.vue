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
      <div class="ui-card p-7">
        <h2 class="text-lg font-semibold text-slate-800 mb-4">登录</h2>

        <form @submit.prevent="handleLogin" class="space-y-4">
          <div>
            <label class="block text-sm font-medium text-slate-700 mb-1">用户名</label>
            <input
              v-model="username"
              type="text"
              required
              autocomplete="username"
              class="ui-input"
              placeholder="admin"
            />
          </div>

          <div>
            <label class="block text-sm font-medium text-slate-700 mb-1">密码</label>
            <input
              v-model="password"
              type="password"
              required
              autocomplete="current-password"
              class="ui-input"
              placeholder="••••••••"
            />
          </div>

          <div v-if="error" class="text-sm text-red-600 bg-red-50 border border-red-200 rounded px-3 py-2">
            {{ error }}
          </div>

          <button
            type="submit"
            :disabled="loading"
            class="ui-button-primary w-full disabled:opacity-50 disabled:cursor-not-allowed"
          >
            <span v-if="loading">登录中...</span>
            <span v-else>登录</span>
          </button>
        </form>
      </div>

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
