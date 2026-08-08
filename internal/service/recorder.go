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
	"strconv"
	"strings"
	"sync"
	"time"

	"CameraIO/internal/model"
	"CameraIO/internal/pkg"

	"io"

	"gorm.io/gorm"
)

// RecorderService 管理录像任务：Stream-Copy 将 RTSP 流直接写入 MP4。
type RecorderService struct {
	db      *gorm.DB
	cfg     *pkg.Config
	mu      sync.Mutex
	tasks   map[uint]*recordTask // recordingID → task
	ctx     context.Context
	cancel  context.CancelFunc
	streams *StreamService // 用于 GB28181 录像（从 NALU 流录制）
	events  *EventBus

	segmentCommand segmentCommandFactory
	probeAAC       func(context.Context, string) (bool, error)
	segmentProbe   segmentDurationProbe
}

type recordTask struct {
	recording    *model.Recording
	cmd          *exec.Cmd
	cancel       context.CancelFunc
	stderr       *bytes.Buffer // 捕获 FFmpeg 错误输出
	done         chan struct{} // FFmpeg 已确认退出，由 watcher 或 sweep 发布
	doneOnce     sync.Once
	forceStopped bool // 是否由内部 watchdog 强制停止（到期）
	segmenter    *segmentSupervisor
	stopping     bool
	finalizing   bool
	finalized    chan struct{}
	// GB28181 录像：NALU 订阅
	isGB28181 bool
	naluCh    <-chan NALU
	unsub     func()
	stdin     io.WriteCloser // FFmpeg 标准输入
}

func NewRecorderService(db *gorm.DB, cfg *pkg.Config) *RecorderService {
	ctx, cancel := context.WithCancel(context.Background())
	return &RecorderService{
		db:     db,
		cfg:    cfg,
		tasks:  make(map[uint]*recordTask),
		ctx:    ctx,
		cancel: cancel,
	}
}

// SetStreamService 注入流服务（用于 GB28181 录像）。
func (s *RecorderService) SetStreamService(st *StreamService) {
	s.streams = st
}

// SetEventBus 注入事件总线，用于推送录像状态变更。
func (s *RecorderService) SetEventBus(events *EventBus) {
	s.events = events
}

func (s *RecorderService) publishRecordingStatus(recordingID, cameraID uint, status string) {
	if s.events != nil {
		s.events.PublishRecordingStatus(recordingID, cameraID, status)
	}
}

// StartSweep 启动周期清扫：检测活跃录像的 FFmpeg 进程是否存活，
// 若已死但 watchTask 未清理，则强制 finalize。兜底机制。
func (s *RecorderService) StartSweep() {
	go s.sweepLoop()
}

func (s *RecorderService) sweepLoop() {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			s.sweepDeadProcesses()
		}
	}
}

// sweepDeadProcesses 检查所有活跃任务，finalize 已死亡进程的录像。
func (s *RecorderService) sweepDeadProcesses() {
	s.mu.Lock()
	var dead []*recordTask
	for _, t := range s.tasks {
		if t.segmenter != nil {
			select {
			case <-t.segmenter.done:
				dead = append(dead, t)
			default:
			}
			continue
		}
		if t.stopping {
			continue
		}
		if processExited(t.cmd) {
			dead = append(dead, t)
		}
	}
	s.mu.Unlock()

	for _, t := range dead {
		s.mu.Lock()
		current, ok := s.tasks[t.recording.ID]
		claimed := ok && current == t
		if t.segmenter != nil {
			claimed = claimed && !t.finalizing
			s.mu.Unlock()
			if claimed {
				s.finalizeSegmentTask(t.recording.ID, t)
			}
			continue
		}
		if claimed {
			delete(s.tasks, t.recording.ID)
		}
		s.mu.Unlock()
		if !claimed {
			continue
		}
		t.markDone()
		s.finalizeRecording(t.recording.ID, t)
		log.Printf("[recorder] sweep: recording %d ffmpeg not alive, finalized", t.recording.ID)
	}
}

// isProcessAlive 检查进程是否存活（跨平台）。
func isProcessAlive(cmd *exec.Cmd) bool {
	return !processExited(cmd)
}

func (t *recordTask) markDone() {
	if t.done == nil {
		return
	}
	t.doneOnce.Do(func() {
		close(t.done)
	})
}

func waitForTaskExit(task *recordTask, timeout time.Duration) bool {
	if task == nil {
		return true
	}
	if processExited(task.cmd) {
		task.markDone()
		return true
	}

	timer := time.NewTimer(timeout)
	defer timer.Stop()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-task.done:
			return true
		case <-ticker.C:
			if processExited(task.cmd) {
				task.markDone()
				return true
			}
		case <-timer.C:
			return false
		}
	}
}

// Shutdown 停止所有正在进行的录像任务（优雅关闭时调用）。
// 会更新数据库状态，避免重启后留下"录制中"的孤儿记录。
func (s *RecorderService) Shutdown() {
	s.cancel()
	s.mu.Lock()
	recordingIDs := make([]uint, 0, len(s.tasks))
	for recordingID := range s.tasks {
		recordingIDs = append(recordingIDs, recordingID)
	}
	s.mu.Unlock()

	for _, recordingID := range recordingIDs {
		if err := s.StopRecording(recordingID); err != nil {
			log.Printf("[recorder] shutdown recording %d: %v", recordingID, err)
		}
	}
}

// ---------- Input DTOs ----------

type StartRecordingInput struct {
	CameraID    uint   `json:"camera_id" binding:"required"`
	CustomName  string `json:"custom_name"`
	Format      string `json:"format"`       // "mp4" / "webm" / "ts" (默认 "mp4")
	WithAudio   bool   `json:"with_audio"`   // 是否包含音频
	TriggerType string `json:"trigger_type"` // "api" / "manual" / "schedule"（默认 "api"）
	MaxDuration int    `json:"max_duration"` // 最大录制时长（秒），0=不限；到期自动停止（录像器内部兜底）
	Bitrate     int    `json:"bitrate"`      // 视频码率（kbps），0=流拷贝（相机原码率，体积大）；>0=转码限码率（体积小，需 CPU）
}

type StopRecordingInput struct {
	RecordingID uint `json:"recording_id" binding:"required"`
}

// RecordingValidationError identifies caller-correctable recording options.
type RecordingValidationError struct {
	Message string
}

func (e *RecordingValidationError) Error() string { return e.Message }

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
		return nil, &RecordingValidationError{Message: fmt.Sprintf("unsupported format: %s (use mp4 or ts)", format)}
	}
	if format == model.FormatWebM {
		return nil, &RecordingValidationError{Message: "webm recordings are not supported by the resource-safe recorder; use mp4"}
	}
	if in.Bitrate > 0 {
		return nil, &RecordingValidationError{Message: "bitrate must be 0 for resource-safe stream-copy recording"}
	}

	now := time.Now().UTC()
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
	segmented := format == model.FormatMP4 && cam.AccessProtocol != model.ProtocolGB28181
	if segmented {
		recording.FilePath = dir
		recording.StorageMode = model.StorageModeSegmented
	}
	if err := s.db.Create(recording).Error; err != nil {
		return nil, err
	}

	var task *recordTask
	var err error

	if segmented {
		task, err = s.startSegmentedRecording(recording, cam)
	} else if cam.AccessProtocol == model.ProtocolGB28181 {
		// GB28181：从 Stream 的 NALU 流录制（与预览同一条链路）
		task, err = s.startGB28181Recording(recording, cam, format, in.WithAudio, in.Bitrate)
	} else {
		// RTSP/本地：FFmpeg 直接拉流录制
		args := s.buildRecordingArgs(cam.RTSPUrl, filePath, format, in.WithAudio, in.Bitrate)
		task, err = s.startFFmpegRecording(recording, args)
	}
	if err != nil {
		var updateErr, cleanupErr error
		if dbErr := s.db.Model(recording).Update("status", model.RecordingStatusFailed).Error; dbErr != nil {
			updateErr = fmt.Errorf("mark recording start failed: %w", dbErr)
		}
		if segmented {
			if removeErr := removeEmptyRecordingSessionDir(recording.FilePath); removeErr != nil {
				cleanupErr = fmt.Errorf("remove empty recording session dir: %w", removeErr)
			}
		}
		return nil, errors.Join(err, updateErr, cleanupErr)
	}

	s.mu.Lock()
	s.tasks[recording.ID] = task
	s.mu.Unlock()
	s.publishRecordingStatus(recording.ID, recording.CameraID, model.RecordingStatusRecording)

	// 录像器内部 watchdog：达到最大时长强制停止（不依赖调度器）
	if in.MaxDuration > 0 {
		go s.maxDurationWatchdog(recording.ID, task, in.MaxDuration)
	}

	// 后台等待 FFmpeg 退出（异常中断时更新状态）。分段录像的 supervisor
	// 是 cmd.Wait 的唯一所有者，旧任务继续由 watchTask 所有。
	if task.segmenter != nil {
		go s.watchSegmentTask(recording.ID, task)
	} else {
		go s.watchTask(recording.ID, task)
	}

	return recording, nil
}

func (s *RecorderService) startSegmentedRecording(recording *model.Recording, cam model.Camera) (*recordTask, error) {
	sessionDir := filepath.Join(s.cfg.RecordingsDir, strconv.FormatUint(uint64(cam.ID), 10), strconv.FormatUint(uint64(recording.ID), 10))
	recording.FilePath = sessionDir
	if err := s.db.Model(recording).Update("file_path", sessionDir).Error; err != nil {
		return nil, fmt.Errorf("store recording session path: %w", err)
	}
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		return nil, fmt.Errorf("create recording session dir: %w", err)
	}

	withAAC := false
	if recording.WithAudio {
		probe := s.probeAAC
		if probe == nil {
			probe = probeRTSPAAC
		}
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		var err error
		withAAC, err = probe(ctx, cam.RTSPUrl)
		cancel()
		if err != nil {
			log.Printf("[recorder] camera %d AAC probe failed; recording video only: %v", cam.ID, err)
			withAAC = false
		}
	}
	segmentSeconds := s.cfg.RecordingSegmentSeconds
	if segmentSeconds <= 0 {
		segmentSeconds = 300
	}
	outputPattern := filepath.Join(sessionDir, "%Y%m%dT%H%M%SZ.mp4")
	supervisor := &segmentSupervisor{
		db:            s.db,
		recording:     recording,
		sessionDir:    sessionDir,
		args:          buildSegmentRecordingArgs(cam.RTSPUrl, outputPattern, segmentSeconds, withAAC),
		newCommand:    s.segmentCommand,
		probeDuration: s.segmentProbe,
	}
	if err := supervisor.start(); err != nil {
		return nil, err
	}
	return &recordTask{
		recording: recording,
		cmd:       supervisor.cmd,
		stderr:    &supervisor.stderr,
		done:      supervisor.done,
		segmenter: supervisor,
		finalized: make(chan struct{}),
	}, nil
}

func removeEmptyRecordingSessionDir(path string) error {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return nil
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return err
	}
	if len(entries) != 0 {
		return nil
	}
	return os.Remove(path)
}

// startFFmpegRecording 启动 FFmpeg 直接拉流录制（RTSP/本地）。
func (s *RecorderService) startFFmpegRecording(recording *model.Recording, args []string) (*recordTask, error) {
	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, pkg.FFmpegBinPath(), args...)

	var stderrBuf bytes.Buffer
	cmd.Stderr = &stderrBuf

	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("start ffmpeg: %w", err)
	}

	return &recordTask{
		recording: recording,
		cmd:       cmd,
		cancel:    cancel,
		stderr:    &stderrBuf,
		done:      make(chan struct{}),
	}, nil
}

// startGB28181Recording 从 Stream 的 NALU 流录制（GB28181 无 RTSP）。
// 启动流 → 订阅 NALU → 喂给 FFmpeg（H.264 流拷贝成 MP4）。
func (s *RecorderService) startGB28181Recording(recording *model.Recording, cam model.Camera, format string, withAudio bool, bitrate int) (*recordTask, error) {
	if s.streams == nil {
		return nil, fmt.Errorf("stream service not available for GB28181 recording")
	}
	// 确保流已启动（触发 INVITE + RTP 接收）
	if err := s.streams.StartStream(cam.ID); err != nil {
		return nil, fmt.Errorf("start stream: %w", err)
	}
	st := s.streams.GetStream(cam.ID)
	if st == nil {
		return nil, fmt.Errorf("stream not started for camera %d", cam.ID)
	}

	// 启动 FFmpeg：读 H.264 流，写成 MP4
	ctx, cancel := context.WithCancel(context.Background())
	ffArgs := []string{
		"-f", "h264",
		"-i", "pipe:0",
		"-c:v", "copy",
	}
	if !withAudio {
		ffArgs = append(ffArgs, "-an")
	} else {
		ffArgs = append(ffArgs, "-c:a", "aac")
	}
	if bitrate > 0 {
		ffArgs = append(ffArgs,
			"-c:v", "libx264",
			"-b:v", fmt.Sprintf("%dk", bitrate),
			"-preset", "veryfast",
		)
	}
	switch format {
	case model.FormatMP4:
		ffArgs = append(ffArgs, "-movflags", "+frag_keyframe+empty_moov", "-f", "mp4")
	case model.FormatWebM:
		ffArgs = append(ffArgs, "-c:v", "libvpx-vp9", "-deadline", "realtime", "-cpu-used", "4")
		if bitrate > 0 {
			ffArgs = append(ffArgs, "-b:v", fmt.Sprintf("%dk", bitrate))
		}
		ffArgs = append(ffArgs, "-f", "webm")
	case model.FormatTS:
		ffArgs = append(ffArgs, "-f", "mpegts")
	}
	ffArgs = append(ffArgs, recording.FilePath)

	cmd := exec.CommandContext(ctx, pkg.FFmpegBinPath(), ffArgs...)
	var stderrBuf bytes.Buffer
	cmd.Stderr = &stderrBuf

	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("stdin pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("start ffmpeg: %w", err)
	}

	// 订阅 NALU 流，喂给 FFmpeg
	naluCh, unsub := st.Subscribe()
	task := &recordTask{
		recording: recording,
		cmd:       cmd,
		cancel:    cancel,
		stderr:    &stderrBuf,
		done:      make(chan struct{}),
		isGB28181: true,
		naluCh:    naluCh,
		unsub:     unsub,
		stdin:     stdin,
	}

	// 喂 NALU（带 start code）到 FFmpeg stdin
	startCode := []byte{0, 0, 0, 1}
	go func() {
		defer stdin.Close()
		for {
			select {
			case <-ctx.Done():
				return
			case nalu, ok := <-naluCh:
				if !ok {
					return
				}
				if len(nalu.Data) == 0 {
					continue
				}
				// 拼 start code + NALU
				buf := make([]byte, 0, len(startCode)+len(nalu.Data))
				buf = append(buf, startCode...)
				buf = append(buf, nalu.Data...)
				if _, err := stdin.Write(buf); err != nil {
					return
				}
			}
		}
	}()

	return task, nil
}

// maxDurationWatchdog 在达到最大录制时长后强制停止录像。
// 即使调度器/时间窗口逻辑失效，录像也会在到期时被停止。
func (s *RecorderService) maxDurationWatchdog(recordingID uint, task *recordTask, maxSeconds int) {
	time.Sleep(time.Duration(maxSeconds) * time.Second)

	s.mu.Lock()
	_, stillActive := s.tasks[recordingID]
	if stillActive {
		task.forceStopped = true // 标记为"到期停止"，让 watchTask 标记 completed
	}
	s.mu.Unlock()

	if !stillActive {
		return // 录像已通过其他方式停止
	}
	log.Printf("[recorder] recording %d reached max duration (%ds), force stopping", recordingID, maxSeconds)
	if task.segmenter != nil {
		if err := s.StopRecording(recordingID); err != nil {
			log.Printf("[recorder] stop segmented recording %d at max duration: %v", recordingID, err)
		}
		return
	}
	task.cancel() // 取消 context → CommandContext 杀掉 FFmpeg
}

// buildRecordingArgs 根据格式和音频选项构建 FFmpeg 命令行参数。
//
// 重要：不同容器对编码的支持不同：
//   - MP4/TS: 支持 H.264/H.265 视频流拷贝，音频转码为 AAC（兼容所有摄像头音频）
//   - WebM:   只支持 VP8/VP9/AV1 视频 + Vorbis/Opus 音频，必须转码（不能流拷贝 H.264/H.265）
func (s *RecorderService) buildRecordingArgs(rtspURL, filePath, format string, withAudio bool, bitrate int) []string {
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

	// 视频编码辅助：bitrate>0 时转码限码率（控制文件大小），否则流拷贝（相机原码率）
	videoArgs := func() {
		if bitrate > 0 {
			// 转码 H.264 并限码率
			args = append(args,
				"-c:v", "libx264",
				"-b:v", fmt.Sprintf("%dk", bitrate),
				"-maxrate", fmt.Sprintf("%dk", bitrate*12/10),
				"-bufsize", fmt.Sprintf("%dk", bitrate*2),
				"-preset", "veryfast",
				"-profile:v", "main",
			)
		} else {
			args = append(args, "-c:v", "copy") // 流拷贝零转码
		}
	}

	switch format {
	case model.FormatMP4:
		videoArgs()
		audioArgs()
		args = append(args, "-movflags", "+frag_keyframe+empty_moov")
		args = append(args, "-f", "mp4")

	case model.FormatWebM:
		// WebM 必须转码视频为 VP9
		args = append(args, "-c:v", "libvpx-vp9", "-deadline", "realtime", "-cpu-used", "4")
		if bitrate > 0 {
			args = append(args, "-b:v", fmt.Sprintf("%dk", bitrate))
		}
		if withAudio {
			args = append(args, "-c:a", "libopus")
		} else {
			args = append(args, "-an")
		}
		args = append(args, "-f", "webm")

	case model.FormatTS:
		videoArgs()
		audioArgs()
		args = append(args, "-f", "mpegts")

	default:
		videoArgs()
		audioArgs()
		args = append(args, "-movflags", "+frag_keyframe+empty_moov")
		args = append(args, "-f", "mp4")
	}

	args = append(args, filePath)
	return args
}

// StopRecording 停止指定录像（幂等：任务不存在时直接更新 DB 状态）。
func (s *RecorderService) StopRecording(recordingID uint) error {
	ctx, cancel := context.WithTimeout(context.Background(), segmentStopGracePeriod+2*time.Second)
	defer cancel()
	return s.stopRecording(ctx, recordingID)
}

func (s *RecorderService) stopRecording(ctx context.Context, recordingID uint) error {
	s.mu.Lock()
	task, ok := s.tasks[recordingID]
	if ok {
		if task.segmenter != nil {
			task.stopping = true
			if task.finalized == nil {
				task.finalized = make(chan struct{})
			}
		} else {
			delete(s.tasks, recordingID)
		}
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
		if rec.StorageMode == model.StorageModeSegmented {
			return s.recoverSegmentedRecording(&rec)
		}
		now := time.Now()
		duration := int(now.Sub(rec.StartTime).Seconds())
		var fileSize int64
		if info, err := os.Stat(rec.FilePath); err == nil {
			fileSize = info.Size()
		}
		if err := s.db.Model(&rec).Updates(map[string]any{
			"end_time":  now,
			"file_size": fileSize,
			"duration":  duration,
			"status":    model.RecordingStatusCompleted,
		}).Error; err != nil {
			return fmt.Errorf("complete recording: %w", err)
		}
		s.publishRecordingStatus(rec.ID, rec.CameraID, model.RecordingStatusCompleted)
		return nil
	}
	if task.segmenter != nil {
		stopErr := task.segmenter.stop(ctx)
		if stopErr != nil {
			return stopErr
		}
		select {
		case <-task.finalized:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	// GB28181：先关闭 FFmpeg 标准输入（EOF 让 FFmpeg 完成 MP4 收尾），再取消
	if task.isGB28181 {
		if task.unsub != nil {
			task.unsub()
		}
		if task.stdin != nil {
			_ = task.stdin.Close() // EOF → FFmpeg 正常结束
		}
		task.cancel()
	} else {
		// RTSP：取消 context 让 FFmpeg 退出
		task.cancel()
	}

	// watchTask 是 cmd.Wait 的唯一调用方；停止请求只等待它发布完成信号。
	if !waitForTaskExit(task, 5*time.Second) {
		// 超时仍没退出，强制杀死
		if task.cmd.Process != nil {
			_ = task.cmd.Process.Kill()
		}
		if !waitForTaskExit(task, 2*time.Second) {
			return fmt.Errorf("stop ffmpeg: process did not exit after cancellation")
		}
	}

	// 获取文件大小
	var fileSize int64
	if info, err := os.Stat(task.recording.FilePath); err == nil {
		fileSize = info.Size()
	}
	now := time.Now()
	duration := int(now.Sub(task.recording.StartTime).Seconds())

	if err := s.db.Model(task.recording).Updates(map[string]any{
		"end_time":  now,
		"file_size": fileSize,
		"duration":  duration,
		"status":    model.RecordingStatusCompleted,
	}).Error; err != nil {
		return fmt.Errorf("complete recording: %w", err)
	}
	s.publishRecordingStatus(task.recording.ID, task.recording.CameraID, model.RecordingStatusCompleted)

	return nil
}

func (s *RecorderService) watchSegmentTask(recordingID uint, task *recordTask) {
	<-task.segmenter.done
	s.finalizeSegmentTask(recordingID, task)
}

func (s *RecorderService) finalizeSegmentTask(recordingID uint, task *recordTask) {
	s.mu.Lock()
	current, active := s.tasks[recordingID]
	if !active || current != task || task.finalizing {
		s.mu.Unlock()
		return
	}
	task.finalizing = true
	if task.finalized == nil {
		task.finalized = make(chan struct{})
	}
	s.mu.Unlock()

	if err := task.segmenter.scanCompleted(true); err != nil {
		log.Printf("[recorder] recording %d final segment scan after FFmpeg exit: %v", recordingID, err)
		s.mu.Lock()
		task.finalizing = false
		s.mu.Unlock()
		return
	}
	if err := s.finalizeSegmentedRecording(recordingID, task.recording.CameraID); err != nil {
		log.Printf("[recorder] recording %d final status update: %v", recordingID, err)
		s.mu.Lock()
		task.finalizing = false
		s.mu.Unlock()
		return
	}
	s.mu.Lock()
	if current, ok := s.tasks[recordingID]; ok && current == task {
		delete(s.tasks, recordingID)
	}
	close(task.finalized)
	s.mu.Unlock()
}

func (s *RecorderService) finalizeSegmentedRecording(recordingID, cameraID uint) error {
	var rec model.Recording
	if err := s.db.First(&rec, recordingID).Error; err != nil {
		return fmt.Errorf("load segmented recording: %w", err)
	}
	var segmentCount int64
	if err := s.db.Model(&model.RecordingSegment{}).Where("recording_id = ? AND file_size > 0", recordingID).Count(&segmentCount).Error; err != nil {
		return fmt.Errorf("count recording segments: %w", err)
	}
	status := model.RecordingStatusCompleted
	updates := map[string]any{"status": status}
	if segmentCount == 0 {
		status = model.RecordingStatusFailed
		updates["status"] = status
		updates["file_size"] = 0
		updates["duration"] = 0
		now := time.Now().UTC()
		updates["end_time"] = now
	}
	if err := s.db.Model(&model.Recording{}).Where("id = ?", recordingID).Updates(updates).Error; err != nil {
		return fmt.Errorf("update segmented recording: %w", err)
	}
	s.publishRecordingStatus(recordingID, cameraID, status)
	return nil
}

func (s *RecorderService) recoverSegmentedRecording(recording *model.Recording) error {
	supervisor := &segmentSupervisor{
		db:            s.db,
		recording:     recording,
		sessionDir:    recording.FilePath,
		probeDuration: s.segmentProbe,
	}
	if err := supervisor.scanCompleted(true); err != nil {
		return fmt.Errorf("scan recovered recording segments: %w", err)
	}
	return s.finalizeSegmentedRecording(recording.ID, recording.CameraID)
}

// ReconcileOrphaned 清理孤儿录像记录：状态为 recording/failed 但 FFmpeg 进程已不存在。
// 服务启动时调用。文件有效则标记 completed（可下载查看），否则标记 failed。
func (s *RecorderService) ReconcileOrphaned() {
	var recs []model.Recording
	if err := s.db.Where("status IN ?", []string{model.RecordingStatusRecording, model.RecordingStatusFailed}).Find(&recs).Error; err != nil {
		log.Printf("[recorder] reconcile orphaned: %v", err)
		return
	}

	for _, rec := range recs {
		// 检查是否有活跃任务（内存中）
		s.mu.Lock()
		_, active := s.tasks[rec.ID]
		s.mu.Unlock()
		if active {
			continue // 仍在录制
		}
		if rec.StorageMode == model.StorageModeSegmented {
			if err := s.recoverSegmentedRecording(&rec); err != nil {
				log.Printf("[recorder] recover orphaned segmented recording %d: %v", rec.ID, err)
			}
			continue
		}

		// 检查文件是否有效
		var fileSize int64
		if info, err := os.Stat(rec.FilePath); err == nil {
			fileSize = info.Size()
		}

		if fileSize > 0 {
			// 文件有效 → 标记 completed，用户可下载/查看
			s.db.Model(&rec).Updates(map[string]any{
				"end_time":  time.Now(),
				"file_size": fileSize,
				"duration":  probeVideoDuration(rec.FilePath),
				"status":    model.RecordingStatusCompleted,
			})
			log.Printf("[recorder] orphaned recording %d recovered as completed (file valid, %d bytes)", rec.ID, fileSize)
		} else if rec.Status == model.RecordingStatusRecording {
			// 无有效文件且曾是 recording → failed
			s.db.Model(&rec).Updates(map[string]any{
				"end_time": time.Now(),
				"status":   model.RecordingStatusFailed,
			})
			log.Printf("[recorder] orphaned recording %d marked as failed (no valid file)", rec.ID)
		}
	}
}

// probeVideoDuration 用 ffprobe 读取视频文件的实际时长（秒）。
func probeVideoDuration(filePath string) int {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, pkg.FFprobeBinPath(),
		"-v", "error",
		"-show_entries", "format=duration",
		"-of", "default=noprint_wrappers=1:nokey=1",
		filePath,
	)
	out, err := cmd.Output()
	if err != nil {
		return 0
	}
	f, err := strconv.ParseFloat(strings.TrimSpace(string(out)), 64)
	if err != nil || f <= 0 {
		return 0
	}
	return int(f)
}

// GetActiveRecordings 返回正在录制的记录列表。
func (s *RecorderService) GetActiveRecordings() ([]model.Recording, error) {
	var recs []model.Recording
	if err := s.db.Where("status = ?", model.RecordingStatusRecording).Find(&recs).Error; err != nil {
		return nil, err
	}
	return recs, nil
}

// watchTask 监控 FFmpeg 进程退出并更新状态。
func (s *RecorderService) watchTask(recordingID uint, task *recordTask) {
	log.Printf("[recorder] watchTask started for recording %d", recordingID)
	err := task.cmd.Wait()
	task.markDone()
	log.Printf("[recorder] watchTask: recording %d ffmpeg exited, wait returned: %v", recordingID, err)
	s.mu.Lock()
	_, stillActive := s.tasks[recordingID]
	if stillActive {
		delete(s.tasks, recordingID)
	}
	forceStopped := task.forceStopped
	s.mu.Unlock()

	if !stillActive {
		return // 已被其他路径处理
	}

	// FFmpeg 退出，记录状态。文件有效 → completed（可下载），无文件 → failed
	stderrInfo := ""
	if task.stderr != nil {
		last := task.stderr.String()
		if len(last) > 500 {
			last = last[len(last)-500:]
		}
		stderrInfo = "\n" + last
	}
	if forceStopped {
		log.Printf("[recorder] recording %d stopped by watchdog (completed)", recordingID)
	} else {
		log.Printf("[recorder] recording %d ffmpeg exited: %v%s", recordingID, err, stderrInfo)
	}
	s.finalizeRecording(recordingID, task)
}

// finalizeRecording 根据文件是否有效更新录像状态：
// 有文件 → completed（可下载/查看），无文件 → failed。
func (s *RecorderService) finalizeRecording(recordingID uint, task *recordTask) {
	var fileSize int64
	if info, err := os.Stat(task.recording.FilePath); err == nil {
		fileSize = info.Size()
	}
	now := time.Now()

	if fileSize > 0 {
		// 文件有效 → completed
		if err := s.db.Model(&model.Recording{}).Where("id = ?", recordingID).Updates(map[string]any{
			"end_time":  now,
			"file_size": fileSize,
			"duration":  probeVideoDuration(task.recording.FilePath),
			"status":    model.RecordingStatusCompleted,
		}).Error; err != nil {
			log.Printf("[recorder] recording %d finalize to completed FAILED: %v", recordingID, err)
		} else {
			log.Printf("[recorder] recording %d finalized as completed (%d bytes)", recordingID, fileSize)
			s.publishRecordingStatus(recordingID, task.recording.CameraID, model.RecordingStatusCompleted)
		}
	} else {
		// 无文件 → failed
		if err := s.db.Model(&model.Recording{}).Where("id = ?", recordingID).Updates(map[string]any{
			"end_time": now,
			"duration": int(now.Sub(task.recording.StartTime).Seconds()),
			"status":   model.RecordingStatusFailed,
		}).Error; err != nil {
			log.Printf("[recorder] recording %d finalize to failed FAILED: %v", recordingID, err)
		} else {
			log.Printf("[recorder] recording %d finalized as failed (no valid file)", recordingID)
			s.publishRecordingStatus(recordingID, task.recording.CameraID, model.RecordingStatusFailed)
		}
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
		db = db.Where("COALESCE(end_time, CURRENT_TIMESTAMP) > ?", query.StartTime.UTC())
	}
	if query.EndTime != nil {
		db = db.Where("start_time < ?", query.EndTime.UTC())
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

	// 如果正在录制，复用统一停止流程；watchTask 仍是 cmd.Wait 的唯一调用方。
	s.mu.Lock()
	_, isActive := s.tasks[id]
	s.mu.Unlock()
	if isActive {
		if err := s.StopRecording(id); err != nil {
			return err
		}
	}
	if rec.StorageMode == model.StorageModeSegmented {
		var segments []model.RecordingSegment
		if err := s.db.Where("recording_id = ?", id).Find(&segments).Error; err != nil {
			return err
		}
		for _, segment := range segments {
			if err := os.Remove(segment.FilePath); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("delete segment file %s: %w", segment.FilePath, err)
			}
		}
		if rec.FilePath != "" {
			entries, err := os.ReadDir(rec.FilePath)
			if err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("read recording session dir %s: %w", rec.FilePath, err)
			}
			for _, entry := range entries {
				if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".mp4") {
					continue
				}
				path := filepath.Join(rec.FilePath, entry.Name())
				if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
					return fmt.Errorf("delete unindexed segment file %s: %w", path, err)
				}
			}
			if err := os.Remove(rec.FilePath); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("delete recording session dir %s: %w", rec.FilePath, err)
			}
		}
		if err := s.db.Transaction(func(tx *gorm.DB) error {
			if err := tx.Where("recording_id = ?", id).Delete(&model.RecordingSegment{}).Error; err != nil {
				return err
			}
			return tx.Delete(&model.Recording{}, id).Error
		}); err != nil {
			return err
		}
		return nil
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
