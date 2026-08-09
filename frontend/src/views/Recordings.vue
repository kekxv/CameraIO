<template>
  <div>
    <!-- 页头 -->
    <div class="ui-page-header">
      <div>
        <h1 class="ui-page-title">录像中心</h1>
        <p class="ui-page-description">浏览、预览和下载所有录像文件</p>
      </div>
    </div>

    <!-- 标签页 -->
    <div class="ui-card compat-flex-gap-1 mb-4 p-1 w-fit">
      <button
        @click="activeTab = 'recordings'"
        class="px-4 py-1.5 rounded-md text-sm font-medium transition-all compat-flex-gap-1"
        :class="activeTab === 'recordings' ? 'bg-white text-primary-700 shadow-sm' : 'text-slate-500 hover:text-slate-700'"
      >
        <AppIcon name="film" class="w-4 h-4" />
        <span>录像列表</span>
      </button>
      <button
        @click="activeTab = 'schedules'"
        class="px-4 py-1.5 rounded-md text-sm font-medium transition-all compat-flex-gap-1"
        :class="activeTab === 'schedules' ? 'bg-white text-primary-700 shadow-sm' : 'text-slate-500 hover:text-slate-700'"
      >
        <AppIcon name="clock" class="w-4 h-4" />
        <span>定时录像</span>
      </button>
    </div>

    <!-- ===== 录像列表标签页 ===== -->
    <template v-if="activeTab === 'recordings'">
      <!-- 筛选栏 -->
      <div class="ui-card p-4 mb-4">
        <div class="compat-flex-gap-3 flex-wrap items-end">
          <div>
            <label class="block text-xs font-medium text-slate-500 mb-1">摄像头</label>
            <select
              v-model="filter.cameraId"
              @change="applyHistoryFilters"
              class="ui-select"
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
              @change="applyHistoryFilters"
              class="ui-select"
            >
              <option value="">全部</option>
              <option value="recording">录制中</option>
              <option value="completed">已完成</option>
              <option value="failed">失败</option>
            </select>
          </div>
          <div>
            <label class="block text-xs font-medium text-slate-500 mb-1">录像日期</label>
            <div class="compat-flex-gap-1 items-center">
              <input v-model="timeSearch.startDate" type="date" class="ui-input" aria-label="开始日期" />
              <span class="text-sm text-slate-500">至</span>
              <input v-model="timeSearch.endDate" type="date" class="ui-input" aria-label="结束日期" />
            </div>
          </div>
          <button @click="clearDateRange" class="ui-button-secondary">清除日期</button>
          <button @click="applyHistoryFilters" class="ui-button-primary">查询历史</button>
          <button
            @click="loadRecordings"
            class="ui-button-secondary"
          >
            刷新
          </button>
          <div class="flex-1"></div>
          <div class="text-xs text-slate-500">
            共 {{ total }} 条记录
          </div>
        </div>
        <p v-if="historyError" class="mt-3 text-sm text-red-600">{{ historyError }}</p>
      </div>

      <!-- 按时间播放 -->
      <div class="ui-card p-4 mb-4">
        <div class="flex flex-wrap items-end compat-flex-gap-3">
          <div>
            <label class="block text-xs font-medium text-slate-500 mb-1">播放摄像头</label>
            <select v-model="timeSearch.cameraId" class="ui-select">
              <option v-for="cam in cameras" :key="cam.id" :value="cam.id">
                {{ cam.name }}
              </option>
            </select>
          </div>
          <div>
            <label class="block text-xs font-medium text-slate-500 mb-1">播放时间</label>
            <input v-model="timeSearch.at" type="datetime-local" step="1" class="ui-input" />
          </div>
          <button
            class="ui-button-primary disabled:opacity-50"
            :disabled="!timeSearch.cameraId"
            @click="playSelectedTime"
          >
            播放所选时间
          </button>
        </div>
        <p v-if="timelineError" class="mt-3 text-sm text-red-600">{{ timelineError }}</p>
      </div>

      <!-- 加载中 -->
      <div v-if="loading" class="text-center py-12 text-slate-500">加载中...</div>

      <!-- 空状态 -->
      <div v-else-if="recordings.length === 0" class="text-center py-16">
        <AppIcon name="film" class="w-12 h-12 mx-auto mb-3 text-slate-300" />
        <p class="text-slate-500">暂无录像记录</p>
      </div>

      <!-- 录像列表 -->
      <div v-else class="ui-card overflow-hidden">
        <div class="overflow-x-auto">
        <table class="min-w-full min-w-[860px] divide-y divide-slate-200">
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
                <div class="compat-flex-gap-1 justify-end">
                  <button
                    v-if="rec.status === 'completed'"
                    @click="openRecordingPreview(rec)"
                    class="ui-icon-button"
                    title="预览"
                    aria-label="预览"
                  >
                    <AppIcon name="eye" class="w-3.5 h-3.5" />
                  </button>
                  <a
                    v-if="rec.status === 'completed' && !isSegmentedRecording(rec)"
                    :href="downloadUrl(rec.id)"
                    :download="`recording_${rec.id}.${rec.format || 'mp4'}`"
                    class="ui-icon-button text-primary-700"
                    title="下载"
                    aria-label="下载"
                  >
                    <AppIcon name="download" class="w-3.5 h-3.5" />
                  </a>
                  <button
                    v-else-if="rec.status === 'completed'"
                    type="button"
                    class="ui-button-secondary compat-flex-gap-1 opacity-50 cursor-not-allowed"
                    disabled
                    title="导出尚未实现"
                    aria-label="导出尚未实现"
                  >
                    <AppIcon name="download" class="w-3.5 h-3.5" />
                    <span>导出</span>
                  </button>
                  <button
                    v-if="rec.status === 'recording'"
                    @click="handleStopRecording(rec)"
                    class="ui-icon-button text-red-700"
                    title="停止录像"
                    aria-label="停止录像"
                  >
                    <AppIcon name="stop" class="w-3.5 h-3.5" />
                  </button>
                  <button
                    @click="handleDeleteRecording(rec)"
                    class="ui-icon-button text-red-700"
                    title="删除"
                    aria-label="删除"
                  >
                    <AppIcon name="trash" class="w-3.5 h-3.5" />
                  </button>
                </div>
              </td>
            </tr>
          </tbody>
        </table>
        </div>

        <!-- 分页 -->
        <div v-if="totalPages > 1" class="px-4 py-3 border-t border-slate-200 flex items-center justify-between">
          <div class="text-xs text-slate-500">
            第 {{ page }} / {{ totalPages }} 页
          </div>
          <div class="compat-flex-gap-1">
            <button
              @click="goPage(page - 1)"
              :disabled="page <= 1"
              class="ui-button-secondary disabled:opacity-50"
            >
              上一页
            </button>
            <button
              @click="goPage(page + 1)"
              :disabled="page >= totalPages"
              class="ui-button-secondary disabled:opacity-50"
            >
              下一页
            </button>
          </div>
        </div>
      </div>
    </template>

    <!-- ===== 定时录像标签页 ===== -->
    <template v-else>
      <div class="ui-card p-4 mb-4 flex flex-wrap items-center justify-between">
        <p class="text-sm text-slate-600">
          定时计划：到时间自动开始录像，离开时间范围自动停止，每天重复。
        </p>
        <button
          @click="openScheduleDialog(null)"
          class="ui-button-primary"
        >
          + 新建计划
        </button>
      </div>

      <!-- 计划列表 -->
      <div v-if="schedules.length === 0" class="text-center py-16">
        <AppIcon name="clock" class="w-12 h-12 mx-auto mb-3 text-slate-300" />
        <p class="text-slate-500">暂无定时录像计划</p>
      </div>

      <div v-else class="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-3 gap-4">
        <div
          v-for="sch in schedules"
          :key="sch.id"
          class="ui-card hover:shadow-md transition-shadow"
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
                <AppIcon name="clock" class="w-3.5 h-3.5 text-slate-400 flex-shrink-0" />
                <span class="font-mono">{{ sch.start_time }} - {{ sch.end_time }}</span>
              </div>
              <div class="flex items-center gap-2">
                <AppIcon name="calendar" class="w-3.5 h-3.5 text-slate-400 flex-shrink-0" />
                <span>{{ daysLabel(sch.days) }}</span>
              </div>
              <div class="flex items-center gap-2">
                <AppIcon name="film" class="w-3.5 h-3.5 text-slate-400 flex-shrink-0" />
                <span>{{ (sch.format || 'mp4').toUpperCase() }}</span>
                <span v-if="sch.bitrate > 0" class="px-1.5 py-0.5 bg-amber-50 text-amber-700 rounded text-[10px]">{{ sch.bitrate }}k</span>
                <span v-if="sch.with_audio" class="px-1.5 py-0.5 bg-blue-50 text-blue-700 rounded text-[10px]">含音频</span>
              </div>
            </div>
          </div>

          <div class="px-4 py-3 bg-slate-50 border-t border-slate-100 compat-flex-gap-2">
            <button
              @click="toggleSchedule(sch)"
              class="flex-1 ui-button-secondary compat-flex-gap-1"
            >
              <AppIcon :name="sch.enabled ? 'pause' : 'play'" class="w-3.5 h-3.5" />
              <span>{{ sch.enabled ? '停用' : '启用' }}</span>
            </button>
            <button
              @click="openScheduleDialog(sch)"
              class="ui-icon-button"
              title="编辑计划"
              aria-label="编辑计划"
            >
              <AppIcon name="edit" class="w-3.5 h-3.5" />
            </button>
            <button
              @click="handleDeleteSchedule(sch)"
              class="ui-icon-button text-red-700"
              title="删除计划"
              aria-label="删除计划"
            >
              <AppIcon name="trash" class="w-3.5 h-3.5" />
            </button>
          </div>
        </div>
      </div>
    </template>

    <!-- 按时间播放对话框 -->
    <div
      v-if="timePlaybackOpen"
      class="ui-modal-backdrop bg-black/80"
      @click.self="closeTimePlayback"
    >
      <div class="ui-modal max-w-3xl overflow-hidden">
        <div class="flex items-center justify-between px-4 py-3 border-b border-slate-200">
          <div>
            <h3 class="font-semibold text-slate-800">按时间播放录像</h3>
            <p v-if="playbackState.point" class="text-xs text-slate-500">
              {{ cameraName(timeSearch.cameraId) }} ·
              {{ formatTime(playbackState.point.segment.start_time) }} -
              {{ formatTime(playbackState.point.segment.end_time) }}
            </p>
          </div>
          <button class="ui-icon-button" title="关闭" aria-label="关闭" @click="closeTimePlayback">
            <AppIcon name="close" class="w-4 h-4" />
          </button>
        </div>
        <div class="relative compat-aspect-video bg-black flex items-center justify-center">
          <video
            :ref="(element) => attachPlaybackVideo(0, element)"
            v-show="playbackState.activeSlot === 0 && !playbackState.gap"
            controls
            :autoplay="playbackState.activeSlot === 0"
            preload="auto"
            class="max-w-full max-h-full"
            @loadedmetadata="handlePlaybackMetadata(0)"
            @canplay="handlePlaybackCanPlay(0)"
            @ended="handlePlaybackEnded(0)"
          ></video>
          <video
            :ref="(element) => attachPlaybackVideo(1, element)"
            v-show="playbackState.activeSlot === 1 && !playbackState.gap"
            controls
            :autoplay="playbackState.activeSlot === 1"
            preload="auto"
            class="max-w-full max-h-full"
            @loadedmetadata="handlePlaybackMetadata(1)"
            @canplay="handlePlaybackCanPlay(1)"
            @ended="handlePlaybackEnded(1)"
          ></video>
          <div v-if="playbackState.loading" class="absolute inset-0 flex items-center justify-center text-sm text-white bg-black/70">
            正在定位录像...
          </div>
          <div v-else-if="playbackState.gap" class="absolute inset-0 flex items-center justify-center text-sm text-white bg-black/70">
            该时间没有录像
          </div>
          <div v-else-if="playbackState.error" class="absolute inset-0 flex items-center justify-center px-6 text-sm text-red-200 bg-black/70">
            {{ playbackState.error }}
          </div>
          <div v-else-if="playbackState.loadingNext" class="absolute inset-x-0 bottom-0 py-2 text-center text-xs text-white bg-black/60">
            正在缓冲下一片段...
          </div>
        </div>
        <div v-if="playbackState.timeline && playbackState.timeline.length" class="px-4 py-3 border-t border-slate-200">
          <p class="mb-2 text-xs text-slate-500">播放片段</p>
          <div class="compat-flex-gap-1 flex-wrap">
            <button
              v-for="segment in playbackState.timeline.slice(0, 5)"
              :key="segment.id"
              type="button"
              class="rounded border px-2 py-1 text-xs"
              :class="playbackState.point && playbackState.point.segment.id === segment.id ? 'border-primary-600 bg-primary-50 text-primary-700' : 'border-slate-200 text-slate-600 hover:bg-slate-50'"
              @click="selectTimelineSegment(segment)"
            >
              {{ formatTime(segment.start_time) }}
            </button>
          </div>
        </div>
      </div>
    </div>

    <!-- 旧版单文件预览对话框 -->
    <div
      v-if="previewRec"
      class="ui-modal-backdrop bg-black/80"
      @click.self="closeLegacyPreview"
    >
      <div class="ui-modal max-w-3xl overflow-hidden">
        <div class="flex items-center justify-between px-4 py-3 border-b border-slate-200">
          <div>
            <h3 class="font-semibold text-slate-800">录像预览 #{{ previewRec.id }}</h3>
            <p class="text-xs text-slate-500">{{ cameraName(previewRec.camera_id) }} · {{ formatTime(previewRec.start_time) }}</p>
          </div>
          <button
            @click="closeLegacyPreview"
            class="ui-icon-button"
            title="关闭"
            aria-label="关闭"
          >
            <AppIcon name="close" class="w-4 h-4" />
          </button>
        </div>
        <div class="compat-aspect-video bg-black flex items-center justify-center">
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
            class="ui-button-primary compat-flex-gap-1"
          >
            <AppIcon name="download" class="w-4 h-4" />
            <span>下载</span>
          </a>
        </div>
      </div>
    </div>

    <!-- 计划编辑对话框 -->
    <div
      v-if="showScheduleDialog"
      class="ui-modal-backdrop"
      @click.self="showScheduleDialog = false"
    >
      <div class="ui-modal max-w-md p-6">
        <h3 class="text-lg font-semibold text-slate-800 mb-4">
          {{ scheduleForm.id ? '编辑计划' : '新建计划' }}
        </h3>

        <div class="space-y-4">
          <div>
            <label class="block text-sm font-medium text-slate-700 mb-1">名称</label>
            <input
              v-model="scheduleForm.name"
              type="text"
              class="ui-input"
              placeholder="白天录像"
            />
          </div>

          <div>
            <label class="block text-sm font-medium text-slate-700 mb-1">摄像头</label>
            <select
              v-model="scheduleForm.camera_id"
              class="ui-select"
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
                class="ui-input"
              />
            </div>
            <div>
              <label class="block text-sm font-medium text-slate-700 mb-1">结束时间</label>
              <input
                v-model="scheduleForm.end_time"
                type="time"
                class="ui-input"
              />
            </div>
          </div>

          <div>
            <label class="block text-sm font-medium text-slate-700 mb-1.5">重复星期</label>
            <div class="compat-flex-gap-1 flex-wrap">
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
                class="ui-select"
              >
                <option value="mp4">MP4</option>
				<option value="ts">TS</option>
              </select>
            </div>
            <div>
              <label class="block text-sm font-medium text-slate-700 mb-1">码率</label>
			  <p class="ui-input bg-slate-50 text-slate-600">原画质（相机码率流拷贝）</p>
            </div>
            <div class="flex items-end">
              <label class="flex items-center gap-2 text-sm pb-2">
                <input v-model="scheduleForm.with_audio" type="checkbox" class="rounded" />
                <span class="text-slate-700">包含音频</span>
              </label>
            </div>
          </div>
        </div>

        <div class="compat-flex-gap-2 justify-end mt-5">
          <button @click="showScheduleDialog = false" class="ui-button-secondary">取消</button>
          <button @click="saveSchedule" :disabled="savingSchedule"
            class="ui-button-primary disabled:opacity-50">
            {{ savingSchedule ? '保存中...' : '保存' }}
          </button>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, reactive, computed, nextTick, onMounted, onUnmounted } from 'vue'
import AppIcon from '../components/AppIcon.vue'
import {
  listCameras, listRecordings, stopRecording, deleteRecording,
  listSchedules, createSchedule, updateSchedule, deleteSchedule,
  connectEventBus, getRecordingTimeline, resolveRecordingPlayback,
  getSegmentMediaUrl, normalizeRecordingDateRange, normalizeRecordingPlayback,
  createRecordingHistoryCoordinator,
  createRecordingPlaybackCoordinator,
	normalizeResourceSafeRecordingOptions,
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
const historyError = ref('')
const timelineError = ref('')
const timePlaybackOpen = ref(false)
const playbackState = ref({
  activeSlot: 0,
  point: null,
  gap: false,
  error: '',
  loading: false,
  loadingNext: false,
  timeline: [],
})
let eventWs = null

const filter = reactive({
  cameraId: null,
  status: '',
})

const timeSearch = reactive({
  cameraId: null,
  startDate: '',
  endDate: '',
  at: '',
})

const playbackCoordinator = createRecordingPlaybackCoordinator({
  resolvePlayback: resolveRecordingPlayback,
  loadTimeline: getRecordingTimeline,
  mediaUrl: getSegmentMediaUrl,
  onStateChange: (state) => {
    playbackState.value = state
  },
})

const pageSize = 20
const page = ref(1)
const totalPages = computed(() => Math.max(1, Math.ceil(total.value / pageSize)))

const historyCoordinator = createRecordingHistoryCoordinator({
  listRecordings,
  onStateChange: (state) => {
    recordings.value = state.recordings
    total.value = state.total
    loading.value = state.loading
    historyError.value = state.error
  },
})

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
  bitrate: 0,
  enabled: true,
})
const scheduleForm = ref(defaultScheduleForm())

const loadRecordings = async () => {
  try {
    const params = { page: page.value, page_size: pageSize }
    if (filter.cameraId) params.camera_id = filter.cameraId
    if (filter.status) params.status = filter.status
    Object.assign(params, normalizeRecordingDateRange(timeSearch))
    await historyCoordinator.load(params)
  } catch (err) {
    historyCoordinator.reportError(err)
  }
}

const loadCameras = async () => {
  cameras.value = await listCameras()
}

const loadSchedules = async () => {
  const loaded = await listSchedules()
  schedules.value = loaded.map((schedule) => ({
    ...schedule,
    ...normalizeResourceSafeRecordingOptions(schedule),
  }))
}

onMounted(async () => {
  await loadCameras()
  initializeTimeSearch()
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
  playbackCoordinator.close()
})

const goPage = (p) => {
  if (p < 1 || p > totalPages.value) return
  page.value = p
  loadRecordings()
}

const applyHistoryFilters = async () => {
  page.value = 1
  await loadRecordings()
}

const clearDateRange = async () => {
  timeSearch.startDate = ''
  timeSearch.endDate = ''
  await applyHistoryFilters()
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

const toDatetimeLocal = (value) => {
  const date = value instanceof Date ? value : new Date(value)
  const pad = (number) => String(number).padStart(2, '0')
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}T${pad(date.getHours())}:${pad(date.getMinutes())}:${pad(date.getSeconds())}`
}

const initializeTimeSearch = () => {
  const now = new Date()
  timeSearch.cameraId = filter.cameraId || cameras.value[0]?.id || null
  timeSearch.at = toDatetimeLocal(now)
}

const attachPlaybackVideo = (slot, video) => {
  playbackCoordinator.attach(slot, video)
}

const handlePlaybackMetadata = (slot) => playbackCoordinator.loadedMetadata(slot)
const handlePlaybackCanPlay = (slot) => playbackCoordinator.canPlay(slot)
const handlePlaybackEnded = (slot) => playbackCoordinator.ended(slot)

const playSelectedTime = async (at = timeSearch.at) => {
  timelineError.value = ''
  try {
    const params = normalizeRecordingPlayback({ cameraId: timeSearch.cameraId, at })
    timePlaybackOpen.value = true
    await nextTick()
    await playbackCoordinator.open({
      cameraId: params.camera_id,
      at: params.at,
    })
  } catch (err) {
    timelineError.value = err.response?.data?.message || err.message || '录像加载失败'
  }
}

const openTimePlayback = async (rec) => {
  if (rec) {
    timeSearch.cameraId = rec.camera_id
    timeSearch.at = toDatetimeLocal(rec.start_time)
  }
  await playSelectedTime(rec ? rec.start_time : timeSearch.at)
}

const selectTimelineSegment = async (segment) => {
  timeSearch.at = toDatetimeLocal(segment.start_time)
  await playSelectedTime(segment.start_time)
}

const closeTimePlayback = () => {
  playbackCoordinator.close()
  timePlaybackOpen.value = false
}

const isSegmentedRecording = (rec) => rec.storage_mode === 'segmented'

const downloadUrl = (id) => {
  const token = localStorage.getItem('token')
  return `/api/v1/recordings/${id}/download?token=${token}`
}

const previewRecording = (rec) => {
  previewRec.value = rec
}

const openRecordingPreview = (rec) => {
  if (isSegmentedRecording(rec)) {
    openTimePlayback(rec)
    return
  }
  previewRecording(rec)
}

const closeLegacyPreview = () => {
  previewRec.value = null
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
		scheduleForm.value = { ...defaultScheduleForm(), ...sch, ...normalizeResourceSafeRecordingOptions(sch) }
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
	const data = { ...scheduleForm.value, ...normalizeResourceSafeRecordingOptions(scheduleForm.value) }
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
