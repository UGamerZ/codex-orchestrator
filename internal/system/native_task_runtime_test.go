package system

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func seedTaskRuntime(t *testing.T, c Config) {
	t.Helper()
	for _, relative := range []string{".sandbox/setup_marker.json", ".sandbox-secrets/sandbox_users.json"} {
		path := filepath.Join(c.NativeRoot, "codex-runtime", filepath.FromSlash(relative))
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("{}"), 0600); err != nil {
			t.Fatal(err)
		}
	}
}
func writeTaskSession(t *testing.T, path, workspace, id, output string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	meta, _ := json.Marshal(map[string]any{"type": "session_meta", "payload": map[string]string{"id": id, "cwd": workspace}})
	event, _ := json.Marshal(map[string]any{"type": "response_item", "payload": map[string]string{"type": "function_call_output", "output": output}})
	if err := os.WriteFile(path, append(append(meta, '\n'), append(event, '\n')...), 0600); err != nil {
		t.Fatal(err)
	}
}
func TestNewNativeTasksAndRolesDoNotShareCodexState(t *testing.T) {
	c := nativeFixture(t)
	seedTaskRuntime(t, c)
	seed := filepath.Join(c.NativeRoot, "codex-runtime")
	for _, file := range []string{"state_5.sqlite", "config.toml", "memories/task.txt", "sessions/old.jsonl"} {
		path := filepath.Join(seed, filepath.FromSlash(file))
		os.MkdirAll(filepath.Dir(path), 0700)
		os.WriteFile(path, []byte("must not copy"), 0600)
	}
	for _, id := range []string{"task-a", "task-b"} {
		for _, role := range []string{"executor", "reviewer"} {
			workspace := filepath.Join(c.NativeRoot, "jobs", id, "workspace")
			home := filepath.Join(c.NativeRoot, "jobs", id, "private", role+"-runtime")
			if err := prepareNativeTaskRuntime(c, home, workspace, ""); err != nil {
				t.Fatal(err)
			}
			for _, file := range []string{"state_5.sqlite", "config.toml", "memories", "sessions"} {
				if _, err := os.Stat(filepath.Join(nativeCodexHome(home), file)); !os.IsNotExist(err) {
					t.Fatal("shared state copied", file)
				}
			}
			if err := os.WriteFile(filepath.Join(nativeCodexHome(home), "own-cache"), []byte(id+role), 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
	home := filepath.Join(c.NativeRoot, "jobs", "task-b", "private", "executor-runtime")
	if err := prepareNativeTaskRuntime(c, home, filepath.Join(c.NativeRoot, "jobs", "task-a", "workspace"), ""); err == nil {
		t.Fatal("runtime rebound to another task")
	}
}
func TestOnlyOwnedLegacySessionIsMigrated(t *testing.T) {
	c := nativeFixture(t)
	seedTaskRuntime(t, c)
	workspace := filepath.Join(c.NativeRoot, "jobs", "task-a", "workspace")
	home := filepath.Join(c.NativeRoot, "jobs", "task-a", "private", "executor-runtime")
	sessions := filepath.Join(c.NativeRoot, "codex-runtime", "sessions")
	writeTaskSession(t, filepath.Join(sessions, "rollout-own.jsonl"), workspace, "own", "own")
	writeTaskSession(t, filepath.Join(sessions, "rollout-other.jsonl"), filepath.Join(c.NativeRoot, "jobs", "task-b", "workspace"), "other", "foreign")
	if err := prepareNativeTaskRuntime(c, home, workspace, "own"); err != nil {
		t.Fatal(err)
	}
	if !nativeOwnedSessionExists(nativeCodexHome(home), workspace, "own") {
		t.Fatal("own session lost")
	}
	if _, err := os.Stat(filepath.Join(nativeCodexHome(home), "sessions", "rollout-other.jsonl")); !os.IsNotExist(err) {
		t.Fatal("foreign session copied")
	}
	if err := prepareNativeTaskRuntime(c, home, workspace, "other"); err == nil {
		t.Fatal("foreign session accepted")
	}
	// Continuations keep only their own cache and work.
	if err := prepareNativeTaskRuntime(c, home, workspace, "own"); err != nil {
		t.Fatal(err)
	}
}
func TestTaskScopedLogsPreferCurrentStoreAndExcludeOtherRole(t *testing.T) {
	c := nativeFixture(t)
	dir := t.TempDir()
	config := filepath.Join(dir, "config.json")
	writePrivateJSON(config, c)
	state := &State{Jobs: map[string]*Job{"task": {ID: "task", ExecutorSession: "own", ReviewerSession: "review", Stage: "closed"}}}
	state.Save(dir)
	workspace := filepath.Join(c.NativeRoot, "jobs", "task", "workspace")
	writeTaskSession(t, filepath.Join(c.NativeRoot, "codex-runtime", "sessions", "rollout-own.jsonl"), workspace, "own", "legacy")
	writeTaskSession(t, filepath.Join(nativeSessionRoots(c, "task", "executor")[0], "rollout-own.jsonl"), workspace, "own", "current executor")
	writeTaskSession(t, filepath.Join(nativeSessionRoots(c, "task", "reviewer")[0], "rollout-review.jsonl"), workspace, "review", "other role")
	logs, err := readAgentLogs(config, dir, "task", "executor")
	if err != nil || len(logs.Entries) != 1 || logs.Entries[0].Text != "current executor" {
		t.Fatal(logs, err)
	}
}
func TestDockerTaskVolumesAreDistinct(t *testing.T) {
	r := &Runner{C: nativeFixture(t)}
	seen := map[string]bool{}
	for _, id := range []string{"task-a", "task-b"} {
		for _, kind := range []string{"workspace", "publish", "review", "executor-home", "reviewer-home"} {
			volume := r.volume(id, kind)
			if seen[volume] {
				t.Fatal("shared task volume", volume)
			}
			seen[volume] = true
		}
	}
}
