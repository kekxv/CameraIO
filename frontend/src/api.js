import axios from 'axios'

const api = axios.create({
  baseURL: '/api/v1',
  timeout: 30000,
})

// 请求拦截器：注入 JWT
api.interceptors.request.use((config) => {
  const token = localStorage.getItem('token')
  if (token) {
    config.headers.Authorization = `Bearer ${token}`
  }
  return config
})

// 响应拦截器：处理 401
api.interceptors.response.use(
  (resp) => resp,
  (err) => {
    if (err.response?.status === 401) {
      localStorage.removeItem('token')
      localStorage.removeItem('user')
      // 跳转到登录页（如果当前不在登录页）
      if (!window.location.pathname.includes('/login')) {
        window.location.href = '/login'
      }
    }
    return Promise.reject(err)
  }
)

// ---------- Auth ----------

export const login = (username, password) =>
  api.post('/login', { username, password }).then((r) => {
    const { token, user } = r.data.data
    localStorage.setItem('token', token)
    localStorage.setItem('user', JSON.stringify(user))
    return { token, user }
  })

export const logout = () => {
  localStorage.removeItem('token')
  localStorage.removeItem('user')
  window.location.href = '/login'
}

export const getCurrentUser = () => {
  const raw = localStorage.getItem('user')
  return raw ? JSON.parse(raw) : null
}

export const isLoggedIn = () => !!localStorage.getItem('token')

// ---------- Cameras ----------

export const listCameras = () => api.get('/cameras').then((r) => r.data.data)
export const getCamera = (id) => api.get(`/cameras/${id}`).then((r) => r.data.data)
export const createCamera = (data) => api.post('/cameras', data).then((r) => r.data.data)
export const updateCamera = (id, data) => api.put(`/cameras/${id}`, data).then((r) => r.data.data)
export const deleteCamera = (id) => api.delete(`/cameras/${id}`)
export const syncCameraTime = (id) => api.post(`/cameras/${id}/sync-time`).then((r) => r.data.data)
export const testCameraConnection = (id) => api.post(`/cameras/${id}/test`).then((r) => r.data.data)
export const testCameraConnectionByIP = (data) => api.post('/cameras/test-by-ip', data).then((r) => r.data.data)
export const discoverNVRChannels = (data) => api.post('/cameras/discover-channels', data).then((r) => r.data.data)
export const scanNetwork = (data) => api.post('/cameras/scan-network', data || { subnet: 'auto' }).then((r) => r.data.data)
export const setCameraCodec = (id, codec) => api.post(`/cameras/${id}/set-codec`, { codec }).then((r) => r.data.data)
export const setCameraNetwork = (id, config) => api.post(`/cameras/${id}/set-network`, config).then((r) => r.data.data)
export const captureSnapshot = (id) => api.get(`/cameras/${id}/snapshot`, { responseType: 'blob' }).then((r) => r.data)

// responseType 为 blob 时，Axios 也会把非 2xx 的 JSON 错误体包装成 Blob。
export const getAPIErrorMessage = async (err) => {
  const data = err.response?.data
  if (data instanceof Blob) {
    const text = await data.text()
    try {
      return JSON.parse(text).message || text || err.message
    } catch {
      return text || err.message
    }
  }
  return data?.message || err.message
}

// ---------- Local Cameras ----------

export const listLocalCameras = () => api.get('/local-cameras').then((r) => r.data.data)

// ---------- Streams ----------

export const startStream = (id) => api.post(`/streams/${id}/start`)
export const stopStream = (id) => api.post(`/streams/${id}/stop`)

// ---------- Recordings ----------

export const startRecording = (cameraId, options = {}) =>
  api.post('/recordings/start', { camera_id: cameraId, ...options }).then((r) => r.data.data)

export const stopRecording = (recordingId) =>
  api.post('/recordings/stop', { recording_id: recordingId })

export const listRecordings = (params) =>
  api.get('/recordings', { params }).then((r) => r.data.data)

export const getRecording = (id) =>
  api.get(`/recordings/${id}`).then((r) => r.data.data)

export const getRecordingDownloadUrl = (id) =>
  `/api/v1/recordings/${id}/download`

export const getRecordingTimeline = (params) =>
  api.get('/recordings/timeline', { params }).then((r) => r.data.data)

export const resolveRecordingPlayback = (params) =>
  api.get('/recordings/play-at', { params }).then((r) => r.data.data)

export const getSegmentMediaUrl = (id) => {
  const token = localStorage.getItem('token')
  const suffix = token ? `?token=${encodeURIComponent(token)}` : ''
  return `/api/v1/recording-segments/${id}/media${suffix}`
}

const recordingRangeLimitMS = 24 * 60 * 60 * 1000

const parseRecordingTime = (value, label) => {
  const parsed = new Date(value)
  if (!value || Number.isNaN(parsed.getTime())) {
    throw new Error(`${label} 不是有效时间`)
  }
  return parsed
}

export const normalizeRecordingSearch = ({ cameraId, from, to, at }) => {
  const fromTime = parseRecordingTime(from, '开始时间')
  const toTime = parseRecordingTime(to, '结束时间')
  const atTime = parseRecordingTime(at, '播放时间')
  const duration = toTime.getTime() - fromTime.getTime()
  if (duration <= 0) throw new Error('结束时间必须晚于开始时间')
  if (duration > recordingRangeLimitMS) throw new Error('查询范围不能超过 24 小时')
  return {
    camera_id: cameraId,
    from: fromTime.toISOString(),
    to: toTime.toISOString(),
    at: atTime.toISOString(),
  }
}

export const buildRecordingCoverage = (segments, from, to) => {
  const fromMS = new Date(from).getTime()
  const toMS = new Date(to).getTime()
  const duration = toMS - fromMS
  if (!(duration > 0)) return []

  const completed = (segments || [])
    .filter((segment) => segment.status === 'completed')
    .slice()
    .sort((left, right) => new Date(left.start_time).getTime() - new Date(right.start_time).getTime())
  const parts = []
  let cursor = fromMS
  const append = (type, start, end, segment) => {
    if (end <= start) return
    parts.push({
      type,
      segment: segment || null,
      start_time: new Date(start).toISOString(),
      end_time: new Date(end).toISOString(),
      startPercent: ((start - fromMS) / duration) * 100,
      widthPercent: ((end - start) / duration) * 100,
    })
  }

  completed.forEach((segment) => {
    const start = Math.max(fromMS, new Date(segment.start_time).getTime())
    const end = Math.min(toMS, new Date(segment.end_time).getTime())
    if (end <= fromMS || start >= toMS || end <= start) return
    if (start > cursor) append('gap', cursor, start)
    append('recording', Math.max(cursor, start), end, segment)
    cursor = Math.max(cursor, end)
  })
  if (cursor < toMS) append('gap', cursor, toMS)
  return parts
}

export const createRecordingPlaybackCoordinator = ({ resolvePlayback, mediaUrl, onStateChange }) => {
  const videos = [null, null]
  const ready = [false, false]
  let segmentByID = new Map()
  let cameraID = null
  let generation = 0
  let pendingSwap = false
  let state = {
    open: false,
    activeSlot: 0,
    point: null,
    gap: false,
    error: '',
    loading: false,
    loadingNext: false,
  }

  const notify = () => {
    if (onStateChange) onStateChange({ ...state })
  }
  const setState = (patch) => {
    state = { ...state, ...patch }
    notify()
  }
  const setSource = (slot, segmentID) => {
    const video = videos[slot]
    ready[slot] = false
    if (!video) return
    video.src = mediaUrl(segmentID)
    if (video.load) video.load()
  }
  const clearVideo = (video, pause) => {
    if (!video) return
    if (pause && video.pause) video.pause()
    if (video.removeAttribute) video.removeAttribute('src')
    else video.src = ''
    if (video.load) video.load()
  }
  const clearAllVideos = (pause) => {
    clearVideo(videos[0], pause)
    clearVideo(videos[1], pause)
    ready[0] = false
    ready[1] = false
  }
  const nextSlot = () => (state.activeSlot === 0 ? 1 : 0)

  const primeNext = (segmentID) => {
    if (!segmentID) return
    setSource(nextSlot(), segmentID)
  }

  const swap = async () => {
    if (!state.open) return
    const oldSlot = state.activeSlot
    const newSlot = nextSlot()
    const nextID = state.point && state.point.next_segment_id
    if (!nextID || !ready[newSlot]) return

    pendingSwap = false
    const segment = segmentByID.get(nextID) || { id: nextID }
    const nextPoint = { segment, offset_ms: 0, next_segment_id: null }
    setState({ activeSlot: newSlot, point: nextPoint, loadingNext: false })
    if (videos[newSlot] && videos[newSlot].play) {
      try {
        await videos[newSlot].play()
      } catch {}
    }

    if (!segment.start_time) return
    const currentGeneration = generation
    try {
      const resolved = await resolvePlayback({
        camera_id: cameraID,
        at: new Date(segment.start_time).toISOString(),
      })
      if (currentGeneration !== generation || !state.open || state.activeSlot !== newSlot) return
      setState({ point: resolved })
      if (resolved.next_segment_id) setSource(oldSlot, resolved.next_segment_id)
      else clearVideo(videos[oldSlot], false)
    } catch (err) {
      if (currentGeneration !== generation) return
      setState({ error: err.message || '无法加载下一录像片段' })
    }
  }

  return {
    get state() {
      return state
    },
    attach(slot, video) {
      videos[slot] = video
    },
    async open({ cameraId, at, segments }) {
      generation += 1
      const currentGeneration = generation
      if (state.open) clearAllVideos(true)
      cameraID = cameraId
      segmentByID = new Map((segments || []).map((segment) => [segment.id, segment]))
      pendingSwap = false
      state = {
        open: true,
        activeSlot: 0,
        point: null,
        gap: false,
        error: '',
        loading: true,
        loadingNext: false,
      }
      notify()
      try {
        const point = await resolvePlayback({ camera_id: cameraId, at })
        if (currentGeneration !== generation) return
        setSource(0, point.segment.id)
        setState({ point, loading: false })
        primeNext(point.next_segment_id)
      } catch (err) {
        if (currentGeneration !== generation) return
        if (err.response && err.response.status === 404) {
          setState({ gap: true, loading: false })
          return
        }
        setState({ error: err.message || '录像加载失败', loading: false })
      }
    },
    loadedMetadata(slot) {
      if (slot !== state.activeSlot || !state.point || !videos[slot]) return
      videos[slot].currentTime = Math.max(0, Number(state.point.offset_ms || 0) / 1000)
    },
    async canPlay(slot) {
      ready[slot] = true
      if (pendingSwap && slot === nextSlot()) await swap()
    },
    async ended(slot) {
      if (slot !== state.activeSlot || !state.point || !state.point.next_segment_id) return
      if (!ready[nextSlot()]) {
        pendingSwap = true
        setState({ loadingNext: true })
        return
      }
      await swap()
    },
    close() {
      generation += 1
      pendingSwap = false
      clearAllVideos(true)
      state = {
        open: false,
        activeSlot: 0,
        point: null,
        gap: false,
        error: '',
        loading: false,
        loadingNext: false,
      }
      notify()
    },
  }
}

export const deleteRecording = (id) =>
  api.delete(`/recordings/${id}`)

// ---------- 定时录像计划 ----------

export const listSchedules = () => api.get('/schedules').then((r) => r.data.data)
export const createSchedule = (data) => api.post('/schedules', data).then((r) => r.data.data)
export const updateSchedule = (id, data) => api.put(`/schedules/${id}`, data).then((r) => r.data.data)
export const deleteSchedule = (id) => api.delete(`/schedules/${id}`)

// ---------- System ----------

export const getFFmpegStatus = () => api.get('/system/ffmpeg').then((r) => r.data.data)

// ---------- WebSocket ----------

export const connectEventBus = (onMessage) => {
  const token = localStorage.getItem('token')
  const ws = new WebSocket(`ws://${location.host}/ws/v1/system?token=${token}`)
  ws.onmessage = (e) => {
    try {
      onMessage(JSON.parse(e.data))
    } catch {}
  }
  ws.onclose = () => {
    // 5 秒后重连
    setTimeout(() => connectEventBus(onMessage), 5000)
  }
  return ws
}

export default api
