package service

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"CameraIO/internal/model"
	"CameraIO/internal/pkg"

	"gorm.io/gorm"
)

func TestBuildSegmentRecordingArgsCopiesVideoAndSegmentsMP4(t *testing.T) {
	args := buildSegmentRecordingArgs("rtsp://camera/live", "/archive/%Y%m%dT%H%M%S.mp4", 300, false)
	joined := strings.Join(args, " ")
	for _, want := range []string{"-c:v copy", "-an", "-f segment", "-segment_time 300", "-segment_atclocktime 1", "-reset_timestamps 1", "-strftime 1", "+frag_keyframe+empty_moov"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing %q in %s", want, joined)
		}
	}
	for _, forbidden := range []string{"libx264", "libvpx", "-vf", "-r "} {
		if strings.Contains(joined, forbidden) {
			t.Fatalf("unsafe transcode option %q", forbidden)
		}
	}
}

func TestBuildSegmentRecordingArgsCopiesOnlyProbedAACAudio(t *testing.T) {
	joined := strings.Join(buildSegmentRecordingArgs("rtsp://camera/live", "/archive/out.mp4", 300, true), " ")
	for _, want := range []string{"-map 0:a:0?", "-c:a copy"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing %q in %s", want, joined)
		}
	}
	for _, forbidden := range []string{"-an", "-c:a aac", "libopus"} {
		if strings.Contains(joined, forbidden) {
			t.Fatalf("unexpected audio option %q in %s", forbidden, joined)
		}
	}
}

func TestSegmentSupervisorStopUsesSingleWaitAndIngestsFinalFragment(t *testing.T) {
	db, cleanup := setupRecorderTestDB(t)
	defer cleanup()

	sessionDir := t.TempDir()
	finalPath := filepath.Join(sessionDir, "20260808T120000Z.mp4")
	qPath := filepath.Join(t.TempDir(), "stdin.txt")
	recording := &model.Recording{
		CameraID:    9,
		FilePath:    sessionDir,
		StartTime:   time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC),
		Status:      model.RecordingStatusRecording,
		Format:      model.FormatMP4,
		StorageMode: model.StorageModeSegmented,
	}
	if err := db.Create(recording).Error; err != nil {
		t.Fatalf("create recording: %v", err)
	}

	supervisor := &segmentSupervisor{
		db:         db,
		recording:  recording,
		sessionDir: sessionDir,
		newCommand: func(_ string, _ ...string) *exec.Cmd {
			cmd := exec.Command(os.Args[0], "-test.run=TestSegmentFFmpegProcessHelper", "--")
			cmd.Env = append(os.Environ(),
				"CAMERAIO_SEGMENT_HELPER=1",
				"CAMERAIO_SEGMENT_Q_PATH="+qPath,
				"CAMERAIO_SEGMENT_FINAL_PATH="+finalPath,
			)
			return cmd
		},
		probeDuration: func(_ context.Context, path string) (time.Duration, error) {
			if path != finalPath {
				return 0, fmt.Errorf("unexpected path %s", path)
			}
			return 1250 * time.Millisecond, nil
		},
	}
	if err := supervisor.start(); err != nil {
		t.Fatalf("start supervisor: %v", err)
	}
	t.Cleanup(func() {
		if supervisor.cmd != nil && supervisor.cmd.Process != nil {
			_ = supervisor.cmd.Process.Kill()
		}
	})

	started := time.Now()
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() { errs <- supervisor.stop(context.Background()) }()
	}
	for i := 0; i < 2; i++ {
		if err := <-errs; err != nil {
			t.Fatalf("stop supervisor: %v", err)
		}
	}
	elapsed := time.Since(started)
	if elapsed < 3*time.Second {
		t.Fatalf("stop killed FFmpeg before the three-second grace period: %v", elapsed)
	}
	if elapsed > 4*time.Second {
		t.Fatalf("stop exceeded the bounded grace period: %v", elapsed)
	}

	qBytes, err := os.ReadFile(qPath)
	if err != nil {
		t.Fatalf("read captured stdin: %v", err)
	}
	if string(qBytes) != "q\n" {
		t.Fatalf("FFmpeg stdin = %q, want %q", qBytes, "q\\n")
	}
	assertSegmentRows(t, db, recording.ID, 1)
	select {
	case <-supervisor.done:
	default:
		t.Fatal("stop returned before the sole Wait owner observed process exit")
	}
}

func TestSegmentFFmpegProcessHelper(t *testing.T) {
	if os.Getenv("CAMERAIO_SEGMENT_HELPER") != "1" {
		return
	}
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil {
		return
	}
	if err := os.WriteFile(os.Getenv("CAMERAIO_SEGMENT_Q_PATH"), []byte(line), 0o644); err != nil {
		return
	}
	if err := os.WriteFile(os.Getenv("CAMERAIO_SEGMENT_FINAL_PATH"), []byte("playable fragment"), 0o644); err != nil {
		return
	}
	for {
		time.Sleep(time.Second)
	}
}

func TestSegmentSupervisorStopReapsProcessAfterBrokenStdin(t *testing.T) {
	db, cleanup := setupRecorderTestDB(t)
	defer cleanup()
	sessionDir := t.TempDir()
	recording := &model.Recording{
		CameraID:    10,
		FilePath:    sessionDir,
		StartTime:   time.Now().UTC(),
		Status:      model.RecordingStatusRecording,
		Format:      model.FormatMP4,
		StorageMode: model.StorageModeSegmented,
	}
	if err := db.Create(recording).Error; err != nil {
		t.Fatalf("create recording: %v", err)
	}
	readyPath := filepath.Join(t.TempDir(), "ready")
	supervisor := &segmentSupervisor{
		db:         db,
		recording:  recording,
		sessionDir: sessionDir,
		newCommand: func(_ string, _ ...string) *exec.Cmd {
			cmd := exec.Command(os.Args[0], "-test.run=TestBrokenStdinSegmentFFmpegProcessHelper", "--")
			cmd.Env = append(os.Environ(), "CAMERAIO_BROKEN_STDIN_SEGMENT_HELPER=1", "CAMERAIO_SEGMENT_READY_PATH="+readyPath)
			return cmd
		},
	}
	if err := supervisor.start(); err != nil {
		t.Fatalf("start supervisor: %v", err)
	}
	t.Cleanup(func() {
		if supervisor.cmd != nil && supervisor.cmd.Process != nil {
			_ = supervisor.cmd.Process.Kill()
		}
	})
	deadline := time.Now().Add(time.Second)
	for {
		if _, err := os.Stat(readyPath); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("helper did not close stdin")
		}
		time.Sleep(10 * time.Millisecond)
	}

	started := time.Now()
	err := supervisor.stop(context.Background())
	if err == nil || !strings.Contains(err.Error(), "request ffmpeg stop") {
		t.Fatalf("stop error = %v, want broken-stdin error", err)
	}
	if elapsed := time.Since(started); elapsed < 3*time.Second {
		t.Fatalf("broken stdin returned before child was reaped: %v", elapsed)
	}
	select {
	case <-supervisor.done:
	default:
		t.Fatal("broken-stdin stop returned before Wait completed")
	}
}

func TestBrokenStdinSegmentFFmpegProcessHelper(t *testing.T) {
	if os.Getenv("CAMERAIO_BROKEN_STDIN_SEGMENT_HELPER") != "1" {
		return
	}
	_ = os.Stdin.Close()
	if err := os.WriteFile(os.Getenv("CAMERAIO_SEGMENT_READY_PATH"), []byte("ready"), 0o644); err != nil {
		return
	}
	for {
		time.Sleep(time.Second)
	}
}

func TestRecorderRejectsUnsafeSegmentedRecordingOptions(t *testing.T) {
	db, cleanup := setupRecorderTestDB(t)
	defer cleanup()
	camera := &model.Camera{Name: "front", RTSPUrl: "rtsp://camera/live", AccessProtocol: model.ProtocolRTSP}
	if err := db.Create(camera).Error; err != nil {
		t.Fatalf("create camera: %v", err)
	}
	svc := NewRecorderService(db, pkg.DefaultConfig())

	for _, test := range []struct {
		name  string
		input StartRecordingInput
		want  string
	}{
		{"webm", StartRecordingInput{CameraID: camera.ID, Format: model.FormatWebM}, "webm recordings are not supported"},
		{"bitrate", StartRecordingInput{CameraID: camera.ID, Format: model.FormatMP4, Bitrate: 600}, "bitrate must be 0"},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := svc.StartRecording(&test.input)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("StartRecording error = %v, want containing %q", err, test.want)
			}
		})
	}
	var count int64
	if err := db.Model(&model.Recording{}).Count(&count).Error; err != nil {
		t.Fatalf("count recordings: %v", err)
	}
	if count != 0 {
		t.Fatalf("unsafe requests created %d recording rows", count)
	}
}

func TestRecorderStartsRTSPMP4AsSegmentedSession(t *testing.T) {
	db, cleanup := setupRecorderTestDB(t)
	defer cleanup()
	recordingsDir := t.TempDir()
	camera := &model.Camera{Name: "front", RTSPUrl: "rtsp://camera/live", AccessProtocol: model.ProtocolRTSP}
	if err := db.Create(camera).Error; err != nil {
		t.Fatalf("create camera: %v", err)
	}
	cfg := pkg.DefaultConfig()
	cfg.RecordingsDir = recordingsDir
	svc := NewRecorderService(db, cfg)
	svc.segmentCommand = func(_ string, _ ...string) *exec.Cmd {
		cmd := exec.Command(os.Args[0], "-test.run=TestCooperativeSegmentFFmpegProcessHelper", "--")
		cmd.Env = append(os.Environ(), "CAMERAIO_COOPERATIVE_SEGMENT_HELPER=1")
		return cmd
	}

	recording, err := svc.StartRecording(&StartRecordingInput{CameraID: camera.ID, Format: model.FormatMP4})
	if err != nil {
		t.Fatalf("StartRecording: %v", err)
	}
	wantDir := filepath.Join(recordingsDir, fmt.Sprint(camera.ID), fmt.Sprint(recording.ID))
	if recording.StorageMode != model.StorageModeSegmented {
		t.Errorf("storage mode = %q, want %q", recording.StorageMode, model.StorageModeSegmented)
	}
	if recording.FilePath != wantDir {
		t.Errorf("session path = %q, want %q", recording.FilePath, wantDir)
	}
	if info, err := os.Stat(wantDir); err != nil || !info.IsDir() {
		t.Fatalf("session directory was not created: info=%v err=%v", info, err)
	}
	svc.mu.Lock()
	task := svc.tasks[recording.ID]
	svc.mu.Unlock()
	if task == nil || task.segmenter == nil {
		t.Fatal("public recording ID was not routed to a segment supervisor")
	}
	if err := svc.StopRecording(recording.ID); err != nil {
		t.Fatalf("StopRecording: %v", err)
	}
}

func TestRecorderConcurrentSegmentStopsShareSupervisorLifecycle(t *testing.T) {
	db, cleanup := setupRecorderTestDB(t)
	defer cleanup()
	recordingsDir := t.TempDir()
	qPath := filepath.Join(t.TempDir(), "stdin.txt")
	camera := &model.Camera{Name: "front", RTSPUrl: "rtsp://camera/live", AccessProtocol: model.ProtocolRTSP}
	if err := db.Create(camera).Error; err != nil {
		t.Fatalf("create camera: %v", err)
	}
	cfg := pkg.DefaultConfig()
	cfg.RecordingsDir = recordingsDir
	svc := NewRecorderService(db, cfg)
	var finalPath string
	svc.segmentCommand = func(_ string, args ...string) *exec.Cmd {
		pattern := args[len(args)-1]
		finalPath = strings.ReplaceAll(pattern, "%Y%m%dT%H%M%SZ", "20260808T120000Z")
		cmd := exec.Command(os.Args[0], "-test.run=TestSegmentFFmpegProcessHelper", "--")
		cmd.Env = append(os.Environ(),
			"CAMERAIO_SEGMENT_HELPER=1",
			"CAMERAIO_SEGMENT_Q_PATH="+qPath,
			"CAMERAIO_SEGMENT_FINAL_PATH="+finalPath,
		)
		return cmd
	}
	recording, err := svc.StartRecording(&StartRecordingInput{CameraID: camera.ID, Format: model.FormatMP4})
	if err != nil {
		t.Fatalf("StartRecording: %v", err)
	}
	svc.mu.Lock()
	svc.tasks[recording.ID].segmenter.probeDuration = func(_ context.Context, path string) (time.Duration, error) {
		if path != finalPath {
			return 0, fmt.Errorf("unexpected path %s", path)
		}
		return time.Second, nil
	}
	svc.mu.Unlock()

	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() { errs <- svc.StopRecording(recording.ID) }()
	}
	deadline := time.Now().Add(time.Second)
	for {
		if _, err := os.Stat(qPath); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("segmented stop did not write q")
		}
		time.Sleep(10 * time.Millisecond)
	}
	time.Sleep(100 * time.Millisecond)
	var duringStop model.Recording
	if err := db.First(&duringStop, recording.ID).Error; err != nil {
		t.Fatalf("load recording during stop: %v", err)
	}
	if duringStop.Status != model.RecordingStatusRecording {
		t.Fatalf("concurrent stop bypassed supervisor and finalized early: status=%q", duringStop.Status)
	}
	for i := 0; i < 2; i++ {
		if err := <-errs; err != nil {
			t.Fatalf("StopRecording: %v", err)
		}
	}
}

func TestCooperativeSegmentFFmpegProcessHelper(t *testing.T) {
	if os.Getenv("CAMERAIO_COOPERATIVE_SEGMENT_HELPER") != "1" {
		return
	}
	_, _ = bufio.NewReader(os.Stdin).ReadString('\n')
}

func TestSegmentSupervisorScanCompletedExcludesOpenFileAndAggregatesIdempotently(t *testing.T) {
	db, cleanup := setupRecorderTestDB(t)
	defer cleanup()

	sessionDir := t.TempDir()
	files := []struct {
		name     string
		contents string
		duration time.Duration
	}{
		{"20260808T120000Z.mp4", "first", 5 * time.Second},
		{"20260808T120500Z.mp4", "second-fragment", 6 * time.Second},
		{"20260808T121000Z.mp4", "newest-fragment", 7 * time.Second},
	}
	durations := make(map[string]time.Duration, len(files))
	var totalSize int64
	for _, file := range files {
		path := filepath.Join(sessionDir, file.name)
		if err := os.WriteFile(path, []byte(file.contents), 0o644); err != nil {
			t.Fatalf("write %s: %v", file.name, err)
		}
		durations[path] = file.duration
		totalSize += int64(len(file.contents))
	}

	recording := &model.Recording{
		CameraID:    7,
		FilePath:    sessionDir,
		StartTime:   time.Date(2026, 8, 8, 19, 0, 0, 0, time.FixedZone("UTC+07", 7*60*60)),
		Status:      model.RecordingStatusRecording,
		Format:      model.FormatMP4,
		StorageMode: model.StorageModeSegmented,
	}
	if err := db.Create(recording).Error; err != nil {
		t.Fatalf("create recording: %v", err)
	}

	supervisor := &segmentSupervisor{
		db:         db,
		recording:  recording,
		sessionDir: sessionDir,
		probeDuration: func(_ context.Context, path string) (time.Duration, error) {
			duration, ok := durations[path]
			if !ok {
				return 0, fmt.Errorf("unexpected path %s", path)
			}
			return duration, nil
		},
	}

	if err := supervisor.scanCompleted(false); err != nil {
		t.Fatalf("non-final scan: %v", err)
	}
	assertSegmentRows(t, db, recording.ID, 2)

	if err := supervisor.scanCompleted(false); err != nil {
		t.Fatalf("duplicate non-final scan: %v", err)
	}
	assertSegmentRows(t, db, recording.ID, 2)

	if err := supervisor.scanCompleted(true); err != nil {
		t.Fatalf("final scan: %v", err)
	}
	assertSegmentRows(t, db, recording.ID, 3)

	var segments []model.RecordingSegment
	if err := db.Order("sequence").Find(&segments, "recording_id = ?", recording.ID).Error; err != nil {
		t.Fatalf("load segments: %v", err)
	}
	for index, segment := range segments {
		if segment.Sequence != index+1 {
			t.Errorf("segment %d sequence = %d, want %d", index, segment.Sequence, index+1)
		}
		if segment.StartTime.Location() != time.UTC || segment.EndTime.Location() != time.UTC || segment.CreatedAt.Location() != time.UTC {
			t.Errorf("segment %d times are not all UTC: start=%s end=%s created=%s", index, segment.StartTime.Location(), segment.EndTime.Location(), segment.CreatedAt.Location())
		}
	}

	var aggregate model.Recording
	if err := db.First(&aggregate, recording.ID).Error; err != nil {
		t.Fatalf("load aggregate: %v", err)
	}
	if aggregate.FileSize != totalSize {
		t.Errorf("aggregate file size = %d, want %d", aggregate.FileSize, totalSize)
	}
	if aggregate.Duration != 18 {
		t.Errorf("aggregate duration = %d, want 18", aggregate.Duration)
	}
	if aggregate.StartTime.Location() != time.UTC || aggregate.EndTime == nil || aggregate.EndTime.Location() != time.UTC {
		t.Errorf("aggregate times are not UTC: start=%s end=%v", aggregate.StartTime.Location(), aggregate.EndTime)
	}
}

func assertSegmentRows(t *testing.T, db *gorm.DB, recordingID uint, want int64) {
	t.Helper()
	var count int64
	if err := db.Model(&model.RecordingSegment{}).Where("recording_id = ?", recordingID).Count(&count).Error; err != nil {
		t.Fatalf("count segments: %v", err)
	}
	if count != want {
		t.Fatalf("segment count = %d, want %d", count, want)
	}
}
