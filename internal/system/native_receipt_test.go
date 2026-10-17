package system

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestEnvironmentIsolationCheckTracksInstalledCLI(t *testing.T) {
	root := t.TempDir()
	cli := filepath.Join(root, "codex.exe")
	if err := os.WriteFile(cli, []byte("current CLI"), 0600); err != nil {
		t.Fatal(err)
	}
	p := EnvironmentProfile{IsolationVerified: true, Config: Config{Backend: "native-windows", NativeRoot: root, CodexPath: cli}}
	if !environmentNeedsIsolationCheck(p) {
		t.Fatal("missing receipt must trigger verification")
	}
	writeReceipt := func(version int, binary string) {
		t.Helper()
		sum := sha256.Sum256([]byte(binary))
		b, _ := json.Marshal(map[string]any{"version": version, "codex_sha256": hex.EncodeToString(sum[:])})
		if err := os.WriteFile(filepath.Join(root, "isolation.json"), b, 0600); err != nil {
			t.Fatal(err)
		}
	}
	writeReceipt(4, "current CLI")
	if environmentNeedsIsolationCheck(p) {
		t.Fatal("current verified receipt should be reused")
	}
	p.IsolationVerified = false
	if !environmentNeedsIsolationCheck(p) {
		t.Fatal("unverified profile must run probes even with receipt")
	}
	p.IsolationVerified = true
	writeReceipt(3, "current CLI")
	if !environmentNeedsIsolationCheck(p) {
		t.Fatal("old receipt format must trigger verification")
	}
	writeReceipt(4, "old CLI")
	if !environmentNeedsIsolationCheck(p) {
		t.Fatal("updated CLI must trigger verification")
	}
	writeReceipt(4, "current CLI")
	if err := os.WriteFile(cli, []byte("another update"), 0600); err != nil {
		t.Fatal(err)
	}
	if !environmentNeedsIsolationCheck(p) {
		t.Fatal("binary changes must invalidate receipt")
	}
	p.Config.Backend = "docker"
	if environmentNeedsIsolationCheck(p) {
		t.Fatal("Docker must not run native probes")
	}
}
