package system

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

func TestSQLRejectionReturnsToExecutorWithoutRepublishing(t *testing.T) {
	c := fixture(t)
	j := &Job{ID: "task", Role: "publisher", Stage: "publishing", Branch: "codex/task", BaseSHA: "base", ExecutorSession: "same-session", Answer: "Do not write destructive SQL anywhere", FixLimitDisabled: true, Findings: []string{"Original review finding"}}
	c.State.Jobs[j.ID] = j
	r := RunResult{ID: j.ID, Role: "publisher", Err: fmt.Errorf("data-preservation guard: newly added destructive SQL in tests/unit/TagAdminSaveTest.php is forbidden")}
	for attempt := 1; attempt <= 4; attempt++ {
		if err := c.finish(context.Background(), r); err != nil {
			t.Fatal(err)
		}
		if j.Role != "executor" || j.ExecutorSession != "same-session" || j.BaseSHA != "base" || j.Branch != "codex/task" || j.Answer == "" || j.Findings[0] != "Original review finding" {
			t.Fatal("SQL correction lost the task context")
		}
		if attempt <= 3 {
			if j.Stage != "queued" || j.Question != "" {
				t.Fatal("publisher rejection became a human clarification")
			}
			// Successful executor output must not reset the publication-guard budget.
			if err := c.finish(context.Background(), RunResult{ID: j.ID, Role: "executor", Verdict: Verdict{Status: "completed", CommitTitle: "Correct test database stub"}}); err != nil {
				t.Fatal(err)
			}
			if j.DataGuardRetries != attempt || j.Stage != "checkpoint" {
				t.Fatal("guard correction budget reset before a safe publication")
			}
		} else if j.Stage != "waiting" {
			t.Fatal("unbounded rejected-publication loop")
		}
	}
	if !strings.Contains(strings.Join(j.Findings, "\n"), "TagAdminSaveTest.php") {
		t.Fatal("executor was not told which file failed")
	}
	if err := c.executePanelCommand(PanelCommand{Action: "retry", ID: j.ID}); err != nil || j.Stage != "queued" || j.Role != "executor" || j.DataGuardRetries != 0 {
		t.Fatal("manual retry did not return to correction with a fresh budget", err)
	}
}

func TestLegacySQLPublisherBlockerRecoversOnceAsCorrection(t *testing.T) {
	c := fixture(t)
	j := &Job{ID: "task", Stage: "waiting", Role: "publisher", LastError: "data-preservation guard: blocked", Question: "Same technical blocker remains", DataGuardVersion: 1}
	c.State.Jobs[j.ID] = j
	c.recoverDataGuardPublications()
	if j.Stage != "queued" || j.Role != "executor" || j.Question != "" || j.DataGuardRetries != 1 {
		t.Fatal("legacy blocker was republished instead of corrected")
	}
	c.recoverDataGuardPublications()
	if j.DataGuardRetries != 1 {
		t.Fatal("startup recovery repeated")
	}
}
