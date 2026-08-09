# CameraIO

轻量级无延迟网络视频录像系统 (Software NVR)

CameraIO 是一款高吞吐、极低延迟、API 优先的轻量级软件 NVR，直接通过网线/局域网连接海康威视、宇视及各类标准 PTZ 球机，替代传统硬件 NVR。

## 核心特性

- **极致低延迟**：设备到 CameraIO 采集延迟 < 30ms，端到端预览延迟 < 300ms
- **零 CPU 占用录像**：Stream-Copy 技术，多路视频无损直接存盘
- **自动时间同步**：ONVIF / ISAPI / NTP 自动对时
- **GB/T 28181 国标支持**：SIP 信令服务，接收摄像头注册、心跳、目录查询、视频点播
- **本地摄像头支持**：直接捕获系统/USB 摄像头（v4l2/AVFoundation/DirectShow）
- **localcam 工具**：独立客户端，将本地摄像头发布为 RTSP 流，扩展设备集
- **开放 API**：完整 HTTP REST API + WebSocket 实时事件推送
- **轻量部署**：Go 静态编译，单文件 + SQLite，无外部依赖

## 系统架构

```
┌──────────────────────────────────────────────┐
│          第三方系统 / Web 客户端              │
└──────────────┬───────────────────────────────┘
               │ HTTP REST / WebSocket
┌──────────────▼───────────────────────────────┐
│              CameraIO 核心服务                │
│  ┌───────────┐ ┌───────────┐ ┌───────────┐  │
│  │ HTTP/WS   │ │ Camera &  │ │ 轻量 Web  │  │
│  │ API       │ │ User Mgmt │ │ UI        │  │
│  └─────┬─────┘ └─────┬─────┘ └─────┬─────┘  │
│  ┌─────▼─────────────▼─────────────▼─────┐  │
│  │          SQLite (GORM)                │  │
│  └───────────────┬───────────────────────┘  │
│  ┌───────────────▼───────────────────────┐  │
│  │  核心引擎                              │  │
│  │  · ONVIF/ISAPI 自动调优               │  │
│  │  · GB28181 SIP 信令 (UAS)             │  │
│  │  · RTP 收流 + PS 解封装               │  │
│  │  · RTSP (UDP) 零缓冲拉流              │  │
│  │  · MJPEG 低延迟分发          │  │
│  │  · Stream-Copy MP4 录像               │  │
│  │  · WebSocket 事件推送                 │  │
│  └───────────────────────────────────────┘  │
└──────────────┬──────────────────────────────┘
               │ RTSP/UDP / ONVIF
┌──────────────▼──────────────────────────────┐
│     前端摄像头 (海康 / 宇视 / PTZ 球机)     │
└──────────────────────────────────────────────┘
```

## 快速开始

### 环境要求

- Go 1.21+
- FFmpeg（用于 RTSP 拉流和录像）

### 编译

前端通过 `//go:embed` 直接打进 Go 二进制，因此 **必须先构建前端再编译后端**（`go build`/`go test` 都依赖 `frontend/dist` 存在）。

```bash
# 1. 构建前端
cd frontend && npm install && npm run build && cd ..

# 2. 编译后端（内嵌前端产物，生成自包含单文件；无需 GCC/CGO）
CGO_ENABLED=0 go build -o cameraio ./cmd/server/

# 运行（单文件即可，任意工作目录下均能访问 WebUI）
./cameraio
# 访问 http://localhost:8080
```

### 开发模式

```bash
# Terminal 1：启动后端
go run ./cmd/server/

# Terminal 2：启动前端（带热更新，自动代理 API 到后端）
cd frontend && npm run dev
# 访问 http://localhost:3000
```

### 环境变量配置

| 变量 | 默认值 | 说明 |
|---|---|---|
| `CAMERAIO_ADDR` | `:8080` | HTTP 监听地址 |
| `CAMERAIO_DB_PATH` | `data/cameradio.db` | SQLite 数据库路径 |
| `CAMERAIO_JWT_SECRET` | `change-me-in-production` | JWT 签名密钥 |
| `CAMERAIO_RECORDINGS_DIR` | `data/recordings` | 录像文件存储目录 |
| `CAMERAIO_RECORDING_SEGMENT_SECONDS` | `300` | MP4 录像分段时长（60–1800 秒） |
| `CAMERAIO_RECORDING_RETENTION_DAYS` | `30` | 录像保留天数（1–3650 天） |
| `CAMERAIO_RECORDING_CLEANUP_FREE_PERCENT` | `15` | 磁盘可用空间低于此比例时开始清理（1–99%） |
| `CAMERAIO_RECORDING_STOP_FREE_PERCENT` | `5` | 磁盘可用空间低于此比例时停止录像（1–99%，必须低于清理阈值） |
| `CAMERAIO_SIP_ADDR` | `:5060` | GB28181 SIP 信令监听地址 |
| `CAMERAIO_SIP_SERVER_ID` | `34020000002000000001` | SIP 服务器 20 位国标编码 |
| `CAMERAIO_SIP_REALM` | `3402000000` | SIP 域 |
| `CAMERAIO_RTP_PORT_MIN` | `10000` | RTP 端口范围下限 |
| `CAMERAIO_RTP_PORT_MAX` | `11000` | RTP 端口范围上限 |
| `CAMERAIO_CONFIG` | `config.json` | 配置文件路径（不存在则自动创建默认配置） |

配置优先级：环境变量 > 配置文件 > 内置默认值。

### Single-host safe mode

Live preview remains an independent, unchanged path: MJPEG is used for live
viewing, and m3u8 is not used for live viewing. Continuous archive recording
uses MP4 stream-copy; WebM remains playable only for legacy files and cannot be
selected for new recordings. Plan storage capacity at
approximately 1 Mbps ≈ 10.8 GB/day/camera.

### 单机录像验收与部署上限

不要以估算的摄像头数量作为这台 i5 主机的部署上限。每次部署到目标主机，使用
Linux 或 Windows 验收脚本产生一份带时间戳的证据日志；脚本运行构建/浏览器测试，
生成五分钟的 FFmpeg 分段样本并逐个用 `ffprobe` 验证可播放，并检查实际录像目录
和 SQLite `recording_segments` 的相邻时间间隙（不得超过两秒）：

```bash
scripts/verify-single-host-recording.sh \
  --camera-url 'rtsp://camera/substream' \
  --segments-dir data/recordings/CAMERA_ID/RECORDING_ID \
  --database data/cameradio.db \
  --latency-baseline baseline-ms.txt \
  --latency-recording recording-ms.txt \
  --resource-samples resource-samples.csv \
  --acceptance-evidence acceptance-evidence.csv
```

```powershell
.\scripts\verify-single-host-recording.ps1 -CameraUrl 'rtsp://camera/substream' `
  -SegmentsDir data\recordings\CAMERA_ID\RECORDING_ID -Database data\cameradio.db `
  -LatencyBaseline baseline-ms.txt -LatencyRecording recording-ms.txt `
  -ResourceSamples resource-samples.csv -AcceptanceEvidence acceptance-evidence.csv
```

`baseline-ms.txt` 和 `recording-ms.txt` 各有至少 30 行，每行是一次玻璃到玻璃
延迟（毫秒）。将一个摄像头对准另一块屏幕上的毫秒时钟，并在同一手机画面内拍到
实体时钟和 CameraIO 预览：先收集未录像的 30 次样本，再在连续分段录像时收集
30 次。每个样本必须小于 1000 ms，录像期间 p95 相对基线增加不得超过 100 ms。

`resource-samples.csv` 的表头必须为
`timestamp_unix,host_cpu_percent,recording_cpu_percent_per_stream,free_disk_percent`，其中
`timestamp_unix` 为 Unix 秒。至少记录 31 个严格递增的样本，任意相邻样本不得相隔
超过 60 秒，首尾必须相隔 30 分钟或更多。在全部必要摄像头
持续录像且打开正常数量预览格的情况下，完成 30 分钟正常自助服务流程；记录定期
样本并确认自助服务没有新增超时/错误、录制 FFmpeg 处于低于普通优先级、每流录像
CPU 小于 5%、主机持续 CPU 小于 70%、可用磁盘大于 15%。

在回放中，分别选择每个会话开始、中间、最后一秒，及两段相邻片段的精确边界。核对
正确墙钟内容打开、seek 误差最多一 GOP（配置上限一秒）、自动越界暂停最多 250 ms。
故意制造的真实录像间隙必须显示，不可悄悄跳过。资源门槛失败时，仅依次调整摄像头
H.264 子码流、相机侧 VBR、10–15 fps、一秒 GOP、再降低源码率/分辨率；不要启用服务端
WebM/VP9 或 H.264 软件转码。

`acceptance-evidence.csv` 使用精确表头 `field,value`，并包含以下 15 个且仅包含这些字段：
`playback_beginning`、`playback_middle`、`playback_final_second`、
`playback_segment_boundary`、`playback_gap_visible`、`ffmpeg_priority_below_normal`、
`self_service_workload` 的值均为 `pass`；`max_seek_error_ms` 不超过 1000，
`max_boundary_pause_ms` 不超过 250，`self_service_timeout_error_delta` 不大于 0；
`max_recording_cameras`、`max_preview_tiles`、`per_camera_bitrate_kbps`、
`retention_days`、`disk_capacity_gb` 必须是正数。该文件既记录计划要求的边界回放、
FFmpeg 低优先级和自助服务共存证据，也固定实测部署上限。

非 `--smoke`/`-Smoke` 模式缺少摄像头、生产片段、数据库、两组延迟、资源或上述验收
证据时会失败，也不允许跳过构建门禁。缩短的 smoke 会明确标记为非验收，允许输出
`NOT COLLECTED`，不能用于批准部署。

将实测值填入下表并与脚本日志一起保存；所有字段为空时，表示该主机尚未获得部署批准。

| 验收日期/日志 | 最大同时录像摄像头 | 最大预览格 | 每相机码率 | 保留天数 / 磁盘容量 | 基线/录像 p95 与最大延迟 | 主机 CPU |
|---|---:|---:|---:|---:|---|---:|
| 未测量 | 未测量 | 未测量 | 未测量 | 未测量 | 未测量 | 未测量 |

### 首次运行

系统会自动：
1. 检查配置文件 `config.json`，不存在则用默认值创建
2. 创建 SQLite 数据库并建表
3. 创建默认管理员账户 `admin / admin`
4. 启动 HTTP 服务，控制台打印 WebUI 访问地址（如 `http://localhost:8080`），用浏览器打开即可

## 项目结构

```
CameraIO/
├── cmd/server/main.go          # 入口
├── internal/
│   ├── api/                    # HTTP/WS 路由与 Handler
│   │   ├── auth.go             # 登录 & JWT 中间件
│   │   ├── camera.go           # 摄像头 CRUD
│   │   ├── handler.go          # Handler 统一结构
│   │   ├── recording.go        # 录像控制 & 下载
│   │   ├── router.go           # 路由注册
│   │   ├── stream.go           # 实时流（MJPEG）
│   │   └── websocket.go        # WebSocket 事件推送
│   ├── model/                  # 数据模型
│   │   ├── camera.go
│   │   ├── recording.go
│   │   └── user.go
│   ├── pkg/                    # 公共工具包
│   │   ├── config.go           # 配置
│   │   ├── database.go         # 数据库初始化 & 迁移
│   │   └── jwt.go              # JWT 生成/解析
│   └── service/                # 业务逻辑
│       ├── camera_service.go   # 摄像头管理 + ONVIF 集成
│       ├── eventbus.go         # 事件总线
│       ├── gb28181.go          # GB/T 28181 SIP 信令服务
│       ├── monitor.go          # 后台系统监控
│       ├── netutil.go          # 网络工具
│       ├── onvif.go            # ONVIF 对时 & ISAPI 调优
│       ├── ps_demuxer.go       # MPEG-2 PS 解封装器
│       ├── recorder.go         # Stream-Copy 录像引擎
│       ├── rtp_receiver.go     # GB28181 RTP 收流器
│       ├── stream.go           # RTSP 拉流器
│       ├── user_service.go     # 用户管理
│       └── mjpeg.go            # MJPEG 帧分发
└── go.mod
```

## API 概览

### 认证

所有 API（除 `/api/v1/login` 和 `/health`）需要 JWT 鉴权：

```
Authorization: Bearer <token>
```

### 用户管理

| 方法 | 路径 | 说明 |
|---|---|---|
| POST | `/api/v1/login` | 登录获取 JWT |
| GET | `/api/v1/users` | 用户列表 |
| POST | `/api/v1/users` | 创建用户 |

### 摄像头管理

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/v1/cameras` | 摄像头列表 |
| POST | `/api/v1/cameras` | 添加摄像头（自动触发 ONVIF 调优） |
| GET | `/api/v1/cameras/:id` | 获取摄像头详情 |
| PUT | `/api/v1/cameras/:id` | 更新摄像头 |
| DELETE | `/api/v1/cameras/:id` | 删除摄像头 |
| POST | `/api/v1/cameras/:id/sync-time` | 手动对时 |

### 实时流

| 方法 | 路径 | 说明 |
|---|---|---|
| POST | `/api/v1/streams/:id/start` | 启动拉流 |
| POST | `/api/v1/streams/:id/stop` | 停止拉流 |

| GET | `/api/v1/streams/:id/mjpeg` | MJPEG 预览流 |

### 录像控制

| 方法 | 路径 | 说明 |
|---|---|---|
| POST | `/api/v1/recordings/start` | 开始录像 |
| POST | `/api/v1/recordings/stop` | 停止录像 |
| GET | `/api/v1/recordings` | 录像列表（支持分页/筛选） |
| GET | `/api/v1/recordings/:id` | 录像详情 |
| GET | `/api/v1/recordings/:id/download` | 下载 MP4（支持断点续传） |

录像列表可按可选日期范围筛选全部历史记录，并保留现有分页；未填写日期时会查询
所有历史。分段录像从所选时间开始播放时，播放弹窗会动态使用命中片段及至多四个
连续后续片段会在当前片段结束后逐段加载播放；这不等同于历史查询范围。

### WebSocket

```
GET /ws/v1/system?client_id=xxx
```

推送事件类型：
- `camera_status`：摄像头上线/离线
- `recording_status`：录像开始/结束/异常
- `time_sync_event`：对时结果
- `system_metrics`：CPU/内存/磁盘指标

详细 API 文档请参考 [API.md](./API.md)。

## 开发

### 运行测试

```bash
# 全量测试（纯 Go，无需 CGO）
CGO_ENABLED=0 go test ./...

# 带覆盖率
CGO_ENABLED=0 go test ./... -cover

# 指定包
CGO_ENABLED=0 go test ./internal/service/... -v
```

### 技术栈

| 组件 | 选型 |
|---|---|
| 后端语言 | Go 1.21+ |
| HTTP 框架 | Gin |
| 数据库 | SQLite + GORM（纯 Go 驱动） |
| 鉴权 | JWT (golang-jwt) |

| WebSocket | gorilla/websocket |
| RTSP/录像 | FFmpeg (CLI subprocess) |

## 部署

### 编译为单文件

```bash
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o cameraio ./cmd/server/
```

将 `cameraio` 二进制文件复制到目标服务器即可运行，无需额外依赖（FFmpeg 需系统预装）。

### Systemd 服务

```ini
[Unit]
Description=CameraIO NVR Service
After=network.target

[Service]
ExecStart=/opt/cameraio/cameraio
Restart=always
Environment=CAMERAIO_JWT_SECRET=your-secret-here

[Install]
WantedBy=multi-user.target
```

### localcam 工具使用

`localcam` 是一个独立客户端工具，将本地 USB/系统摄像头作为 RTSP 流对外发布，用于扩展设备集。

```bash
# 列出本机可用摄像头
localcam list

# 将本机摄像头发布为 RTSP 流
localcam serve --index 0 --port 8554

# 按名称选择摄像头
localcam serve --name "FaceTime HD Camera"

# 按 USB VID/PID 选择
localcam serve --vid 0x046d --pid 0x0825
```

启动后，RTSP 流可通过 `rtsp://localhost:8554/live` 访问。CameraIO 可以像普通 IP 摄像头一样添加此流。

支持平台：
- **Linux**: v4l2 (USB 摄像头)
- **macOS**: AVFoundation (内置/FaceTime 摄像头)
- **Windows**: DirectShow (USB 摄像头)

## License

MIT
