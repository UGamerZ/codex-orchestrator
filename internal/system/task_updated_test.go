package system

import (
	"testing"
	"time"
)

func TestTaskLastChangeIgnoresPollingAndSurvivesRestart(t *testing.T) {
	c := fixture(t)
	j := &Job{ID: "task", Stage: "queued"}
	c.State.Jobs[j.ID] = j
	if err := c.save(); err != nil {
		t.Fatal(err)
	}
	first := j.UpdatedAt
	if first.IsZero() {
		t.Fatal("new task has no change timestamp")
	}
	j.GitHubFeedbackSeen = map[string]string{"comment:1": "ignored"}
	j.BlockedReminder = time.Now()
	if err := c.save(); err != nil || !j.UpdatedAt.Equal(first) {
		t.Fatal("polling bookkeeping changed task timestamp", err)
	}
	j.Stage = "review"
	if err := c.save(); err != nil || !j.UpdatedAt.After(first) {
		t.Fatal("task transition did not update timestamp", err)
	}
	changed := j.UpdatedAt
	s, err := LoadState(c.C.StateDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Save(c.C.StateDir); err != nil || !s.Jobs[j.ID].UpdatedAt.Equal(changed) {
		t.Fatal("restart reordered unchanged task", err)
	}
}

func TestLegacyTaskLastChangeUsesActivityRatherThanCardID(t *testing.T) {
	c := fixture(t)
	old := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	recent := old.Add(10 * time.Hour)
	j := &Job{ID: "6b05e6266edd7b00160e329a", Stage: "waiting", RunStartedAt: old}
	c.State.Jobs[j.ID] = j
	c.State.Events = []PanelEvent{{At: recent, Kind: "blocked", Task: j.ID}, {At: recent.Add(time.Hour), Kind: "cleanup", Task: j.ID}}
	// Write an old-format state without invoking the new timestamp tracker.
	if err := writePrivateJSON(c.C.StateDir+"/state.json", c.State); err != nil {
		t.Fatal(err)
	}
	s, err := LoadState(c.C.StateDir)
	if err != nil || !s.Jobs[j.ID].UpdatedAt.Equal(recent) {
		t.Fatal("legacy task date did not come from actual activity", err)
	}
}
