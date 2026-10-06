param([string]$Config='config.json')
$ErrorActionPreference = 'Stop'
$projectRoot = Split-Path -Parent $PSScriptRoot
Set-Location -LiteralPath $projectRoot
if (!(Test-Path -LiteralPath $Config)) { & (Join-Path $PSScriptRoot 'Configure-Project.ps1') -Backend docker -Config $Config }
$settings=Get-Content -LiteralPath $Config -Raw | ConvertFrom-Json
if ($settings.backend -ne 'docker') { throw 'Docker configuration required' }
$dockerDistro = $settings.docker_distro
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
$goCommand=Get-Command go -ErrorAction SilentlyContinue
$goPath=if ($goCommand) {$goCommand.Source} else {'C:\Program Files\Go\bin\go.exe'}
& $goPath build -trimpath -o bin/orchestrator.exe ./cmd/orchestrator
if ($LASTEXITCODE -ne 0) { throw 'Controller build failed' }
Write-Host 'Images and networks ready. Configure integrations before Start-Orchestrator.ps1 -Check.'
