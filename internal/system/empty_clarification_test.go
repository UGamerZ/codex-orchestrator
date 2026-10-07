package system

import (
	"context"
	"fmt"
	"testing"
)

func TestEmptyQuestionRetriesOnce(t *testing.T) {
	c := fixture(t)
	j := &Job{ID: "task", Role: "reviewer", Stage: "running", ReviewerSession: "review-session"}
	c.State.Jobs[j.ID] = j
	r := RunResult{ID: j.ID, Role: "reviewer", Session: "review-session", EmptyClarification: true, Err: fmt.Errorf("empty clarification")}
	if e := c.finish(context.Background(), r); e != nil {
		t.Fatal(e)
	}
	if j.Stage != "review" || j.Question != "" || j.ReviewerSession != "review-session" {
		t.Fatal("invalid response did not retry review")
	}
	if e := c.finish(context.Background(), r); e != nil {
		t.Fatal(e)
	}
	if j.Stage != "waiting" {
		t.Fatal("invalid response retried indefinitely")
	}
}
