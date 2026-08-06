# CameraIO API Reference

Base URL: `http://<host>:8080/api/v1`

## 认证

除 `/login` 和 `/health` 外，所有接口需在请求头携带 JWT：

```
Authorization: Bearer <token>
```

---

## 认证接口

### POST /login

登录获取 JWT token。

**请求**

```json
{
  "username": "admin",
  "password": "admin"
}
```

**响应** (200)

```json
{
  "code": 0,
  "message": "ok",
  "data": {
    "token": "eyJhbGciOiJIUzI1NiIs...",
    "user": {
      "id": 1,
      "username": "admin",
      "role": "admin",
      "created_at": "2026-01-01T00:00:00Z"
    }
  }
}
```

---

## 用户管理

### GET /users

获取所有用户列表。

**响应** (200)

```json
{
  "code": 0,
  "data": [
    {"id": 1, "username": "admin", "role": "admin", "created_at": "..."}
  ]
}
```

### POST /users

创建新用户。

**请求**

```json
{
  "username": "viewer1",
  "password": "secure123",
  "role": "viewer"
}
```

**字段说明**

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| username | string | ✅ | 用户名（唯一） |
| password | string | ✅ | 密码 |
| role | string | | 角色：`admin` / `viewer`，默认 `viewer` |

---

## 摄像头管理

### GET /cameras

获取摄像头列表。

**响应** (200)

```json
{
  "code": 0,
  "data": [
    {
      "id": 1,
      "name": "Front Gate",
      "ip": "192.168.1.100",
      "port": 554,
      "rtsp_url": "rtsp://admin:pass@192.168.1.100:554/Streaming/Channels/101",
      "brand": "hikvision",
      "auto_tune_enabled": true,
      "status": "online",
      "last_time_sync": "2026-08-02T10:00:00Z",
      "created_at": "2026-08-01T08:00:00Z"
    }
  ]
}
```

### POST /cameras

添加摄像头。如果 `auto_tune_enabled` 为 true（默认），系统会异步执行：
1. ONVIF 时间同步
2. 视频编码参数优化（关闭 B 帧，GOP=FPS，CBR 模式）

**请求**

```json
{
  "name": "Front Gate",
  "ip": "192.168.1.100",
  "port": 554,
  "brand": "hikvision",
  "username": "admin",
  "password": "hikadmin",
  "auto_tune_enabled": true,
  "access_protocol": "rtsp"
}
```

**字段说明**

| 字段 | 类型 | 必填 | 默认值 | 说明 |
|---|---|---|---|---|
| name | string | ✅ | - | 摄像头名称 |
| ip | string | ✅ | - | 设备 IP 地址 |
| port | int | | 554 | RTSP 端口 |
| rtsp_url | string | | 自动生成 | 自定义 RTSP URL |
| brand | string | | `custom` | 品牌：`hikvision` / `uniview` / `custom` |
| username | string | | - | ONVIF/RTSP 用户名 |
| password | string | | - | ONVIF/RTSP 密码 |
| auto_tune_enabled | bool | | true | 是否自动调优（仅 RTSP） |
| access_protocol | string | | `rtsp` | 接入协议：`rtsp` 或 `gb28181` |
| device_id | string | | - | 20 位国标编码（GB28181 必填） |
| channel_id | string | | - | 通道编码（GB28181） |
| transport | string | | `UDP` | GB28181 流传输协议：`UDP` / `TCP` |

### GB28181 摄像头接入示例

```json
{
  "name": "Gate Camera",
  "ip": "192.168.1.100",
  "access_protocol": "gb28181",
  "device_id": "34020000001320000001",
  "channel_id": "34020000001320000001",
  "transport": "UDP"
}
```

设备需要在 GB28181 配置界面中填入 CameraIO 的 SIP 服务器信息：
- **服务器 IP**: CameraIO 服务器 IP
- **服务器端口**: 5060（默认）
- **服务器编码**: 34020000002000000001（默认）
- **域**: 3402000000（默认）

设备配置完成后会自动向 CameraIO 发起 SIP REGISTER，CameraIO 会：
1. 回复 200 OK（含 `Date` 头，用于对时）
2. 每 60 秒接收心跳（`MESSAGE` with `CmdType=Keepalive`）
3. 响应目录查询（`MESSAGE` with `CmdType=Catalog`）
4. 通过 `INVITE` 向设备点播视频流

### 本地摄像头接入示例

```json
{
  "name": "USB Webcam",
  "ip": "local",
  "access_protocol": "local",
  "local_index": 0,
  "local_name": "USB Camera"
}
```

或使用 `localcam` 工具将本地摄像头发布为 RTSP 后，作为普通 RTSP 摄像头接入：
```bash
# 终端 1：启动 localcam
localcam serve --index 0 --port 8554

# 终端 2：添加到 CameraIO
curl -X POST http://localhost:8080/api/v1/cameras -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"name":"Local Cam","ip":"localhost","port":8554,"rtsp_url":"rtsp://localhost:8554/live"}'
```

### GET /cameras/:id

获取单个摄像头详情。

### PUT /cameras/:id

更新摄像头配置。仅传递需要修改的字段。

**请求**

```json
{
  "name": "New Name",
  "ip": "192.168.1.200"
}
```

### DELETE /cameras/:id

删除摄像头。

**响应**: 204 No Content

### POST /cameras/:id/sync-time

手动触发时间同步。

**响应** (200)

```json
{
  "code": 0,
  "data": {"message": "time sync completed"}
}
```

---

## 本地摄像头

### GET /local-cameras

扫描并返回本机所有可用的本地摄像头（USB/内置摄像头）。

**响应** (200)

```json
{
  "code": 0,
  "data": [
    {
      "index": 0,
      "name": "FaceTime HD Camera",
      "path": "0"
    },
    {
      "index": 1,
      "name": "USB Camera",
      "path": "/dev/video0"
    }
  ]
}
```

**字段说明**

| 字段 | 类型 | 说明 |
|---|---|---|
| index | int | 设备索引号（Linux: /dev/videoN, macOS: 索引） |
| name | string | 设备显示名称 |
| path | string | 设备路径（Linux: /dev/videoN, macOS: 索引字符串, Windows: video=设备名） |
| vid | string | USB Vendor ID（如果可识别） |
| pid | string | USB Product ID（如果可识别） |

**跨平台支持**

| 平台 | 后端 | 实现 |
|---|---|---|
| Linux | v4l2 | `/dev/video*` |
| macOS | AVFoundation | FaceTime / USB |
| Windows | DirectShow | USB 摄像头 |

---

## 实时流

### POST /streams/:id/start

启动指定摄像头的 RTSP 拉流。

**响应** (200)

```json
{
  "code": 0,
  "data": {"message": "stream started"}
}
```

### POST /streams/:id/stop

停止拉流。

**响应** (200)

```json
{
  "code": 0,
  "message": "stream stopped"
}
```

### GET /streams/:id/mjpeg

MJPEG 预览流（`multipart/x-mixed-replace`）。

```html
<img src="/api/v1/streams/1/mjpeg?token=<jwt>" />
```

> **说明**: 如果流未启动，接口会自动启动拉流。首帧可能需要等待 FFmpeg 连接摄像头（最多 10 秒）。

---

## 录像控制

### POST /recordings/start

开始录像。

**请求**

```json
{
  "camera_id": 1,
  "custom_name": "incident_20260802"
}
```

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| camera_id | uint | ✅ | 摄像头 ID |
| custom_name | string | | 自定义文件名标识 |

**响应** (201)

```json
{
  "code": 0,
  "data": {
    "id": 105,
    "camera_id": 1,
    "file_path": "data/recordings/1/1_20260802_143000_incident_20260802.mp4",
    "start_time": "2026-08-02T14:30:00Z",
    "status": "recording"
  }
}
```

### POST /recordings/stop

停止录像。FFmpeg 会优雅关闭，写入 MP4 的 moov atom。

**请求**

```json
{
  "recording_id": 105
}
```

### GET /recordings

查询录像列表。

**查询参数**

| 参数 | 类型 | 说明 |
|---|---|---|
| camera_id | uint | 按摄像头筛选 |
| status | string | 按状态筛选：`recording` / `completed` / `failed` |
| page | int | 页码（默认 1） |
| page_size | int | 每页条数（默认 20） |

**响应** (200)

```json
{
  "code": 0,
  "data": {
    "recordings": [
      {
        "id": 105,
        "camera_id": 1,
        "file_path": "data/recordings/1/1_20260802_143000.mp4",
        "file_size": 52428800,
        "start_time": "2026-08-02T14:30:00Z",
        "end_time": "2026-08-02T15:00:00Z",
        "duration": 1800,
        "trigger_type": "api",
        "status": "completed"
      }
    ],
    "total": 42,
    "page": 1,
    "page_size": 20
  }
}
```

### GET /recordings/:id

获取单个录像记录。

### GET /recordings/:id/download

下载 MP4 录像文件。支持 HTTP Range 断点续传。

**断点续传示例**

```bash
# 完整下载
curl -O http://localhost:8080/api/v1/recordings/105/download \
  -H "Authorization: Bearer <token>"

# 断点续传
curl -C 1048576 -o recording.mp4 \
  http://localhost:8080/api/v1/recordings/105/download \
  -H "Authorization: Bearer <token>"
```

---

## WebSocket

### GET /ws/v1/system

WebSocket 连接，用于接收实时事件推送。

**连接**

```javascript
const ws = new WebSocket('ws://localhost:8080/ws/v1/system?client_id=web-1');
ws.onmessage = (event) => {
  const msg = JSON.parse(event.data);
  console.log(msg.type, msg.data);
};
```

**事件格式**

```json
{
  "type": "camera_status",
  "timestamp": "2026-08-02T10:00:00Z",
  "data": {
    "camera_id": 1,
    "name": "Front Gate",
    "status": "online"
  }
}
```

**事件类型**

| type | 触发时机 | data 字段 |
|---|---|---|
| `connected` | 连接成功 | `client_id`, `message` |
| `camera_status` | 摄像头上线/离线 | `camera_id`, `name`, `status` |
| `recording_status` | 录像开始/结束/异常 | `recording_id`, `camera_id`, `status` |
| `time_sync_event` | 对时完成 | `camera_id`, `success`, `message` |
| `system_metrics` | 每 5 秒推送 | `cpu_goroutines`, `mem_alloc_mb`, `mem_sys_mb`, `uptime_seconds` |

---

## 错误响应

所有错误使用统一格式：

```json
{
  "code": 500,
  "message": "error description"
}
```

常见 HTTP 状态码：

| 状态码 | 说明 |
|---|---|
| 200 | 成功 |
| 201 | 创建成功 |
| 204 | 删除成功 |
| 400 | 请求参数错误 |
| 401 | 未认证 / token 过期 |
| 404 | 资源不存在 |
| 500 | 服务端错误 |

---

## 健康检查

### GET /health

无需鉴权。

**响应** (200)

```json
{"status": "ok"}
```
