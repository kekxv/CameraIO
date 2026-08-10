# CI Recording Validation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the recorder reject unsupported segmented recording options before any disk admission side effect, so the CI regression test reports the caller-correctable validation error.

**Architecture:** `StartRecording` currently validates generic known formats, then checks disk admission, and only later determines whether the request uses the segmented MP4 path. Add the segmented-recorder format restriction to the validation phase, after a camera has been loaded but before `checkRecordingAdmission`. The existing integration test exercises the public API with the real SQLite database and default missing recordings directory.

**Tech Stack:** Go 1.20, GORM/SQLite, Go `testing`, GitHub Actions.

## Global Constraints

- Preserve MP4 segmented recording, legacy TS/WebM recording, GB28181 recording, and the zero-bitrate stream-copy policy.
- Return `RecordingValidationError` before filesystem, disk-stat, database-write, or FFmpeg side effects for unsupported segmented format requests.
- Verify with the exact Go and frontend commands declared in `.github/workflows/build.yml` and `.github/workflows/test.yml`.

---

### Task 1: Validate unsupported segmented formats before disk admission

**Files:**
- Modify: `internal/service/recorder.go:307-347`
- Test: `internal/service/recording_segmenter_test.go:293-324`

**Interfaces:**
- Consumes: `StartRecording(in *StartRecordingInput) (*model.Recording, error)`, `model.Camera.AccessProtocol`, `model.FormatMP4`, and `model.TriggerManual`.
- Produces: an early `*RecordingValidationError` with message `webm recordings are not supported` when an RTSP API recording requests WebM; no `Recording` row is created.

- [x] **Step 1: Establish the failing regression test**

`TestRecorderRejectsUnsafeSegmentedRecordingOptions/webm` in `recording_segmenter_test.go` invokes `StartRecording` for a real RTSP camera with `FormatWebM`. It must receive the literal error substring `webm recordings are not supported`, even when `DefaultConfig().RecordingsDir` does not exist.

- [x] **Step 2: Run the regression test to verify RED**

Run: `CGO_ENABLED=0 CAMERAIO_SKIP_FFMPEG_DOWNLOAD=1 go test ./internal/service -run '^TestRecorderRejectsUnsafeSegmentedRecordingOptions$' -count=1 -v`

Expected: FAIL in `webm` because `checkRecordingAdmission` returns `stat recordings disk: no such file or directory` before WebM validation.

- [x] **Step 3: Add the smallest early validation**

In `StartRecording`, after generic format normalization and validation and before `checkRecordingAdmission`, compute the trigger type and whether the camera/request selects the segmented recorder. Reject every non-MP4 format on that path:

```go
triggerType := in.TriggerType
if triggerType == "" {
    triggerType = model.TriggerAPI
}
segmented := cam.AccessProtocol != model.ProtocolGB28181 && triggerType != model.TriggerManual
if segmented && format != model.FormatMP4 {
    return nil, &RecordingValidationError{Message: fmt.Sprintf("%s recordings are not supported", format)}
}
```

Use one computed `triggerType` and `segmented` value later when constructing the recording, rather than recomputing either value. Keep GB28181 and manual recordings on their existing legacy-format path.

- [x] **Step 4: Run the targeted regression test to verify GREEN**

Run: `CGO_ENABLED=0 CAMERAIO_SKIP_FFMPEG_DOWNLOAD=1 go test ./internal/service -run '^TestRecorderRejectsUnsafeSegmentedRecordingOptions$' -count=1 -v`

Expected: PASS; both the WebM and bitrate subtests pass, and the database has zero recording rows.

- [x] **Step 5: Run CI-equivalent verification**

Run from `frontend/`: `npm ci && npm test && npm run test:chrome72`.

Run from repository root: `CGO_ENABLED=0 CAMERAIO_SKIP_FFMPEG_DOWNLOAD=1 go vet ./...`, `CGO_ENABLED=0 CAMERAIO_SKIP_FFMPEG_DOWNLOAD=1 go test ./... -coverprofile=coverage.out -count=1`, `CGO_ENABLED=0 CAMERAIO_SKIP_FFMPEG_DOWNLOAD=1 go test ./... -v -count=1`, and cross-platform builds matching `build.yml`.

- [x] **Step 6: Review the diff**

Run: `git diff --check && git status --short`

Expected: only the recorder validation, this regression-plan document, and ignored build/test artifacts are changed.

### Task 2: Normalize unsupported WebM options in frontend API requests

**Files:**
- Modify: `frontend/src/api.js:97-101`
- Test: `frontend/src/api.test.mjs:61-72,173-218`

**Interfaces:**
- Consumes: `normalizeResourceSafeRecordingOptions(options = {})`.
- Produces: options with `format: 'mp4'` for WebM or absent/unknown input, preserves `format: 'ts'`, and always sends `bitrate: 0`.

- [x] **Step 1: Establish the failing frontend regression test**

`resource-safe recording options normalize persisted unsafe choices` calls the public normalizer with `{ format: 'webm', bitrate: 1000, with_audio: true }` and expects `{ format: 'mp4', bitrate: 0, with_audio: true }`. The endpoint test independently confirms that `startRecording(7, { format: 'webm', bitrate: 1000 })` serializes MP4 and zero bitrate.

- [x] **Step 2: Run the frontend regression test to verify RED**

Run from `frontend/`: `node --test src/api.test.mjs`

Expected: two failures, both showing actual `format: 'webm'` where MP4 is expected.

- [x] **Step 3: Add the smallest normalizer correction**

Replace the format branch with:

```js
format: options.format === 'ts' ? 'ts' : 'mp4',
```

This preserves the sole supported non-MP4 legacy option while mapping WebM and unknown values to the stream-copy-safe MP4 default.

- [x] **Step 4: Run the frontend regression test to verify GREEN**

Run from `frontend/`: `node --test src/api.test.mjs`

Expected: 10 tests passed and 0 failed.

- [x] **Step 5: Re-run CI-equivalent verification**

Run the frontend and Go checks listed in Task 1, Step 5, then inspect `git diff --check` and `git status --short`.
