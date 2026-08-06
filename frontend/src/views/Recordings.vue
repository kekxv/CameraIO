<template>
  <div class="p-6">
    <!-- 页头 -->
    <div class="flex items-center justify-between mb-4">
      <div>
        <h1 class="text-2xl font-bold text-slate-800">录像中心</h1>
        <p class="text-sm text-slate-500 mt-1">浏览、预览和下载所有录像文件</p>
      </div>
    </div>

    <!-- 标签页 -->
    <div class="flex gap-1 mb-4 bg-slate-100 rounded-lg p-0.5 w-fit">
      <button
        @click="activeTab = 'recordings'"
        class="px-4 py-1.5 rounded-md text-sm font-medium transition-all"
        :class="activeTab === 'recordings' ? 'bg-white text-primary-700 shadow-sm' : 'text-slate-500 hover:text-slate-700'"
      >
        🎬 录像列表
      </button>
      <button
        @click="activeTab = 'schedules'"
        class="px-4 py-1.5 rounded-md text-sm font-medium transition-all"
        :class="activeTab === 'schedules' ? 'bg-white text-primary-700 shadow-sm' : 'text-slate-500 hover:text-slate-700'"
      >
        ⏰ 定时录像
      </button>
    </div>

    <!-- ===== 录像列表标签页 ===== -->
    <template v-if="activeTab === 'recordings'">
      <!-- 筛选栏 -->
      <div class="bg-white rounded-lg border border-slate-200 shadow-sm p-4 mb-4">
        <div class="flex flex-wrap items-end gap-3">
          <div>
            <label class="block text-xs font-medium text-slate-500 mb-1">摄像头</label>
            <select
              v-model="filter.cameraId"
              @change="loadRecordings"
              class="px-3 py-1.5 border border-slate-300 rounded-md text-sm focus:outline-none focus:ring-2 focus:ring-primary-500"
            >
              <option :value="null">全部</option>
              <option v-for="cam in cameras" :key="cam.id" :value="cam.id">
                {{ cam.name }}
              </option>
            </select>
          </div>
          <div>
            <label class="block text-xs font-medium text-slate-500 mb-1">状态</label>
            <select
              v-model="filter.status"
              @change="loadRecordings"
              class="px-3 py-1.5 border border-slate-300 rounded-md text-sm focus:outline-none focus:ring-2 focus:ring-primary-500"
            >
              <option value="">全部</option>
              <option value="recording">录制中</option>
              <option value="completed">已完成</option>
              <option value="failed">失败</option>
            </select>
          </div>
          <button
            @click="loadRecordings"
            class="px-3 py-1.5 bg-slate-100 hover:bg-slate-200 text-slate-700 rounded-md text-sm transition-colors"
          >
            刷新
          </button>
          <div class="flex-1"></div>
          <div class="text-xs text-slate-500">
            共 {{ total }} 条记录
          </div>
        </div>
      </div>

      <!-- 加载中 -->
      <div v-if="loading" class="text-center py-12 text-slate-500">加载中...</div>

      <!-- 空状态 -->
      <div v-else-if="recordings.length === 0" class="text-center py-16">
        <p class="text-4xl mb-3">🎬</p>
        <p class="text-slate-500">暂无录像记录</p>
      </div>

      <!-- 录像列表 -->
      <div v-else class="bg-white rounded-lg border border-slate-200 shadow-sm overflow-hidden">
        <table class="min-w-full divide-y divide-slate-200">
          <thead class="bg-slate-50">
            <tr>
              <th class="px-4 py-2.5 text-left text-xs font-medium text-slate-500 uppercase tracking-wider">ID</th>
              <th class="px-4 py-2.5 text-left text-xs font-medium text-slate-500 uppercase tracking-wider">摄像头</th>
              <th class="px-4 py-2.5 text-left text-xs font-medium text-slate-500 uppercase tracking-wider">开始时间</th>
              <th class="px-4 py-2.5 text-left text-xs font-medium text-slate-500 uppercase tracking-wider">时长</th>
              <th class="px-4 py-2.5 text-left text-xs font-medium text-slate-500 uppercase tracking-wider">大小</th>
              <th class="px-4 py-2.5 text-left text-xs font-medium text-slate-500 uppercase tracking-wider">来源</th>
              <th class="px-4 py-2.5 text-left text-xs font-medium text-slate-500 uppercase tracking-wider">状态</th>
              <th class="px-4 py-2.5 text-right text-xs font-medium text-slate-500 uppercase tracking-wider">操作</th>
            </tr>
          </thead>
          <tbody class="divide-y divide-slate-100">
            <tr v-for="rec in recordings" :key="rec.id" class="hover:bg-slate-50">
              <td class="px-4 py-2.5 text-sm text-slate-600">{{ rec.id }}</td>
              <td class="px-4 py-2.5 text-sm text-slate-800">
                {{ cameraName(rec.camera_id) }}
              </td>
              <td class="px-4 py-2.5 text-sm text-slate-600">
                {{ formatTime(rec.start_time) }}
              </td>
              <td class="px-4 py-2.5 text-sm text-slate-600">
                {{ formatDuration(rec.duration) }}
              </td>
              <td class="px-4 py-2.5 text-sm text-slate-600">
                {{ formatSize(rec.file_size) }}
              </td>
              <td class="px-4 py-2.5 text-xs">
                <span
                  class="px-2 py-0.5 rounded-full font-medium"
                  :class="triggerClass(rec.trigger_type)"
                >
                  {{ triggerLabel(rec.trigger_type) }}
                </span>
              </td>
              <td class="px-4 py-2.5">
                <span
                  class="px-2 py-0.5 rounded-full text-xs font-medium"
                  :class="{
                    'bg-red-50 text-red-700': rec.status === 'recording',
                    'bg-emerald-50 text-emerald-700': rec.status === 'completed',
                    'bg-orange-50 text-orange-700': rec.status === 'failed',
                  }"
                >
                  {{ statusLabel(rec.status) }}
                  <span v-if="rec.status === 'recording'" class="inline-block w-1.5 h-1.5 rounded-full bg-red-500 animate-pulse ml-1"></span>
                </span>
              </td>
              <td class="px-4 py-2.5 text-right">
                <div class="flex items-center justify-end gap-1">
                  <button
                    v-if="rec.status === 'completed'"
                    @click="previewRecording(rec)"
                    class="px-2 py-1 text-xs bg-slate-100 hover:bg-slate-200 text-slate-700 rounded transition-colors"
                  >
                    👁
                  </button>
                  <a
                    v-if="rec.status === 'completed'"
                    :href="downloadUrl(rec.id)"
                    :download="`recording_${rec.id}.${rec.format || 'mp4'}`"
                    class="px-2 py-1 text-xs bg-primary-50 hover:bg-primary-100 text-primary-700 rounded transition-colors"
                  >
                    ⬇
                  </a>
                  <button
                    v-if="rec.status === 'recording'"
                    @click="handleStopRecording(rec)"
                    class="px-2 py-1 text-xs bg-red-50 hover:bg-red-100 text-red-700 rounded transition-colors"
                  >
                    ⏹
                  </button>
                  <button
                    @click="handleDeleteRecording(rec)"
                    class="px-2 py-1 text-xs bg-slate-100 hover:bg-red-50 text-slate-600 hover:text-red-600 rounded transition-colors"
                    title="删除"
                  >
                    🗑
                  </button>
                </div>
              </td>
            </tr>
          </tbody>
        </table>

        <!-- 分页 -->
        <div v-if="totalPages > 1" class="px-4 py-3 border-t border-slate-200 flex items-center justify-between">
          <div class="text-xs text-slate-500">
            第 {{ page }} / {{ totalPages }} 页
          </div>
          <div class="flex gap-1">
            <button
              @click="goPage(page - 1)"
              :disabled="page <= 1"
              class="px-3 py-1 text-xs border border-slate-300 rounded hover:bg-slate-50 disabled:opacity-50"
            >
              上一页
            </button>
            <button
              @click="goPage(page + 1)"
              :disabled="page >= totalPages"
              class="px-3 py-1 text-xs border border-slate-300 rounded hover:bg-slate-50 disabled:opacity-50"
            >
              下一页
            </button>
          </div>
        </div>
      </div>
    </template>

    <!-- ===== 定时录像标签页 ===== -->
    <template v-else>
      <div class="bg-white rounded-lg border border-slate-200 shadow-sm p-4 mb-4 flex items-center justify-between">
        <p class="text-sm text-slate-600">
          定时计划：到时间自动开始录像，离开时间范围自动停止，每天重复。
        </p>
        <button
          @click="openScheduleDialog(null)"
          class="px-4 py-2 bg-primary-600 hover:bg-primary-700 text-white rounded-md text-sm font-medium transition-colors"
        >
          + 新建计划
        </button>
      </div>

      <!-- 计划列表 -->
      <div v-if="schedules.length === 0" class="text-center py-16">
        <p class="text-4xl mb-3">⏰</p>
        <p class="text-slate-500">暂无定时录像计划</p>
      </div>

      <div v-else class="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-3 gap-4">
        <div
          v-for="sch in schedules"
          :key="sch.id"
          class="bg-white rounded-lg border border-slate-200 shadow-sm hover:shadow-md transition-shadow"
          :class="{ 'opacity-60': !sch.enabled }"
        >
          <div class="p-4">
            <div class="flex items-start justify-between">
              <div class="flex-1 min-w-0">
                <h3 class="font-semibold text-slate-800 truncate">{{ sch.name }}</h3>
                <p class="text-xs text-slate-500 mt-0.5">{{ cameraName(sch.camera_id) }}</p>
              </div>
              <span
                class="px-2 py-0.5 rounded-full text-xs font-medium"
                :class="sch.enabled ? 'bg-emerald-50 text-emerald-700' : 'bg-slate-100 text-slate-500'"
              >
                {{ sch.enabled ? '启用' : '停用' }}
              </span>
            </div>

            <div class="mt-3 space-y-1.5 text-xs text-slate-600">
              <div class="flex items-center gap-2">
                <span class="text-slate-400">🕐</span>
                <span class="font-mono">{{ sch.start_time }} - {{ sch.end_time }}</span>
              </div>
              <div class="flex items-center gap-2">
                <span class="text-slate-400">📅</span>
                <span>{{ daysLabel(sch.days) }}</span>
              </div>
              <div class="flex items-center gap-2">
                <span class="text-slate-400">🎬</span>
                <span>{{ (sch.format || 'mp4').toUpperCase() }}</span>
                <span v-if="sch.with_audio" class="px-1.5 py-0.5 bg-blue-50 text-blue-700 rounded text-[10px]">含音频</span>
              </div>
            </div>
          </div>

          <div class="px-4 py-3 bg-slate-50 border-t border-slate-100 flex items-center gap-2">
            <button
              @click="toggleSchedule(sch)"
              class="flex-1 px-2 py-1.5 text-xs bg-white border border-slate-200 rounded hover:bg-slate-100 transition-colors"
            >
              {{ sch.enabled ? '⏸ 停用' : '▶ 启用' }}
            </button>
            <button
              @click="openScheduleDialog(sch)"
              class="px-2 py-1.5 text-xs bg-white border border-slate-200 rounded hover:bg-slate-100 transition-colors"
            >
              ✏️
            </button>
            <button
              @click="handleDeleteSchedule(sch)"
              class="px-2 py-1.5 text-xs bg-white border border-red-200 text-red-600 rounded hover:bg-red-50 transition-colors"
            >
              🗑
            </button>
          </div>
        </div>
      </div>
    </template>

    <!-- 预览对话框 -->
    <div
      v-if="previewRec"
      class="fixed inset-0 bg-black/80 flex items-center justify-center z-50 p-8"
      @click.self="previewRec = null"
    >
      <div class="bg-white rounded-lg shadow-xl w-full max-w-3xl overflow-hidden">
        <div class="flex items-center justify-between px-4 py-3 border-b border-slate-200">
          <div>
            <h3 class="font-semibold text-slate-800">录像预览 #{{ previewRec.id }}</h3>
            <p class="text-xs text-slate-500">{{ cameraName(previewRec.camera_id) }} · {{ formatTime(previewRec.start_time) }}</p>
          </div>
          <button
            @click="previewRec = null"
            class="w-7 h-7 flex items-center justify-center rounded hover:bg-slate-100 text-slate-500"
          >
            ✕
          </button>
        </div>
        <div class="bg-black aspect-video flex items-center justify-center">
          <video
            :src="downloadUrl(previewRec.id)"
            controls
            autoplay
            class="max-w-full max-h-full"
          ></video>
        </div>
        <div class="px-4 py-3 border-t border-slate-200 flex justify-end">
          <a
            :href="downloadUrl(previewRec.id)"
            :download="`recording_${previewRec.id}.${previewRec.format || 'mp4'}`"
            class="px-4 py-2 bg-primary-600 hover:bg-primary-700 text-white rounded-md text-sm transition-colors"
          >
            ⬇ 下载
          </a>
        </div>
      </div>
    </div>

    <!-- 计划编辑对话框 -->
    <div
      v-if="showScheduleDialog"
      class="fixed inset-0 bg-black/50 flex items-center justify-center z-50"
      @click.self="showScheduleDialog = false"
    >
      <div class="bg-white rounded-lg shadow-xl w-full max-w-md p-6">
        <h3 class="text-lg font-semibold text-slate-800 mb-4">
          {{ scheduleForm.id ? '编辑计划' : '新建计划' }}
        </h3>

        <div class="space-y-4">
          <div>
            <label class="block text-sm font-medium text-slate-700 mb-1">名称</label>
            <input
              v-model="scheduleForm.name"
              type="text"
              class="w-full px-3 py-2 border border-slate-300 rounded-md text-sm focus:outline-none focus:ring-2 focus:ring-primary-500"
              placeholder="白天录像"
            />
          </div>

          <div>
            <label class="block text-sm font-medium text-slate-700 mb-1">摄像头</label>
            <select
              v-model="scheduleForm.camera_id"
              class="w-full px-3 py-2 border border-slate-300 rounded-md text-sm focus:outline-none focus:ring-2 focus:ring-primary-500"
            >
              <option v-for="cam in cameras" :key="cam.id" :value="cam.id">
                {{ cam.name }} ({{ cam.ip }})
              </option>
            </select>
          </div>

          <div class="grid grid-cols-2 gap-3">
            <div>
              <label class="block text-sm font-medium text-slate-700 mb-1">开始时间</label>
              <input
                v-model="scheduleForm.start_time"
                type="time"
                class="w-full px-3 py-2 border border-slate-300 rounded-md text-sm focus:outline-none focus:ring-2 focus:ring-primary-500"
              />
            </div>
            <div>
              <label class="block text-sm font-medium text-slate-700 mb-1">结束时间</label>
              <input
                v-model="scheduleForm.end_time"
                type="time"
                class="w-full px-3 py-2 border border-slate-300 rounded-md text-sm focus:outline-none focus:ring-2 focus:ring-primary-500"
              />
            </div>
          </div>

          <div>
            <label class="block text-sm font-medium text-slate-700 mb-1.5">重复星期</label>
            <div class="flex flex-wrap gap-1.5">
              <button
                v-for="(d, i) in weekdays"
                :key="d.name"
                type="button"
                @click="toggleDay(i)"
                class="px-2.5 py-1 rounded text-xs font-medium transition-colors"
                :class="(scheduleForm.days & (1 << i)) ? 'bg-primary-600 text-white' : 'bg-slate-100 text-slate-600 hover:bg-slate-200'"
              >
                {{ d.short }}
              </button>
            </div>
          </div>

          <div class="grid grid-cols-2 gap-3">
            <div>
              <label class="block text-sm font-medium text-slate-700 mb-1">格式</label>
              <select
                v-model="scheduleForm.format"
                class="w-full px-3 py-2 border border-slate-300 rounded-md text-sm focus:outline-none focus:ring-2 focus:ring-primary-500"
              >
                <option value="mp4">MP4</option>
                <option value="webm">WebM</option>
                <option value="ts">TS</option>
              </select>
            </div>
            <div class="flex items-end">
              <label class="flex items-center gap-2 text-sm pb-2">
                <input v-model="scheduleForm.with_audio" type="checkbox" class="rounded" />
                <span class="text-slate-700">包含音频</span>
              </label>
            </div>
          </div>
        </div>

        <div class="flex justify-end gap-2 mt-5">
          <button @click="showScheduleDialog = false" class="px-4 py-2 text-sm text-slate-600 hover:bg-slate-100 rounded-md">取消</button>
          <button @click="saveSchedule" :disabled="savingSchedule"
            class="px-4 py-2 bg-primary-600 hover:bg-primary-700 text-white rounded-md text-sm disabled:opacity-50">
            {{ savingSchedule ? '保存中...' : '保存' }}
          </button>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, reactive, computed, onMounted, onUnmounted } from 'vue'
import {
  listCameras, listRecordings, stopRecording, deleteRecording,
  listSchedules, createSchedule, updateSchedule, deleteSchedule,
  connectEventBus,
} from '../api'

const activeTab = ref('recordings')
const cameras = ref([])
const recordings = ref([])
const total = ref(0)
const loading = ref(true)
const previewRec = ref(null)
const schedules = ref([])
const showScheduleDialog = ref(false)
const savingSchedule = ref(false)
let eventWs = null

const filter = reactive({
  cameraId: null,
  status: '',
})

const pageSize = 20
const page = ref(1)
const totalPages = computed(() => Math.max(1, Math.ceil(total.value / pageSize)))

const weekdays = [
  { name: '周一', short: '一' },
  { name: '周二', short: '二' },
  { name: '周三', short: '三' },
  { name: '周四', short: '四' },
  { name: '周五', short: '五' },
  { name: '周六', short: '六' },
  { name: '周日', short: '日' },
]

const defaultScheduleForm = () => ({
  id: null,
  name: '',
  camera_id: cameras.value[0]?.id || null,
  start_time: '09:00',
  end_time: '17:00',
  days: 127,
  format: 'mp4',
  with_audio: false,
  enabled: true,
})
const scheduleForm = ref(defaultScheduleForm())

const loadRecordings = async () => {
  loading.value = true
  try {
    const params = { page: page.value, page_size: pageSize }
    if (filter.cameraId) params.camera_id = filter.cameraId
    if (filter.status) params.status = filter.status
    const data = await listRecordings(params)
    recordings.value = data.recordings
    total.value = data.total
  } finally {
    loading.value = false
  }
}

const loadCameras = async () => {
  cameras.value = await listCameras()
}

const loadSchedules = async () => {
  schedules.value = await listSchedules()
}

onMounted(async () => {
  await loadCameras()
  await loadRecordings()
  await loadSchedules()

  eventWs = connectEventBus((event) => {
    if (event.type === 'recording_status') {
      loadRecordings()
    }
  })
})

onUnmounted(() => {
  if (eventWs) eventWs.close()
})

const goPage = (p) => {
  if (p < 1 || p > totalPages.value) return
  page.value = p
  loadRecordings()
}

const cameraName = (id) => {
  const cam = cameras.value.find((c) => c.id === id)
  return cam ? cam.name : `Camera #${id}`
}

const statusLabel = (s) => {
  const map = { recording: '录制中', completed: '已完成', failed: '失败' }
  return map[s] || s
}

const triggerLabel = (t) => {
  const map = { api: '手动', manual: '手动', schedule: '定时' }
  return map[t] || t || '手动'
}

const triggerClass = (t) => {
  if (t === 'schedule') return 'bg-purple-50 text-purple-700'
  return 'bg-slate-100 text-slate-500'
}

const daysLabel = (days) => {
  if (days === 127 || days === 127) return '每天'
  const names = ['周一', '周二', '周三', '周四', '周五', '周六', '周日']
  const selected = []
  for (let i = 0; i < 7; i++) {
    if (days & (1 << i)) selected.push(names[i])
  }
  return selected.length ? selected.join(' ') : '无'
}

const formatTime = (t) => {
  if (!t) return '-'
  return new Date(t).toLocaleString('zh-CN')
}

const formatDuration = (seconds) => {
  if (!seconds) return '-'
  const h = Math.floor(seconds / 3600)
  const m = Math.floor((seconds % 3600) / 60)
  const s = seconds % 60
  if (h > 0) return `${h}:${String(m).padStart(2, '0')}:${String(s).padStart(2, '0')}`
  return `${m}:${String(s).padStart(2, '0')}`
}

const formatSize = (bytes) => {
  if (!bytes) return '-'
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
  if (bytes < 1024 * 1024 * 1024) return `${(bytes / 1024 / 1024).toFixed(1)} MB`
  return `${(bytes / 1024 / 1024 / 1024).toFixed(2)} GB`
}

const downloadUrl = (id) => {
  const token = localStorage.getItem('token')
  return `/api/v1/recordings/${id}/download?token=${token}`
}

const previewRecording = (rec) => {
  previewRec.value = rec
}

const handleStopRecording = async (rec) => {
  if (!confirm('确定停止该录像吗？')) return
  try {
    await stopRecording(rec.id)
    loadRecordings()
  } catch (err) {
    alert('停止失败: ' + (err.response?.data?.message || err.message))
  }
}

const handleDeleteRecording = async (rec) => {
  if (!confirm(`确定删除录像 #${rec.id} 吗？视频文件将一并删除，不可恢复！`)) return
  try {
    await deleteRecording(rec.id)
    loadRecordings()
  } catch (err) {
    alert('删除失败: ' + (err.response?.data?.message || err.message))
  }
}

// ---------- 定时计划 ----------

const openScheduleDialog = (sch) => {
  if (sch) {
    scheduleForm.value = { ...defaultScheduleForm(), ...sch }
  } else {
    scheduleForm.value = defaultScheduleForm()
    scheduleForm.value.camera_id = cameras.value[0]?.id || null
  }
  showScheduleDialog.value = true
}

const toggleDay = (i) => {
  scheduleForm.value.days ^= (1 << i)
}

const saveSchedule = async () => {
  savingSchedule.value = true
  try {
    const data = { ...scheduleForm.value }
    if (data.id) {
      await updateSchedule(data.id, data)
    } else {
      delete data.id
      await createSchedule(data)
    }
    showScheduleDialog.value = false
    await loadSchedules()
  } catch (err) {
    alert('保存失败: ' + (err.response?.data?.message || err.message))
  } finally {
    savingSchedule.value = false
  }
}

const toggleSchedule = async (sch) => {
  try {
    await updateSchedule(sch.id, { ...sch, enabled: !sch.enabled })
    await loadSchedules()
  } catch (err) {
    alert('操作失败: ' + (err.response?.data?.message || err.message))
  }
}

const handleDeleteSchedule = async (sch) => {
  if (!confirm(`确定删除计划 "${sch.name}" 吗？`)) return
  try {
    await deleteSchedule(sch.id)
    await loadSchedules()
  } catch (err) {
    alert('删除失败: ' + (err.response?.data?.message || err.message))
  }
}
</script>
