# 自助机接口说明

本文档面向使用 CameraIO 的自助机、闸机或业务终端。所有示例均使用相对路径；将其拼接到 CameraIO 服务地址即可，例如 `http://192.168.1.10:8080`。

## 约定

- API 前缀：`/api/v1`
- 请求和响应编码：`application/json; charset=utf-8`
- 除登录外，接口使用 JWT 鉴权：`Authorization: Bearer <token>`。
- MJPEG 预览和浏览器直接下载不能自定义请求头时，可以改用 `?token=<token>` 查询参数。
- 通用成功响应为 `{ "code": 0, "message": "ok", "data": ... }`；失败时 `code` 为 HTTP 状态码，`message` 为错误原因。
- `camera_id` 和 `recording_id` 均为正整数。

## 推荐调用流程

1. 登录，保存 JWT。
2. 获取摄像头列表，让用户选择摄像头。
3. 获取 MJPEG 预览地址；需要留存图片时，保存该 MJPEG 流中的下一帧 JPEG。
4. 开始录像，保存返回的 `recording.id`。
5. 停止录像，从响应的 `download_url` 获取视频文件。

## 1. 登录

`POST /api/v1/login`

请求：

```json
{
  "username": "kiosk",
  "password": "your-password"
}
```

成功响应：

```json
{
  "code": 0,
  "message": "ok",
  "data": {
    "token": "eyJhbGciOiJIUzI1NiIs...",
    "user": {
      "id": 3,
      "username": "kiosk",
      "role": "viewer",
      "created_at": "2026-08-07T06:00:00Z"
    }
  }
}
```

后续请求示例：

```http
Authorization: Bearer eyJhbGciOiJIUzI1NiIs...
```

认证失败返回 `401`。

## 2. 获取摄像头列表

`GET /api/v1/cameras`

成功响应中的自助机常用字段：

```json
{
  "code": 0,
  "message": "ok",
  "data": [
    {
      "id": 1,
      "name": "入口摄像头",
      "ip": "192.168.1.100",
      "status": "online",
      "resolution": "1920x1080",
      "codec": "H.264",
      "access_protocol": "rtsp"
    }
  ]
}
```

`status` 为 `online` 或 `offline`。自助机应仅展示或选择 `online` 摄像头，并以 `id` 作为后续接口的 `camera_id`。

> 当前摄像头管理接口返回的是完整摄像头模型。自助机只应读取上例中的业务字段，不能记录、显示或转发 RTSP 地址及设备凭据。

## 3. 当前图像、拍照与实时预览

### 获取预览地址

`GET /api/v1/streams/{camera_id}/mjpeg?token={token}`

示例：

```text
http://192.168.1.10:8080/api/v1/streams/1/mjpeg?token=eyJhbGciOiJIUzI1NiIs...
```

响应为 `multipart/x-mixed-replace` MJPEG 流，每个分段都是 `image/jpeg`。该接口会在需要时自动启动摄像头拉流；不需要预览后关闭客户端连接即可。

### 当前图像/拍照说明

当前版本没有单独的静态拍照接口。自助机如需“拍照”，应连接上面的 MJPEG 地址并保存收到的第一帧或最新一帧 JPEG；该文件就是拍照结果。若业务必须由服务端保存图片，需要另行增加独立的 snapshot 接口。

## 4. 开始视频录制

`POST /api/v1/recordings/start`

请求：

```json
{
  "camera_id": 1,
  "format": "mp4",
  "with_audio": false,
  "custom_name": "kiosk_20260807_001",
  "trigger_type": "api",
  "max_duration": 120,
  "bitrate": 0
}
```

字段说明：

| 字段 | 必填 | 说明 |
| --- | --- | --- |
| `camera_id` | 是 | 摄像头 ID。 |
| `format` | 否 | `mp4`（默认）、`webm` 或 `ts`。自助机推荐 `mp4`。 |
| `with_audio` | 否 | 是否保留音频，默认 `false`。 |
| `custom_name` | 否 | 文件名附加标识；请只使用安全的业务编号。 |
| `trigger_type` | 否 | 建议固定为 `api`。 |
| `max_duration` | 否 | 最大录制秒数，`0` 表示不限制。建议设置上限，防止异常流程持续录像。 |
| `bitrate` | 否 | kbps；`0` 为原码流拷贝，非 0 时转码限码率。 |

成功响应（`201`）：

```json
{
  "code": 0,
  "message": "created",
  "data": {
    "id": 58,
    "camera_id": 1,
    "status": "recording",
    "format": "mp4",
    "start_time": "2026-08-07T06:10:00Z"
  }
}
```

保存 `data.id`，它是停止和下载录像所需的 `recording_id`。

## 5. 结束视频录制并获取下载地址

`POST /api/v1/recordings/stop`

请求：

```json
{
  "recording_id": 58
}
```

成功响应（`200`）：

```json
{
  "code": 0,
  "message": "ok",
  "data": {
    "message": "recording stopped",
    "recording_id": 58,
    "download_url": "/api/v1/recordings/58/download"
  }
}
```

`download_url` 是受 JWT 保护的相对地址。自助机 HTTP 客户端下载时应继续带 `Authorization` 请求头；若由浏览器地址栏、`<a>` 或播放器直接访问，可使用：

```text
{base_url}{download_url}?token={token}
```

下载接口支持 HTTP Range，可用于断点续传：`GET /api/v1/recordings/{recording_id}/download`。

## 错误处理建议

| HTTP 状态 | 处理建议 |
| --- | --- |
| `400` | 参数不完整或 `camera_id`/`recording_id` 非法；提示用户重新选择。 |
| `401` | Token 缺失、失效或无效；重新登录后重试一次。 |
| `404` | 摄像头或录像文件不存在；刷新摄像头/录像状态。 |
| `500` | 设备离线、FFmpeg 拉流失败或停止失败；保留错误消息供运维排查。 |

## JavaScript 简例

```js
const baseURL = 'http://192.168.1.10:8080'

const login = await fetch(`${baseURL}/api/v1/login`, {
  method: 'POST',
  headers: { 'Content-Type': 'application/json' },
  body: JSON.stringify({ username: 'kiosk', password: 'your-password' }),
}).then(r => r.json())

const token = login.data.token
const headers = {
  Authorization: `Bearer ${token}`,
  'Content-Type': 'application/json',
}

const started = await fetch(`${baseURL}/api/v1/recordings/start`, {
  method: 'POST',
  headers,
  body: JSON.stringify({ camera_id: 1, format: 'mp4', max_duration: 120 }),
}).then(r => r.json())

const stopped = await fetch(`${baseURL}/api/v1/recordings/stop`, {
  method: 'POST',
  headers,
  body: JSON.stringify({ recording_id: started.data.id }),
}).then(r => r.json())

const downloadURL = `${baseURL}${stopped.data.download_url}?token=${encodeURIComponent(token)}`
const previewURL = `${baseURL}/api/v1/streams/1/mjpeg?token=${encodeURIComponent(token)}`
```
