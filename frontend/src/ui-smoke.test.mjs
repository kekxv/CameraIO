import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { compile } from '@vue/compiler-dom'
import { parse } from '@vue/compiler-sfc'
import * as Vue from 'vue'
import { renderToString } from '@vue/server-renderer'

const css = readFileSync(new URL('./assets/main.css', import.meta.url), 'utf8')
const recordingBehavior = await import('./api.js')

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
      from: '2026-08-08T10:00',
      to: '2026-08-08T11:00',
      at: '2026-08-08T10:15',
    },
    searchingTimeline: false,
    timelineError: '',
    coverageParts: [
      { type: 'gap', startPercent: 0, widthPercent: 16.666, start_time: '2026-08-08T10:00:00Z', end_time: segment.start_time, segment: null },
      { type: 'recording', startPercent: 16.666, widthPercent: 16.666, start_time: segment.start_time, end_time: segment.end_time, segment },
    ],
    selectedTimePercent: 25,
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
    searchTimeline() {},
    playSelectedTime() {},
    openTimePlayback() {},
    openRecordingPreview() {},
    closeTimePlayback() {},
    closeLegacyPreview() {},
    selectCoveragePart() {},
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

test('camera management keeps operational actions and uses compatible layout primitives', () => {
  const cameras = readFileSync(new URL('./views/Cameras.vue', import.meta.url), 'utf8')
  assert.match(cameras, /handleScanLAN/)
  assert.match(cameras, /ui-card/)
  assert.match(cameras, /compat-flex-gap-/)
  assert.match(cameras, /ui-modal/)
})

test('live view keeps media controls and applies the Chrome 72 aspect fallback', () => {
  const live = readFileSync(new URL('./views/Live.vue', import.meta.url), 'utf8')
  assert.match(live, /startStream/)
  assert.match(live, /stopStream/)
  assert.match(live, /compat-aspect-video/)
  assert.match(live, /captureSnapshot/)
  assert.match(live, /ui-modal/)
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

test('recording time search converts local wall-clock values to UTC and rejects ranges over 24 hours', () => {
  assert.equal(typeof recordingBehavior.normalizeRecordingSearch, 'function')

  assert.deepEqual(recordingBehavior.normalizeRecordingSearch({
    cameraId: 7,
    from: '2026-08-08T09:00',
    to: '2026-08-08T10:00',
    at: '2026-08-08T09:15',
  }), {
    camera_id: 7,
    from: '2026-08-08T09:00:00.000Z',
    to: '2026-08-08T10:00:00.000Z',
    at: '2026-08-08T09:15:00.000Z',
  })

  assert.throws(() => recordingBehavior.normalizeRecordingSearch({
    cameraId: 7,
    from: '2026-08-07T09:59',
    to: '2026-08-08T10:00',
    at: '2026-08-08T09:15',
  }), /24/)
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

  await coordinator.open({ cameraId: 7, at: '2026-08-08T10:00:02.500Z', segments })
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

test('recording center renders wall-clock coverage and seekable dual-video playback behavior', async () => {
  const html = await renderRecordings()

  assert.equal((html.match(/type="datetime-local"/g) || []).length, 3)
  assert.match(html, /North Gate/)
  assert.match(html, /录像空档/)
  assert.match(html, /可播放录像/)
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
