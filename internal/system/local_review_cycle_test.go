package system

import (
	"context"
	"strings"
	"testing"

	"codex-orchestrator/internal/broker"
)

func TestFixCyclesRemainLocalUntilReviewPasses(t *testing.T) {
	c := fixture(t)
	j := &Job{ID: "task", Title: "Process photos", Role: "executor", Stage: "running", BaseSHA: "base", Branch: "codex/task"}
	c.State.Jobs[j.ID] = j
	c.API.ghTransport = func(context.Context, ...string) ([]byte, error) {
		t.Fatal("unreviewed changes caused a GitHub call")
		return nil, nil
	}
	for _, sha := range []string{"draft-one", "draft-two"} {
		if err := c.finish(context.Background(), RunResult{ID: j.ID, Role: "executor", Verdict: Verdict{Status: "completed", CommitTitle: "Process pending photos safely"}}); err != nil || j.Stage != "checkpoint" {
			t.Fatal("executor did not enter local checkpoint stage", err)
		}
		if err := c.finish(context.Background(), RunResult{ID: j.ID, Role: "publisher", Checkpoint: true, Publication: broker.Result{SHA: sha, Changed: true}}); err != nil || j.Stage != "review" || j.Head != "" {
			t.Fatal("checkpoint was treated as remote publication", err)
		}
		if approvedDraftPublication(j) == nil {
			t.Fatal("unreviewed draft can be published")
		}
		if sha == "draft-one" {
			if err := c.finish(context.Background(), RunResult{ID: j.ID, Role: "reviewer", Verdict: Verdict{Status: "changes", Findings: []string{"material defect"}}}); err != nil || j.Stage != "queued" {
				t.Fatal("local findings were not returned to executor", err)
			}
		}
	}
	if err := c.finish(context.Background(), RunResult{ID: j.ID, Role: "reviewer", Verdict: Verdict{Status: "passed", Summary: "Focused tests passed"}}); err != nil || j.Stage != "publish" || approvedDraftPublication(j) != nil {
		t.Fatal("successful review did not authorize exact draft", err)
	}
	posts := 0
	c.API.ghTransport = func(_ context.Context, args ...string) ([]byte, error) {
		call := strings.Join(args, " ")
		if strings.Contains(call, "state=all") {
			return []byte(`[]`), nil
		}
		posts++
		if strings.Contains(call, "/issues/7/comments") {
			if !strings.Contains(call, "Automated review of commit draft-two.") {
				t.Fatal("automated comment would be mistaken for human feedback")
			}
			return []byte(`{}`), nil
		}
		return []byte(`{"number":7,"html_url":"https://github.com/owner/repo/pull/7"}`), nil
	}
	if err := c.finish(context.Background(), RunResult{ID: j.ID, Role: "publisher", Publication: broker.Result{SHA: "draft-two", Changed: true}}); err != nil {
		t.Fatal(err)
	}
	if j.Stage != "passed" || j.Head != "draft-two" || j.ReviewedHead != j.Head || j.DraftSHA != "" || posts != 2 {
		t.Fatal("reviewed publication did not finish exactly once")
	}
}
