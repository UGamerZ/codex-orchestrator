package system

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const completedPullCard = "6acb9e048d588b71a576a657"

func linkedPullFixture(t *testing.T) (*Controller, Pull) {
	c := fixture(t)
	c.C.Board = "board"
	c.C.Todo = "todo"
	c.C.Progress = "progress"
	c.C.Done = "done"
	c.API.C = c.C
	c.State.LastManager = time.Now()
	p := Pull{Number: 16, Title: "Tests", Body: "Trello: https://trello.com/c/" + completedPullCard}
	p.Head.Ref = "codex/" + completedPullCard
	p.Head.SHA = "new-head"
	p.Head.Repo.FullName = c.C.Repository
	c.API.ghTransport = func(_ context.Context, args ...string) ([]byte, error) {
		if !strings.Contains(strings.Join(args, " "), "/pulls?") {
			t.Fatal("unexpected GitHub operation")
		}
		return json.Marshal([][]Pull{{p}})
	}
	c.API.Client.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/getUpdates") {
			return response(`{"ok":true,"result":[]}`), nil
		}
		if r.Method == "GET" && strings.Contains(r.URL.Path, "/cards/") {
			return response(`{"id":"` + completedPullCard + `","idList":"merged","closed":false,"shortLink":"YBpFStrY"}`), nil
		}
		t.Fatalf("completed task caused request %s %s", r.Method, r.URL.Path)
		return nil, nil
	})
	return c, p
}
func TestDiscoveredPullInMergedGetsCompletedBeforeReview(t *testing.T) {
	c, p := linkedPullFixture(t)
	if err := c.discoverPRs(context.Background()); err != nil {
		t.Fatal(err)
	}
	j := c.State.Jobs["pr-16"]
	if j == nil || !j.BoardCompleted || j.Stage != "completed" || j.Card != completedPullCard {
		t.Fatal("untracked completed pull queued for review")
	}
	p.Head.SHA = "later-change"
	if _, skip, err := c.reconcileDiscoveredPull(context.Background(), p, j); !skip || err != nil {
		t.Fatal("later update reopened completed pull", err)
	}
	c.save()
	saved, err := LoadState(c.C.StateDir)
	if err != nil || !saved.Jobs[j.ID].BoardCompleted {
		t.Fatal("completion not retained")
	}
}
func TestLegacyUnlinkedWaitingPullStopsQuestionsAndRetries(t *testing.T) {
	c, p := linkedPullFixture(t)
	j := &Job{ID: "pr-16", PR: 16, External: true, Role: "reviewer", Stage: "waiting", Description: p.Body, Head: p.Head.SHA, Question: "result violates role policy", QuestionMessage: 42}
	c.State.Jobs[j.ID] = j
	if err := c.poll(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !j.BoardCompleted || j.Stage != "completed" || j.Question != "" || j.QuestionMessage != 0 {
		t.Fatal("legacy duplicate still blocked")
	}
	if c.executePanelCommand(PanelCommand{Action: "retry", ID: j.ID}) == nil {
		t.Fatal("completed pull retried")
	}
	if err := c.poll(context.Background()); err != nil {
		t.Fatal(err)
	}
}
func TestCardReferencesUseStableBranchAndSupportShortLinks(t *testing.T) {
	for _, body := range []string{"https://trello.com/c/" + completedPullCard, "https://trello.com/c/" + completedPullCard + "/title", "https://trello.com/c/YBpFStrY?x=1"} {
		if pullCardReference(body, "") == "" {
			t.Fatal("valid reference missed")
		}
	}
	if got := pullCardReference("https://trello.com/c/YBpFStrY", "codex/"+completedPullCard); got != completedPullCard {
		t.Fatal("mutable description replaced stable task reference")
	}
	c, p := linkedPullFixture(t)
	p.Body = "https://trello.com/c/YBpFStrY/title"
	p.Head.Ref = "feature/tests"
	j, skip, err := c.reconcileDiscoveredPull(context.Background(), p, nil)
	if err != nil || !skip || j.Card != completedPullCard {
		t.Fatal("short link not canonicalized", err)
	}
}
func TestForkBranchCannotImplicitlyBindAnUnrelatedCard(t *testing.T) {
	c, p := linkedPullFixture(t)
	p.Body = "No Trello reference"
	p.Head.Repo.FullName = "other/fork"
	j, skip, err := c.reconcileDiscoveredPull(context.Background(), p, nil)
	if err != nil || skip || j.Card != "" || j.Stage != "review" {
		t.Fatal("fork branch bound to unrelated card", err)
	}
}
func TestInactiveEnvironmentOwnerSuppressesDuplicateReview(t *testing.T) {
	c, p := linkedPullFixture(t)
	c.ConfigPath = filepath.Join(t.TempDir(), "config.json")
	old := c.C
	old.StateDir = t.TempDir()
	history := &State{Jobs: map[string]*Job{"original": {ID: "original", Card: completedPullCard, PR: 16, Branch: p.Head.Ref, Stage: "passed", ExecutorSession: "retain"}}}
	history.Save(old.StateDir)
	before, _ := os.ReadFile(filepath.Join(old.StateDir, "state.json"))
	writePrivateJSON(registryPath(c.ConfigPath), EnvironmentRegistry{Active: "active", Profiles: []EnvironmentProfile{{ID: "active", Config: c.C}, {ID: "previous", Config: old}}})
	c.API.Client.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
		if r.Method != "GET" {
			t.Fatal("inactive task caused mutation")
		}
		return response(`{"id":"` + completedPullCard + `","idList":"progress","closed":false}`), nil
	})
	if j, skip, err := c.reconcileDiscoveredPull(context.Background(), p, nil); j != nil || !skip || err != nil {
		t.Fatal("inactive PR duplicated", err)
	}
	duplicate := &Job{ID: "pr-16", External: true, Stage: "waiting", PR: 16, Question: "error"}
	c.State.Jobs[duplicate.ID] = duplicate
	if _, skip, err := c.reconcileDiscoveredPull(context.Background(), p, duplicate); !skip || err != nil || !duplicate.BoardCompleted || duplicate.Question != "" {
		t.Fatal("existing duplicate not stopped", err)
	}
	after, _ := os.ReadFile(filepath.Join(old.StateDir, "state.json"))
	if string(before) != string(after) {
		t.Fatal("previous setup altered")
	}
	old.Repository = "unrelated/repo"
	writePrivateJSON(registryPath(c.ConfigPath), EnvironmentRegistry{Active: "active", Profiles: []EnvironmentProfile{{ID: "active", Config: c.C}, {ID: "previous", Config: old}}})
	owned, _, err := c.inactivePullOwner(p)
	if err != nil || owned {
		t.Fatal("PR number matched another repository")
	}
}
func TestLinkedCardReadFailureHoldsScheduling(t *testing.T) {
	c, _ := linkedPullFixture(t)
	c.API.Client.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/getUpdates") {
			return response(`{"ok":true,"result":[]}`), nil
		}
		if strings.HasSuffix(r.URL.Path, "/sendMessage") {
			return response(`{"ok":true,"result":{"message_id":1}}`), nil
		}
		return nil, fmt.Errorf("offline")
	})
	c.poll(context.Background())
	if c.ChannelReady || len(c.State.Jobs) > 0 {
		t.Fatal("unverified linked PR allowed work")
	}
}
func TestCardMovedDuringReviewDiscardsLatePolicyError(t *testing.T) {
	c, p := linkedPullFixture(t)
	j := &Job{ID: "pr-16", PR: 16, External: true, Description: p.Body, Role: "reviewer", Stage: "running"}
	c.State.Jobs[j.ID] = j
	if err := c.checkResultCard(context.Background(), j); err != nil {
		t.Fatal(err)
	}
	if !j.BoardCompleted {
		t.Fatal("late error did not recheck card")
	}
	if err := c.finish(context.Background(), RunResult{ID: j.ID, Role: "reviewer", Err: fmt.Errorf("result violates role policy")}); err != nil {
		t.Fatal(err)
	}
	if j.Stage != "completed" || j.Question != "" {
		t.Fatal("late error generated blocker")
	}
}
