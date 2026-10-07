package system

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func TestModelCapacityRetryPreservesSession(t *testing.T) {
	c := fixture(t)
	j := &Job{ID: "task", Stage: "running", Role: "executor", BaseSHA: "base", ExecutorSession: "session"}
	c.State.Jobs[j.ID] = j
	for n := 1; n <= 7; n++ {
		if e := c.finish(context.Background(), RunResult{ID: j.ID, Role: "executor", Session: "session", ModelBusy: true, Err: fmt.Errorf("overloaded")}); e != nil {
			t.Fatal(e)
		}
		if n <= 6 {
			if j.Stage != "queued" || j.Question != "" || j.ExecutorSession != "session" || j.BaseSHA != "base" || time.Until(j.RetryAt) < 4*time.Minute {
				t.Fatal("capacity error lost task or asked unnecessarily")
			}
		} else if j.Stage != "waiting" {
			t.Fatal("retries unbounded")
		}
	}
	if modelBusy("filesystem access denied") || !modelBusy("Selected model is at capacity") {
		t.Fatal("capacity classification incorrect")
	}
}
