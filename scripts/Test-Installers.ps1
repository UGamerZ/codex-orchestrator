$ErrorActionPreference='Stop'
$root=Split-Path -Parent $PSScriptRoot
$fixture=Join-Path $root ('dist/installer-test-'+[Guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path (Join-Path $fixture 'scripts') -Force | Out-Null
Copy-Item -LiteralPath (Join-Path $PSScriptRoot 'Configure-Integrations.ps1') -Destination (Join-Path $fixture 'scripts/Configure-Integrations.ps1')
$config=Join-Path $fixture 'config.json'
Copy-Item -LiteralPath (Join-Path $root 'config.example.json') -Destination $config
$auth=Join-Path $fixture 'mock-auth.json'
'{"tokens":{"access_token":"dummy-access-token","account_id":"dummy-account"}}' | Set-Content -LiteralPath $auth -Encoding UTF8
$global:orchestratorInstallerTestAnswers=New-Object Collections.Queue
foreach($value in @('dummy-trello-key','dummy-trello-token','0','1','1','1','1','dummy-telegram-token','','1')){$global:orchestratorInstallerTestAnswers.Enqueue($value)}
function global:Read-Host {
 param([string]$Prompt,[switch]$AsSecureString)
 if(!$global:orchestratorInstallerTestAnswers.Count){throw "Unexpected prompt: $Prompt"}
 $value=[string]$global:orchestratorInstallerTestAnswers.Dequeue()
 if($AsSecureString){return ConvertTo-SecureString $value -AsPlainText -Force}
 return $value
}
function global:Invoke-RestMethod {
 param([string]$Uri,[int]$TimeoutSec)
 if($Uri -like '*members/me/boards*'){return @([pscustomobject]@{id=('a'*24);name='Test board'})}
 if($Uri -like '*/lists?*'){return @('b','c','d') | ForEach-Object {[pscustomobject]@{id=($_*24);name=('List '+$_)}}}
 if($Uri -like '*/getMe'){return [pscustomobject]@{ok=$true;result=[pscustomobject]@{username='dummy_bot'}}}
 if($Uri -like '*/getUpdates'){return [pscustomobject]@{result=@([pscustomobject]@{message=[pscustomobject]@{chat=[pscustomobject]@{id=12345;type='private';first_name='Test';username='test'}}})}}
 throw 'Unexpected mocked API request'
}
try{
 & (Join-Path $fixture 'scripts/Configure-Integrations.ps1') -Config $config -AuthFile $auth
 $settings=Get-Content -LiteralPath $config -Raw -Encoding UTF8 | ConvertFrom-Json
 $secrets=Get-Content -LiteralPath (Join-Path $fixture 'secrets/integrations.json') -Raw -Encoding UTF8 | ConvertFrom-Json
 if($settings.trello_board -ne ('a'*24) -or $settings.trello_todo -ne ('b'*24) -or $settings.trello_progress -ne ('c'*24) -or $settings.trello_done -ne ('d'*24)){throw 'Selected Trello routes were not persisted correctly.'}
 if($settings.telegram_chat -ne 12345 -or $settings.codex_auth_file -ne $auth){throw 'Telegram or auth-file routing failed.'}
 if($secrets.trello_token -ne 'dummy-trello-token' -or $global:orchestratorInstallerTestAnswers.Count){throw 'Credential prompts or selections failed.'}
 $script:apiFailure=$true
 function global:Invoke-RestMethod {param([string]$Uri,[int]$TimeoutSec);throw "Synthetic network error containing credential: dummy-private-token"}
 $global:orchestratorInstallerTestAnswers.Enqueue('n');$global:orchestratorInstallerTestAnswers.Enqueue('dummy-key');$global:orchestratorInstallerTestAnswers.Enqueue('dummy-private-token')
 $failed=$false
 try{& (Join-Path $fixture 'scripts/Configure-Integrations.ps1') -Config $config -AuthFile $auth}catch{
  $failed=$true
  if($_.Exception.Message.Contains('dummy-private-token')){throw 'Network failure exposed a credential.'}
 }
 if(!$failed){throw 'API failure was not reported.'}
 Write-Host 'Installer tests passed: selection retries, routes, hidden credentials, auth path and sanitized API failures.'
}finally{
 Remove-Item Function:\Read-Host,Function:\Invoke-RestMethod -ErrorAction SilentlyContinue
 Remove-Variable -Name orchestratorInstallerTestAnswers -Scope Global -ErrorAction SilentlyContinue
 Set-Location -LiteralPath $root
}
