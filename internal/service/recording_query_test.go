package service

import (
	"testing"
	"time"

	"CameraIO/internal/model"
)

// TestRecordingSegmentMigrationAndUniqueness catches a schema migration that
// omits RecordingSegment or either of its required uniqueness constraints.
func TestRecordingSegmentMigrationAndUniqueness(t *testing.T) {
	db, cleanup := setupRecorderTestDB(t)
	defer cleanup()

	now := time.Now().UTC()
	seg := model.RecordingSegment{
		RecordingID: 1,
		CameraID:    2,
		Sequence:    1,
		FilePath:    "/archive/a.mp4",
		StartTime:   now,
		EndTime:     now.Add(5 * time.Minute),
		Status:      model.RecordingStatusCompleted,
		Format:      model.FormatMP4,
	}
	if err := db.Create(&seg).Error; err != nil {
		t.Fatal(err)
	}

	duplicateSequence := seg
	duplicateSequence.ID = 0
	duplicateSequence.FilePath = "/archive/b.mp4"
	if err := db.Create(&duplicateSequence).Error; err == nil {
		t.Fatal("duplicate recording sequence must fail")
	}

	duplicateFilePath := seg
	duplicateFilePath.ID = 0
	duplicateFilePath.RecordingID = 3
	duplicateFilePath.Sequence = 2
	if err := db.Create(&duplicateFilePath).Error; err == nil {
		t.Fatal("duplicate file path must fail")
	}
}
