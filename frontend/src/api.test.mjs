import assert from 'node:assert/strict'
import test from 'node:test'
import { execFileSync } from 'node:child_process'

globalThis.localStorage = {
  getItem: () => null,
  removeItem: () => {},
}

const apiModule = await import('./api.js')
const { default: api, captureSnapshot, getAPIErrorMessage } = apiModule

test('recording time helpers call the timeline and play-at APIs with UTC parameters', async () => {
  assert.equal(typeof apiModule.getRecordingTimeline, 'function')
  assert.equal(typeof apiModule.resolveRecordingPlayback, 'function')
  assert.equal(typeof apiModule.getSegmentMediaUrl, 'function')

  const originalAdapter = api.defaults.adapter
  const requests = []
  api.defaults.adapter = async (config) => {
    requests.push(config)
    return {
      data: { code: 0, data: config.url.endsWith('/timeline') ? { segments: [{ id: 41 }] } : { segment: { id: 41 }, offset_ms: 2500 } },
      status: 200,
      statusText: 'OK',
      headers: {},
      config,
    }
  }

  try {
    const timeline = await apiModule.getRecordingTimeline({
      camera_id: 7,
      from: '2026-08-08T09:00:00.000Z',
      to: '2026-08-08T10:00:00.000Z',
    })
    const playback = await apiModule.resolveRecordingPlayback({
      camera_id: 7,
      at: '2026-08-08T09:15:30.000Z',
    })

    assert.deepEqual(timeline, { segments: [{ id: 41 }] })
    assert.deepEqual(playback, { segment: { id: 41 }, offset_ms: 2500 })
    assert.equal(requests[0].url, '/recordings/timeline')
    assert.deepEqual(requests[0].params, {
      camera_id: 7,
      from: '2026-08-08T09:00:00.000Z',
      to: '2026-08-08T10:00:00.000Z',
    })
    assert.equal(requests[1].url, '/recordings/play-at')
    assert.deepEqual(requests[1].params, {
      camera_id: 7,
      at: '2026-08-08T09:15:30.000Z',
    })
    assert.equal(apiModule.getSegmentMediaUrl(41), '/api/v1/recording-segments/41/media')
  } finally {
    api.defaults.adapter = originalAdapter
  }
})

test('resource-safe recording options normalize persisted unsafe choices', () => {
  assert.deepEqual(apiModule.normalizeResourceSafeRecordingOptions({ format: 'webm', bitrate: 1000, with_audio: true }), {
    format: 'mp4',
    bitrate: 0,
    with_audio: true,
  })
  assert.deepEqual(apiModule.normalizeResourceSafeRecordingOptions({ format: 'ts', bitrate: 0 }), {
    format: 'ts',
    bitrate: 0,
  })
})

test('recording date range filters use local day boundaries without limiting history playback', () => {
  assert.deepEqual(apiModule.normalizeRecordingDateRange({}), {})
  assert.deepEqual(apiModule.normalizeRecordingDateRange({
    startDate: '2026-08-02',
    endDate: '2026-08-09',
  }), {
    start_time: '2026-08-02T00:00:00.000Z',
    end_time: '2026-08-10T00:00:00.000Z',
  })
  assert.deepEqual(apiModule.normalizeRecordingPlayback({
    cameraId: 7,
    at: '2026-08-09T07:50:37.123Z',
  }), {
    camera_id: 7,
    at: '2026-08-09T07:50:37.123Z',
  })
  assert.throws(() => apiModule.normalizeRecordingDateRange({
    startDate: '2026-08-09',
    endDate: '2026-08-02',
  }), /结束日期必须不早于开始日期/)
})

test('recording date range uses the operator local midnight outside UTC', () => {
  const output = execFileSync(process.execPath, [
    '--input-type=module',
    '-e',
    "globalThis.localStorage={getItem:()=>null,removeItem:()=>{}}; const api=await import('./src/api.js'); process.stdout.write(JSON.stringify(api.normalizeRecordingDateRange({startDate:'2026-08-02',endDate:'2026-08-09'})))",
  ], {
    cwd: new URL('../', import.meta.url),
    env: { ...process.env, TZ: 'America/New_York' },
  }).toString()

  assert.deepEqual(JSON.parse(output), {
    start_time: '2026-08-02T04:00:00.000Z',
    end_time: '2026-08-10T04:00:00.000Z',
  })
})

test('recording history ignores an older out-of-order response', async () => {
  let firstResolve
  let secondResolve
  const first = new Promise((resolve) => { firstResolve = resolve })
  const second = new Promise((resolve) => { secondResolve = resolve })
  const states = []
  const coordinator = apiModule.createRecordingHistoryCoordinator({
    listRecordings: ({ page }) => page === 1 ? first : second,
    onStateChange: (state) => states.push(state),
  })

  const firstRequest = coordinator.load({ page: 1 })
  const secondRequest = coordinator.load({ page: 2 })
  secondResolve({ recordings: [{ id: 2 }], total: 1 })
  await secondRequest
  firstResolve({ recordings: [{ id: 1 }], total: 1 })
  await firstRequest

  assert.deepEqual(coordinator.state, {
    recordings: [{ id: 2 }],
    total: 1,
    loading: false,
    error: '',
  })
  assert.equal(states.at(-1).loading, false)
})

test('recording history surfaces a latest query error', async () => {
  const coordinator = apiModule.createRecordingHistoryCoordinator({
    listRecordings: async () => { throw new Error('历史筛选失败') },
  })

  await coordinator.load({ page: 1 })

  assert.equal(coordinator.state.error, '历史筛选失败')
})

test('captureSnapshot requests the native JPEG endpoint as a blob', async () => {
  const originalAdapter = api.defaults.adapter
  let request
  api.defaults.adapter = async (config) => {
    request = config
    return {
      data: new Blob(['jpeg'], { type: 'image/jpeg' }),
      status: 200,
      statusText: 'OK',
      headers: { 'content-type': 'image/jpeg' },
      config,
    }
  }

  try {
    const snapshot = await captureSnapshot(12)
    assert.equal(request.url, '/cameras/12/snapshot')
    assert.equal(request.method, 'get')
    assert.equal(request.responseType, 'blob')
    assert.equal(await snapshot.text(), 'jpeg')
  } finally {
    api.defaults.adapter = originalAdapter
  }
})

test('getAPIErrorMessage reads a JSON error Blob from a snapshot request', async () => {
  const message = await getAPIErrorMessage({
    message: 'Request failed with status code 502',
    response: {
      data: new Blob([JSON.stringify({ message: 'camera snapshot returned 401 Unauthorized' })], { type: 'application/json' }),
    },
  })

  assert.equal(message, 'camera snapshot returned 401 Unauthorized')
})
