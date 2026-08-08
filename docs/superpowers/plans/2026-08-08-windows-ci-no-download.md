# Windows CI FFmpeg Download Guard Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ensure every CI test run is offline with respect to FFmpeg while preserving automatic FFmpeg installation for normal application runs.

**Architecture:** `EnsureFFmpegAsync` will recognize one CI-only environment variable before it starts the background downloader and publish a terminal unavailable status. The FFmpeg package test will force the no-local-binary path and use a local proxy trap, proving the opt-out makes no network request. Both workflows will set the opt-out globally, so package tests cannot accidentally download FFmpeg.

**Tech Stack:** Go 1.20, GitHub Actions, Go standard library `net/http/httptest`.

## Global Constraints

- CI must never start an FFmpeg download.
- Normal runs without `CAMERAIO_SKIP_FFMPEG_DOWNLOAD=1` retain the current automatic-download behavior.
- Tests must use only local temporary resources and must not depend on PATH, a local FFmpeg installation, or the public FFmpeg download host.

---

### Task 1: Add the FFmpeg downloader opt-out contract

**Files:**
- Modify: `internal/pkg/ffmpeg_test.go`
- Modify: `internal/pkg/ffmpeg.go`

**Interfaces:**
- Consumes: `CAMERAIO_SKIP_FFMPEG_DOWNLOAD=1` in the process environment.
- Produces: `EnsureFFmpegAsync() bool` returns `false` and `GetFFmpegStatus()` reports `State == "error"` without starting a download.

- [x] **Step 1: Write the failing test**

```go
func TestEnsureFFmpegAsync_SkipsDownloadWhenDisabled(t *testing.T) {
	t.Setenv("CAMERAIO_SKIP_FFMPEG_DOWNLOAD", "1")
	// Force the no-system-binary path and use a local proxy trap.
	// Assert EnsureFFmpegAsync returns false, status is "error", and no proxy request occurs.
}
```

- [x] **Step 2: Run test to verify it fails**

Run: `go test ./internal/pkg -run '^TestEnsureFFmpegAsync_SkipsDownloadWhenDisabled$' -count=1`

Expected: FAIL because the current implementation starts the downloader and reports `State == "downloading"`.

- [x] **Step 3: Write minimal implementation**

```go
if os.Getenv("CAMERAIO_SKIP_FFMPEG_DOWNLOAD") == "1" {
	setStatus(FFmpegStatus{State: "error", Error: "FFmpeg automatic download disabled"})
	return false
}
```

Place it immediately before the existing background-download block in `EnsureFFmpegAsync`.

- [x] **Step 4: Run test to verify it passes**

Run: `go test ./internal/pkg -run '^TestEnsureFFmpegAsync_SkipsDownloadWhenDisabled$' -count=1`

Expected: PASS and no request reaches the local proxy trap.

### Task 2: Apply the opt-out to CI workflows

**Files:**
- Modify: `.github/workflows/test.yml`
- Modify: `.github/workflows/build.yml`

**Interfaces:**
- Consumes: the opt-out implemented in Task 1.
- Produces: `CAMERAIO_SKIP_FFMPEG_DOWNLOAD: "1"` in every CI job environment.

- [x] **Step 1: Add the CI environment variable**

```yaml
env:
  CGO_ENABLED: "0"
  CAMERAIO_SKIP_FFMPEG_DOWNLOAD: "1"
```

Use the existing top-level `env` block in both workflow files.

- [x] **Step 2: Run the focused package tests**

Run: `go test ./internal/pkg -count=1`

Expected: PASS without any FFmpeg network activity.

- [x] **Step 3: Cross-compile the package tests for Windows**

Run: `CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go test -exec /bin/true -run '^$' ./...`

Expected: PASS, proving every updated package and test compiles for the failing runner.

- [x] **Step 4: Run the full suite**

Run: `go test ./... -count=1`

Expected: PASS.
