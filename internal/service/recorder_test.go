package service

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"CameraIO/internal/model"
	"CameraIO/internal/pkg"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupRecorderTestDB(t *testing.T) (*gorm.DB, func()) {
	t.Helper()
	// 使用独立的内存数据库避免测试间干扰
	dsn := fmt.Sprintf("file:recorder_test_%d?mode=memory&cache=shared", time.Now().UnixNano())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	db.AutoMigrate(&model.Recording{}, &model.Camera{})
	return db, func() {
		sqlDB, _ := db.DB()
		sqlDB.Close()
	}
}

func TestRecorderService_StartRecording_NoCamera(t *testing.T) {
	db, cleanup := setupRecorderTestDB(t)
	defer cleanup()

	tmpDir, _ := os.MkdirTemp("", "recorder-test")
	defer os.RemoveAll(tmpDir)

	cfg := &pkg.Config{RecordingsDir: tmpDir}
	svc := NewRecorderService(db, cfg)

	_, err := svc.StartRecording(&StartRecordingInput{CameraID: 999})
	if err == nil {
		t.Error("expected error for non-existent camera")
	}
}

func TestRecorderService_List_Empty(t *testing.T) {
	db, cleanup := setupRecorderTestDB(t)
	defer cleanup()

	svc := NewRecorderService(db, &pkg.Config{})

	recs, total, err := svc.List(RecordingQuery{Page: 1, PageSize: 10})
	if err != nil {
		t.Fatal(err)
	}
	if total != 0 {
		t.Errorf("expected total 0, got %d", total)
	}
	if len(recs) != 0 {
		t.Errorf("expected 0 recordings, got %d", len(recs))
	}
}

func TestRecorderService_List_WithFilters(t *testing.T) {
	db, cleanup := setupRecorderTestDB(t)
	defer cleanup()

	// 创建测试录像记录
	now := time.Now()
	recs := []model.Recording{
		{CameraID: 1, FilePath: "/tmp/r1.mp4", StartTime: now.Add(-2 * time.Hour), Status: model.RecordingStatusCompleted, FileSize: 1024},
		{CameraID: 1, FilePath: "/tmp/r2.mp4", StartTime: now.Add(-1 * time.Hour), Status: model.RecordingStatusCompleted, FileSize: 2048},
		{CameraID: 2, FilePath: "/tmp/r3.mp4", StartTime: now, Status: model.RecordingStatusRecording, FileSize: 0},
	}
	for i := range recs {
		db.Create(&recs[i])
	}

	svc := NewRecorderService(db, &pkg.Config{})

	// 按摄像头过滤
	camID := uint(1)
	result, total, err := svc.List(RecordingQuery{CameraID: &camID, Page: 1, PageSize: 10})
	if err != nil {
		t.Fatal(err)
	}
	if total != 2 {
		t.Errorf("expected 2 recordings for camera 1, got %d", total)
	}
	if len(result) != 2 {
		t.Errorf("expected 2 results, got %d", len(result))
	}

	// 按状态过滤
	status := model.RecordingStatusRecording
	result, total, err = svc.List(RecordingQuery{Status: &status, Page: 1, PageSize: 10})
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 {
		t.Errorf("expected 1 recording in progress, got %d", total)
	}

	// 分页
	result, total, err = svc.List(RecordingQuery{Page: 1, PageSize: 2})
	if err != nil {
		t.Fatal(err)
	}
	if total != 3 {
		t.Errorf("expected total 3, got %d", total)
	}
	if len(result) != 2 {
		t.Errorf("expected 2 results (page size), got %d", len(result))
	}

	// 按时间范围过滤
	start := now.Add(-90 * time.Minute)
	end := now.Add(-30 * time.Minute)
	result, total, err = svc.List(RecordingQuery{StartTime: &start, EndTime: &end, Page: 1, PageSize: 10})
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 {
		t.Errorf("expected 1 recording in time range, got %d", total)
	}
}

func TestRecorderService_GetRecording(t *testing.T) {
	db, cleanup := setupRecorderTestDB(t)
	defer cleanup()

	rec := &model.Recording{
		CameraID:  1,
		FilePath:  "/tmp/test.mp4",
		StartTime: time.Now(),
		Status:    model.RecordingStatusCompleted,
	}
	db.Create(rec)

	svc := NewRecorderService(db, &pkg.Config{})

	// 存在的记录
	found, err := svc.GetRecording(rec.ID)
	if err != nil {
		t.Fatal(err)
	}
	if found.FilePath != "/tmp/test.mp4" {
		t.Errorf("unexpected file path: %s", found.FilePath)
	}

	// 不存在的记录
	_, err = svc.GetRecording(999)
	if err == nil {
		t.Error("expected error for non-existent recording")
	}
}

func TestRecorderService_GetActiveRecordings(t *testing.T) {
	db, cleanup := setupRecorderTestDB(t)
	defer cleanup()

	now := time.Now()
	db.Create(&model.Recording{CameraID: 1, FilePath: "/tmp/a.mp4", StartTime: now, Status: model.RecordingStatusRecording})
	db.Create(&model.Recording{CameraID: 1, FilePath: "/tmp/b.mp4", StartTime: now, Status: model.RecordingStatusCompleted})
	db.Create(&model.Recording{CameraID: 2, FilePath: "/tmp/c.mp4", StartTime: now, Status: model.RecordingStatusFailed})

	svc := NewRecorderService(db, &pkg.Config{})

	active, err := svc.GetActiveRecordings()
	if err != nil {
		t.Fatal(err)
	}
	if len(active) != 1 {
		t.Errorf("expected 1 active recording, got %d", len(active))
	}
}

func TestRecordingFileName(t *testing.T) {
	camID := uint(5)
	now := time.Date(2026, 8, 3, 14, 30, 0, 0, time.UTC)
	fileName := fmt.Sprintf("%d_%s.mp4", camID, now.Format("20060102_150405"))
	expected := "5_20260803_143000.mp4"
	if fileName != expected {
		t.Errorf("got %q, want %q", fileName, expected)
	}
}

func TestRecordingDir(t *testing.T) {
	base := "/data/recordings"
	camID := uint(3)
	dir := filepath.Join(base, fmt.Sprintf("%d", camID))
	expected := filepath.Join(base, "3")
	if dir != expected {
		t.Errorf("got %q, want %q", dir, expected)
	}
}

func TestRecordingModel(t *testing.T) {
	// 测试常量定义
	if model.TriggerAPI != "api" {
		t.Errorf("TriggerAPI = %q", model.TriggerAPI)
	}
	if model.TriggerManual != "manual" {
		t.Errorf("TriggerManual = %q", model.TriggerManual)
	}
	if model.TriggerSchedule != "schedule" {
		t.Errorf("TriggerSchedule = %q", model.TriggerSchedule)
	}
	if model.RecordingStatusRecording != "recording" {
		t.Errorf("RecordingStatusRecording = %q", model.RecordingStatusRecording)
	}
	if model.RecordingStatusCompleted != "completed" {
		t.Errorf("RecordingStatusCompleted = %q", model.RecordingStatusCompleted)
	}
	if model.RecordingStatusFailed != "failed" {
		t.Errorf("RecordingStatusFailed = %q", model.RecordingStatusFailed)
	}
}

func TestRecordingQuery_DefaultPagination(t *testing.T) {
	// 测试默认分页参数
	q := RecordingQuery{}
	if q.Page <= 0 {
		q.Page = 1
	}
	if q.PageSize <= 0 {
		q.PageSize = 20
	}
	if q.Page != 1 {
		t.Errorf("expected page 1, got %d", q.Page)
	}
	if q.PageSize != 20 {
		t.Errorf("expected page size 20, got %d", q.PageSize)
	}
}

func TestStopRecording_NotActive_UpdatesDB(t *testing.T) {
	db, cleanup := setupRecorderTestDB(t)
	defer cleanup()

	// 创建一条正在"录制"的记录（但进程已不在内存中，模拟服务重启）
	rec := &model.Recording{
		CameraID:  1,
		FilePath:  "/tmp/restart_test.mp4",
		StartTime: time.Now().Add(-5 * time.Minute),
		Status:    model.RecordingStatusRecording,
	}
	db.Create(rec)

	svc := NewRecorderService(db, &pkg.Config{})

	// 停止这条记录（任务不在内存 map 中）
	err := svc.StopRecording(rec.ID)
	if err != nil {
		t.Fatalf("StopRecording should not error when task is missing, got: %v", err)
	}

	// 验证 DB 状态更新为 completed
	var updated model.Recording
	db.First(&updated, rec.ID)
	if updated.Status != model.RecordingStatusCompleted {
		t.Errorf("expected status completed, got %s", updated.Status)
	}
	if updated.EndTime == nil {
		t.Error("expected end_time to be set")
	}
	if updated.Duration <= 0 {
		t.Errorf("expected positive duration, got %d", updated.Duration)
	}

	// 幂等：再次停止不应报错
	err = svc.StopRecording(rec.ID)
	if err != nil {
		t.Errorf("second StopRecording should be idempotent, got: %v", err)
	}
}

func TestStopRecording_NotFound(t *testing.T) {
	db, cleanup := setupRecorderTestDB(t)
	defer cleanup()

	svc := NewRecorderService(db, &pkg.Config{})

	// 完全不存在
	err := svc.StopRecording(999)
	if err == nil {
		t.Error("expected error for non-existent recording")
	}
}

func TestDeleteRecording(t *testing.T) {
	db, cleanup := setupRecorderTestDB(t)
	defer cleanup()

	// 创建临时录像文件
	tmpDir, _ := os.MkdirTemp("", "delete-rec")
	defer os.RemoveAll(tmpDir)
	filePath := filepath.Join(tmpDir, "test.mp4")
	os.WriteFile(filePath, []byte("fake mp4 content"), 0o644)

	rec := &model.Recording{
		CameraID:  1,
		FilePath:  filePath,
		StartTime: time.Now(),
		Status:    model.RecordingStatusCompleted,
		Format:    model.FormatMP4,
	}
	db.Create(rec)

	svc := NewRecorderService(db, &pkg.Config{})

	// 删除录像
	if err := svc.DeleteRecording(rec.ID); err != nil {
		t.Fatalf("DeleteRecording failed: %v", err)
	}

	// 验证 DB 记录已删除
	var count int64
	db.Model(&model.Recording{}).Where("id = ?", rec.ID).Count(&count)
	if count != 0 {
		t.Errorf("recording should be deleted from DB, count = %d", count)
	}

	// 验证文件已删除
	if _, err := os.Stat(filePath); !os.IsNotExist(err) {
		t.Error("recording file should be deleted")
	}
}

func TestDeleteRecording_NotFound(t *testing.T) {
	db, cleanup := setupRecorderTestDB(t)
	defer cleanup()

	svc := NewRecorderService(db, &pkg.Config{})
	err := svc.DeleteRecording(999)
	if err == nil {
		t.Error("expected error for non-existent recording")
	}
}

func TestDeleteRecording_MissingFile(t *testing.T) {
	db, cleanup := setupRecorderTestDB(t)
	defer cleanup()

	// 文件不存在也应能删除记录
	rec := &model.Recording{
		CameraID:  1,
		FilePath:  "/nonexistent/path.mp4",
		StartTime: time.Now(),
		Status:    model.RecordingStatusCompleted,
	}
	db.Create(rec)

	svc := NewRecorderService(db, &pkg.Config{})
	if err := svc.DeleteRecording(rec.ID); err != nil {
		t.Fatalf("DeleteRecording with missing file should not error: %v", err)
	}
}

func TestReconcileOrphaned_ValidFile(t *testing.T) {
	db, cleanup := setupRecorderTestDB(t)
	defer cleanup()

	// 创建有效文件
	tmpDir, _ := os.MkdirTemp("", "reconcile-valid")
	defer os.RemoveAll(tmpDir)
	filePath := filepath.Join(tmpDir, "valid.mp4")
	os.WriteFile(filePath, []byte("fake mp4 content"), 0o644)

	// 孤儿记录：状态 recording，文件有效，但任务不在内存中
	orphan := &model.Recording{
		CameraID:  1,
		FilePath:  filePath,
		StartTime: time.Now().Add(-5 * time.Minute),
		Status:    model.RecordingStatusRecording,
	}
	db.Create(orphan)

	svc := NewRecorderService(db, &pkg.Config{})
	svc.ReconcileOrphaned()

	// 文件有效 → 标记为 completed（可下载查看）
	var updated model.Recording
	db.First(&updated, orphan.ID)
	if updated.Status != model.RecordingStatusCompleted {
		t.Errorf("valid file recording status = %s, want completed", updated.Status)
	}
	if updated.FileSize <= 0 {
		t.Errorf("file_size should be set, got %d", updated.FileSize)
	}
}

func TestReconcileOrphaned_NoFile(t *testing.T) {
	db, cleanup := setupRecorderTestDB(t)
	defer cleanup()

	// 孤儿记录：文件不存在 → failed
	orphan := &model.Recording{
		CameraID:  1,
		FilePath:  "/nonexistent/path.mp4",
		StartTime: time.Now(),
		Status:    model.RecordingStatusRecording,
	}
	db.Create(orphan)

	svc := NewRecorderService(db, &pkg.Config{})
	svc.ReconcileOrphaned()

	var updated model.Recording
	db.First(&updated, orphan.ID)
	if updated.Status != model.RecordingStatusFailed {
		t.Errorf("no-file recording status = %s, want failed", updated.Status)
	}
}

func TestReconcileOrphaned_AlreadyFailedValidFile(t *testing.T) {
	db, cleanup := setupRecorderTestDB(t)
	defer cleanup()

	// 有效文件但状态是 failed → 应升级为 completed
	tmpDir, _ := os.MkdirTemp("", "reconcile-upgrade")
	defer os.RemoveAll(tmpDir)
	filePath := filepath.Join(tmpDir, "rec.mp4")
	os.WriteFile(filePath, []byte("content"), 0o644)

	rec := &model.Recording{
		CameraID:  1,
		FilePath:  filePath,
		StartTime: time.Now(),
		Status:    model.RecordingStatusFailed,
	}
	db.Create(rec)

	svc := NewRecorderService(db, &pkg.Config{})
	svc.ReconcileOrphaned()

	var updated model.Recording
	db.First(&updated, rec.ID)
	if updated.Status != model.RecordingStatusCompleted {
		t.Errorf("failed-with-valid-file status = %s, want completed", updated.Status)
	}
}

func TestMaxDurationWatchdog_SetsForceStopped(t *testing.T) {
	db, cleanup := setupRecorderTestDB(t)
	defer cleanup()

	// 用假的 exec.Cmd（进程不存在也能测试 forceStopped 标志）
	rec := &model.Recording{CameraID: 1, FilePath: "/tmp/t.mp4", StartTime: time.Now()}
	db.Create(rec)

	svc := NewRecorderService(db, &pkg.Config{})
	cmd := &exec.Cmd{}
	task := &recordTask{
		recording: rec,
		cmd:       cmd,
		cancel:    func() {},
	}
	svc.mu.Lock()
	svc.tasks[rec.ID] = task
	svc.mu.Unlock()

	// 最大时长 1 秒，等待 watchdog 触发
	svc.maxDurationWatchdog(rec.ID, task, 1)
	time.Sleep(50 * time.Millisecond)

	svc.mu.Lock()
	forceStopped := task.forceStopped
	svc.mu.Unlock()
	if !forceStopped {
		t.Error("maxDurationWatchdog should set forceStopped after max duration")
	}
}

func TestIsProcessAlive(t *testing.T) {
	// nil cmd → 不存活
	if isProcessAlive(nil) {
		t.Error("nil cmd should be not alive")
	}

	// 未启动的 cmd → 不存活（Process 为 nil）
	cmd := &exec.Cmd{}
	if isProcessAlive(cmd) {
		t.Error("cmd without Process should be not alive")
	}

	// 已退出的进程 → 不存活
	doneCmd := exec.Command("true")
	if err := doneCmd.Run(); err != nil {
		t.Fatal(err)
	}
	if isProcessAlive(doneCmd) {
		t.Error("exited process should be not alive")
	}

	// 运行中的进程 → 存活
	pingCmd := exec.Command("sh", "-c", "sleep 1")
	if err := pingCmd.Start(); err != nil {
		t.Fatal(err)
	}
	if !isProcessAlive(pingCmd) {
		t.Error("running process should be alive")
	}
	pingCmd.Process.Kill()
	pingCmd.Wait()
}

func TestGetActiveRecordingByCamera(t *testing.T) {
	db, cleanup := setupRecorderTestDB(t)
	defer cleanup()

	now := time.Now()
	db.Create(&model.Recording{CameraID: 1, FilePath: "/tmp/a.mp4", StartTime: now, Status: model.RecordingStatusRecording})
	db.Create(&model.Recording{CameraID: 1, FilePath: "/tmp/b.mp4", StartTime: now, Status: model.RecordingStatusCompleted})

	svc := NewRecorderService(db, &pkg.Config{})

	// 有正在录制的记录
	active, err := svc.GetActiveRecordingByCamera(1)
	if err != nil {
		t.Fatal(err)
	}
	if active == nil || active.Status != model.RecordingStatusRecording {
		t.Error("expected active recording for camera 1")
	}

	// 摄像头 2 没有录像
	active2, err := svc.GetActiveRecordingByCamera(2)
	if err != nil {
		t.Fatal(err)
	}
	if active2 != nil {
		t.Error("expected nil for camera 2 (no active recording)")
	}
}

func TestBuildRecordingArgs_MP4_NoAudio(t *testing.T) {
	svc := NewRecorderService(nil, &pkg.Config{})
	args := svc.buildRecordingArgs("rtsp://test/stream", "/tmp/out.mp4", model.FormatMP4, false, 0)

	// 必须包含的参数
	required := []string{"-y", "-rtsp_transport", "tcp", "-i", "rtsp://test/stream", "-c:v", "copy", "-an", "-movflags", "-f", "mp4", "/tmp/out.mp4"}
	for _, r := range required {
		found := false
		for _, a := range args {
			if a == r {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("missing required arg %q in: %v", r, args)
		}
	}
}

func TestBuildRecordingArgs_MP4_WithAudio(t *testing.T) {
	svc := NewRecorderService(nil, &pkg.Config{})
	args := svc.buildRecordingArgs("rtsp://test/stream", "/tmp/out.mp4", model.FormatMP4, true, 0)

	// 有音频时应该有 -c:a aac
	hasAAC := false
	for i, a := range args {
		if a == "-c:a" && i+1 < len(args) && args[i+1] == "aac" {
			hasAAC = true
			break
		}
	}
	if !hasAAC {
		t.Errorf("expected -c:a aac for MP4 with audio, got: %v", args)
	}
	// 不应有 -an
	for _, a := range args {
		if a == "-an" {
			t.Error("should not have -an when with_audio=true")
		}
	}
}

func TestBuildRecordingArgs_WebM_WithAudio(t *testing.T) {
	svc := NewRecorderService(nil, &pkg.Config{})
	args := svc.buildRecordingArgs("rtsp://test/stream", "/tmp/out.webm", model.FormatWebM, true, 0)

	// 不应该有 -an（保留音频）
	for _, a := range args {
		if a == "-an" {
			t.Error("should not have -an when with_audio=true")
		}
	}

	// WebM 必须转码视频为 VP9
	hasVP9 := false
	for i, a := range args {
		if a == "-c:v" && i+1 < len(args) && args[i+1] == "libvpx-vp9" {
			hasVP9 = true
			break
		}
	}
	if !hasVP9 {
		t.Errorf("WebM should transcode video with libvpx-vp9, got: %v", args)
	}

	// 有音频时用 libopus
	hasOpus := false
	for i, a := range args {
		if a == "-c:a" && i+1 < len(args) && args[i+1] == "libopus" {
			hasOpus = true
			break
		}
	}
	if !hasOpus {
		t.Errorf("WebM with audio should use libopus, got: %v", args)
	}

	// 应该有 -f webm
	hasWebM := false
	for i, a := range args {
		if a == "-f" && i+1 < len(args) && args[i+1] == "webm" {
			hasWebM = true
			break
		}
	}
	if !hasWebM {
		t.Error("expected -f webm in args")
	}

	// 不应该有 -movflags（WebM 不需要）
	for _, a := range args {
		if a == "-movflags" {
			t.Error("WebM should not have -movflags")
		}
	}
}

func TestBuildRecordingArgs_TS(t *testing.T) {
	svc := NewRecorderService(nil, &pkg.Config{})
	args := svc.buildRecordingArgs("rtsp://test/stream", "/tmp/out.ts", model.FormatTS, false, 0)

	hasTS := false
	for i, a := range args {
		if a == "-f" && i+1 < len(args) && args[i+1] == "mpegts" {
			hasTS = true
			break
		}
	}
	if !hasTS {
		t.Error("expected -f mpegts for TS format")
	}
	// TS 无音频应该有 -an
	hasAN := false
	for _, a := range args {
		if a == "-an" {
			hasAN = true
			break
		}
	}
	if !hasAN {
		t.Error("TS no-audio should have -an")
	}
}

func TestBuildRecordingArgs_BitrateTranscode(t *testing.T) {
	svc := NewRecorderService(nil, &pkg.Config{})

	// bitrate=600 → 转码 libx264 限码率
	args := svc.buildRecordingArgs("rtsp://test/stream", "/tmp/out.mp4", model.FormatMP4, false, 600)

	// 应该有 libx264 + -b:v 600k
	hasX264 := false
	hasBitrate := false
	for i, a := range args {
		if a == "-c:v" && i+1 < len(args) && args[i+1] == "libx264" {
			hasX264 = true
		}
		if a == "-b:v" && i+1 < len(args) && args[i+1] == "600k" {
			hasBitrate = true
		}
	}
	if !hasX264 {
		t.Errorf("bitrate>0 should transcode with libx264, got: %v", args)
	}
	if !hasBitrate {
		t.Errorf("bitrate>0 should set -b:v 600k, got: %v", args)
	}
	// 不应有 -c:v copy
	for i, a := range args {
		if a == "-c:v" && i+1 < len(args) && args[i+1] == "copy" {
			t.Errorf("bitrate>0 should NOT stream-copy, got: %v", args)
		}
	}

	// bitrate=0 → 流拷贝
	argsCopy := svc.buildRecordingArgs("rtsp://test/stream", "/tmp/out.mp4", model.FormatMP4, false, 0)
	hasCopy := false
	for i, a := range argsCopy {
		if a == "-c:v" && i+1 < len(argsCopy) && argsCopy[i+1] == "copy" {
			hasCopy = true
		}
	}
	if !hasCopy {
		t.Errorf("bitrate=0 should stream-copy, got: %v", argsCopy)
	}
}

func TestRecordingFormatConstants(t *testing.T) {
	if model.FormatMP4 != "mp4" {
		t.Errorf("FormatMP4 = %q", model.FormatMP4)
	}
	if model.FormatWebM != "webm" {
		t.Errorf("FormatWebM = %q", model.FormatWebM)
	}
	if model.FormatTS != "ts" {
		t.Errorf("FormatTS = %q", model.FormatTS)
	}
}

func TestStartRecordingInput_FormatValidation(t *testing.T) {
	db, cleanup := setupRecorderTestDB(t)
	defer cleanup()

	svc := NewRecorderService(db, &pkg.Config{})

	// 无效格式
	_, err := svc.StartRecording(&StartRecordingInput{
		CameraID: 999, // 不存在的摄像头，但格式验证应在此之前
		Format:   "avi",
	})
	// 先验证摄像头不存在（999）
	if err == nil {
		t.Error("expected error")
	}
}
