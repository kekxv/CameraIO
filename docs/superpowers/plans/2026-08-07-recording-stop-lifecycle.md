# Recording Stop Lifecycle Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the first stop-recording request finish predictably without racing multiple `exec.Cmd.Wait` calls or exceeding the frontend's 30-second timeout.

**Architecture:** Give `watchTask` exclusive ownership of `exec.Cmd.Wait` and expose process completion through a per-task `done` channel. All stop, shutdown, sweep, and delete paths request termination and wait on that shared completion signal with bounded timeouts, while exactly one path claims database finalization.

**Tech Stack:** Go, `os/exec`, channels, GORM/SQLite tests, FFmpeg process lifecycle.

## Global Constraints

- Keep `POST /api/v1/recordings/stop` and its request/response shape unchanged.
- A stop request must return before the frontend Axios timeout of 30000 ms.
- Never call `exec.Cmd.Wait` more than once for a recording process.
- Preserve idempotent repeated stop behavior.
- Keep Windows 7 compatibility; do not depend on `os.Interrupt` for FFmpeg termination.

---

### Task 1: Prove stop requests use the watcher completion signal

**Files:**
- Modify: `internal/service/recorder_test.go`
- Modify: `internal/service/recorder.go`

**Interfaces:**
- Consumes: existing `RecorderService.StopRecording(recordingID uint) error` and `watchTask(recordingID uint, task *recordTask)`.
- Produces: `recordTask.done <-chan struct{}` semantics: `watchTask` closes it exactly once after its sole `cmd.Wait` returns.

- [ ] **Step 1: Write the failing tests**

Add a helper subprocess test that starts a cancellable long-running process, registers a `recordTask` containing `done: make(chan struct{})`, starts `watchTask`, and calls `StopRecording`. Assert the first call returns well below 30 seconds, `done` is closed, the task leaves the active map, and the database status is finalized. Add a second test where a pre-closed `done` channel proves `StopRecording` consumes completion without calling `Wait` itself.

- [ ] **Step 2: Run tests to verify they fail**

Run: `CGO_ENABLED=0 /usr/local/go/bin/go test ./internal/service -run 'TestStopRecording_(UsesWatcherCompletion|IsIdempotent)' -count=1 -v`

Expected: FAIL because `recordTask` has no shared completion channel and `StopRecording` independently calls `cmd.Wait`.

- [ ] **Step 3: Implement single-owner waiting**

Add `done chan struct{}` to `recordTask`, initialize it in both FFmpeg start paths, and make `watchTask` close it immediately after its single `cmd.Wait` call. Replace the local goroutine calling `Wait` in `StopRecording` with bounded selects on `task.done`; request cancellation first, force-kill only after the first bound, and return a controlled error after a second bound rather than blocking indefinitely.

- [ ] **Step 4: Run focused and package tests**

Run: `CGO_ENABLED=0 /usr/local/go/bin/go test ./internal/service -run 'TestStopRecording_' -count=1 -v`

Run: `CGO_ENABLED=0 /usr/local/go/bin/go test ./internal/service -count=1`

Expected: PASS with no 30-second wait.

- [ ] **Step 5: Commit**

```bash
git add internal/service/recorder.go internal/service/recorder_test.go
git commit -m "fix: serialize recording process shutdown"
```

### Task 2: Remove remaining competing wait/finalize paths

**Files:**
- Modify: `internal/service/recorder.go`
- Modify: `internal/service/recorder_test.go`

**Interfaces:**
- Consumes: `recordTask.done` from Task 1.
- Produces: shutdown/delete/sweep paths that reuse the stop lifecycle and never call `cmd.Wait`.

- [ ] **Step 1: Write failing lifecycle tests**

Add tests that run `Shutdown` and `DeleteRecording` while `watchTask` owns a real helper process. Assert each operation completes, the watcher channel closes, and the record/task/file outcomes match their public contracts. Add a sweep ownership test that verifies an already-claimed task is not finalized twice.

- [ ] **Step 2: Run tests to verify they fail**

Run: `CGO_ENABLED=0 /usr/local/go/bin/go test ./internal/service -run 'TestRecorderService_(Shutdown|DeleteRecording|Sweep)' -count=1 -v`

Expected: FAIL or expose the current duplicate `cmd.Wait` ownership.

- [ ] **Step 3: Route lifecycle operations through shared completion**

Make `Shutdown` stop active recording IDs through `StopRecording`. Make `DeleteRecording` invoke `StopRecording` for active recordings before removing the file and row. Make the sweeper finalize only when it successfully removes the same task from `s.tasks`; otherwise leave finalization to the owner that already claimed it.

- [ ] **Step 4: Run CGO-free service tests**

Run: `CGO_ENABLED=0 /usr/local/go/bin/go test ./internal/service -count=1`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/service/recorder.go internal/service/recorder_test.go
git commit -m "fix: share recorder shutdown lifecycle"
```

### Task 3: Backend integration verification

**Files:**
- Verify: `internal/api/recording.go`
- Verify: `internal/service/recorder.go`

**Interfaces:**
- Consumes: unchanged stop API and repaired recorder service.
- Produces: verified backend artifacts.

- [ ] **Step 1: Run all Go tests**

Run: `CGO_ENABLED=0 /usr/local/go/bin/go test ./... -count=1`

Expected: PASS.

- [ ] **Step 2: Build the server**

Run: `CGO_ENABLED=0 /usr/local/go/bin/go build -o /tmp/cameraio-stop-check ./cmd/server`

Expected: exit 0.

- [ ] **Step 3: Audit Wait ownership**

Run: `rg -n 'cmd\.Wait|\.Wait\(\)' internal/service/recorder.go`

Expected: the recording FFmpeg command is waited by `watchTask` only; unrelated probing commands are not affected.

- [ ] **Step 4: Commit any verification-driven correction**

```bash
git add internal/service/recorder.go internal/service/recorder_test.go
git commit -m "test: cover recording stop lifecycle"
```
