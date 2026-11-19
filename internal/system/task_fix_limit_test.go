package system

import (
	"context"
	"testing"
)

func TestTaskFixLimitOverridePersistsAndContinuesReviews(t *testing.T) {
	c := fixture(t)
	j := &Job{ID: "task", Role: "executor", Stage: "waiting", PR: 7, Head: "abc", Fixes: 7, Question: fixLimitQuestion, QuestionOrigin: "system", ExecutorSession: "original", Findings: []string{"correct the ticket authorization"}}
	c.State.Jobs[j.ID] = j
	if err := c.executePanelCommand(PanelCommand{Action: "disable-fix-limit", ID: j.ID}); err != nil {
		t.Fatal(err)
	}
	if j.Stage != "queued" || j.Question != "" || !j.FixLimitDisabled || len(j.Findings) != 1 || j.ExecutorSession != "original" || j.Fixes != 7 {
		t.Fatalf("limit override lost task context: %+v", j)
	}
	if err := c.save(); err != nil {
		t.Fatal(err)
	}
	state, err := LoadState(c.C.StateDir)
	if err != nil || !state.Jobs[j.ID].FixLimitDisabled {
		t.Fatal("override not retained across restart", err)
	}
	c.State = state
	j = state.Jobs[j.ID]
	fakePull(c.API, "abc")
	for n := 0; n < c.C.MaxFixes+2; n++ {
		j.Stage = "running"
		if err := c.finish(context.Background(), RunResult{ID: j.ID, Role: "reviewer", Verdict: Verdict{Status: "changes", Findings: []string{"concrete defect"}}}); err != nil {
			t.Fatal(err)
		}
		if j.Stage != "queued" || j.Question != "" {
			t.Fatal("unlimited task stopped at global fix limit")
		}
	}
	if c.C.MaxFixes != 3 {
		t.Fatal("per-task override changed the global limit")
	}
	if err := c.executePanelCommand(PanelCommand{Action: "enable-fix-limit", ID: j.ID}); err != nil {
		t.Fatal(err)
	}
	if j.FixLimitDisabled || j.Fixes != 0 {
		t.Fatal("re-enabling did not restore a fresh bounded budget")
	}
}

func TestDisablingFixLimitDoesNotBypassOtherBlockers(t *testing.T) {
	c := fixture(t)
	j := &Job{ID: "resource", Stage: "waiting", Role: "executor", Question: "PHP runtime unavailable", QuestionOrigin: "system"}
	c.State.Jobs[j.ID] = j
	if err := c.executePanelCommand(PanelCommand{Action: "disable-fix-limit", ID: j.ID}); err != nil || j.Stage != "waiting" || j.Question == "" {
		t.Fatal("unrelated technical blocker bypassed", err)
	}
	for _, j := range []*Job{{ID: "closed", ClosedPR: true}, {ID: "archived", BoardCompleted: true}, {ID: "external", External: true}, {ID: "manager"}} {
		c.State.Jobs[j.ID] = j
		if c.executePanelCommand(PanelCommand{Action: "disable-fix-limit", ID: j.ID}) == nil || j.FixLimitDisabled {
			t.Fatalf("invalid task override accepted: %s", j.ID)
		}
	}
}
