$ErrorActionPreference = 'Stop'
$projectRoot = Split-Path -Parent $PSScriptRoot
Set-Location -LiteralPath $projectRoot
$dockerDistro = 'Ubuntu-24.04'
function Invoke-LinuxDocker { & wsl.exe --distribution $dockerDistro --user root --exec docker @args }
Invoke-LinuxDocker info | Out-Null
if ($LASTEXITCODE -ne 0) { throw 'Linux Docker Engine must be running first' }
$engineType = Invoke-LinuxDocker info --format '{{.OSType}}'
if ($engineType -ne 'linux') { throw 'Linux containers required' }
$linuxProjectRoot = (& wsl.exe --distribution $dockerDistro --user root --exec wslpath -a $projectRoot).Trim()
Invoke-LinuxDocker build -t codex-worker:0.160.0 -f "$linuxProjectRoot/docker/Dockerfile.worker" $linuxProjectRoot
if ($LASTEXITCODE -ne 0) { throw 'Worker image build failed' }
Invoke-LinuxDocker compose --project-directory $linuxProjectRoot -f "$linuxProjectRoot/compose.yaml" up -d --build
if ($LASTEXITCODE -ne 0) { throw 'Proxy setup failed' }
& wsl.exe --distribution $dockerDistro --user root --exec sh "$linuxProjectRoot/scripts/network-policy.sh"
if ($LASTEXITCODE -ne 0) { throw 'Agent network isolation policy failed' }
New-Item -ItemType Directory -Path bin,secrets -Force | Out-Null
go build -trimpath -o bin/orchestrator.exe ./cmd/orchestrator
if ($LASTEXITCODE -ne 0) { throw 'Controller build failed' }
if (!(Test-Path -LiteralPath config.json)) { Copy-Item -LiteralPath config.example.json -Destination config.json }
Write-Host 'Images and networks ready. Configure integrations before Start-Orchestrator.ps1 -Check.'
