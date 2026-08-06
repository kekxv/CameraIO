package pkg

import (
	"archive/zip"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestLocalPaths(t *testing.T) {
	ffmpeg, ffprobe := localPaths("data/bin")

	if runtime.GOOS == "windows" {
		if ffmpeg != filepath.Join("data", "bin", "ffmpeg.exe") {
			t.Errorf("expected ffmpeg.exe on Windows, got %s", ffmpeg)
		}
		if ffprobe != filepath.Join("data", "bin", "ffprobe.exe") {
			t.Errorf("expected ffprobe.exe on Windows, got %s", ffprobe)
		}
	} else {
		if ffmpeg != filepath.Join("data", "bin", "ffmpeg") {
			t.Errorf("unexpected ffmpeg path: %s", ffmpeg)
		}
		if ffprobe != filepath.Join("data", "bin", "ffprobe") {
			t.Errorf("unexpected ffprobe path: %s", ffprobe)
		}
	}
}

func TestIsExecutable(t *testing.T) {
	// 不存在的文件
	if isExecutable("/nonexistent/path/ffmpeg") {
		t.Error("nonexistent file should not be executable")
	}

	// 空路径
	if isExecutable("") {
		t.Error("empty path should not be executable")
	}

	// 当前测试文件本身应该不是可执行文件
	if runtime.GOOS != "windows" && isExecutable("ffmpeg_test.go") {
		t.Error("source file should not be executable")
	}
}

func TestDownloadInfo(t *testing.T) {
	url, filename := downloadInfo()

	// 至少应该有 URL（所有主流平台都支持）
	if url == "" {
		t.Logf("Warning: no download URL for %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	if filename == "" && url != "" {
		t.Error("filename should not be empty when URL is set")
	}
	if url != "" && filename == "" {
		t.Error("URL set but filename empty")
	}

	// 验证文件名扩展名
	switch runtime.GOOS {
	case "darwin", "windows":
		if url != "" && !hasSuffix(filename, ".zip") {
			t.Errorf("expected .zip for %s, got %s", runtime.GOOS, filename)
		}
	case "linux":
		if url != "" && !hasSuffix(filename, ".tar.xz") {
			t.Errorf("expected .tar.xz for linux, got %s", filename)
		}
	}
}

func TestEnsureFFmpeg_SystemPath(t *testing.T) {
	// 如果系统已安装 ffmpeg，应该能找到
	// 这个测试在有 ffmpeg 的环境下会通过，没有时会尝试下载
	ffmpeg, ffprobe, err := EnsureFFmpeg()
	if err != nil {
		t.Skipf("FFmpeg not available: %v", err)
	}
	if ffmpeg == "" {
		t.Error("ffmpeg path should not be empty")
	}
	if ffprobe == "" {
		t.Error("ffprobe path should not be empty")
	}
}

func TestFFmpegBinPath_Default(t *testing.T) {
	// 未调用 EnsureFFmpeg 时返回默认值
	// 注意：这依赖于测试运行顺序，其他测试可能已设置路径
	path := FFmpegBinPath()
	if path == "" {
		t.Error("FFmpegBinPath should not return empty string")
	}
}

func TestFFprobeBinPath_Default(t *testing.T) {
	path := FFprobeBinPath()
	if path == "" {
		t.Error("FFprobeBinPath should not return empty string")
	}
}

func TestExtractArchive_UnsupportedFormat(t *testing.T) {
	err := extractArchive("/tmp/test.unknown", "/tmp/dest")
	if err == nil {
		t.Error("expected error for unsupported format")
	}
}

func TestValidateArchive(t *testing.T) {
	dir := t.TempDir()

	// 有效 zip：包含 ffmpeg/ffprobe → 通过
	valid := filepath.Join(dir, "valid.zip")
	if err := writeTestZip(valid, []string{"bin/ffmpeg", "bin/ffprobe"}); err != nil {
		t.Fatal(err)
	}
	if !validateArchive(valid) {
		t.Error("valid zip with ffmpeg/ffprobe should pass validation")
	}

	// 结构有效但不含 ffmpeg/ffprobe → 不通过
	noBin := filepath.Join(dir, "nobin.zip")
	if err := writeTestZip(noBin, []string{"readme.txt"}); err != nil {
		t.Fatal(err)
	}
	if validateArchive(noBin) {
		t.Error("zip without ffmpeg/ffprobe should fail validation")
	}

	// 截断的 zip（模拟下载中断）→ 不通过
	truncated := filepath.Join(dir, "truncated.zip")
	data, _ := os.ReadFile(valid)
	if err := os.WriteFile(truncated, data[:len(data)/2], 0o644); err != nil {
		t.Fatal(err)
	}
	if validateArchive(truncated) {
		t.Error("truncated zip should fail validation")
	}

	// 不存在 / 空文件 → 不通过
	if validateArchive(filepath.Join(dir, "missing.zip")) {
		t.Error("missing file should fail validation")
	}
	empty := filepath.Join(dir, "empty.zip")
	os.WriteFile(empty, []byte{}, 0o644)
	if validateArchive(empty) {
		t.Error("empty file should fail validation")
	}
}

// writeTestZip 创建一个包含指定条目的 zip 文件（用于测试）。
func writeTestZip(path string, entries []string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	for _, name := range entries {
		w, err := zw.Create(name)
		if err != nil {
			return err
		}
		if _, err := w.Write([]byte("x")); err != nil {
			return err
		}
	}
	return zw.Close()
}

func hasSuffix(s, suffix string) bool {
	return len(s) >= len(suffix) && s[len(s)-len(suffix):] == suffix
}
