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
  --acceptance-evidence FILE CSV evidence for playback boundaries, process priority,
                             self-service workload, and measured operating envelope.
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
acceptance_evidence_file=""
smoke=false

while (($#)); do
  case "$1" in
    --segments-dir|--database|--camera-url|--log-dir|--sample-seconds|--latency-baseline|--latency-recording|--resource-samples|--acceptance-evidence)
      (($# >= 2)) || { echo "missing value for $1" >&2; exit 2; }
      case "$1" in
        --segments-dir) segments_dir=$2 ;; --database) database=$2 ;; --camera-url) camera_url=$2 ;;
        --log-dir) log_dir=$2 ;; --sample-seconds) sample_seconds=$2 ;;
        --latency-baseline) baseline_file=$2 ;; --latency-recording) recording_file=$2 ;;
        --resource-samples) resource_file=$2 ;;
        --acceptance-evidence) acceptance_evidence_file=$2 ;;
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
  $skip_build && { echo "acceptance runs cannot use --skip-build; use --smoke for a focused non-acceptance rerun" >&2; exit 2; }
  missing=()
  [[ -n $camera_url ]] || missing+=(--camera-url)
  [[ -n $segments_dir ]] || missing+=(--segments-dir)
  [[ -n $database ]] || missing+=(--database)
  [[ -n $baseline_file ]] || missing+=(--latency-baseline)
  [[ -n $recording_file ]] || missing+=(--latency-recording)
  [[ -n $resource_file ]] || missing+=(--resource-samples)
  [[ -n $acceptance_evidence_file ]] || missing+=(--acceptance-evidence)
  ((${#missing[@]} == 0)) || { echo "acceptance requires mandatory target-host evidence: ${missing[*]}" >&2; exit 2; }
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
  camera_info=$(ffprobe -v error -select_streams v:0 -show_entries stream=codec_name,width,height,r_frame_rate,bit_rate -of default=noprint_wrappers=1 "$camera_url")
  echo "$camera_info"
  awk -F= '$1 == "codec_name" && tolower($2) == "h264" { found=1 } END { exit !found }' <<<"$camera_info" || {
    echo "Camera stream contract failed: resource-safe acceptance requires an actual H.264 camera stream" >&2
    exit 1
  }
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

percentile95() { sort -n "$1" | awk '{v[NR]=$1} END { if (NR < 30) exit 2; rank=int(NR*.95); if (rank < NR*.95) rank++; print v[rank] }'; }
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
    n > 0 && ($1 <= previous || ($1 - previous) > 60) { invalid = 1 }
    { previous = $1; last = $1; n++; if ($2 >= 70 || $3 >= 5 || $4 <= 15) limits = 1 }
    END { exit !(validHeader && !invalid && n >= 31 && (last - first) >= 1800 && !limits) }
  ' "$resource_file" || { echo "Resource contract failed: require >=31 numeric samples at least once per 60 seconds spanning >=30 minutes, host CPU <70%, recording CPU <5%/stream, free disk >15%" >&2; exit 1; }
  echo "Resource samples: at least 31 once-per-minute samples spanning 30+ minutes are within limits"
else
  echo "30-minute coexistence resource samples: NOT COLLECTED (pass --resource-samples)"
fi

validate_acceptance_evidence() {
  awk -F, '
    NR == 1 { header = ($0 == "field,value"); next }
    NF != 2 || seen[$1]++ { invalid = 1; next }
    { value[$1] = $2; rows++ }
    END {
      pass = value["playback_beginning"] == "pass" && value["playback_middle"] == "pass" &&
        value["playback_final_second"] == "pass" && value["playback_segment_boundary"] == "pass" &&
        value["playback_gap_visible"] == "pass" && value["ffmpeg_priority_below_normal"] == "pass" &&
        value["self_service_workload"] == "pass"
      numeric = value["max_seek_error_ms"] ~ /^[0-9]+([.][0-9]+)?$/ && value["max_seek_error_ms"] <= 1000 &&
        value["max_boundary_pause_ms"] ~ /^[0-9]+([.][0-9]+)?$/ && value["max_boundary_pause_ms"] <= 250 &&
        value["self_service_timeout_error_delta"] ~ /^-?[0-9]+([.][0-9]+)?$/ && value["self_service_timeout_error_delta"] <= 0
      envelope = value["max_recording_cameras"] ~ /^[1-9][0-9]*$/ && value["max_preview_tiles"] ~ /^[1-9][0-9]*$/ &&
        value["per_camera_bitrate_kbps"] ~ /^[1-9][0-9]*$/ && value["retention_days"] ~ /^[1-9][0-9]*$/ &&
        value["disk_capacity_gb"] ~ /^[1-9][0-9]*([.][0-9]+)?$/
      exit !(header && !invalid && rows == 15 && pass && numeric && envelope)
    }
  ' "$1"
}

if [[ -n $acceptance_evidence_file ]]; then
  [[ -f $acceptance_evidence_file ]] || { echo "--acceptance-evidence does not exist: $acceptance_evidence_file" >&2; exit 2; }
  validate_acceptance_evidence "$acceptance_evidence_file" || {
    echo "Acceptance evidence contract failed: require all documented playback, priority, self-service, and operating-envelope fields" >&2
    exit 1
  }
  echo "Playback boundaries, below-normal FFmpeg priority, self-service workload, and operating envelope: validated"
elif $smoke; then
  echo "Acceptance observations: NOT COLLECTED (allowed only for non-acceptance smoke)"
fi

echo "Acceptance log: $log"
if $smoke; then
  echo "NON-ACCEPTANCE SMOKE COMPLETE"
else
  echo "TARGET-HOST ACCEPTANCE PASSED"
fi
