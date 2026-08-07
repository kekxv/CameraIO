<template>
  <div class="app-shell layout-root">
    <header class="layout-mobile-header">
      <button class="ui-icon-button" type="button" aria-label="切换导航" :aria-expanded="mobileNavOpen" @click="mobileNavOpen = !mobileNavOpen">
        <span class="layout-menu-lines" aria-hidden="true"></span>
      </button>
      <div class="layout-mobile-brand"><span>Camera</span>IO</div>
      <button class="layout-mobile-user" type="button" @click="handleLogout">退出</button>
    </header>

    <aside class="layout-sidebar" :class="{ 'layout-sidebar--open': mobileNavOpen }">
      <div class="layout-brand">
        <h1><span>Camera</span>IO</h1>
        <p>轻量级 NVR 控制台</p>
      </div>
      <nav class="layout-nav" aria-label="主导航">
        <router-link v-for="item in navItems" :key="item.to" :to="item.to" class="layout-nav-link compat-flex-gap-3" :class="{ 'layout-nav-link--active': $route.path === item.to }" @click="mobileNavOpen = false">
          <AppIcon :name="item.icon" class="w-5 h-5 flex-shrink-0" />
          <span>{{ item.label }}</span>
        </router-link>
      </nav>
      <div class="layout-account">
        <div class="compat-flex-gap-2 layout-account-name"><span class="layout-account-dot"></span><span>{{ username }}</span></div>
        <button class="ui-button-secondary layout-logout" type="button" @click="handleLogout">退出登录</button>
      </div>
    </aside>

    <main class="layout-main">
      <FfmpegBanner />
      <div class="page-frame"><router-view /></div>
    </main>
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
