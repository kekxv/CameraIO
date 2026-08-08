package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"CameraIO/internal/model"
	"CameraIO/internal/pkg"
)

func TestReconcileSegmentsRecoversUnindexedPlayableSegment(t *testing.T) {
	db, cleanup := setupRecorderTestDB(t)
	defer cleanup()

	root := t.TempDir()
	recording := &model.Recording{
		CameraID:    7,
		StartTime:   time.Date(2026, 8, 8, 11, 55, 0, 0, time.UTC),
		Status:      model.RecordingStatusCompleted,
		Format:      model.FormatMP4,
		StorageMode: model.StorageModeSegmented,
	}
	if err := db.Create(recording).Error; err != nil {
		t.Fatalf("create recording: %v", err)
	}
	sessionDir := filepath.Join(root, "7", fmt.Sprint(recording.ID))
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatalf("create session directory: %v", err)
	}
	if err := db.Model(recording).Update("file_path", sessionDir).Error; err != nil {
		t.Fatalf("store session directory: %v", err)
	}
	segmentPath := filepath.Join(sessionDir, "20260808T120000Z.mp4")
	contents := []byte("playable recovered fragment")
	if err := os.WriteFile(segmentPath, contents, 0o644); err != nil {
		t.Fatalf("write segment: %v", err)
	}

	cfg := pkg.DefaultConfig()
	cfg.RecordingsDir = root
	svc := NewRecorderService(db, cfg)
	svc.segmentProbe = func(_ context.Context, path string) (time.Duration, error) {
		if path != segmentPath {
			return 0, fmt.Errorf("unexpected probe path %s", path)
		}
		return 2500 * time.Millisecond, nil
	}

	if err := svc.ReconcileSegments(); err != nil {
		t.Fatalf("ReconcileSegments: %v", err)
	}

	var segment model.RecordingSegment
	if err := db.Where("file_path = ?", segmentPath).First(&segment).Error; err != nil {
		t.Fatalf("load recovered segment: %v", err)
	}
	wantStart := time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC)
	if segment.Status != model.RecordingStatusCompleted || segment.FileSize != int64(len(contents)) || segment.DurationMS != 2500 {
		t.Fatalf("recovered segment = status %q size %d duration %d", segment.Status, segment.FileSize, segment.DurationMS)
	}
	if !segment.StartTime.Equal(wantStart) || !segment.EndTime.Equal(wantStart.Add(2500*time.Millisecond)) {
		t.Fatalf("recovered times = %s..%s", segment.StartTime, segment.EndTime)
	}

	var aggregate model.Recording
	if err := db.First(&aggregate, recording.ID).Error; err != nil {
		t.Fatalf("load aggregate: %v", err)
	}
	if aggregate.FileSize != int64(len(contents)) || aggregate.Duration != 3 {
		t.Fatalf("aggregate = size %d duration %d", aggregate.FileSize, aggregate.Duration)
	}
}

func TestReconcileSegmentsQuarantinesZeroBytePartial(t *testing.T) {
	db, cleanup := setupRecorderTestDB(t)
	defer cleanup()

	root := t.TempDir()
	recording := &model.Recording{
		CameraID:    8,
		StartTime:   time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC),
		Status:      model.RecordingStatusCompleted,
		Format:      model.FormatMP4,
		StorageMode: model.StorageModeSegmented,
	}
	if err := db.Create(recording).Error; err != nil {
		t.Fatalf("create recording: %v", err)
	}
	sessionDir := filepath.Join(root, "8", fmt.Sprint(recording.ID))
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatalf("create session directory: %v", err)
	}
	if err := db.Model(recording).Update("file_path", sessionDir).Error; err != nil {
		t.Fatalf("store session directory: %v", err)
	}
	segmentPath := filepath.Join(sessionDir, "20260808T120000Z.mp4")
	if err := os.WriteFile(segmentPath, nil, 0o644); err != nil {
		t.Fatalf("write partial segment: %v", err)
	}

	cfg := pkg.DefaultConfig()
	cfg.RecordingsDir = root
	svc := NewRecorderService(db, cfg)
	probeCalls := 0
	svc.segmentProbe = func(_ context.Context, path string) (time.Duration, error) {
		probeCalls++
		return 0, fmt.Errorf("not playable: %s", path)
	}

	if err := svc.ReconcileSegments(); err != nil {
		t.Fatalf("ReconcileSegments: %v", err)
	}
	if probeCalls != 1 {
		t.Fatalf("probe calls = %d, want 1", probeCalls)
	}
	var segment model.RecordingSegment
	if err := db.Where("file_path = ?", segmentPath).First(&segment).Error; err != nil {
		t.Fatalf("load quarantined segment: %v", err)
	}
	if segment.Status != model.RecordingStatusFailed || segment.FileSize != 0 || segment.DurationMS != 0 {
		t.Fatalf("quarantined segment = status %q size %d duration %d", segment.Status, segment.FileSize, segment.DurationMS)
	}
	if info, err := os.Stat(segmentPath); err != nil || info.Size() != 0 {
		t.Fatalf("partial segment was destructively changed: info=%v err=%v", info, err)
	}
	var aggregate model.Recording
	if err := db.First(&aggregate, recording.ID).Error; err != nil {
		t.Fatalf("load aggregate: %v", err)
	}
	if aggregate.FileSize != 0 || aggregate.Duration != 0 {
		t.Fatalf("aggregate = size %d duration %d", aggregate.FileSize, aggregate.Duration)
	}
}

func TestReconcileSegmentsMarksMissingIndexedFileFailed(t *testing.T) {
	db, cleanup := setupRecorderTestDB(t)
	defer cleanup()

	root := t.TempDir()
	recording := &model.Recording{
		CameraID:    9,
		FileSize:    1234,
		Duration:    60,
		StartTime:   time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC),
		Status:      model.RecordingStatusCompleted,
		Format:      model.FormatMP4,
		StorageMode: model.StorageModeSegmented,
	}
	if err := db.Create(recording).Error; err != nil {
		t.Fatalf("create recording: %v", err)
	}
	sessionDir := filepath.Join(root, "9", fmt.Sprint(recording.ID))
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatalf("create session directory: %v", err)
	}
	if err := db.Model(recording).Update("file_path", sessionDir).Error; err != nil {
		t.Fatalf("store session directory: %v", err)
	}
	missingPath := filepath.Join(sessionDir, "20260808T120000Z.mp4")
	segment := &model.RecordingSegment{
		RecordingID: recording.ID,
		CameraID:    recording.CameraID,
		Sequence:    1,
		FilePath:    missingPath,
		FileSize:    1234,
		StartTime:   recording.StartTime,
		EndTime:     recording.StartTime.Add(time.Minute),
		DurationMS:  int64(time.Minute / time.Millisecond),
		Status:      model.RecordingStatusCompleted,
		Format:      model.FormatMP4,
	}
	if err := db.Create(segment).Error; err != nil {
		t.Fatalf("create indexed segment: %v", err)
	}

	cfg := pkg.DefaultConfig()
	cfg.RecordingsDir = root
	svc := NewRecorderService(db, cfg)
	if err := svc.ReconcileSegments(); err != nil {
		t.Fatalf("ReconcileSegments: %v", err)
	}

	var reconciled model.RecordingSegment
	if err := db.First(&reconciled, segment.ID).Error; err != nil {
		t.Fatalf("reload segment: %v", err)
	}
	if reconciled.Status != model.RecordingStatusFailed {
		t.Fatalf("missing segment status = %q, want failed", reconciled.Status)
	}
	var aggregate model.Recording
	if err := db.First(&aggregate, recording.ID).Error; err != nil {
		t.Fatalf("load aggregate: %v", err)
	}
	if aggregate.FileSize != 0 || aggregate.Duration != 0 {
		t.Fatalf("aggregate = size %d duration %d", aggregate.FileSize, aggregate.Duration)
	}
}

func TestReconcileSegmentsFinalizesOrphanRecordingSession(t *testing.T) {
	db, cleanup := setupRecorderTestDB(t)
	defer cleanup()

	root := t.TempDir()
	recording := &model.Recording{
		CameraID:    10,
		StartTime:   time.Date(2026, 8, 8, 11, 50, 0, 0, time.UTC),
		Status:      model.RecordingStatusRecording,
		Format:      model.FormatMP4,
		StorageMode: model.StorageModeSegmented,
	}
	if err := db.Create(recording).Error; err != nil {
		t.Fatalf("create recording: %v", err)
	}
	sessionDir := filepath.Join(root, "10", fmt.Sprint(recording.ID))
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatalf("create session directory: %v", err)
	}
	if err := db.Model(recording).Update("file_path", sessionDir).Error; err != nil {
		t.Fatalf("store session directory: %v", err)
	}
	segmentPath := filepath.Join(sessionDir, "20260808T120000Z.mp4")
	contents := []byte("surviving orphan fragment")
	if err := os.WriteFile(segmentPath, contents, 0o644); err != nil {
		t.Fatalf("write segment: %v", err)
	}

	cfg := pkg.DefaultConfig()
	cfg.RecordingsDir = root
	svc := NewRecorderService(db, cfg)
	svc.segmentProbe = func(_ context.Context, _ string) (time.Duration, error) {
		return 4 * time.Second, nil
	}
	if err := svc.ReconcileSegments(); err != nil {
		t.Fatalf("ReconcileSegments: %v", err)
	}

	var aggregate model.Recording
	if err := db.First(&aggregate, recording.ID).Error; err != nil {
		t.Fatalf("load aggregate: %v", err)
	}
	if aggregate.Status != model.RecordingStatusCompleted {
		t.Fatalf("orphan status = %q, want completed", aggregate.Status)
	}
	wantStart := time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC)
	if !aggregate.StartTime.Equal(wantStart) || aggregate.EndTime == nil || !aggregate.EndTime.Equal(wantStart.Add(4*time.Second)) {
		t.Fatalf("aggregate times = %s..%v", aggregate.StartTime, aggregate.EndTime)
	}
	if aggregate.FileSize != int64(len(contents)) || aggregate.Duration != 4 {
		t.Fatalf("aggregate = size %d duration %d", aggregate.FileSize, aggregate.Duration)
	}
}

func TestReconcileSegmentsAssignsUnusedSequenceToCrashFragment(t *testing.T) {
	db, cleanup := setupRecorderTestDB(t)
	defer cleanup()
	root := t.TempDir()
	recording := &model.Recording{CameraID: 16, StartTime: time.Now().UTC(), Status: model.RecordingStatusCompleted, Format: model.FormatMP4, StorageMode: model.StorageModeSegmented}
	if err := db.Create(recording).Error; err != nil {
		t.Fatal(err)
	}
	sessionDir := filepath.Join(root, "16", fmt.Sprint(recording.ID))
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(recording).Update("file_path", sessionDir).Error; err != nil {
		t.Fatal(err)
	}
	firstPath := filepath.Join(sessionDir, "20260808T120000Z.mp4")
	knownPath := filepath.Join(sessionDir, "20260808T120500Z.mp4")
	for _, path := range []string{firstPath, knownPath} {
		if err := os.WriteFile(path, []byte("playable"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	start := time.Date(2026, 8, 8, 12, 5, 0, 0, time.UTC)
	known := &model.RecordingSegment{RecordingID: recording.ID, CameraID: 16, Sequence: 1, FilePath: knownPath, FileSize: 8, StartTime: start, EndTime: start.Add(time.Second), DurationMS: 1000, Status: model.RecordingStatusCompleted, Format: model.FormatMP4}
	if err := db.Create(known).Error; err != nil {
		t.Fatal(err)
	}
	cfg := pkg.DefaultConfig()
	cfg.RecordingsDir = root
	svc := NewRecorderService(db, cfg)
	svc.segmentProbe = func(context.Context, string) (time.Duration, error) { return time.Second, nil }
	if err := svc.ReconcileSegments(); err != nil {
		t.Fatalf("ReconcileSegments: %v", err)
	}
	assertSegmentRows(t, db, recording.ID, 2)
}

func TestRecordingRetentionRemovesExpiredCompletedSegments(t *testing.T) {
	db, cleanup := setupRecorderTestDB(t)
	defer cleanup()

	root := t.TempDir()
	now := time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC)
	recording := &model.Recording{
		CameraID:    11,
		StartTime:   now.Add(-40 * 24 * time.Hour),
		Status:      model.RecordingStatusCompleted,
		Format:      model.FormatMP4,
		StorageMode: model.StorageModeSegmented,
	}
	if err := db.Create(recording).Error; err != nil {
		t.Fatalf("create recording: %v", err)
	}
	sessionDir := filepath.Join(root, "11", fmt.Sprint(recording.ID))
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatalf("create session directory: %v", err)
	}
	if err := db.Model(recording).Update("file_path", sessionDir).Error; err != nil {
		t.Fatalf("store session directory: %v", err)
	}

	oldPath := filepath.Join(sessionDir, "20260629T120000Z.mp4")
	recentPath := filepath.Join(sessionDir, "20260807T120000Z.mp4")
	for sequence, fixture := range []struct {
		path  string
		start time.Time
	}{
		{oldPath, now.Add(-40 * 24 * time.Hour)},
		{recentPath, now.Add(-24 * time.Hour)},
	} {
		if err := os.WriteFile(fixture.path, []byte("segment"), 0o644); err != nil {
			t.Fatalf("write segment: %v", err)
		}
		segment := &model.RecordingSegment{
			RecordingID: recording.ID,
			CameraID:    recording.CameraID,
			Sequence:    sequence + 1,
			FilePath:    fixture.path,
			FileSize:    7,
			StartTime:   fixture.start,
			EndTime:     fixture.start.Add(time.Minute),
			DurationMS:  int64(time.Minute / time.Millisecond),
			Status:      model.RecordingStatusCompleted,
			Format:      model.FormatMP4,
		}
		if err := db.Create(segment).Error; err != nil {
			t.Fatalf("create segment: %v", err)
		}
	}

	cfg := pkg.DefaultConfig()
	cfg.RecordingsDir = root
	cfg.RecordingRetentionDays = 30
	svc := NewRecorderService(db, cfg)
	svc.retentionNow = func() time.Time { return now }
	svc.diskStat = func(path string) (diskUsage, error) {
		if path != root {
			t.Fatalf("disk stat path = %q, want %q", path, root)
		}
		return diskUsage{Total: 100, Free: 80}, nil
	}

	if err := svc.RunRetentionOnce(); err != nil {
		t.Fatalf("RunRetentionOnce: %v", err)
	}
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Fatalf("expired segment still exists: %v", err)
	}
	if _, err := os.Stat(recentPath); err != nil {
		t.Fatalf("recent segment was removed: %v", err)
	}
	var oldCount, recentCount int64
	if err := db.Model(&model.RecordingSegment{}).Where("file_path = ?", oldPath).Count(&oldCount).Error; err != nil {
		t.Fatalf("count old segment: %v", err)
	}
	if err := db.Model(&model.RecordingSegment{}).Where("file_path = ?", recentPath).Count(&recentCount).Error; err != nil {
		t.Fatalf("count recent segment: %v", err)
	}
	if oldCount != 0 || recentCount != 1 {
		t.Fatalf("segment rows = old %d recent %d", oldCount, recentCount)
	}
}

func TestRecordingRetentionWatermarkRemovesOldestFirstAndRebuildsAggregate(t *testing.T) {
	db, cleanup := setupRecorderTestDB(t)
	defer cleanup()

	root := t.TempDir()
	now := time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC)
	recording := &model.Recording{
		CameraID:    12,
		StartTime:   now.Add(-3 * time.Hour),
		Status:      model.RecordingStatusCompleted,
		Format:      model.FormatMP4,
		StorageMode: model.StorageModeSegmented,
	}
	if err := db.Create(recording).Error; err != nil {
		t.Fatalf("create recording: %v", err)
	}
	sessionDir := filepath.Join(root, "12", fmt.Sprint(recording.ID))
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatalf("create session directory: %v", err)
	}
	if err := db.Model(recording).Update("file_path", sessionDir).Error; err != nil {
		t.Fatalf("store session directory: %v", err)
	}
	paths := make([]string, 3)
	for index := range paths {
		start := now.Add(time.Duration(index-3) * time.Hour)
		paths[index] = filepath.Join(sessionDir, start.Format("20060102T150405Z")+".mp4")
		contents := []byte(fmt.Sprintf("segment-%d", index+1))
		if err := os.WriteFile(paths[index], contents, 0o644); err != nil {
			t.Fatalf("write segment: %v", err)
		}
		segment := &model.RecordingSegment{
			RecordingID: recording.ID,
			CameraID:    recording.CameraID,
			Sequence:    index + 1,
			FilePath:    paths[index],
			FileSize:    int64(len(contents)),
			StartTime:   start,
			EndTime:     start.Add(time.Minute),
			DurationMS:  int64(time.Minute / time.Millisecond),
			Status:      model.RecordingStatusCompleted,
			Format:      model.FormatMP4,
		}
		if err := db.Create(segment).Error; err != nil {
			t.Fatalf("create segment: %v", err)
		}
	}

	cfg := pkg.DefaultConfig()
	cfg.RecordingsDir = root
	cfg.RecordingRetentionDays = 365
	cfg.RecordingCleanupFreePercent = 15
	svc := NewRecorderService(db, cfg)
	svc.retentionNow = func() time.Time { return now }
	statCalls := 0
	svc.diskStat = func(_ string) (diskUsage, error) {
		statCalls++
		if statCalls == 1 {
			return diskUsage{Total: 100, Free: 15}, nil
		}
		return diskUsage{Total: 100, Free: 16}, nil
	}

	if err := svc.RunRetentionOnce(); err != nil {
		t.Fatalf("RunRetentionOnce: %v", err)
	}
	if statCalls != 2 {
		t.Fatalf("disk stat calls = %d, want 2", statCalls)
	}
	if _, err := os.Stat(paths[0]); !os.IsNotExist(err) {
		t.Fatalf("oldest segment still exists: %v", err)
	}
	for _, path := range paths[1:] {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("newer segment %s was removed: %v", path, err)
		}
	}
	var aggregate model.Recording
	if err := db.First(&aggregate, recording.ID).Error; err != nil {
		t.Fatalf("load aggregate: %v", err)
	}
	wantSize := int64(len("segment-2") + len("segment-3"))
	wantStart := now.Add(-2 * time.Hour)
	wantEnd := now.Add(-time.Hour).Add(time.Minute)
	if aggregate.FileSize != wantSize || aggregate.Duration != 120 || !aggregate.StartTime.Equal(wantStart) || aggregate.EndTime == nil || !aggregate.EndTime.Equal(wantEnd) {
		t.Fatalf("aggregate = size %d duration %d times %s..%v", aggregate.FileSize, aggregate.Duration, aggregate.StartTime, aggregate.EndTime)
	}
}

func TestRecordingRetentionNeverDeletesActiveOrNewestSegment(t *testing.T) {
	db, cleanup := setupRecorderTestDB(t)
	defer cleanup()

	root := t.TempDir()
	now := time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC)
	type fixture struct {
		recording *model.Recording
		paths     []string
	}
	makeFixture := func(cameraID uint, status string, starts []time.Time) fixture {
		t.Helper()
		recording := &model.Recording{
			CameraID:    cameraID,
			StartTime:   starts[0],
			Status:      status,
			Format:      model.FormatMP4,
			StorageMode: model.StorageModeSegmented,
		}
		if err := db.Create(recording).Error; err != nil {
			t.Fatalf("create recording: %v", err)
		}
		sessionDir := filepath.Join(root, fmt.Sprint(cameraID), fmt.Sprint(recording.ID))
		if err := os.MkdirAll(sessionDir, 0o755); err != nil {
			t.Fatalf("create session directory: %v", err)
		}
		if err := db.Model(recording).Update("file_path", sessionDir).Error; err != nil {
			t.Fatalf("store session directory: %v", err)
		}
		result := fixture{recording: recording, paths: make([]string, len(starts))}
		for index, start := range starts {
			path := filepath.Join(sessionDir, start.Format("20060102T150405Z")+".mp4")
			result.paths[index] = path
			if err := os.WriteFile(path, []byte("segment"), 0o644); err != nil {
				t.Fatalf("write segment: %v", err)
			}
			segment := &model.RecordingSegment{
				RecordingID: recording.ID,
				CameraID:    cameraID,
				Sequence:    index + 1,
				FilePath:    path,
				FileSize:    7,
				StartTime:   start,
				EndTime:     start.Add(time.Minute),
				DurationMS:  int64(time.Minute / time.Millisecond),
				Status:      model.RecordingStatusCompleted,
				Format:      model.FormatMP4,
			}
			if err := db.Create(segment).Error; err != nil {
				t.Fatalf("create segment: %v", err)
			}
		}
		return result
	}
	active := makeFixture(13, model.RecordingStatusRecording, []time.Time{now.Add(-4 * time.Hour)})
	completed := makeFixture(14, model.RecordingStatusCompleted, []time.Time{now.Add(-3 * time.Hour), now.Add(-2 * time.Hour)})

	cfg := pkg.DefaultConfig()
	cfg.RecordingsDir = root
	cfg.RecordingRetentionDays = 365
	cfg.RecordingCleanupFreePercent = 15
	svc := NewRecorderService(db, cfg)
	svc.retentionNow = func() time.Time { return now }
	svc.diskStat = func(_ string) (diskUsage, error) {
		return diskUsage{Total: 100, Free: 1}, nil
	}

	if err := svc.RunRetentionOnce(); err != nil {
		t.Fatalf("RunRetentionOnce: %v", err)
	}
	if _, err := os.Stat(active.paths[0]); err != nil {
		t.Fatalf("active recording segment was removed: %v", err)
	}
	if _, err := os.Stat(completed.paths[0]); !os.IsNotExist(err) {
		t.Fatalf("eligible older segment still exists: %v", err)
	}
	if _, err := os.Stat(completed.paths[1]); err != nil {
		t.Fatalf("newest completed segment was removed: %v", err)
	}
}

func TestLowDiskRejectsNewRecordingAndPublishesSystemEvent(t *testing.T) {
	db, cleanup := setupRecorderTestDB(t)
	defer cleanup()

	root := t.TempDir()
	camera := &model.Camera{Name: "low-space", RTSPUrl: "rtsp://camera/live", AccessProtocol: model.ProtocolRTSP}
	if err := db.Create(camera).Error; err != nil {
		t.Fatalf("create camera: %v", err)
	}
	cfg := pkg.DefaultConfig()
	cfg.RecordingsDir = root
	cfg.RecordingStopFreePercent = 5
	svc := NewRecorderService(db, cfg)
	svc.diskStat = func(path string) (diskUsage, error) {
		if path != root {
			t.Fatalf("disk stat path = %q, want %q", path, root)
		}
		return diskUsage{Total: 100, Free: 5}, nil
	}
	events := NewEventBus()
	client := events.NewClient("low-disk-test")
	defer events.RemoveClient(client)
	svc.SetEventBus(events)

	_, err := svc.StartRecording(&StartRecordingInput{CameraID: camera.ID, Format: model.FormatMP4})
	var lowDisk *LowDiskError
	if !errors.As(err, &lowDisk) {
		t.Fatalf("StartRecording error = %v, want LowDiskError", err)
	}
	var recordingCount int64
	if err := db.Model(&model.Recording{}).Count(&recordingCount).Error; err != nil {
		t.Fatalf("count recordings: %v", err)
	}
	if recordingCount != 0 {
		t.Fatalf("low-disk rejection created %d recording rows", recordingCount)
	}
	if _, err := os.Stat(filepath.Join(root, fmt.Sprint(camera.ID))); !os.IsNotExist(err) {
		t.Fatalf("low-disk rejection created camera directory: %v", err)
	}

	select {
	case payload := <-client.Send:
		var event struct {
			Type string `json:"type"`
			Data struct {
				Code     string `json:"code"`
				CameraID uint   `json:"camera_id"`
			} `json:"data"`
		}
		if err := json.Unmarshal(payload, &event); err != nil {
			t.Fatalf("decode event: %v", err)
		}
		if event.Type != "system_event" || event.Data.Code != "recording_low_disk" || event.Data.CameraID != camera.ID {
			t.Fatalf("low-disk event = type %q code %q camera %d", event.Type, event.Data.Code, event.Data.CameraID)
		}
	case <-time.After(time.Second):
		t.Fatal("low-disk system event was not published")
	}
}

func TestRecordingDiskStatReportsFilesystemCapacity(t *testing.T) {
	usage, err := statDiskUsage(t.TempDir())
	if err != nil {
		t.Fatalf("statDiskUsage: %v", err)
	}
	if usage.Total == 0 || usage.Free == 0 || usage.Free > usage.Total {
		t.Fatalf("disk usage = total %d free %d", usage.Total, usage.Free)
	}
}

func TestRecordingRetentionLoopRunsWithRecorderLifecycle(t *testing.T) {
	db, cleanup := setupRecorderTestDB(t)
	defer cleanup()

	cfg := pkg.DefaultConfig()
	cfg.RecordingsDir = t.TempDir()
	svc := NewRecorderService(db, cfg)
	svc.retentionInterval = time.Millisecond
	called := make(chan struct{}, 1)
	svc.diskStat = func(_ string) (diskUsage, error) {
		select {
		case called <- struct{}{}:
		default:
		}
		return diskUsage{Total: 100, Free: 80}, nil
	}
	svc.StartRetention()
	defer svc.Shutdown()

	select {
	case <-called:
	case <-time.After(time.Second):
		t.Fatal("retention loop did not run")
	}
}

func TestRecordingRetentionRefusesSymlinkEscapeOutsideRoot(t *testing.T) {
	db, cleanup := setupRecorderTestDB(t)
	defer cleanup()

	root := t.TempDir()
	outside := t.TempDir()
	linkDir := filepath.Join(root, "linked-outside")
	if err := os.Symlink(outside, linkDir); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	now := time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC)
	recording := &model.Recording{
		CameraID:    15,
		FilePath:    filepath.Join(root, "15", "1"),
		StartTime:   now.Add(-10 * 24 * time.Hour),
		Status:      model.RecordingStatusCompleted,
		Format:      model.FormatMP4,
		StorageMode: model.StorageModeSegmented,
	}
	if err := db.Create(recording).Error; err != nil {
		t.Fatalf("create recording: %v", err)
	}
	escapedPath := filepath.Join(linkDir, "escaped.mp4")
	if err := os.WriteFile(escapedPath, []byte("outside must survive"), 0o644); err != nil {
		t.Fatalf("write outside file: %v", err)
	}
	safeDir := filepath.Join(root, "15", fmt.Sprint(recording.ID))
	if err := os.MkdirAll(safeDir, 0o755); err != nil {
		t.Fatalf("create safe session directory: %v", err)
	}
	safeNewest := filepath.Join(safeDir, "newest.mp4")
	if err := os.WriteFile(safeNewest, []byte("newest"), 0o644); err != nil {
		t.Fatalf("write newest segment: %v", err)
	}
	for index, fixture := range []struct {
		path  string
		start time.Time
	}{
		{escapedPath, now.Add(-10 * 24 * time.Hour)},
		{safeNewest, now.Add(-9 * 24 * time.Hour)},
	} {
		segment := &model.RecordingSegment{
			RecordingID: recording.ID,
			CameraID:    recording.CameraID,
			Sequence:    index + 1,
			FilePath:    fixture.path,
			FileSize:    7,
			StartTime:   fixture.start,
			EndTime:     fixture.start.Add(time.Minute),
			DurationMS:  int64(time.Minute / time.Millisecond),
			Status:      model.RecordingStatusCompleted,
			Format:      model.FormatMP4,
		}
		if err := db.Create(segment).Error; err != nil {
			t.Fatalf("create segment: %v", err)
		}
	}

	cfg := pkg.DefaultConfig()
	cfg.RecordingsDir = root
	cfg.RecordingRetentionDays = 1
	svc := NewRecorderService(db, cfg)
	svc.retentionNow = func() time.Time { return now }
	svc.diskStat = func(_ string) (diskUsage, error) {
		return diskUsage{Total: 100, Free: 80}, nil
	}
	if err := svc.RunRetentionOnce(); err == nil {
		t.Fatal("retention followed a symlink outside the recordings root")
	}
	contents, err := os.ReadFile(filepath.Join(outside, "escaped.mp4"))
	if err != nil || string(contents) != "outside must survive" {
		t.Fatalf("outside file changed: contents=%q err=%v", contents, err)
	}
	var count int64
	if err := db.Model(&model.RecordingSegment{}).Where("file_path = ?", escapedPath).Count(&count).Error; err != nil {
		t.Fatalf("count escaped row: %v", err)
	}
	if count != 1 {
		t.Fatalf("escaped segment rows = %d, want 1", count)
	}
}
