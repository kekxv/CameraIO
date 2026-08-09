import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { compile } from '@vue/compiler-dom'
import { parse, compileScript } from '@vue/compiler-sfc'
import * as Vue from 'vue'
import { renderToString } from '@vue/server-renderer'

const css = readFileSync(new URL('./assets/main.css', import.meta.url), 'utf8')
const recordingBehavior = await import('./api.js')

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
    showScheduleDialog: false,
    savingSchedule: false,
    scheduleForm: {},
    weekdays: [],
    timeSearch: {
      cameraId: 7,
      startDate: '2026-08-08',
      endDate: '2026-08-08',
      at: '2026-08-08T10:15',
    },
    historyError: '',
    timelineError: '',
    timePlaybackOpen: true,
    playbackState: {
      activeSlot: 0,
      point: { segment, offset_ms: 0, next_segment_id: null },
      gap: false,
      error: '',
      loading: false,
      loadingNext: false,
    },
    loadRecordings() {},
    applyHistoryFilters() {},
    clearDateRange() {},
    playSelectedTime() {},
    openTimePlayback() {},
    openRecordingPreview() {},
    closeTimePlayback() {},
    closeLegacyPreview() {},
    attachPlaybackVideo() {},
    handlePlaybackMetadata() {},
    handlePlaybackCanPlay() {},
    handlePlaybackEnded() {},
    previewRecording() {},
    handleStopRecording() {},
    handleDeleteRecording() {},
    goPage() {},
    openScheduleDialog() {},
    toggleSchedule() {},
    handleDeleteSchedule() {},
    toggleDay() {},
    saveSchedule() {},
    cameraName: () => 'North Gate',
    formatTime: (value) => value,
    formatDuration: () => '10:00',
    formatSize: () => '1000 B',
    triggerClass: () => '',
    triggerLabel: () => '定时',
    statusLabel: () => '已完成',
    downloadUrl: () => '/legacy-download',
    isSegmentedRecording: (recording) => recording.storage_mode === 'segmented',
    coverageTitle: (part) => part.type === 'gap' ? '录像空档' : '可播放录像',
    ...overrides,
  }
  const app = Vue.createSSRApp({ setup: () => context, render })
  app.component('AppIcon', { render: () => null })
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
  assert.match(live, /toggleCameraSelection/)
  assert.match(live, /clearCameraSelection/)
})

test('recordings view preserves recording and schedule actions in a responsive surface', () => {
  const recordings = readFileSync(new URL('./views/Recordings.vue', import.meta.url), 'utf8')
  assert.match(recordings, /stopRecording/)
  assert.match(recordings, /saveSchedule/)
  assert.match(recordings, /ui-card/)
  assert.match(recordings, /overflow-x-auto/)
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

test('recording center renders aligned history filters without redundant manual playback controls', async () => {
  const html = await renderRecordings()

  assert.equal((html.match(/type="date"/g) || []).length, 2)
  assert.equal((html.match(/type="datetime-local"/g) || []).length, 0)
  assert.match(html, /North Gate/)
  assert.match(html, /至/)
  assert.match(html, /清除日期/)
  assert.match(html, /查询历史/)
  assert.doesNotMatch(html, /播放摄像头/)
  assert.doesNotMatch(html, /播放时间/)
  assert.doesNotMatch(html, /播放所选时间/)
  assert.doesNotMatch(html, /播放片段/)
  assert.equal((html.match(/<video/g) || []).length, 2)
  assert.match(html, /preload="auto"/)
  assert.match(html, /导出尚未实现/)
  assert.doesNotMatch(html, /href="\/legacy-download"/)
})

test('recording center preloads the hidden player without autoplaying it', async () => {
  const html = await renderRecordings()
  const videos = html.match(/<video[^>]*>/g) || []

  assert.equal(videos.length, 2)
  assert.equal(videos.filter((video) => video.includes(' autoplay')).length, 1)
  assert.equal(videos.filter((video) => video.includes('style="display:none;"')).length, 1)
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
    timePlaybackOpen: false,
  })

  assert.match(html, /录像预览 #19/)
  assert.match(html, /<video[^>]*src="\/legacy-download"/)
  assert.equal((html.match(/href="\/legacy-download"/g) || []).length, 2)
  assert.doesNotMatch(html, /导出尚未实现/)
})
