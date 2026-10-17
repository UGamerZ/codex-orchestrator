package system

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// Codex's legacy elevated sandbox shares an offline Windows identity. All native
// runs are serialized, then explicit per-directory denies close older SDK grants.
// Only the trusted controller changes these ACLs; sandboxed commands cannot.
func applyNativeTaskScope(ctx context.Context, c Config, workspace, home string) error {
	paths, err := nativeTaskScopePaths(c, workspace, home)
	if err != nil {
		return err
	}
	data, err := json.Marshal(paths)
	if err != nil {
		return err
	}
	script := `
param([string]$DataFile)
$ErrorActionPreference='Stop'
$scopeData=Get-Content -LiteralPath $DataFile -Raw -Encoding UTF8 | ConvertFrom-Json
if(!$scopeData.allow){throw 'Invalid task scope input'}
$taskIdentity=(New-Object Security.Principal.NTAccount('CodexSandboxOffline')).Translate([Security.Principal.SecurityIdentifier])
$taskRights=[Security.AccessControl.FileSystemRights]::FullControl
$scopeRule=New-Object Security.AccessControl.FileSystemAccessRule($taskIdentity,$taskRights,'ContainerInherit,ObjectInherit','None','Deny')
foreach($scopePath in @($scopeData.allow)){
 if(!(Test-Path -LiteralPath $scopePath -PathType Container)){continue}
 $scopeACL=Get-Acl -LiteralPath $scopePath
 $changed=$false
 foreach($entry in @($scopeACL.GetAccessRules($true,$false,[Security.Principal.SecurityIdentifier]))){
  if(!$entry.IsInherited -and $entry.AccessControlType -eq 'Deny' -and $entry.FileSystemRights -eq $taskRights -and $entry.IdentityReference.Value -eq $taskIdentity.Value){
   [void]$scopeACL.RemoveAccessRuleSpecific($entry)
   $changed=$true
  }
 }
 if($changed){Set-Acl -LiteralPath $scopePath -AclObject $scopeACL}
}
foreach($scopePath in @($scopeData.deny)){
 $scopeACL=Get-Acl -LiteralPath $scopePath
 $present=$false
 foreach($entry in @($scopeACL.GetAccessRules($true,$false,[Security.Principal.SecurityIdentifier]))){
  if(!$entry.IsInherited -and $entry.AccessControlType -eq 'Deny' -and $entry.FileSystemRights -eq $taskRights -and $entry.IdentityReference.Value -eq $taskIdentity.Value){$present=$true}
 }
 if(!$present){[void]$scopeACL.AddAccessRule($scopeRule);Set-Acl -LiteralPath $scopePath -AclObject $scopeACL}
}
`
	control := filepath.Join(c.NativeRoot, "control")
	if info, err := os.Lstat(control); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("native control directory is a reparse point")
	}
	if err := os.MkdirAll(control, 0700); err != nil {
		return err
	}
	scriptFile, err := os.CreateTemp(control, "task-scope-*.ps1")
	if err != nil {
		return err
	}
	defer os.Remove(scriptFile.Name())
	if _, err = scriptFile.WriteString(script); err != nil {
		scriptFile.Close()
		return err
	}
	if err = scriptFile.Close(); err != nil {
		return err
	}
	dataFile, err := os.CreateTemp(control, "task-scope-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(dataFile.Name())
	if _, err = dataFile.Write(data); err != nil {
		dataFile.Close()
		return err
	}
	if err = dataFile.Close(); err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-File", scriptFile.Name(), "-DataFile", dataFile.Name())
	cmd.WaitDelay = 5 * time.Second
	if b, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("task filesystem scope failed (%v): %s", err, clip(string(b), 1600))
	}
	return nil
}
