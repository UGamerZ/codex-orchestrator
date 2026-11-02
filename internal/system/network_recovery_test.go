package system

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"
)

const resetFetch = "trusted Git operation failed: fetch: fatal: Recv failure: Connection was reset"

func TestNetworkFailureRetriesAndExhaustion(t *testing.T) {
	c := fixture(t)
	notifications := 0
	c.API.Client.Transport = roundTrip(func(*http.Request) (*http.Response, error) {
		notifications++
		return response(`{"ok":true,"result":{"message_id":9}}`), nil
	})
	j := &Job{ID: "task", Role: "reviewer", Stage: "running", Head: "current", ReviewerSession: "session"}
	c.State.Jobs[j.ID] = j
	for attempt := 1; attempt <= 7; attempt++ {
		if err := c.finish(context.Background(), RunResult{ID: j.ID, Role: "reviewer", Err: errors.New(resetFetch)}); err != nil {
			t.Fatal(err)
		}
		if attempt <= 6 && (j.Stage != "review" || !j.RetryAt.After(time.Now()) || j.Question != "") {
			t.Fatalf("retry %d not scheduled: %+v", attempt, j)
		}
		if j.Head != "current" || j.ReviewerSession != "session" {
			t.Fatal("lost task state")
		}
	}
	if j.Stage != "waiting" || j.Question == "" || notifications != 1 {
		t.Fatalf("bad exhaustion/notification behavior: stage=%s notifications=%d", j.Stage, notifications)
	}
}

func TestNetworkRecoveryPreservesSafetyBlockers(t *testing.T) {
	c := fixture(t)
	c.API.Client.Transport = roundTrip(func(*http.Request) (*http.Response, error) {
		return response(`{"ok":true,"result":{"message_id":9}}`), nil
	})
	for _, tc := range []struct {
		id, role, origin, diagnostic string
		completed, closed            bool
	}{
		{"recover", "reviewer", "system", resetFetch, false, false},
		{"human", "reviewer", "agent", resetFetch, false, false},
		{"publisher", "publisher", "system", resetFetch, false, false},
		{"done", "reviewer", "system", resetFetch, true, false},
		{"closed", "reviewer", "system", resetFetch, false, true},
		{"auth", "reviewer", "system", "trusted Git operation failed: fetch: Authentication failed", false, false},
	} {
		c.State.Jobs[tc.id] = &Job{ID: tc.id, Role: tc.role, Stage: "waiting", QuestionOrigin: tc.origin, LastError: tc.diagnostic, BoardCompleted: tc.completed, ClosedPR: tc.closed}
	}
	c.recoverNetworkFailures(context.Background())
	for id, j := range c.State.Jobs {
		if (id == "recover") != (j.Stage == "review") {
			t.Fatalf("unexpected recovery %s: %s", id, j.Stage)
		}
	}
}
