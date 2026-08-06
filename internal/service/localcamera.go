package service

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"runtime"
	"strings"

	"CameraIO/internal/pkg"
	"time"
)

// LocalCamera 代表一个被枚举到的本地摄像头设备。
type LocalCamera struct {
	Index int    `json:"index"`       // 设备索引号 (如 /dev/video0 的 0)
	Name  string `json:"name"`        // 设备显示名
	Path  string `json:"path"`        // 设备路径（如 /dev/video0, video=0）
	VID   string `json:"vid,omitempty"`  // USB Vendor ID
	PID   string `json:"pid,omitempty"`  // USB Product ID
}

// LocalCameraService 提供本地摄像头枚举与捕获能力。
// 通过 ffmpeg 的设备枚举功能实现跨平台支持。
type LocalCameraService struct{}

func NewLocalCameraService() *LocalCameraService {
	return &LocalCameraService{}
}

// Enumerate 枚举系统中所有可用的本地摄像头。
func (s *LocalCameraService) Enumerate(ctx context.Context) ([]LocalCamera, error) {
	switch runtime.GOOS {
	case "linux":
		return s.enumerateV4L2(ctx)
	case "darwin":
		return s.enumerateAVFoundation(ctx)
	case "windows":
		return s.enumerateDirectShow(ctx)
	default:
		return nil, fmt.Errorf("unsupported OS: %s", runtime.GOOS)
	}
}

// ---------- Linux: v4l2 ----------

func (s *LocalCameraService) enumerateV4L2(ctx context.Context) ([]LocalCamera, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, pkg.FFmpegBinPath(),
		"-f", "v4l2",
		"-list_devices", "list_formats",
		"-i", "",
	)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	_ = cmd.Run() // ffmpeg 对 list_devices 总是返回非 0

	return parseV4L2Output(stderr.String()), nil
}

func parseV4L2Output(output string) []LocalCamera {
	var cameras []LocalCamera
	// 匹配形如: [video4linux4,v4l2 @ 0x...] /dev/video0: USB Camera: USB Camera
	re := regexp.MustCompile(`(/dev/video(\d+))[^\n]*?:\s*(.+?)(?:\s*\(|$)`)
	matches := re.FindAllStringSubmatch(output, -1)

	seen := make(map[int]bool)
	for _, m := range matches {
		if len(m) < 4 {
			continue
		}
		path := m[1]
		var idx int
		fmt.Sscanf(m[2], "%d", &idx)
		name := strings.TrimSpace(m[3])

		if seen[idx] {
			continue
		}
		seen[idx] = true

		cam := LocalCamera{
			Index: idx,
			Name:  name,
			Path:  path,
		}
		cameras = append(cameras, cam)
	}
	return cameras
}

// ---------- macOS: AVFoundation ----------

func (s *LocalCameraService) enumerateAVFoundation(ctx context.Context) ([]LocalCamera, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, pkg.FFmpegBinPath(),
		"-f", "avfoundation",
		"-list_devices", "true",
		"-i", "dummy",
	)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	_ = cmd.Run()

	return parseAVFoundationOutput(stderr.String()), nil
}

func parseAVFoundationOutput(output string) []LocalCamera {
	var cameras []LocalCamera
	// 匹配形如: [0] FaceTime HD Camera
	re := regexp.MustCompile(`\[(\d+)\]\s+(.+?)$`)

	inVideoSection := false
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		// 进入视频区域
		if strings.Contains(line, "video devices:") || strings.Contains(line, "AVVideo") {
			inVideoSection = true
			continue
		}
		// 离开视频区域（进入音频）
		if strings.Contains(line, "audio devices:") {
			inVideoSection = false
			continue
		}
		if !inVideoSection {
			continue
		}

		// 提取 [N] 和名称（去掉前面的 [AVFoundation indev @ ...] 前缀）
		if idx := strings.LastIndex(line, "["); idx >= 0 {
			rest := line[idx:]
			m := re.FindStringSubmatch(rest)
			if len(m) >= 3 {
				var camIdx int
				fmt.Sscanf(m[1], "%d", &camIdx)
				name := strings.TrimSpace(m[2])
				// 去掉可能的引号
				name = strings.Trim(name, "\"")
				cameras = append(cameras, LocalCamera{
					Index: camIdx,
					Name:  name,
					Path:  fmt.Sprintf("%d", camIdx),
				})
			}
		}
	}
	return cameras
}

// ---------- Windows: DirectShow ----------

func (s *LocalCameraService) enumerateDirectShow(ctx context.Context) ([]LocalCamera, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, pkg.FFmpegBinPath(),
		"-f", "dshow",
		"-list_devices", "true",
		"-i", "dummy",
	)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	_ = cmd.Run()

	return parseDirectShowOutput(stderr.String()), nil
}

func parseDirectShowOutput(output string) []LocalCamera {
	var cameras []LocalCamera
	inVideoSection := false

	lines := strings.Split(output, "\n")
	for _, line := range lines {
		if strings.Contains(line, "DirectShow video devices") {
			inVideoSection = true
			continue
		}
		if strings.Contains(line, "DirectShow audio devices") {
			inVideoSection = false
			continue
		}
		if !inVideoSection {
			continue
		}
		// 匹配形如: [dshow @ ...] "USB Camera"
		if idx := strings.Index(line, "\""); idx >= 0 {
			end := strings.LastIndex(line, "\"")
			if end > idx {
				name := line[idx+1 : end]
				cameras = append(cameras, LocalCamera{
					Index: len(cameras),
					Name:  name,
					Path:  fmt.Sprintf("video=%s", name),
				})
			}
		}
	}
	return cameras
}

// ---------- 本地摄像头 FFmpeg 参数构造 ----------

// BuildLocalCaptureArgs 为指定本地摄像机构建 ffmpeg 输入参数。
// 返回的 args 用于 `ffmpeg <args> -c copy ...` 或类似命令。
func (s *LocalCameraService) BuildLocalCaptureArgs(cam *LocalCamera) []string {
	switch runtime.GOOS {
	case "linux":
		return []string{"-f", "v4l2", "-input_format", "mjpeg", "-i", cam.Path}
	case "darwin":
		return []string{"-f", "avfoundation", "-framerate", "30", "-i", cam.Path}
	case "windows":
		return []string{"-f", "dshow", "-i", cam.Path}
	default:
		return nil
	}
}

// MatchCamera 根据 VID/PID/Index/Name 查找匹配的本地摄像头。
func (s *LocalCameraService) MatchCamera(cameras []LocalCamera, vid, pid string, index int, name string) *LocalCamera {
	for i := range cameras {
		c := &cameras[i]
		// 按 VID+PID 匹配
		if vid != "" && pid != "" && c.VID == vid && c.PID == pid {
			return c
		}
		// 按 Index 匹配
		if index >= 0 && c.Index == index {
			return c
		}
		// 按 Name 匹配（不区分大小写）
		if name != "" && strings.EqualFold(c.Name, name) {
			return c
		}
	}
	return nil
}
