package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"CameraIO/internal/model"
	"CameraIO/internal/pkg"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type segmentDurationProbe func(context.Context, string) (time.Duration, error)
type segmentCommandFactory func(string, ...string) *exec.Cmd

type segmentProbeInfrastructureError struct {
	err error
}

func (e *segmentProbeInfrastructureError) Error() string { return e.err.Error() }
func (e *segmentProbeInfrastructureError) Unwrap() error { return e.err }

func isSegmentProbeInfrastructureError(err error) bool {
	if err == nil {
		return false
	}
	var infrastructureErr *segmentProbeInfrastructureError
	var execErr *exec.Error
	return errors.As(err, &infrastructureErr) || errors.Is(err, exec.ErrNotFound) || errors.As(err, &execErr) || os.IsNotExist(err)
}

const segmentStopGracePeriod = 3 * time.Second

type segmentSupervisor struct {
	db            *gorm.DB
	recording     *model.Recording
	sessionDir    string
	probeDuration segmentDurationProbe
	ffmpegPath    string
	args          []string
	newCommand    segmentCommandFactory

	mu           sync.Mutex
	cmd          *exec.Cmd
	stdin        io.WriteCloser
	stderr       bytes.Buffer
	done         chan struct{}
	scanWG       sync.WaitGroup
	stopOnce     sync.Once
	stopFinished chan struct{}
	stopErr      error
}

func buildSegmentRecordingArgs(rtspURL, outputPattern string, segmentSeconds int, withAACAudio bool) []string {
	args := []string{
		"-y",
		"-rtsp_transport", "tcp",
		"-i", rtspURL,
		"-map", "0:v:0",
		"-c:v", "copy",
	}
	if withAACAudio {
		args = append(args, "-map", "0:a:0?", "-c:a", "copy")
	} else {
		args = append(args, "-an")
	}
	return append(args,
		"-f", "segment",
		"-segment_time", strconv.Itoa(segmentSeconds),
		"-segment_atclocktime", "1",
		"-reset_timestamps", "1",
		"-strftime", "1",
		"-segment_format", "mp4",
		"-segment_format_options", "movflags=+frag_keyframe+empty_moov",
		outputPattern,
	)
}

func (s *segmentSupervisor) start() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cmd != nil {
		return fmt.Errorf("segment ffmpeg already started")
	}
	command := s.newCommand
	if command == nil {
		command = exec.Command
	}
	ffmpegPath := s.ffmpegPath
	if ffmpegPath == "" {
		ffmpegPath = pkg.FFmpegBinPath()
	}
	cmd := command(ffmpegPath, s.args...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("open ffmpeg stdin: %w", err)
	}
	cmd.Stderr = &s.stderr
	cmd.Env = append(cmd.Environ(), "TZ=UTC")
	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		return fmt.Errorf("start ffmpeg: %w", err)
	}
	if err := pkg.LowerRecordingProcessPriority(cmd.Process.Pid); err != nil {
		log.Printf("[recorder] recording %d could not lower archive segmenter priority: %v", s.recording.ID, err)
	}
	s.cmd = cmd
	s.stdin = stdin
	s.done = make(chan struct{})
	s.stopFinished = make(chan struct{})
	scanCtx, scanCancel := context.WithCancel(context.Background())

	s.scanWG.Add(1)
	go s.waitForProcess(cmd, scanCancel)
	go func() {
		defer s.scanWG.Done()
		s.scanLoop(scanCtx)
	}()
	return nil
}

func (s *segmentSupervisor) waitForProcess(cmd *exec.Cmd, scanCancel context.CancelFunc) {
	_ = cmd.Wait()
	scanCancel()
	s.scanWG.Wait()
	s.mu.Lock()
	close(s.done)
	s.mu.Unlock()
}

func (s *segmentSupervisor) scanLoop(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := s.scanCompleted(false); err != nil {
				log.Printf("[recorder] scan recording %d segments: %v", s.recording.ID, err)
			}
		}
	}
}

func (s *segmentSupervisor) stop(ctx context.Context) error {
	s.mu.Lock()
	if s.cmd == nil {
		s.mu.Unlock()
		return fmt.Errorf("segment ffmpeg not started")
	}
	finished := s.stopFinished
	s.mu.Unlock()

	s.stopOnce.Do(func() {
		go func() {
			s.stopErr = s.stopProcess()
			close(finished)
		}()
	})
	select {
	case <-finished:
		return s.stopErr
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *segmentSupervisor) stopProcess() error {
	s.mu.Lock()
	stdin := s.stdin
	done := s.done
	cmd := s.cmd
	s.mu.Unlock()

	var requestErr error
	if _, err := io.WriteString(stdin, "q\n"); err != nil {
		requestErr = fmt.Errorf("request ffmpeg stop: %w", err)
	}
	timer := time.NewTimer(segmentStopGracePeriod)
	defer timer.Stop()
	select {
	case <-done:
	case <-timer.C:
		if cmd.Process != nil {
			if err := cmd.Process.Kill(); err != nil && !errorsIsProcessDone(err) {
				return errors.Join(requestErr, fmt.Errorf("kill ffmpeg after grace period: %w", err))
			}
		}
		<-done
	}
	_ = stdin.Close()
	return requestErr
}

func errorsIsProcessDone(err error) bool {
	return errors.Is(err, os.ErrProcessDone)
}

func (s *segmentSupervisor) scanCompleted(final bool) error {
	entries, err := os.ReadDir(s.sessionDir)
	if err != nil {
		return fmt.Errorf("read segment directory: %w", err)
	}
	paths := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".mp4") {
			continue
		}
		paths = append(paths, filepath.Join(s.sessionDir, entry.Name()))
	}
	sort.Strings(paths)
	if !final && len(paths) > 0 {
		paths = paths[:len(paths)-1]
	}
	var knownPaths []string
	if err := s.db.Model(&model.RecordingSegment{}).
		Where("recording_id = ?", s.recording.ID).
		Pluck("file_path", &knownPaths).Error; err != nil {
		return fmt.Errorf("load known recording segments: %w", err)
	}
	known := make(map[string]struct{}, len(knownPaths))
	for _, path := range knownPaths {
		known[path] = struct{}{}
	}

	probe := s.probeDuration
	if probe == nil {
		probe = probeSegmentDuration
	}
	segments := make([]model.RecordingSegment, 0, len(paths))
	for sequence, path := range paths {
		if _, indexed := known[path]; indexed {
			continue
		}
		start, err := segmentStartTime(path)
		if err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		duration, err := probe(ctx, path)
		cancel()
		if err != nil {
			return fmt.Errorf("probe segment %s: %w", path, err)
		}
		if duration <= 0 {
			return fmt.Errorf("probe segment %s: non-positive duration", path)
		}
		info, err := os.Stat(path)
		if err != nil {
			return fmt.Errorf("stat segment %s: %w", path, err)
		}
		segments = append(segments, model.RecordingSegment{
			RecordingID: s.recording.ID,
			CameraID:    s.recording.CameraID,
			Sequence:    sequence + 1,
			FilePath:    path,
			FileSize:    info.Size(),
			StartTime:   start.UTC(),
			EndTime:     start.Add(duration).UTC(),
			DurationMS:  duration.Milliseconds(),
			Status:      model.RecordingStatusCompleted,
			Format:      model.FormatMP4,
		})
	}
	if len(segments) == 0 && len(knownPaths) == 0 {
		return nil
	}
	return s.storeSegments(segments)
}

func (s *segmentSupervisor) storeSegments(segments []model.RecordingSegment) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		if len(segments) > 0 {
			result := tx.Clauses(clause.OnConflict{
				Columns:   []clause.Column{{Name: "file_path"}},
				DoNothing: true,
			}).Create(&segments)
			if result.Error != nil {
				return fmt.Errorf("store segment: %w", result.Error)
			}
		}

		var sums struct {
			FileSize   int64
			DurationMS int64
		}
		if err := tx.Model(&model.RecordingSegment{}).
			Select("COALESCE(SUM(file_size), 0) AS file_size, COALESCE(SUM(duration_ms), 0) AS duration_ms").
			Where("recording_id = ?", s.recording.ID).
			Scan(&sums).Error; err != nil {
			return fmt.Errorf("sum recording segments: %w", err)
		}
		var first, last model.RecordingSegment
		if err := tx.Where("recording_id = ?", s.recording.ID).Order("start_time ASC").First(&first).Error; err != nil {
			return fmt.Errorf("load first segment: %w", err)
		}
		if err := tx.Where("recording_id = ?", s.recording.ID).Order("end_time DESC").First(&last).Error; err != nil {
			return fmt.Errorf("load last segment: %w", err)
		}
		return tx.Model(&model.Recording{}).Where("id = ?", s.recording.ID).Updates(map[string]any{
			"file_size":  sums.FileSize,
			"duration":   int((sums.DurationMS + 999) / 1000),
			"start_time": first.StartTime.UTC(),
			"end_time":   last.EndTime.UTC(),
		}).Error
	})
}

func segmentStartTime(path string) (time.Time, error) {
	name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	for _, layout := range []string{"20060102T150405Z", "20060102T150405"} {
		if parsed, err := time.Parse(layout, name); err == nil {
			return parsed.UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("parse segment timestamp %q", filepath.Base(path))
}

func probeSegmentDuration(ctx context.Context, path string) (time.Duration, error) {
	status := pkg.GetFFmpegStatus()
	if status.State == "downloading" || status.State == "extracting" || status.State == "checking" {
		return 0, &segmentProbeInfrastructureError{err: fmt.Errorf("ffprobe unavailable while FFmpeg state is %s", status.State)}
	}
	cmd := exec.CommandContext(ctx, pkg.FFprobeBinPath(),
		"-v", "error",
		"-show_entries", "format=duration",
		"-of", "default=noprint_wrappers=1:nokey=1",
		path,
	)
	out, err := cmd.Output()
	if err != nil {
		if isSegmentProbeInfrastructureError(err) {
			return 0, &segmentProbeInfrastructureError{err: err}
		}
		return 0, err
	}
	seconds, err := strconv.ParseFloat(strings.TrimSpace(string(out)), 64)
	if err != nil || seconds <= 0 {
		return 0, fmt.Errorf("invalid duration %q", strings.TrimSpace(string(out)))
	}
	return time.Duration(seconds * float64(time.Second)), nil
}

func probeRTSPAAC(ctx context.Context, rtspURL string) (bool, error) {
	cmd := exec.CommandContext(ctx, pkg.FFprobeBinPath(),
		"-v", "error",
		"-rtsp_transport", "tcp",
		"-select_streams", "a:0",
		"-show_entries", "stream=codec_name",
		"-of", "default=noprint_wrappers=1:nokey=1",
		rtspURL,
	)
	out, err := cmd.Output()
	if err != nil {
		return false, err
	}
	return strings.EqualFold(strings.TrimSpace(string(out)), "aac"), nil
}
