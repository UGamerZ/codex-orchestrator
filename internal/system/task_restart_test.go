package system

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRestartCreatesIndependentCleanTask(t *testing.T) {
	c := fixture(t)
	c.C.Backend, c.C.NativeRoot = "native-windows", t.TempDir()
	c.Runner = &Runner{C: c.C, API: c.API}
	old := &Job{ID: "old", Card: "card", Title: "Process photos", Description: "Requirements", Branch: "codex/old", Head: "old-head", BaseSHA: "old-base", PR: 252, ClosedPR: true, Stage: "closed", ExecutorSession: "old-executor", ReviewerSession: "old-reviewer", Fixes: 9, DraftSHA: "old-draft", Findings: []string{"Old finding"}, Clarifications: []Clarification{{Origin: "agent", Question: "Which format?", Answer: "Keep JPEG support"}, {Origin: "system", Question: fixLimitQuestion, Answer: "Continue"}}}
	c.State.Jobs[old.ID] = old
	workspace := filepath.Join(c.C.NativeRoot, "jobs", old.ID, "workspace")
	os.MkdirAll(workspace, 0700)
	os.WriteFile(filepath.Join(workspace, "old.txt"), []byte("discarded implementation"), 0600)
	if err := c.restartCompletedTask(old.ID); err != nil {
		t.Fatal(err)
	}
	if len(c.State.Jobs) != 1 || c.State.Jobs[old.ID] != nil {
		t.Fatal("old run remained in the active task list")
	}
	for _, next := range c.State.Jobs {
		if next.Card != old.Card || next.ID == old.ID || next.Branch == old.Branch || next.BaseSHA != "" || next.Head != "" || next.PR != 0 || next.ExecutorSession != "" || next.ReviewerSession != "" || next.Fixes != 0 || next.DraftSHA != "" || len(next.Findings) != 0 || next.Stage != "queued" || !next.SystemCreated {
			t.Fatalf("new run inherited implementation or agent context: %+v", next)
		}
		if len(next.Clarifications) != 1 || next.Clarifications[0].Answer != "Keep JPEG support" || next.Description != old.Description {
			t.Fatal("explicit task requirements were lost")
		}
		if _, err := os.Stat(filepath.Join(c.C.NativeRoot, "jobs", next.ID)); !os.IsNotExist(err) {
			t.Fatal("fresh task reused an existing runtime")
		}
	}
	if _, err := os.Stat(filepath.Join(c.C.StateDir, "deleted-tasks", old.ID+".json")); err != nil {
		t.Fatal("old run history was not retained")
	}
}
