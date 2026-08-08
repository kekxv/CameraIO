[CmdletBinding()]
param(
  [string]$SegmentsDir,
  [string]$Database,
  [string]$CameraUrl,
  [string]$LogDir = (Join-Path $PSScriptRoot '..\artifacts'),
  [ValidateRange(120, [int]::MaxValue)][int]$SampleSeconds = 300,
  [switch]$SkipBuild,
  [string]$LatencyBaseline,
  [string]$LatencyRecording,
  [string]$ResourceSamples,
  [switch]$Help
)

if ($Help) {
@'
Usage: scripts/verify-single-host-recording.ps1 [options]
Runs target-host build/test gates and behavior-level FFmpeg segment checks. It
does not modify CameraIO configuration or live code.

-SegmentsDir DIR       Probe completed production MP4 segments.
-Database PATH         Check recording_segments continuity in SQLite.
-CameraUrl URL         Probe camera codec/resolution/fps/bitrate (not logged).
-LogDir DIR            Acceptance-log directory (default: artifacts).
-SampleSeconds N       Generated sample duration; default: 300 (minimum: 120).
-SkipBuild             Skip Go/frontend gates for a focused rerun.
-LatencyBaseline FILE  One millisecond latency sample per line; >=30 lines.
-LatencyRecording FILE One millisecond latency sample per line; >=30 lines.
-ResourceSamples FILE  CSV: host_cpu_percent,recording_cpu_percent_per_stream,free_disk_percent.
'@
  exit 0
}

$ErrorActionPreference = 'Stop'
$root = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
New-Item -ItemType Directory -Force -Path $LogDir | Out-Null
$log = Join-Path $LogDir ("single-host-recording-{0}.log" -f (Get-Date).ToUniversalTime().ToString('yyyyMMddTHHmmssZ'))
Start-Transcript -Path $log | Out-Null
try {
  function Require([string]$Name) { if (-not (Get-Command $Name -ErrorAction SilentlyContinue)) { throw "required command not found: $Name" } }
  function Check-Segments([string]$Dir, [string]$Label) {
    $files = Get-ChildItem -LiteralPath $Dir -Filter '*.mp4' -File | Sort-Object Name
    if ($files.Count -lt 2) { throw "$Label: expected at least two MP4 segments in $Dir" }
    foreach ($file in $files) {
      $duration = & ffprobe -v error -show_entries format=duration -of default=noprint_wrappers=1:nokey=1 $file.FullName
      try { $numericDuration = [double]$duration } catch { throw "$Label: unplayable or zero-duration segment: $($file.FullName)" }
      if ($numericDuration -le 0) { throw "$Label: unplayable or zero-duration segment: $($file.FullName)" }
    }
    Write-Output "$Label: $($files.Count) independently playable MP4 segments"
  }
  function P95([string]$Path) {
    $values = Get-Content -LiteralPath $Path | Where-Object { $_.Trim() } | ForEach-Object { [double]$_ } | Sort-Object
    if ($values.Count -lt 30) { throw "Latency file $Path must contain at least 30 samples" }
    return $values[[math]::Floor(($values.Count - 1) * .95)]
  }

  Require ffmpeg; Require ffprobe
  Write-Output 'CameraIO single-host recording acceptance'
  Write-Output ("UTC: {0}" -f (Get-Date).ToUniversalTime().ToString('o'))
  $cpu = Get-CimInstance Win32_Processor | Select-Object -First 1 -ExpandProperty Name
  Write-Output "Host CPU: $cpu"
  Write-Output ("FFmpeg: " + ((& ffmpeg -version | Select-Object -First 1)))
  if ($CameraUrl) {
    Write-Output 'Camera stream (credentials redacted):'
    & ffprobe -v error -select_streams v:0 -show_entries stream=codec_name,width,height,r_frame_rate,bit_rate -of default=noprint_wrappers=1 $CameraUrl
  } else { Write-Output 'Camera stream: NOT COLLECTED (pass -CameraUrl)' }

  if (-not $SkipBuild) {
    Push-Location $root; try { $env:CGO_ENABLED = '0'; & go test ./...; if ($LASTEXITCODE) { throw 'Go tests failed' } } finally { Pop-Location }
    Push-Location (Join-Path $root 'frontend'); try { & npm test; if ($LASTEXITCODE) { throw 'Frontend unit tests failed' }; & npm run test:chrome72; if ($LASTEXITCODE) { throw 'Chrome 72 test failed' } } finally { Pop-Location }
  } else { Write-Output 'Build/test gates: SKIPPED by -SkipBuild' }

  $sampleDir = Join-Path ([IO.Path]::GetTempPath()) ("cameraio-segment-check-" + [guid]::NewGuid())
  New-Item -ItemType Directory -Path $sampleDir | Out-Null
  try {
    Write-Output "Generating $SampleSeconds`s FFmpeg segment sample in $sampleDir"
    & ffmpeg -hide_banner -loglevel error -y -f lavfi -i 'testsrc2=size=320x180:rate=10' -t $SampleSeconds -c:v mpeg4 -q:v 5 -g 10 -f segment -segment_time 60 -reset_timestamps 1 -segment_format mp4 -segment_format_options 'movflags=+frag_keyframe+empty_moov' (Join-Path $sampleDir 'sample-%03d.mp4')
    if ($LASTEXITCODE) { throw 'FFmpeg generated segment sample failed' }
    Check-Segments $sampleDir 'Generated segment safety sample'
  } finally { if (Test-Path $sampleDir) { Remove-Item -LiteralPath $sampleDir -Recurse -Force } }
  if ($SegmentsDir) { Check-Segments $SegmentsDir 'Production segment sample' }

  if ($Database) {
    Require sqlite3
    if (-not (Test-Path -LiteralPath $Database -PathType Leaf)) { throw "-Database does not exist: $Database" }
    $sql = "WITH ordered AS (SELECT camera_id,start_time,end_time,LAG(end_time) OVER (PARTITION BY camera_id ORDER BY start_time,id) AS previous_end FROM recording_segments WHERE status='completed') SELECT camera_id || ':' || previous_end || ' -> ' || start_time FROM ordered WHERE previous_end IS NOT NULL AND (julianday(start_time)-julianday(previous_end))*86400.0 > 2.0;"
    $gaps = & sqlite3 -noheader $Database $sql
    if ($gaps) { throw "Database segment continuity gaps >2 seconds: $gaps" }
    Write-Output 'Database segment continuity: no completed adjacent gap exceeds 2 seconds'
  } else { Write-Output 'Database segment continuity: NOT COLLECTED (pass -Database)' }

  if ($LatencyBaseline -or $LatencyRecording) {
    if (-not ($LatencyBaseline -and $LatencyRecording)) { throw 'Both latency files are required' }
    $baseline = Get-Content $LatencyBaseline | Where-Object { $_.Trim() } | ForEach-Object { [double]$_ }
    $recording = Get-Content $LatencyRecording | Where-Object { $_.Trim() } | ForEach-Object { [double]$_ }
    if (($baseline + $recording | Where-Object { $_ -ge 1000 }).Count) { throw 'Latency contract failed: every sample must be <1000 ms' }
    $baseP95 = P95 $LatencyBaseline; $recordingP95 = P95 $LatencyRecording
    if (($recordingP95 - $baseP95) -gt 100) { throw 'Latency contract failed: recording p95 increase exceeds 100 ms' }
    Write-Output "Latency baseline p95: $baseP95 ms; recording p95: $recordingP95 ms"
  } else { Write-Output 'Glass-to-glass latency: NOT COLLECTED (pass both latency files)' }

  if ($ResourceSamples) {
    $bad = Import-Csv -LiteralPath $ResourceSamples | Where-Object { [double]$_.host_cpu_percent -ge 70 -or [double]$_.recording_cpu_percent_per_stream -ge 5 -or [double]$_.free_disk_percent -le 15 }
    if ($bad) { throw 'Resource contract failed: require host CPU <70%, recording CPU <5%/stream, free disk >15%' }
    Write-Output 'Resource samples: host CPU, recorder CPU/stream, and free disk are within limits'
  } else { Write-Output '30-minute coexistence resource samples: NOT COLLECTED (pass -ResourceSamples)' }
  Write-Output "Acceptance log: $log"
  Write-Output 'Manual playback boundary checks and operating-envelope fields are documented in README.md.'
} finally { Stop-Transcript | Out-Null }
