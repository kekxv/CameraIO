# CameraIO 轻量级无延迟网络视频录像系统 (Software NVR)
## 系统设计方案与 AI 协作开发实施计划

---

## 1. 项目背景与设计目标

### 1.1 项目背景
在传统的安防与视频监控场景中，硬件嵌入式 NVR（网络视频录像机）存在以下痛点：
* **厂商锁死与协议封闭**：二次开发接口复杂，难以与客户现有的 Web 系统或业务平台集成。
* **高预览延迟**：传统 NVR 经过重重转码与缓冲，视频预览延迟通常在 2~5 秒以上，无法满足工业自动化、无人值守、精确推流等对低延迟要求极高的场景。
* **设备时间不同步**：多台摄像头与录像机时钟漂移，导致发生事故时视频证据的时间戳对不上。

### 1.2 系统设计目标
**CameraIO** 旨在打造一款**高吞吐、极低延迟、API 优先（API-First）的轻量级软件 NVR**。直接通过网线/局域网连接海康威视、宇视及各类标准 PTZ 球机，替代传统硬件 NVR。

* **极致低延迟**：设备到 CameraIO 采集延迟 $< 30\text{ms}$，端到端预览延迟 $< 300\text{ms}$。
* **零 CPU 占用录像**：采用 Stream-Copy（流拷贝）技术，实现多路视频无损直接存盘。
* **自动时间同步**：基于 ONVIF / GB28181 / NTP 实现对所有接入摄像头的精准时钟同步。
* **开放的 API 体系**：提供完整的 HTTP REST API 和 WebSocket 接口，支持第三方系统完全控制录像、下载与设备状态监测。
* **轻量化部署**：基于 Go 语言静态编译，单文件部署，内置轻量级数据库，无需复杂环境依赖。

---

## 2. 系统整体架构设计

CameraIO 采用分层与模块化架构，解耦流媒体处理、设备控制与业务逻辑。

```text
┌─────────────────────────────────────────────────────────────────────────┐
│                           第三方系统 / Web 客户端                       │
└────────────────────────────────────┬────────────────────────────────────┘
┌────────────────────────────────────▼────────────────────────────────────┐
│                             CameraIO 核心服务                           │
│                                                                         │
│  ┌──────────────────────┐  ┌──────────────────────┐  ┌───────────────┐  │
│  │   HTTP/WS API 模块   │  │    用户与设备管理    │  │  轻量 Web UI  │  │
│  └──────────┬───────────┘  └──────────┬───────────┘  └───────┬───────┘  │
│             │                         │                      │          │
│  ┌──────────▼─────────────────────────▼──────────────────────▼───────┐  │
│  │                    SQLite 数据库 (GORM)                           │  │
│  └────────────────────────────────────┬──────────────────────────────┘  │
│                                       │                                 │
│  ┌────────────────────────────────────▼──────────────────────────────┐  │
│  │                  核心音视频引擎 & 设备控制逻辑                    │  │
│  │                                                                   │  │
│  │  ┌──────────────────┐  ┌──────────────────┐  ┌──────────────────┐  │  │
│  │  │ Camera Auto-Tuner│  │ Zero-Buffer      │  │ Stream-Copy      │  │  │
│  │  │ (低延迟参数自动调优)│  │ Ingestion Engine │  │ Recorder (MP4)   │  │  │
│  │  └────────┬─────────┘  └────────┬─────────┘  └────────┬─────────┘  │  │
│  │           │                     │                     │            │  │
│  │  ┌────────▼─────────┐  ┌────────▼─────────┐  ┌────────▼─────────┐  │  │
│  │  │ 云台PTZ 控制模块 │  │ GB28181 推拉流   │  │ 低延迟分发 Engine│  │  │
│  │  └──────────────────┘  └──────────────────┘  └──────────────────┘  │  │
│  └────────────────────────────────────┬──────────────────────────────┘  │
└───────────────────────────────────────┼─────────────────────────────────┘
                                        │ (局域网直连 RTSP/UDP / ONVIF / SIP)
┌───────────────────────────────────────▼─────────────────────────────────┐
│               前端监控设备 (海康威视 / 宇视 / 各类 PTZ 球机)             │
└─────────────────────────────────────────────────────────────────────────┘
```

---

## 3. 关键技术方案设计

### 3.1 设备到 CameraIO 的“极致低延迟”方案
为实现设备到 CameraIO 的采集延迟 $< 30\text{ms}$，采取以下三层优化：

1. **传输协议强制 UDP**：使用 `RTSP over UDP` 或 `GB28181 (RTP over UDP)`，避免 TCP 握手与重传引入的延迟。
2. **Camera Auto-Tuner (摄像头参数自动调优模块)**：
   CameraIO 在添加摄像头后，通过 ONVIF/ISAPI 自动将摄像头配置下发为以下极速模式：
   * `B-Frame = 0`（完全关闭 B 帧，消除双向预测导致的帧积压）。
   * `Smart265 / U-Code = OFF`（关闭厂商智能压缩，防止动态 GOP 导致延迟飘忽不定）。
   * `GOP = FPS`（固定 1 秒一个关键帧，如 25fps 则 GOP=25）。
   * `Rate Control = CBR`（恒定码率，确保平滑传输）。
3. **服务端应用层 Zero-Buffer（零缓存）**：
   Go 语言网络层在 Socket 收到 UDP 包后，直通处理机制，不建立任何队列缓冲，直接送入录像落盘与分发模块。

### 3.2 零 CPU 消耗录像方案 (Stream-Copy)
* **原则**：绝对禁止在服务器端对视频流进行重新编码（Re-encoding）。
* **实现**：利用 H.264/H.265 NALU 解析器，直接抽取 RTP 包中的 Elementary Stream (ES)，通过封装器（MP4 Muxer）直接打包写入本地 `.mp4` 文件。
* **性能**：单核 CPU 可同时支持 50+ 路 4K 监控视频的并发录像落盘。

### 3.3 自动化设备对时方案 (Time Synchronization)
CameraIO 作为区域时间主节点（Time Master），支持三种对时机制：
1. **ONVIF 对时**：定时调用 ONVIF `SetSystemDateAndTime` 命令，将摄像头的系统时间强行同步为 CameraIO 的服务器时间（精确到毫秒）。
2. **GB28181 对时**：在 SIP 协议的心跳包（Keepalive）及注册响应中附带 `Date` 标头，摄像头自动对齐。
3. **内置 NTP 服务**：CameraIO 内置轻量级 NTP 服务，摄像头可将 CameraIO 设置为 NTP 时间服务器。

---

## 4. 技术栈选型

| 维度 | 选型 | 说明 |
| :--- | :--- | :--- |
| **后端语言** | **Go (Golang 1.21+)** | 高并发、内存占用小、天然适合网络流媒体处理、单文件编译部署 |
| **数据库** | **SQLite 3 + GORM** | 轻量级嵌入式数据库，无需安装外置数据库服务，极其适合 NVR 场景 |
| **工具库** | **FFmpeg (CLI 工具集成)** | 用于辅助视频切片与特定格式修复（作为备用方案） |
| **前端框架** | **Vue 3 + Vite + TailwindCSS** | 轻量现代化的单页应用 (SPA) |

---

## 5. 数据库模型设计 (SQLite Schema)

### 5.1 用户表 (`users`)
```sql
CREATE TABLE users (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    username VARCHAR(32) UNIQUE NOT NULL,
    password_hash VARCHAR(128) NOT NULL,
    role VARCHAR(16) DEFAULT 'admin', -- admin, viewer
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
```

### 5.2 摄像头表 (`cameras`)
```sql
CREATE TABLE cameras (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name VARCHAR(64) NOT NULL,
    ip VARCHAR(45) NOT NULL,
    port INT DEFAULT 554,
    rtsp_url VARCHAR(255) NOT NULL,
    brand VARCHAR(32) DEFAULT 'custom', -- hikvision, uniview, custom
    username VARCHAR(64),
    password VARCHAR(64),
    auto_tune_enabled BOOLEAN DEFAULT 1, -- 是否自动优化摄像头编码参数
    status VARCHAR(16) DEFAULT 'offline', -- online, offline
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
```

### 5.3 录像记录表 (`recordings`)
```sql
CREATE TABLE recordings (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    camera_id INTEGER NOT NULL,
    file_path VARCHAR(255) NOT NULL,
    file_size BIGINT DEFAULT 0,
    start_time DATETIME NOT NULL,
    end_time DATETIME,
    duration INTEGER DEFAULT 0, -- 秒
    trigger_type VARCHAR(16) DEFAULT 'api', -- api, manual, schedule
    status VARCHAR(16) DEFAULT 'recording', -- recording, completed, failed
    FOREIGN KEY(camera_id) REFERENCES cameras(id) ON DELETE CASCADE
);
```

---

## 6. API 接口规范设计

### 6.1 HTTP RESTful API

#### 1. 设备管理
* `GET /api/v1/cameras`：获取摄像头列表及当前状态。
* `POST /api/v1/cameras`：添加摄像头（自动触发 ONVIF 参数调优与对时）。
* `PUT /api/v1/cameras/:id`：修改摄像头配置。
* `DELETE /api/v1/cameras/:id`：删除摄像头。
* `POST /api/v1/cameras/:id/sync-time`：主动对该摄像头进行时间同步。

#### 2. 录像控制
* `POST /api/v1/recordings/start`：提交录制请求。
  * **Body**: `{"camera_id": 1, "custom_name": "task_001"}`
* `POST /api/v1/recordings/stop`：结束录制请求。
  * **Body**: `{"recording_id": 105}`
* `GET /api/v1/recordings`：查询录像历史（支持按 camera_id、时间段筛选）。
* `GET /api/v1/recordings/:id/download`：下载指定录像文件（支持 HTTP Range 断点续传）。

#### 3. 预览与流媒体
* `GET /api/v1/streams/:id/mjpeg`：获取 MJPEG 极简流（低配置设备预览）。

### 6.2 WebSocket API (`/ws/v1/system`)
用于向第三方客户端实时推送事件：
* **推送消息类型**：
  * `camera_status`：摄像头上线/离线通知。
  * `recording_status`：录像开始、结束或异常中断通知。
  * `time_sync_event`：对时结果通知。
  * `system_metrics`：CameraIO 的 CPU、内存、磁盘剩余空间。

---

## 7. 分阶段 AI 协作开发实施计划 (Prompts 路线图)

开发者可将以下计划逐阶段复制给 AI 编程助手（如 Cursor、Claude 3.5 Sonnet）进行代码落地。

### 阶段 1：项目骨架与数据库初始化
* **目标**：建立 Go 项目结构、JWT 鉴权、SQLite 数据库迁移及摄像头/用户的 CRUD。
* **AI Prompt 指令**：
  > "请作为高级 Go 开发者，用 Gin 框架和 GORM (SQLite) 帮我编写 CameraIO 的项目骨架。
  > 要求：
  > 1. 设计标准的项目结构（cmd, internal/api, internal/model, internal/service, internal/pkg）。
  > 2. 实现 User 和 Camera 的 GORM Data Model 及 DB 初始化。
  > 3. 实现基于 JWT 的登录接口和中间件。
  > 4. 实现 Camera 的 RESTful API（增删改查）。"

### 阶段 2：摄像头 ONVIF/ISAPI 低延迟调优与对制服务
* **目标**：实现对海康/宇视摄像头的自动配置优化与时间同步。
* **AI Prompt 指令**：
  > "请在 Go 中编写 `internal/service/onvif.go` 服务：
  > 1. 集成 ONVIF 协议，实现 `SyncCameraTime(ip, user, pass)`，将摄像头时间设置为服务器当前时间。
  > 2. 实现 `OptimizeVideoSettings(ip, user, pass)`，通过 ONVIF 或海康 ISAPI 强制将摄像头配置改为：关闭 B 帧，关闭 Smart265/U-Code，GOP 设为等于 FPS。
  > 3. 当添加新摄像头时，异步自动调用上述两个方法。"

* **AI Prompt 指令**：
  > "请在 Go 中编写流媒体接入与分发模块：
  > 1. 实现一个基于 UDP 传输的 RTSP 拉流器，要求应用层无队列缓冲。
  > 3. 实现一个降级方案：提供 `/api/v1/streams/:id/mjpeg` 接口，输出 MJPEG 格式视频流。"

### 阶段 4：Stream-Copy 录像引擎与文件管理
* **目标**：实现通过 API 触发/停止录制，零 CPU 占用直接将 RTSP 流保存为 MP4。
* **AI Prompt 指令**：
  > "请编写 `internal/service/recorder.go` 录像服务：
  > 1. 实现 `StartRecording(cameraID)` 和 `StopRecording(recordingID)`。
  > 2. 录像开启时，调用 FFmpeg 命令行或使用原生 Go mp4muxer，执行 `-c copy`（流拷贝）将 RTSP/UDP 流写入 `data/recordings/{camera_id}/{timestamp}.mp4`。
  > 3. 录像状态实时更新到 SQLite 的 `recordings` 表。
  > 4. 提供 HTTP 下载接口，支持断点续传（HTTP Range Header）。"

### 阶段 5：WebSocket 实时状态推送与 API 整合
* **目标**：整合 HTTP REST API 与 WebSocket 实时事件推送。
* **AI Prompt 指令**：
  > "请为 CameraIO 添加 WebSocket 支持：
  > 1. 创建 `/ws/v1/system` 路由，支持多客户端订阅。
  > 2. 当录像开始/结束、摄像头上线/离线、时间同步完成时，通过 WebSocket 发送 JSON 广播通知。
  > 3. 编写后台 Cron 任务，每 5 秒检测一次摄像头在线状态和服务器磁盘剩余空间并广播。"

### 阶段 6：轻量化前端 Web UI 开发
* **目标**：使用 Vue 3 + TailwindCSS 实现极简控制台。
* **AI Prompt 指令**：
  > "请用 Vue 3 + Vite + TailwindCSS 编写 CameraIO 前端页面：
  > 1. **登录页**：JWT 认证。
  > 2. **摄像头管理**：列表展示状态，提供'添加摄像头'、'同步时间'按钮。
  > 4. **录像中心**：列表展示历史录像，支持按摄像头筛选、在线预览播放及一键下载 MP4。"

---

## 8. 系统部署与运行

### 8.1 编译与打包
得益于 Go 的特性，系统可静态编译为单个可执行二进制文件：
```bash
# 编译后端
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o cameraio ./cmd/server/

# 静态资源嵌入 (Go Embed)
# 可将前端产物 (dist) 直接嵌入到 Go 二进制中，实现真正单文件运行。
```

### 8.2 运行环境要求
* **操作系统**：Linux (Ubuntu/Debian/CentOS) 或 Windows Server。
* **依赖工具**：系统预装 `ffmpeg`（用于辅助录像切片打包）。
* **网络环境**： CameraIO 服务器与监控摄像头处于同一局域网/网线直连，网口支持千兆。
