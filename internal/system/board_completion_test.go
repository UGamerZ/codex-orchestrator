package system

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestOutsideListCardNeverReopensAfterPRChange(t *testing.T) {
	c := fixture(t)
	c.C.Board = "board"
	c.C.Todo = "todo"
	c.C.Progress = "progress"
	c.C.Done = "done"
	c.API.C = c.C
	c.State.LastManager = time.Now()
	j := &Job{ID: "task", Card: "card", PR: 7, Head: "old", ReviewedHead: "old", Stage: "passed", DoneMoved: true, Role: "reviewer"}
	c.State.Jobs[j.ID] = j
	fakePull(c.API, "new-commit")
	c.API.Client.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/getUpdates") {
			return response(`{"ok":true,"result":[]}`), nil
		}
		if strings.HasSuffix(r.URL.Path, "/cards") {
			return response(`[{"id":"card","idList":"merged","closed":false}]`), nil
		}
		t.Fatalf("completed card caused unexpected request %s %s", r.Method, r.URL.Path)
		return nil, nil
	})
	if err := c.poll(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !j.BoardCompleted || j.Stage != "completed" || j.Head != "old" {
		t.Fatal("completed task reopened", j)
	}
	if err := c.schedule(); err != nil {
		t.Fatal(err)
	}
	if err := c.finish(context.Background(), RunResult{ID: j.ID, Role: "executor", Verdict: Verdict{Status: "completed", Summary: "late result", CommitTitle: "Late change"}}); err != nil {
		t.Fatal(err)
	}
	if j.Stage != "completed" {
		t.Fatal("late executor result queued publication")
	}
	if c.executePanelCommand(PanelCommand{Action: "retry", ID: j.ID}) == nil {
		t.Fatal("dashboard reopened completed task")
	}
	c.save()
	saved, err := LoadState(c.C.StateDir)
	if err != nil || !saved.Jobs[j.ID].BoardCompleted {
		t.Fatal("completion not persistent")
	}
}
func TestBoardCompletionManagedListsAndArchive(t *testing.T) {
	config := Config{Todo: "todo", Progress: "progress", Done: "done"}
	for _, list := range []string{"todo", "progress", "done"} {
		if cardOutsideLists(config, Card{List: list}) {
			t.Fatal("managed list completed", list)
		}
	}
	if !cardOutsideLists(config, Card{List: "merged"}) || !cardOutsideLists(config, Card{List: "done", Closed: true}) {
		t.Fatal("outside/archived card not completed")
	}
}
func TestBoardReadFailurePreventsScheduling(t *testing.T) {
	c := fixture(t)
	c.State.Jobs["task"] = &Job{ID: "task", Card: "card", Stage: "queued"}
	c.C.Board = "board"
	c.API.C = c.C
	c.API.Client.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/getUpdates") {
			return response(`{"ok":true,"result":[]}`), nil
		}
		resp := response(`{}`)
		resp.StatusCode = 500
		return resp, nil
	})
	if c.poll(context.Background()) == nil || c.ChannelReady {
		t.Fatal("unverified board allowed scheduling")
	}
}

func TestOutsideCardBlocksMoveAndPublication(t *testing.T) {
	c := fixture(t)
	c.C.Board = "board"
	c.C.Todo = "todo"
	c.C.Progress = "progress"
	c.C.Done = "done"
	c.API.C = c.C
	c.API.Client.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
		if r.Method != "GET" {
			t.Fatal("outside card was moved")
		}
		return response(`{"id":"card","idList":"merged","closed":false}`), nil
	})
	if err := c.API.Move(context.Background(), "card", "progress"); err != errCardCompleted {
		t.Fatal("outside card moved", err)
	}
	runner := &Runner{C: c.C, API: c.API}
	if err := runner.verifyPublicationCard(context.Background(), &Job{Card: "card"}); err != errCardCompleted {
		t.Fatal("outside card publication allowed", err)
	}
}
