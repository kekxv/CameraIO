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
| `CAMERAIO_SIP_ADDR` | `:5060` | GB28181 SIP 信令监听地址 |
| `CAMERAIO_SIP_SERVER_ID` | `34020000002000000001` | SIP 服务器 20 位国标编码 |
| `CAMERAIO_SIP_REALM` | `3402000000` | SIP 域 |
| `CAMERAIO_RTP_PORT_MIN` | `10000` | RTP 端口范围下限 |
| `CAMERAIO_RTP_PORT_MAX` | `11000` | RTP 端口范围上限 |
| `CAMERAIO_CONFIG` | `config.json` | 配置文件路径（不存在则自动创建默认配置） |

配置优先级：环境变量 > 配置文件 > 内置默认值。

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
