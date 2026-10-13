package system

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNativeActivityUsesEventTimeForResumedSession(t *testing.T) {
	dir := t.TempDir()
	native := filepath.Join(dir, "native")
	sessions := filepath.Join(native, "codex-runtime", "sessions")
	os.MkdirAll(sessions, 0700)
	cfg := Config{Backend: "native-windows", NativeRoot: native}
	config := filepath.Join(dir, "config.json")
	writePrivateJSON(config, cfg)
	now := time.Now().UTC()
	started := now.Add(-5 * time.Minute)
	eventTime := now.Add(-time.Minute)
	state := &State{Jobs: map[string]*Job{"task": {ID: "task", Stage: "running", Role: "reviewer", ReviewerSession: "resumed", RunStartedAt: started}}}
	state.Save(dir)
	events := []any{map[string]any{"type": "session_meta", "payload": map[string]any{"id": "resumed", "cwd": filepath.Join(native, "jobs", "task", "workspace")}}, map[string]any{"timestamp": eventTime.Format(time.RFC3339Nano), "type": "response_item", "payload": map[string]any{"type": "custom_tool_call_output"}}}
	path := filepath.Join(sessions, "resumed.jsonl")
	f, _ := os.Create(path)
	for _, event := range events {
		json.NewEncoder(f).Encode(event)
	}
	f.Close()
	os.Chtimes(path, now.Add(-time.Hour), now.Add(-time.Hour))
	activity := nativePanelActivity(config, dir)["task"]
	if activity.Session != "resumed" || !activity.LastActivity.Equal(eventTime) || activity.Event != "Tool result received" {
		t.Fatal("live/resumed event time not shown", activity)
	}
}
