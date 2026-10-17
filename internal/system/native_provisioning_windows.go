package system

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// The legacy CLI uses machine-wide Windows accounts. Independent copies of its
// account records can reset each other's passwords. Share only OS provisioning
// and ACL bookkeeping, while all agent state stays in each task's Codex home.
func prepareNativeProvisioning(c Config, root string) error {
	seed := nativeProvisioningRoot(c)
	for _, name := range []string{".sandbox", ".sandbox-secrets"} {
		source := filepath.Join(seed, name)
		sourceInfo, err := os.Lstat(source)
		if err != nil || !sourceInfo.IsDir() || sourceInfo.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("native machine sandbox provisioning missing or invalid; run native isolation setup")
		}
		target := filepath.Join(root, name)
		if info, err := os.Lstat(target); err == nil {
			// Windows junctions may be reported as ModeIrregular rather than
			// ModeSymlink. Compare target directory identities, not mode bits.
			if resolved, err := os.Stat(target); err == nil && os.SameFile(sourceInfo, resolved) {
				continue
			}
			if !info.IsDir() || info.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0 {
				return fmt.Errorf("invalid task sandbox provisioning directory")
			}
			// Preserve earlier OS diagnostics; never move agent sessions or files.
			backup := fmt.Sprintf("%s.before-shared-%d", target, time.Now().UnixNano())
			if err := os.Rename(target, backup); err != nil {
				return err
			}
		} else if !os.IsNotExist(err) {
			return err
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		cmd := exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command",
			"$ErrorActionPreference='Stop'; New-Item -ItemType Junction -Path "+psQuote(target)+" -Target "+psQuote(source)+" | Out-Null")
		output, err := cmd.CombinedOutput()
		cancel()
		if err != nil {
			return fmt.Errorf("cannot link machine sandbox provisioning: %s", clip(string(output), 1000))
		}
	}
	return nil
}
