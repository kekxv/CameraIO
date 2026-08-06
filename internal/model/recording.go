package model

import (
	"time"
)

type Recording struct {
	ID          uint       `json:"id" gorm:"primaryKey"`
	CameraID    uint       `json:"camera_id" gorm:"not null;index"`
	FilePath    string     `json:"file_path" gorm:"type:varchar(255);not null"`
	FileSize    int64      `json:"file_size" gorm:"default:0"`
	StartTime   time.Time  `json:"start_time" gorm:"not null"`
	EndTime     *time.Time `json:"end_time"`
	Duration    int        `json:"duration" gorm:"default:0"`
	TriggerType string     `json:"trigger_type" gorm:"type:varchar(16);default:api"`
	Status      string     `json:"status" gorm:"type:varchar(16);default:recording"`
	Format      string     `json:"format" gorm:"type:varchar(8);default:mp4"`
	WithAudio   bool       `json:"with_audio" gorm:"default:false"`

	Camera *Camera `json:"camera,omitempty" gorm:"foreignKey:CameraID"`
}

const (
	TriggerAPI      = "api"
	TriggerManual   = "manual"
	TriggerSchedule = "schedule"
)

const (
	RecordingStatusRecording = "recording"
	RecordingStatusCompleted = "completed"
	RecordingStatusFailed    = "failed"
)

// 录像格式常量
const (
	FormatMP4  = "mp4"
	FormatWebM = "webm"
	FormatTS   = "ts"
)

// RecordingSchedule 定时录像计划。
// 每天在 StartTime~EndTime 之间自动开始/停止录像，支持多段（每天一段）。
type RecordingSchedule struct {
	ID        uint      `json:"id" gorm:"primaryKey"`
	Name      string    `json:"name" gorm:"type:varchar(64);not null"`
	CameraID  uint      `json:"camera_id" gorm:"not null;index"`
	StartTime string    `json:"start_time" gorm:"type:varchar(5);not null"` // "HH:MM"
	EndTime   string    `json:"end_time" gorm:"type:varchar(5);not null"`   // "HH:MM"
	Days      int       `json:"days" gorm:"default:127"`                    // bitmask: bit0=周一 ... bit6=周日，127=每天
	Format    string    `json:"format" gorm:"type:varchar(8);default:mp4"`
	WithAudio bool      `json:"with_audio" gorm:"default:false"`
	Enabled   bool      `json:"enabled" gorm:"default:true"`
	CreatedAt time.Time `json:"created_at"`

	Camera *Camera `json:"camera,omitempty" gorm:"foreignKey:CameraID"`
}

// 星期 bitmask 常量
const (
	DayMonday    = 1 << 0 // 1
	DayTuesday   = 1 << 1 // 2
	DayWednesday = 1 << 2 // 4
	DayThursday  = 1 << 3 // 8
	DayFriday    = 1 << 4 // 16
	DaySaturday  = 1 << 5 // 32
	DaySunday    = 1 << 6 // 64
	DayAllWeek   = 127    // 每天
)

// HasDay 判断 Days bitmask 是否包含指定星期。
// weekday: time.Weekday，Sunday=0, Monday=1 ... Saturday=6
func (s *RecordingSchedule) HasDay(weekday int) bool {
	// 转换: Go 的 Weekday Sunday=0, 我们的 bitmask bit0=Monday
	// Sunday(0) → bit6(64), Monday(1) → bit0(1), ..., Saturday(6) → bit5(32)
	bit := 1 << uint((weekday+6)%7)
	return s.Days&bit != 0
}
