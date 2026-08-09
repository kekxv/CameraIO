# Manual Recording Heartbeat Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make an explicitly manual recording a single-file, heartbeat-leased recording session with an optional remark and a discoverable download address.

**Architecture:** The recording row stores the optional remark and its most recent manual-session heartbeat. Manual MP4 starts select the existing legacy single-file path while schedule/API recordings retain segmented behavior. The recorder sweep stops only expired active manual legacy tasks; API handlers expose the session ID, renew the lease, and return the single-file download path. Live view passes the manual trigger and remark, then renews its lease every 30 seconds.

**Tech Stack:** Go, Gin, GORM/SQLite, FFmpeg, Vue 3, Element Plus, Node test runner.

## Global Constraints

- A manual `trigger_type: "manual"` start must be a single file; never change schedule/API segmented MP4 behavior.
- A valid heartbeat is required once per 30 seconds; a session expires only when its last heartbeat is more than 60 seconds old.
- Keep resource-safe stream-copy recording (`bitrate: 0`) and Chrome 72 compatible frontend syntax.
- Preserve existing start and stop response fields while adding the explicit `recording_id` field.
- Do not merge this branch into `main` unless the user explicitly asks.

---

### Task 1: Persist metadata and select single-file storage

**Files:**
- Modify: `internal/model/recording.go`
- Modify: `internal/service/recorder.go`
- Test: `internal/service/recorder_test.go`

**Interfaces:**
- Consumes: `StartRecordingInput{TriggerType, Remark}`.
- Produces: `Recording.Remark string`, `Recording.HeartbeatAt *time.Time`, and manual MP4 records with `StorageModeLegacy`.

- [x] **Step 1: Write the failing test**

```go
func TestStartRecordingManualCreatesSingleFileHeartbeatSession(t *testing.T) {
    recording, err := svc.StartRecording(&StartRecordingInput{
        CameraID: camera.ID, Format: model.FormatMP4,
        TriggerType: model.TriggerManual, Remark: "柜员交接",
    })
    if err != nil { t.Fatal(err) }
    if recording.StorageMode == model.StorageModeSegmented { t.Fatal("manual recording was segmented") }
    if recording.HeartbeatAt == nil { t.Fatal("manual recording has no heartbeat") }
    if recording.Remark != "柜员交接" { t.Fatalf("remark = %q", recording.Remark) }
}
```

- [x] **Step 2: Run the targeted test to verify RED**

Run: `CGO_ENABLED=0 go test ./internal/service -run TestStartRecordingManualCreatesSingleFileHeartbeatSession -count=1`

Expected: FAIL because `Remark`/`HeartbeatAt` and manual non-segmented behavior do not yet exist.

- [x] **Step 3: Write the minimal implementation**

```go
type Recording struct {
    Remark string `json:"remark" gorm:"type:varchar(255);default:''"`
    HeartbeatAt *time.Time `json:"heartbeat_at" gorm:"index"`
}

recording.HeartbeatAt = &now // manual trigger only
segmented := format == model.FormatMP4 && cam.AccessProtocol != model.ProtocolGB28181 && triggerType != model.TriggerManual
```

- [x] **Step 4: Run the targeted service test to verify GREEN**

Run: `CGO_ENABLED=0 go test ./internal/service -run TestStartRecordingManualCreatesSingleFileHeartbeatSession -count=1`

Expected: PASS.

### Task 2: Renew and expire only manual-session leases

**Files:**
- Modify: `internal/service/recorder.go`
- Test: `internal/service/recorder_test.go`

**Interfaces:**
- Produces: `HeartbeatRecording(recordingID uint) (*model.Recording, error)` and `sweepExpiredManualHeartbeats(now time.Time)`.
- Consumes: active legacy manual `Recording` rows and the existing `StopRecording` finalization path.

- [x] **Step 1: Write failing service tests**

```go
func TestHeartbeatRecordingRefreshesActiveManualLease(t *testing.T) {
    updated, err := svc.HeartbeatRecording(recording.ID)
    if err != nil { t.Fatal(err) }
    if updated.HeartbeatAt == nil || !updated.HeartbeatAt.After(before) { t.Fatal("heartbeat was not refreshed") }
}

func TestSweepExpiredManualHeartbeatsStopsOnlyExpiredManualLegacyTask(t *testing.T) {
    svc.sweepExpiredManualHeartbeats(now)
    // Assert expired manual task stops; fresh manual, scheduled and segmented tasks stay active.
}
```

- [x] **Step 2: Run the targeted tests to verify RED**

Run: `CGO_ENABLED=0 go test ./internal/service -run 'TestHeartbeatRecordingRefreshesActiveManualLease|TestSweepExpiredManualHeartbeatsStopsOnlyExpiredManualLegacyTask' -count=1`

Expected: FAIL because no heartbeat service or expiry sweep exists.

- [x] **Step 3: Write the minimal implementation**

```go
func (s *RecorderService) HeartbeatRecording(id uint) (*model.Recording, error) {
    // Accept only status=recording, trigger=manual, storage!=segmented.
}

func (s *RecorderService) sweepDeadProcesses() {
    // Existing dead-process finalization.
    s.sweepExpiredManualHeartbeats(time.Now().UTC())
}
```

Identify expired IDs while holding `s.mu`, then call `StopRecording` outside the lock. Use `now.Sub(*HeartbeatAt) > 60*time.Second`; never stop missing-heartbeat legacy/API/schedule records.

- [x] **Step 4: Run the targeted service tests to verify GREEN**

Run: `CGO_ENABLED=0 go test ./internal/service -run 'TestHeartbeatRecordingRefreshesActiveManualLease|TestSweepExpiredManualHeartbeatsStopsOnlyExpiredManualLegacyTask' -count=1`

Expected: PASS.

### Task 3: Expose manual recording session endpoints

**Files:**
- Modify: `internal/api/recording.go`
- Modify: `internal/api/router.go`
- Test: `internal/api/api_test.go`

**Interfaces:**
- Produces: `POST /api/v1/recordings/:id/heartbeat`, `GET /api/v1/recordings/:id/download-url`.
- Produces: start response `data.recording_id` plus existing `data.recording`.

- [x] **Step 1: Write failing API tests**

```go
// Start asserts data.recording_id equals data.recording.id.
// Heartbeat asserts 200 with recording_id, heartbeat_at, lease_expires_at.
// Download URL asserts completed legacy recording returns its canonical path.
// Non-manual/segmented/incomplete download URL and inactive heartbeat return 409; missing IDs return 404.
```

- [x] **Step 2: Run the targeted tests to verify RED**

Run: `CGO_ENABLED=0 go test ./internal/api -run 'TestManualRecording(Start|Heartbeat|Download)' -count=1`

Expected: FAIL because the routes and response schema do not yet exist.

- [x] **Step 3: Write the minimal handlers and routes**

```go
protected.POST("/recordings/:id/heartbeat", h.RecordingHeartbeat)
protected.GET("/recordings/:id/download-url", h.GetRecordingDownloadURL)
created(c, gin.H{"recording_id": recording.ID, "recording": recording})
```

Use existing `parseUintParam`, return a relative authenticated URL (`/api/v1/recordings/:id/download`), and do not advertise a URL before a single-file recording is completed.

- [x] **Step 4: Run the targeted API tests to verify GREEN**

Run: `CGO_ENABLED=0 go test ./internal/api -run 'TestManualRecording(Start|Heartbeat|Download)' -count=1`

Expected: PASS.

### Task 4: Send heartbeats from Live view and collect optional remarks

**Files:**
- Modify: `frontend/src/api.js`
- Modify: `frontend/src/api.test.mjs`
- Modify: `frontend/src/views/Live.vue`
- Test: `frontend/src/ui-smoke.test.mjs`

**Interfaces:**
- Produces: `heartbeatRecording(id)` and `getRecordingDownloadLocation(id)` API adapters.
- Consumes: explicit `recording_id` with backwards-compatible fallback to `recording.id`.

- [x] **Step 1: Write failing frontend tests**

```js
await apiModule.heartbeatRecording(9)
await apiModule.getRecordingDownloadLocation(9)
// Assert POST /recordings/9/heartbeat and GET /recordings/9/download-url.
// Assert Live sends trigger_type: 'manual', passes recordRemark, and clears heartbeat timers.
```

- [x] **Step 2: Run the frontend tests to verify RED**

Run: `cd frontend && npm test -- --test-name-pattern='heartbeat|manual recording'`

Expected: FAIL because the adapter and Live manual-session behavior do not yet exist.

- [x] **Step 3: Write the minimal UI behavior**

Add an optional Element Plus input labelled `录像备注` to the existing dialog. Start with `{ trigger_type: 'manual', remark: recordRemark.value }`; store the returned ID; use one 30-second interval per active manual recording; clear it on successful explicit stop, status event, start failure, and component unmount. A heartbeat failure must not issue a client stop; report it and allow the server-side lease to decide.

- [x] **Step 4: Run targeted frontend tests to verify GREEN**

Run: `cd frontend && npm test -- --test-name-pattern='heartbeat|manual recording'`

Expected: PASS.

### Task 5: Validate the integration and prepare handoff

**Files:**
- Modify: `docs/superpowers/plans/2026-08-09-manual-recording-heartbeat.md` (mark completed steps)

- [x] **Step 1: Run server verification**

Run: `PATH=/tmp/cameraio-go-1.24.5/bin:/usr/local/go/bin:$PATH CGO_ENABLED=0 go test ./... -count=1 && PATH=/tmp/cameraio-go-1.24.5/bin:/usr/local/go/bin:$PATH CGO_ENABLED=0 go vet ./...`

Expected: all packages pass and vet exits 0.

- [x] **Step 2: Run frontend and recording contract verification**

Run: `cd frontend && npm test && npm run test:chrome72 && cd .. && bash scripts/tests/verify-single-host-recording-test.sh && bash -n scripts/verify-single-host-recording.sh`

Expected: all frontend, Chrome 72, and shell checks exit 0.

- [x] **Step 3: Inspect the exact changes**

Run: `git diff --check && git status --short`

Expected: no whitespace errors; only manual-recording feature files are modified.

- [x] **Step 4: Commit only after fresh verification**

```bash
git add internal/model/recording.go internal/service/recorder.go internal/service/recorder_test.go internal/api/recording.go internal/api/router.go internal/api/api_test.go frontend/src/api.js frontend/src/api.test.mjs frontend/src/views/Live.vue frontend/src/ui-smoke.test.mjs docs/superpowers/plans/2026-08-09-manual-recording-heartbeat.md
git commit -m "feat: add manual recording heartbeat sessions"
```
