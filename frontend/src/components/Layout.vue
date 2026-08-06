<template>
  <div class="flex h-screen bg-slate-50">
    <!-- 侧边栏 -->
    <aside class="w-56 bg-slate-900 text-slate-100 flex flex-col">
      <div class="px-5 py-4 border-b border-slate-800">
        <h1 class="text-lg font-bold tracking-tight">
          <span class="text-primary-400">Camera</span><span class="text-white">IO</span>
        </h1>
        <p class="text-[11px] text-slate-400 mt-0.5">轻量级 NVR 控制台</p>
      </div>

      <nav class="flex-1 px-3 py-4 space-y-1">
        <router-link
          v-for="item in navItems"
          :key="item.to"
          :to="item.to"
          class="flex items-center gap-3 px-3 py-2 rounded-md text-sm transition-colors"
          :class="$route.path === item.to
            ? 'bg-primary-600 text-white'
            : 'text-slate-300 hover:bg-slate-800 hover:text-white'"
        >
          <span class="text-base" v-html="item.icon"></span>
          <span>{{ item.label }}</span>
        </router-link>
      </nav>

      <div class="px-3 py-3 border-t border-slate-800">
        <div class="flex items-center gap-2 px-2 py-1.5 text-xs text-slate-400">
          <span class="w-2 h-2 rounded-full bg-emerald-400 animate-pulse"></span>
          <span>{{ currentUser?.username || 'Guest' }}</span>
        </div>
        <button
          @click="handleLogout"
          class="mt-2 w-full px-3 py-1.5 text-xs rounded text-slate-400 hover:bg-slate-800 hover:text-white transition-colors"
        >
          退出登录
        </button>
      </div>
    </aside>

    <!-- 主内容区 -->
    <main class="flex-1 overflow-auto">
      <router-view />
    </main>
  </div>
</template>

<script setup>
import { computed } from 'vue'
import { getCurrentUser, logout } from '../api'

const navItems = [
  { to: '/cameras', label: '摄像头管理', icon: '📷' },
  { to: '/live', label: '实时预览', icon: '📺' },
  { to: '/recordings', label: '录像中心', icon: '🎬' },
]

const currentUser = computed(() => getCurrentUser())

const handleLogout = () => {
  logout()
}
</script>
