package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"CameraIO/internal/model"
	"CameraIO/internal/pkg"

	"gorm.io/gorm"
)

// RecorderService 管理录像任务：Stream-Copy 将 RTSP 流直接写入 MP4。
type RecorderService struct {
	db      *gorm.DB
	cfg     *pkg.Config
	mu      sync.Mutex
	tasks   map[uint]*recordTask // recordingID → task
}

type recordTask struct {
	recording *model.Recording
	cmd       *exec.Cmd
	cancel    context.CancelFunc
	stderr    *bytes.Buffer // 捕获 FFmpeg 错误输出
}

func NewRecorderService(db *gorm.DB, cfg *pkg.Config) *RecorderService {
	return &RecorderService{
		db:    db,
		cfg:   cfg,
		tasks: make(map[uint]*recordTask),
	}
}

// Shutdown 停止所有正在进行的录像任务（优雅关闭时调用）。
func (s *RecorderService) Shutdown() {
	s.mu.Lock()
	tasks := make([]*recordTask, 0, len(s.tasks))
	for _, t := range s.tasks {
		tasks = append(tasks, t)
	}
	s.mu.Unlock()

	for _, t := range tasks {
		t.cancel()
		t.cmd.Wait()
	}
}

// ---------- Input DTOs ----------

type StartRecordingInput struct {
	CameraID    uint   `json:"camera_id" binding:"required"`
	CustomName  string `json:"custom_name"`
	Format      string `json:"format"`      // "mp4" / "webm" / "ts" (默认 "mp4")
	WithAudio   bool   `json:"with_audio"`  // 是否包含音频
	TriggerType string `json:"trigger_type"` // "api" / "manual" / "schedule"（默认 "api"）
}

type StopRecordingInput struct {
	RecordingID uint `json:"recording_id" binding:"required"`
}

// ---------- 录像控制 ----------

// StartRecording 开始录制指定摄像头的 RTSP 流。
func (s *RecorderService) StartRecording(in *StartRecordingInput) (*model.Recording, error) {
	// 查询摄像头
	var cam model.Camera
	if err := s.db.First(&cam, in.CameraID).Error; err != nil {
		return nil, fmt.Errorf("camera %d not found: %w", in.CameraID, err)
	}

	// 格式处理
	format := in.Format
	if format == "" {
		format = model.FormatMP4
	}
	// 验证格式
	switch format {
	case model.FormatMP4, model.FormatWebM, model.FormatTS:
		// OK
	default:
		return nil, fmt.Errorf("unsupported format: %s (use mp4, webm, or ts)", format)
	}

	now := time.Now()
	fileName := fmt.Sprintf("%d_%s.%s", cam.ID, now.Format("20060102_150405"), format)
	if in.CustomName != "" {
		fileName = fmt.Sprintf("%d_%s_%s.%s", cam.ID, now.Format("20060102_150405"), in.CustomName, format)
	}
	// 按摄像头 ID 分目录存放
	dir := filepath.Join(s.cfg.RecordingsDir, fmt.Sprintf("%d", cam.ID))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create recording dir: %w", err)
	}
	filePath := filepath.Join(dir, fileName)

	// 触发类型
	triggerType := in.TriggerType
	if triggerType == "" {
		triggerType = model.TriggerAPI
	}

	// 创建录像记录
	recording := &model.Recording{
		CameraID:    cam.ID,
		FilePath:    filePath,
		StartTime:   now,
		TriggerType: triggerType,
		Status:      model.RecordingStatusRecording,
		Format:      format,
		WithAudio:   in.WithAudio,
	}
	if err := s.db.Create(recording).Error; err != nil {
		return nil, err
	}

	// 构建 FFmpeg 参数
	args := s.buildRecordingArgs(cam.RTSPUrl, filePath, format, in.WithAudio)

	// 启动 FFmpeg 进程
	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, pkg.FFmpegBinPath(), args...)

	// 捕获 stderr 用于诊断
	var stderrBuf bytes.Buffer
	cmd.Stderr = &stderrBuf

	if err := cmd.Start(); err != nil {
		s.db.Model(recording).Update("status", model.RecordingStatusFailed)
		cancel()
		return nil, fmt.Errorf("start ffmpeg: %w", err)
	}

	task := &recordTask{
		recording: recording,
		cmd:       cmd,
		cancel:    cancel,
		stderr:    &stderrBuf,
	}
	s.mu.Lock()
	s.tasks[recording.ID] = task
	s.mu.Unlock()

	// 后台等待 FFmpeg 退出（异常中断时更新状态）
	go s.watchTask(recording.ID, task)

	return recording, nil
}

// buildRecordingArgs 根据格式和音频选项构建 FFmpeg 命令行参数。
//
// 重要：不同容器对编码的支持不同：
//   - MP4/TS: 支持 H.264/H.265 视频流拷贝，音频转码为 AAC（兼容所有摄像头音频）
//   - WebM:   只支持 VP8/VP9/AV1 视频 + Vorbis/Opus 音频，必须转码（不能流拷贝 H.264/H.265）
func (s *RecorderService) buildRecordingArgs(rtspURL, filePath, format string, withAudio bool) []string {
	args := []string{
		"-y",
		"-rtsp_transport", "tcp",
		"-i", rtspURL,
	}

	// 音频处理辅助
	audioArgs := func() {
		if withAudio {
			args = append(args, "-c:a", "aac") // 转码为 AAC，兼容性最好
		} else {
			args = append(args, "-an")
		}
	}

	switch format {
	case model.FormatMP4:
		args = append(args, "-c:v", "copy") // 视频流拷贝零转码
		audioArgs()
		args = append(args, "-movflags", "+frag_keyframe+empty_moov")
		args = append(args, "-f", "mp4")

	case model.FormatWebM:
		// WebM 必须转码视频为 VP9
		args = append(args, "-c:v", "libvpx-vp9", "-deadline", "realtime", "-cpu-used", "4")
		if withAudio {
			args = append(args, "-c:a", "libopus")
		} else {
			args = append(args, "-an")
		}
		args = append(args, "-f", "webm")

	case model.FormatTS:
		args = append(args, "-c:v", "copy")
		audioArgs()
		args = append(args, "-f", "mpegts")

	default:
		args = append(args, "-c:v", "copy")
		audioArgs()
		args = append(args, "-movflags", "+frag_keyframe+empty_moov")
		args = append(args, "-f", "mp4")
	}

	args = append(args, filePath)
	return args
}

// StopRecording 停止指定录像（幂等：任务不存在时直接更新 DB 状态）。
func (s *RecorderService) StopRecording(recordingID uint) error {
	s.mu.Lock()
	task, ok := s.tasks[recordingID]
	if ok {
		delete(s.tasks, recordingID)
	}
	s.mu.Unlock()

	if !ok {
		// 任务不在内存中（服务重启或已停止），直接更新 DB 记录为 completed
		var rec model.Recording
		if err := s.db.First(&rec, recordingID).Error; err != nil {
			return errors.New("recording not found")
		}
		// 幂等：已经是完成/失败状态，直接返回成功
		if rec.Status == model.RecordingStatusCompleted || rec.Status == model.RecordingStatusFailed {
			return nil
		}
		now := time.Now()
		duration := int(now.Sub(rec.StartTime).Seconds())
		var fileSize int64
		if info, err := os.Stat(rec.FilePath); err == nil {
			fileSize = info.Size()
		}
		s.db.Model(&rec).Updates(map[string]any{
			"end_time":  now,
			"file_size": fileSize,
			"duration":  duration,
			"status":    model.RecordingStatusCompleted,
		})
		return nil
	}

	// 发送 SIGINT 让 FFmpeg 优雅关闭（写入 moov atom）
	task.cancel()
	_ = task.cmd.Wait()

	// 获取文件大小
	var fileSize int64
	if info, err := os.Stat(task.recording.FilePath); err == nil {
		fileSize = info.Size()
	}
	now := time.Now()
	duration := int(now.Sub(task.recording.StartTime).Seconds())

	s.db.Model(task.recording).Updates(map[string]any{
		"end_time":  now,
		"file_size": fileSize,
		"duration":  duration,
		"status":    model.RecordingStatusCompleted,
	})

	return nil
}

// GetActiveRecordings 返回正在录制的记录列表。
func (s *RecorderService) GetActiveRecordings() ([]model.Recording, error) {
	var recs []model.Recording
	if err := s.db.Where("status = ?", model.RecordingStatusRecording).Find(&recs).Error; err != nil {
		return nil, err
	}
	return recs, nil
}

// watchTask 监控 FFmpeg 进程退出。
func (s *RecorderService) watchTask(recordingID uint, task *recordTask) {
	err := task.cmd.Wait()
	s.mu.Lock()
	_, stillActive := s.tasks[recordingID]
	if stillActive {
		delete(s.tasks, recordingID)
	}
	s.mu.Unlock()

	if stillActive {
		// FFmpeg 异常退出
		stderrInfo := ""
		if task.stderr != nil {
			last := task.stderr.String()
			if len(last) > 500 {
				last = last[len(last)-500:]
			}
			stderrInfo = "\n" + last
		}
		log.Printf("[recorder] recording %d ffmpeg exited unexpectedly: %v%s", recordingID, err, stderrInfo)
		var fileSize int64
		if info, statErr := os.Stat(task.recording.FilePath); statErr == nil {
			fileSize = info.Size()
		}
		s.db.Model(task.recording).Updates(map[string]any{
			"end_time":  time.Now(),
			"file_size": fileSize,
			"status":    model.RecordingStatusFailed,
		})
	}
}

// ---------- 录像查询 ----------

type RecordingQuery struct {
	CameraID  *uint
	StartTime *time.Time
	EndTime   *time.Time
	Status    *string
	Page      int
	PageSize  int
}

func (s *RecorderService) List(query RecordingQuery) ([]model.Recording, int64, error) {
	db := s.db.Model(&model.Recording{})
	if query.CameraID != nil {
		db = db.Where("camera_id = ?", *query.CameraID)
	}
	if query.StartTime != nil {
		db = db.Where("start_time >= ?", *query.StartTime)
	}
	if query.EndTime != nil {
		db = db.Where("start_time <= ?", *query.EndTime)
	}
	if query.Status != nil {
		db = db.Where("status = ?", *query.Status)
	}
	var total int64
	db.Count(&total)
	if query.Page <= 0 {
		query.Page = 1
	}
	if query.PageSize <= 0 {
		query.PageSize = 20
	}
	var recs []model.Recording
	if err := db.Order("start_time DESC").
		Offset((query.Page - 1) * query.PageSize).
		Limit(query.PageSize).
		Find(&recs).Error; err != nil {
		return nil, 0, err
	}
	return recs, total, nil
}

// GetRecording 返回指定录像记录。
func (s *RecorderService) GetRecording(id uint) (*model.Recording, error) {
	var rec model.Recording
	if err := s.db.First(&rec, id).Error; err != nil {
		return nil, err
	}
	return &rec, nil
}

// GetActiveRecordingByCamera 返回指定摄像头正在录制的记录（无则返回 nil）。
func (s *RecorderService) GetActiveRecordingByCamera(cameraID uint) (*model.Recording, error) {
	var rec model.Recording
	err := s.db.Where("camera_id = ? AND status = ?", cameraID, model.RecordingStatusRecording).First(&rec).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &rec, nil
}

// DeleteRecording 删除录像记录和对应的视频文件。
// 若录像正在录制，先停止再删除。
func (s *RecorderService) DeleteRecording(id uint) error {
	rec, err := s.GetRecording(id)
	if err != nil {
		return err
	}

	// 如果正在录制，先停止
	s.mu.Lock()
	task, isActive := s.tasks[id]
	if isActive {
		delete(s.tasks, id)
	}
	s.mu.Unlock()
	if isActive {
		task.cancel()
		_ = task.cmd.Wait()
	}

	// 删除视频文件（忽略不存在）
	if rec.FilePath != "" {
		if err := os.Remove(rec.FilePath); err != nil && !os.IsNotExist(err) {
			log.Printf("[recorder] delete file %s: %v", rec.FilePath, err)
		}
	}

	// 删除 DB 记录
	if err := s.db.Delete(&model.Recording{}, id).Error; err != nil {
		return err
	}
	return nil
}
