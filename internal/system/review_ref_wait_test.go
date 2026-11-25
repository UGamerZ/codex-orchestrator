package system

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestLaggingPullReferenceRetriesWithoutChangingHead(t *testing.T) {
	c := fixture(t)
	j := &Job{ID: "task", SystemCreated: true, PR: 246, Branch: "codex/task", Head: "expected", BaseSHA: "base", Stage: "running", Role: "reviewer", ReviewerSession: "review-session"}
	c.State.Jobs[j.ID] = j
	c.API.ghTransport = func(_ context.Context, args ...string) ([]byte, error) {
		if !strings.Contains(strings.Join(args, " "), "/git/ref/heads/codex%2Ftask") || strings.Contains(strings.Join(args, " "), "--method") {
			t.Fatal("unexpected GitHub operation", args)
		}
		return []byte(`{"object":{"sha":"expected"}}`), nil
	}
	notifications := 0
	c.API.Client.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
		if !strings.HasSuffix(r.URL.Path, "/sendMessage") {
			t.Fatal("unexpected external operation")
		}
		notifications++
		return response(`{"ok":true,"result":{"message_id":42}}`), nil
	})
	for attempt := 0; attempt < 3; attempt++ {
		if err := c.finish(context.Background(), RunResult{ID: j.ID, Role: "reviewer", Err: fmt.Errorf(reviewRefMismatchError)}); err != nil {
			t.Fatal(err)
		}
		if j.Stage != "review" || !j.ReviewRefWaiting || j.Head != "expected" || j.BaseSHA != "base" || j.ReviewerSession != "review-session" || j.Question != "" || time.Until(j.RetryAt) < 50*time.Second {
			t.Fatalf("lagging PR ref altered the task or required a reply: %+v", j)
		}
	}
	if notifications != 1 {
		t.Fatal("synchronization wait notification repeated")
	}
	if err := c.save(); err != nil {
		t.Fatal(err)
	}
	s, err := LoadState(c.C.StateDir)
	if err != nil || !s.Jobs[j.ID].ReviewRefWaiting || s.Jobs[j.ID].Head != "expected" {
		t.Fatal("synchronization wait lost across restart", err)
	}
}

func TestRealBranchChangeStillBlocksPreparation(t *testing.T) {
	c := fixture(t)
	j := &Job{ID: "task", SystemCreated: true, PR: 246, Branch: "codex/task", Head: "expected", Stage: "running", Role: "reviewer"}
	c.State.Jobs[j.ID] = j
	c.API.ghTransport = func(context.Context, ...string) ([]byte, error) {
		return []byte(`{"object":{"sha":"human-edit"}}`), nil
	}
	if err := c.finish(context.Background(), RunResult{ID: j.ID, Role: "reviewer", Err: fmt.Errorf(reviewRefMismatchError)}); err != nil {
		t.Fatal(err)
	}
	if j.Stage != "waiting" || j.Head != "expected" || j.ReviewRefWaiting {
		t.Fatal("real head change was silently accepted")
	}
}
