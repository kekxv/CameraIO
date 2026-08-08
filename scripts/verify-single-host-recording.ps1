[CmdletBinding()]
param(
  [string]$SegmentsDir,
  [string]$Database,
  [string]$CameraUrl,
  [string]$LogDir = (Join-Path $PSScriptRoot '..\artifacts'),
  [int]$SampleSeconds = 300,
  [switch]$Smoke,
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
-SampleSeconds N       300-second acceptance sample (shorter values require -Smoke).
-Smoke                  Mark a shortened run as non-acceptance verification only.
-SkipBuild             Skip Go/frontend gates for a focused rerun.
-LatencyBaseline FILE  One millisecond latency sample per line; >=30 lines.
-LatencyRecording FILE One millisecond latency sample per line; >=30 lines.
-ResourceSamples FILE  CSV: host_cpu_percent,recording_cpu_percent_per_stream,free_disk_percent.
'@
  exit 0
}

$ErrorActionPreference = 'Stop'
if ($Smoke) {
  if ($SampleSeconds -lt 120 -or $SampleSeconds -ge 300) { throw '-Smoke requires -SampleSeconds from 120 through 299' }
} elseif ($SampleSeconds -ne 300) {
  throw 'Acceptance runs require -SampleSeconds 300; use -Smoke for a shorter non-acceptance run'
}
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
  function Read-Latency([string]$Path, [string]$Label) {
    $values = @()
    foreach ($line in Get-Content -LiteralPath $Path) {
      if ($line -notmatch '^[0-9]+(\.[0-9]+)?$') { throw "$Label latency samples must be nonblank numeric milliseconds, one per line" }
      try { $value = [double]$line } catch { throw "$Label latency samples must be nonblank numeric milliseconds, one per line" }
      $values += $value
    }
    if ($values.Count -lt 30) { throw "$Label latency samples must contain at least 30 values" }
    return ,$values
  }
  function P95([double[]]$Values) {
    $ordered = $Values | Sort-Object
    return $ordered[[math]::Floor(($ordered.Count - 1) * .95)]
  }

  Require ffmpeg; Require ffprobe
  if ($Smoke) {
    Write-Output 'CameraIO single-host recording NON-ACCEPTANCE SMOKE RUN'
    Write-Output 'This shortened run cannot satisfy the target-host acceptance contract.'
  } else { Write-Output 'CameraIO single-host recording acceptance' }
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
    $sql = "WITH ordered AS (SELECT recording_id,camera_id,start_time,end_time,LAG(end_time) OVER (PARTITION BY recording_id,camera_id ORDER BY start_time,id) AS previous_end FROM recording_segments WHERE status='completed') SELECT recording_id || '/' || camera_id || ':' || previous_end || ' -> ' || start_time FROM ordered WHERE previous_end IS NOT NULL AND (julianday(start_time)-julianday(previous_end))*86400.0 > 2.0;"
    $gaps = & sqlite3 -noheader $Database $sql
    if ($gaps) { throw "Database segment continuity gaps >2 seconds: $gaps" }
    Write-Output 'Database segment continuity: no completed adjacent gap exceeds 2 seconds'
  } else { Write-Output 'Database segment continuity: NOT COLLECTED (pass -Database)' }

  if ($LatencyBaseline -or $LatencyRecording) {
    if (-not ($LatencyBaseline -and $LatencyRecording)) { throw 'Both latency files are required' }
    $baseline = Read-Latency $LatencyBaseline 'Baseline'
    $recording = Read-Latency $LatencyRecording 'Recording'
    if (($baseline + $recording | Where-Object { $_ -ge 1000 }).Count) { throw 'Latency contract failed: every sample must be <1000 ms' }
    $baseP95 = P95 $baseline; $recordingP95 = P95 $recording
    if (($recordingP95 - $baseP95) -gt 100) { throw 'Latency contract failed: recording p95 increase exceeds 100 ms' }
    Write-Output "Latency baseline p95: $baseP95 ms; recording p95: $recordingP95 ms"
  } else { Write-Output 'Glass-to-glass latency: NOT COLLECTED (pass both latency files)' }

  if ($ResourceSamples) {
    $header = Get-Content -LiteralPath $ResourceSamples -TotalCount 1
    if ($header -ne 'timestamp_unix,host_cpu_percent,recording_cpu_percent_per_stream,free_disk_percent') { throw 'Resource samples require the exact timestamp_unix header' }
    $rows = @(Import-Csv -LiteralPath $ResourceSamples)
    if ($rows.Count -lt 2) { throw 'Resource samples require at least two timestamped rows spanning 30 minutes' }
    $first = $null; $previous = $null
    foreach ($row in $rows) {
      if ($row.timestamp_unix -notmatch '^[0-9]+(\.[0-9]+)?$' -or $row.host_cpu_percent -notmatch '^[0-9]+(\.[0-9]+)?$' -or $row.recording_cpu_percent_per_stream -notmatch '^[0-9]+(\.[0-9]+)?$' -or $row.free_disk_percent -notmatch '^[0-9]+(\.[0-9]+)?$') { throw 'Resource samples must contain numeric timestamp/CPU/disk fields' }
      try { $timestamp = [double]$row.timestamp_unix; $hostCpu = [double]$row.host_cpu_percent; $recordingCpu = [double]$row.recording_cpu_percent_per_stream; $freeDisk = [double]$row.free_disk_percent } catch { throw 'Resource samples must contain numeric timestamp/CPU/disk fields' }
      if ($null -ne $previous -and $timestamp -lt $previous) { throw 'Resource sample timestamps must be chronological' }
      if ($null -eq $first) { $first = $timestamp }; $previous = $timestamp
      if ($hostCpu -ge 70 -or $recordingCpu -ge 5 -or $freeDisk -le 15) { throw 'Resource contract failed: require host CPU <70%, recording CPU <5%/stream, free disk >15%' }
    }
    if (($previous - $first) -lt 1800) { throw 'Resource samples must span at least 30 minutes' }
    Write-Output 'Resource samples: 30+ minutes of numeric host CPU, recorder CPU/stream, and free disk evidence are within limits'
  } else { Write-Output '30-minute coexistence resource samples: NOT COLLECTED (pass -ResourceSamples)' }
  Write-Output "Acceptance log: $log"
  Write-Output 'Manual playback boundary checks and operating-envelope fields are documented in README.md.'
} finally { Stop-Transcript | Out-Null }
