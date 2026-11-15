package system

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

func TestUntrackedPullsNeverCreateTasks(t *testing.T) {
	for _, branch := range []string{"swarm", "codex/" + completedPullCard, "feature/build"} {
		c := fixture(t)
		c.C.Board = "board"
		p := Pull{Number: 224, Title: "Optimize build", Body: "Trello: https://trello.com/c/" + completedPullCard}
		p.Head.Ref = branch
		p.Head.SHA = "foreign-head"
		p.Head.Repo.FullName = c.C.Repository
		c.API.ghTransport = func(context.Context, ...string) ([]byte, error) { return json.Marshal([][]Pull{{p}}) }
		c.API.Client.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
			t.Fatalf("untracked PR caused external request: %s", r.URL.Path)
			return nil, nil
		})
		for n := 0; n < 2; n++ {
			if err := c.discoverPRs(context.Background()); err != nil {
				t.Fatal(err)
			}
			if len(c.State.Jobs) != 0 || len(c.State.Pending) != 0 {
				t.Fatalf("foreign PR imported for branch %s", branch)
			}
			p.Head.SHA = "updated-foreign-head"
		}
	}
}

func TestDiscoveryBindsPublishedPullToExistingTask(t *testing.T) {
	c := fixture(t)
	j := &Job{ID: "task", Card: "task", Branch: "codex/task", SystemCreated: true, Stage: "publish"}
	c.State.Jobs[j.ID] = j
	p := Pull{Number: 224, URL: "https://github.com/owner/repo/pull/224"}
	p.Head.Ref = j.Branch
	p.Head.SHA = "published"
	p.Head.Repo.FullName = c.C.Repository
	c.API.ghTransport = func(context.Context, ...string) ([]byte, error) { return json.Marshal([][]Pull{{p}}) }
	if err := c.discoverPRs(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(c.State.Jobs) != 1 || j.PR != 224 || j.Head != "published" || j.Stage != "review" || j.External {
		t.Fatalf("own publication was not reconciled: %+v", j)
	}
}

func TestImportedPullsAreArchivedWithoutRetiringOwnTasks(t *testing.T) {
	c := fixture(t)
	foreign := &Job{ID: "pr-224", PR: 224, External: true, Card: completedPullCard, Stage: "waiting", Question: "Old blocker"}
	own := &Job{ID: "task", Card: "task", Branch: "codex/task", ExecutorSession: "known", External: true, Stage: "waiting"}
	c.State.Jobs[foreign.ID] = foreign
	c.State.Jobs[own.ID] = own
	if err := c.retireImportedPulls(); err != nil {
		t.Fatal(err)
	}
	if len(c.State.Jobs) != 1 || c.State.Jobs[own.ID] != own || own.Stage != "waiting" {
		t.Fatal("unrelated PR retained or own task changed")
	}
	b, err := os.ReadFile(filepath.Join(c.C.StateDir, "ignored-external-prs", "pr-224.json"))
	if err != nil {
		t.Fatal(err)
	}
	var saved Job
	if json.Unmarshal(b, &saved) != nil || saved.PR != 224 || saved.Question != foreign.Question {
		t.Fatal("imported PR history not preserved")
	}
	if err := c.retireImportedPulls(); err != nil {
		t.Fatal(err)
	}
	if len(c.State.Events) != 1 {
		t.Fatal("retirement repeats on every poll")
	}
}
