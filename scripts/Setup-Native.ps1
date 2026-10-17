param([string]$Config='config.native.json',[switch]$BuildFromSource)
$ErrorActionPreference='Stop'
$projectRoot=Split-Path -Parent $PSScriptRoot
Set-Location -LiteralPath $projectRoot
$actor=[Security.Principal.WindowsIdentity]::GetCurrent()
if (!(New-Object Security.Principal.WindowsPrincipal($actor)).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) { throw 'Run native setup in an Administrator PowerShell' }
if (!(Test-Path -LiteralPath $Config)) { & (Join-Path $PSScriptRoot 'Configure-Project.ps1') -Backend native-windows -Config $Config }
$settings=Get-Content -LiteralPath $Config -Raw | ConvertFrom-Json
if ($settings.backend -ne 'native-windows') { throw 'Native configuration required' }
$nativeRoot=[IO.Path]::GetFullPath($settings.native_root)
if ($nativeRoot -eq [IO.Path]::GetPathRoot($nativeRoot)) { throw 'Native root must be a dedicated directory' }
$ownerSID=$actor.User.Value
New-Item -ItemType Directory -Path $nativeRoot -Force | Out-Null
if ((Get-Item -LiteralPath $nativeRoot -Force).Attributes -band [IO.FileAttributes]::ReparsePoint) { throw 'Native root must not be a reparse point' }
# Do not grant sandbox users access to the parent, runtime credentials or publisher store.
# Codex grants scoped access only to the current workspace/scratch directories.
$privateACL=New-Object Security.AccessControl.DirectorySecurity
$privateACL.SetAccessRuleProtection($true,$false)
foreach ($sid in @($ownerSID,'S-1-5-18')) {
  $rule=New-Object Security.AccessControl.FileSystemAccessRule((New-Object Security.Principal.SecurityIdentifier($sid)), 'FullControl','ContainerInherit,ObjectInherit','None','Allow')
  $privateACL.AddAccessRule($rule)
}
Set-Acl -LiteralPath $nativeRoot -AclObject $privateACL
$runtimeRoot=Join-Path $nativeRoot 'codex-runtime'
New-Item -ItemType Directory -Path $runtimeRoot -Force | Out-Null
# Reuse the already provisioned Codex sandbox accounts without resetting their passwords.
# No user config, skills, plugins, MCPs or login tokens are copied here.
$existingCodexRoot=Join-Path $env:USERPROFILE '.codex'
foreach ($directory in @('.sandbox','.sandbox-secrets')) {
  $source=Join-Path $existingCodexRoot $directory
  $target=Join-Path $runtimeRoot $directory
  if ((Test-Path -LiteralPath $source) -and !(Test-Path -LiteralPath $target)) { Copy-Item -LiteralPath $source -Destination $target -Recurse }
}
New-Item -ItemType Directory -Path bin -Force | Out-Null
if ($BuildFromSource -or !(Test-Path -LiteralPath 'VERSION' -PathType Leaf) -or !(Test-Path -LiteralPath 'bin/orchestrator.exe' -PathType Leaf)) {
$goCommand=Get-Command go -ErrorAction SilentlyContinue
$goPath=if ($goCommand) { $goCommand.Source } else { 'C:\Program Files\Go\bin\go.exe' }
& $goPath build -trimpath -o bin/orchestrator.exe ./cmd/orchestrator
if ($LASTEXITCODE -ne 0) { throw 'Native controller build failed' }
}
# First-time hosts do not have CodexSandboxOffline yet. Provision only the SDK's
# OS accounts with a fixed identity probe before installing SID-bound WFP rules.
# No model, project task, login token or external command is involved.
$previousCodexHome=$env:CODEX_HOME
try {
 $env:CODEX_HOME=$runtimeRoot
 & $settings.codex_path sandbox --permission-profile orchestrator-setup -c 'default_permissions="orchestrator-setup"' -c 'permissions.orchestrator-setup.filesystem={":root"="read"}' -c 'permissions.orchestrator-setup.network.enabled=false' -c 'windows.sandbox="elevated"' -c 'approval_policy="never"' -C $runtimeRoot (Join-Path $env:SystemRoot 'System32/whoami.exe')
 if($LASTEXITCODE -ne 0){throw 'Codex Windows sandbox provisioning failed. No agents were started.'}
} finally {$env:CODEX_HOME=$previousCodexHome}
& '.\bin\orchestrator.exe' native-policy install
if ($LASTEXITCODE -ne 0) { throw 'Offline sandbox network isolation failed' }
& (Join-Path $PSScriptRoot 'Check-NativeIsolation.ps1') -Config $Config
Write-Host 'Native execution installed and checked. Configure Trello, Telegram and Codex login before starting the service.'
