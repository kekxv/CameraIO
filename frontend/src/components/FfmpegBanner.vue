<template>
  <div v-if="status && status.state && status.state !== 'installed'" class="flex-shrink-0">
    <!-- 下载中 -->
    <div v-if="status.state === 'downloading'" class="bg-blue-50 border-b border-blue-100 px-4 py-2">
      <div class="flex items-center gap-3">
        <svg class="w-4 h-4 text-primary-500 animate-spin flex-shrink-0" xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24">
          <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
          <path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
        </svg>
        <div class="flex-1 min-w-0">
          <div class="flex items-center justify-between gap-4 mb-1">
            <span class="text-sm text-slate-700 font-medium truncate">FFmpeg 下载中，完成后流媒体功能即可使用</span>
            <span class="text-xs text-slate-500 font-mono flex-shrink-0">{{ progressText }}</span>
          </div>
          <div class="h-1.5 bg-slate-200 rounded-full overflow-hidden">
            <div class="h-full bg-primary-500 rounded-full transition-all duration-300" :style="{ width: status.progress + '%' }"></div>
          </div>
        </div>
      </div>
    </div>

    <!-- 解压中 -->
    <div v-else-if="status.state === 'extracting'" class="bg-blue-50 border-b border-blue-100 px-4 py-2.5 flex items-center gap-3">
      <svg class="w-4 h-4 text-primary-500 animate-spin flex-shrink-0" xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24">
        <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
        <path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
      </svg>
      <span class="text-sm text-slate-700">FFmpeg 下载完成，正在解压...</span>
    </div>

    <!-- 检查中 -->
    <div v-else-if="status.state === 'checking'" class="bg-blue-50 border-b border-blue-100 px-4 py-2.5 flex items-center gap-3">
      <svg class="w-4 h-4 text-primary-500 animate-spin flex-shrink-0" xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24">
        <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
        <path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
      </svg>
      <span class="text-sm text-slate-700">正在检查 FFmpeg...</span>
    </div>

    <!-- 错误 -->
    <div v-else-if="status.state === 'error'" class="ui-alert border-x-0 border-t-0 rounded-none px-4 py-2.5 gap-3">
      <AppIcon name="warning" class="w-4 h-4 text-red-500 flex-shrink-0" />
      <span class="text-sm text-red-700">FFmpeg 不可用：{{ status.error }}。可手动安装 FFmpeg 或设置 <code class="font-mono text-xs bg-red-100 px-1 rounded">CAMERAIO_FFMPEG_PATH</code> 环境变量。</span>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, onMounted, onUnmounted } from 'vue'
import { getFFmpegStatus } from '../api'
import AppIcon from './AppIcon.vue'

const status = ref(null)
let timer = null
let disposed = false

const progressText = computed(() => {
  if (!status.value) return ''
  const s = status.value
  if (s.total > 0) {
    return `${formatMB(s.downloaded)} / ${formatMB(s.total)} (${s.progress}%)`
  }
  return `${formatMB(s.downloaded)}`
})

function formatMB(bytes) {
  return (bytes / 1024 / 1024).toFixed(0) + ' MB'
}

// 仅在需要时轮询（下载/解压/错误），安装完成后停止，避免无效请求。
function isActive() {
  return status.value && status.value.state !== 'installed' && status.value.state !== ''
}

async function refresh() {
  try {
    status.value = await getFFmpegStatus()
  } catch (e) {
    // 未登录或网络错误：静默处理，稍后重试
  }
  if (disposed) return
  if (isActive()) {
    clearTimeout(timer)
    timer = setTimeout(refresh, 2000)
  }
}

onMounted(() => {
  disposed = false
  refresh()
})

onUnmounted(() => {
  disposed = true
  clearTimeout(timer)
})
</script>
