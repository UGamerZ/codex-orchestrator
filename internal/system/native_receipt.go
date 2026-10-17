package system

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// validateNativeIsolationReceipt binds successful probes to the installed CLI.
func validateNativeIsolationReceipt(c Config) error {
	b, err := os.ReadFile(filepath.Join(c.NativeRoot, "isolation.json"))
	if err != nil {
		return fmt.Errorf("native isolation receipt missing; run Check-NativeIsolation.ps1")
	}
	var receipt struct {
		Version int    `json:"version"`
		Codex   string `json:"codex_sha256"`
	}
	if json.Unmarshal(b, &receipt) != nil || receipt.Version != 4 {
		return fmt.Errorf("invalid isolation receipt")
	}
	cli, err := os.ReadFile(c.CodexPath)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(cli)
	if receipt.Codex != hex.EncodeToString(sum[:]) {
		return fmt.Errorf("Codex binary changed; rerun native isolation check")
	}
	return nil
}

func environmentNeedsIsolationCheck(p EnvironmentProfile) bool {
	return p.Config.Backend == "native-windows" && (!p.IsolationVerified || validateNativeIsolationReceipt(p.Config) != nil)
}
