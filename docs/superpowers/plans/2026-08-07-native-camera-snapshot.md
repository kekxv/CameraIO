# Native Camera Snapshot Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Return a camera-native JPEG snapshot without starting RTSP, FFmpeg, or MJPEG preview.

**Architecture:** `CameraService` resolves an ONVIF Media profile for the selected IPC/NVR channel, obtains its `GetSnapshotUri` URL, then fetches the resulting JPEG with the stored device credentials. The API streams those bytes directly as `image/jpeg`; preview start/stop remain explicit stream operations.

**Tech Stack:** Go, ONVIF Media SOAP, `net/http`, Gin, GORM/SQLite tests.

## Global Constraints

- Do not start `StreamService`, FFmpeg, or MJPEG to capture a snapshot.
- Use ONVIF `GetSnapshotUri` rather than vendor-specific guessed paths.
- Bound device and snapshot HTTP work by the request context and a short timeout.
- Never expose device credentials or an authenticated snapshot URL in API responses or errors.
- Keep `main` as the delivery branch.

---

### Task 1: Resolve the native ONVIF snapshot endpoint

**Files:**
- Modify: `internal/service/onvif_test.go`
- Modify: `internal/service/onvif.go`

**Interfaces:**
- Produces: `GetSnapshotURI(ctx, ip, user, pass, profileToken) (string, error)`.
- Produces: `FindProfileToken(ctx, ip, user, pass, nvrChannel) (string, error)`.

- [x] **Step 1: Write failing tests**

Add a test whose ONVIF response contains `<tt:Uri>http://camera/snapshot.jpg</tt:Uri>` and asserts the returned URI, plus a profile-selection test asserting that the requested NVR channel uses its matching profile token.

- [x] **Step 2: Run tests to verify they fail**

Run: `CGO_ENABLED=1 /usr/local/go/bin/go test ./internal/service -run 'Test(GetSnapshotURI|FindProfileToken)' -count=1 -v`

Expected: compile failure because the snapshot methods do not exist.

- [x] **Step 3: Implement minimal ONVIF support**

Build a `GetSnapshotUri` SOAP envelope with XML-escaped profile token, call the Media service with existing WS-UsernameToken support, and extract the `Uri`. Resolve the profile through `GetProfiles`, selecting a matching NVR channel or the first profile for a direct IPC.

- [x] **Step 4: Run focused tests**

Run: `CGO_ENABLED=1 /usr/local/go/bin/go test ./internal/service -run 'Test(GetSnapshotURI|FindProfileToken)' -count=1 -v`

Expected: PASS.

### Task 2: Return JPEG through the protected camera API

**Files:**
- Modify: `internal/service/camera_service.go`
- Modify: `internal/service/onvif.go`
- Modify: `internal/service/onvif_test.go`
- Modify: `internal/api/camera.go`
- Modify: `internal/api/router.go`
- Modify: `internal/api/api_test.go`

**Interfaces:**
- Produces: `CameraService.CaptureSnapshot(ctx, cameraID) ([]byte, error)`.
- Produces: `GET /api/v1/cameras/:id/snapshot` with `Content-Type: image/jpeg`.

- [x] **Step 1: Write failing tests**

Add an HTTP test that requests the new route without a token and expects `401`. Add service tests using a local ONVIF/Snapshot HTTP server that assert the JPEG payload is returned and that no stream service is needed.

- [x] **Step 2: Run tests to verify they fail**

Run: `CGO_ENABLED=1 /usr/local/go/bin/go test ./internal/api ./internal/service -run 'Test(CameraSnapshot|CaptureSnapshot)' -count=1 -v`

Expected: route or method is unavailable.

- [x] **Step 3: Implement direct capture**

Have `CaptureSnapshot` load the camera, reject non-RTSP sources, resolve its profile URI, issue an authenticated bounded HTTP GET, verify a successful JPEG response, and return bytes. The handler writes only JPEG bytes and no JSON wrapper.

- [x] **Step 4: Run focused tests**

Run: `CGO_ENABLED=1 /usr/local/go/bin/go test ./internal/api ./internal/service -run 'Test(CameraSnapshot|CaptureSnapshot)' -count=1 -v`

Expected: PASS.

### Task 3: Publish the correct kiosk lifecycle

**Files:**
- Modify: `KIOSK_API.md`

- [x] **Step 1: Document capture and preview separately**

Document `GET /cameras/{id}/snapshot` as a direct JPEG operation, `POST /streams/{id}/start`, `GET /streams/{id}/mjpeg`, and `POST /streams/{id}/stop` as the explicit preview lifecycle. Update the JavaScript example accordingly.

- [ ] **Step 2: Verify the repository**

Run:

```sh
CGO_ENABLED=1 /usr/local/go/bin/go test ./internal/service -count=1
CGO_ENABLED=1 /usr/local/go/bin/go test ./internal/api -count=1
CGO_ENABLED=1 /usr/local/go/bin/go test ./... -count=1
CGO_ENABLED=1 /usr/local/go/bin/go test -race ./internal/service -count=1
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 /usr/local/go/bin/go test -c -o /tmp/cameraio-service-windows.test.exe ./internal/service
CGO_ENABLED=1 /usr/local/go/bin/go build -o /tmp/cameraio-check ./cmd/server
git diff --check
```

- [ ] **Step 3: Commit**

```sh
git add internal/service/onvif.go internal/service/onvif_test.go internal/service/camera_service.go internal/api/camera.go internal/api/router.go internal/api/api_test.go KIOSK_API.md docs/superpowers/plans/2026-08-07-native-camera-snapshot.md
git commit -m "feat: add native camera snapshots"
```
