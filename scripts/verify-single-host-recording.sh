#!/usr/bin/env bash
# Target-host acceptance harness for single-host segmented recording.
set -euo pipefail

usage() {
  cat <<'EOF'
Usage: scripts/verify-single-host-recording.sh [options]

Runs build/test gates and behavior-level FFmpeg segment checks, then records
target-host evidence. It never changes CameraIO configuration or live code.

Options:
  --segments-dir DIR       Probe completed production MP4 segments in DIR.
  --database PATH          Check recording_segments continuity in SQLite PATH.
  --camera-url URL         Probe camera codec/resolution/fps/bitrate (URL is not logged).
  --log-dir DIR            Write the timestamped acceptance log here (default: artifacts).
  --sample-seconds N       300-second acceptance sample (shorter values require --smoke).
  --smoke                  Mark a shortened run as non-acceptance verification only.
  --skip-build             Skip Go and frontend gates (only for a focused rerun).
  --latency-baseline FILE  One millisecond latency sample per line; at least 30 lines.
  --latency-recording FILE One millisecond latency sample per line; at least 30 lines.
  --resource-samples FILE  CSV: timestamp_unix,host_cpu_percent,recording_cpu_percent_per_stream,free_disk_percent.
  -h, --help               Show this help.
EOF
}

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
segments_dir=""
database=""
camera_url=""
log_dir="$root/artifacts"
sample_seconds=300
skip_build=false
baseline_file=""
recording_file=""
resource_file=""
smoke=false

while (($#)); do
  case "$1" in
    --segments-dir|--database|--camera-url|--log-dir|--sample-seconds|--latency-baseline|--latency-recording|--resource-samples)
      (($# >= 2)) || { echo "missing value for $1" >&2; exit 2; }
      case "$1" in
        --segments-dir) segments_dir=$2 ;; --database) database=$2 ;; --camera-url) camera_url=$2 ;;
        --log-dir) log_dir=$2 ;; --sample-seconds) sample_seconds=$2 ;;
        --latency-baseline) baseline_file=$2 ;; --latency-recording) recording_file=$2 ;;
        --resource-samples) resource_file=$2 ;;
      esac
      shift 2 ;;
    --skip-build) skip_build=true; shift ;;
    --smoke) smoke=true; shift ;;
    -h|--help) usage; exit 0 ;;
    *) echo "unknown option: $1" >&2; usage >&2; exit 2 ;;
  esac
done

[[ $sample_seconds =~ ^[0-9]+$ ]] || { echo "--sample-seconds must be an integer" >&2; exit 2; }
if $smoke; then
  ((sample_seconds >= 120 && sample_seconds < 300)) || { echo "--smoke requires --sample-seconds from 120 through 299" >&2; exit 2; }
else
  ((sample_seconds == 300)) || { echo "acceptance runs require --sample-seconds 300; use --smoke for a shorter non-acceptance run" >&2; exit 2; }
fi
mkdir -p "$log_dir"
log="$log_dir/single-host-recording-$(date -u +%Y%m%dT%H%M%SZ).log"
exec > >(tee "$log") 2>&1

require() { command -v "$1" >/dev/null || { echo "required command not found: $1" >&2; exit 1; }; }
require ffmpeg; require ffprobe; require awk; require sort

if $smoke; then
  echo "CameraIO single-host recording NON-ACCEPTANCE SMOKE RUN"
  echo "This shortened run cannot satisfy the target-host acceptance contract."
else
  echo "CameraIO single-host recording acceptance"
fi
echo "UTC: $(date -u +%FT%TZ)"
echo "Host CPU: $(lscpu 2>/dev/null | awk -F: '/Model name:/ {sub(/^ +/, "", $2); print $2; exit}' || uname -m)"
echo "FFmpeg: $(ffmpeg -version | head -1)"

if [[ -n $camera_url ]]; then
  echo "Camera stream (credentials redacted):"
  ffprobe -v error -select_streams v:0 -show_entries stream=codec_name,width,height,r_frame_rate,bit_rate -of default=noprint_wrappers=1 "$camera_url"
else
  echo "Camera stream: NOT COLLECTED (pass --camera-url)"
fi

if ! $skip_build; then
  (cd "$root" && CGO_ENABLED=0 go test ./...)
  (cd "$root/frontend" && npm test && npm run test:chrome72)
else
  echo "Build/test gates: SKIPPED by --skip-build"
fi

sample_dir=$(mktemp -d "${TMPDIR:-/tmp}/cameraio-segment-check.XXXXXX")
latency_dir=""
trap 'rm -rf "$sample_dir" "$latency_dir"' EXIT
echo "Generating ${sample_seconds}s FFmpeg segment sample in $sample_dir"
ffmpeg -hide_banner -loglevel error -y -f lavfi -i "testsrc2=size=320x180:rate=10" -t "$sample_seconds" \
  -c:v mpeg4 -q:v 5 -g 10 -f segment -segment_time 60 -reset_timestamps 1 \
  -segment_format mp4 -segment_format_options movflags=+frag_keyframe+empty_moov "$sample_dir/sample-%03d.mp4"

check_segments() {
  local dir=$1 label=$2 count=0 duration
  shopt -s nullglob
  local files=("$dir"/*.mp4)
  shopt -u nullglob
  ((${#files[@]} >= 2)) || { echo "$label: expected at least two MP4 segments in $dir" >&2; return 1; }
  for file in "${files[@]}"; do
    duration=$(ffprobe -v error -show_entries format=duration -of default=noprint_wrappers=1:nokey=1 "$file")
    awk -v d="$duration" 'BEGIN { exit !(d > 0) }' || { echo "$label: unplayable or zero-duration segment: $file" >&2; return 1; }
    count=$((count + 1))
  done
  echo "$label: $count independently playable MP4 segments"
}
check_segments "$sample_dir" "Generated segment safety sample"
[[ -z $segments_dir ]] || { [[ -d $segments_dir ]] || { echo "--segments-dir does not exist: $segments_dir" >&2; exit 2; }; check_segments "$segments_dir" "Production segment sample"; }

if [[ -n $database ]]; then
  require sqlite3
  [[ -f $database ]] || { echo "--database does not exist: $database" >&2; exit 2; }
  gaps=$(sqlite3 -noheader "$database" "WITH ordered AS (SELECT recording_id,camera_id,start_time,end_time,LAG(end_time) OVER (PARTITION BY recording_id,camera_id ORDER BY start_time,id) AS previous_end FROM recording_segments WHERE status='completed') SELECT recording_id || '/' || camera_id || ':' || previous_end || ' -> ' || start_time FROM ordered WHERE previous_end IS NOT NULL AND (julianday(start_time)-julianday(previous_end))*86400.0 > 2.0;")
  [[ -z $gaps ]] || { echo "Database segment continuity gaps >2 seconds:"; echo "$gaps"; exit 1; }
  echo "Database segment continuity: no completed adjacent gap exceeds 2 seconds"
else
  echo "Database segment continuity: NOT COLLECTED (pass --database)"
fi

percentile95() { sort -n "$1" | awk '{v[NR]=$1} END { if (NR < 30) exit 2; n=int((NR-1)*.95)+1; print v[n] }'; }
validate_latency() {
  local input=$1 output=$2 label=$3
  awk 'NF != 1 || $1 !~ /^[0-9]+(\.[0-9]+)?$/ { exit 1 } { print $1 }' "$input" >"$output" || {
    echo "$label latency samples must be nonblank numeric milliseconds, one per line" >&2
    return 1
  }
  [[ $(wc -l <"$output") -ge 30 ]] || { echo "$label latency samples must contain at least 30 values" >&2; return 1; }
}
if [[ -n $baseline_file || -n $recording_file ]]; then
  [[ -n $baseline_file && -n $recording_file && -f $baseline_file && -f $recording_file ]] || { echo "both readable latency files are required" >&2; exit 2; }
  latency_dir=$(mktemp -d "${TMPDIR:-/tmp}/cameraio-latency-check.XXXXXX")
  validate_latency "$baseline_file" "$latency_dir/baseline" "Baseline"
  validate_latency "$recording_file" "$latency_dir/recording" "Recording"
  awk '$1 >= 1000 { exit 1 }' "$latency_dir/baseline" "$latency_dir/recording" || { echo "Latency contract failed: every sample must be <1000 ms" >&2; exit 1; }
  base_p95=$(percentile95 "$latency_dir/baseline"); recording_p95=$(percentile95 "$latency_dir/recording")
  awk -v b="$base_p95" -v r="$recording_p95" 'BEGIN { exit !((r-b) <= 100) }' || { echo "Latency contract failed: recording p95 increase exceeds 100 ms" >&2; exit 1; }
  echo "Latency baseline p95: ${base_p95} ms; recording p95: ${recording_p95} ms"
else
  echo "Glass-to-glass latency: NOT COLLECTED (pass both latency files after manual clock test)"
fi

if [[ -n $resource_file ]]; then
  [[ -f $resource_file ]] || { echo "--resource-samples does not exist: $resource_file" >&2; exit 2; }
  awk -F, '
    NR == 1 { validHeader = ($0 == "timestamp_unix,host_cpu_percent,recording_cpu_percent_per_stream,free_disk_percent"); next }
    NF != 4 || $1 !~ /^[0-9]+(\.[0-9]+)?$/ || $2 !~ /^[0-9]+(\.[0-9]+)?$/ || $3 !~ /^[0-9]+(\.[0-9]+)?$/ || $4 !~ /^[0-9]+(\.[0-9]+)?$/ { invalid = 1; next }
    n == 0 { first = $1 }
    n > 0 && $1 < previous { invalid = 1 }
    { previous = $1; last = $1; n++; if ($2 >= 70 || $3 >= 5 || $4 <= 15) limits = 1 }
    END { exit !(validHeader && !invalid && n >= 2 && (last - first) >= 1800 && !limits) }
  ' "$resource_file" || { echo "Resource contract failed: require exact timestamp_unix header, numeric samples spanning >=30 minutes, host CPU <70%, recording CPU <5%/stream, free disk >15%" >&2; exit 1; }
  echo "Resource samples: 30+ minutes of numeric host CPU, recorder CPU/stream, and free disk evidence are within limits"
else
  echo "30-minute coexistence resource samples: NOT COLLECTED (pass --resource-samples)"
fi

echo "Acceptance log: $log"
echo "Manual playback boundary checks and operating-envelope fields are documented in README.md."
