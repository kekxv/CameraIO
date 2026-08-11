# Camera Timezone and Recording Segment List Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Preserve or explicitly configure each camera's ONVIF timezone during time synchronization, show every physical recording segment in the recording list, and support minute-precise history filters.

**Architecture:** Persist an optional POSIX device timezone on `Camera`; synchronization prioritizes that value and otherwise retrieves the authenticated ONVIF timezone, using China Standard Time as the safe default. Add a segment-list query and API response shape that uses `RecordingSegment` as the listed physical unit while retaining its parent recording metadata for control and playback. The browser sends exact local datetime range boundaries as UTC RFC3339 values.

**Tech Stack:** Go, Gin, GORM/SQLite, Vue 3, Element Plus, Node test runner.

## Global Constraints

- Preserve all stored recording timestamps and API query timestamps in UTC RFC3339 form.
- Store timezone values as POSIX ONVIF strings; China Standard Time is `CST-8`.
- Use test-driven development: each production behavior begins with a failing automated test.
- Keep segmented recording playback based on its parent logical recording and the selected segment start time.

---

### Task 1: Camera timezone persistence and authenticated ONVIF synchronization

**Files:**
- Modify: `internal/model/camera.go`
- Modify: `internal/service/camera_service.go`
- Modify: `internal/service/onvif.go`
- Modify: `internal/service/onvif_test.go`
- Modify: `internal/service/camera_service_test.go`
- Modify: `frontend/src/views/Cameras.vue`
- Modify: `frontend/src/ui-smoke.test.mjs`

**Interfaces:**
- Produces `Camera.DeviceTimezone string` serialized as `device_timezone`.
- Produces `CreateCameraInput.DeviceTimezone string` and `UpdateCameraInput.DeviceTimezone *string`.
- Changes `SyncCameraTime` to accept a timezone override and use authenticated `GetSystemDateAndTime` when no override is supplied.

- [ ] **Step 1: Write the failing timezone tests**

```go
func TestBuildSetSystemDateAndTimeEnvelopeUsesChinaTimezone(t *testing.T) {
    got := buildSetSystemDateAndTimeEnvelope(time.Date(2026, 8, 11, 8, 0, 0, 0, time.UTC), "CST-8")
    if !strings.Contains(got, "<tt:TZ>CST-8</tt:TZ>") { t.Fatal("timezone override was not sent") }
}
```

Add an HTTP test server that rejects unauthenticated `GetSystemDateAndTime`, returns `<tt:TZ>CST-8</tt:TZ>` after WS-Security authentication, and asserts the following `SetSystemDateAndTime` body contains `CST-8`. Add service create/update tests asserting `device_timezone` persists.

- [ ] **Step 2: Run the focused tests and verify they fail because the camera field and authenticated timezone path do not exist**

Run: `go test ./internal/service -run 'Test(BuildSetSystemDateAndTimeEnvelopeUsesChinaTimezone|SyncCameraTime.*Timezone|Camera.*DeviceTimezone)' -count=1`

Expected: FAIL before the implementation, showing the missing override/persistence behavior.

- [ ] **Step 3: Implement the minimal timezone flow**

```go
type Camera struct { DeviceTimezone string `json:"device_timezone,omitempty" gorm:"type:varchar(64)"` }

func (s *ONVIFService) SyncCameraTime(ctx context.Context, ip, user, pass string, nvrChannel int, timezoneOverride string) error {
    tz := timezoneOverride
    if tz == "" { tz = s.getDeviceTimezone(ctx, endpoint, user, pass) }
    body := buildSetSystemDateAndTimeEnvelope(time.Now(), tz)
    // Send the authenticated SetSystemDateAndTime request.
}
```

Make `getDeviceTimezone` call the existing authenticated `callONVIF`; set `CST-8` only when the device did not provide a timezone. Thread the override from create/update persistence into automatic, manual, and monitor time synchronization. Add an optional `设备时区（POSIX）` field to the add/edit camera dialog with a `CST-8` placeholder.

- [ ] **Step 4: Run focused backend and UI tests and verify they pass**

Run: `go test ./internal/service -run 'Test(BuildSetSystemDateAndTimeEnvelopeUsesChinaTimezone|SyncCameraTime.*Timezone|Camera.*DeviceTimezone)' -count=1 && npm --prefix frontend test -- --test-name-pattern='timezone|device timezone'`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/model/camera.go internal/service/camera_service.go internal/service/onvif.go internal/service/onvif_test.go internal/service/camera_service_test.go frontend/src/views/Cameras.vue frontend/src/ui-smoke.test.mjs
git commit -m "fix: preserve camera timezone during ONVIF sync"
```

### Task 2: List each segmented recording file as an independent history row

**Files:**
- Modify: `internal/service/recorder.go`
- Modify: `internal/api/recording.go`
- Modify: `internal/service/recording_query_test.go`
- Modify: `internal/api/api_test.go`
- Modify: `frontend/src/api.js`
- Modify: `frontend/src/api.test.mjs`
- Modify: `frontend/src/views/Recordings.vue`
- Modify: `frontend/src/ui-smoke.test.mjs`

**Interfaces:**
- Produces `RecorderService.ListSegments(query RecordingQuery) ([]RecordingListItem, int64, error)`.
- Produces `RecordingListItem` with `segment_id`, `recording_id`, segment file/timing fields, and parent recording trigger/remark/storage metadata.
- `GET /recordings` returns `recordings` as physical segment rows for segmented recordings and one synthesized row for legacy recordings.

- [ ] **Step 1: Write the failing list regression tests**

```go
func TestListRecordingsReturnsEverySegmentOfOneScheduledRecording(t *testing.T) {
    // Create one segmented recording and two completed RecordingSegment rows.
    // GET /api/v1/recordings must return total == 2 and both segment IDs.
}
```

Add a service test for segment time-overlap filtering, ordering by segment start time, and pagination. Add a frontend test that the selected segment opens playback at its own `start_time`, while stop/delete uses its parent `recording_id`.

- [ ] **Step 2: Run the focused tests and verify they fail because `List` queries logical recordings only**

Run: `go test ./internal/service ./internal/api -run 'Test(ListRecordingsReturnsEverySegment|ListSegments)' -count=1 && npm --prefix frontend test -- --test-name-pattern='segment.*history|history.*segment'`

Expected: FAIL with one logical-recording row instead of two physical rows.

- [ ] **Step 3: Implement a physical-file history projection**

```go
type RecordingListItem struct {
    ID uint `json:"id"`
    SegmentID *uint `json:"segment_id,omitempty"`
    RecordingID uint `json:"recording_id"`
    CameraID uint `json:"camera_id"`
    StartTime time.Time `json:"start_time"`
    EndTime *time.Time `json:"end_time,omitempty"`
    Duration int `json:"duration"`
    FileSize int64 `json:"file_size"`
    // format, status, trigger_type, remark, storage_mode
}
```

Query `recording_segments` joined to `recordings` for segmented sessions, union legacy `recordings`, apply the existing camera/status/overlap filters to the physical time range, and paginate the combined projection. In the UI show a `片段` column, use segment-specific media for preview/download where available, and use `recording_id` for parent-session operations.

- [ ] **Step 4: Run focused backend and frontend tests and verify they pass**

Run: `go test ./internal/service ./internal/api -run 'Test(ListRecordingsReturnsEverySegment|ListSegments)' -count=1 && npm --prefix frontend test -- --test-name-pattern='segment.*history|history.*segment'`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/service/recorder.go internal/api/recording.go internal/service/recording_query_test.go internal/api/api_test.go frontend/src/api.js frontend/src/api.test.mjs frontend/src/views/Recordings.vue frontend/src/ui-smoke.test.mjs
git commit -m "feat: show recording segments in history"
```

### Task 3: Minute-precise recording history filters

**Files:**
- Modify: `frontend/src/api.js`
- Modify: `frontend/src/api.test.mjs`
- Modify: `frontend/src/views/Recordings.vue`
- Modify: `frontend/src/ui-smoke.test.mjs`

**Interfaces:**
- Replaces `normalizeRecordingDateRange({ startDate, endDate })` with a compatible helper that accepts optional `startTime` and `endTime` local datetime values.
- Recording list sends `start_time` and `end_time` as UTC RFC3339 with minute precision when values are specified.

- [ ] **Step 1: Write the failing frontend tests**

```js
test('recording history accepts minute-precise local datetime bounds', () => {
  assert.deepEqual(normalizeRecordingDateRange({
    startDate: '2026-08-11T09:15', endDate: '2026-08-11T10:45',
  }), {
    start_time: '2026-08-11T09:15:00.000Z', end_time: '2026-08-11T10:45:00.000Z',
  })
})
```

Add tests for an omitted end bound and reversed datetime values. Add a UI smoke assertion for an Element Plus `datetimerange` picker with `HH:mm` format.

- [ ] **Step 2: Run the frontend tests and verify they fail because the helper accepts date-only values**

Run: `npm --prefix frontend test -- --test-name-pattern='minute-precise|datetime.*history'`

Expected: FAIL because `parseRecordingDate` rejects the datetime input.

- [ ] **Step 3: Implement minute-precise local range conversion and UI**

```js
<el-date-picker v-model="dateRange" type="datetimerange" value-format="YYYY-MM-DDTHH:mm" format="YYYY-MM-DD HH:mm" />
```

Parse the local `YYYY-MM-DDTHH:mm` value strictly, preserve the old date-only end-of-day behavior only for existing date-only input, and send exact datetime bounds unchanged except for conversion to UTC. Rename labels and clear action to refer to `录像时间` rather than `日期`.

- [ ] **Step 4: Run the frontend tests and verify they pass**

Run: `npm --prefix frontend test -- --test-name-pattern='recording (date range|history|minute-precise)|datetime.*history'`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/api.js frontend/src/api.test.mjs frontend/src/views/Recordings.vue frontend/src/ui-smoke.test.mjs
git commit -m "feat: filter recording history by minute"
```

### Task 4: Full verification and documentation

**Files:**
- Modify: `API.md`

- [ ] **Step 1: Document camera timezone and segment-list semantics**

Add `device_timezone` to the camera request/response fields. Clarify that `/recordings` lists physical segment files and that `recording_id` identifies the parent recording session.

- [ ] **Step 2: Run all backend tests**

Run: `go test ./... -count=1`

Expected: PASS.

- [ ] **Step 3: Run all frontend tests and build**

Run: `npm --prefix frontend test && npm --prefix frontend run build`

Expected: PASS.

- [ ] **Step 4: Inspect the final diff and commit documentation**

Run: `git diff --check && git status --short`

Expected: no whitespace errors; only the planned source, test, and documentation changes.

```bash
git add API.md
git commit -m "docs: describe camera timezone and segment history"
```
