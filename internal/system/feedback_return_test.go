package system

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
)

func feedbackBoardFixture(t *testing.T, list string, archived bool, prState string) (*Controller, *Job) {
	t.Helper()
	c := fixture(t)
	c.C.Board = "board"
	c.C.Todo = "todo"
	c.C.Progress = "progress"
	c.C.Done = "done"
	c.API.C = c.C
	j := &Job{ID: "task", Card: "card", PR: 7, Branch: "codex/task", Head: "abc", ReviewedHead: "abc", Stage: "completed", BoardCompleted: true, DoneMoved: true, CompletionMessages: []int64{42}}
	c.State.Jobs[j.ID] = j
	c.API.Client.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
		if r.Method != "GET" || !strings.HasSuffix(r.URL.Path, "/cards/card") {
			t.Fatal("unexpected card mutation", r.Method, r.URL.Path)
		}
		b, _ := json.Marshal(map[string]any{"id": "card", "idList": list, "closed": archived})
		return response(string(b)), nil
	})
	c.API.ghTransport = func(_ context.Context, args ...string) ([]byte, error) {
		if !strings.Contains(strings.Join(args, " "), "/pulls/7") {
			t.Fatal("wrong PR lookup", args)
		}
		return []byte(`{"number":7,"state":"` + prState + `","head":{"sha":"abc"}}`), nil
	}
	return c, j
}

func TestExplicitFeedbackResumesReturnedCardButNotMergedOrArchived(t *testing.T) {
	for _, list := range []string{"todo", "progress", "done", "merged", "archived"} {
		t.Run(list, func(t *testing.T) {
			c, j := feedbackBoardFixture(t, list, list == "archived", "open")
			handled, ack, err := c.completionFeedback(context.Background(), &TGMessage{Text: "Correct the call verification flow", Reply: &TGMessage{ID: 42}})
			if !handled || err != nil {
				t.Fatal(handled, err)
			}
			if len(j.Clarifications) != 1 || j.Answer != "Correct the call verification flow" {
				t.Fatal("feedback not retained")
			}
			if list == "merged" || list == "archived" {
				if !j.BoardCompleted || j.Stage != "completed" || strings.Contains(ack, "executor is queued") {
					t.Fatal("terminal card reopened", ack)
				}
			} else {
				if j.BoardCompleted || j.Stage != "queued" || reviewComplete(j) || j.DoneMoved != (list == "done") {
					t.Fatal("returned card did not resume", ack)
				}
			}
		})
	}
}
func TestFeedbackChecksLiveClosedPRAndRetainsRejectedReview(t *testing.T) {
	c, j := feedbackBoardFixture(t, "progress", false, "closed")
	_, ack, err := c.completionFeedback(context.Background(), &TGMessage{Text: "Bad result", Reply: &TGMessage{ID: 42}})
	if err != nil || !j.ClosedPR || j.Stage != "closed" || len(j.Clarifications) != 1 || !strings.Contains(ack, "no work was restarted") {
		t.Fatal("closed PR restarted", ack, err)
	}
}
func TestFeedbackLookupFailureRetainsReviewAndBlocksWork(t *testing.T) {
	c, j := feedbackBoardFixture(t, "progress", false, "open")
	c.API.ghTransport = func(context.Context, ...string) ([]byte, error) { return nil, errors.New("offline") }
	_, ack, err := c.completionFeedback(context.Background(), &TGMessage{Text: "Bad result", Reply: &TGMessage{ID: 42}})
	if err != nil || j.Stage != "waiting" || len(j.Clarifications) != 1 || !strings.Contains(ack, "not restarted") {
		t.Fatal("unverified PR queued", ack, err)
	}
}
func TestCompletionAcknowledgesBlockedFeedbackOnlyOnceAfterSaving(t *testing.T) {
	c, j := feedbackBoardFixture(t, "progress", false, "open")
	j.ClosedPR = true
	m := &TGMessage{Text: "Bad result", Reply: &TGMessage{ID: 42}}
	m.Chat.ID = c.C.Chat
	m.Chat.Type = "private"
	b, _ := json.Marshal(map[string]any{"ok": true, "result": []Update{{ID: 11, Message: m}}})
	acknowledgments := 0
	c.API.Client.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/getUpdates") {
			return response(string(b)), nil
		}
		if !strings.HasSuffix(r.URL.Path, "/sendMessage") {
			t.Fatal("unexpected request")
		}
		var payload struct {
			Text string `json:"text"`
		}
		json.NewDecoder(r.Body).Decode(&payload)
		if strings.Contains(payload.Text, "executor is queued") || !strings.Contains(payload.Text, "PR is closed") {
			t.Fatal("false success acknowledgment", payload.Text)
		}
		saved, err := LoadState(c.C.StateDir)
		if err != nil || saved.TelegramOffset != 12 || len(saved.Jobs[j.ID].Clarifications) != 1 {
			t.Fatal("acknowledged before saving", err)
		}
		acknowledgments++
		return response(`{"ok":true,"result":{"message_id":99}}`), nil
	})
	if err := c.answers(context.Background()); err != nil {
		t.Fatal(err)
	}
	if acknowledgments != 1 {
		t.Fatal("duplicate acknowledgments", acknowledgments)
	}
}
func TestFeedbackDuringActiveRunDoesNotOverwriteSnapshot(t *testing.T) {
	c, j := feedbackBoardFixture(t, "progress", false, "open")
	j.BoardCompleted = false
	j.Stage = "running"
	j.Role = "reviewer"
	c.API.ghTransport = func(context.Context, ...string) ([]byte, error) {
		t.Fatal("active run should retain feedback for follow-up")
		return nil, nil
	}
	_, ack, err := c.completionFeedback(context.Background(), &TGMessage{Text: "Correct the flow", Reply: &TGMessage{ID: 42}})
	if err != nil || j.Stage != "running" || !j.FeedbackPending || !strings.Contains(ack, "active run") {
		t.Fatal("active snapshot overwritten", ack, err)
	}
}
func TestReturnedFeedbackPreservesLatestOwnedTaskRevision(t *testing.T) {
	c, j := feedbackBoardFixture(t, "progress", false, "open")
	c.C.AutoContinueTasks = true
	j.SystemCreated = true
	j.ExecutorSession = "old-session"
	c.API.ghTransport = func(context.Context, ...string) ([]byte, error) {
		return []byte(`{"number":7,"state":"open","head":{"sha":"updated","ref":"codex/task","repo":{"full_name":"owner/repo"}}}`), nil
	}
	_, _, err := c.completionFeedback(context.Background(), &TGMessage{Text: "Fix this", Reply: &TGMessage{ID: 42}})
	if err != nil || j.Head != "updated" || j.External || j.BoardCompleted || j.Stage != "queued" || j.Answer != "Fix this" {
		t.Fatal("latest branch revision or feedback lost", err)
	}
}

func TestFeedbackRequiresKnownCardList(t *testing.T) {
	c, j := feedbackBoardFixture(t, "", false, "open")
	_, ack, err := c.completionFeedback(context.Background(), &TGMessage{Text: "Fix this", Reply: &TGMessage{ID: 42}})
	if err != nil || !j.BoardCompleted || j.Stage != "waiting" || !strings.Contains(ack, "could not be verified") {
		t.Fatal("unknown card list resumed task", ack, err)
	}
}
func TestInactiveFeedbackChecksOriginatingRepositoryAndTruthfulAck(t *testing.T) {
	c := environmentFixture(t)
	r, _ := readRegistry(c.ConfigPath)
	config := c.C
	config.Repository = "other/project"
	config.GithubUser = "other-account"
	config.StateDir = t.TempDir()
	r.Profiles = append(r.Profiles, EnvironmentProfile{ID: "inactive", Name: "Other", Config: config})
	if err := writePrivateJSON(registryPath(c.ConfigPath), r); err != nil {
		t.Fatal(err)
	}
	history := &State{Jobs: map[string]*Job{"task": {ID: "task", PR: 7, Head: "abc", Stage: "passed", CompletionMessages: []int64{42}}}}
	if err := history.Save(config.StateDir); err != nil {
		t.Fatal(err)
	}
	c.API.ghTransport = func(_ context.Context, args ...string) ([]byte, error) {
		if !strings.Contains(strings.Join(args, " "), "repos/other/project/pulls/7") {
			t.Fatal("feedback checked active repository", args)
		}
		return []byte(`{"number":7,"state":"closed"}`), nil
	}
	count := 0
	c.API.Client.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
		if !strings.HasSuffix(r.URL.Path, "/sendMessage") {
			t.Fatal("unexpected request")
		}
		var payload struct {
			Text string `json:"text"`
		}
		json.NewDecoder(r.Body).Decode(&payload)
		if !strings.Contains(payload.Text, "closed or merged") || strings.Contains(payload.Text, "executor is queued") {
			t.Fatal("false inactive acknowledgment", payload.Text)
		}
		saved, err := LoadState(config.StateDir)
		if err != nil || len(saved.Jobs["task"].Clarifications) != 1 {
			t.Fatal("inactive feedback not saved before ack", err)
		}
		count++
		return response(`{"ok":true,"result":{"message_id":99}}`), nil
	})
	m := &TGMessage{Text: "Bad result", Reply: &TGMessage{ID: 42}}
	m.Chat.ID = c.C.Chat
	if handled, err := c.routeInactiveAnswer(context.Background(), m); !handled || err != nil {
		t.Fatal(handled, err)
	}
	if count != 1 {
		t.Fatal("wrong acknowledgment count", count)
	}
}

func TestOperatorFeedbackUsesSameCheckedRecoveryPath(t *testing.T) {
	c, j := feedbackBoardFixture(t, "progress", false, "open")
	if err := c.executePanelCommand(PanelCommand{Action: "feedback", ID: j.ID, Text: "Acceptance tests fail in GitHub Actions"}); err != nil {
		t.Fatal(err)
	}
	if j.BoardCompleted || j.Stage != "queued" || j.Answer != "Acceptance tests fail in GitHub Actions" {
		t.Fatal("operator feedback not applied")
	}
	if err := c.executePanelCommand(PanelCommand{Action: "feedback", ID: j.ID}); err == nil {
		t.Fatal("empty feedback accepted")
	}
}
