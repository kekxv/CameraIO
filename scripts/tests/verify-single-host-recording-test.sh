#!/usr/bin/env bash
set -euo pipefail

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
script="$repo_root/scripts/verify-single-host-recording.sh"

if [[ ! -x "$script" ]]; then
  echo "expected executable verifier at $script" >&2
  exit 1
fi

help=$("$script" --help)
[[ "$help" == *"--segments-dir"* ]]
[[ "$help" == *"--database"* ]]
[[ "$help" == *"--skip-build"* ]]
[[ "$help" == *"--smoke"* ]]
[[ "$help" == *"timestamp_unix,host_cpu_percent,recording_cpu_percent_per_stream,free_disk_percent"* ]]
[[ "$help" == *"--acceptance-evidence"* ]]

test_root=$(mktemp -d)
trap 'rm -rf "$test_root"' EXIT
mkdir "$test_root/bin"
cat >"$test_root/bin/ffmpeg" <<'EOF'
#!/usr/bin/env bash
if [[ ${1:-} == "-version" ]]; then echo 'ffmpeg version test'; exit 0; fi
last=${!#}
mkdir -p "$(dirname "$last")"
touch "${last/\%03d/000}" "${last/\%03d/001}"
EOF
cat >"$test_root/bin/ffprobe" <<'EOF'
#!/usr/bin/env bash
if [[ ${!#} == rtsp://* ]]; then
  printf 'codec_name=h264\nwidth=1920\nheight=1080\nr_frame_rate=15/1\nbit_rate=1024000\n'
  exit 0
fi
echo 1.0
EOF
chmod +x "$test_root/bin/ffmpeg" "$test_root/bin/ffprobe"

# Acceptance mode may never succeed when target-host evidence is omitted.
if PATH="$test_root/bin:$PATH" "$script" --log-dir "$test_root/logs"; then
  echo "acceptance unexpectedly succeeded without mandatory evidence" >&2
  exit 1
fi

# A shortened generated sample is a smoke-only run, never an acceptance run.
if PATH="$test_root/bin:$PATH" "$script" --skip-build --sample-seconds 120 --log-dir "$test_root/logs"; then
  echo "short sample unexpectedly accepted without --smoke" >&2
  exit 1
fi
PATH="$test_root/bin:$PATH" "$script" --skip-build --smoke --sample-seconds 120 --log-dir "$test_root/logs" >/dev/null

# Blank/non-numeric samples cannot be silently omitted from latency percentile input.
for _ in $(seq 1 30); do echo 100; done >"$test_root/baseline.txt"
{ for _ in $(seq 1 29); do echo 110; done; echo; } >"$test_root/recording.txt"
if PATH="$test_root/bin:$PATH" "$script" --skip-build --smoke --sample-seconds 120 --log-dir "$test_root/logs" --latency-baseline "$test_root/baseline.txt" --latency-recording "$test_root/recording.txt"; then
  echo "blank latency sample unexpectedly accepted" >&2
  exit 1
fi

# Resource evidence requires the full header, once-per-minute cadence, and at
# least 31 samples spanning 30 minutes.
printf 'timestamp_unix,host_cpu_percent,recording_cpu_percent_per_stream,free_disk_percent\n' >"$test_root/resources.csv"
if PATH="$test_root/bin:$PATH" "$script" --skip-build --smoke --sample-seconds 120 --log-dir "$test_root/logs" --resource-samples "$test_root/resources.csv"; then
  echo "header-only resource evidence unexpectedly accepted" >&2
  exit 1
fi

{
  echo 'timestamp_unix,host_cpu_percent,recording_cpu_percent_per_stream,free_disk_percent'
  for index in $(seq 0 30); do
    echo "$((1000 + index * 60)),69,4,16"
  done
} >"$test_root/resources.csv"
sqlite3 "$test_root/recordings.db" 'CREATE TABLE recording_segments (id INTEGER, recording_id INTEGER, camera_id INTEGER, start_time TEXT, end_time TEXT, status TEXT); INSERT INTO recording_segments VALUES (1, 10, 1, "2026-01-01 00:00:00", "2026-01-01 00:01:00", "completed"); INSERT INTO recording_segments VALUES (2, 11, 1, "2026-01-01 01:00:00", "2026-01-01 01:01:00", "completed");'
# Separate sessions may be far apart; this is not an in-session continuity gap.
PATH="$test_root/bin:$PATH" "$script" --skip-build --smoke --sample-seconds 120 --log-dir "$test_root/logs" --database "$test_root/recordings.db" --resource-samples "$test_root/resources.csv" >/dev/null
sqlite3 "$test_root/recordings.db" 'INSERT INTO recording_segments VALUES (3, 10, 1, "2026-01-01 00:01:03", "2026-01-01 00:02:00", "completed");'
if PATH="$test_root/bin:$PATH" "$script" --skip-build --smoke --sample-seconds 120 --log-dir "$test_root/logs" --database "$test_root/recordings.db"; then
  echo "in-session continuity gap unexpectedly accepted" >&2
  exit 1
fi
sqlite3 "$test_root/recordings.db" 'DELETE FROM recording_segments WHERE id = 3;'

# Nearest-rank p95 for 30 samples is the 29th ordered value, not the 28th.
for _ in $(seq 1 30); do echo 100; done >"$test_root/baseline.txt"
{ for _ in $(seq 1 28); do echo 100; done; echo 900; echo 900; } >"$test_root/recording.txt"
if PATH="$test_root/bin:$PATH" "$script" --skip-build --smoke --sample-seconds 120 --log-dir "$test_root/logs" --latency-baseline "$test_root/baseline.txt" --latency-recording "$test_root/recording.txt"; then
  echo "incorrect p95 rank accepted a recording latency regression" >&2
  exit 1
fi

# A complete target-host evidence package admits acceptance mode.
for _ in $(seq 1 30); do echo 100; done >"$test_root/recording.txt"
mkdir -p "$test_root/production-segments"
touch "$test_root/production-segments/one.mp4" "$test_root/production-segments/two.mp4"
cat >"$test_root/acceptance.csv" <<'EOF'
field,value
playback_beginning,pass
playback_middle,pass
playback_final_second,pass
playback_segment_boundary,pass
playback_gap_visible,pass
max_seek_error_ms,1000
max_boundary_pause_ms,250
ffmpeg_priority_below_normal,pass
self_service_workload,pass
self_service_timeout_error_delta,0
max_recording_cameras,4
max_preview_tiles,9
per_camera_bitrate_kbps,1024
retention_days,30
disk_capacity_gb,1000
EOF
cat >"$test_root/bin/go" <<'EOF'
#!/usr/bin/env bash
exit 0
EOF
cat >"$test_root/bin/npm" <<'EOF'
#!/usr/bin/env bash
exit 0
EOF
chmod +x "$test_root/bin/go" "$test_root/bin/npm"
PATH="$test_root/bin:$PATH" "$script" --log-dir "$test_root/logs" \
  --camera-url 'rtsp://camera/live' --segments-dir "$test_root/production-segments" \
  --database "$test_root/recordings.db" --latency-baseline "$test_root/baseline.txt" \
  --latency-recording "$test_root/recording.txt" --resource-samples "$test_root/resources.csv" \
  --acceptance-evidence "$test_root/acceptance.csv" >/dev/null
echo "verify-single-host-recording command contract: PASS"
