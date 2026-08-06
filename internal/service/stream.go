package service

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"

	"CameraIO/internal/model"
	"CameraIO/internal/pkg"

	"gorm.io/gorm"
)

// StreamService 管理所有摄像头的实时流（RTSP 拉取 → WebRTC/MJPEG 分发）。
type StreamService struct {
	db      *gorm.DB
	mu      sync.RWMutex
	streams map[uint]*Stream // cameraID → Stream
	gb28181 *GB28181Service  // 用于 GB28181 摄像头 SIP INVITE
}

func NewStreamService(db *gorm.DB) *StreamService {
	return &StreamService{
		db:      db,
		streams: make(map[uint]*Stream),
	}
}

// SetGB28181 注入 GB28181 服务（用于 GB28181 摄像头的点播）。
func (s *StreamService) SetGB28181(g *GB28181Service) {
	s.gb28181 = g
}

// ---------- Stream ----------

// Stream 代表一路摄像头的实时流：一个 FFmpeg 拉流进程 + 多个消费者。
type Stream struct {
	CameraID uint
	Camera   *model.Camera

	ctx    context.Context
	cancel context.CancelFunc

	// 编码格式: "h264" 或 "hevc"
	Codec string

	// NALU 广播：拉流器写入，消费者读取。
	mu       sync.RWMutex
	naluSubs map[int]chan NALU
	nextSub  int

	// 最新 SPS/PPS（给新 WebRTC 客户端用）
	sps []byte
	pps []byte
	vps []byte // HEVC Video Parameter Set

	// 最新 JPEG 帧（给 MJPEG 客户端用）
	jpegMu    sync.RWMutex
	latestJPG []byte

	// JPEG 提取节流（避免每个 IDR 帧都启动 FFmpeg）
	extractMu     sync.Mutex
	lastExtractAt time.Time
	extracting    bool

	// 持续 MJPEG 转码器（10+ FPS）
	mjpegDone chan struct{}

	done chan struct{}
}

// NALU 代表一帧 H.264 NAL 单元。
type NALU struct {
	Type  byte // NALU type (1=IDR, 5=non-IDR, 7=SPS, 8=PPS, etc.)
	Data  []byte
	IsIDR bool
	Pts   time.Duration
}

// ---------- StreamService API ----------

// StartStream 开始拉取指定摄像头的 RTSP 流。
func (s *StreamService) StartStream(cameraID uint) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.streams[cameraID]; ok {
		return nil // 已在运行
	}
	// 查询摄像头信息
	var cam model.Camera
	if err := s.db.First(&cam, cameraID).Error; err != nil {
		return fmt.Errorf("camera %d not found: %w", cameraID, err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	stream := &Stream{
		CameraID:  cameraID,
		Camera:    &cam,
		ctx:       ctx,
		cancel:    cancel,
		Codec:     "h264",
		naluSubs:  make(map[int]chan NALU),
		mjpegDone: make(chan struct{}),
		done:      make(chan struct{}),
	}
	s.streams[cameraID] = stream

	// GB28181: 通过 SIP INVITE 获取 RTP 流，不启动 FFmpeg 拉流/转码
	if cam.AccessProtocol == model.ProtocolGB28181 {
		// 停止时关闭 done（RTP 接收器由 GB28181 服务管理）
		go func() {
			<-ctx.Done()
			close(stream.done)
		}()
		if s.gb28181 != nil {
			channelID := cam.ChannelID
			if channelID == "" {
				channelID = cam.DeviceID
			}
			go func() {
				_, err := s.gb28181.InviteStream(ctx, channelID)
				if err != nil {
					log.Printf("[stream] camera %d gb28181 invite failed: %v", cameraID, err)
				}
			}()
		}
		return nil
	}

	go s.runPuller(stream)
	// 持续 MJPEG 转码器（10+ FPS），供预览使用
	go s.runMJPEGTranscoder(stream)
	return nil
}

// Shutdown 停止所有正在拉流的摄像头（优雅关闭时调用）。
func (s *StreamService) Shutdown() {
	s.mu.Lock()
	streams := make([]*Stream, 0, len(s.streams))
	for _, st := range s.streams {
		streams = append(streams, st)
	}
	s.streams = make(map[uint]*Stream) // 清空 map
	s.mu.Unlock()

	for _, st := range streams {
		st.cancel()
	}
}

// StopStream 停止指定摄像头的拉流。
func (s *StreamService) StopStream(cameraID uint) {
	s.mu.Lock()
	stream, ok := s.streams[cameraID]
	if ok {
		delete(s.streams, cameraID)
	}
	s.mu.Unlock()

	if ok {
		stream.cancel()
		<-stream.done
	}
}

// GetStream 返回指定摄像头的流（如果存在）。
func (s *StreamService) GetStream(cameraID uint) *Stream {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.streams[cameraID]
}

// Subscribe 订阅 NALU 流，返回 channel 和取消函数。
func (st *Stream) Subscribe() (<-chan NALU, func()) {
	st.mu.Lock()
	id := st.nextSub
	st.nextSub++
	ch := make(chan NALU, 64)
	st.naluSubs[id] = ch
	st.mu.Unlock()

	// 发送最新的 SPS/PPS（让新客户端能立即解码）
	if st.Codec == "hevc" {
		// HEVC: VPS + SPS + PPS
		if st.vps != nil {
			ch <- NALU{Type: 32, Data: st.vps}
		}
		if st.sps != nil {
			ch <- NALU{Type: 33, Data: st.sps}
		}
		if st.pps != nil {
			ch <- NALU{Type: 34, Data: st.pps}
		}
	} else {
		if st.sps != nil {
			ch <- NALU{Type: 7, Data: st.sps}
		}
		if st.pps != nil {
			ch <- NALU{Type: 8, Data: st.pps}
		}
	}

	return ch, func() {
		st.mu.Lock()
		delete(st.naluSubs, id)
		st.mu.Unlock()
	}
}

// GetLatestJPEG 返回最新的 JPEG 帧（用于 MJPEG 流的第一帧）。
func (st *Stream) GetLatestJPEG() []byte {
	st.jpegMu.RLock()
	defer st.jpegMu.RUnlock()
	if st.latestJPG == nil {
		return nil
	}
	cp := make([]byte, len(st.latestJPG))
	copy(cp, st.latestJPG)
	return cp
}

// Done 返回流停止的 channel（供外部判断流是否已停止）。
func (st *Stream) Done() <-chan struct{} {
	return st.ctx.Done()
}

// ---------- FFmpeg 拉流器 ----------

func (s *StreamService) runPuller(st *Stream) {
	defer close(st.done)

	for {
		select {
		case <-st.ctx.Done():
			return
		default:
		}

		err := s.pullRTSP(st)
		if st.ctx.Err() != nil {
			return // context 已取消，不重启
		}
		log.Printf("[stream] camera %d puller error: %v, restarting in 2s...", st.CameraID, err)
		select {
		case <-st.ctx.Done():
			return
		case <-time.After(2 * time.Second):
		}
	}
}

func (s *StreamService) pullRTSP(st *Stream) error {
	// 根据接入协议构造 ffmpeg 命令
	args := s.buildFFmpegArgs(st.Camera)
	if len(args) == 0 {
		return fmt.Errorf("unsupported access protocol: %s", st.Camera.AccessProtocol)
	}

	// 先检测视频编码格式（H.264 vs H.265）
	videoCodec := s.detectVideoCodec(st.ctx, st.Camera.RTSPUrl)
	st.Codec = videoCodec

	// 如果是 HEVC，修改输出格式
	if videoCodec == "hevc" {
		args = replaceOutputFormat(args, "hevc")
		log.Printf("[stream] camera %d: detected H.265 (HEVC), using hevc output", st.CameraID)
	}

	cmd := exec.CommandContext(st.ctx, pkg.FFmpegBinPath(), args...)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("stdout pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("ffmpeg start: %w", err)
	}

	// context 取消时立即杀死 FFmpeg 进程（避免优雅关闭时卡住）
	go func() {
		<-st.ctx.Done()
		cmd.Process.Kill()
	}()
	defer func() {
		cmd.Process.Kill()
		cmd.Wait()
	}()

	// 更新摄像头状态为在线
	s.db.Model(&model.Camera{}).Where("id = ?", st.CameraID).Update("status", model.CameraStatusOnline)
	defer s.db.Model(&model.Camera{}).Where("id = ?", st.CameraID).Update("status", model.CameraStatusOffline)

	// 解析 raw H.264/HEVC Annex B 流
	return s.parseH264Stream(st, stdout)
}

// detectVideoCodec 通过 ffprobe 检测 RTSP 流的视频编码格式。
func (s *StreamService) detectVideoCodec(ctx context.Context, rtspURL string) string {
	probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(probeCtx, pkg.FFprobeBinPath(),
		"-rtsp_transport", "tcp",
		"-timeout", "5000000",
		"-select_streams", "v:0",
		"-show_entries", "stream=codec_name",
		"-of", "csv=p=0:nk=1",
		"-v", "quiet",
		rtspURL,
	)
	out, err := cmd.Output()
	if err != nil {
		return "h264" // 默认假设 H.264
	}
	codec := strings.TrimSpace(string(out))
	if codec == "hevc" || codec == "h265" {
		return "hevc"
	}
	return "h264"
}

// replaceOutputFormat 将 FFmpeg 参数中的 -f h264 替换为 -f hevc。
func replaceOutputFormat(args []string, newFormat string) []string {
	result := make([]string, len(args))
	copy(result, args)
	for i := 0; i < len(result)-1; i++ {
		if result[i] == "-f" && result[i+1] == "h264" {
			result[i+1] = newFormat
			break
		}
	}
	return result
}

// pullRTSPHEVC 已废弃：现在由 pullRTSP 通过 detectVideoCodec 自动处理。
func (s *StreamService) pullRTSPHEVC(st *Stream) error {
	return s.pullRTSP(st)
}

// parseH264Stream 从 raw H.264/H.265 Annex B 流中解析 NALU 并广播。
// 正确处理跨数据块边界的 NAL 单元，不丢失字节。
func (s *StreamService) parseH264Stream(st *Stream, r io.Reader) error {
	reader := bufio.NewReaderSize(r, 64*1024)

	// 累积缓冲区：存放尚未完成的 NAL 数据（含未检测到下一 start code 的部分）
	var pending []byte

	for {
		chunk := make([]byte, 64*1024)
		n, err := reader.Read(chunk)
		if n > 0 {
			pending = append(pending, chunk[:n]...)
		}

		// 从 pending 中提取所有完整的 NAL units（两个 start code 之间的数据）
		consumed := 0
		for {
			startIdx := indexStartCode(pending, consumed)
			if startIdx < 0 {
				break // 没有 start code，等待更多数据
			}
			// 查找下一个 start code
			nextStart := indexStartCode(pending, startIdx+4)
			if nextStart < 0 {
				break // 只有一个 start code，等待下一块数据
			}
			// startIdx..nextStart 之间是一个完整的 NAL unit
			nalStart := startIdx + startCodeLen(pending, startIdx)
			nalData := pending[nalStart:nextStart]
			if len(nalData) > 0 {
				s.processNALU(st, nalData)
			}
			consumed = nextStart
		}

		// 丢弃已处理的数据，保留未完成的部分
		if consumed > 0 {
			pending = append([]byte{}, pending[consumed:]...)
		}

		// 防止 pending 无限增长（流异常时）
		if len(pending) > 1<<20 {
			log.Printf("[stream] camera %d: pending buffer overflow, resetting", st.CameraID)
			pending = nil
		}

		if err != nil {
			// 处理最后未完成的 NAL
			if len(pending) > 0 {
				startIdx := indexStartCode(pending, 0)
				if startIdx >= 0 {
					nalStart := startIdx + startCodeLen(pending, startIdx)
					if nalStart < len(pending) {
						s.processNALU(st, pending[nalStart:])
					}
				}
			}
			if err == io.EOF {
				return nil
			}
			return err
		}
	}
}

// indexStartCode 从 offset 开始查找 Annex B start code (0x000001 或 0x00000001)。
// 返回 start code 的第一个字节索引；未找到返回 -1。
func indexStartCode(data []byte, offset int) int {
	if offset < 0 {
		offset = 0
	}
	// 需要至少 3 字节才能构成 start code
	for i := offset; i+2 < len(data); i++ {
		if data[i] == 0 && data[i+1] == 0 && data[i+2] == 1 {
			return i
		}
	}
	return -1
}

// startCodeLen 返回 start code 的字节长度（3 或 4 字节）。
func startCodeLen(data []byte, idx int) int {
	if idx+4 < len(data) && data[idx] == 0 && data[idx+1] == 0 && data[idx+2] == 0 && data[idx+3] == 1 {
		return 4
	}
	return 3
}

func (s *StreamService) processNALU(st *Stream, data []byte) {
	if len(data) == 0 {
		return
	}

	nalu := NALU{
		Data: make([]byte, len(data)),
	}
	copy(nalu.Data, data)

	var nalType int
	if st.Codec == "hevc" {
		// HEVC NAL header: 2 bytes, type = (byte0 >> 1) & 0x3F
		if len(data) < 2 {
			return
		}
		nalType = int((data[0] >> 1) & 0x3F)
		nalu.Type = byte(nalType)

		switch nalType {
		case 32: // VPS (Video Parameter Set)
			st.vps = make([]byte, len(data))
			copy(st.vps, data)
		case 33: // SPS
			st.sps = make([]byte, len(data))
			copy(st.sps, data)
		case 34: // PPS
			st.pps = make([]byte, len(data))
			copy(st.pps, data)
		case 19, 20: // IDR_W_RADL, IDR_N_LP
			nalu.IsIDR = true
			go s.extractJPEG(st, data)
		}
	} else {
		// H.264 NAL header: 1 byte, type = byte0 & 0x1F
		nalType = int(data[0] & 0x1F)
		nalu.Type = byte(nalType)

		switch nalType {
		case 7: // SPS
			st.sps = make([]byte, len(data))
			copy(st.sps, data)
		case 8: // PPS
			st.pps = make([]byte, len(data))
			copy(st.pps, data)
		case 5: // IDR
			nalu.IsIDR = true
			// MJPEG 预览由持续转码器 (runMJPEGTranscoder) 提供 10+ FPS，
			// 这里不再逐个 IDR 提取（避免额外 CPU 开销）
		}
	}

	// 广播给所有订阅者
	st.mu.RLock()
	defer st.mu.RUnlock()
	for _, ch := range st.naluSubs {
		select {
		case ch <- nalu:
		default:
			// 消费者跟不上，丢弃
		}
	}
}

// extractJPEG 从 IDR 帧提取 JPEG 图像（通过 FFmpeg 辅助）。
func (s *StreamService) extractJPEG(st *Stream, idrData []byte) {
	// 构建最小的 Annex B 流（SPS+PPS+IDR 或 VPS+SPS+PPS+IDR）
	var h264 []byte
	startCode := []byte{0, 0, 0, 1}
	// HEVC 需要 VPS
	if st.Codec == "hevc" && len(st.vps) > 0 {
		h264 = append(h264, startCode...)
		h264 = append(h264, st.vps...)
	}
	if len(st.sps) > 0 {
		h264 = append(h264, startCode...)
		h264 = append(h264, st.sps...)
	}
	if len(st.pps) > 0 {
		h264 = append(h264, startCode...)
		h264 = append(h264, st.pps...)
	}
	h264 = append(h264, startCode...)
	h264 = append(h264, idrData...)

	ctx, cancel := context.WithTimeout(st.ctx, 3*time.Second)
	defer cancel()

	// 根据编码格式指定输入格式
	inputFmt := "h264"
	if st.Codec == "hevc" {
		inputFmt = "hevc"
	}

	cmd := exec.CommandContext(ctx, pkg.FFmpegBinPath(),
		"-f", inputFmt,
		"-i", "pipe:0",
		"-vf", "scale='min(1280,iw)':-2", // 限制最大宽度 1280，加快解码
		"-vframes", "1",
		"-f", "mjpeg",
		"-q:v", "5",
		"pipe:1",
	)
	cmd.Stdin = bytes.NewReader(h264)
	out, err := cmd.Output()
	if err != nil {
		return
	}
	st.jpegMu.Lock()
	st.latestJPG = make([]byte, len(out))
	copy(st.latestJPG, out)
	st.jpegMu.Unlock()
}

// ---------- 持续 MJPEG 转码器（10+ FPS） ----------

// runMJPEGTranscoder 运行持续的 FFmpeg 转码进程，将 RTSP 转为 MJPEG 流。
// 相比从 IDR 帧提取（受 GOP 限制最高 1 FPS），这可以达到 10+ FPS。
func (s *StreamService) runMJPEGTranscoder(st *Stream) {
	defer close(st.mjpegDone)

	restartDelay := time.Second
	for {
		select {
		case <-st.ctx.Done():
			return
		default:
		}

		err := s.transcodeMJPEG(st)
		if st.ctx.Err() != nil {
			return // context 取消，不重启
		}
		log.Printf("[stream] camera %d mjpeg transcoder: %v, restarting", st.CameraID, err)
		select {
		case <-st.ctx.Done():
			return
		case <-time.After(restartDelay):
		}
		if restartDelay < 5*time.Second {
			restartDelay *= 2
		}
	}
}

// transcodeMJPEG 启动一次 FFmpeg 转码并解析输出的 MJPEG 帧。
func (s *StreamService) transcodeMJPEG(st *Stream) error {
	// 持续转码为 MJPEG，12 FPS，最大宽度 960px（降低 CPU 和带宽）
	args := []string{
		"-rtsp_transport", "tcp",
		"-timeout", "5000000",
		"-i", st.Camera.RTSPUrl,
		"-vf", "fps=12,scale='min(960,iw)':-2",
		"-f", "mjpeg",
		"-q:v", "4",
		"pipe:1",
	}
	cmd := exec.CommandContext(st.ctx, pkg.FFmpegBinPath(), args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("stdout pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("ffmpeg start: %w", err)
	}

	// context 取消时立即杀死 FFmpeg
	go func() {
		<-st.ctx.Done()
		cmd.Process.Kill()
	}()
	defer func() {
		cmd.Process.Kill()
		cmd.Wait()
	}()

	return s.parseMJPEGFrames(st, stdout)
}

// parseMJPEGFrames 从 MJPEG 流中解析完整的 JPEG 帧（以 SOI/EOI 分隔）。
// StartH264MJPEGTranscoder 启动一个持续的 H.264→MJPEG 转码器。
// 返回用于写入 NALU（带 start code）的 writer；转码器以 ~12 FPS 输出 JPEG。
// 供 GB28181 等非 RTSP 流使用（没有 RTSP 可拉的场景）。
func (s *StreamService) StartH264MJPEGTranscoder(st *Stream) (io.WriteCloser, error) {
	cmd := exec.CommandContext(st.ctx, pkg.FFmpegBinPath(),
		"-f", "h264",
		"-i", "pipe:0",
		"-vf", "fps=12,scale='min(960,iw)':-2",
		"-f", "mjpeg",
		"-q:v", "4",
		"pipe:1",
	)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("ffmpeg start: %w", err)
	}

	// context 取消时杀死 FFmpeg
	go func() {
		<-st.ctx.Done()
		cmd.Process.Kill()
	}()
	go func() {
		defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
		_ = s.parseMJPEGFrames(st, stdout)
	}()

	return stdin, nil
}

func (s *StreamService) parseMJPEGFrames(st *Stream, r io.Reader) error {
	var pending []byte
	buf := make([]byte, 64*1024)
	soiMarker := []byte{0xFF, 0xD8}
	eoiMarker := []byte{0xFF, 0xD9}

	for {
		n, err := r.Read(buf)
		if n > 0 {
			pending = append(pending, buf[:n]...)
		}

		// 提取所有完整的 JPEG 帧
		for {
			soi := indexBytes(pending, soiMarker)
			if soi < 0 {
				break
			}
			eoi := indexBytesFrom(pending, eoiMarker, soi+2)
			if eoi < 0 {
				break // 帧未完成，等待更多数据
			}
			frame := pending[soi : eoi+2]
			st.jpegMu.Lock()
			st.latestJPG = make([]byte, len(frame))
			copy(st.latestJPG, frame)
			st.jpegMu.Unlock()
			pending = pending[eoi+2:]
		}

		// 限制 pending 防止内存无限增长
		if len(pending) > 1<<20 {
			pending = nil
		}

		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
	}
}

// indexBytes 在 data 中查找子序列 sub 的起始索引。
func indexBytes(data, sub []byte) int {
	return indexBytesFrom(data, sub, 0)
}

// indexBytesFrom 从 offset 开始查找子序列 sub 的起始索引。
func indexBytesFrom(data, sub []byte, offset int) int {
	if offset < 0 {
		offset = 0
	}
	if len(sub) == 0 || len(data) < offset+len(sub) {
		return -1
	}
	for i := offset; i <= len(data)-len(sub); i++ {
		match := true
		for j := range sub {
			if data[i+j] != sub[j] {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}

// ---------- 辅助：FFmpeg 探活（检测摄像头是否在线） ----------

func (s *StreamService) ProbeCamera(rtspURL string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, pkg.FFmpegBinPath(),
		"-rtsp_transport", "udp",
		"-i", rtspURL,
		"-vframes", "1",
		"-f", "null",
		"-",
	)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("probe failed: %w", err)
	}
	return nil
}

// ---------- 辅助函数 ----------

// buildFFmpegArgs 根据摄像头接入协议构建 ffmpeg 命令行参数。
func (s *StreamService) buildFFmpegArgs(cam *model.Camera) []string {
	// 通用输出参数：raw H.264 输出，零转码
	outArgs := []string{"-c", "copy", "-f", "h264", "-an", "pipe:1"}

	switch cam.AccessProtocol {
	case model.ProtocolRTSP, "":
		// RTSP 拉流（默认）
		// 使用 TCP 传输更可靠（NVR 对 UDP 支持不稳定）
		// -timeout: 连接超时（微秒），FFmpeg 8.x 使用此参数
		return append([]string{
			"-rtsp_transport", "tcp",
			"-timeout", "5000000",
			"-i", cam.RTSPUrl,
		}, outArgs...)

	case model.ProtocolLocal:
		// 本地摄像头
		return s.buildLocalCaptureArgs(cam, outArgs)

	default:
		// GB28181 不走 FFmpeg（通过 RTP 接收器）
		return nil
	}
}

// buildLocalCaptureArgs 为本地摄像机构建 ffmpeg 输入参数（跨平台）。
func (s *StreamService) buildLocalCaptureArgs(cam *model.Camera, outArgs []string) []string {
	os := runtime.GOOS
	switch os {
	case "linux":
		// 通过 VID/PID 匹配 /dev/v4l/by-id/ 路径，或使用 index
		path := cam.RTSPUrl // 复用 RTSPUrl 字段存储本地设备路径
		if path == "" {
			if cam.LocalIndex >= 0 {
				path = fmt.Sprintf("/dev/video%d", cam.LocalIndex)
			} else {
				return nil
			}
		}
		return append([]string{
			"-f", "v4l2",
			"-input_format", "mjpeg",
			"-video_size", "1280x720",
			"-framerate", "30",
			"-i", path,
		}, outArgs...)

	case "darwin":
		// macOS AVFoundation，索引号格式 "index"
		path := cam.RTSPUrl
		if path == "" {
			if cam.LocalIndex >= 0 {
				path = fmt.Sprintf("%d", cam.LocalIndex)
			} else if cam.LocalName != "" {
				path = cam.LocalName
			} else {
				return nil
			}
		}
		return append([]string{
			"-f", "avfoundation",
			"-framerate", "30",
			"-video_size", "1280x720",
			"-i", path,
		}, outArgs...)

	case "windows":
		// Windows DirectShow
		path := cam.RTSPUrl
		if path == "" {
			if cam.LocalName != "" {
				path = fmt.Sprintf("video=%s", cam.LocalName)
			} else if cam.LocalIndex >= 0 {
				path = fmt.Sprintf("video=%d", cam.LocalIndex)
			} else {
				return nil
			}
		}
		return append([]string{
			"-f", "dshow",
			"-video_size", "1280x720",
			"-framerate", "30",
			"-i", path,
		}, outArgs...)

	default:
		return nil
	}
}

// h264NALUToAnnexB 将 NALU 转换为 Annex B 格式（带 start code）。
func h264NALUToAnnexB(nalu NALU) []byte {
	// start code: 0x00000001
	sc := []byte{0, 0, 0, 1}
	result := make([]byte, len(sc)+len(nalu.Data))
	copy(result, sc)
	copy(result[len(sc):], nalu.Data)
	return result
}

// ---------- 工具函数（用于 WebRTC RTP 打包） ----------

// SplitNALUForRTP 将 NALU 拆分为适合 RTP 传输的包（max size 1200）。
func SplitNALUForRTP(data []byte, maxPktSize int) [][]byte {
	if len(data) <= maxPktSize {
		return [][]byte{data}
	}
	var pkts [][]byte
	for len(data) > 0 {
		end := maxPktSize
		if end > len(data) {
			end = len(data)
		}
		pkt := make([]byte, end)
		copy(pkt, data[:end])
		pkts = append(pkts, pkt)
		data = data[end:]
	}
	return pkts
}

// ---------- 错误定义 ----------

var (
	ErrStreamNotFound = errors.New("stream not found")
)
