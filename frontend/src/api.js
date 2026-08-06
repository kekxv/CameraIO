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

export const deleteRecording = (id) =>
  api.delete(`/recordings/${id}`)

// ---------- 定时录像计划 ----------

export const listSchedules = () => api.get('/schedules').then((r) => r.data.data)
export const createSchedule = (data) => api.post('/schedules', data).then((r) => r.data.data)
export const updateSchedule = (id, data) => api.put(`/schedules/${id}`, data).then((r) => r.data.data)
export const deleteSchedule = (id) => api.delete(`/schedules/${id}`)

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
