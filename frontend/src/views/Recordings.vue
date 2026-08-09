<template>
  <div>
    <div class="ui-page-header">
      <div><h1 class="ui-page-title">录像中心</h1><p class="ui-page-description">浏览、预览和下载所有录像文件</p></div>
    </div>

    <el-tabs v-model="activeTab" class="mb-4">
      <el-tab-pane label="录像列表" name="recordings">
        <template #label><span class="compat-flex-gap-1"><AppIcon name="film" class="w-4 h-4" /><span>录像列表</span></span></template>
        <el-card shadow="never" class="mb-4">
          <div class="flex flex-wrap items-end gap-3">
            <div><label class="block text-xs font-medium text-slate-500 mb-1">摄像头</label><el-select v-model="filter.cameraId" clearable placeholder="全部" class="w-40" @change="applyHistoryFilters"><el-option label="全部" :value="null" /><el-option v-for="cam in cameras" :key="cam.id" :label="cam.name" :value="cam.id" /></el-select></div>
            <div><label class="block text-xs font-medium text-slate-500 mb-1">状态</label><el-select v-model="filter.status" clearable placeholder="全部" class="w-32" @change="applyHistoryFilters"><el-option label="录制中" value="recording" /><el-option label="已完成" value="completed" /><el-option label="失败" value="failed" /></el-select></div>
            <div><label class="block text-xs font-medium text-slate-500 mb-1">录像日期</label><el-date-picker v-model="dateRange" type="daterange" range-separator="至" value-format="YYYY-MM-DD" start-placeholder="开始日期" end-placeholder="结束日期" /></div>
            <el-button plain @click="clearDateRange">清除日期</el-button>
            <el-button type="primary" @click="applyHistoryFilters">查询历史</el-button>
            <el-button plain @click="loadRecordings">刷新</el-button>
            <span class="ml-auto text-xs text-slate-500">共 {{ total }} 条记录</span>
          </div>
          <el-alert v-if="historyError" :title="historyError" type="error" :closable="false" class="mt-3" />
        </el-card>

        <el-alert v-if="loading" title="加载中..." type="info" :closable="false" />
        <el-empty v-else-if="recordings.length === 0" description="暂无录像记录" class="py-12" />
        <el-card v-else shadow="never" class="overflow-hidden">
          <el-table :data="recordings" class="w-full">
            <el-table-column prop="id" label="ID" width="72" />
            <el-table-column label="摄像头" min-width="130"><template #default="{ row = {} }">{{ cameraName(row.camera_id) }}</template></el-table-column>
            <el-table-column label="开始时间" min-width="180"><template #default="{ row = {} }">{{ formatTime(row.start_time) }}</template></el-table-column>
            <el-table-column label="时长" width="100"><template #default="{ row = {} }">{{ formatDuration(row.duration) }}</template></el-table-column>
            <el-table-column label="大小" width="100"><template #default="{ row = {} }">{{ formatSize(row.file_size) }}</template></el-table-column>
            <el-table-column label="来源" width="90"><template #default="{ row = {} }"><el-tag effect="plain" size="small">{{ triggerLabel(row.trigger_type) }}</el-tag></template></el-table-column>
            <el-table-column label="状态" width="100"><template #default="{ row = {} }"><el-tag :type="statusType(row.status)" effect="plain" size="small">{{ statusLabel(row.status) }}</el-tag></template></el-table-column>
            <el-table-column label="操作" width="150" align="right"><template #default="{ row = {} }"><div class="compat-flex-gap-1 justify-end"><el-button v-if="row.status === 'completed'" text type="primary" @click="openRecordingPreview(row)">预览</el-button><a v-if="row.status === 'completed' && !isSegmentedRecording(row)" :href="downloadUrl(row.id)" :download="`recording_${row.id}.${row.format || 'mp4'}`"><el-button text type="primary">下载</el-button></a><el-button v-else-if="row.status === 'completed'" text disabled>导出</el-button><el-button v-if="row.status === 'recording'" text type="danger" @click="handleStopRecording(row)">停止</el-button><el-button text type="danger" @click="handleDeleteRecording(row)">删除</el-button></div></template></el-table-column>
          </el-table>
          <div v-if="totalPages > 1" class="flex justify-end pt-4"><el-pagination v-model:current-page="page" :page-size="pageSize" :total="total" layout="prev, pager, next" @current-change="goPage" /></div>
        </el-card>
      </el-tab-pane>

      <el-tab-pane label="定时录像" name="schedules">
        <template #label><span class="compat-flex-gap-1"><AppIcon name="clock" class="w-4 h-4" /><span>定时录像</span></span></template>
        <el-card shadow="never" class="mb-4"><div class="flex flex-wrap items-center justify-between gap-3"><p class="text-sm text-slate-600">定时计划：到时间自动开始录像，离开时间范围自动停止，每天重复。</p><el-button type="primary" @click="openScheduleDialog(null)">新建计划</el-button></div></el-card>
        <el-empty v-if="schedules.length === 0" description="暂无定时录像计划" class="py-12" />
        <div v-else class="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-3 gap-4"><el-card v-for="sch in schedules" :key="sch.id" shadow="never" :class="{ 'opacity-60': !sch.enabled }"><template #header><div class="flex items-center justify-between"><div><strong>{{ sch.name }}</strong><p class="text-xs text-slate-500 mt-1">{{ cameraName(sch.camera_id) }}</p></div><el-tag :type="sch.enabled ? 'success' : 'info'" effect="plain">{{ sch.enabled ? '启用' : '停用' }}</el-tag></div></template><div class="space-y-2 text-sm text-slate-600"><p>{{ sch.start_time }} - {{ sch.end_time }}</p><p>{{ daysLabel(sch.days) }}</p><p>{{ (sch.format || 'mp4').toUpperCase() }}<el-tag v-if="sch.with_audio" size="small" effect="plain" class="ml-2">含音频</el-tag></p></div><div class="mt-4 compat-flex-gap-2"><el-button plain @click="toggleSchedule(sch)">{{ sch.enabled ? '停用' : '启用' }}</el-button><el-button plain @click="openScheduleDialog(sch)">编辑</el-button><el-button plain type="danger" @click="handleDeleteSchedule(sch)">删除</el-button></div></el-card></div>
      </el-tab-pane>
    </el-tabs>

    <el-dialog v-model="previewOpen" :title="previewRec ? `录像预览 #${previewRec.id}` : '录像预览'" width="760px" @closed="closePreview">
      <p v-if="previewRec" class="text-xs text-slate-500 mb-3">{{ cameraName(previewRec.camera_id) }} · {{ formatTime(previewRec.start_time) }}</p>
      <el-alert v-if="previewError" :title="previewError" type="error" :closable="false" class="mb-3" />
      <div v-else-if="previewLoading" class="py-12 text-center text-slate-500">正在加载录像...</div>
      <div v-else class="compat-aspect-video bg-black flex items-center justify-center"><video v-if="previewRec" :src="previewMediaUrl" controls autoplay preload="metadata" class="max-w-full max-h-full"></video></div>
      <template #footer><a v-if="previewRec && !isSegmentedRecording(previewRec)" :href="downloadUrl(previewRec.id)" :download="`recording_${previewRec.id}.${previewRec.format || 'mp4'}`"><el-button type="primary">下载</el-button></a><el-button plain @click="previewOpen = false">关闭</el-button></template>
    </el-dialog>

    <el-dialog v-model="showScheduleDialog" :title="scheduleForm.id ? '编辑计划' : '新建计划'" width="520px">
      <el-form label-position="top" @submit.prevent="saveSchedule"><el-form-item label="名称"><el-input v-model="scheduleForm.name" placeholder="白天录像" /></el-form-item><el-form-item label="摄像头"><el-select v-model="scheduleForm.camera_id" class="w-full"><el-option v-for="cam in cameras" :key="cam.id" :label="`${cam.name} (${cam.ip})`" :value="cam.id" /></el-select></el-form-item><div class="grid grid-cols-2 gap-3"><el-form-item label="开始时间"><el-time-picker v-model="scheduleForm.start_time" value-format="HH:mm" format="HH:mm" class="w-full" /></el-form-item><el-form-item label="结束时间"><el-time-picker v-model="scheduleForm.end_time" value-format="HH:mm" format="HH:mm" class="w-full" /></el-form-item></div><el-form-item label="重复星期"><el-checkbox-group v-model="selectedDays"><el-checkbox-button v-for="(d, i) in weekdays" :key="d.name" :label="i">{{ d.short }}</el-checkbox-button></el-checkbox-group></el-form-item><div class="grid grid-cols-2 gap-3"><el-form-item label="格式"><el-select v-model="scheduleForm.format"><el-option label="MP4" value="mp4" /><el-option label="TS" value="ts" /></el-select></el-form-item><el-form-item label="音频" class="flex items-center"><el-checkbox v-model="scheduleForm.with_audio">包含音频</el-checkbox></el-form-item></div></el-form>
      <template #footer><el-button plain @click="showScheduleDialog = false">取消</el-button><el-button type="primary" :loading="savingSchedule" @click="saveSchedule">保存</el-button></template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, reactive, computed, onMounted, onUnmounted } from 'vue'
import AppIcon from '../components/AppIcon.vue'
import { listCameras, listRecordings, stopRecording, deleteRecording, listSchedules, createSchedule, updateSchedule, deleteSchedule, connectEventBus, resolveRecordingPlayback, getSegmentMediaUrl, normalizeRecordingDateRange, normalizeRecordingPlayback, createRecordingHistoryCoordinator, normalizeResourceSafeRecordingOptions } from '../api'

const activeTab = ref('recordings')
const cameras = ref([])
const recordings = ref([])
const total = ref(0)
const loading = ref(true)
const schedules = ref([])
const showScheduleDialog = ref(false)
const savingSchedule = ref(false)
const historyError = ref('')
const previewRec = ref(null)
const previewOpen = ref(false)
const previewLoading = ref(false)
const previewError = ref('')
const previewMediaUrl = ref('')
let eventWs = null
const filter = reactive({ cameraId: null, status: '' })
const timeSearch = reactive({ startDate: '', endDate: '' })
const dateRange = computed({ get: () => timeSearch.startDate || timeSearch.endDate ? [timeSearch.startDate, timeSearch.endDate] : [], set: (value) => { timeSearch.startDate = value?.[0] || ''; timeSearch.endDate = value?.[1] || '' } })
const pageSize = 20
const page = ref(1)
const totalPages = computed(() => Math.max(1, Math.ceil(total.value / pageSize)))
const historyCoordinator = createRecordingHistoryCoordinator({ listRecordings, onStateChange: (state) => { recordings.value = state.recordings; total.value = state.total; loading.value = state.loading; historyError.value = state.error } })
const weekdays = [{ name: '周一', short: '一' }, { name: '周二', short: '二' }, { name: '周三', short: '三' }, { name: '周四', short: '四' }, { name: '周五', short: '五' }, { name: '周六', short: '六' }, { name: '周日', short: '日' }]
const defaultScheduleForm = () => ({ id: null, name: '', camera_id: cameras.value[0]?.id || null, start_time: '09:00', end_time: '17:00', days: 127, format: 'mp4', with_audio: false, bitrate: 0, enabled: true })
const scheduleForm = ref(defaultScheduleForm())
const selectedDays = computed({ get: () => weekdays.map((_, index) => index).filter((index) => scheduleForm.value.days & (1 << index)), set: (days) => { scheduleForm.value.days = days.reduce((mask, index) => mask | (1 << index), 0) } })

const loadRecordings = async () => { try { const params = { page: page.value, page_size: pageSize }; if (filter.cameraId) params.camera_id = filter.cameraId; if (filter.status) params.status = filter.status; Object.assign(params, normalizeRecordingDateRange(timeSearch)); await historyCoordinator.load(params) } catch (err) { historyCoordinator.reportError(err) } }
const loadCameras = async () => { cameras.value = await listCameras() }
const loadSchedules = async () => { const loaded = await listSchedules(); schedules.value = loaded.map((schedule) => ({ ...schedule, ...normalizeResourceSafeRecordingOptions(schedule) })) }
onMounted(async () => { await loadCameras(); await loadRecordings(); await loadSchedules(); eventWs = connectEventBus((event) => { if (event.type === 'recording_status') loadRecordings() }) })
onUnmounted(() => { if (eventWs) eventWs.close() })
const goPage = (nextPage) => { if (nextPage < 1 || nextPage > totalPages.value) return; page.value = nextPage; loadRecordings() }
const applyHistoryFilters = async () => { page.value = 1; await loadRecordings() }
const clearDateRange = async () => { dateRange.value = []; await applyHistoryFilters() }
const cameraName = (id) => cameras.value.find((camera) => camera.id === id)?.name || `Camera #${id}`
const statusLabel = (status) => ({ recording: '录制中', completed: '已完成', failed: '失败' }[status] || status)
const statusType = (status) => ({ recording: 'danger', completed: 'success', failed: 'warning' }[status] || 'info')
const triggerLabel = (trigger) => ({ api: '手动', manual: '手动', schedule: '定时' }[trigger] || trigger || '手动')
const daysLabel = (days) => days === 127 ? '每天' : weekdays.filter((_, index) => days & (1 << index)).map((day) => day.name).join(' ') || '无'
const formatTime = (value) => value ? new Date(value).toLocaleString('zh-CN') : '-'
const formatDuration = (seconds) => { if (!seconds) return '-'; const h = Math.floor(seconds / 3600); const m = Math.floor((seconds % 3600) / 60); const s = seconds % 60; return h > 0 ? `${h}:${String(m).padStart(2, '0')}:${String(s).padStart(2, '0')}` : `${m}:${String(s).padStart(2, '0')}` }
const formatSize = (bytes) => { if (!bytes) return '-'; if (bytes < 1024) return `${bytes} B`; if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`; if (bytes < 1024 * 1024 * 1024) return `${(bytes / 1024 / 1024).toFixed(1)} MB`; return `${(bytes / 1024 / 1024 / 1024).toFixed(2)} GB` }
const isSegmentedRecording = (recording) => recording.storage_mode === 'segmented'
const downloadUrl = (id) => { const token = localStorage.getItem('token'); return `/api/v1/recordings/${id}/download?token=${token}` }
const openRecordingPreview = async (recording) => { previewRec.value = recording; previewOpen.value = true; previewLoading.value = true; previewError.value = ''; previewMediaUrl.value = ''; try { if (isSegmentedRecording(recording)) { const playback = await resolveRecordingPlayback(normalizeRecordingPlayback({ cameraId: recording.camera_id, at: recording.start_time })); previewMediaUrl.value = getSegmentMediaUrl(playback.segment.id) } else { previewMediaUrl.value = downloadUrl(recording.id) } } catch (err) { previewError.value = err.response?.data?.message || err.message || '录像加载失败' } finally { previewLoading.value = false } }
const closePreview = () => { previewRec.value = null; previewMediaUrl.value = ''; previewError.value = '' }
const handleStopRecording = async (recording) => { if (!confirm('确定停止该录像吗？')) return; try { await stopRecording(recording.id); loadRecordings() } catch (err) { alert('停止失败: ' + (err.response?.data?.message || err.message)) } }
const handleDeleteRecording = async (recording) => { if (!confirm(`确定删除录像 #${recording.id} 吗？视频文件将一并删除，不可恢复！`)) return; try { await deleteRecording(recording.id); loadRecordings() } catch (err) { alert('删除失败: ' + (err.response?.data?.message || err.message)) } }
const openScheduleDialog = (schedule) => { scheduleForm.value = schedule ? { ...defaultScheduleForm(), ...schedule, ...normalizeResourceSafeRecordingOptions(schedule) } : defaultScheduleForm(); showScheduleDialog.value = true }
const saveSchedule = async () => { savingSchedule.value = true; try { const data = { ...scheduleForm.value, ...normalizeResourceSafeRecordingOptions(scheduleForm.value) }; if (data.id) await updateSchedule(data.id, data); else { delete data.id; await createSchedule(data) }; showScheduleDialog.value = false; await loadSchedules() } catch (err) { alert('保存失败: ' + (err.response?.data?.message || err.message)) } finally { savingSchedule.value = false } }
const toggleSchedule = async (schedule) => { try { await updateSchedule(schedule.id, { ...schedule, enabled: !schedule.enabled }); await loadSchedules() } catch (err) { alert('操作失败: ' + (err.response?.data?.message || err.message)) } }
const handleDeleteSchedule = async (schedule) => { if (!confirm(`确定删除计划 "${schedule.name}" 吗？`)) return; try { await deleteSchedule(schedule.id); await loadSchedules() } catch (err) { alert('删除失败: ' + (err.response?.data?.message || err.message)) } }
</script>
