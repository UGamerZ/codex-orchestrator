param([string]$Config='config.native.json',[string]$AuthFile)
$ErrorActionPreference='Stop'
$root=Split-Path -Parent $PSScriptRoot
Set-Location -LiteralPath $root
$settings=Get-Content -LiteralPath $Config -Raw -Encoding UTF8 | ConvertFrom-Json
$secretsPath=Join-Path $root 'secrets/integrations.json'
function Read-PrivateValue([string]$Prompt){
 $secure=Read-Host $Prompt -AsSecureString
 $value=(New-Object Net.NetworkCredential('',$secure)).Password
 if([string]::IsNullOrWhiteSpace($value)){throw "$Prompt is required."}
 return $value.Trim()
}
function Select-SetupItem([object[]]$Items,[string]$Prompt){
 if(!$Items.Count){throw "No items available: $Prompt"}
 for($i=0;$i -lt $Items.Count;$i++){Write-Host ("{0}. {1}" -f ($i+1),$Items[$i].name)}
 while($true){
  $selection=0
  if([int]::TryParse((Read-Host $Prompt),[ref]$selection) -and $selection -ge 1 -and $selection -le $Items.Count){return $Items[$selection-1]}
  Write-Host "Choose a number from 1 to $($Items.Count)."
 }
}
function Invoke-SetupApi([string]$Uri){
 try{return Invoke-RestMethod -Uri $Uri -TimeoutSec 30}
 catch{throw 'Integration request failed. Check the credentials, internet access and selected account. Credentials have not been printed.'}
}
if(Test-Path -LiteralPath $secretsPath){
 if((Read-Host 'Integration credentials already exist. Keep them? [Y/n]') -ne 'n'){
  Write-Host 'Keeping existing credentials and Trello/Telegram routing.'
  return
 }
}
Write-Host 'Create a Trello API key and read/write token: https://trello.com/power-ups/admin'
$key=Read-PrivateValue 'Trello API key (hidden)'
$token=Read-PrivateValue 'Trello API token (hidden)'
$query='key='+[Uri]::EscapeDataString($key)+'&token='+[Uri]::EscapeDataString($token)
$boards=@(Invoke-SetupApi "https://api.trello.com/1/members/me/boards?filter=open&fields=name&$query")
$board=Select-SetupItem $boards 'Select the board number'
$lists=@(Invoke-SetupApi "https://api.trello.com/1/boards/$($board.id)/lists?filter=open&fields=name&$query")
if($lists.Count -lt 3){throw 'Create at least three lists on the selected board, then rerun the installer.'}
$todo=Select-SetupItem $lists 'Select To Do'
$progress=Select-SetupItem @($lists | Where-Object {$_.id -ne $todo.id}) 'Select In Progress'
$done=Select-SetupItem @($lists | Where-Object {$_.id -notin @($todo.id,$progress.id)}) 'Select Done'
$telegram=Read-PrivateValue 'Telegram bot token from @BotFather (hidden)'
$bot=Invoke-SetupApi "https://api.telegram.org/bot$telegram/getMe"
if(!$bot.ok){throw 'Telegram did not accept the bot token.'}
Write-Host "Open https://t.me/$($bot.result.username) and send /start. Do not run another poller for this bot."
[void](Read-Host 'Press Enter after sending /start')
$updates=Invoke-SetupApi "https://api.telegram.org/bot$telegram/getUpdates"
$chats=@($updates.result | ForEach-Object {$_.message.chat} | Where-Object {$_.type -eq 'private'} | Sort-Object id -Unique | ForEach-Object {[pscustomobject]@{id=$_.id;name=($_.first_name+' '+$_.username).Trim()}})
$chatId=0L
if($chats.Count){$chat=Select-SetupItem $chats 'Select your private Telegram chat';$chatId=[long]$chat.id}
else{if(![long]::TryParse((Read-Host 'No private chat found. Enter your Telegram chat ID'),[ref]$chatId) -or !$chatId){throw 'A valid Telegram chat ID is required.'}}
if(!$AuthFile){
 $codexRoot=if($env:CODEX_HOME){$env:CODEX_HOME}else{Join-Path $env:USERPROFILE '.codex'}
 $defaultAuth=Join-Path $codexRoot 'auth.json'
 $answer=Read-Host "Codex ChatGPT login file [$defaultAuth]"
 $AuthFile=if($answer){$answer}else{$defaultAuth}
}
if(!(Test-Path -LiteralPath $AuthFile -PathType Leaf)){throw 'Codex auth.json was not found. Sign in to Codex CLI with ChatGPT first.'}
$auth=Get-Content -LiteralPath $AuthFile -Raw | ConvertFrom-Json
if($auth.OPENAI_API_KEY -or !$auth.tokens.access_token -or !$auth.tokens.account_id){throw 'A Codex ChatGPT login is required; API-key authentication is not supported.'}
$settings.codex_auth_file=[IO.Path]::GetFullPath($AuthFile)
$settings.trello_board=$board.id;$settings.trello_todo=$todo.id;$settings.trello_progress=$progress.id;$settings.trello_done=$done.id;$settings.telegram_chat=$chatId
$secretDir=Split-Path -Parent $secretsPath
New-Item -ItemType Directory -Path $secretDir -Force | Out-Null
if((Get-Item -LiteralPath $secretDir -Force).Attributes -band [IO.FileAttributes]::ReparsePoint){throw 'Secrets directory must not be a reparse point.'}
$acl=New-Object Security.AccessControl.DirectorySecurity
$acl.SetAccessRuleProtection($true,$false)
foreach($sid in @([Security.Principal.WindowsIdentity]::GetCurrent().User.Value,'S-1-5-18')){
 $rule=New-Object Security.AccessControl.FileSystemAccessRule((New-Object Security.Principal.SecurityIdentifier($sid)),'FullControl','ContainerInherit,ObjectInherit','None','Allow')
 [void]$acl.AddAccessRule($rule)
}
Set-Acl -LiteralPath $secretDir -AclObject $acl
if(Test-Path -LiteralPath $secretsPath){
 if((Get-Item -LiteralPath $secretsPath -Force).Attributes -band [IO.FileAttributes]::ReparsePoint){throw 'Secrets file must not be a reparse point.'}
}
$utf8=New-Object Text.UTF8Encoding($false)
[IO.File]::WriteAllText($secretsPath,(@{trello_key=$key;trello_token=$token;telegram_token=$telegram}|ConvertTo-Json),$utf8)
[IO.File]::WriteAllText([IO.Path]::GetFullPath($Config),($settings|ConvertTo-Json -Depth 12),$utf8)
Write-Host 'Integrations saved privately. No agents have been started.'
