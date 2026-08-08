package service

import (
	"fmt"
	"time"

	"CameraIO/internal/model"
)

const maxTimelineRange = 24 * time.Hour

// TimelineQuery selects completed recording segments overlapping a UTC range.
type TimelineQuery struct {
	CameraID uint
	From     time.Time
	To       time.Time
}

// TimelineSegment is the media metadata needed to display recording coverage.
type TimelineSegment struct {
	ID          uint      `json:"id"`
	RecordingID uint      `json:"recording_id"`
	StartTime   time.Time `json:"start_time"`
	EndTime     time.Time `json:"end_time"`
	DurationMS  int64     `json:"duration_ms"`
	FileSize    int64     `json:"file_size"`
	Status      string    `json:"status"`
}

// PlaybackPoint identifies the segment and media offset for a wall-clock time.
type PlaybackPoint struct {
	Segment       TimelineSegment `json:"segment"`
	OffsetMS      int64           `json:"offset_ms"`
	NextSegmentID *uint           `json:"next_segment_id"`
}

// ListTimeline returns completed segments whose half-open intervals overlap
// the requested half-open interval.
func (s *RecorderService) ListTimeline(query TimelineQuery) ([]TimelineSegment, error) {
	from := query.From.UTC()
	to := query.To.UTC()
	if from.IsZero() || to.IsZero() || !to.After(from) {
		return nil, fmt.Errorf("timeline range must have from before to")
	}
	if to.Sub(from) > maxTimelineRange {
		return nil, fmt.Errorf("timeline range must not exceed 24 hours")
	}

	var stored []model.RecordingSegment
	if err := s.db.
		Where("camera_id = ? AND end_time > ? AND start_time < ? AND status = ?", query.CameraID, from, to, model.RecordingStatusCompleted).
		Order("start_time ASC, id ASC").
		Find(&stored).Error; err != nil {
		return nil, err
	}

	segments := make([]TimelineSegment, len(stored))
	for i := range stored {
		segments[i] = timelineSegment(stored[i])
	}
	return segments, nil
}

// ResolvePlaybackPoint returns nil without an error when no completed segment
// covers the requested wall-clock time.
func (s *RecorderService) ResolvePlaybackPoint(cameraID uint, at time.Time) (*PlaybackPoint, error) {
	at = at.UTC()
	var stored model.RecordingSegment
	result := s.db.
		Where("camera_id = ? AND start_time <= ? AND end_time > ? AND duration_ms > 0 AND status = ?", cameraID, at, at, model.RecordingStatusCompleted).
		Order("start_time DESC, id DESC").
		Limit(1).
		Find(&stored)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, nil
	}

	point := &PlaybackPoint{
		Segment:  timelineSegment(stored),
		OffsetMS: clampPlaybackOffset(at.Sub(stored.StartTime).Milliseconds(), stored.DurationMS),
	}

	var next model.RecordingSegment
	result = s.db.
		Where("camera_id = ? AND duration_ms > 0 AND status = ? AND start_time > ?", cameraID, model.RecordingStatusCompleted, stored.StartTime).
		Order("start_time ASC, id ASC").
		Limit(1).
		Find(&next)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return point, nil
	}

	gap := next.StartTime.Sub(stored.EndTime)
	if gap >= -2*time.Second && gap <= 2*time.Second {
		nextID := next.ID
		point.NextSegmentID = &nextID
	}
	return point, nil
}

func timelineSegment(segment model.RecordingSegment) TimelineSegment {
	return TimelineSegment{
		ID:          segment.ID,
		RecordingID: segment.RecordingID,
		StartTime:   segment.StartTime.UTC(),
		EndTime:     segment.EndTime.UTC(),
		DurationMS:  segment.DurationMS,
		FileSize:    segment.FileSize,
		Status:      segment.Status,
	}
}

func clampPlaybackOffset(offsetMS, durationMS int64) int64 {
	if offsetMS < 0 || durationMS <= 0 {
		return 0
	}
	if offsetMS >= durationMS {
		return durationMS - 1
	}
	return offsetMS
}
