package main

import (
	"bufio"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// localcam - 本地摄像头 RTSP 模拟器客户端
//
// 功能：将本地 USB/系统摄像头作为 RTSP 流对外发布，
// 用于扩展 CameraIO 或任何其他 RTSP 客户端的设备集。
//
// 用法：
//   localcam list                                  # 列出本地摄像头
//   localcam serve --index 0 --port 8554           # 将 index 0 的摄像头发布为 RTSP
//   localcam serve --name "FaceTime HD Camera"     # 按名称选择摄像头
//   localcam serve --vid 0x046d --pid 0x0825       # 按 USB VID/PID 选择

const version = "1.0.0"

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "list":
		cmdList()
	case "serve":
		cmdServe()
	case "help", "-h", "--help":
		printUsage()
	case "version", "-v", "--version":
		fmt.Printf("localcam %s (%s/%s)\n", version, runtime.GOOS, runtime.GOARCH)
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", os.Args[1])
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println(`localcam - 本地摄像头 RTSP 模拟器

用法:
  localcam <command> [options]

命令:
  list                     列出系统中所有可用的本地摄像头
  serve                    将选定的本地摄像头作为 RTSP 流发布

serve 选项:
  --index <N>              按索引号选择摄像头（如 0 表示 /dev/video0）
  --name <name>            按设备名称选择（不区分大小写）
  --vid <VID>              按 USB Vendor ID 选择
  --pid <PID>              按 USB Product ID 选择
  --port <port>            RTSP 服务端口（默认 8554）
  --path <path>            RTSP 路径（默认 /live）
  --width <W>              视频宽度（默认 1280）
  --height <H>             视频高度（默认 720）
  --fps <N>                帧率（默认 30）

示例:
  localcam list
  localcam serve --index 0 --port 8554
  localcam serve --name "FaceTime HD Camera"
  localcam serve --vid 0x046d --pid 0x0825 --port 554

输出:
  启动后，RTSP 流可通过以下地址访问：
    rtsp://<host>:<port><path>
  例如：rtsp://localhost:8554/live`)
}

// ---------- Local Camera Enumeration ----------

type LocalCamera struct {
	Index int
	Name  string
	Path  string
	VID   string
	PID   string
}

func enumerateLocalCameras() ([]LocalCamera, error) {
	switch runtime.GOOS {
	case "linux":
		return enumerateV4L2()
	case "darwin":
		return enumerateAVFoundation()
	case "windows":
		return enumerateDirectShow()
	default:
		return nil, fmt.Errorf("不支持的操作系统: %s", runtime.GOOS)
	}
}

func enumerateV4L2() ([]LocalCamera, error) {
	cmd := exec.Command("ffmpeg", "-f", "v4l2", "-list_devices", "list_formats", "-i", "")
	out, _ := cmd.CombinedOutput()
	return parseV4L2(string(out)), nil
}

func parseV4L2(output string) []LocalCamera {
	var cams []LocalCamera
	seen := map[int]bool{}
	for _, line := range strings.Split(output, "\n") {
		// 匹配: /dev/video0: USB Camera
		if idx := strings.Index(line, "/dev/video"); idx >= 0 {
			rest := line[idx:]
			parts := strings.SplitN(rest, ":", 2)
			if len(parts) < 2 {
				continue
			}
			path := strings.TrimSpace(parts[0])
			numStr := strings.TrimPrefix(path, "/dev/video")
			num, err := strconv.Atoi(numStr)
			if err != nil {
				continue
			}
			if seen[num] {
				continue
			}
			seen[num] = true
			name := strings.TrimSpace(parts[1])
			// 去掉尾部的括号内容
			if i := strings.Index(name, "("); i > 0 {
				name = strings.TrimSpace(name[:i])
			}
			cams = append(cams, LocalCamera{Index: num, Name: name, Path: path})
		}
	}
	return cams
}

func enumerateAVFoundation() ([]LocalCamera, error) {
	cmd := exec.Command("ffmpeg", "-f", "avfoundation", "-list_devices", "true", "-i", "")
	out, _ := cmd.CombinedOutput()
	return parseAVFoundation(string(out)), nil
}

func parseAVFoundation(output string) []LocalCamera {
	var cams []LocalCamera
	inVideo := false
	for _, line := range strings.Split(output, "\n") {
		// 进入视频区域
		if strings.Contains(line, "AVFoundation video devices:") {
			inVideo = true
			continue
		}
		// 离开视频区域（进入音频）
		if strings.Contains(line, "AVFoundation audio devices:") {
			inVideo = false
			continue
		}
		if !inVideo {
			continue
		}

		// 提取 [N] 和名称（去掉前面的 [AVFoundation indev @ ...] 前缀）
		if idx := strings.LastIndex(line, "["); idx >= 0 {
			rest := line[idx:]
			if end := strings.Index(rest, "]"); end > 1 {
				numStr := rest[1:end]
				num, err := strconv.Atoi(strings.TrimSpace(numStr))
				if err != nil {
					continue
				}
				name := strings.TrimSpace(rest[end+1:])
				// 去掉可能的引号
				name = strings.Trim(name, "\"")
				cams = append(cams, LocalCamera{
					Index: num,
					Name:  name,
					Path:  fmt.Sprintf("%d", num),
				})
			}
		}
	}
	return cams
}

func enumerateDirectShow() ([]LocalCamera, error) {
	cmd := exec.Command("ffmpeg", "-f", "dshow", "-list_devices", "true", "-i", "dummy")
	out, _ := cmd.CombinedOutput()
	return parseDirectShow(string(out)), nil
}

func parseDirectShow(output string) []LocalCamera {
	var cams []LocalCamera
	inVideo := false
	idx := 0
	for _, line := range strings.Split(output, "\n") {
		if strings.Contains(line, "DirectShow video devices") {
			inVideo = true
			continue
		}
		if strings.Contains(line, "DirectShow audio devices") {
			inVideo = false
			continue
		}
		if !inVideo {
			continue
		}
		// 匹配: [dshow @ ...] "USB Camera"
		if start := strings.Index(line, "\""); start >= 0 {
			end := strings.LastIndex(line, "\"")
			if end > start {
				name := line[start+1 : end]
				cams = append(cams, LocalCamera{
					Index: idx,
					Name:  name,
					Path:  fmt.Sprintf("video=%s", name),
				})
				idx++
			}
		}
	}
	return cams
}

// ---------- list 命令 ----------

func cmdList() {
	cams, err := enumerateLocalCameras()
	if err != nil {
		fmt.Fprintf(os.Stderr, "枚举失败: %v\n", err)
		os.Exit(1)
	}
	if len(cams) == 0 {
		fmt.Println("未检测到本地摄像头")
		return
	}
	fmt.Printf("检测到 %d 个本地摄像头:\n\n", len(cams))
	for _, c := range cams {
		fmt.Printf("  [%d] %s\n      路径: %s\n", c.Index, c.Name, c.Path)
		if c.VID != "" {
			fmt.Printf("      VID: %s  PID: %s\n", c.VID, c.PID)
		}
		fmt.Println()
	}
}

// ---------- serve 命令 ----------

type serveConfig struct {
	Index  int
	Name   string
	VID    string
	PID    string
	Port   int
	Path   string
	Width  int
	Height int
	FPS    int
}

func cmdServe() {
	cfg := parseServeArgs()

	// 选择摄像头
	cams, err := enumerateLocalCameras()
	if err != nil {
		fmt.Fprintf(os.Stderr, "枚举失败: %v\n", err)
		os.Exit(1)
	}
	cam := selectCamera(cams, cfg)
	if cam == nil {
		fmt.Fprintf(os.Stderr, "未找到匹配的摄像头\n")
		os.Exit(1)
	}
	fmt.Printf("选择摄像头: [%d] %s (%s)\n", cam.Index, cam.Name, cam.Path)

	// 启动 FFmpeg 捕获本地摄像头，输出 H.264 到 pipe
	ffmpegCmd := buildFFmpegCmd(cam, cfg)
	fmt.Printf("启动 FFmpeg: %s\n", strings.Join(ffmpegCmd.Args, " "))

	stdout, err := ffmpegCmd.StdoutPipe()
	if err != nil {
		fmt.Fprintf(os.Stderr, "FFmpeg stdout pipe 失败: %v\n", err)
		os.Exit(1)
	}
	if err := ffmpegCmd.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "FFmpeg 启动失败: %v\n", err)
		os.Exit(1)
	}
	defer ffmpegCmd.Process.Kill()

	// 启动 RTSP 服务器
	rtsp := NewRTSPServer(cfg.Port, cfg.Path)
	go rtsp.Serve()

	// 从 FFmpeg 读取 H.264 数据并推送到 RTSP 客户端
	go func() {
		buf := make([]byte, 8*1024) // 减小缓冲区到 8KB，降低延迟
		for {
			n, err := stdout.Read(buf)
			if n > 0 {
				rtsp.Broadcast(buf[:n])
			}
			if err != nil {
				log.Printf("FFmpeg 输出读取错误: %v", err)
				return
			}
		}
	}()

	fmt.Printf("\nRTSP 服务已启动:\n")
	fmt.Printf("  rtsp://localhost:%d%s\n", cfg.Port, cfg.Path)
	fmt.Printf("  http://localhost:%d%s/live.mjpg (MJPEG 降级)\n\n", cfg.Port+1, cfg.Path)
	fmt.Println("按 Ctrl+C 停止...")

	// 启动 HTTP MJPEG 降级服务（作为备用）
	go serveMJPEG(rtsp, cfg.Port+1, cfg.Path)

	// 等待退出信号
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh
	fmt.Println("\n正在停止...")
}

func parseServeArgs() serveConfig {
	cfg := serveConfig{
		Index:  -1,
		Port:   8554,
		Path:   "/live",
		Width:  1280,
		Height: 720,
		FPS:    30,
	}
	for i := 2; i < len(os.Args); i++ {
		switch os.Args[i] {
		case "--index":
			if i+1 < len(os.Args) {
				cfg.Index, _ = strconv.Atoi(os.Args[i+1])
				i++
			}
		case "--name":
			if i+1 < len(os.Args) {
				cfg.Name = os.Args[i+1]
				i++
			}
		case "--vid":
			if i+1 < len(os.Args) {
				cfg.VID = os.Args[i+1]
				i++
			}
		case "--pid":
			if i+1 < len(os.Args) {
				cfg.PID = os.Args[i+1]
				i++
			}
		case "--port":
			if i+1 < len(os.Args) {
				cfg.Port, _ = strconv.Atoi(os.Args[i+1])
				i++
			}
		case "--path":
			if i+1 < len(os.Args) {
				cfg.Path = os.Args[i+1]
				i++
			}
		case "--width":
			if i+1 < len(os.Args) {
				cfg.Width, _ = strconv.Atoi(os.Args[i+1])
				i++
			}
		case "--height":
			if i+1 < len(os.Args) {
				cfg.Height, _ = strconv.Atoi(os.Args[i+1])
				i++
			}
		case "--fps":
			if i+1 < len(os.Args) {
				cfg.FPS, _ = strconv.Atoi(os.Args[i+1])
				i++
			}
		}
	}
	return cfg
}

func selectCamera(cams []LocalCamera, cfg serveConfig) *LocalCamera {
	for i := range cams {
		c := &cams[i]
		if cfg.VID != "" && cfg.PID != "" && c.VID == cfg.VID && c.PID == cfg.PID {
			return c
		}
		if cfg.Index >= 0 && c.Index == cfg.Index {
			return c
		}
		if cfg.Name != "" && strings.EqualFold(c.Name, cfg.Name) {
			return c
		}
	}
	// 如果没有指定选择器，返回第一个
	if cfg.Index < 0 && cfg.Name == "" && cfg.VID == "" {
		if len(cams) > 0 {
			return &cams[0]
		}
	}
	return nil
}

func buildFFmpegCmd(cam *LocalCamera, cfg serveConfig) *exec.Cmd {
	var inputArgs []string
	switch runtime.GOOS {
	case "linux":
		inputArgs = []string{"-f", "v4l2", "-input_format", "mjpeg",
			"-video_size", fmt.Sprintf("%dx%d", cfg.Width, cfg.Height),
			"-framerate", strconv.Itoa(cfg.FPS),
			"-i", cam.Path}
	case "darwin":
		inputArgs = []string{"-f", "avfoundation",
			"-video_size", fmt.Sprintf("%dx%d", cfg.Width, cfg.Height),
			"-framerate", strconv.Itoa(cfg.FPS),
			"-i", cam.Path}
	case "windows":
		inputArgs = []string{"-f", "dshow",
			"-video_size", fmt.Sprintf("%dx%d", cfg.Width, cfg.Height),
			"-framerate", strconv.Itoa(cfg.FPS),
			"-i", cam.Path}
	default:
		inputArgs = []string{"-f", "v4l2", "-i", cam.Path}
	}

	args := append(inputArgs,
		"-fflags", "nobuffer",           // 禁用输入缓冲
		"-flags", "low_delay",           // 启用低延迟模式
		"-probesize", "32",              // 减少探测大小
		"-analyzeduration", "0",         // 禁用分析时长
		"-c:v", "libx264",
		"-preset", "ultrafast",
		"-tune", "zerolatency",
		"-pix_fmt", "yuv420p",
		"-g", "10",                      // GOP 大小（10 帧）
		"-bf", "0",                      // 禁用 B 帧
		"-x264-params", "bframes=0:force-cfr=1:no-mbtree=1:sync-lookahead=0:sliced-threads=1:rc-lookahead=0",
		"-f", "h264",
		"pipe:1",
	)
	return exec.Command("ffmpeg", args...)
}

// ---------- 最小 RTSP 服务器 ----------

// RTSPServer 是一个简化的 RTSP 服务器，支持 H.264 单路流。
type RTSPServer struct {
	port int
	path string
	ln   net.Listener

	mu      sync.RWMutex
	clients map[*rtspClient]struct{}

	// RTP 数据缓冲
	rtpData []byte
	sps     []byte
	pps     []byte

	// 时间戳计数器（90kHz 时钟）
	timestamp uint32
}

type rtspClient struct {
	conn     net.Conn
	session  string
	rtpPort  int
	rtcpPort int
	rtpConn  *net.UDPConn
	seqNum   uint16
	ssrc     uint32
}

func NewRTSPServer(port int, path string) *RTSPServer {
	return &RTSPServer{
		port:    port,
		path:    path,
		clients: make(map[*rtspClient]struct{}),
	}
}

func (s *RTSPServer) Serve() error {
	// 同时监听 IPv4 和 IPv6
	ln, err := net.Listen("tcp", fmt.Sprintf("0.0.0.0:%d", s.port))
	if err != nil {
		// 如果 IPv4 失败，尝试 IPv6
		ln, err = net.Listen("tcp", fmt.Sprintf("[::]:%d", s.port))
		if err != nil {
			return err
		}
	}
	s.ln = ln
	log.Printf("[RTSP] 服务器监听端口 %d", s.port)

	for {
		conn, err := ln.Accept()
		if err != nil {
			return err
		}
		go s.handleClient(conn)
	}
}

func (s *RTSPServer) handleClient(conn net.Conn) {
	client := &rtspClient{
		conn:    conn,
		session: fmt.Sprintf("%08x", time.Now().UnixNano()),
		ssrc:    uint32(time.Now().UnixNano() & 0xFFFFFFFF),
	}
	defer func() {
		s.mu.Lock()
		delete(s.clients, client)
		s.mu.Unlock()
		if client.rtpConn != nil {
			client.rtpConn.Close()
		}
		conn.Close()
		log.Printf("[RTSP] 客户端断开连接: %s", conn.RemoteAddr())
	}()

	log.Printf("[RTSP] 新客户端连接: %s", conn.RemoteAddr())

	scanner := bufio.NewScanner(conn)
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.SplitN(line, " ", 3)
		if len(parts) < 3 {
			continue
		}
		method := parts[0]
		uri := parts[1]

		// 读取完整请求（直到空行）
		headers := map[string]string{}
		contentLen := 0
		cseq := "1"
		for scanner.Scan() {
			h := scanner.Text()
			if h == "" {
				break
			}
			kv := strings.SplitN(h, ": ", 2)
			if len(kv) == 2 {
				headers[kv[0]] = kv[1]
				if strings.ToLower(kv[0]) == "content-length" {
					contentLen, _ = strconv.Atoi(kv[1])
				}
				if kv[0] == "CSeq" {
					cseq = kv[1]
				}
			}
		}
		// 读取 body
		if contentLen > 0 {
			body := make([]byte, contentLen)
			// 简化处理：忽略 body 内容
			for i := 0; i < contentLen; i++ {
				if scanner.Scan() {
					body[i] = scanner.Bytes()[0]
				}
			}
		}

		log.Printf("[RTSP] 收到请求: %s %s (CSeq: %s)", method, uri, cseq)

		switch method {
		case "OPTIONS":
			s.respond(conn, 200, "OK", cseq, map[string]string{
				"Public": "OPTIONS, DESCRIBE, SETUP, PLAY, TEARDOWN",
			})
		case "DESCRIBE":
			sdp := s.buildSDP()
			s.respond(conn, 200, "OK", cseq, map[string]string{
				"Content-Type":   "application/sdp",
				"Content-Length": strconv.Itoa(len(sdp)),
			})
			conn.Write([]byte(sdp))
		case "SETUP":
			// 解析 Transport 头
			transport := headers["Transport"]

			// 检查是否请求 TCP interleaved 模式
			if strings.Contains(transport, "TCP") || strings.Contains(transport, "interleaved") {
				// TCP interleaved 模式
				client.rtpConn = nil // 使用 RTSP 连接本身
				client.session = fmt.Sprintf("%08x", time.Now().UnixNano())

				s.mu.Lock()
				s.clients[client] = struct{}{}
				s.mu.Unlock()

				log.Printf("[RTSP] 客户端使用 TCP interleaved 模式: %s", conn.RemoteAddr())

				s.respond(conn, 200, "OK", cseq, map[string]string{
					"Session":   client.session,
					"Transport": "RTP/AVP/TCP;unicast;interleaved=0-1",
				})
			} else {
				// UDP 模式
				cPorts := extractClientPorts(transport)
				client.rtpPort = cPorts[0]
				client.rtcpPort = cPorts[1]
				if client.rtpPort == 0 {
					client.rtpPort = 5004
					client.rtcpPort = 5005
				}

				// 创建 UDP 连接用于 RTP 传输
				rtpAddr := &net.UDPAddr{
					IP:   net.ParseIP("127.0.0.1"),
					Port: client.rtpPort,
				}
				rtpConn, err := net.DialUDP("udp", nil, rtpAddr)
				if err != nil {
					log.Printf("创建 RTP 连接失败: %v", err)
					s.respond(conn, 500, "Internal Server Error", cseq, nil)
					continue
				}
				client.rtpConn = rtpConn

				s.mu.Lock()
				s.clients[client] = struct{}{}
				s.mu.Unlock()

				log.Printf("[RTSP] 客户端已设置 RTP: %s -> port %d", conn.RemoteAddr(), client.rtpPort)

				s.respond(conn, 200, "OK", cseq, map[string]string{
					"Session":   client.session,
					"Transport": fmt.Sprintf("RTP/AVP;unicast;client_port=%d-%d;server_port=%d-%d",
						client.rtpPort, client.rtcpPort, client.rtpPort, client.rtcpPort),
				})
			}
		case "PLAY":
			log.Printf("[RTSP] 开始播放: %s", conn.RemoteAddr())
			s.respond(conn, 200, "OK", cseq, map[string]string{
				"Session": client.session,
			})
		case "TEARDOWN":
			s.respond(conn, 200, "OK", cseq, nil)
			return
		}
	}
}

func (s *RTSPServer) respond(conn net.Conn, status int, reason string, cseq string, headers map[string]string) {
	var buf strings.Builder
	buf.WriteString(fmt.Sprintf("RTSP/1.0 %d %s\r\n", status, reason))
	buf.WriteString(fmt.Sprintf("CSeq: %s\r\n", cseq))
	for k, v := range headers {
		buf.WriteString(fmt.Sprintf("%s: %s\r\n", k, v))
	}
	buf.WriteString("\r\n")
	conn.Write([]byte(buf.String()))
}

func (s *RTSPServer) buildSDP() string {
	return fmt.Sprintf(`v=0
o=- %d %d IN IP4 127.0.0.1
s=CameraIO Local Camera
c=IN IP4 127.0.0.1
t=0 0
m=video 0 RTP/AVP/TCP 96
a=rtpmap:96 H264/90000
a=fmtp:96 packetization-mode=1
a=control:trackID=0
a=transport:RTP/AVP/TCP;unicast;interleaved=0-1
`, time.Now().Unix(), time.Now().UnixNano())
}

// Broadcast 将 H.264 数据通过 RTP over UDP 广播给所有连接的客户端。
func (s *RTSPServer) Broadcast(data []byte) {
	// 解析所有 NALU
	nalus := s.splitNALUs(data)
	if len(nalus) == 0 {
		return
	}

	// 检查是否有 IDR 帧，如果是，需要发送 SPS/PPS
	hasIDR := false
	for _, nalu := range nalus {
		if len(nalu) > 0 {
			nalType := nalu[0] & 0x1F
			if nalType == 5 { // IDR
				hasIDR = true
				break
			}
		}
	}

	// 提取并缓存 SPS/PPS
	s.extractSPSPPS(nalus)

	s.mu.RLock()
	defer s.mu.RUnlock()

	// 递增时间戳（每帧递增一次，而不是每个 NALU）
	s.timestamp += 3000 // 90kHz / 30fps = 3000

	for client := range s.clients {
		if client.rtpConn == nil {
			continue
		}

		// 如果是 IDR 帧，先发送 SPS/PPS
		if hasIDR {
			if s.sps != nil && len(s.sps) > 0 {
				s.sendSingleRTPPacket(client, s.sps, false)
			}
			if s.pps != nil && len(s.pps) > 0 {
				s.sendSingleRTPPacket(client, s.pps, false)
			}
		}

		// 发送所有 NALU
		for i, nalu := range nalus {
			isLast := (i == len(nalus)-1)
			s.sendRTPPacket(client, nalu, isLast)
		}
	}
}

// splitNALUs 将 H.264 数据分割成独立的 NALU
func (s *RTSPServer) splitNALUs(data []byte) [][]byte {
	var nalus [][]byte
	i := 0
	for i < len(data) {
		// 查找起始码
		startCodeLen := 0
		if i+3 < len(data) && data[i] == 0 && data[i+1] == 0 && data[i+2] == 0 && data[i+3] == 1 {
			startCodeLen = 4
		} else if i+2 < len(data) && data[i] == 0 && data[i+1] == 0 && data[i+2] == 1 {
			startCodeLen = 3
		}

		if startCodeLen > 0 {
			i += startCodeLen
			// 查找下一个起始码
			nextStart := i
			for nextStart < len(data) {
				if nextStart+3 < len(data) && data[nextStart] == 0 && data[nextStart+1] == 0 && data[nextStart+2] == 0 && data[nextStart+3] == 1 {
					break
				}
				if nextStart+2 < len(data) && data[nextStart] == 0 && data[nextStart+1] == 0 && data[nextStart+2] == 1 {
					break
				}
				nextStart++
			}
			// 提取 NALU（不包括起始码）
			nalu := make([]byte, nextStart-i)
			copy(nalu, data[i:nextStart])
			nalus = append(nalus, nalu)
			i = nextStart
		} else {
			i++
		}
	}
	return nalus
}

// extractSPSPPS 从 NALU 列表中提取并缓存 SPS 和 PPS
func (s *RTSPServer) extractSPSPPS(nalus [][]byte) {
	for _, nalu := range nalus {
		if len(nalu) == 0 {
			continue
		}
		nalType := nalu[0] & 0x1F
		if nalType == 7 && len(s.sps) == 0 { // SPS
			s.sps = make([]byte, len(nalu))
			copy(s.sps, nalu)
		} else if nalType == 8 && len(s.pps) == 0 { // PPS
			s.pps = make([]byte, len(nalu))
			copy(s.pps, nalu)
		}
	}
}

// sendRTPPacket 发送一个 RTP 包，支持 H.264 NALU 分片和 TCP interleaved 模式
func (s *RTSPServer) sendRTPPacket(client *rtspClient, payload []byte, marker bool) {
	if len(payload) == 0 {
		return
	}

	const maxPayloadSize = 1200 // MTU - IP/UDP/RTP headers

	if len(payload) <= maxPayloadSize {
		// 单包模式：NALU 可以直接放入一个 RTP 包
		s.sendSingleRTPPacket(client, payload, marker)
	} else {
		// 分片模式：使用 FU-A 分片
		s.sendFragmentedRTPPackets(client, payload, marker)
	}
}

// sendTCPPacket 通过 TCP interleaved 模式发送 RTP 包
func (s *RTSPServer) sendTCPPacket(client *rtspClient, rtpPacket []byte) {
	// TCP interleaved 格式: $ + channel(1) + length(2) + data
	header := make([]byte, 4)
	header[0] = '$'
	header[1] = 0 // RTP channel
	header[2] = byte(len(rtpPacket) >> 8)
	header[3] = byte(len(rtpPacket))

	// 通过 RTSP 连接发送
	client.conn.Write(header)
	client.conn.Write(rtpPacket)
}

// sendSingleRTPPacket 发送单个 RTP 包（NALU 不需要分片）
func (s *RTSPServer) sendSingleRTPPacket(client *rtspClient, payload []byte, marker bool) {
	rtpHeader := make([]byte, 12)
	rtpHeader[0] = 0x80 // V=2, P=0, X=0, CC=0

	// PT=96 (dynamic payload type for H.264)
	if marker {
		rtpHeader[1] = 96 | 0x80 // M=1 (marker bit)
	} else {
		rtpHeader[1] = 96
	}

	// Sequence number (2 bytes)
	rtpHeader[2] = byte(client.seqNum >> 8)
	rtpHeader[3] = byte(client.seqNum)
	client.seqNum++

	// Timestamp (4 bytes) - 使用服务器时间戳（不递增）
	rtpHeader[4] = byte(s.timestamp >> 24)
	rtpHeader[5] = byte(s.timestamp >> 16)
	rtpHeader[6] = byte(s.timestamp >> 8)
	rtpHeader[7] = byte(s.timestamp)

	// SSRC (4 bytes)
	rtpHeader[8] = byte(client.ssrc >> 24)
	rtpHeader[9] = byte(client.ssrc >> 16)
	rtpHeader[10] = byte(client.ssrc >> 8)
	rtpHeader[11] = byte(client.ssrc)

	rtpPacket := append(rtpHeader, payload...)

	// 检查是否使用 TCP interleaved 模式
	if client.rtpConn == nil {
		// TCP interleaved 模式
		s.sendTCPPacket(client, rtpPacket)
	} else {
		// UDP 模式
		client.rtpConn.Write(rtpPacket)
	}
}

// sendFragmentedRTPPackets 发送分片的 RTP 包（FU-A 模式）
func (s *RTSPServer) sendFragmentedRTPPackets(client *rtspClient, payload []byte, marker bool) {
	const maxPayloadSize = 1200

	// 第一个字节是 NALU header
	naluHeader := payload[0]
	nalTypeActual := naluHeader & 0x1F
	nalRefIdc := naluHeader & 0x60

	// FU indicator
	fuIndicator := (nalRefIdc & 0x60) | 28 // FU-A type is 28

	// 跳过 NALU header
	payloadData := payload[1:]

	// 分片发送
	offset := 0
	firstFragment := true
	for offset < len(payloadData) {
		end := offset + maxPayloadSize - 2 // -2 for FU indicator and FU header
		if end > len(payloadData) {
			end = len(payloadData)
		}

		fragment := payloadData[offset:end]

		// FU header
		fuHeader := nalTypeActual
		if firstFragment {
			fuHeader |= 0x80 // Start bit
			firstFragment = false
		}
		if end >= len(payloadData) {
			fuHeader |= 0x40 // End bit
		}

		// 构建 RTP payload
		fuPayload := make([]byte, 2+len(fragment))
		fuPayload[0] = fuIndicator
		fuPayload[1] = fuHeader
		copy(fuPayload[2:], fragment)

		// 构建 RTP 头
		rtpHeader := make([]byte, 12)
		rtpHeader[0] = 0x80

		// 设置 marker bit（只在最后一个分片且 marker 为 true 时设置）
		if marker && end >= len(payloadData) {
			rtpHeader[1] = 96 | 0x80
		} else {
			rtpHeader[1] = 96
		}

		// Sequence number
		rtpHeader[2] = byte(client.seqNum >> 8)
		rtpHeader[3] = byte(client.seqNum)
		client.seqNum++

		// Timestamp（使用服务器时间戳，不递增）
		rtpHeader[4] = byte(s.timestamp >> 24)
		rtpHeader[5] = byte(s.timestamp >> 16)
		rtpHeader[6] = byte(s.timestamp >> 8)
		rtpHeader[7] = byte(s.timestamp)

		// SSRC
		rtpHeader[8] = byte(client.ssrc >> 24)
		rtpHeader[9] = byte(client.ssrc >> 16)
		rtpHeader[10] = byte(client.ssrc >> 8)
		rtpHeader[11] = byte(client.ssrc)

		packet := append(rtpHeader, fuPayload...)

		// 检查是否使用 TCP interleaved 模式
		if client.rtpConn == nil {
			// TCP interleaved 模式
			s.sendTCPPacket(client, packet)
		} else {
			// UDP 模式
			client.rtpConn.Write(packet)
		}

		offset = end
	}
}

func extractClientPorts(transport string) [2]int {
	var ports [2]int
	idx := strings.Index(transport, "client_port=")
	if idx < 0 {
		return ports
	}
	portStr := transport[idx+len("client_port="):]
	end := strings.IndexAny(portStr, ";,")
	if end > 0 {
		portStr = portStr[:end]
	}
	parts := strings.Split(portStr, "-")
	if len(parts) >= 1 {
		ports[0], _ = strconv.Atoi(parts[0])
	}
	if len(parts) >= 2 {
		ports[1], _ = strconv.Atoi(parts[1])
	}
	return ports
}

// ---------- HTTP MJPEG 降级服务 ----------

func serveMJPEG(rtsp *RTSPServer, port int, basePath string) {
	mux := http.NewServeMux()
	mux.HandleFunc(basePath+"/live.mjpg", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "multipart/x-mixed-replace; boundary=frame")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "close")

		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming not supported", 500)
			return
		}

		// 使用 FFmpeg 从摄像头捕获 JPEG 帧
		cmd := exec.Command("ffmpeg",
			"-f", "avfoundation",
			"-video_size", "640x480",
			"-framerate", "15",
			"-i", "0",
			"-vframes", "1",
			"-f", "image2pipe",
			"-vcodec", "mjpeg",
			"-")

		stdout, err := cmd.StdoutPipe()
		if err != nil {
			http.Error(w, "failed to create pipe", 500)
			return
		}

		if err := cmd.Start(); err != nil {
			http.Error(w, "failed to start ffmpeg", 500)
			return
		}

		buf := make([]byte, 1024*1024)
		n, _ := stdout.Read(buf)
		cmd.Wait()

		if n > 0 {
			fmt.Fprintf(w, "--frame\r\nContent-Type: image/jpeg\r\nContent-Length: %d\r\n\r\n", n)
			w.Write(buf[:n])
			fmt.Fprintf(w, "\r\n")
			flusher.Flush()
		}
	})
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"status":"ok"}`))
	})

	log.Printf("[HTTP] MJPEG 服务监听端口 %d", port)
	http.ListenAndServe(fmt.Sprintf(":%d", port), mux)
}
