package system

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestBlockedReminderToggleIsImmediateAndPersistent(t *testing.T) {
	c := fixture(t)
	c.ConfigPath = filepath.Join(t.TempDir(), "config.json")
	c.Runner = &Runner{C: c.C, API: c.API}
	c.State.Jobs["active"] = &Job{ID: "active", Stage: "running"}
	c.State.PendingConfig = map[string]json.RawMessage{"worker_model": json.RawMessage(`"next-model"`)}
	sends := 0
	c.API.Client.Transport = roundTrip(func(*http.Request) (*http.Response, error) {
		sends++
		return response(`{"ok":true,"result":{"message_id":99}}`), nil
	})
	j := &Job{ID: "blocked", QuestionMessage: 42, BlockedReminder: time.Now().Add(-2 * time.Hour)}
	before := j.BlockedReminder
	if !blockedRemindersEnabled(c.C) {
		t.Fatal("legacy default changed")
	}
	if err := c.executePanelCommand(PanelCommand{Action: "blocked-reminders", Text: "false"}); err != nil {
		t.Fatal(err)
	}
	if blockedRemindersEnabled(c.C) || blockedRemindersEnabled(c.Runner.C) || string(c.State.PendingConfig["blocked_reminders_enabled"]) != "false" {
		t.Fatal("toggle waits for workers or can be undone by pending config")
	}
	if err := c.sendBlockedReminder(context.Background(), j); err != nil || sends != 0 || !j.BlockedReminder.Equal(before) || j.QuestionMessage != 42 {
		t.Fatal("disabled reminder sent or task/reply routing changed", err)
	}
	b, err := os.ReadFile(c.ConfigPath)
	var saved Config
	if err != nil || json.Unmarshal(b, &saved) != nil || blockedRemindersEnabled(saved) {
		t.Fatal("disabled choice not persisted", err)
	}
	if err := c.executePanelCommand(PanelCommand{Action: "blocked-reminders", Text: "true"}); err != nil {
		t.Fatal(err)
	}
	if err := c.sendBlockedReminder(context.Background(), j); err != nil || sends != 1 {
		t.Fatal("re-enabled reminders did not resume", err)
	}
}
