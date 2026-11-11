package system

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestManagerSeesCardIDsWithoutChecklistIDs(t *testing.T) {
	j := &Job{ID: "card-id", Title: "Chat", Description: "Add a chat", BaseSHA: "private-lifecycle-field", Checklists: []TaskChecklist{{ID: "checklist-id", Name: "Requirements", Items: []TaskCheckItem{{ID: "item-id", Name: "Replace the sidebar widget", State: "incomplete"}}}}}
	text := managerCards([]*Job{j, {ID: "archived-card", BoardCompleted: true}})
	var cards []map[string]any
	if err := json.Unmarshal([]byte(text), &cards); err != nil {
		t.Fatal(err)
	}
	if len(cards) != 1 || len(cards[0]) != 3 || cards[0]["id"] != "card-id" || !strings.Contains(text, "Replace the sidebar widget") {
		t.Fatalf("card ID or checklist requirements lost: %s", text)
	}
	for _, unwanted := range []string{"checklist-id", "item-id", "private-lifecycle-field", "archived-card"} {
		if strings.Contains(text, unwanted) {
			t.Fatalf("unselectable ID or internal data exposed: %s", unwanted)
		}
	}
}

func TestInvalidManagerSelectionRetriesWithoutHumanQuestion(t *testing.T) {
	for _, selected := range [][]string{{"checklist-id"}, {"card-id", "card-id"}, {"card-id", "other", "third"}} {
		c := fixture(t)
		p := &Job{ID: "card-id", Stage: "queued", Branch: "codex/card-id"}
		c.State.Pending = []*Job{p}
		j := &Job{ID: "manager", Role: "manager", Stage: "running", Session: "confused-session"}
		c.State.Jobs[j.ID] = j
		r := RunResult{ID: j.ID, Role: "manager", Verdict: Verdict{Status: "completed", Selected: selected}}
		if err := c.finish(context.Background(), r); err != nil {
			t.Fatal(err)
		}
		if j.Stage != "queued" || j.Session != "" || j.Question != "" || j.InvalidResultRetries != 1 || !c.State.BoardCheckRequested || len(c.State.Jobs) != 1 || c.State.Pending[0] != p {
			t.Fatalf("invalid result was not safely retried: %+v", j)
		}
		// A second invalid result cannot loop indefinitely or invent a task.
		if err := c.finish(context.Background(), r); err != nil {
			t.Fatal(err)
		}
		if j.Stage != "waiting" || len(c.State.Jobs) != 1 || len(c.State.Pending) != 1 {
			t.Fatal("repeated invalid selections lost tasks or retried without bound")
		}
	}
}

func TestCorrectManagerSelectionAfterRetryQueuesOnlyRealCard(t *testing.T) {
	c := fixture(t)
	c.State.Pending = []*Job{{ID: "card-id", Stage: "queued"}}
	j := &Job{ID: "manager", Role: "manager", Stage: "waiting", QuestionOrigin: "system", Question: invalidManagerIDs, Session: "old"}
	c.State.Jobs[j.ID] = j
	c.recoverManagerSelectionBlockers()
	if j.Stage != "queued" || j.Session != "" || j.Question != "" {
		t.Fatal("existing blocker not recovered")
	}
	if err := c.finish(context.Background(), RunResult{ID: j.ID, Role: "manager", Verdict: Verdict{Status: "completed", Selected: []string{"card-id"}}}); err != nil {
		t.Fatal(err)
	}
	if j.Stage != "done" || j.InvalidResultRetries != 0 || c.State.Jobs["card-id"] == nil || !c.State.Jobs["card-id"].SystemCreated || c.State.Pending != nil {
		t.Fatal("valid retry did not admit the real card")
	}
}
