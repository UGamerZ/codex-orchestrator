package system

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func TestTimedOutAgentRetriesBoundedlyWithSession(t *testing.T) {
	c := fixture(t)
	j := &Job{ID: "task", Stage: "running", Role: "executor", BaseSHA: "base", ExecutorSession: "same"}
	c.State.Jobs[j.ID] = j
	for attempt := 1; attempt <= 4; attempt++ {
		err := c.finish(context.Background(), RunResult{ID: j.ID, Role: "executor", TimedOut: true, Err: fmt.Errorf("timeout"), Session: "same"})
		if err != nil {
			t.Fatal(err)
		}
		if j.ExecutorSession != "same" || j.BaseSHA != "base" {
			t.Fatal("workspace/session reset")
		}
		if attempt <= 3 && (j.Stage != "queued" || !j.RetryAt.After(time.Now())) {
			t.Fatal("retry not scheduled")
		}
	}
	if j.Stage != "waiting" || j.Question == "" {
		t.Fatal("unbounded retries")
	}
}

func TestInterruptedRecoverySkipsPermissionsAndPublishers(t *testing.T) {
	c := fixture(t)
	for _, role := range []string{"executor", "reviewer", "manager", "publisher"} {
		c.State.Jobs[role] = &Job{ID: role, Role: role, Stage: "waiting", Question: interruptedQuestion}
	}
	c.State.Jobs["permission"] = &Job{Role: "executor", Stage: "waiting", Question: "May I modify workflows?"}
	c.recoverInterruptedAgents()
	if c.State.Jobs["executor"].Stage != "queued" || c.State.Jobs["reviewer"].Stage != "review" {
		t.Fatal("agents not recovered")
	}
	if c.State.Jobs["publisher"].Stage != "waiting" || c.State.Jobs["permission"].Question != "May I modify workflows?" {
		t.Fatal("unsafe blocker recovery")
	}
}
