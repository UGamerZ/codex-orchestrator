param([Parameter(Mandatory=$true)][ValidatePattern('^v[0-9]+\.[0-9]+\.[0-9]+(?:-[A-Za-z0-9.-]+)?$')][string]$Version,[string]$Output='dist')
$ErrorActionPreference='Stop'
$root=Split-Path -Parent $PSScriptRoot
Set-Location -LiteralPath $root
$out=[IO.Path]::GetFullPath((Join-Path $root $Output))
if(!$out.StartsWith($root+[IO.Path]::DirectorySeparatorChar,[StringComparison]::OrdinalIgnoreCase)){throw 'Release output must stay within the project.'}
New-Item -ItemType Directory -Path $out -Force | Out-Null
$go=Get-Command go -ErrorAction SilentlyContinue
$goPath=if($go){$go.Source}else{'C:\Program Files\Go\bin\go.exe'}
$dirty=git status --porcelain --untracked-files=no
if($LASTEXITCODE -ne 0 -or $dirty){throw 'Commit tracked release changes before building packages.'}
$commit=(git rev-parse HEAD).Trim()
if($LASTEXITCODE -ne 0){throw 'Cannot determine release commit.'}
$files=@(git ls-files)
if($LASTEXITCODE -ne 0){throw 'A Git checkout is required to select public release files.'}
$files=@($files | Where-Object {$_ -match '^(cmd/|internal/|docker/|scripts/|docs/|go\.mod$|go\.sum$|README\.md$|INSTALL\.md$|PROJECT-SETUP\.md$|NATIVE-INSTALL\.md$|compose\.yaml$|config\.(native\.)?example\.json$|secrets\.example\.json$|Install\.cmd$|Start\.cmd$|install\.sh$|start\.sh$|LICENSE(?:\..*)?$)'})
foreach($required in @('scripts/Install.ps1','scripts/configure-linux.py','INSTALL.md')){if($required -notin $files){throw "Stage the release sources first: missing $required"}}
$originalOS=$env:GOOS;$originalArch=$env:GOARCH;$originalCGO=$env:CGO_ENABLED
$assets=@()
try{
 foreach($target in @(@('windows','amd64'),@('linux','amd64'),@('linux','arm64'),@('darwin','amd64'),@('darwin','arm64'))){
  $os=$target[0];$arch=$target[1]
  $name="codex-orchestrator-$Version-$os-$arch"
  $bundle=Join-Path $out $name
  if(Test-Path -LiteralPath $bundle){throw "Build directory already exists: $bundle. Choose a new output directory."}
  New-Item -ItemType Directory -Path $bundle | Out-Null
  foreach($relative in $files){
   $destination=Join-Path $bundle $relative
   New-Item -ItemType Directory -Path (Split-Path -Parent $destination) -Force | Out-Null
   Copy-Item -LiteralPath (Join-Path $root $relative) -Destination $destination
   if($relative -match '\.(sh|py)$'){
    $text=[IO.File]::ReadAllText($destination).Replace("`r`n","`n")
    [IO.File]::WriteAllText($destination,$text,(New-Object Text.UTF8Encoding($false)))
   }
   if($relative -match '\.cmd$'){
    $text=[IO.File]::ReadAllText($destination).Replace("`r`n","`n").Replace("`n","`r`n")
    [IO.File]::WriteAllText($destination,$text,(New-Object Text.UTF8Encoding($false)))
   }
  }
  New-Item -ItemType Directory -Path (Join-Path $bundle 'bin') -Force | Out-Null
  $env:GOOS=$os;$env:GOARCH=$arch;$env:CGO_ENABLED='0'
  $exe=if($os -eq 'windows'){'orchestrator.exe'}else{'orchestrator'}
  & $goPath build -trimpath -buildvcs=false -ldflags "-s -w -X main.version=$Version" -o (Join-Path $bundle "bin/$exe") ./cmd/orchestrator
  if($LASTEXITCODE -ne 0){throw "Build failed: $os/$arch"}
  [IO.File]::WriteAllText((Join-Path $bundle 'VERSION'),$Version+"`n",(New-Object Text.UTF8Encoding($false)))
  [IO.File]::WriteAllText((Join-Path $bundle 'BUILD-INFO.json'),(@{version=$Version;commit=$commit;os=$os;arch=$arch}|ConvertTo-Json),(New-Object Text.UTF8Encoding($false)))
  if($os -eq 'windows'){
   $archive=Join-Path $out ($name+'.zip')
   Compress-Archive -LiteralPath $bundle -DestinationPath $archive
  }else{
   $archive=Join-Path $out ($name+'.tar.gz')
   & tar.exe -czf $archive -C $out $name
   if($LASTEXITCODE -ne 0){throw 'Archive creation failed'}
  }
  $assets+=$archive
 }
}finally{$env:GOOS=$originalOS;$env:GOARCH=$originalArch;$env:CGO_ENABLED=$originalCGO}
$checks=@($assets | ForEach-Object {(Get-FileHash -LiteralPath $_ -Algorithm SHA256).Hash.ToLowerInvariant()+'  '+(Split-Path -Leaf $_)})
[IO.File]::WriteAllText((Join-Path $out 'SHA256SUMS.txt'),($checks -join "`n")+"`n",(New-Object Text.UTF8Encoding($false)))
Write-Host 'Release packages created without local configs, state, credentials or working project copies.'
$assets | ForEach-Object {Write-Host $_}
