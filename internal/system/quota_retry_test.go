package system

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestBoardCheckResumesQuotaDeferredManager(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		t.Run(map[bool]string{false: "new state", true: "existing installation"}[legacy], func(t *testing.T) {
			c := fixture(t)
			c.C.Todo = "todo"
			c.State.LastManager = time.Now().UTC()
			c.State.Pending = []*Job{{ID: "card", Title: "Old task text"}}
			c.State.Jobs["manager"] = &Job{ID: "manager", Role: "manager", Stage: "running"}
			if err := c.finish(context.Background(), RunResult{ID: "manager", Role: "manager", Session: "original", RateLimited: true, Err: io.EOF}); err != nil {
				t.Fatal(err)
			}
			if legacy {
				c.State.Jobs["manager"].RateLimited = false
			}
			if err := c.save(); err != nil {
				t.Fatal(err)
			}
			var err error
			c.State, err = LoadState(c.C.StateDir)
			if err != nil {
				t.Fatal(err)
			}
			cardCalls := 0
			c.API.ghTransport = func(context.Context, ...string) ([]byte, error) { return []byte(`[[]]`), nil }
			c.API.Client.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
				switch {
				case strings.HasSuffix(r.URL.Path, "/getUpdates"):
					return response(`{"ok":true,"result":[]}`), nil
				case strings.HasSuffix(r.URL.Path, "/sendMessage"):
					return response(`{"ok":true,"result":{"message_id":42}}`), nil
				case strings.HasSuffix(r.URL.Path, "/cards"):
					cardCalls++
					return response(`[{"id":"card","name":"Updated task text","idList":"todo"}]`), nil
				default:
					t.Fatalf("unexpected request: %s", r.URL.Path)
					return nil, nil
				}
			})
			// Polling alone respects the existing quota backoff.
			if err := c.poll(context.Background()); err != nil {
				t.Fatal(err)
			}
			if cardCalls != 0 || !c.State.Jobs["manager"].RetryAt.After(time.Now()) {
				t.Fatal("ordinary poll bypassed quota backoff")
			}
			if err := c.executePanelCommand(PanelCommand{Action: "check"}); err != nil {
				t.Fatal(err)
			}
			if err := c.poll(context.Background()); err != nil {
				t.Fatal(err)
			}
			j := c.State.Jobs["manager"]
			if cardCalls != 1 || c.State.BoardCheckRequested || !j.RetryAt.IsZero() || j.Stage != "queued" || j.Session != "original" || len(c.State.Pending) != 1 || c.State.Pending[0].Title != "Updated task text" {
				t.Fatalf("manager did not resume with refreshed snapshot: %+v", j)
			}
			if err := c.finish(context.Background(), RunResult{ID: "manager", Role: "manager", Verdict: Verdict{Status: "completed", Selected: []string{"card"}}}); err != nil {
				t.Fatal(err)
			}
			if c.State.Jobs["card"] == nil || c.State.Jobs["card"].Stage != "queued" || c.State.Pending != nil || j.RateLimited {
				t.Fatal("retained card was not admitted to the executor queue")
			}
		})
	}
}

func TestQuotaBoardRetryPreservesOtherBlockers(t *testing.T) {
	c := fixture(t)
	future := time.Now().Add(time.Hour)
	for _, tc := range []struct {
		id, stage                       string
		quota, closed, completed, retry bool
	}{
		{"executor", "queued", true, false, false, true},
		{"reviewer", "review", true, false, false, true},
		{"active", "running", true, false, false, false},
		{"question", "waiting", true, false, false, false},
		{"network", "queued", false, false, false, false},
		{"closed", "queued", true, true, false, false},
		{"archived", "queued", true, false, true, false},
	} {
		j := &Job{ID: tc.id, Stage: tc.stage, RateLimited: tc.quota, ClosedPR: tc.closed, BoardCompleted: tc.completed, RetryAt: future, Branch: "codex/" + tc.id, ExecutorSession: "executor", ReviewerSession: "reviewer", Fixes: 2}
		c.State.Jobs[j.ID] = j
		c.retryQuotaOnBoardCheck()
		if j.RetryAt.IsZero() != tc.retry || j.ExecutorSession != "executor" || j.ReviewerSession != "reviewer" || j.Fixes != 2 || j.Branch != "codex/"+tc.id {
			t.Fatalf("incorrect recovery for %s: %+v", tc.id, j)
		}
	}
}
