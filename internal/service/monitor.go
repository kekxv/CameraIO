package service

import (
	"context"
	"log"
	"os"
	"runtime"
	"time"

	"gorm.io/gorm"

	"CameraIO/internal/model"
)

// SystemMonitor 后台监控摄像头状态与系统指标，定期广播到 EventBus。
type SystemMonitor struct {
	db       *gorm.DB
	eventBus *EventBus
	ctx      context.Context
	cancel   context.CancelFunc
}

func NewSystemMonitor(db *gorm.DB, eventBus *EventBus) *SystemMonitor {
	ctx, cancel := context.WithCancel(context.Background())
	return &SystemMonitor{
		db:       db,
		eventBus: eventBus,
		ctx:      ctx,
		cancel:   cancel,
	}
}

// Start 启动后台监控循环。
func (m *SystemMonitor) Start() {
	go m.cameraStatusLoop()
	go m.systemMetricsLoop()
}

// Stop 停止后台监控。
func (m *SystemMonitor) Stop() {
	m.cancel()
}

// cameraStatusLoop 每 10 秒检测一次摄像头在线状态。
func (m *SystemMonitor) cameraStatusLoop() {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-m.ctx.Done():
			return
		case <-ticker.C:
			m.checkCameraStatus()
		}
	}
}

func (m *SystemMonitor) checkCameraStatus() {
	var cameras []model.Camera
	if err := m.db.Find(&cameras).Error; err != nil {
		log.Printf("[monitor] list cameras: %v", err)
		return
	}

	for _, cam := range cameras {
		newStatus := m.probeCameraStatus(cam)

		if cam.Status != newStatus {
			m.db.Model(&model.Camera{}).Where("id = ?", cam.ID).
				Update("status", newStatus)
			m.eventBus.PublishCameraStatus(cam.ID, cam.Name, newStatus)
		}
	}
}

// probeCameraStatus 多策略探活摄像头。
func (m *SystemMonitor) probeCameraStatus(cam model.Camera) string {
	// 策略 1: TCP 探测 RTSP 端口
	port := cam.Port
	if port == 0 {
		port = 554
	}
	if err := probeTCPPort(cam.IP, port, 3*time.Second); err == nil {
		return model.CameraStatusOnline
	}

	// 策略 2: TCP 探测 ONVIF HTTP 端口 (80)
	if cam.IP != "" {
		if err := probeTCPPort(cam.IP, 80, 2*time.Second); err == nil {
			return model.CameraStatusOnline
		}
	}

	return model.CameraStatusOffline
}

// systemMetricsLoop 每 5 秒广播一次系统指标。
func (m *SystemMonitor) systemMetricsLoop() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-m.ctx.Done():
			return
		case <-ticker.C:
			m.broadcastMetrics()
		}
	}
}

func (m *SystemMonitor) broadcastMetrics() {
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)

	metrics := map[string]interface{}{
		"cpu_goroutines": runtime.NumGoroutine(),
		"mem_alloc_mb":   float64(mem.Alloc) / 1024 / 1024,
		"mem_sys_mb":     float64(mem.Sys) / 1024 / 1024,
		"uptime_seconds": time.Since(startTime).Seconds(),
	}

	// 磁盘剩余空间（录像目录）
	if stat, err := os.Stat("."); err == nil {
		_ = stat
	}

	m.eventBus.PublishSystemMetrics(metrics)
}

var startTime = time.Now()
