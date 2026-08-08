# Task 9 verification harness report

## Delivered

- `scripts/verify-single-host-recording.sh`: Linux target-host acceptance
  harness. It runs the required Go/frontend gates, produces a timestamped log,
  creates a five-minute FFmpeg MP4 segment sample, probes every produced (and
  optional production) segment, checks optional SQLite segment continuity, and
  evaluates collected latency/resource samples against the acceptance limits.
- `scripts/verify-single-host-recording.ps1`: equivalent Windows PowerShell
  harness.
- `scripts/tests/verify-single-host-recording-test.sh`: command-contract smoke
  test written before the Linux harness.
- `README.md`: target-host procedure, physical glass-to-glass method,
  resource/playback checks, tuning order, and an intentionally unfilled
  operating-envelope table.

## Adjustment applied

The superseded source-grep audit was not implemented. Automated validation is
behavior-level: repository test gates plus real FFmpeg segment generation and
`ffprobe` playability checks; a target-host run can additionally inspect actual
segment files and the SQLite continuity records. No `internal/service/stream.go`,
`internal/api/stream.go`, `frontend/src/views/Live.vue`, or other live code was
modified.

## Evidence run in this workspace

| Check | Result |
|---|---|
| `bash scripts/tests/verify-single-host-recording-test.sh` | Passed |
| `bash -n scripts/verify-single-host-recording.sh` | Passed |
| `scripts/verify-single-host-recording.sh --help` | Passed |
| `CGO_ENABLED=0 go test ./...` | Blocked: `go` is not installed in this development workspace |
| `cd frontend && npm test && npm run test:chrome72` | Passed: 20 unit tests and Chrome 72 check |
| `git diff --check` | Passed |
| Focused behavior run with `--skip-build --sample-seconds 120` | Blocked: `ffmpeg` is not installed in this development workspace |

## Target-host evidence still required

Run the full harness on the i5 host with FFmpeg, camera/segment/database paths,
30+ baseline and recording latency samples, and the 30-minute resource CSV. The
manual playback-boundary observations and measured deployment envelope must then
replace the `未测量` row in the README. No latency, CPU, capacity, or deployment
claim is made by this change.
