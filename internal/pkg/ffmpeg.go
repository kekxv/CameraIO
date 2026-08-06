package pkg

import (
	"archive/zip"
	"compress/bzip2"
	"compress/gzip"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// FFmpegPaths 返回 ffmpeg 和 ffprobe 的可执行文件路径。
// 优先使用系统已安装的版本，否则自动下载。
var (
	ffmpegPath  string
	ffprobePath string

	statusMu sync.Mutex
	status   FFmpegStatus
)

// FFmpegStatus FFmpeg 可用性/下载状态（供前端展示）。
type FFmpegStatus struct {
	State      string `json:"state"` // "installed" / "downloading" / "extracting" / "error" / "checking"
	Progress   int    `json:"progress"` // 0-100
	Downloaded int64  `json:"downloaded_bytes"`
	Total      int64  `json:"total_bytes"`
	Path       string `json:"path"`
	Error      string `json:"error"`
}

// setStatus 更新全局 FFmpeg 状态。
func setStatus(s FFmpegStatus) {
	statusMu.Lock()
	status = s
	statusMu.Unlock()
}

// GetFFmpegStatus 返回当前 FFmpeg 状态。
func GetFFmpegStatus() FFmpegStatus {
	statusMu.Lock()
	defer statusMu.Unlock()
	return status
}

// EnsureFFmpegAsync 确保 ffmpeg 可用。若不可用，则在后台启动下载（不阻塞启动）。
// 返回是否立即可用。
func EnsureFFmpegAsync() bool {
	// 1. 环境变量
	if envPath := os.Getenv("CAMERAIO_FFMPEG_PATH"); envPath != "" {
		ffmpegPath = envPath
		ffprobePath = envPath
		if p := findAlongside(envPath, "ffprobe"); p != "" {
			ffprobePath = p
		}
		setStatus(FFmpegStatus{State: "installed", Path: ffmpegPath})
		return true
	}

	// 2. 系统 PATH
	if sysFFmpeg, err := exec.LookPath("ffmpeg"); err == nil {
		ffmpegPath = sysFFmpeg
		if sysFFprobe, err := exec.LookPath("ffprobe"); err == nil {
			ffprobePath = sysFFprobe
		} else {
			ffprobePath = findAlongside(sysFFmpeg, "ffprobe")
			if ffprobePath == "" {
				ffprobePath = sysFFmpeg
			}
		}
		setStatus(FFmpegStatus{State: "installed", Path: ffmpegPath})
		return true
	}

	// 3. 本地缓存
	binDir := filepath.Join("data", "bin")
	localFFmpeg, localFFprobe := localPaths(binDir)
	if isExecutable(localFFmpeg) {
		ffmpegPath = localFFmpeg
		ffprobePath = localFFprobe
		setStatus(FFmpegStatus{State: "installed", Path: ffmpegPath})
		return true
	}

	// 4. 后台下载（不阻塞启动）
	log.Printf("[FFmpeg] 系统未安装 ffmpeg，后台开始自动下载...")
	setStatus(FFmpegStatus{State: "downloading", Progress: 0})
	go func() {
		if err := downloadFFmpeg(binDir); err != nil {
			setStatus(FFmpegStatus{State: "error", Error: err.Error()})
			log.Printf("[FFmpeg] 自动下载失败: %v", err)
			return
		}
		statusMu.Lock()
		ffmpegPath = localFFmpeg
		ffprobePath = localFFprobe
		statusMu.Unlock()
		setStatus(FFmpegStatus{State: "installed", Progress: 100, Path: ffmpegPath})
		log.Printf("[FFmpeg] 下载完成: %s", ffmpegPath)
	}()
	return false
}

// EnsureFFmpeg 确保 ffmpeg 和 ffprobe 可用（同步，阻塞直到完成）。用于测试和兼容。
func EnsureFFmpeg() (string, string, error) {
	if EnsureFFmpegAsync() {
		return ffmpegPath, ffprobePath, nil
	}
	// 等待后台下载完成
	deadline := time.Now().Add(10 * time.Minute)
	for time.Now().Before(deadline) {
		st := GetFFmpegStatus()
		if st.State == "installed" {
			return ffmpegPath, ffprobePath, nil
		}
		if st.State == "error" {
			return "", "", fmt.Errorf("自动下载 FFmpeg 失败: %s (请手动安装 ffmpeg 或设置 CAMERAIO_FFMPEG_PATH)", st.Error)
		}
		time.Sleep(500 * time.Millisecond)
	}
	return "", "", fmt.Errorf("FFmpeg 下载超时")
}

// FFmpegBinPath 返回已解析的 ffmpeg 路径（在 EnsureFFmpeg 之后调用）。
func FFmpegBinPath() string {
	statusMu.Lock()
	defer statusMu.Unlock()
	if ffmpegPath != "" {
		return ffmpegPath
	}
	return "ffmpeg"
}

// FFprobeBinPath 返回已解析的 ffprobe 路径。
func FFprobeBinPath() string {
	statusMu.Lock()
	defer statusMu.Unlock()
	if ffprobePath != "" {
		return ffprobePath
	}
	return "ffprobe"
}

// ---------- 内部实现 ----------

func localPaths(binDir string) (string, string) {
	if runtime.GOOS == "windows" {
		return filepath.Join(binDir, "ffmpeg.exe"), filepath.Join(binDir, "ffprobe.exe")
	}
	return filepath.Join(binDir, "ffmpeg"), filepath.Join(binDir, "ffprobe")
}

func findAlongside(ffmpegPath, name string) string {
	dir := filepath.Dir(ffmpegPath)
	candidate := filepath.Join(dir, name)
	if runtime.GOOS == "windows" {
		candidate += ".exe"
	}
	if isExecutable(candidate) {
		return candidate
	}
	return ""
}

func isExecutable(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	if runtime.GOOS == "windows" {
		return !info.IsDir()
	}
	return info.Mode()&0111 != 0
}

// downloadFFmpeg 下载适合当前平台的 FFmpeg。
// 若本地已存在完整有效的安装包（上次下载中断/解压失败遗留），直接解压复用，避免重新下载。
func downloadFFmpeg(destDir string) error {
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return err
	}

	url, filename := downloadInfo()
	if url == "" {
		return fmt.Errorf("不支持的平台: %s/%s", runtime.GOOS, runtime.GOARCH)
	}

	archivePath := filepath.Join(destDir, filename)

	// 1. 本地已有完整有效的安装包 → 直接解压复用，不重新下载
	if validateArchive(archivePath) {
		log.Printf("[FFmpeg] 本地已存在完整安装包 %s，跳过下载，直接解压...", filename)
		setStatus(FFmpegStatus{State: "extracting", Progress: 100})
		if err := extractArchive(archivePath, destDir); err != nil {
			return fmt.Errorf("解压本地安装包失败: %w", err)
		}
		return makeExecutable(destDir)
	}
	// 2. 残留的损坏/不完整安装包 → 删除后重新下载
	if st, err := os.Stat(archivePath); err == nil && !st.IsDir() {
		log.Printf("[FFmpeg] 本地安装包无效（下载不完整或已损坏），删除后重新下载: %s", filename)
		if err := os.Remove(archivePath); err != nil {
			log.Printf("[FFmpeg] 删除无效安装包失败: %v", err)
		}
	}

	log.Printf("[FFmpeg] 系统未安装 ffmpeg，正在从 %s 下载（请耐心等待，约几十MB）...", url)

	client := &http.Client{Timeout: 600 * time.Second} // 10 分钟，大文件
	resp, err := client.Get(url)
	if err != nil {
		return fmt.Errorf("下载请求失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("下载返回 %s", resp.Status)
	}

	// 下载到安装包文件（覆盖旧的残留文件），带进度日志
	f, err := os.Create(archivePath)
	if err != nil {
		return err
	}

	total := resp.ContentLength
	written := int64(0)
	buf := make([]byte, 256*1024)
	lastLog := time.Now()
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := f.Write(buf[:n]); werr != nil {
				f.Close()
				os.Remove(archivePath)
				return fmt.Errorf("写入失败: %w", werr)
			}
			written += int64(n)
			// 上报进度（供前端实时展示）
			progress := 0
			if total > 0 {
				progress = int(float64(written) * 100 / float64(total))
			}
			setStatus(FFmpegStatus{State: "downloading", Progress: progress, Downloaded: written, Total: total})
			// 每 5 秒或每 ~20MB 打印一次进度
			if time.Since(lastLog) >= 5*time.Second {
				if total > 0 {
					log.Printf("[FFmpeg] 下载进度: %d/%d MB (%.0f%%)",
						written/1024/1024, total/1024/1024, float64(written)*100/float64(total))
				} else {
					log.Printf("[FFmpeg] 下载进度: %d MB", written/1024/1024)
				}
				lastLog = time.Now()
			}
		}
		if rerr != nil {
			if rerr != io.EOF {
				f.Close()
				os.Remove(archivePath)
				return fmt.Errorf("下载中断: %w", rerr)
			}
			break
		}
	}
	f.Close()
	setStatus(FFmpegStatus{State: "extracting", Progress: 100, Downloaded: written, Total: total})
	log.Printf("[FFmpeg] 下载完成: %d MB，正在解压...", written/1024/1024)

	// 解压
	if err := extractArchive(archivePath, destDir); err != nil {
		return fmt.Errorf("解压失败: %w", err)
	}

	// 安装包作为缓存保留，供下次启动复用（避免重新下载）
	return makeExecutable(destDir)
}

// makeExecutable 设置 ffmpeg/ffprobe 可执行权限（Windows 无需设置）。
func makeExecutable(destDir string) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	for _, name := range []string{"ffmpeg", "ffprobe"} {
		p := filepath.Join(destDir, name)
		if err := os.Chmod(p, 0o755); err != nil {
			return err
		}
	}
	return nil
}

// validateArchive 检查本地安装包是否完整可用：结构有效且包含 ffmpeg/ffprobe。
func validateArchive(path string) bool {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() || info.Size() == 0 {
		return false
	}
	lower := strings.ToLower(path)
	switch {
	case strings.HasSuffix(lower, ".zip"):
		// 截断/损坏的 zip 无法读取中央目录，zip.OpenReader 会返回错误
		zr, err := zip.OpenReader(path)
		if err != nil {
			return false
		}
		defer zr.Close()
		hasFFmpeg, hasFFprobe := false, false
		for _, zf := range zr.File {
			switch filepath.Base(zf.Name) {
			case "ffmpeg", "ffmpeg.exe":
				hasFFmpeg = true
			case "ffprobe", "ffprobe.exe":
				hasFFprobe = true
			}
		}
		return hasFFmpeg && hasFFprobe
	case strings.HasSuffix(lower, ".tar.xz"):
		return tarCanList(path, "-tJf")
	case strings.HasSuffix(lower, ".tar.gz") || strings.HasSuffix(lower, ".tgz"):
		return tarCanList(path, "-tzf")
	case strings.HasSuffix(lower, ".tar.bz2"):
		return tarCanList(path, "-tjf")
	}
	return false
}

// tarCanList 用系统 tar 列出压缩包内容，能成功列出说明结构完整。
func tarCanList(path, flag string) bool {
	cmd := exec.Command("tar", flag, path)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	return cmd.Run() == nil
}

// downloadInfo 返回当前平台的下载 URL 和文件名。
func downloadInfo() (url, filename string) {
	switch runtime.GOOS {
	case "darwin":
		// macOS: evermeet.cx 提供单独的 ffmpeg/ffprobe 二进制
		switch runtime.GOARCH {
		case "arm64":
			return "https://www.osxexperts.net/ffmpeg7arm.zip", "ffmpeg7arm.zip"
		default:
			return "https://evermeet.cx/ffmpeg/getrelease/zip", "ffmpeg.zip"
		}
	case "linux":
		switch runtime.GOARCH {
		case "amd64":
			return "https://johnvansickle.com/ffmpeg/releases/ffmpeg-release-amd64-static.tar.xz", "ffmpeg-release-amd64-static.tar.xz"
		case "arm64":
			return "https://johnvansickle.com/ffmpeg/releases/ffmpeg-release-arm64-static.tar.xz", "ffmpeg-release-arm64-static.tar.xz"
		}
	case "windows":
		return "https://www.gyan.dev/ffmpeg/builds/ffmpeg-release-essentials.zip", "ffmpeg-release-essentials.zip"
	}
	return "", ""
}

// extractArchive 根据扩展名解压文件。
func extractArchive(archivePath, destDir string) error {
	lower := strings.ToLower(archivePath)
	switch {
	case strings.HasSuffix(lower, ".zip"):
		return extractZip(archivePath, destDir)
	case strings.HasSuffix(lower, ".tar.xz"):
		return extractTarXz(archivePath, destDir)
	case strings.HasSuffix(lower, ".tar.gz") || strings.HasSuffix(lower, ".tgz"):
		return extractTarGz(archivePath, destDir)
	case strings.HasSuffix(lower, ".tar.bz2"):
		return extractTarBz2(archivePath, destDir)
	default:
		return fmt.Errorf("不支持的归档格式: %s", archivePath)
	}
}

func extractZip(src, destDir string) error {
	r, err := zip.OpenReader(src)
	if err != nil {
		return err
	}
	defer r.Close()

	for _, f := range r.File {
		if f.FileInfo().IsDir() {
			continue
		}
		name := filepath.Base(f.Name)
		if name == "." || name == "" {
			continue
		}
		target := filepath.Join(destDir, name)
		if err := extractZipFile(f, target); err != nil {
			return err
		}
	}
	return nil
}

func extractZipFile(f *zip.File, target string) error {
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()

	out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, rc)
	return err
}

func extractTarXz(src, destDir string) error {
	// tar.xz 需要外部 xz 命令或纯 Go 实现
	// 简化: 使用系统 tar 命令
	cmd := exec.Command("tar", "-xJf", src, "-C", destDir, "--strip-components=1", "--wildcards", "*/ffmpeg", "*/ffprobe")
	if out, err := cmd.CombinedOutput(); err != nil {
		// 回退: 解压所有文件
		cmd2 := exec.Command("tar", "-xJf", src, "-C", destDir, "--strip-components=1")
		if out2, err2 := cmd2.CombinedOutput(); err2 != nil {
			return fmt.Errorf("tar 解压失败: %s / %s", string(out), string(out2))
		}
	}
	return nil
}

func extractTarGz(src, destDir string) error {
	f, err := os.Open(src)
	if err != nil {
		return err
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()

	// 使用系统 tar
	tmpTar := src[:len(src)-3] // 去掉 .gz
	tf, err := os.Create(tmpTar)
	if err != nil {
		return err
	}
	io.Copy(tf, gz)
	tf.Close()

	cmd := exec.Command("tar", "-xf", tmpTar, "-C", destDir, "--strip-components=1")
	err = cmd.Run()
	os.Remove(tmpTar)
	return err
}

func extractTarBz2(src, destDir string) error {
	f, err := os.Open(src)
	if err != nil {
		return err
	}
	defer f.Close()

	bz := bzip2.NewReader(f)
	tmpTar := src[:len(src)-4] // 去掉 .bz2
	tf, err := os.Create(tmpTar)
	if err != nil {
		return err
	}
	io.Copy(tf, bz)
	tf.Close()

	cmd := exec.Command("tar", "-xf", tmpTar, "-C", destDir, "--strip-components=1")
	err = cmd.Run()
	os.Remove(tmpTar)
	return err
}
