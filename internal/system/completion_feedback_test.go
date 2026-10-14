package system

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
)

func TestCompletionFeedbackRoutesLegacyAndNewNotifications(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		c := fixture(t)
		c.API.ghTransport = func(context.Context, ...string) ([]byte, error) {
			return []byte(`{"number":7,"state":"open","head":{"sha":"abc"}}`), nil
		}
		j := &Job{ID: "task", Title: "Task", PR: 7, PRURL: "https://github.com/o/r/pull/7", Stage: "passed", Role: "reviewer", Head: "abc", ReviewedHead: "abc", NotifiedHead: "abc", DoneMoved: true, BaseSHA: "base", ExecutorSession: "original", CompletionMessages: []int64{42}}
		c.State.Jobs[j.ID] = j
		reply := &TGMessage{ID: 42}
		if legacy {
			j.CompletionMessages = nil
			reply.Text = "Review complete: no issues found.\nTask\nhttps://github.com/o/r/pull/7\n\nThis is an internal review result."
		}
		m := &TGMessage{Text: "Fix the failing E2E run", Reply: reply}
		m.Chat.ID = 123
		m.Chat.Type = "private"
		data, _ := json.Marshal(map[string]any{"ok": true, "result": []Update{{ID: 11, Message: m}}})
		c.API.Client.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
			if r.URL.Path == "/bot/getUpdates" {
				return response(string(data)), nil
			}
			return response(`{"ok":true,"result":{"message_id":99}}`), nil
		})
		if err := c.answers(context.Background()); err != nil {
			t.Fatal(err)
		}
		if j.Stage != "queued" || j.Role != "executor" || reviewComplete(j) || j.ExecutorSession != "original" || j.BaseSHA != "base" || len(j.Clarifications) != 1 {
			t.Fatalf("feedback not routed: %+v", j)
		}
		saved, err := LoadState(c.C.StateDir)
		if err != nil || saved.TelegramOffset != 12 || saved.Jobs[j.ID].Clarifications[0].Answer != m.Text {
			t.Fatal("feedback not persisted", err)
		}
		// Further replies to the same historical notification remain valid.
		if !completionReply(j, reply) {
			t.Fatal("subsequent reply no longer recognized")
		}
	}
}

func TestFeedbackDoesNotRaceActiveWorkerOrAuthorizeExternalWrites(t *testing.T) {
	c := fixture(t)
	c.API.ghTransport = func(context.Context, ...string) ([]byte, error) {
		return []byte(`{"number":7,"state":"open","head":{"sha":"abc"}}`), nil
	}
	j := &Job{ID: "task", Stage: "running", Role: "reviewer", Head: "abc", ReviewedHead: "abc", FeedbackPending: true}
	c.resumeFeedback(j)
	if j.Stage != "running" || !j.FeedbackPending || reviewComplete(j) {
		t.Fatal("active worker overwritten")
	}
	j.Stage = "passed"
	c.resumeFeedback(j)
	if j.Stage != "queued" || j.FeedbackPending || reviewComplete(j) {
		t.Fatal("feedback lost after worker completion")
	}
	j.Stage = "passed"
	j.External = true
	j.FeedbackPending = true
	c.resumeFeedback(j)
	if j.Stage != "review" || j.Role != "reviewer" || !j.External {
		t.Fatal("external write protection relaxed")
	}
}
