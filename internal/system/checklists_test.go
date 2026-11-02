package system

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestChecklistOnlyTaskReachesManagerExecutorAndReviewer(t *testing.T) {
	c := fixture(t)
	c.C.Board = "board"
	c.C.Todo = "todo"
	c.C.Progress = "progress"
	c.C.Done = "done"
	c.API.C = c.C
	c.State.LastManager = time.Now()
	c.State.BoardCheckRequested = true
	c.API.ghTransport = func(context.Context, ...string) ([]byte, error) { return []byte(`[]`), nil }
	checklistBody := `[{"id":"second","name":"Second","pos":20,"checkItems":[]},{"id":"first","name":"Requirements","pos":10,"checkItems":[{"id":"b","name":"Check through proxy https://example.org/reference.png","state":"complete","pos":20},{"id":"a","name":"Detect response code zero","state":"incomplete","pos":10}]}]`
	failChecklists := false
	c.API.Client.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/getUpdates"):
			return response(`{"ok":true,"result":[]}`), nil
		case strings.HasSuffix(r.URL.Path, "/sendMessage"):
			return response(`{"ok":true,"result":{"message_id":9}}`), nil
		case r.URL.Path == "/1/boards/board/cards":
			return response(`[{"id":"card","name":"Site checks","desc":"","idList":"todo","closed":false}]`), nil
		case r.URL.Path == "/1/cards/card/checklists":
			if r.URL.Query().Get("checkItems") != "all" || r.URL.Query().Get("checkItem_fields") != "name,state,pos" {
				t.Fatal("missing checklist item fields")
			}
			if failChecklists {
				resp := response(`{}`)
				resp.StatusCode = 503
				return resp, nil
			}
			return response(checklistBody), nil
		default:
			t.Fatalf("unexpected request %s", r.URL.Path)
			return nil, nil
		}
	})
	if err := c.poll(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(c.State.Pending) != 1 {
		t.Fatal("checklist-only task not discovered")
	}
	j := c.State.Pending[0]
	if j.Description != "" || len(j.Checklists) != 2 || j.Checklists[0].Name != "Requirements" || j.Checklists[0].Items[0].State != "incomplete" {
		t.Fatalf("checklist contents/order lost: %+v", j)
	}
	managerPrompt := c.prompt(Job{ID: "manager"}, "manager")
	for _, role := range []string{"executor", "reviewer"} {
		prompt := c.prompt(*j, role)
		for _, text := range []string{"- [ ] Detect response code zero", "- [x] Check through proxy", "https://example.org/reference.png"} {
			if !strings.Contains(prompt, text) {
				t.Fatalf("%s missing %s", role, text)
			}
		}
	}
	if !strings.Contains(managerPrompt, "Detect response code zero") {
		t.Fatal("manager did not receive checklist")
	}
	if !strings.Contains(taskDescription(j), "Detect response code zero") {
		t.Fatal("trusted review description missing checklist")
	}
	// Existing tasks refresh requirements on the next execution/review turn.
	c.State.Jobs[j.ID] = j
	c.State.Pending = nil
	checklistBody = `[{"name":"Updated","pos":1,"checkItems":[{"name":"New requirement","state":"incomplete","pos":1}]}]`
	if err := c.poll(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(j.Checklists) != 1 || j.Checklists[0].Items[0].Name != "New requirement" {
		t.Fatal("requirements not refreshed")
	}
	if err := c.save(); err != nil {
		t.Fatal(err)
	}
	saved, err := LoadState(c.C.StateDir)
	if err != nil || len(saved.Jobs[j.ID].Checklists) != 1 {
		t.Fatal("checklists not persisted", err)
	}
	failChecklists = true
	if err := c.poll(context.Background()); err == nil || c.ChannelReady {
		t.Fatal("missing requirements allowed scheduling")
	}
	// Archived tasks must not fetch requirements or resume.
	j.BoardCompleted = true
	j.Stage = "completed"
	if err := c.poll(context.Background()); err != nil {
		t.Fatal("completed task fetched checklists", err)
	}
	if j.Stage != "completed" {
		t.Fatal("archived task restarted")
	}
}
