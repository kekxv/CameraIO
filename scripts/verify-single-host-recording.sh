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
  --sample-seconds N       Generated FFmpeg sample duration; default: 300 (minimum: 120).
  --skip-build             Skip Go and frontend gates (only for a focused rerun).
  --latency-baseline FILE  One millisecond latency sample per line; at least 30 lines.
  --latency-recording FILE One millisecond latency sample per line; at least 30 lines.
  --resource-samples FILE  CSV: host_cpu_percent,recording_cpu_percent_per_stream,free_disk_percent.
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
    -h|--help) usage; exit 0 ;;
    *) echo "unknown option: $1" >&2; usage >&2; exit 2 ;;
  esac
done

[[ $sample_seconds =~ ^[0-9]+$ ]] && ((sample_seconds >= 120)) || { echo "--sample-seconds must be an integer >= 120" >&2; exit 2; }
mkdir -p "$log_dir"
log="$log_dir/single-host-recording-$(date -u +%Y%m%dT%H%M%SZ).log"
exec > >(tee "$log") 2>&1

require() { command -v "$1" >/dev/null || { echo "required command not found: $1" >&2; exit 1; }; }
require ffmpeg; require ffprobe; require awk; require sort

echo "CameraIO single-host recording acceptance"
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
trap 'rm -rf "$sample_dir"' EXIT
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
  gaps=$(sqlite3 -noheader "$database" "WITH ordered AS (SELECT camera_id,start_time,end_time,LAG(end_time) OVER (PARTITION BY camera_id ORDER BY start_time,id) AS previous_end FROM recording_segments WHERE status='completed') SELECT camera_id || ':' || previous_end || ' -> ' || start_time FROM ordered WHERE previous_end IS NOT NULL AND (julianday(start_time)-julianday(previous_end))*86400.0 > 2.0;")
  [[ -z $gaps ]] || { echo "Database segment continuity gaps >2 seconds:"; echo "$gaps"; exit 1; }
  echo "Database segment continuity: no completed adjacent gap exceeds 2 seconds"
else
  echo "Database segment continuity: NOT COLLECTED (pass --database)"
fi

percentile95() { sort -n "$1" | awk '{v[NR]=$1} END { if (NR < 30) exit 2; n=int((NR-1)*.95)+1; print v[n] }'; }
if [[ -n $baseline_file || -n $recording_file ]]; then
  [[ -n $baseline_file && -n $recording_file && -f $baseline_file && -f $recording_file ]] || { echo "both readable latency files are required" >&2; exit 2; }
  awk 'NF && ($1 !~ /^[0-9]+(\.[0-9]+)?$/ || $1 >= 1000) { exit 1 }' "$baseline_file" "$recording_file" || { echo "Latency contract failed: every sample must be <1000 ms" >&2; exit 1; }
  base_p95=$(percentile95 "$baseline_file"); recording_p95=$(percentile95 "$recording_file")
  awk -v b="$base_p95" -v r="$recording_p95" 'BEGIN { exit !((r-b) <= 100) }' || { echo "Latency contract failed: recording p95 increase exceeds 100 ms" >&2; exit 1; }
  echo "Latency baseline p95: ${base_p95} ms; recording p95: ${recording_p95} ms"
else
  echo "Glass-to-glass latency: NOT COLLECTED (pass both latency files after manual clock test)"
fi

if [[ -n $resource_file ]]; then
  [[ -f $resource_file ]] || { echo "--resource-samples does not exist: $resource_file" >&2; exit 2; }
  awk -F, 'NR > 1 { if ($1 >= 70 || $2 >= 5 || $3 <= 15) exit 1; n++ } END { exit !(n > 0) }' "$resource_file" || { echo "Resource contract failed: require host CPU <70%, recording CPU <5%/stream, free disk >15%" >&2; exit 1; }
  echo "Resource samples: host CPU, recorder CPU/stream, and free disk are within limits"
else
  echo "30-minute coexistence resource samples: NOT COLLECTED (pass --resource-samples)"
fi

echo "Acceptance log: $log"
echo "Manual playback boundary checks and operating-envelope fields are documented in README.md."
