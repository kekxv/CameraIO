package service

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"CameraIO/internal/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type diskUsage struct {
	Total uint64
	Free  uint64
}

// LowDiskError reports that the hard-stop watermark rejected a new archive
// recording before any database row or output directory was created.
type LowDiskError struct {
	FreePercent float64
	StopPercent int
}

func (e *LowDiskError) Error() string {
	return fmt.Sprintf("recording rejected: disk free %.2f%% is at or below %d%% stop watermark", e.FreePercent, e.StopPercent)
}

func (s *RecorderService) checkRecordingAdmission(cameraID uint) error {
	if s.diskStat == nil {
		return nil
	}
	root, err := filepath.Abs(s.cfg.RecordingsDir)
	if err != nil {
		return fmt.Errorf("resolve recordings root: %w", err)
	}
	usage, err := s.diskStat(root)
	if err != nil {
		return fmt.Errorf("stat recordings disk: %w", err)
	}
	if usage.Total == 0 {
		return fmt.Errorf("stat recordings disk: total bytes is zero")
	}
	stopPercent := s.cfg.RecordingStopFreePercent
	if stopPercent <= 0 {
		stopPercent = 5
	}
	freePercent := diskFreePercent(usage)
	if freePercent > float64(stopPercent) {
		return nil
	}
	if s.events != nil {
		s.events.Publish(&Event{
			Type:      "system_event",
			Timestamp: time.Now().UTC(),
			Data: map[string]any{
				"code":         "recording_low_disk",
				"camera_id":    cameraID,
				"free_percent": freePercent,
				"stop_percent": stopPercent,
			},
		})
	}
	return &LowDiskError{FreePercent: freePercent, StopPercent: stopPercent}
}

// ReconcileSegments recovers physical segmented MP4 files after an unclean
// shutdown. Discovery is intentionally limited to the configured
// recordings/<camera-id>/<recording-id> directory shape.
func (s *RecorderService) ReconcileSegments() error {
	root, err := filepath.Abs(s.cfg.RecordingsDir)
	if err != nil {
		return fmt.Errorf("resolve recordings root: %w", err)
	}
	cameraEntries, err := os.ReadDir(root)
	if err != nil {
		return fmt.Errorf("read recordings root: %w", err)
	}
	for _, cameraEntry := range cameraEntries {
		if !cameraEntry.IsDir() {
			continue
		}
		cameraID, err := strconv.ParseUint(cameraEntry.Name(), 10, 64)
		if err != nil || cameraID == 0 {
			continue
		}
		cameraDir := filepath.Join(root, cameraEntry.Name())
		sessionEntries, err := os.ReadDir(cameraDir)
		if err != nil {
			return fmt.Errorf("read camera recording directory %s: %w", cameraDir, err)
		}
		for _, sessionEntry := range sessionEntries {
			if !sessionEntry.IsDir() {
				continue
			}
			recordingID, err := strconv.ParseUint(sessionEntry.Name(), 10, 64)
			if err != nil || recordingID == 0 {
				continue
			}
			var recording model.Recording
			if err := s.db.Where("id = ? AND camera_id = ? AND storage_mode = ?", recordingID, cameraID, model.StorageModeSegmented).First(&recording).Error; err != nil {
				if err == gorm.ErrRecordNotFound {
					continue
				}
				return fmt.Errorf("load recording session %d: %w", recordingID, err)
			}
			sessionDir := filepath.Join(cameraDir, sessionEntry.Name())
			storedDir, err := filepath.Abs(recording.FilePath)
			if err != nil || filepath.Clean(storedDir) != filepath.Clean(sessionDir) {
				continue
			}
			if err := s.reconcileSegmentDirectory(&recording, sessionDir); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *RecorderService) reconcileSegmentDirectory(recording *model.Recording, sessionDir string) error {
	entries, err := os.ReadDir(sessionDir)
	if err != nil {
		return fmt.Errorf("read recording session %d: %w", recording.ID, err)
	}
	paths := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".mp4") {
			continue
		}
		paths = append(paths, filepath.Join(sessionDir, entry.Name()))
	}
	sort.Strings(paths)
	var indexed []model.RecordingSegment
	if err := s.db.Where("recording_id = ?", recording.ID).Find(&indexed).Error; err != nil {
		return fmt.Errorf("load recording %d segments: %w", recording.ID, err)
	}
	indexedByPath := make(map[string]*model.RecordingSegment, len(indexed))
	usedSequences := make(map[int]struct{}, len(indexed))
	for index := range indexed {
		segment := &indexed[index]
		usedSequences[segment.Sequence] = struct{}{}
		if filepath.Clean(filepath.Dir(segment.FilePath)) != filepath.Clean(sessionDir) ||
			!strings.EqualFold(filepath.Ext(segment.FilePath), ".mp4") {
			continue
		}
		indexedByPath[segment.FilePath] = segment
		if _, err := os.Lstat(segment.FilePath); os.IsNotExist(err) {
			if err := s.db.Model(segment).Updates(map[string]any{
				"file_size":   0,
				"duration_ms": 0,
				"status":      model.RecordingStatusFailed,
			}).Error; err != nil {
				return fmt.Errorf("mark missing segment %s failed: %w", segment.FilePath, err)
			}
		} else if err != nil {
			return fmt.Errorf("inspect indexed segment %s: %w", segment.FilePath, err)
		}
	}

	probe := s.segmentProbe
	if probe == nil {
		probe = probeSegmentDuration
	}
	for index, path := range paths {
		start, err := segmentStartTime(path)
		if err != nil {
			return err
		}
		info, err := os.Stat(path)
		if err != nil {
			return fmt.Errorf("stat recovered segment %s: %w", path, err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		duration, probeErr := probe(ctx, path)
		cancel()
		status := model.RecordingStatusCompleted
		if info.Size() == 0 || probeErr != nil || duration <= 0 {
			status = model.RecordingStatusFailed
			duration = 0
		}
		sequence := index + 1
		if _, used := usedSequences[sequence]; used && indexedByPath[path] == nil {
			sequence = 1
			for {
				if _, used := usedSequences[sequence]; !used {
					break
				}
				sequence++
			}
		}
		segment := model.RecordingSegment{
			RecordingID: recording.ID,
			CameraID:    recording.CameraID,
			Sequence:    sequence,
			FilePath:    path,
			FileSize:    info.Size(),
			StartTime:   start.UTC(),
			EndTime:     start.Add(duration).UTC(),
			DurationMS:  duration.Milliseconds(),
			Status:      status,
			Format:      model.FormatMP4,
		}
		if known := indexedByPath[path]; known != nil {
			if err := s.db.Model(known).Updates(map[string]any{
				"file_size":   segment.FileSize,
				"start_time":  segment.StartTime,
				"end_time":    segment.EndTime,
				"duration_ms": segment.DurationMS,
				"status":      segment.Status,
			}).Error; err != nil {
				return fmt.Errorf("update recovered segment %s: %w", path, err)
			}
			continue
		}
		usedSequences[sequence] = struct{}{}
		if err := s.db.Clauses(clause.OnConflict{DoNothing: true}).Create(&segment).Error; err != nil {
			return fmt.Errorf("store recovered segment %s: %w", path, err)
		}
	}
	if err := s.recomputeRecordingAggregate(s.db, recording.ID); err != nil {
		return err
	}
	if recording.Status != model.RecordingStatusRecording {
		return nil
	}
	var completed int64
	if err := s.db.Model(&model.RecordingSegment{}).
		Where("recording_id = ? AND status = ?", recording.ID, model.RecordingStatusCompleted).
		Count(&completed).Error; err != nil {
		return fmt.Errorf("count recovered recording %d segments: %w", recording.ID, err)
	}
	status := model.RecordingStatusCompleted
	updates := map[string]any{"status": status}
	if completed == 0 {
		status = model.RecordingStatusFailed
		updates["status"] = status
		updates["end_time"] = time.Now().UTC()
	}
	if err := s.db.Model(&model.Recording{}).Where("id = ?", recording.ID).Updates(updates).Error; err != nil {
		return fmt.Errorf("finalize recovered recording %d: %w", recording.ID, err)
	}
	s.publishRecordingStatus(recording.ID, recording.CameraID, status)
	return nil
}

func (s *RecorderService) recomputeRecordingAggregate(tx *gorm.DB, recordingID uint) error {
	var sums struct {
		FileSize   int64
		DurationMS int64
	}
	if err := tx.Model(&model.RecordingSegment{}).
		Select("COALESCE(SUM(file_size), 0) AS file_size, COALESCE(SUM(duration_ms), 0) AS duration_ms").
		Where("recording_id = ? AND status = ?", recordingID, model.RecordingStatusCompleted).
		Scan(&sums).Error; err != nil {
		return fmt.Errorf("sum recording %d segments: %w", recordingID, err)
	}
	updates := map[string]any{
		"file_size": sums.FileSize,
		"duration":  int((sums.DurationMS + 999) / 1000),
	}
	var first, last model.RecordingSegment
	firstErr := tx.Where("recording_id = ? AND status = ?", recordingID, model.RecordingStatusCompleted).Order("start_time ASC").First(&first).Error
	if firstErr == nil {
		if err := tx.Where("recording_id = ? AND status = ?", recordingID, model.RecordingStatusCompleted).Order("end_time DESC").First(&last).Error; err != nil {
			return fmt.Errorf("load last recording %d segment: %w", recordingID, err)
		}
		updates["start_time"] = first.StartTime.UTC()
		updates["end_time"] = last.EndTime.UTC()
	} else if firstErr != gorm.ErrRecordNotFound {
		return fmt.Errorf("load first recording %d segment: %w", recordingID, firstErr)
	}
	if err := tx.Model(&model.Recording{}).Where("id = ?", recordingID).Updates(updates).Error; err != nil {
		return fmt.Errorf("update recording %d aggregate: %w", recordingID, err)
	}
	return nil
}

// RunRetentionOnce removes completed physical segment files that have aged
// beyond the configured retention window. Database rows are removed only
// after the individual file has been removed or is already absent.
func (s *RecorderService) RunRetentionOnce() error {
	root, err := filepath.Abs(s.cfg.RecordingsDir)
	if err != nil {
		return fmt.Errorf("resolve recordings root: %w", err)
	}
	now := time.Now().UTC()
	if s.retentionNow != nil {
		now = s.retentionNow().UTC()
	}
	retentionDays := s.cfg.RecordingRetentionDays
	if retentionDays <= 0 {
		retentionDays = 30
	}
	cutoff := now.Add(-time.Duration(retentionDays) * 24 * time.Hour)
	allCompleted, err := s.completedRetentionCandidates("")
	if err != nil {
		return err
	}
	newestID := newestSegmentID(allCompleted)
	candidates, err := s.completedRetentionCandidates("recording_segments.end_time < ?", cutoff)
	if err != nil {
		return err
	}
	for index := range candidates {
		if candidates[index].ID == newestID {
			continue
		}
		if err := s.deleteRetainedSegment(root, &candidates[index]); err != nil {
			return err
		}
	}

	stat := s.diskStat
	if stat == nil {
		return nil
	}
	usage, err := stat(root)
	if err != nil {
		return fmt.Errorf("stat recordings disk: %w", err)
	}
	if usage.Total == 0 {
		return fmt.Errorf("stat recordings disk: total bytes is zero")
	}
	cleanupPercent := s.cfg.RecordingCleanupFreePercent
	if cleanupPercent <= 0 {
		cleanupPercent = 15
	}
	if diskFreePercent(usage) > float64(cleanupPercent) {
		return nil
	}

	pressureCandidates, err := s.completedRetentionCandidates("")
	if err != nil {
		return err
	}
	newestID = newestSegmentID(pressureCandidates)
	for index := range pressureCandidates {
		segment := &pressureCandidates[index]
		if segment.ID == newestID {
			continue
		}
		if err := s.deleteRetainedSegment(root, segment); err != nil {
			return err
		}
		usage, err = stat(root)
		if err != nil {
			return fmt.Errorf("restat recordings disk: %w", err)
		}
		if usage.Total == 0 {
			return fmt.Errorf("restat recordings disk: total bytes is zero")
		}
		if diskFreePercent(usage) > float64(cleanupPercent) {
			return nil
		}
	}
	return nil
}

// StartRetention runs safe segment-file retention every five minutes until
// the recorder service is shut down.
func (s *RecorderService) StartRetention() {
	interval := s.retentionInterval
	if interval <= 0 {
		interval = 5 * time.Minute
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-s.ctx.Done():
				return
			case <-ticker.C:
				if err := s.RunRetentionOnce(); err != nil {
					log.Printf("[recorder] retention: %v", err)
				}
			}
		}
	}()
}

func diskFreePercent(usage diskUsage) float64 {
	return float64(usage.Free) * 100 / float64(usage.Total)
}

func newestSegmentID(segments []model.RecordingSegment) uint {
	var newest model.RecordingSegment
	for index := range segments {
		candidate := segments[index]
		if newest.ID == 0 || candidate.EndTime.After(newest.EndTime) ||
			(candidate.EndTime.Equal(newest.EndTime) && candidate.ID > newest.ID) {
			newest = candidate
		}
	}
	return newest.ID
}

func (s *RecorderService) completedRetentionCandidates(where string, args ...any) ([]model.RecordingSegment, error) {
	var segments []model.RecordingSegment
	query := s.db.Model(&model.RecordingSegment{}).
		Joins("JOIN recordings ON recordings.id = recording_segments.recording_id").
		Where("recording_segments.status = ? AND recordings.status = ?", model.RecordingStatusCompleted, model.RecordingStatusCompleted)
	if where != "" {
		query = query.Where(where, args...)
	}
	if err := query.Order("recording_segments.start_time ASC, recording_segments.id ASC").Find(&segments).Error; err != nil {
		return nil, fmt.Errorf("list retention candidates: %w", err)
	}
	return segments, nil
}

func (s *RecorderService) deleteRetainedSegment(root string, segment *model.RecordingSegment) error {
	path, err := filepath.Abs(segment.FilePath)
	if err != nil {
		return fmt.Errorf("resolve segment path %s: %w", segment.FilePath, err)
	}
	if !pathWithinRoot(root, path) {
		return fmt.Errorf("refuse to remove segment outside recordings root: %s", segment.FilePath)
	}
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return fmt.Errorf("resolve recordings root symlinks: %w", err)
	}
	resolvedPath, err := resolvePathSymlinksAllowMissing(path)
	if err != nil {
		return fmt.Errorf("resolve segment path symlinks %s: %w", segment.FilePath, err)
	}
	if !pathWithinRoot(resolvedRoot, resolvedPath) {
		return fmt.Errorf("refuse to remove segment through path outside recordings root: %s", segment.FilePath)
	}
	info, err := os.Lstat(path)
	switch {
	case err == nil && info.IsDir():
		return fmt.Errorf("refuse to remove segment directory: %s", path)
	case err == nil:
		if err := os.Remove(path); err != nil {
			return fmt.Errorf("remove retained segment %s: %w", path, err)
		}
	case os.IsNotExist(err):
		// The external deletion is already complete; remove the stale row.
	default:
		return fmt.Errorf("inspect retained segment %s: %w", path, err)
	}

	if err := s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Delete(&model.RecordingSegment{}, segment.ID).Error; err != nil {
			return fmt.Errorf("delete retained segment row: %w", err)
		}
		return s.recomputeRecordingAggregate(tx, segment.RecordingID)
	}); err != nil {
		return fmt.Errorf("update retained recording %d: %w", segment.RecordingID, err)
	}
	return nil
}

func pathWithinRoot(root, path string) bool {
	relative, err := filepath.Rel(filepath.Clean(root), filepath.Clean(path))
	if err != nil || relative == "." || relative == ".." {
		return false
	}
	return !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func resolvePathSymlinksAllowMissing(path string) (string, error) {
	cursor := filepath.Clean(path)
	missing := make([]string, 0, 2)
	for {
		resolved, err := filepath.EvalSymlinks(cursor)
		if err == nil {
			for index := len(missing) - 1; index >= 0; index-- {
				resolved = filepath.Join(resolved, missing[index])
			}
			return resolved, nil
		}
		if !os.IsNotExist(err) {
			return "", err
		}
		parent := filepath.Dir(cursor)
		if parent == cursor {
			return "", err
		}
		missing = append(missing, filepath.Base(cursor))
		cursor = parent
	}
}
