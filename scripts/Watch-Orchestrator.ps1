param([string]$Config='config.native.json')
$ErrorActionPreference='Stop'
$projectRoot=Split-Path -Parent $PSScriptRoot
Set-Location -LiteralPath $projectRoot
$settings=Get-Content -LiteralPath $Config -Raw -Encoding UTF8 | ConvertFrom-Json
$stateRoot=$settings.state_dir
if (![IO.Path]::IsPathRooted($stateRoot)) { $stateRoot=Join-Path $projectRoot $stateRoot }
$stateRoot=[IO.Path]::GetFullPath($stateRoot)
New-Item -ItemType Directory -Path (Join-Path $stateRoot 'logs') -Force | Out-Null
$installationLock=Join-Path $projectRoot 'environment-controller.lock'
$watchLog=Join-Path $stateRoot 'watchdog.log'
$notificationStamp=Join-Path $stateRoot 'watchdog-notified.txt'
function Notify-Owner([string]$Message) {
 if ((Test-Path -LiteralPath $notificationStamp) -and ((Get-Date)-(Get-Item -LiteralPath $notificationStamp).LastWriteTime).TotalMinutes -lt 60) { return }
 try {
  $secrets=Get-Content -LiteralPath (Join-Path $projectRoot 'secrets/integrations.json') -Raw | ConvertFrom-Json
  $payload=@{chat_id=$settings.telegram_chat;text=$Message} | ConvertTo-Json -Compress
  Invoke-RestMethod -Uri ('https://api.telegram.org/bot'+$secrets.telegram_token+'/sendMessage') -Method Post -ContentType 'application/json; charset=utf-8' -Body ([Text.Encoding]::UTF8.GetBytes($payload)) -TimeoutSec 20 | Out-Null
  [IO.File]::WriteAllText($notificationStamp,(Get-Date).ToString('o'))
 } catch { Add-Content -LiteralPath $watchLog -Value ((Get-Date).ToString('o')+' Telegram notification unavailable') }
}
$lockPath=Join-Path $stateRoot 'controller.lock'
$controller=$null
if (Test-Path -LiteralPath $lockPath) {
 $controllerId=0
 if ([int]::TryParse((Get-Content -LiteralPath $lockPath -Raw).Trim(),[ref]$controllerId)) { $controller=Get-Process -Id $controllerId -ErrorAction SilentlyContinue }
 if ($controller -and $controller.ProcessName -ne 'orchestrator') { Notify-Owner 'Controller lock refers to a different process. Automatic restart stopped; inspect the machine.';return }
}
if ($controller) {
 $statePath=Join-Path $stateRoot 'state.json'
 if ((Test-Path -LiteralPath $statePath) -and ((Get-Date)-(Get-Item -LiteralPath $statePath).LastWriteTime).TotalMinutes -gt 5) { Notify-Owner 'The orchestrator process is running, but its task state has not updated for over five minutes. Check the dashboard and controller connectivity. Active workers were not terminated.' }
 return
}
if (Test-Path -LiteralPath $installationLock) {
 $installationId=0
 if (![int]::TryParse((Get-Content -LiteralPath $installationLock -Raw).Trim(),[ref]$installationId)) { Notify-Owner 'Installation lock is invalid. Inspect it before restarting the controller.';return }
 $installationProcess=Get-Process -Id $installationId -ErrorAction SilentlyContinue
 if ($installationProcess) { Notify-Owner 'An installation controller still holds the environment registry. Automatic restart stopped to avoid duplicate workers.';return }
 Remove-Item -LiteralPath $installationLock
}
Notify-Owner 'The orchestrator controller stopped. The watchdog is restarting it. Agent tasks recover only after worker cleanup; uncertain publications remain paused.'
if (Test-Path -LiteralPath $lockPath) { Remove-Item -LiteralPath $lockPath }
$stamp=Get-Date -Format yyyyMMdd-HHmmss
Start-Process -FilePath powershell.exe -ArgumentList @('-NoProfile','-ExecutionPolicy','Bypass','-File',(Join-Path $PSScriptRoot 'Start-Orchestrator.ps1'),'-Config',$Config) -WorkingDirectory $projectRoot -WindowStyle Hidden -RedirectStandardOutput (Join-Path $stateRoot "logs/watchdog-$stamp.out.log") -RedirectStandardError (Join-Path $stateRoot "logs/watchdog-$stamp.err.log") | Out-Null
Add-Content -LiteralPath $watchLog -Value ((Get-Date).ToString('o')+' Controller restart requested')