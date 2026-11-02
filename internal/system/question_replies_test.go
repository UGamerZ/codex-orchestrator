package system

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestReminderReplySurvivesRestartAndRoutesToWaitingRole(t *testing.T) {
	for _, role := range []string{"executor", "reviewer", "publisher"} {
		t.Run(role, func(t *testing.T) {
			c := fixture(t)
			j := &Job{ID: "task", Title: "Task", Role: role, Stage: "waiting", Question: "Resource ready?", QuestionOrigin: "system", QuestionMessage: 42, ExecutorSession: "executor-session", ReviewerSession: "review-session"}
			c.State.Jobs[j.ID] = j
			c.API.Client.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
				if strings.HasSuffix(r.URL.Path, "/sendMessage") {
					return response(`{"ok":true,"result":{"message_id":99}}`), nil
				}
				return response(`{"ok":true,"result":[{"update_id":1,"message":{"text":"Resource is ready","chat":{"id":123,"type":"private"},"reply_to_message":{"message_id":99}}}]}`), nil
			})
			if err := c.sendBlockedReminder(context.Background(), j); err != nil {
				t.Fatal(err)
			}
			if j.BlockedReminder.IsZero() || j.QuestionReminderRoot != 42 {
				t.Fatal("reminder not recorded")
			}
			if err := c.save(); err != nil {
				t.Fatal(err)
			}
			s, err := LoadState(c.C.StateDir)
			if err != nil {
				t.Fatal(err)
			}
			c.State = s
			j = s.Jobs[j.ID]
			if err = c.answers(context.Background()); err != nil {
				t.Fatal(err)
			}
			want := map[string]string{"executor": "queued", "reviewer": "review", "publisher": "publish"}[role]
			if j.Stage != want || j.Answer != "Resource is ready" || len(j.Clarifications) != 1 || j.Clarifications[0].Question != "Resource ready?" || j.ExecutorSession != "executor-session" || j.ReviewerSession != "review-session" {
				t.Fatalf("wrong routing: %+v", j)
			}
			if questionReply(j, &TGMessage{ID: 99}) {
				t.Fatal("answered reminder remained active")
			}
		})
	}
}

func TestLegacyReminderMatchesOnlyCurrentQuestionFromBot(t *testing.T) {
	j := &Job{ID: "task", Title: "Task", Role: "executor", Question: "Which option?", QuestionMessage: 42}
	reply := &TGMessage{ID: 99, Text: blockedReminderText(j, legacyReminderFooter)}
	if questionReply(j, reply) {
		t.Fatal("forged reminder accepted")
	}
	if err := json.Unmarshal([]byte(`{"from":{"is_bot":true}}`), reply); err != nil {
		t.Fatal(err)
	}
	if !questionReply(j, reply) {
		t.Fatal("previously sent reminder ignored")
	}
	j.Question = "Different question?"
	if questionReply(j, reply) {
		t.Fatal("old question answered new blocker")
	}
	j.QuestionReminders = []int64{99}
	j.QuestionReminderRoot = 41
	if questionReply(j, reply) {
		t.Fatal("old question ID alias accepted")
	}
	j.Question = strings.Repeat("x", 4000)
	reply.Text = clip(blockedReminderText(j, legacyReminderFooter), 3800)
	if questionReply(j, reply) {
		t.Fatal("ambiguous truncated legacy question accepted")
	}
}

func TestReminderReplyForInactiveOrDeletedEnvironment(t *testing.T) {
	for _, deleted := range []bool{false, true} {
		t.Run(map[bool]string{false: "inactive", true: "deleted"}[deleted], func(t *testing.T) {
			c, p := deletionFixture(t)
			s, err := LoadState(p.Config.StateDir)
			if err != nil {
				t.Fatal(err)
			}
			j := s.Jobs["task"]
			j.QuestionReminderRoot = j.QuestionMessage
			j.QuestionReminders = []int64{100}
			if err = s.Save(p.Config.StateDir); err != nil {
				t.Fatal(err)
			}
			if deleted {
				if err = c.deleteEnvironment(p.ID); err != nil {
					t.Fatal(err)
				}
			}
			c.API.Client.Transport = roundTrip(func(*http.Request) (*http.Response, error) {
				return response(`{"ok":true,"result":{"message_id":999}}`), nil
			})
			m := &TGMessage{Text: "Resource ready", Reply: &TGMessage{ID: 100}}
			m.Chat.ID = c.C.Chat
			if handled, err := c.routeInactiveAnswer(context.Background(), m); !handled || err != nil {
				t.Fatal(handled, err)
			}
			loaded, err := LoadState(p.Config.StateDir)
			if err != nil {
				t.Fatal(err)
			}
			want := "queued"
			if deleted {
				want = "waiting"
			}
			if loaded.Jobs["task"].Stage != want {
				t.Fatalf("wrong stage for deleted=%v: %s", deleted, loaded.Jobs["task"].Stage)
			}
		})
	}
}

func TestFailedReminderDoesNotRecordDelivery(t *testing.T) {
	c := fixture(t)
	j := &Job{QuestionMessage: 42, BlockedReminder: time.Now().Add(-2 * time.Hour)}
	before := j.BlockedReminder
	c.API.Client.Transport = roundTrip(func(*http.Request) (*http.Response, error) { return response(`{"ok":false}`), nil })
	if err := c.sendBlockedReminder(context.Background(), j); err == nil || !j.BlockedReminder.Equal(before) || len(j.QuestionReminders) != 0 {
		t.Fatal("failed reminder recorded as delivered")
	}
}
