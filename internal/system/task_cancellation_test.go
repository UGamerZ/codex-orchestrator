package system

import (
	"context"
	"errors"
	"net/http"
	"testing"
)

func TestArchivedOrOutsideCardCancelsOnlyItsActiveRun(t *testing.T) {
	for _, card := range []string{
		`{"id":"card","idList":"progress","closed":true}`,
		`{"id":"card","idList":"merged","closed":false}`,
	} {
		for _, role := range []string{"executor", "reviewer", "publisher"} {
			t.Run(card+role, func(t *testing.T) {
				c := fixture(t)
				c.C.Board = "board"
				c.C.Todo = "todo"
				c.C.Progress = "progress"
				c.C.Done = "done"
				c.API.C = c.C
				j := &Job{ID: "task", Card: "card", Stage: "running", Role: role}
				if role == "publisher" {
					j.Stage = "publishing"
				}
				c.State.Jobs[j.ID] = j
				c.API.Client.Transport = roundTrip(func(*http.Request) (*http.Response, error) { return response("[" + card + "]"), nil })
				run := c.taskRunContext(j.ID)
				other := c.taskRunContext("other")
				defer c.releaseTaskRun("other")
				if err := c.syncBoardCompletion(context.Background()); err != nil {
					t.Fatal(err)
				}
				if !errors.Is(run.Err(), context.Canceled) || other.Err() != nil || !j.BoardCompleted {
					t.Fatal("archive did not cancel exactly its own worker")
				}
				if c.workersIdle() {
					t.Fatal("switch allowed before worker cleanup")
				}
				if err := c.finish(context.Background(), RunResult{ID: j.ID, Role: role, Err: context.Canceled, TimedOut: true}); err != nil {
					t.Fatal(err)
				}
				if j.Stage != "completed" || j.Question != "" || j.StallRetries != 0 || c.ActiveRuns[j.ID] != nil {
					t.Fatal("cancelled result reopened task or retained worker")
				}
				if !c.workersIdle() {
					t.Fatal("worker cleanup did not release environment")
				}
			})
		}
	}
}

func TestManagedCardDoesNotCancelActiveRun(t *testing.T) {
	c := fixture(t)
	c.C.Board = "board"
	c.C.Progress = "progress"
	c.API.C = c.C
	c.State.Jobs["task"] = &Job{ID: "task", Card: "card", Stage: "running"}
	c.API.Client.Transport = roundTrip(func(*http.Request) (*http.Response, error) {
		return response(`[{"id":"card","idList":"progress","closed":false}]`), nil
	})
	run := c.taskRunContext("task")
	defer c.releaseTaskRun("task")
	if err := c.syncBoardCompletion(context.Background()); err != nil {
		t.Fatal(err)
	}
	if run.Err() != nil || c.State.Jobs["task"].BoardCompleted {
		t.Fatal("managed task cancelled")
	}
}

func TestCancelledRunnerNeverStartsPreparation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, backend := range []string{"native-windows", "docker"} {
		r := &Runner{C: Config{Backend: backend}}
		result := r.RunContext(ctx, Job{ID: "task"}, "executor", "")
		if !errors.Is(result.Err, context.Canceled) {
			t.Fatalf("%s started cancelled run: %v", backend, result.Err)
		}
	}
}
