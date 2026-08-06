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
	"time"
)

// FFmpegPaths 返回 ffmpeg 和 ffprobe 的可执行文件路径。
// 优先使用系统已安装的版本，否则自动下载。
var (
	ffmpegPath  string
	ffprobePath string
)

// EnsureFFmpeg 确保 ffmpeg 和 ffprobe 可用。返回两者的路径。
func EnsureFFmpeg() (string, string, error) {
	// 1. 检查环境变量覆盖
	if envPath := os.Getenv("CAMERAIO_FFMPEG_PATH"); envPath != "" {
		ffmpegPath = envPath
		ffprobePath = envPath // 某些发行包中 ffmpeg 包含 ffprobe 功能
		if p := findAlongside(envPath, "ffprobe"); p != "" {
			ffprobePath = p
		}
		log.Printf("[FFmpeg] 使用环境变量指定路径: %s", ffmpegPath)
		return ffmpegPath, ffprobePath, nil
	}

	// 2. 检查系统 PATH
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
		log.Printf("[FFmpeg] 使用系统安装: %s", ffmpegPath)
		return ffmpegPath, ffprobePath, nil
	}

	// 3. 检查本地缓存
	binDir := filepath.Join("data", "bin")
	localFFmpeg, localFFprobe := localPaths(binDir)
	if isExecutable(localFFmpeg) {
		ffmpegPath = localFFmpeg
		ffprobePath = localFFprobe
		log.Printf("[FFmpeg] 使用本地缓存: %s", ffmpegPath)
		return ffmpegPath, ffprobePath, nil
	}

	// 4. 自动下载
	log.Printf("[FFmpeg] 系统未安装 ffmpeg，正在自动下载...")
	if err := downloadFFmpeg(binDir); err != nil {
		return "", "", fmt.Errorf("自动下载 FFmpeg 失败: %w (请手动安装 ffmpeg 或设置 CAMERAIO_FFMPEG_PATH 环境变量)", err)
	}

	ffmpegPath = localFFmpeg
	ffprobePath = localFFprobe
	log.Printf("[FFmpeg] 下载完成: %s", ffmpegPath)
	return ffmpegPath, ffprobePath, nil
}

// FFmpegBinPath 返回已解析的 ffmpeg 路径（在 EnsureFFmpeg 之后调用）。
func FFmpegBinPath() string {
	if ffmpegPath != "" {
		return ffmpegPath
	}
	return "ffmpeg"
}

// FFprobeBinPath 返回已解析的 ffprobe 路径。
func FFprobeBinPath() string {
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
func downloadFFmpeg(destDir string) error {
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return err
	}

	url, filename := downloadInfo()
	if url == "" {
		return fmt.Errorf("不支持的平台: %s/%s", runtime.GOOS, runtime.GOARCH)
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

	// 下载到临时文件，带进度日志
	tmpFile := filepath.Join(destDir, filename)
	f, err := os.Create(tmpFile)
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
				os.Remove(tmpFile)
				return fmt.Errorf("写入失败: %w", werr)
			}
			written += int64(n)
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
				os.Remove(tmpFile)
				return fmt.Errorf("下载中断: %w", rerr)
			}
			break
		}
	}
	f.Close()
	log.Printf("[FFmpeg] 下载完成: %d MB，正在解压...", written/1024/1024)

	// 解压
	if err := extractArchive(tmpFile, destDir); err != nil {
		return fmt.Errorf("解压失败: %w", err)
	}

	// 清理临时文件
	os.Remove(tmpFile)

	// 设置可执行权限
	if runtime.GOOS != "windows" {
		for _, name := range []string{"ffmpeg", "ffprobe"} {
			p := filepath.Join(destDir, name)
			os.Chmod(p, 0o755)
		}
	}

	return nil
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
