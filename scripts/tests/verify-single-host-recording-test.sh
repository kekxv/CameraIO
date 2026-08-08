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
echo 1.0
EOF
chmod +x "$test_root/bin/ffmpeg" "$test_root/bin/ffprobe"

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

# Resource evidence requires the full header and at least 30 minutes of timestamps.
printf 'timestamp_unix,host_cpu_percent,recording_cpu_percent_per_stream,free_disk_percent\n' >"$test_root/resources.csv"
if PATH="$test_root/bin:$PATH" "$script" --skip-build --smoke --sample-seconds 120 --log-dir "$test_root/logs" --resource-samples "$test_root/resources.csv"; then
  echo "header-only resource evidence unexpectedly accepted" >&2
  exit 1
fi

cat >"$test_root/resources.csv" <<'EOF'
timestamp_unix,host_cpu_percent,recording_cpu_percent_per_stream,free_disk_percent
1000,69,4,16
2800,69,4,16
EOF
sqlite3 "$test_root/recordings.db" 'CREATE TABLE recording_segments (id INTEGER, recording_id INTEGER, camera_id INTEGER, start_time TEXT, end_time TEXT, status TEXT); INSERT INTO recording_segments VALUES (1, 10, 1, "2026-01-01 00:00:00", "2026-01-01 00:01:00", "completed"); INSERT INTO recording_segments VALUES (2, 11, 1, "2026-01-01 01:00:00", "2026-01-01 01:01:00", "completed");'
# Separate sessions may be far apart; this is not an in-session continuity gap.
PATH="$test_root/bin:$PATH" "$script" --skip-build --smoke --sample-seconds 120 --log-dir "$test_root/logs" --database "$test_root/recordings.db" --resource-samples "$test_root/resources.csv" >/dev/null
sqlite3 "$test_root/recordings.db" 'INSERT INTO recording_segments VALUES (3, 10, 1, "2026-01-01 00:01:03", "2026-01-01 00:02:00", "completed");'
if PATH="$test_root/bin:$PATH" "$script" --skip-build --smoke --sample-seconds 120 --log-dir "$test_root/logs" --database "$test_root/recordings.db"; then
  echo "in-session continuity gap unexpectedly accepted" >&2
  exit 1
fi
echo "verify-single-host-recording command contract: PASS"
