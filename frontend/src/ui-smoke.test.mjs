import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { compile } from '@vue/compiler-dom'
import { parse, compileScript } from '@vue/compiler-sfc'
import * as Vue from 'vue'
import { renderToString } from '@vue/server-renderer'

const css = readFileSync(new URL('./assets/main.css', import.meta.url), 'utf8')
const recordingBehavior = await import('./api.js')

const viewSource = (name) => readFileSync(new URL(`./views/${name}.vue`, import.meta.url), 'utf8')

const renderTemplateFragment = async (source, startMarker, endMarker, context) => {
  const template = parse(source).descriptor.template.content
  const start = template.indexOf(startMarker)
  const end = template.indexOf(endMarker, start + startMarker.length)
  assert.notEqual(start, -1, `missing fragment start ${startMarker}`)
  assert.notEqual(end, -1, `missing fragment end ${endMarker}`)
  const includeEndMarker = endMarker.startsWith('</') ? endMarker.length : 0
  const fragment = template.slice(start + startMarker.length, end + includeEndMarker)
  const { code } = compile(`<div>${fragment}</div>`, { mode: 'function' })
  const render = new Function('Vue', code)(Vue)
  const app = Vue.createSSRApp({ setup: () => context, render })
  app.component('el-dialog', {
    setup(_, { slots }) {
      return () => Vue.h('section', slots.default?.())
    },
  })
  app.component('el-button', {
    setup(_, { slots }) {
      return () => Vue.h('button', slots.default?.())
    },
  })
  app.component('AppIcon', { render: () => null })
  app.config.warnHandler = () => {}
  return renderToString(app)
}

test('Element Plus is registered globally with Chinese locale and a restrained plain theme', () => {
  const packageManifest = JSON.parse(readFileSync(new URL('../package.json', import.meta.url), 'utf8'))
  const main = readFileSync(new URL('./main.js', import.meta.url), 'utf8')

  assert.ok(packageManifest.dependencies['element-plus'], 'Element Plus must be an application dependency')
  assert.match(main, /import ElementPlus from 'element-plus'/)
  assert.match(main, /import zhCn from 'element-plus\/es\/locale\/lang\/zh-cn'/)
  assert.match(main, /import 'element-plus\/dist\/index\.css'/)
  assert.match(main, /app\.use\(ElementPlus, \{ locale: zhCn \}\)/)

  for (const variable of [
    '--el-bg-color: #ffffff',
    '--el-bg-color-page: #f6f8fc',
    '--el-border-color: #dbe3ee',
    '--el-text-color-primary: #172033',
    '--el-color-primary: #2563eb',
    '--el-box-shadow-light: 0 4px 12px rgba(15, 23, 42, 0.06)',
    '--el-button-bg-color: var(--surface)',
    '--el-button-border-color: var(--line)',
  ]) {
    assert.ok(css.includes(variable), `missing plain Element Plus theme variable ${variable}`)
  }
})

const loadRecordingsSetup = async () => {
  const source = readFileSync(new URL('./views/Recordings.vue', import.meta.url), 'utf8')
  const descriptor = parse(source).descriptor
  const vueURL = import.meta.resolve('vue')
  const apiURL = new URL('./api.js', import.meta.url).href
  const compiled = compileScript(descriptor, { id: 'recordings-test' }).content
    .replace(/from 'vue'/g, `from '${vueURL}'`)
    .replace(/import AppIcon from '[^']+'\n/, 'const AppIcon = null\n')
    .replace(/from '\.\.\/api'/g, `from '${apiURL}'`)
  const moduleURL = `data:text/javascript;base64,${Buffer.from(compiled).toString('base64')}`
  const component = (await import(moduleURL)).default
  let setup
  const app = Vue.createSSRApp({
    setup(props, context) {
      setup = component.setup(props, context)
      return () => null
    },
  })
  await renderToString(app)
  return setup
}

const renderRecordings = async (overrides = {}) => {
  const source = readFileSync(new URL('./views/Recordings.vue', import.meta.url), 'utf8')
  const descriptor = parse(source).descriptor
  const { code } = compile(descriptor.template.content, { mode: 'function' })
  const render = new Function('Vue', code)(Vue)
  const segment = {
    id: 1,
    recording_id: 18,
    start_time: '2026-08-08T10:10:00Z',
    end_time: '2026-08-08T10:20:00Z',
    status: 'completed',
  }
  const context = {
    activeTab: 'recordings',
    cameras: [{ id: 7, name: 'North Gate', ip: '192.0.2.7' }],
    recordings: [{
      id: 18,
      camera_id: 7,
      start_time: segment.start_time,
      duration: 600,
      file_size: 1000,
      trigger_type: 'schedule',
      status: 'completed',
      storage_mode: 'segmented',
      format: 'mp4',
    }],
    total: 1,
    loading: false,
    filter: { cameraId: 7, status: '' },
    totalPages: 1,
    page: 1,
    schedules: [],
    previewRec: null,
    previewOpen: false,
    previewLoading: false,
    previewError: '',
    previewMediaUrl: '/legacy-download',
    previewPlayback: { activeSlot: 0, gap: false },
    showScheduleDialog: false,
    savingSchedule: false,
    scheduleForm: {},
    weekdays: [],
    selectedDays: [],
    timeSearch: {
      startDate: '2026-08-08',
      endDate: '2026-08-08',
    },
    dateRange: ['2026-08-08', '2026-08-08'],
    historyError: '',
    loadRecordings() {},
    applyHistoryFilters() {},
    clearDateRange() {},
    openRecordingPreview() {},
    closePreview() {},
    attachPreviewVideo() {},
    handlePreviewMetadata() {},
    handlePreviewCanPlay() {},
    handlePreviewEnded() {},
    handleStopRecording() {},
    handleDeleteRecording() {},
    goPage() {},
    openScheduleDialog() {},
    toggleSchedule() {},
    handleDeleteSchedule() {},
    saveSchedule() {},
    cameraName: () => 'North Gate',
    formatTime: (value) => value,
    formatDuration: () => '10:00',
    formatSize: () => '1000 B',
    triggerClass: () => '',
    triggerLabel: () => '定时',
    statusLabel: () => '已完成',
    statusType: () => 'success',
    downloadUrl: () => '/legacy-download',
    isSegmentedRecording: (recording) => recording.storage_mode === 'segmented',
    coverageTitle: (part) => part.type === 'gap' ? '录像空档' : '可播放录像',
    ...overrides,
  }
  const app = Vue.createSSRApp({ setup: () => context, render })
  app.component('AppIcon', { render: () => null })
  app.component('el-table-column', {
    setup(_, { slots }) {
      return () => slots.default?.({ row: context.recordings[0] })
    },
  })
  app.config.warnHandler = () => {}
  return renderToString(app)
}

test('bright design system exposes shared primitives and Chrome 72 fallbacks', () => {
  for (const selector of [
    '.app-shell', '.ui-card', '.ui-button-primary', '.ui-input', '.ui-status',
    '--chrome72-flex-gap-fallback', '@supports not (aspect-ratio: 1 / 1)',
  ]) {
    assert.ok(css.includes(selector), `missing ${selector}`)
  }
})

test('compatible flex helpers provide spacing in modern browsers and a Chrome 72 fallback', () => {
  for (const [name, value] of [
    ['1', '0.25rem'],
    ['2', '0.5rem'],
    ['3', '0.75rem'],
    ['4', '1rem'],
  ]) {
    const marginRule = `.compat-flex-gap-${name} > * + * { margin-left: ${value}; }`
    assert.ok(css.includes(marginRule), `missing default fallback ${marginRule}`)
    assert.match(css, new RegExp(`\\.has-flex-gap \\.compat-flex-gap-${name} \\{ gap: ${value}; \\}`))
  }
  assert.ok(css.includes('.compat-flex-column > * + * { margin-top: 0.75rem; }'))
  assert.match(css, /\.has-flex-gap \.compat-flex-column \{ gap: 0\.75rem; \}/)
  const compat = readFileSync(new URL('./compat.js', import.meta.url), 'utf8')
  assert.match(compat, /has-flex-gap/)
})

test('application shell uses a light navigation surface and an accessible mobile toggle', () => {
  const layout = readFileSync(new URL('./components/Layout.vue', import.meta.url), 'utf8')
  assert.match(layout, /app-shell/)
  assert.match(layout, /aria-label="切换导航"/)
  assert.doesNotMatch(layout, /bg-slate-900/)
  assert.match(css, /\.layout-sidebar \{ top: 60px; right: auto;/)
})

test('shell, login, and FFmpeg feedback use Element Plus while retaining authentication and mobile navigation behavior', () => {
  const layout = readFileSync(new URL('./components/Layout.vue', import.meta.url), 'utf8')
  const login = readFileSync(new URL('./views/Login.vue', import.meta.url), 'utf8')
  const ffmpegBanner = readFileSync(new URL('./components/FfmpegBanner.vue', import.meta.url), 'utf8')

  assert.match(layout, /<el-container/)
  assert.match(layout, /<el-aside/)
  assert.match(layout, /<el-menu/)
  assert.match(layout, /aria-label="切换导航"/)
  assert.match(layout, /mobileNavOpen = !mobileNavOpen/)
  assert.match(layout, /@click="handleLogout"/)
  assert.match(layout, /const handleLogout = \(\) => logout\(\)/)

  assert.match(login, /<el-card/)
  assert.match(login, /<el-form/)
  assert.match(login, /<el-alert/)
  assert.match(login, /@submit\.prevent="handleLogin"/)
  assert.match(login, /await login\(username\.value, password\.value\)/)
  assert.match(login, /router\.push\('\/cameras'\)/)

  assert.match(ffmpegBanner, /<el-alert/)
})

test('camera management uses Element Plus primitives while retaining camera operations', () => {
  const cameras = readFileSync(new URL('./views/Cameras.vue', import.meta.url), 'utf8')
  for (const primitive of ['el-card', 'el-tag', 'el-form', 'el-input', 'el-select', 'el-radio-group', 'el-checkbox-group', 'el-dialog', 'el-tooltip', 'el-empty', 'el-alert']) {
    assert.match(cameras, new RegExp(`<${primitive}`), `camera management must use ${primitive}`)
  }
  for (const handler of ['handleScanLAN', 'handleSubmit', 'handleTest', 'handleDelete', 'handleSetCodec', 'handleSubmitNetwork', 'handleSyncTime']) {
    assert.match(cameras, new RegExp(handler), `camera management must retain ${handler}`)
  }
})

test('camera dialog layout uses a single aligned protocol choice group and compact field grid', () => {
  const cameras = readFileSync(new URL('./views/Cameras.vue', import.meta.url), 'utf8')

  assert.match(cameras, /class="camera-dialog-form"/)
  assert.match(cameras, /class="camera-protocol-choice"/)
  assert.match(cameras, /class="camera-protocol-options"/)
  assert.match(cameras, /class="camera-form-grid camera-form-grid--network"/)
  assert.match(cameras, /class="camera-stream-choice"/)
  assert.match(cameras, /<template #footer>[\s\S]*?class="camera-dialog-footer\b/)
  assert.doesNotMatch(cameras, /<div class="grid grid-cols-3 gap-2">\s*<el-radio-group/)
})

test('live view uses Element Plus feedback and popover controls while retaining native media operations', () => {
  const live = readFileSync(new URL('./views/Live.vue', import.meta.url), 'utf8')
  for (const primitive of ['el-card', 'el-popover', 'el-checkbox-group', 'el-dialog', 'el-tooltip', 'el-empty', 'el-alert']) {
    assert.match(live, new RegExp(`<${primitive}`), `live view must use ${primitive}`)
  }
  for (const handler of ['startStream', 'stopStream', 'startStreamFor', 'stopStreamFor', 'captureSnapshot', 'takeSnapshot', 'toggleRecord', 'confirmStartRecording']) {
    assert.match(live, new RegExp(handler), `live view must retain ${handler}`)
  }
  assert.match(live, /getMjpegUrl\(cam\.id\)/)
  assert.match(live, /:src="snapshotURL"/)
  assert.match(live, /compat-aspect-video/)
})

test('live view lets operators select the cameras shown in the preview grid', () => {
  const live = readFileSync(new URL('./views/Live.vue', import.meta.url), 'utf8')
  assert.match(live, /选择摄像头/)
  assert.match(live, /selectedCameraIDs/)
  assert.match(live, /clearCameraSelection/)
})

test('Element Plus owns the live picker trigger and selection control labels', () => {
  const cameras = readFileSync(new URL('./views/Cameras.vue', import.meta.url), 'utf8')
  const live = readFileSync(new URL('./views/Live.vue', import.meta.url), 'utf8')

  assert.match(live, /<el-popover v-model:visible="showCameraPicker" trigger="click"/)
  assert.doesNotMatch(live, /@click="showCameraPicker = !showCameraPicker"/)
  assert.doesNotMatch(live, /toggleCameraSelection/)
  assert.doesNotMatch(cameras, /<label\b[^>]*>(?:(?!<\/label>)[\s\S])*?<el-(?:radio|checkbox)\b/)
  assert.doesNotMatch(live, /<label\b[^>]*>(?:(?!<\/label>)[\s\S])*?<el-checkbox\b/)
})

test('camera and live controls complete the Element Plus plain migration', () => {
  const cameras = viewSource('Cameras')
  const live = viewSource('Live')

  for (const [name, source] of [['camera', cameras], ['live', live]]) {
    const template = parse(source).descriptor.template.content
    assert.doesNotMatch(template, /<button\b/, `${name} must not retain native action buttons`)
    assert.doesNotMatch(template, /<(?:input|select|textarea)\b/, `${name} must not retain native form controls`)
    assert.doesNotMatch(template, /class="[^"]*\bui-(?:button|icon-button)/, `${name} must not retain custom button styling`)
    assert.doesNotMatch(template, /<el-button\b(?=[^>]*\stype="button")/, `${name} must use native-type for native button semantics`)
  }

  assert.match(cameras, /<el-checkbox(?=[^>]*:value="ch\.channel")[^>]*>[\s\S]*?CH\{\{ ch\.channel \}\}[\s\S]*?<\/el-checkbox>/)
  assert.doesNotMatch(cameras, /<el-checkbox\s+:label="ch\.channel"/)
  assert.match(live, /<el-radio-group\s+v-model="gridSize"/)
  assert.match(live, /<el-radio-group\s+v-model="recordFormat"/)
})

test('camera test-info dialog remains render-safe after its backing object is cleared', async () => {
  await assert.doesNotReject(renderTemplateFragment(
    viewSource('Cameras'),
    '<!-- 测试结果弹窗 -->',
    '<!-- 局域网扫描弹窗 -->',
    { testInfoDialogOpen: false, testInfoModal: null, closeTestInfoDialog() {}, clearTestInfoDialog() {} },
  ))
})

test('live snapshot dialog remains render-safe after its backing object is cleared', async () => {
  await assert.doesNotReject(renderTemplateFragment(
    viewSource('Live'),
    '<!-- 原生抓拍结果 -->',
    '</el-dialog>',
    { snapshotDialogOpen: false, snapshotTarget: null, snapshotURL: '', closeSnapshot() {}, clearSnapshot() {} },
  ))
})

test('recordings view preserves recording and schedule actions in a responsive surface', () => {
  const recordings = readFileSync(new URL('./views/Recordings.vue', import.meta.url), 'utf8')
  assert.match(recordings, /stopRecording/)
  assert.match(recordings, /saveSchedule/)
  assert.match(recordings, /<el-card/)
  assert.match(recordings, /<el-table/)
})

test('every page keeps its Element Plus plain surface and core action without restoring manual playback', () => {
  const pages = {
    login: readFileSync(new URL('./views/Login.vue', import.meta.url), 'utf8'),
    cameras: readFileSync(new URL('./views/Cameras.vue', import.meta.url), 'utf8'),
    live: readFileSync(new URL('./views/Live.vue', import.meta.url), 'utf8'),
    recordings: readFileSync(new URL('./views/Recordings.vue', import.meta.url), 'utf8'),
  }

  for (const [page, primitives] of Object.entries({
    login: ['el-card', 'el-form', 'el-button'],
    cameras: ['el-card', 'el-form', 'el-dialog', 'el-button'],
    live: ['el-card', 'el-popover', 'el-dialog', 'el-button'],
    recordings: ['el-card', 'el-table', 'el-dialog', 'el-button'],
  })) {
    for (const primitive of primitives) {
      assert.match(pages[page], new RegExp(`<${primitive}`), `${page} must retain ${primitive}`)
    }
  }

  assert.match(pages.login, /await login\(username\.value, password\.value\)/)
  for (const action of ['createCamera', 'updateCamera', 'deleteCamera', 'scanNetwork']) {
    assert.match(pages.cameras, new RegExp(`await ${action}`), `camera management must retain ${action}`)
  }
  for (const action of ['startStream', 'stopStream', 'startRecording', 'stopRecording', 'captureSnapshot']) {
    assert.match(pages.live, new RegExp(`await ${action}`), `live view must retain ${action}`)
  }
  for (const action of ['listRecordings', 'stopRecording', 'deleteRecording', 'createSchedule', 'updateSchedule', 'deleteSchedule']) {
    assert.match(pages.recordings, new RegExp(action), `recordings must retain ${action}`)
  }

  assert.match(pages.live, /<img[\s\S]*?:src="getMjpegUrl\(cam\.id\)"/)
  assert.match(pages.recordings, /<video[\s\S]*?controls/)
  for (const manualControl of ['播放摄像头', '播放时间', '播放所选时间', '播放片段']) {
    assert.doesNotMatch(pages.recordings, new RegExp(manualControl), `recordings must not restore ${manualControl}`)
  }
})

test('recordings playback can only originate from a list row, not a timestamp-and-camera panel', () => {
  const recordings = readFileSync(new URL('./views/Recordings.vue', import.meta.url), 'utf8')

  assert.match(recordings, /<el-button[\s\S]*?@click="openRecordingPreview\(row\)"/)
  assert.match(recordings, /const openRecordingPreview = async \(recording\) => \{[\s\S]*?previewRec\.value = recording[\s\S]*?previewOpen\.value = true/)
  assert.match(recordings, /normalizeRecordingPlayback\(\{ cameraId: recording\.camera_id, at: recording\.start_time \}\)/)
  assert.match(recordings, /await previewCoordinator\.open\(\{ cameraId: params\.camera_id, at: params\.at \}\)/)
  assert.equal((recordings.match(/<video/g) || []).length, 2, 'the list preview must retain its sequential native video slots')

  for (const legacyManualPlaybackStructure of [
    /v-model="timeSearch\.cameraId"/,
    /v-model="timeSearch\.at"/,
    /timePlaybackOpen/,
    /playSelectedTime/,
    /openTimePlayback/,
    /attachPlaybackVideo/,
    /handlePlayback(?:Metadata|CanPlay|Ended)/,
    /type="datetime-local"/,
  ]) {
    assert.doesNotMatch(recordings, legacyManualPlaybackStructure, `recordings must not restore manual playback structure: ${legacyManualPlaybackStructure}`)
  }
})

test('recordings history and row actions retain their concrete list, stop, and delete paths', () => {
  const recordings = readFileSync(new URL('./views/Recordings.vue', import.meta.url), 'utf8')

  assert.match(recordings, /createRecordingHistoryCoordinator\(\{ listRecordings,/)
  assert.match(recordings, /const loadRecordings = async \(\) => \{[\s\S]*?page_size: pageSize[\s\S]*?normalizeRecordingDateRange\(timeSearch\)[\s\S]*?await historyCoordinator\.load\(params\)/)
  assert.match(recordings, /@click="handleStopRecording\(row\)"/)
  assert.match(recordings, /@click="handleDeleteRecording\(row\)"/)
  assert.match(recordings, /const handleStopRecording = async \(recording\) => \{[\s\S]*?await stopRecording\(recording\.id\)[\s\S]*?loadRecordings\(\)/)
  assert.match(recordings, /const handleDeleteRecording = async \(recording\) => \{[\s\S]*?await deleteRecording\(recording\.id\)[\s\S]*?loadRecordings\(\)/)
})

test('recordings center uses Element Plus filters, data surfaces, and single-video list preview', () => {
  const recordings = readFileSync(new URL('./views/Recordings.vue', import.meta.url), 'utf8')

  assert.match(recordings, /<el-date-picker[\s\S]*?v-model="dateRange"[\s\S]*?type="daterange"[\s\S]*?range-separator="至"/)
  for (const primitive of ['el-tabs', 'el-select', 'el-table', 'el-pagination', 'el-dialog', 'el-form', 'el-tag', 'el-empty', 'el-alert']) {
    assert.match(recordings, new RegExp(`<${primitive}`), `recordings center must use ${primitive}`)
  }
  assert.match(recordings, /normalizeRecordingDateRange\(timeSearch\)/)
  assert.match(recordings, /page_size: pageSize/)
  assert.equal((recordings.match(/<video/g) || []).length, 2, 'sequential list preview must keep two native video slots')
  assert.doesNotMatch(recordings, /按时间播放录像/)
})

test('recording filter toolbar keeps fields, actions, and result count aligned', () => {
  const recordings = readFileSync(new URL('./views/Recordings.vue', import.meta.url), 'utf8')

  assert.match(recordings, /class="recording-filter-bar"/)
  assert.match(recordings, /class="recording-filter-fields compat-flex-gap-3"/)
  assert.match(recordings, /class="recording-filter-actions compat-flex-gap-2"/)
  assert.match(recordings, /class="recording-filter-count"/)
  assert.match(recordings, /<el-date-picker[\s\S]*?v-model="dateRange"[\s\S]*?type="daterange"[\s\S]*?range-separator="至"/)
  assert.match(recordings, /@click="applyHistoryFilters"/)
  assert.match(recordings, /@click="clearDateRange"/)
})

test('recordings list preview restores guarded native segment continuation and Chrome 72-safe filter spacing', () => {
  const recordings = readFileSync(new URL('./views/Recordings.vue', import.meta.url), 'utf8')

  assert.match(recordings, /createRecordingPlaybackCoordinator/)
  assert.match(recordings, /:ref="\(element\) => attachPreviewVideo\(0, element\)"/)
  assert.match(recordings, /@ended="handlePreviewEnded\(0\)"/)
  assert.match(recordings, /previewCoordinator\.close\(\)/)
  assert.equal((recordings.match(/compat-flex-gap-3/g) || []).length, 2, 'filter and schedule header must use Chrome 72-compatible spacing')
  assert.doesNotMatch(recordings, /items-end gap-3/)
  assert.doesNotMatch(recordings, /justify-between gap-3/)
})

test('recordings use valid download links and surface a missing-time preview gap', async () => {
  const recordings = viewSource('Recordings')
  assert.doesNotMatch(recordings, /<a\b[^>]*>[\s\S]*?<el-button\b/)
  assert.match(recordings, /<el-button(?=[^>]*\btag="a")(?=[^>]*:href="downloadUrl\(row\.id\)")[^>]*>/)

  const html = await renderRecordings({
    previewRec: { id: 18, camera_id: 7, start_time: '2026-08-08T10:10:00Z', storage_mode: 'segmented' },
    previewOpen: true,
    previewPlayback: { activeSlot: 0, gap: true },
  })
  assert.match(html, /该时间没有录像/)
})

test('recording history filters use local date boundaries and playback resolves independently', () => {
  assert.equal(typeof recordingBehavior.normalizeRecordingDateRange, 'function')
  assert.equal(typeof recordingBehavior.normalizeRecordingPlayback, 'function')

  assert.deepEqual(recordingBehavior.normalizeRecordingDateRange({}), {})
  assert.deepEqual(recordingBehavior.normalizeRecordingDateRange({
    startDate: '2026-08-02',
    endDate: '2026-08-09',
  }), {
    start_time: '2026-08-02T00:00:00.000Z',
    end_time: '2026-08-10T00:00:00.000Z',
  })
  assert.deepEqual(recordingBehavior.normalizeRecordingPlayback({
    cameraId: 7,
    at: '2026-08-09T07:50',
  }), {
    camera_id: 7,
    at: '2026-08-09T07:50:00.000Z',
  })
})

test('recording coverage describes completed ranges and explicit gaps', () => {
  assert.equal(typeof recordingBehavior.buildRecordingCoverage, 'function')

  const parts = recordingBehavior.buildRecordingCoverage([
    { id: 1, start_time: '2026-08-08T10:10:00Z', end_time: '2026-08-08T10:20:00Z', status: 'completed' },
    { id: 2, start_time: '2026-08-08T10:40:00Z', end_time: '2026-08-08T11:00:00Z', status: 'completed' },
    { id: 3, start_time: '2026-08-08T10:25:00Z', end_time: '2026-08-08T10:30:00Z', status: 'recording' },
  ], '2026-08-08T10:00:00Z', '2026-08-08T11:00:00Z')

  assert.deepEqual(parts.map((part) => ({
    type: part.type,
    id: part.segment ? part.segment.id : null,
    startPercent: Math.round(part.startPercent),
    widthPercent: Math.round(part.widthPercent),
  })), [
    { type: 'gap', id: null, startPercent: 0, widthPercent: 17 },
    { type: 'recording', id: 1, startPercent: 17, widthPercent: 17 },
    { type: 'gap', id: null, startPercent: 33, widthPercent: 33 },
    { type: 'recording', id: 2, startPercent: 67, widthPercent: 33 },
  ])
})

test('recording playback seeks after metadata and swaps only when the preloaded MP4 can play', async () => {
  assert.equal(typeof recordingBehavior.createRecordingPlaybackCoordinator, 'function')

  const makeVideo = () => ({
    src: '',
    currentTime: 0,
    pauseCalls: 0,
    playCalls: 0,
    loadCalls: 0,
    pause() { this.pauseCalls += 1 },
    play() { this.playCalls += 1; return Promise.resolve() },
    load() { this.loadCalls += 1 },
    removeAttribute(name) { if (name === 'src') this.src = '' },
  })
  const first = makeVideo()
  const second = makeVideo()
  const segments = [
    { id: 1, start_time: '2026-08-08T10:00:00Z', end_time: '2026-08-08T10:01:00Z' },
    { id: 2, start_time: '2026-08-08T10:01:00Z', end_time: '2026-08-08T10:02:00Z' },
    { id: 3, start_time: '2026-08-08T10:02:00Z', end_time: '2026-08-08T10:03:00Z' },
  ]
  const resolveCalls = []
  const coordinator = recordingBehavior.createRecordingPlaybackCoordinator({
    mediaUrl: (id) => `/media/${id}.mp4`,
    loadTimeline: async () => ({ segments: [segments[1]] }),
    resolvePlayback: async (params) => {
      resolveCalls.push(params)
      if (params.at === '2026-08-08T10:00:02.500Z') {
        return { segment: segments[0], offset_ms: 2500, next_segment_id: 2 }
      }
      return { segment: segments[1], offset_ms: 0, next_segment_id: 3 }
    },
  })
  coordinator.attach(0, first)
  coordinator.attach(1, second)

  await coordinator.open({ cameraId: 7, at: '2026-08-08T10:00:02.500Z' })
  assert.equal(first.src, '/media/1.mp4')
  assert.equal(second.src, '/media/2.mp4')
  coordinator.loadedMetadata(0)
  assert.equal(first.currentTime, 2.5)

  await coordinator.ended(0)
  assert.equal(coordinator.state.activeSlot, 0)
  assert.equal(coordinator.state.loadingNext, true)
  assert.equal(first.playCalls, 0)

  await coordinator.canPlay(1)
  assert.equal(coordinator.state.activeSlot, 1)
  assert.equal(second.playCalls, 1)
  assert.equal(first.src, '/media/3.mp4')
  assert.deepEqual(resolveCalls, [
    { camera_id: 7, at: '2026-08-08T10:00:02.500Z' },
    { camera_id: 7, at: '2026-08-08T10:01:00.000Z' },
  ])

  coordinator.close()
  assert.equal(first.src, '')
  assert.equal(second.src, '')
  assert.equal(first.pauseCalls, 1)
  assert.equal(second.pauseCalls, 1)
})

test('recording playback opens one segment at an arbitrary time without a playback list', async () => {
  const segments = [0, 1, 2, 3, 4].map((index) => ({
    id: index + 1,
    start_time: `2026-08-01T0${index}:00:00Z`,
    end_time: `2026-08-01T0${index}:01:00Z`,
    status: 'completed',
  }))
  let timelineCalls = 0
  const coordinator = recordingBehavior.createRecordingPlaybackCoordinator({
    mediaUrl: (id) => `/media/${id}.mp4`,
    loadTimeline: async () => { timelineCalls += 1; return { segments: [] } },
    resolvePlayback: async (params) => {
      assert.deepEqual(params, { camera_id: 7, at: '2026-08-09T07:50:00.000Z' })
      return { segment: segments[2], offset_ms: 0, next_segment_id: segments[3].id }
    },
  })

  await coordinator.open({ cameraId: 7, at: '2026-08-09T07:50:00.000Z' })

  assert.equal(coordinator.state.timeline, undefined)
  assert.equal(timelineCalls, 0)
})

test('recording playback discovers metadata beyond the searched boundary and preloads the third segment', async () => {
  const makeVideo = () => ({
    src: '',
    play() { return Promise.resolve() },
    load() {},
    pause() {},
    removeAttribute(name) { if (name === 'src') this.src = '' },
  })
  const firstVideo = makeVideo()
  const secondVideo = makeVideo()
  const first = { id: 1, start_time: '2026-08-08T10:59:00Z', end_time: '2026-08-08T11:00:00Z', status: 'completed' }
  const second = { id: 2, start_time: '2026-08-08T11:00:00Z', end_time: '2026-08-08T11:01:00Z', status: 'completed' }
  const third = { id: 3, start_time: '2026-08-08T11:01:00Z', end_time: '2026-08-08T11:02:00Z', status: 'completed' }
  const coordinator = recordingBehavior.createRecordingPlaybackCoordinator({
    mediaUrl: (id) => `/media/${id}.mp4`,
    loadTimeline: async () => ({ segments: [second] }),
    resolvePlayback: async ({ at }) => {
      if (at === '2026-08-08T10:59:30.000Z') {
        return { segment: first, offset_ms: 30000, next_segment_id: second.id }
      }
      if (at === '2026-08-08T11:00:00.000Z') {
        return { segment: second, offset_ms: 0, next_segment_id: third.id }
      }
      throw new Error(`unexpected playback time ${at}`)
    },
  })
  coordinator.attach(0, firstVideo)
  coordinator.attach(1, secondVideo)

  await coordinator.open({
    cameraId: 7,
    at: '2026-08-08T10:59:30.000Z',
    segments: [first],
  })
  await coordinator.canPlay(1)
  await coordinator.ended(0)

  assert.equal(coordinator.state.activeSlot, 1)
  assert.equal(firstVideo.src, '/media/3.mp4')
  assert.equal(coordinator.state.point.segment.id, second.id)
})

test('recording playback ignores a delayed swap after the dialog closes and reopens', async () => {
  let finishStalePlay
  const stalePlay = new Promise((resolve) => { finishStalePlay = resolve })
  const firstVideo = {
    src: '',
    play() { return Promise.resolve() },
    load() {},
    pause() {},
    removeAttribute(name) { if (name === 'src') this.src = '' },
  }
  const secondVideo = {
    ...firstVideo,
    play() { return stalePlay },
  }
  const oldFirst = { id: 1, start_time: '2026-08-08T10:00:00Z', end_time: '2026-08-08T10:01:00Z' }
  const oldSecond = { id: 2, start_time: '2026-08-08T10:01:00Z', end_time: '2026-08-08T10:02:00Z' }
  const reopened = { id: 10, start_time: '2026-08-08T12:00:00Z', end_time: '2026-08-08T12:01:00Z' }
  const resolveCalls = []
  const coordinator = recordingBehavior.createRecordingPlaybackCoordinator({
    mediaUrl: (id) => `/media/${id}.mp4`,
    resolvePlayback: async (params) => {
      resolveCalls.push(params)
      if (params.camera_id === 7 && params.at === '2026-08-08T10:00:30.000Z') {
        return { segment: oldFirst, offset_ms: 30000, next_segment_id: oldSecond.id }
      }
      if (params.camera_id === 8 && params.at === '2026-08-08T12:00:10.000Z') {
        return { segment: reopened, offset_ms: 10000, next_segment_id: null }
      }
      throw new Error('stale playback lookup')
    },
  })
  coordinator.attach(0, firstVideo)
  coordinator.attach(1, secondVideo)

  await coordinator.open({ cameraId: 7, at: '2026-08-08T10:00:30.000Z', segments: [oldFirst, oldSecond] })
  await coordinator.canPlay(1)
  const oldSwap = coordinator.ended(0)
  coordinator.close()
  await coordinator.open({ cameraId: 8, at: '2026-08-08T12:00:10.000Z', segments: [reopened] })
  finishStalePlay()
  await oldSwap

  assert.deepEqual(resolveCalls, [
    { camera_id: 7, at: '2026-08-08T10:00:30.000Z' },
    { camera_id: 8, at: '2026-08-08T12:00:10.000Z' },
  ])
  assert.equal(coordinator.state.point.segment.id, reopened.id)
  assert.equal(coordinator.state.error, '')
})

test('recording playback exposes a gap state for a missing selected time', async () => {
  const coordinator = recordingBehavior.createRecordingPlaybackCoordinator({
    mediaUrl: (id) => `/media/${id}.mp4`,
    resolvePlayback: async () => {
      const error = new Error('not found')
      error.response = { status: 404 }
      throw error
    },
  })

  await coordinator.open({ cameraId: 7, at: '2026-08-08T10:30:00.000Z', segments: [] })
  assert.equal(coordinator.state.gap, true)
  assert.equal(coordinator.state.error, '')
})

test('recording center renders Element Plus history filters without manual playback controls', async () => {
  const html = await renderRecordings()

  assert.match(html, /el-date-picker/)
  assert.match(html, /North Gate/)
  assert.match(html, /至/)
  assert.match(html, /清除日期/)
  assert.match(html, /查询历史/)
  assert.match(html, /10:00/)
  assert.match(html, /1000 B/)
  assert.doesNotMatch(html, /播放摄像头/)
  assert.doesNotMatch(html, /播放时间/)
  assert.doesNotMatch(html, /播放所选时间/)
  assert.doesNotMatch(html, /播放片段/)
  assert.equal((html.match(/<video/g) || []).length, 0)
})

test('recording preview renders native sequential video slots when opened from the list', async () => {
  const html = await renderRecordings({
    previewRec: { id: 18, camera_id: 7, start_time: '2026-08-08T10:10:00Z', storage_mode: 'segmented' },
    previewOpen: true,
    previewMediaUrl: '/segment-media',
  })
  const videos = html.match(/<video[^>]*>/g) || []

  assert.equal(videos.length, 2)
  assert.equal(videos.filter((video) => video.includes(' autoplay')).length, 1)
})

test('loading a segmented list preview keeps both coordinator video slots mounted', async () => {
  const html = await renderRecordings({
    previewRec: { id: 18, camera_id: 7, start_time: '2026-08-08T10:10:00Z', storage_mode: 'segmented' },
    previewOpen: true,
    previewLoading: true,
  })

  assert.match(html, /正在加载录像/)
  assert.equal((html.match(/<video/g) || []).length, 2)
})

test('segmented recording preview sends its exact ISO start timestamp to play-at', async () => {
  globalThis.localStorage = { getItem: () => null, removeItem: () => {} }
  const api = recordingBehavior.default
  const originalAdapter = api.defaults.adapter
  let request
  api.defaults.adapter = async (config) => {
    request = config
    return {
      data: { code: 0, data: { segment: { id: 18 }, offset_ms: 0, next_segment_id: null, segments: [] } },
      status: 200,
      statusText: 'OK',
      headers: {},
      config,
    }
  }

  try {
    const setup = await loadRecordingsSetup()
    setup.openRecordingPreview({
      id: 18,
      camera_id: 7,
      start_time: '2026-08-09T07:50:37.123Z',
      storage_mode: 'segmented',
    })
    await new Promise((resolve) => setImmediate(resolve))

    assert.deepEqual(request.params, {
      camera_id: 7,
      at: '2026-08-09T07:50:37.123Z',
    })
  } finally {
    api.defaults.adapter = originalAdapter
  }
})

test('reversed history dates render a filter error without replacing prior recordings', async () => {
  const api = recordingBehavior.default
  const originalAdapter = api.defaults.adapter
  const priorRecording = {
    id: 44,
    camera_id: 7,
    start_time: '2026-08-08T10:10:00Z',
    duration: 60,
    status: 'completed',
    storage_mode: 'segmented',
  }
  let requestCount = 0
  api.defaults.adapter = async (config) => {
    requestCount += 1
    return {
      data: { code: 0, data: { recordings: [priorRecording], total: 1 } },
      status: 200,
      statusText: 'OK',
      headers: {},
      config,
    }
  }

  try {
    const setup = await loadRecordingsSetup()
    await setup.loadRecordings()
    setup.timeSearch.startDate = '2026-08-09'
    setup.timeSearch.endDate = '2026-08-02'
    await setup.applyHistoryFilters()

    assert.equal(requestCount, 1)
    assert.deepEqual(setup.recordings.value, [priorRecording])
    assert.equal(setup.total.value, 1)
    assert.equal(setup.historyError.value, '结束日期必须不早于开始日期')
    const html = await renderRecordings({
      recordings: setup.recordings.value,
      total: setup.total.value,
      historyError: setup.historyError.value,
    })
    assert.match(html, /结束日期必须不早于开始日期/)
  } finally {
    api.defaults.adapter = originalAdapter
  }
})

test('recording history query and clear reset paging with the expected date filters', async () => {
  const api = recordingBehavior.default
  const originalAdapter = api.defaults.adapter
  const requests = []
  api.defaults.adapter = async (config) => {
    requests.push(config)
    return {
      data: { code: 0, data: { recordings: [], total: 0 } },
      status: 200,
      statusText: 'OK',
      headers: {},
      config,
    }
  }

  try {
    const setup = await loadRecordingsSetup()
    setup.page.value = 3
    setup.timeSearch.startDate = '2026-08-02'
    setup.timeSearch.endDate = '2026-08-09'
    await setup.applyHistoryFilters()
    setup.page.value = 3
    await setup.clearDateRange()

    assert.deepEqual(requests.map((request) => request.params), [
      {
        page: 1,
        page_size: 20,
        start_time: '2026-08-02T00:00:00.000Z',
        end_time: '2026-08-10T00:00:00.000Z',
      },
      { page: 1, page_size: 20 },
    ])
  } finally {
    api.defaults.adapter = originalAdapter
  }
})

test('recording center preserves preview and download for legacy single-file recordings', async () => {
  const legacy = {
    id: 19,
    camera_id: 7,
    start_time: '2026-08-08T09:00:00Z',
    duration: 600,
    file_size: 1000,
    trigger_type: 'schedule',
    status: 'completed',
    storage_mode: 'legacy',
    format: 'mp4',
  }
  const html = await renderRecordings({
    recordings: [legacy],
    previewRec: legacy,
    previewOpen: true,
    previewMediaUrl: '/legacy-download',
  })

  assert.match(html, /录像预览 #19/)
  assert.match(html, /<video[^>]*src="\/legacy-download"/)
})
