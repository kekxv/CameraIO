<template>
  <div class="live-workspace h-screen flex flex-col text-slate-800 overflow-hidden">
    <!-- 顶部控制栏 -->
    <el-card shadow="never" class="rounded-none border-x-0 border-t-0 flex-shrink-0">
      <div class="flex flex-wrap items-center justify-between px-4 py-2.5">
      <div class="compat-flex-gap-4">
        <h1 class="text-base font-semibold text-slate-800">实时预览</h1>
        <div class="compat-flex-gap-1 text-xs text-slate-500">
          <span class="w-1.5 h-1.5 rounded-full" :class="onlineCount > 0 ? 'bg-emerald-500' : 'bg-slate-300'"></span>
          {{ onlineCount }} / {{ cameras.length }} 在线
        </div>
      </div>
      <!-- 当前时间（用于判断延迟） -->
      <div class="compat-flex-gap-2 text-xs text-slate-600 font-mono">
        <svg xmlns="http://www.w3.org/2000/svg" class="w-3.5 h-3.5 text-slate-400" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
          <path stroke-linecap="round" stroke-linejoin="round" d="M12 8v4l3 3m6-3a9 9 0 11-18 0 9 9 0 0118 0z" />
        </svg>
        <span>{{ nowStr }}</span>
      </div>
      <div class="compat-flex-gap-3 ml-auto">
        <!-- 摄像头选择器 -->
        <el-popover v-model:visible="showCameraPicker" trigger="click" placement="bottom-end" width="288">
          <template #reference>
          <el-button
            plain
            native-type="button"
            :aria-expanded="showCameraPicker"
            aria-controls="live-camera-picker"
          >
            <AppIcon name="camera" class="w-4 h-4" />
            <span>选择摄像头</span>
            <span class="text-xs text-slate-500">{{ cameraSelectionLabel }}</span>
          </el-button>
          </template>
          <div
            id="live-camera-picker"
            class="p-1"
          >
            <div class="flex items-center justify-between mb-2">
              <span class="text-sm font-semibold text-slate-800">显示的摄像头</span>
              <el-button text circle native-type="button" aria-label="关闭摄像头选择" @click="showCameraPicker = false">
                <AppIcon name="close" class="w-4 h-4" />
              </el-button>
            </div>
            <el-checkbox-group v-model="pickerSelectedCameraIDs" class="block max-h-56 overflow-y-auto border-y border-slate-100 py-1">
              <div
                v-for="cam in cameras"
                :key="cam.id"
                class="flex items-center px-2 py-2 rounded-md cursor-pointer hover:bg-slate-50"
              >
                <el-checkbox :value="cam.id" class="min-w-0 flex-1">
                  <span class="block text-sm text-slate-700 truncate">{{ cam.name }}</span>
                  <span class="block text-xs text-slate-400 truncate">{{ cam.ip }}</span>
                </el-checkbox>
                <span class="w-2 h-2 rounded-full" :class="cam.status === 'online' ? 'bg-emerald-500' : 'bg-slate-300'"></span>
              </div>
            </el-checkbox-group>
            <div class="compat-flex-gap-2 justify-between mt-3">
              <el-button plain native-type="button" @click="clearCameraSelection">清空</el-button>
              <el-button type="primary" native-type="button" @click="selectAllCameras">全部显示</el-button>
            </div>
          </div>
        </el-popover>
        <!-- 网格切换 -->
        <el-radio-group v-model="gridSize" size="small" aria-label="预览网格">
          <el-radio-button
            v-for="n in [1, 4, 9, 16]"
            :key="n"
            :value="n"
          >
            {{ gridLabel(n) }}
          </el-radio-button>
        </el-radio-group>
        <!-- 全屏切换 -->
        <el-tooltip content="全屏 (F)">
          <el-button plain circle aria-label="切换全屏" @click="toggleFullscreen">
            <svg xmlns="http://www.w3.org/2000/svg" class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
              <path stroke-linecap="round" stroke-linejoin="round" d="M4 8V4m0 0h4M4 4l5 5m11-5h-4m4 0v4m0 0l-5-5m-7 14H4m0 0v-4m0 4l5-5m7 5h4m0 0v-4m0 4l-5-5" />
            </svg>
          </el-button>
        </el-tooltip>
      </div>
      </div>
    </el-card>

    <!-- 视频网格区域 -->
    <div class="flex-1 p-2.5 min-h-0">
      <!-- 加载状态 -->
      <el-alert v-if="loading" title="加载中..." type="info" :closable="false" />

      <!-- 空状态 -->
      <el-empty v-else-if="cameras.length === 0" description="暂无摄像头"><router-link to="/cameras">去添加 →</router-link></el-empty>

      <!-- 尚未选择摄像头 -->
      <el-empty v-else-if="visibleCameras.length === 0" description="尚未选择用于预览的摄像头"><el-button type="primary" @click="showCameraPicker = true">选择摄像头</el-button></el-empty>

      <!-- 视频网格 -->
      <div v-else class="h-full grid gap-2" :style="gridStyle">
        <div
          v-for="cam in visibleCameras"
          :key="cam.id"
          class="live-stream-tile compat-aspect-video relative rounded-xl overflow-hidden border border-slate-200 bg-slate-100 group"
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
                <div class="w-14 h-14 rounded-full bg-white border border-slate-200 flex items-center justify-center">
                  <svg xmlns="http://www.w3.org/2000/svg" class="w-6 h-6 text-slate-400" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
                    <path stroke-linecap="round" stroke-linejoin="round" d="M14.752 11.168l-3.197-2.132A1 1 0 0010 9.87v4.263a1 1 0 001.555.832l3.197-2.132a1 1 0 000-1.664z" />
                    <path stroke-linecap="round" stroke-linejoin="round" d="M21 12a9 9 0 11-18 0 9 9 0 0118 0z" />
                  </svg>
                </div>
                <el-button type="primary" @click="startStreamFor(cam.id)">
                  开始预览
                </el-button>
              </div>
              <div v-else class="flex flex-col items-center gap-2 text-slate-400">
                <div class="w-12 h-12 rounded-full bg-white border border-slate-200 flex items-center justify-center">
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
              <el-button
                v-if="streaming[cam.id]"
                text
                circle
                size="small"
                @click="stopStreamFor(cam.id)"
                class="live-media-action"
                aria-label="停止预览"
              >
                <svg xmlns="http://www.w3.org/2000/svg" class="w-3.5 h-3.5" viewBox="0 0 20 20" fill="currentColor">
                  <rect x="4" y="4" width="12" height="12" rx="1.5" />
                </svg>
              </el-button>
              <el-button
                text
                circle
                size="small"
                @click="takeSnapshot(cam)"
                :loading="capturing[cam.id]"
                class="live-media-action"
                :aria-label="capturing[cam.id] ? '抓拍中' : '原生抓拍'"
              >
                <AppIcon name="camera" class="w-3.5 h-3.5" />
              </el-button>
              <el-button
                text
                circle
                size="small"
                @click="toggleRecord(cam)"
                :loading="recording[cam.id] === 'toggling'"
                class="live-media-action"
                :class="recording[cam.id] === 'active' ? 'text-red-400 bg-red-500/20' : 'text-slate-300 hover:text-white hover:bg-white/15'"
                :aria-label="recording[cam.id] === 'active' ? '停止录像' : '开始录像'"
              >
                <svg xmlns="http://www.w3.org/2000/svg" class="w-3.5 h-3.5" viewBox="0 0 20 20" fill="currentColor">
                  <circle cx="10" cy="10" r="6" />
                </svg>
              </el-button>
            </div>
          </div>
        </div>
      </div>
    </div>

    <!-- 录像设置弹窗 -->
    <el-dialog v-model="showRecordDialog" title="录像设置" width="420px">
          <h3 class="text-sm font-semibold text-slate-800 mb-3 flex items-center gap-1.5">
            <AppIcon name="film" class="w-4 h-4" />
            <span>录像设置</span>
          </h3>
          <p class="text-xs text-slate-500 mb-3">{{ recordTarget?.name }} · {{ recordTarget?.ip }}</p>

          <!-- 格式选择 -->
          <div class="mb-3">
            <label class="block text-xs font-medium text-slate-600 mb-1.5">录像格式</label>
            <el-radio-group v-model="recordFormat" class="w-full">
              <el-radio-button
                v-for="fmt in [{v:'mp4',l:'MP4'},{v:'ts',l:'TS'}]"
                :key="fmt.v"
                :value="fmt.v"
                class="flex-1"
              >
                {{ fmt.l }}
              </el-radio-button>
            </el-radio-group>
            <p class="text-[10px] text-slate-400 mt-1">
              {{ recordFormat === 'mp4' ? '单文件 MP4 流拷贝，推荐' : '单文件 TS 流拷贝' }}
            </p>
          </div>

          <!-- 资源安全模式固定使用相机原码率流拷贝 -->
          <div class="mb-3">
            <label class="block text-xs font-medium text-slate-600 mb-1.5">码率</label>
			<p class="text-[10px] text-slate-400 mt-1">
			  原画质（相机码率，视频流拷贝）
			</p>
          </div>

          <!-- 音频开关 -->
          <el-checkbox v-model="recordWithAudio" class="mb-4">包含音频</el-checkbox>

          <div class="mb-4">
            <label class="block text-xs font-medium text-slate-600 mb-1.5">录像备注</label>
            <el-input
              v-model="recordRemark"
              maxlength="255"
              clearable
              placeholder="可选，例如：柜员交接"
            />
          </div>

          <!-- 操作按钮 -->
          <div class="compat-flex-gap-2 justify-end">
            <el-button plain @click="showRecordDialog = false">取消</el-button>
            <el-button type="danger" @click="confirmStartRecording">
              <AppIcon name="record" class="w-3.5 h-3.5" />
              <span>开始录像</span>
            </el-button>
          </div>
    </el-dialog>

    <!-- 原生抓拍结果 -->
    <el-dialog v-model="snapshotDialogOpen" title="原生抓拍" width="900px" @closed="clearSnapshot">
          <div v-if="snapshotTarget" class="px-4 py-3 border-b border-slate-200 flex items-center justify-between">
            <div>
              <h3 class="text-sm font-semibold text-slate-800">原生抓拍</h3>
              <p class="text-xs text-slate-500 mt-0.5">{{ snapshotTarget.name }} · {{ snapshotTarget.ip }}</p>
            </div>
            <el-button text circle aria-label="关闭" @click="closeSnapshot">
              <AppIcon name="close" class="w-4 h-4" />
            </el-button>
          </div>
          <div v-if="snapshotTarget" class="min-h-0 p-4 bg-slate-100 flex items-center justify-center">
            <img :src="snapshotURL" :alt="`${snapshotTarget.name} 抓拍`" class="max-w-full max-h-[70vh] object-contain rounded" />
          </div>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, computed, onMounted, onUnmounted } from 'vue'
import { listCameras, startStream, stopStream, startRecording, stopRecording, heartbeatRecording, captureSnapshot, getAPIErrorMessage, connectEventBus } from '../api'
import AppIcon from '../components/AppIcon.vue'

const cameras = ref([])
const loading = ref(true)
const gridSize = ref(4)
const selectedCameraIDs = ref(null)
const showCameraPicker = ref(false)
const streaming = ref({})
const starting = ref({})
const recording = ref({})
const recordingIdMap = ref({})
const capturing = ref({})
const snapshotTarget = ref(null)
const snapshotURL = ref('')
const snapshotDialogOpen = ref(false)
const showRecordDialog = ref(false)
const recordTarget = ref(null)
const recordFormat = ref('mp4')
const recordWithAudio = ref(false)
const recordBitrate = ref(0) // kbps, 0=流拷贝原画质
const recordRemark = ref('')
const nowStr = ref('')
let clockTimer = null
let eventWs = null
const manualRecordingHeartbeatTimers = {}

// 每秒更新当前时间
const updateClock = () => {
  const d = new Date()
  const pad = (n) => String(n).padStart(2, '0')
  nowStr.value = `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`
}
updateClock()

const onlineCount = computed(() => cameras.value.filter((c) => c.status === 'online').length)
const selectedCameras = computed(() => {
  if (selectedCameraIDs.value === null) return cameras.value
  return cameras.value.filter((camera) => selectedCameraIDs.value.includes(camera.id))
})
const pickerSelectedCameraIDs = computed({
  get: () => selectedCameraIDs.value === null ? cameras.value.map((camera) => camera.id) : selectedCameraIDs.value,
  set: (cameraIDs) => { selectedCameraIDs.value = cameraIDs },
})
const visibleCameras = computed(() => selectedCameras.value.slice(0, gridSize.value))
const cameraSelectionLabel = computed(() => {
  if (selectedCameraIDs.value === null) return '全部'
  return `已选 ${selectedCameraIDs.value.length} 台`
})

const gridStyle = computed(() => {
  const n = Math.max(1, visibleCameras.value.length)
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

const clearCameraSelection = () => {
  selectedCameraIDs.value = []
}

const selectAllCameras = () => {
  selectedCameraIDs.value = cameras.value.map((camera) => camera.id)
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
    if (selectedCameraIDs.value !== null) {
      selectedCameraIDs.value = selectedCameraIDs.value.filter((id) => cameras.value.some((camera) => camera.id === id))
    }
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

const clearSnapshot = () => {
  if (snapshotURL.value) URL.revokeObjectURL(snapshotURL.value)
  snapshotURL.value = ''
  snapshotTarget.value = null
}

const closeSnapshot = () => {
  snapshotDialogOpen.value = false
}

const takeSnapshot = async (cam) => {
  capturing.value[cam.id] = true
  try {
    const jpeg = await captureSnapshot(cam.id)
    clearSnapshot()
    snapshotURL.value = URL.createObjectURL(jpeg)
    snapshotTarget.value = cam
    snapshotDialogOpen.value = true
  } catch (err) {
    alert('抓拍失败: ' + await getAPIErrorMessage(err))
  } finally {
    delete capturing.value[cam.id]
  }
}

const clearManualRecordingHeartbeat = (cameraId) => {
  const timer = manualRecordingHeartbeatTimers[cameraId]
  if (timer) clearInterval(timer)
  delete manualRecordingHeartbeatTimers[cameraId]
}

const startManualRecordingHeartbeat = (cameraId, recordingId) => {
  clearManualRecordingHeartbeat(cameraId)
  manualRecordingHeartbeatTimers[cameraId] = setInterval(async () => {
    try {
      await heartbeatRecording(recordingId)
    } catch (err) {
      console.warn('recording heartbeat failed:', err)
    }
  }, 30000)
}

const toggleRecord = async (cam) => {
  const state = recording.value[cam.id]
  if (state === 'active') {
    recording.value[cam.id] = 'toggling'
    try {
      const recId = recordingIdMap.value[cam.id]
      if (recId) await stopRecording(recId)
      clearManualRecordingHeartbeat(cam.id)
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
    recordRemark.value = ''
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
      trigger_type: 'manual',
      remark: recordRemark.value,
    })
    const recordingId = rec.recording_id || rec.id
    if (!recordingId) throw new Error('开始录像未返回录像 ID')
    recordingIdMap.value[cam.id] = recordingId
    startManualRecordingHeartbeat(cam.id, recordingId)
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
        clearManualRecordingHeartbeat(camera_id)
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
  Object.keys(manualRecordingHeartbeatTimers).forEach((cameraId) => clearManualRecordingHeartbeat(cameraId))
  clearSnapshot()
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
