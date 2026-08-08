$ErrorActionPreference = 'Stop'
$script = (Resolve-Path (Join-Path $PSScriptRoot '..\verify-single-host-recording.ps1')).Path

Describe 'single-host recording verifier command contract' {
  It 'rejects acceptance when mandatory target-host evidence is omitted' {
    { & $script -SampleSeconds 300 } | Should -Throw '*requires*evidence*'
  }

  It 'rejects sqlite command failures rather than treating them as no gaps' {
    $source = Get-Content -LiteralPath $script -Raw
    $source | Should -Match '\$LASTEXITCODE.*SQLite continuity query failed'
  }
}
