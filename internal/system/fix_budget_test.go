package system

import (
	"context"
	"net/http"
	"testing"
)

const fixLimitQuestion = "The automatic fix limit has been reached. Findings: concrete defect. Clarify the next step; your reply authorizes another run."

func TestFixLimitTelegramReplyRestoresFullCorrectionCycle(t *testing.T) {
	c := fixture(t)
	j := &Job{ID: "task", Role: "executor", Stage: "waiting", PR: 7, Head: "abc", Fixes: 13, Question: fixLimitQuestion, QuestionOrigin: "system", QuestionMessage: 42, QuestionReminderRoot: 42, QuestionReminders: []int64{99}, ExecutorSession: "same-session"}
	c.State.Jobs[j.ID] = j
	c.API.Client.Transport = roundTrip(func(*http.Request) (*http.Response, error) {
		return response(`{"ok":true,"result":[{"update_id":1,"message":{"text":"Address the findings and continue","chat":{"id":123,"type":"private"},"reply_to_message":{"message_id":99}}}]}`), nil
	})
	if err := c.answers(context.Background()); err != nil {
		t.Fatal(err)
	}
	if j.Fixes != 0 || j.Stage != "queued" || j.ExecutorSession != "same-session" || len(j.Clarifications) != 1 {
		t.Fatalf("reply did not renew budget: %+v", j)
	}
	fakePull(c.API, "abc")
	for round := 1; round <= c.C.MaxFixes+1; round++ {
		j.Stage = "running"
		err := c.finish(context.Background(), RunResult{ID: j.ID, Role: "reviewer", Verdict: Verdict{Status: "changes", Findings: []string{"concrete defect"}}})
		if err != nil {
			t.Fatal(err)
		}
		if round <= c.C.MaxFixes && j.Stage != "queued" {
			t.Fatalf("budget exhausted prematurely at %d", round)
		}
	}
	if j.Stage != "waiting" {
		t.Fatal("new budget became unlimited")
	}
}

func TestFixBudgetPanelAndInactiveReplies(t *testing.T) {
	for _, action := range []string{"answer", "retry"} {
		c := fixture(t)
		j := &Job{ID: "task", Role: "executor", Stage: "waiting", Fixes: 13, Question: fixLimitQuestion, QuestionOrigin: "system"}
		c.State.Jobs[j.ID] = j
		if err := c.executePanelCommand(PanelCommand{Action: action, ID: j.ID, Text: "Continue with corrections"}); err != nil {
			t.Fatal(err)
		}
		if j.Fixes != 0 || j.Stage != "queued" {
			t.Fatalf("%s did not reset budget", action)
		}
	}
	c, p := deletionFixture(t)
	s, err := LoadState(p.Config.StateDir)
	if err != nil {
		t.Fatal(err)
	}
	j := s.Jobs["task"]
	j.Question = fixLimitQuestion
	j.QuestionOrigin = "system"
	j.Fixes = 13
	if err = s.Save(p.Config.StateDir); err != nil {
		t.Fatal(err)
	}
	c.API.Client.Transport = roundTrip(func(*http.Request) (*http.Response, error) {
		return response(`{"ok":true,"result":{"message_id":100}}`), nil
	})
	m := &TGMessage{Text: "Continue", Reply: &TGMessage{ID: j.QuestionMessage}}
	m.Chat.ID = c.C.Chat
	if handled, err := c.routeInactiveAnswer(context.Background(), m); err != nil || !handled {
		t.Fatal(handled, err)
	}
	loaded, err := LoadState(p.Config.StateDir)
	if err != nil || loaded.Jobs[j.ID].Fixes != 0 || loaded.Jobs[j.ID].Stage != "queued" {
		t.Fatal("inactive reply did not persist renewed budget", err)
	}
}

func TestOrdinaryClarificationDoesNotResetFixBudget(t *testing.T) {
	j := &Job{Question: "Resource unavailable", Fixes: 2}
	renewFixBudget(j)
	if j.Fixes != 2 {
		t.Fatal("ordinary technical retry resets correction count")
	}
}
