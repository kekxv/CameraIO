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

func TestRecordingPersistenceNormalizesTimesToUTC(t *testing.T) {
	db, cleanup := setupRecorderTestDB(t)
	defer cleanup()

	nonUTC := time.FixedZone("UTC+07", 7*60*60)
	start := time.Date(2026, 8, 8, 14, 30, 0, 123000000, nonUTC)
	end := start.Add(5 * time.Minute)
	recording := model.Recording{
		CameraID:  2,
		FilePath:  "/archive/session.mp4",
		StartTime: start,
		EndTime:   &end,
		Status:    model.RecordingStatusCompleted,
		Format:    model.FormatMP4,
	}
	if err := db.Create(&recording).Error; err != nil {
		t.Fatal(err)
	}
	segment := model.RecordingSegment{
		RecordingID: recording.ID,
		CameraID:    2,
		Sequence:    1,
		FilePath:    "/archive/session-000001.mp4",
		StartTime:   start,
		EndTime:     end,
		Status:      model.RecordingStatusCompleted,
		Format:      model.FormatMP4,
		CreatedAt:   start,
	}
	if err := db.Create(&segment).Error; err != nil {
		t.Fatal(err)
	}
	assertUTC := func(name string, got, want time.Time) {
		t.Helper()
		if !got.Equal(want) {
			t.Errorf("%s instant = %s, want %s", name, got, want)
		}
		if got.Location() != time.UTC {
			t.Errorf("%s location = %s, want UTC", name, got.Location())
		}
	}
	assertUTC("recording start after create", recording.StartTime, start)
	assertUTC("recording end after create", *recording.EndTime, end)
	assertUTC("segment start after create", segment.StartTime, start)
	assertUTC("segment end after create", segment.EndTime, end)
	assertUTC("segment created after create", segment.CreatedAt, start)

	var persistedRecording model.Recording
	if err := db.First(&persistedRecording, recording.ID).Error; err != nil {
		t.Fatal(err)
	}
	var persistedSegment model.RecordingSegment
	if err := db.First(&persistedSegment, segment.ID).Error; err != nil {
		t.Fatal(err)
	}

	for name, got := range map[string]struct {
		got  time.Time
		want time.Time
	}{
		"recording start": {persistedRecording.StartTime, start},
		"recording end":   {*persistedRecording.EndTime, end},
		"segment start":   {persistedSegment.StartTime, start},
		"segment end":     {persistedSegment.EndTime, end},
		"segment created": {persistedSegment.CreatedAt, start},
	} {
		assertUTC(name, got.got, got.want)
	}
}
