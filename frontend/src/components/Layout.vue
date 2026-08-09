<template>
  <div class="app-shell layout-root">
    <header class="layout-mobile-header">
      <el-button plain circle class="layout-mobile-toggle" native-type="button" aria-label="切换导航" :aria-expanded="mobileNavOpen" @click="mobileNavOpen = !mobileNavOpen">
        <span class="layout-menu-lines" aria-hidden="true"></span>
      </el-button>
      <div class="layout-mobile-brand"><span>Camera</span>IO</div>
      <el-button plain class="layout-mobile-user" native-type="button" @click="handleLogout">退出</el-button>
    </header>

    <el-container class="layout-desktop-shell">
      <el-aside width="240px" class="layout-sidebar" :class="{ 'layout-sidebar--open': mobileNavOpen }">
        <div class="layout-brand">
          <h1><span>Camera</span>IO</h1>
          <p>轻量级 NVR 控制台</p>
        </div>
        <el-menu class="layout-nav" :default-active="$route.path" router aria-label="主导航" @select="mobileNavOpen = false">
          <el-menu-item v-for="item in navItems" :key="item.to" :index="item.to" class="layout-nav-link">
            <AppIcon :name="item.icon" class="w-5 h-5 flex-shrink-0" />
            <span>{{ item.label }}</span>
          </el-menu-item>
        </el-menu>
        <div class="layout-account">
          <div class="compat-flex-gap-2 layout-account-name"><span class="layout-account-dot"></span><span>{{ username }}</span></div>
          <el-button plain class="layout-logout" native-type="button" @click="handleLogout">退出登录</el-button>
        </div>
      </el-aside>

      <el-main class="layout-main">
        <FfmpegBanner />
        <div class="page-frame"><router-view /></div>
      </el-main>
    </el-container>
  </div>
</template>

<script setup>
import { computed, ref } from 'vue'
import { getCurrentUser, logout } from '../api'
import FfmpegBanner from './FfmpegBanner.vue'
import AppIcon from './AppIcon.vue'

const navItems = [
  { to: '/cameras', label: '摄像头管理', icon: 'camera' },
  { to: '/live', label: '实时预览', icon: 'monitor' },
  { to: '/recordings', label: '录像中心', icon: 'film' },
]
const mobileNavOpen = ref(false)
const currentUser = computed(() => getCurrentUser())
const username = computed(() => {
  const user = currentUser.value
  return user && user.username ? user.username : 'Guest'
})
const handleLogout = () => logout()
</script>
