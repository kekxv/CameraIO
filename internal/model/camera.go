package model

import (
	"time"
)

// Camera 摄像头数据模型，支持 RTSP 和 GB28181 两种接入方式。
type Camera struct {
	ID                uint      `json:"id" gorm:"primaryKey"`
	Name              string    `json:"name" gorm:"type:varchar(64);not null"`
	IP                string    `json:"ip" gorm:"type:varchar(45);not null"`
	Port              int       `json:"port" gorm:"default:554"`
	RTSPUrl           string    `json:"rtsp_url" gorm:"type:varchar(255);not null"`
	Brand             string    `json:"brand" gorm:"type:varchar(32);default:custom"`
	Username          string    `json:"username,omitempty" gorm:"type:varchar(64)"`
	Password          string    `json:"password,omitempty" gorm:"type:varchar(64)"`
	AutoTuneEnabled   bool      `json:"auto_tune_enabled" gorm:"default:true"`
	Status            string    `json:"status" gorm:"type:varchar(16);default:offline"`
	LastTimeSync      *time.Time `json:"last_time_sync,omitempty"`
	// Codec: 视频编码格式（H.264 / H.265），由在线检查时获取
	Codec string `json:"codec,omitempty" gorm:"type:varchar(16)"`
	// Resolution: 视频分辨率（如 1920x1080），由在线检查时获取
	Resolution string `json:"resolution,omitempty" gorm:"type:varchar(32)"`

	// ---- 设备类型 ----
	// DeviceType: "ipc" (网络摄像机) / "nvr" (网络录像机) / "dvr" (数字录像机) / "encoder" (编码器)
	// NVR/DVR 模式下，一个设备包含多个通道（摄像头），每个通道通过 NVRChannel 区分。
	DeviceType string `json:"device_type,omitempty" gorm:"type:varchar(16);default:ipc"`
	// NVRChannel: NVR/DVR 上的通道号（1-256）。0 表示 IPC 直连。
	NVRChannel int `json:"nvr_channel,omitempty" gorm:"default:0"`
	// PreferredCodec: 首选编码格式 "auto" / "h264" / "h265"。默认 "auto" 不主动切换。
	PreferredCodec string `json:"preferred_codec,omitempty" gorm:"type:varchar(8);default:auto"`
	// StreamType: "main" (主码流) / "sub" (子码流)。默认 "main"。
	// 主码流清晰度高，子码流占用带宽低、流畅度高。
	StreamType string `json:"stream_type,omitempty" gorm:"type:varchar(8);default:main"`

	// ---- 接入协议 ----
	// AccessProtocol: "rtsp" / "gb28181" / "local"
	AccessProtocol string `json:"access_protocol" gorm:"type:varchar(16);default:rtsp"`

	// ---- GB28181 专用字段 ----
	// DeviceID: 20 位国标编码（如 34020000001320000001）
	DeviceID string `json:"device_id,omitempty" gorm:"type:varchar(20);index"`
	// ChannelID: 通道编码（通常为 DeviceID 本身，或 20 位通道编码）
	ChannelID string `json:"channel_id,omitempty" gorm:"type:varchar(20)"`
	// Transport: GB28181 流传输协议 "UDP" / "TCP" / "TCP/AUTO"
	Transport string `json:"transport,omitempty" gorm:"type:varchar(16);default:UDP"`

	// ---- 本地摄像头专用字段 ----
	// LocalIndex: 本地摄像头索引号（如 /dev/video0 的 0）
	LocalIndex int `json:"local_index,omitempty" gorm:"default:-1"`
	// LocalVID: USB Vendor ID
	LocalVID string `json:"local_vid,omitempty" gorm:"type:varchar(8)"`
	// LocalPID: USB Product ID
	LocalPID string `json:"local_pid,omitempty" gorm:"type:varchar(8)"`
	// LocalName: 本地摄像头设备名称
	LocalName string `json:"local_name,omitempty" gorm:"type:varchar(128)"`

	CreatedAt time.Time `json:"created_at"`
}

const (
	CameraStatusOnline  = "online"
	CameraStatusOffline = "offline"
)

const (
	BrandHikvision = "hikvision"
	BrandUniview   = "uniview"
	BrandCustom    = "custom"
)

// 设备类型常量
const (
	DeviceTypeIPC     = "ipc"     // 网络摄像机
	DeviceTypeNVR     = "nvr"     // 网络录像机（多通道）
	DeviceTypeDVR     = "dvr"     // 数字录像机
	DeviceTypeEncoder = "encoder" // 编码器
)

// 码流类型常量
const (
	StreamTypeMain = "main" // 主码流
	StreamTypeSub  = "sub"  // 子码流
)

const (
	ProtocolRTSP    = "rtsp"
	ProtocolGB28181 = "gb28181"
	ProtocolLocal   = "local"
)

// IsNVR 判断是否为录像机设备（包含多个通道）。
func (c *Camera) IsNVR() bool {
	return c.DeviceType == DeviceTypeNVR || c.DeviceType == DeviceTypeDVR
}
