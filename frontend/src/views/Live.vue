<template>
  <div class="h-screen flex flex-col bg-slate-50 text-slate-800 overflow-hidden">
    <!-- 顶部控制栏 -->
    <div class="flex items-center justify-between px-4 py-2.5 bg-white border-b border-slate-200 flex-shrink-0 shadow-sm">
      <div class="flex items-center gap-4">
        <h1 class="text-base font-semibold text-slate-800">实时预览</h1>
        <div class="flex items-center gap-1.5 text-xs text-slate-500">
          <span class="w-1.5 h-1.5 rounded-full" :class="onlineCount > 0 ? 'bg-emerald-500' : 'bg-slate-300'"></span>
          {{ onlineCount }} / {{ cameras.length }} 在线
        </div>
      </div>
      <!-- 当前时间（用于判断延迟） -->
      <div class="flex items-center gap-2 text-xs text-slate-600 font-mono">
        <svg xmlns="http://www.w3.org/2000/svg" class="w-3.5 h-3.5 text-slate-400" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
          <path stroke-linecap="round" stroke-linejoin="round" d="M12 8v4l3 3m6-3a9 9 0 11-18 0 9 9 0 0118 0z" />
        </svg>
        <span>{{ nowStr }}</span>
      </div>
      <div class="flex items-center gap-3 ml-auto">
        <!-- 网格切换 -->
        <div class="flex items-center gap-0.5 bg-slate-100 rounded-lg p-0.5">
          <button
            v-for="n in [1, 4, 9, 16]"
            :key="n"
            @click="gridSize = n"
            class="px-2.5 py-1 rounded-md text-xs font-medium transition-all"
            :class="gridSize === n ? 'bg-white text-primary-700 shadow-sm' : 'text-slate-500 hover:text-slate-700'"
          >
            {{ gridLabel(n) }}
          </button>
        </div>
        <!-- 全屏切换 -->
        <button @click="toggleFullscreen" class="p-1.5 text-slate-400 hover:text-slate-600 rounded-md hover:bg-slate-100 transition-colors" title="全屏 (F)">
          <svg xmlns="http://www.w3.org/2000/svg" class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
            <path stroke-linecap="round" stroke-linejoin="round" d="M4 8V4m0 0h4M4 4l5 5m11-5h-4m4 0v4m0 0l-5-5m-7 14H4m0 0v-4m0 4l5-5m7 5h4m0 0v-4m0 4l-5-5" />
          </svg>
        </button>
      </div>
    </div>

    <!-- 视频网格区域 -->
    <div class="flex-1 p-2.5 min-h-0">
      <!-- 加载状态 -->
      <div v-if="loading" class="h-full flex items-center justify-center text-slate-400">
        <div class="text-center">
          <div class="inline-block w-8 h-8 border-2 border-primary-500 border-t-transparent rounded-full animate-spin mb-3"></div>
          <p class="text-sm">加载中...</p>
        </div>
      </div>

      <!-- 空状态 -->
      <div v-else-if="cameras.length === 0" class="h-full flex items-center justify-center text-slate-400">
        <div class="text-center">
          <AppIcon name="camera" class="w-14 h-14 mx-auto mb-3 text-slate-300" />
          <p class="text-sm text-slate-500">暂无摄像头</p>
          <router-link to="/cameras" class="text-primary-600 hover:text-primary-500 text-sm mt-2 inline-block">去添加 →</router-link>
        </div>
      </div>

      <!-- 视频网格 -->
      <div v-else class="h-full grid gap-2" :style="gridStyle">
        <div
          v-for="cam in visibleCameras"
          :key="cam.id"
          class="relative rounded-xl overflow-hidden border border-slate-200 bg-slate-100 group shadow-sm hover:shadow-md transition-shadow"
          :class="{ 'ring-2 ring-primary-500/30': streaming[cam.id] }"
        >
          <!-- 视频画面 -->
          <div class="absolute inset-0 flex items-center justify-center">
            <img
              v-if="streaming[cam.id]"
              :src="getMjpegUrl(cam.id)"
              class="w-full h-full object-contain"
              @error="handleStreamError(cam.id)"
            />
            <div v-else class="flex flex-col items-center justify-center gap-3 text-center">
              <div v-if="cam.status === 'online'" class="flex flex-col items-center gap-3">
                <div class="w-14 h-14 rounded-full bg-white shadow-sm border border-slate-200 flex items-center justify-center">
                  <svg xmlns="http://www.w3.org/2000/svg" class="w-6 h-6 text-slate-400" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
                    <path stroke-linecap="round" stroke-linejoin="round" d="M14.752 11.168l-3.197-2.132A1 1 0 0010 9.87v4.263a1 1 0 001.555.832l3.197-2.132a1 1 0 000-1.664z" />
                    <path stroke-linecap="round" stroke-linejoin="round" d="M21 12a9 9 0 11-18 0 9 9 0 0118 0z" />
                  </svg>
                </div>
                <button
                  @click="startStreamFor(cam.id)"
                  class="px-5 py-2 bg-primary-600 hover:bg-primary-500 text-white rounded-lg text-sm font-medium transition-colors shadow-sm"
                >
                  开始预览
                </button>
              </div>
              <div v-else class="flex flex-col items-center gap-2 text-slate-400">
                <div class="w-12 h-12 rounded-full bg-white shadow-sm border border-slate-200 flex items-center justify-center">
                  <svg xmlns="http://www.w3.org/2000/svg" class="w-6 h-6 text-slate-400" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.5">
                    <path stroke-linecap="round" stroke-linejoin="round" d="M18.364 18.364A9 9 0 005.636 5.636m12.728 12.728A9 9 0 015.636 5.636m12.728 12.728L5.636 5.636" />
                  </svg>
                </div>
                <span class="text-xs">离线</span>
              </div>
            </div>
          </div>

          <!-- 加载中覆盖 -->
          <transition name="fade">
            <div v-if="starting[cam.id]" class="absolute inset-0 bg-white/80 flex items-center justify-center z-10 backdrop-blur-sm">
              <div class="flex flex-col items-center gap-2">
                <div class="w-7 h-7 border-2 border-primary-500 border-t-transparent rounded-full animate-spin"></div>
                <span class="text-xs text-primary-600">连接中...</span>
              </div>
            </div>
          </transition>

          <!-- 顶部信息条 -->
          <div class="absolute top-0 left-0 right-0 px-2.5 py-1.5 bg-slate-900/70 backdrop-blur-sm flex items-center justify-between z-5">
            <div class="flex items-center gap-1.5 min-w-0">
              <span class="w-1.5 h-1.5 rounded-full flex-shrink-0" :class="cam.status === 'online' ? 'bg-emerald-400' : 'bg-slate-500'"></span>
              <span class="text-xs text-white truncate font-medium">{{ cam.name }}</span>
            </div>
            <span v-if="streaming[cam.id]" class="flex items-center gap-1 text-[10px] text-red-300">
              <span class="w-1 h-1 rounded-full bg-red-400 animate-pulse"></span>
              LIVE
            </span>
          </div>

          <!-- 底部控制条 -->
          <div class="absolute bottom-0 left-0 right-0 px-2.5 py-1.5 bg-slate-900/70 backdrop-blur-sm flex items-center justify-between z-5">
            <div class="flex items-center gap-1.5">
              <span class="text-[10px] text-white/60 font-mono">{{ cam.ip }}</span>
            </div>
            <div class="flex items-center gap-0.5">
              <button
                v-if="streaming[cam.id]"
                @click="stopStreamFor(cam.id)"
                class="p-1 rounded-md text-slate-300 hover:text-white hover:bg-white/15 transition-colors"
                title="停止预览"
              >
                <svg xmlns="http://www.w3.org/2000/svg" class="w-3.5 h-3.5" viewBox="0 0 20 20" fill="currentColor">
                  <rect x="4" y="4" width="12" height="12" rx="1.5" />
                </svg>
              </button>
              <button
                @click="takeSnapshot(cam)"
                :disabled="capturing[cam.id]"
                class="p-1 rounded-md text-slate-300 hover:text-white hover:bg-white/15 transition-colors disabled:opacity-50"
                :title="capturing[cam.id] ? '抓拍中...' : '原生抓拍'"
              >
                <span v-if="capturing[cam.id]" class="block w-3.5 h-3.5 border-2 border-current border-t-transparent rounded-full animate-spin"></span>
                <AppIcon v-else name="camera" class="w-3.5 h-3.5" />
              </button>
              <button
                @click="toggleRecord(cam)"
                :disabled="recording[cam.id] === 'toggling'"
                class="p-1 rounded-md transition-colors"
                :class="recording[cam.id] === 'active' ? 'text-red-400 bg-red-500/20' : 'text-slate-300 hover:text-white hover:bg-white/15'"
                :title="recording[cam.id] === 'active' ? '停止录像' : '开始录像'"
              >
                <svg xmlns="http://www.w3.org/2000/svg" class="w-3.5 h-3.5" viewBox="0 0 20 20" fill="currentColor">
                  <circle cx="10" cy="10" r="6" />
                </svg>
              </button>
            </div>
          </div>
        </div>
      </div>
    </div>

    <!-- 录像设置弹窗 -->
    <transition name="fade">
      <div v-if="showRecordDialog" class="fixed inset-0 bg-black/40 flex items-center justify-center z-50" @click.self="showRecordDialog = false">
        <div class="bg-white rounded-xl shadow-xl w-80 p-5">
          <h3 class="text-sm font-semibold text-slate-800 mb-3 flex items-center gap-1.5">
            <AppIcon name="film" class="w-4 h-4" />
            <span>录像设置</span>
          </h3>
          <p class="text-xs text-slate-500 mb-3">{{ recordTarget?.name }} · {{ recordTarget?.ip }}</p>

          <!-- 格式选择 -->
          <div class="mb-3">
            <label class="block text-xs font-medium text-slate-600 mb-1.5">录像格式</label>
            <div class="flex rounded-lg border border-slate-200 overflow-hidden">
              <button
                v-for="fmt in [{v:'mp4',l:'MP4'},{v:'webm',l:'WebM'},{v:'ts',l:'TS'}]"
                :key="fmt.v"
                @click="recordFormat = fmt.v"
                class="flex-1 py-1.5 text-xs font-medium transition-colors border-r border-slate-200 last:border-r-0"
                :class="recordFormat === fmt.v ? 'bg-primary-600 text-white' : 'bg-white text-slate-600 hover:bg-slate-50'"
              >
                {{ fmt.l }}
              </button>
            </div>
            <p class="text-[10px] text-slate-400 mt-1">
              {{ recordFormat === 'mp4' ? '兼容性最好，推荐' : recordFormat === 'webm' ? '体积小，浏览器原生播放' : '适合直播流，无 moov 问题' }}
            </p>
          </div>

          <!-- 码率选项 -->
          <div class="mb-3">
            <label class="block text-xs font-medium text-slate-600 mb-1.5">码率（控制文件大小）</label>
            <div class="flex rounded-lg border border-slate-200 overflow-hidden">
              <button
                v-for="opt in bitrateOptions"
                :key="opt.value"
                @click="recordBitrate = opt.value"
                class="flex-1 py-1.5 text-xs font-medium transition-colors border-r border-slate-200 last:border-r-0"
                :class="recordBitrate === opt.value ? 'bg-primary-600 text-white' : 'bg-white text-slate-600 hover:bg-slate-50'"
              >
                {{ opt.label }}
              </button>
            </div>
            <p class="text-[10px] text-slate-400 mt-1">
              {{ recordBitrate === 0 ? '原画质（相机码率，体积大）' : `约 ${(recordBitrate * 600 / 8 / 1024).toFixed(1)}MB/10分钟` }}
            </p>
          </div>

          <!-- 音频开关 -->
          <label class="flex items-center gap-2 mb-4 cursor-pointer">
            <input v-model="recordWithAudio" type="checkbox" class="rounded text-primary-600" />
            <span class="text-xs text-slate-700">包含音频</span>
          </label>

          <!-- 操作按钮 -->
          <div class="flex justify-end gap-2">
            <button @click="showRecordDialog = false" class="px-3 py-1.5 text-xs text-slate-500 hover:bg-slate-100 rounded-md">取消</button>
            <button @click="confirmStartRecording" class="px-4 py-1.5 bg-red-600 hover:bg-red-700 text-white rounded-md text-xs font-medium inline-flex items-center gap-1.5">
              <AppIcon name="record" class="w-3.5 h-3.5" />
              <span>开始录像</span>
            </button>
          </div>
        </div>
      </div>
    </transition>

    <!-- 原生抓拍结果 -->
    <transition name="fade">
      <div v-if="snapshotTarget && snapshotURL" class="fixed inset-0 bg-black/60 flex items-center justify-center z-50 p-5" @click.self="closeSnapshot">
        <div class="bg-white rounded-xl shadow-xl w-full max-w-4xl max-h-full flex flex-col overflow-hidden">
          <div class="px-4 py-3 border-b border-slate-200 flex items-center justify-between">
            <div>
              <h3 class="text-sm font-semibold text-slate-800">原生抓拍</h3>
              <p class="text-xs text-slate-500 mt-0.5">{{ snapshotTarget.name }} · {{ snapshotTarget.ip }}</p>
            </div>
            <button @click="closeSnapshot" class="p-1.5 text-slate-400 hover:text-slate-600 hover:bg-slate-100 rounded-md" title="关闭">
              <AppIcon name="close" class="w-4 h-4" />
            </button>
          </div>
          <div class="min-h-0 p-4 bg-slate-100 flex items-center justify-center">
            <img :src="snapshotURL" :alt="`${snapshotTarget.name} 抓拍`" class="max-w-full max-h-[70vh] object-contain rounded shadow-sm" />
          </div>
        </div>
      </div>
    </transition>
  </div>
</template>

<script setup>
import { ref, computed, onMounted, onUnmounted } from 'vue'
import { listCameras, startStream, stopStream, startRecording, stopRecording, captureSnapshot, getAPIErrorMessage, connectEventBus } from '../api'
import AppIcon from '../components/AppIcon.vue'

const cameras = ref([])
const loading = ref(true)
const gridSize = ref(4)
const streaming = ref({})
const starting = ref({})
const recording = ref({})
const recordingIdMap = ref({})
const capturing = ref({})
const snapshotTarget = ref(null)
const snapshotURL = ref('')
const showRecordDialog = ref(false)
const recordTarget = ref(null)
const recordFormat = ref('mp4')
const recordWithAudio = ref(false)
const recordBitrate = ref(0) // kbps, 0=流拷贝原画质
const bitrateOptions = [
  { value: 0, label: '原画质' },
  { value: 512, label: '512k' },
  { value: 1000, label: '1M' },
  { value: 2000, label: '2M' },
]
const nowStr = ref('')
let clockTimer = null
let eventWs = null

// 每秒更新当前时间
const updateClock = () => {
  const d = new Date()
  const pad = (n) => String(n).padStart(2, '0')
  nowStr.value = `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`
}
updateClock()

const onlineCount = computed(() => cameras.value.filter((c) => c.status === 'online').length)
const visibleCameras = computed(() => cameras.value.slice(0, gridSize.value))

const gridStyle = computed(() => {
  const n = gridSize.value
  const cols = n === 1 ? 1 : n <= 4 ? 2 : n <= 9 ? 3 : 4
  const rows = Math.ceil(n / cols)
  return {
    gridTemplateColumns: `repeat(${cols}, 1fr)`,
    gridTemplateRows: `repeat(${rows}, 1fr)`,
  }
})

const gridLabel = (n) => {
  if (n === 1) return '1×1'
  if (n === 4) return '2×2'
  if (n === 9) return '3×3'
  return '4×4'
}

// MJPEG URL 缓存：每个摄像头只生成一次，避免重渲染导致重新连接
const mjpegUrls = {}
const getMjpegUrl = (cameraId) => {
  if (mjpegUrls[cameraId]) return mjpegUrls[cameraId]
  const token = localStorage.getItem('token')
  // t 只用一次防止浏览器缓存，之后保持不变（MJPEG 是持续推送的）
  mjpegUrls[cameraId] = `/api/v1/streams/${cameraId}/mjpeg?token=${token}&t=${Date.now()}`
  return mjpegUrls[cameraId]
}

const loadCameras = async () => {
  loading.value = true
  try {
    cameras.value = await listCameras()
  } finally {
    loading.value = false
  }
}

const startStreamFor = async (cameraId) => {
  starting.value[cameraId] = true
  try {
    await startStream(cameraId)
    // 清除缓存的 URL，让浏览器重新建立 MJPEG 连接
    delete mjpegUrls[cameraId]
    streaming.value[cameraId] = true
  } catch (err) {
    console.error('start stream failed:', err)
  } finally {
    starting.value[cameraId] = false
  }
}

const stopStreamFor = async (cameraId) => {
  try {
    await stopStream(cameraId)
  } finally {
    delete mjpegUrls[cameraId]
    streaming.value[cameraId] = false
  }
}

const handleStreamError = (cameraId) => {
  streaming.value[cameraId] = false
}

const closeSnapshot = () => {
  if (snapshotURL.value) URL.revokeObjectURL(snapshotURL.value)
  snapshotURL.value = ''
  snapshotTarget.value = null
}

const takeSnapshot = async (cam) => {
  capturing.value[cam.id] = true
  try {
    const jpeg = await captureSnapshot(cam.id)
    closeSnapshot()
    snapshotURL.value = URL.createObjectURL(jpeg)
    snapshotTarget.value = cam
  } catch (err) {
    alert('抓拍失败: ' + await getAPIErrorMessage(err))
  } finally {
    delete capturing.value[cam.id]
  }
}

const toggleRecord = async (cam) => {
  const state = recording.value[cam.id]
  if (state === 'active') {
    recording.value[cam.id] = 'toggling'
    try {
      const recId = recordingIdMap.value[cam.id]
      if (recId) await stopRecording(recId)
      recording.value[cam.id] = false
    } catch (err) {
      alert('停止录像失败: ' + (err.response?.data?.message || err.message))
      recording.value[cam.id] = 'active'
    }
  } else {
    // 显示录像设置弹窗
    recordTarget.value = cam
    recordFormat.value = 'mp4'
    recordWithAudio.value = false
    recordBitrate.value = 0
    showRecordDialog.value = true
  }
}

const confirmStartRecording = async () => {
  if (!recordTarget.value) return
  const cam = recordTarget.value
  showRecordDialog.value = false
  recording.value[cam.id] = 'toggling'
  try {
    const rec = await startRecording(cam.id, {
      format: recordFormat.value,
      with_audio: recordWithAudio.value,
      bitrate: recordBitrate.value,
    })
    recordingIdMap.value[cam.id] = rec.id
    recording.value[cam.id] = 'active'
  } catch (err) {
    alert('开始录像失败: ' + (err.response?.data?.message || err.message))
    recording.value[cam.id] = false
  }
}

const toggleFullscreen = () => {
  if (!document.fullscreenElement) {
    document.documentElement.requestFullscreen()
  } else {
    document.exitFullscreen()
  }
}

const handleKeydown = (e) => {
  if (e.target.tagName === 'INPUT' || e.target.tagName === 'TEXTAREA') return
  if (e.key === 'f' || e.key === 'F') toggleFullscreen()
  if (e.key === '1') gridSize.value = 1
  if (e.key === '2') gridSize.value = 4
  if (e.key === '3') gridSize.value = 9
  if (e.key === '4') gridSize.value = 16
}

onMounted(async () => {
  await loadCameras()
  window.addEventListener('keydown', handleKeydown)
  clockTimer = setInterval(updateClock, 1000)

  eventWs = connectEventBus((event) => {
    if (event.type === 'recording_status') {
      const { camera_id, recording_id, status } = event.data
      if (status === 'recording') {
        recording.value[camera_id] = 'active'
        recordingIdMap.value[camera_id] = recording_id
      } else {
        recording.value[camera_id] = false
        delete recordingIdMap.value[camera_id]
      }
    }
    if (event.type === 'camera_status') {
      loadCameras()
    }
  })
})

// 停止所有正在播放的预览
const stopAllStreams = async () => {
  const activeIds = Object.keys(streaming.value).filter((id) => streaming.value[id])
  // 先清空播放状态，立即移除 <img> 元素 → 浏览器马上关闭 MJPEG 连接
  streaming.value = {}
  // 清空 MJPEG URL 缓存
  Object.keys(mjpegUrls).forEach((k) => delete mjpegUrls[k])
  // 再通知后端停止 FFmpeg 拉流进程
  await Promise.allSettled(activeIds.map((id) => stopStream(Number(id))))
}

onUnmounted(() => {
  window.removeEventListener('keydown', handleKeydown)
  if (clockTimer) clearInterval(clockTimer)
  if (eventWs) eventWs.close()
  closeSnapshot()
  // 离开页面时自动停止所有预览
  stopAllStreams()
})
</script>

<style scoped>
.fade-enter-active, .fade-leave-active {
  transition: opacity 0.2s;
}
.fade-enter-from, .fade-leave-to {
  opacity: 0;
}
</style>
