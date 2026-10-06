param([string]$Config='config.native.json')
$ErrorActionPreference='Stop'
$projectRoot=Split-Path -Parent $PSScriptRoot
Set-Location -LiteralPath $projectRoot
& '.\bin\orchestrator.exe' native-check -config $Config
if ($LASTEXITCODE -ne 0) { throw 'Native sandbox verification failed. Service remains disabled; no full-access fallback is permitted.' }
$settings=Get-Content -LiteralPath $Config -Raw | ConvertFrom-Json
$settings.native_isolation_verified=$true
[IO.File]::WriteAllText([IO.Path]::GetFullPath($Config),($settings | ConvertTo-Json -Depth 12),(New-Object Text.UTF8Encoding($false)))
Write-Host 'Executor write, reviewer read-only, outside read denial and offline sandbox identity checked.'
