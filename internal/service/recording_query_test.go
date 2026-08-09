package service

import (
	"fmt"
	"testing"
	"time"

	"CameraIO/internal/model"

	"gorm.io/gorm"
)

func queryTime(t *testing.T, value string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		t.Fatalf("parse time %q: %v", value, err)
	}
	return parsed
}

func createQuerySegment(t *testing.T, db *gorm.DB, segment model.RecordingSegment) model.RecordingSegment {
	t.Helper()
	if segment.FilePath == "" {
		segment.FilePath = fmt.Sprintf("/archive/query-%d-%d.mp4", segment.RecordingID, segment.Sequence)
	}
	if segment.Format == "" {
		segment.Format = model.FormatMP4
	}
	if err := db.Create(&segment).Error; err != nil {
		t.Fatalf("create segment: %v", err)
	}
	return segment
}

// TestListTimelineUsesIntervalOverlap catches replacing interval overlap with
// a start-time-inside-the-query filter.
func TestListTimelineUsesIntervalOverlap(t *testing.T) {
	db, cleanup := setupRecorderTestDB(t)
	defer cleanup()

	start := queryTime(t, "2026-08-08T09:55:00Z")
	end := queryTime(t, "2026-08-08T10:05:00Z")
	segment := createQuerySegment(t, db, model.RecordingSegment{
		RecordingID: 41,
		CameraID:    1,
		Sequence:    1,
		FileSize:    4096,
		StartTime:   start,
		EndTime:     end,
		DurationMS:  600000,
		Status:      model.RecordingStatusCompleted,
	})

	svc := NewRecorderService(db, nil)
	got, err := svc.ListTimeline(TimelineQuery{
		CameraID: 1,
		From:     queryTime(t, "2026-08-08T10:00:00Z"),
		To:       queryTime(t, "2026-08-08T10:01:00Z"),
	})
	if err != nil {
		t.Fatalf("ListTimeline: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("segments = %+v, want one overlapping segment", got)
	}
	want := TimelineSegment{
		ID:          segment.ID,
		RecordingID: 41,
		StartTime:   start,
		EndTime:     end,
		DurationMS:  600000,
		FileSize:    4096,
		Status:      model.RecordingStatusCompleted,
	}
	if got[0].ID != want.ID || got[0].RecordingID != want.RecordingID ||
		!got[0].StartTime.Equal(want.StartTime) || !got[0].EndTime.Equal(want.EndTime) ||
		got[0].DurationMS != want.DurationMS || got[0].FileSize != want.FileSize ||
		got[0].Status != want.Status {
		t.Fatalf("segment = %+v, want %+v", got[0], want)
	}
}

// TestListTimelineUsesHalfOpenBoundaries catches inclusive end/start boundary
// comparisons, missing camera/status filters, and loss of chronological order.
func TestListTimelineUsesHalfOpenBoundaries(t *testing.T) {
	db, cleanup := setupRecorderTestDB(t)
	defer cleanup()

	from := queryTime(t, "2026-08-08T10:00:00Z")
	to := queryTime(t, "2026-08-08T10:01:00Z")
	fixtures := []model.RecordingSegment{
		{RecordingID: 1, CameraID: 1, Sequence: 1, StartTime: from.Add(-time.Minute), EndTime: from, DurationMS: 60000, Status: model.RecordingStatusCompleted},
		{RecordingID: 2, CameraID: 1, Sequence: 1, StartTime: to, EndTime: to.Add(time.Minute), DurationMS: 60000, Status: model.RecordingStatusCompleted},
		{RecordingID: 3, CameraID: 2, Sequence: 1, StartTime: from, EndTime: to, DurationMS: 60000, Status: model.RecordingStatusCompleted},
		{RecordingID: 4, CameraID: 1, Sequence: 1, StartTime: from, EndTime: to, DurationMS: 60000, Status: model.RecordingStatusFailed},
		{RecordingID: 5, CameraID: 1, Sequence: 1, StartTime: from.Add(30 * time.Second), EndTime: to, DurationMS: 30000, Status: model.RecordingStatusCompleted},
		{RecordingID: 6, CameraID: 1, Sequence: 1, StartTime: from.Add(10 * time.Second), EndTime: to, DurationMS: 50000, Status: model.RecordingStatusCompleted},
	}
	created := make(map[uint]model.RecordingSegment, len(fixtures))
	for _, fixture := range fixtures {
		created[fixture.RecordingID] = createQuerySegment(t, db, fixture)
	}

	svc := NewRecorderService(db, nil)
	got, err := svc.ListTimeline(TimelineQuery{CameraID: 1, From: from, To: to})
	if err != nil {
		t.Fatalf("ListTimeline: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("segments = %+v, want only two completed overlapping camera segments", got)
	}
	if got[0].ID != created[6].ID || got[1].ID != created[5].ID {
		t.Fatalf("segment order = [%d %d], want [%d %d]", got[0].ID, got[1].ID, created[6].ID, created[5].ID)
	}
}

// TestListTimelineRejectsRangesLongerThan24Hours catches an absent or
// off-by-one maximum-range guard.
func TestListTimelineRejectsRangesLongerThan24Hours(t *testing.T) {
	db, cleanup := setupRecorderTestDB(t)
	defer cleanup()
	svc := NewRecorderService(db, nil)
	from := queryTime(t, "2026-08-08T00:00:00Z")

	if _, err := svc.ListTimeline(TimelineQuery{CameraID: 1, From: from, To: from.Add(24 * time.Hour)}); err != nil {
		t.Fatalf("24-hour range must be accepted: %v", err)
	}
	if _, err := svc.ListTimeline(TimelineQuery{CameraID: 1, From: from, To: from.Add(24*time.Hour + time.Nanosecond)}); err == nil {
		t.Fatal("range longer than 24 hours must be rejected")
	}
}

// TestListTimelineRejectsNonIncreasingRanges catches a query that accepts an
// empty or reversed wall-clock interval.
func TestListTimelineRejectsNonIncreasingRanges(t *testing.T) {
	db, cleanup := setupRecorderTestDB(t)
	defer cleanup()
	svc := NewRecorderService(db, nil)
	from := queryTime(t, "2026-08-08T10:00:00Z")

	for name, to := range map[string]time.Time{
		"empty":    from,
		"reversed": from.Add(-time.Nanosecond),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := svc.ListTimeline(TimelineQuery{CameraID: 1, From: from, To: to}); err == nil {
				t.Fatal("non-increasing range must be rejected")
			}
		})
	}
}

// TestResolvePlaybackPointReturnsNilForMissingCoverage catches propagation of
// a database not-found error for an ordinary recording gap.
func TestResolvePlaybackPointReturnsNilForMissingCoverage(t *testing.T) {
	db, cleanup := setupRecorderTestDB(t)
	defer cleanup()

	got, err := NewRecorderService(db, nil).ResolvePlaybackPoint(1, queryTime(t, "2026-08-08T10:00:00Z"))
	if err != nil {
		t.Fatalf("ResolvePlaybackPoint: %v", err)
	}
	if got != nil {
		t.Fatalf("playback point = %+v, want nil for missing coverage", got)
	}
}

// TestResolvePlaybackPointUsesHalfOpenCoverage catches excluding an exact
// segment start or including its exact end.
func TestResolvePlaybackPointUsesHalfOpenCoverage(t *testing.T) {
	db, cleanup := setupRecorderTestDB(t)
	defer cleanup()

	start := queryTime(t, "2026-08-08T10:00:00Z")
	end := start.Add(time.Minute)
	segment := createQuerySegment(t, db, model.RecordingSegment{
		RecordingID: 10,
		CameraID:    1,
		Sequence:    1,
		StartTime:   start,
		EndTime:     end,
		DurationMS:  60000,
		Status:      model.RecordingStatusCompleted,
	})
	svc := NewRecorderService(db, nil)

	atStart, err := svc.ResolvePlaybackPoint(1, start)
	if err != nil {
		t.Fatalf("resolve start: %v", err)
	}
	if atStart == nil || atStart.Segment.ID != segment.ID || atStart.OffsetMS != 0 {
		t.Fatalf("point at start = %+v, want segment %d offset 0", atStart, segment.ID)
	}
	atEnd, err := svc.ResolvePlaybackPoint(1, end)
	if err != nil {
		t.Fatalf("resolve end: %v", err)
	}
	if atEnd != nil {
		t.Fatalf("point at exclusive end = %+v, want nil", atEnd)
	}
}

// TestResolvePlaybackPointClampsOffsetBelowDuration catches returning an
// unseekable offset equal to or beyond the physical media duration.
func TestResolvePlaybackPointClampsOffsetBelowDuration(t *testing.T) {
	db, cleanup := setupRecorderTestDB(t)
	defer cleanup()

	start := queryTime(t, "2026-08-08T10:00:00Z")
	createQuerySegment(t, db, model.RecordingSegment{
		RecordingID: 20,
		CameraID:    1,
		Sequence:    1,
		StartTime:   start,
		EndTime:     start.Add(time.Minute),
		DurationMS:  10000,
		Status:      model.RecordingStatusCompleted,
	})
	createQuerySegment(t, db, model.RecordingSegment{
		RecordingID: 21,
		CameraID:    2,
		Sequence:    1,
		StartTime:   start.Add(30 * time.Second),
		EndTime:     start.Add(time.Minute),
		DurationMS:  30000,
		Status:      model.RecordingStatusCompleted,
	})
	createQuerySegment(t, db, model.RecordingSegment{
		RecordingID: 22,
		CameraID:    1,
		Sequence:    1,
		StartTime:   start.Add(30 * time.Second),
		EndTime:     start.Add(time.Minute),
		DurationMS:  30000,
		Status:      model.RecordingStatusFailed,
	})

	got, err := NewRecorderService(db, nil).ResolvePlaybackPoint(1, start.Add(59*time.Second))
	if err != nil {
		t.Fatalf("ResolvePlaybackPoint: %v", err)
	}
	if got == nil {
		t.Fatal("playback point is nil")
	}
	if got.OffsetMS != 9999 {
		t.Fatalf("offset = %d, want 9999", got.OffsetMS)
	}
	if got.OffsetMS < 0 || got.OffsetMS >= got.Segment.DurationMS {
		t.Fatalf("offset %d is outside [0,%d)", got.OffsetMS, got.Segment.DurationMS)
	}
}

// TestResolvePlaybackPointNormalizesQueryTimeToUTC catches passing a
// non-normalized instant into SQLite's timestamp comparison.
func TestResolvePlaybackPointNormalizesQueryTimeToUTC(t *testing.T) {
	db, cleanup := setupRecorderTestDB(t)
	defer cleanup()

	start := queryTime(t, "2026-08-08T10:00:00Z")
	segment := createQuerySegment(t, db, model.RecordingSegment{
		RecordingID: 30,
		CameraID:    1,
		Sequence:    1,
		StartTime:   start,
		EndTime:     start.Add(time.Minute),
		DurationMS:  60000,
		Status:      model.RecordingStatusCompleted,
	})

	plusSeven := time.FixedZone("UTC+07", 7*60*60)
	got, err := NewRecorderService(db, nil).ResolvePlaybackPoint(1, start.Add(30*time.Second).In(plusSeven))
	if err != nil {
		t.Fatalf("ResolvePlaybackPoint: %v", err)
	}
	if got == nil || got.Segment.ID != segment.ID || got.OffsetMS != 30000 {
		t.Fatalf("playback point = %+v, want segment %d offset 30000", got, segment.ID)
	}
}

// TestResolvePlaybackPointLinksOnlyWithinTwoSecondBoundaryTolerance catches
// rejecting a small overlap or silently skipping a real boundary discontinuity.
func TestResolvePlaybackPointLinksOnlyWithinTwoSecondBoundaryTolerance(t *testing.T) {
	for _, test := range []struct {
		name     string
		gap      time.Duration
		wantNext bool
	}{
		{name: "two second overlap", gap: -2 * time.Second, wantNext: true},
		{name: "over two second overlap", gap: -2001 * time.Millisecond, wantNext: false},
		{name: "two seconds", gap: 2 * time.Second, wantNext: true},
		{name: "over two seconds", gap: 2001 * time.Millisecond, wantNext: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			db, cleanup := setupRecorderTestDB(t)
			defer cleanup()

			start := queryTime(t, "2026-08-08T10:00:00Z")
			end := start.Add(5 * time.Minute)
			createQuerySegment(t, db, model.RecordingSegment{
				RecordingID: 50,
				CameraID:    1,
				Sequence:    1,
				StartTime:   start,
				EndTime:     end,
				DurationMS:  300000,
				Status:      model.RecordingStatusCompleted,
			})
			next := createQuerySegment(t, db, model.RecordingSegment{
				RecordingID: 51,
				CameraID:    1,
				Sequence:    1,
				StartTime:   end.Add(test.gap),
				EndTime:     end.Add(test.gap + 5*time.Minute),
				DurationMS:  300000,
				Status:      model.RecordingStatusCompleted,
			})
			createQuerySegment(t, db, model.RecordingSegment{
				RecordingID: 52,
				CameraID:    2,
				Sequence:    1,
				StartTime:   end,
				EndTime:     end.Add(5 * time.Minute),
				DurationMS:  300000,
				Status:      model.RecordingStatusCompleted,
			})
			createQuerySegment(t, db, model.RecordingSegment{
				RecordingID: 53,
				CameraID:    1,
				Sequence:    1,
				StartTime:   end.Add(time.Second),
				EndTime:     end.Add(5*time.Minute + time.Second),
				DurationMS:  300000,
				Status:      model.RecordingStatusFailed,
			})

			got, err := NewRecorderService(db, nil).ResolvePlaybackPoint(1, start.Add(time.Minute))
			if err != nil {
				t.Fatalf("ResolvePlaybackPoint: %v", err)
			}
			if got == nil {
				t.Fatal("playback point is nil")
			}
			if test.wantNext {
				if got.NextSegmentID == nil || *got.NextSegmentID != next.ID {
					t.Fatalf("next segment = %v, want %d", got.NextSegmentID, next.ID)
				}
			} else if got.NextSegmentID != nil {
				t.Fatalf("next segment = %d, want nil across recording gap", *got.NextSegmentID)
			}
		})
	}
}

// TestResolvePlaybackPointTreatsNonPositiveDurationAsUnavailable catches
// returning an offset for a segment whose valid offset interval is empty.
func TestResolvePlaybackPointTreatsNonPositiveDurationAsUnavailable(t *testing.T) {
	for _, durationMS := range []int64{0, -1} {
		t.Run(fmt.Sprintf("duration_%d", durationMS), func(t *testing.T) {
			db, cleanup := setupRecorderTestDB(t)
			defer cleanup()

			start := queryTime(t, "2026-08-08T10:00:00Z")
			createQuerySegment(t, db, model.RecordingSegment{
				RecordingID: 60,
				CameraID:    1,
				Sequence:    1,
				StartTime:   start,
				EndTime:     start.Add(time.Minute),
				DurationMS:  durationMS,
				Status:      model.RecordingStatusCompleted,
			})

			got, err := NewRecorderService(db, nil).ResolvePlaybackPoint(1, start.Add(30*time.Second))
			if err != nil {
				t.Fatalf("ResolvePlaybackPoint: %v", err)
			}
			if got != nil {
				t.Fatalf("playback point = %+v, want nil for duration %d", got, durationMS)
			}
		})
	}
}

// TestResolvePlaybackPointDoesNotAdvertiseNonPositiveDurationAsNext catches
// preloading a completed successor that cannot produce a valid playback point.
func TestResolvePlaybackPointDoesNotAdvertiseNonPositiveDurationAsNext(t *testing.T) {
	for _, durationMS := range []int64{0, -1} {
		t.Run(fmt.Sprintf("duration_%d", durationMS), func(t *testing.T) {
			db, cleanup := setupRecorderTestDB(t)
			defer cleanup()

			start := queryTime(t, "2026-08-08T10:00:00Z")
			end := start.Add(5 * time.Minute)
			createQuerySegment(t, db, model.RecordingSegment{
				RecordingID: 70,
				CameraID:    1,
				Sequence:    1,
				StartTime:   start,
				EndTime:     end,
				DurationMS:  300000,
				Status:      model.RecordingStatusCompleted,
			})
			createQuerySegment(t, db, model.RecordingSegment{
				RecordingID: 71,
				CameraID:    1,
				Sequence:    1,
				StartTime:   end.Add(time.Second),
				EndTime:     end.Add(5*time.Minute + time.Second),
				DurationMS:  durationMS,
				Status:      model.RecordingStatusCompleted,
			})

			got, err := NewRecorderService(db, nil).ResolvePlaybackPoint(1, start.Add(time.Minute))
			if err != nil {
				t.Fatalf("ResolvePlaybackPoint: %v", err)
			}
			if got == nil {
				t.Fatal("current playback point is nil")
			}
			if got.NextSegmentID != nil {
				t.Fatalf("next segment = %d, want nil for duration %d", *got.NextSegmentID, durationMS)
			}
		})
	}
}

// TestResolvePlaybackPointReturnsBoundedContiguousWindow catches returning an
// unbounded sequence or continuing playback after a recording discontinuity.
func TestResolvePlaybackPointReturnsOnlyImmediateContiguousSuccessor(t *testing.T) {
	db, cleanup := setupRecorderTestDB(t)
	defer cleanup()

	start := queryTime(t, "2026-08-08T10:00:00Z")
	segments := make([]model.RecordingSegment, 0, 7)
	for i := 0; i < 6; i++ {
		segmentStart := start.Add(time.Duration(i) * time.Minute)
		segments = append(segments, createQuerySegment(t, db, model.RecordingSegment{
			RecordingID: uint(80 + i),
			CameraID:    1,
			Sequence:    i + 1,
			StartTime:   segmentStart,
			EndTime:     segmentStart.Add(time.Minute),
			DurationMS:  60000,
			Status:      model.RecordingStatusCompleted,
		}))
	}
	gapStart := start.Add(6*time.Minute + 3*time.Second)
	segments = append(segments, createQuerySegment(t, db, model.RecordingSegment{
		RecordingID: 86,
		CameraID:    1,
		Sequence:    7,
		StartTime:   gapStart,
		EndTime:     gapStart.Add(time.Minute),
		DurationMS:  60000,
		Status:      model.RecordingStatusCompleted,
	}))

	svc := NewRecorderService(db, nil)
	first, err := svc.ResolvePlaybackPoint(1, start.Add(30*time.Second))
	if err != nil {
		t.Fatalf("resolve first segment: %v", err)
	}
	if first == nil {
		t.Fatal("first playback point is nil")
	}
	if first.NextSegmentID == nil || *first.NextSegmentID != segments[1].ID {
		t.Fatalf("first next segment = %v, want %d", first.NextSegmentID, segments[1].ID)
	}

	third, err := svc.ResolvePlaybackPoint(1, start.Add(2*time.Minute+30*time.Second))
	if err != nil {
		t.Fatalf("resolve third segment: %v", err)
	}
	if third == nil {
		t.Fatal("third playback point is nil")
	}
	if third.NextSegmentID == nil || *third.NextSegmentID != segments[3].ID {
		t.Fatalf("third next segment = %v, want %d", third.NextSegmentID, segments[3].ID)
	}
}

// TestRecorderListUsesIntervalOverlap catches filtering legacy sessions only
// by whether their start time lies inside the requested range.
func TestRecorderListUsesIntervalOverlap(t *testing.T) {
	db, cleanup := setupRecorderTestDB(t)
	defer cleanup()

	from := queryTime(t, "2026-08-08T10:00:00Z")
	to := queryTime(t, "2026-08-08T10:01:00Z")
	spanningStart := from.Add(-5 * time.Minute)
	spanningEnd := to.Add(4 * time.Minute)
	endingAtFromStart := from.Add(-time.Minute)
	endingAtFromEnd := from
	startingAtToStart := to
	startingAtToEnd := to.Add(time.Minute)
	recordings := []model.Recording{
		{CameraID: 1, FilePath: "/archive/spanning.mp4", StartTime: spanningStart, EndTime: &spanningEnd, Status: model.RecordingStatusCompleted},
		{CameraID: 1, FilePath: "/archive/ending-at-from.mp4", StartTime: endingAtFromStart, EndTime: &endingAtFromEnd, Status: model.RecordingStatusCompleted},
		{CameraID: 1, FilePath: "/archive/starting-at-to.mp4", StartTime: startingAtToStart, EndTime: &startingAtToEnd, Status: model.RecordingStatusCompleted},
	}
	for i := range recordings {
		if err := db.Create(&recordings[i]).Error; err != nil {
			t.Fatalf("create recording: %v", err)
		}
	}

	got, total, err := NewRecorderService(db, nil).List(RecordingQuery{
		StartTime: &from,
		EndTime:   &to,
		Page:      1,
		PageSize:  10,
	})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if total != 1 || len(got) != 1 || got[0].ID != recordings[0].ID {
		t.Fatalf("recordings = %+v total=%d, want only spanning recording %d", got, total, recordings[0].ID)
	}
}

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

func TestRecordingSegmentPersistenceAssignsZeroCreatedAtInUTC(t *testing.T) {
	db, cleanup := setupRecorderTestDB(t)
	defer cleanup()

	now := time.Now().UTC()
	segment := model.RecordingSegment{
		RecordingID: 1,
		CameraID:    2,
		Sequence:    1,
		FilePath:    "/archive/zero-created-at.mp4",
		StartTime:   now,
		EndTime:     now.Add(5 * time.Minute),
		Status:      model.RecordingStatusCompleted,
		Format:      model.FormatMP4,
	}
	if err := db.Create(&segment).Error; err != nil {
		t.Fatal(err)
	}
	if segment.CreatedAt.IsZero() {
		t.Fatal("created_at must be assigned")
	}
	if segment.CreatedAt.Location() != time.UTC {
		t.Errorf("created_at after create location = %s, want UTC", segment.CreatedAt.Location())
	}

	var persisted model.RecordingSegment
	if err := db.First(&persisted, segment.ID).Error; err != nil {
		t.Fatal(err)
	}
	if !persisted.CreatedAt.Equal(segment.CreatedAt) {
		t.Errorf("created_at instant = %s, want %s", persisted.CreatedAt, segment.CreatedAt)
	}
	if persisted.CreatedAt.Location() != time.UTC {
		t.Errorf("created_at after reload location = %s, want UTC", persisted.CreatedAt.Location())
	}
}
