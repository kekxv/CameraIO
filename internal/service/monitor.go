package service

import (
	"context"
	"log"
	"runtime"
	"time"

	"gorm.io/gorm"

	"CameraIO/internal/model"
	"CameraIO/internal/pkg"
)

// SystemMonitor 后台监控摄像头状态与系统指标，定期广播到 EventBus。
type SystemMonitor struct {
	db       *gorm.DB
	eventBus *EventBus
	onvif    *ONVIFService
	ctx      context.Context
	cancel   context.CancelFunc
}

func NewSystemMonitor(db *gorm.DB, eventBus *EventBus, onvif *ONVIFService) *SystemMonitor {
	ctx, cancel := context.WithCancel(context.Background())
	return &SystemMonitor{
		db:       db,
		eventBus: eventBus,
		onvif:    onvif,
		ctx:      ctx,
		cancel:   cancel,
	}
}

// Start 启动后台监控循环。
func (m *SystemMonitor) Start() {
	go m.cameraStatusLoop()
	go m.enrichLoop()
	go m.timeSyncLoop()
	go m.systemMetricsLoop()
}

// enrichLoop 每 3 分钟对在线摄像头刷新分辨率/编码信息。
// 保证已在线摄像头（未发生状态转换）也能获取到编码信息。
func (m *SystemMonitor) enrichLoop() {
	// 启动时立即刷新一次（已在线摄像头）
	m.enrichAllOnline()

	ticker := time.NewTicker(3 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-m.ctx.Done():
			return
		case <-ticker.C:
			m.enrichAllOnline()
		}
	}
}

// enrichAllOnline 对所有在线摄像头刷新编码信息。
// RTSP 走 ONVIF/ISAPI；GB28181 设备若同时支持 ONVIF 也能获取（纯 SIP 设备快速失败）。
func (m *SystemMonitor) enrichAllOnline() {
	var cameras []model.Camera
	if err := m.db.Where("status = ?", model.CameraStatusOnline).Find(&cameras).Error; err != nil {
		log.Printf("[monitor] list online cameras for enrich: %v", err)
		return
	}

	for _, cam := range cameras {
		// 没有 ONVIF/ISAPI 凭据的设备跳过（如纯 SIP 注册的 GB28181 设备）
		if cam.Username == "" {
			continue
		}
		m.enrichCameraInfo(cam)
	}
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

			// 摄像头上线时，顺便获取分辨率/编码信息
			if newStatus == model.CameraStatusOnline {
				go m.enrichCameraInfo(cam)
			}
		}
	}
}

// enrichCameraInfo 通过 ONVIF 获取摄像头分辨率/编码信息并更新数据库。
// 仅对 RTSP 摄像头生效（GB28181 用 SIP，本地用系统设备，均无 ONVIF）。
func (m *SystemMonitor) enrichCameraInfo(cam model.Camera) {
	if cam.AccessProtocol == model.ProtocolGB28181 || cam.AccessProtocol == model.ProtocolLocal {
		return
	}
	if cam.Username == "" {
		return // 无 ONVIF 凭据
	}
	ctx, cancel := context.WithTimeout(m.ctx, 10*time.Second)
	defer cancel()

	info, err := m.onvif.GetVideoCodecInfo(ctx, cam.Brand, cam.IP, cam.Username, cam.Password, cam.NVRChannel)
	if err != nil || info == nil {
		// 获取失败不致命，仅记录
		return
	}
	updates := map[string]any{}
	if info.Codec != "" {
		updates["codec"] = normalizeCodecName(info.Codec)
	}
	if info.Resolution != "" {
		updates["resolution"] = info.Resolution
	}
	if len(updates) > 0 {
		m.db.Model(&model.Camera{}).Where("id = ?", cam.ID).Updates(updates)
	}
}

// normalizeCodecName 将编码名规范化为 "H.264"/"H.265"。
func normalizeCodecName(codec string) string {
	c := codec
	if len(c) >= 4 && (c[:4] == "H264" || c[:4] == "H265") {
		return c[:1] + "." + c[1:]
	}
	return c
}

// timeSyncLoop 每 10 分钟对在线摄像头执行一次时间同步。
func (m *SystemMonitor) timeSyncLoop() {
	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-m.ctx.Done():
			return
		case <-ticker.C:
			m.syncAllOnlineTimes()
		}
	}
}

// syncAllOnlineTimes 对所有在线摄像头同步时间。
func (m *SystemMonitor) syncAllOnlineTimes() {
	var cameras []model.Camera
	if err := m.db.Where("status = ?", model.CameraStatusOnline).Find(&cameras).Error; err != nil {
		log.Printf("[monitor] list online cameras: %v", err)
		return
	}

	for _, cam := range cameras {
		if cam.AccessProtocol != "" && cam.AccessProtocol != model.ProtocolRTSP {
			continue // 只同步 RTSP 摄像头
		}
		m.syncCameraTime(cam)
	}
}

// syncCameraTime 同步单个摄像头时间。
func (m *SystemMonitor) syncCameraTime(cam model.Camera) {
	ctx, cancel := context.WithTimeout(m.ctx, 10*time.Second)
	defer cancel()

	if err := m.onvif.SyncCameraTime(ctx, cam.IP, cam.Username, cam.Password, cam.NVRChannel, cam.DeviceTimezone); err != nil {
		log.Printf("[monitor] camera %d (%s) time sync failed: %v", cam.ID, cam.IP, err)
		return
	}
	now := time.Now()
	m.db.Model(&model.Camera{}).Where("id = ?", cam.ID).Update("last_time_sync", now)
	m.eventBus.PublishTimeSync(cam.ID, true, "定时同步成功")
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
		"ffmpeg":         pkg.GetFFmpegStatus(),
	}

	m.eventBus.PublishSystemMetrics(metrics)
}

var startTime = time.Now()
