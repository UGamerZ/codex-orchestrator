package system

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNativeTaskHomesObserveMachineCredentialRefreshWithoutSharingAgentState(t *testing.T) {
	c := nativeFixture(t)
	seedTaskRuntime(t, c)
	var homes []string
	for _, id := range []string{"task-a", "task-b"} {
		workspace := filepath.Join(c.NativeRoot, "jobs", id, "workspace")
		home := filepath.Join(c.NativeRoot, "jobs", id, "private", "executor-runtime")
		if err := prepareNativeTaskRuntime(c, home, workspace, ""); err != nil {
			t.Fatal(err)
		}
		// Initialization must also accept its own previously established links.
		if err := prepareNativeTaskRuntime(c, home, workspace, ""); err != nil {
			t.Fatal(err)
		}
		homes = append(homes, nativeCodexHome(home))
	}
	refreshed := []byte(`{"version":5,"test":"refreshed machine metadata"}`)
	if err := os.WriteFile(filepath.Join(nativeProvisioningRoot(c), ".sandbox-secrets", "sandbox_users.json"), refreshed, 0600); err != nil {
		t.Fatal(err)
	}
	for _, home := range homes {
		b, err := os.ReadFile(filepath.Join(home, ".sandbox-secrets", "sandbox_users.json"))
		if err != nil || string(b) != string(refreshed) {
			t.Fatal("stale machine credential copy", err)
		}
	}
	if err := os.WriteFile(filepath.Join(homes[0], "state.sqlite"), []byte("task A"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(homes[1], "state.sqlite")); !os.IsNotExist(err) {
		t.Fatal("agent state shared between task homes")
	}
}
