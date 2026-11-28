package system

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

func TestClarificationHistorySurvivesMultipleReplies(t *testing.T) {
	c := fixture(t)
	j := &Job{ID: "task", Role: "executor", Stage: "waiting", QuestionMessage: 10, Question: "Which mode?", QuestionOrigin: "agent", ExecutorSession: "original"}
	c.State.Jobs[j.ID] = j
	reply := "Use existing mode"
	id := "10"
	c.API.Client.Transport = roundTrip(func(*http.Request) (*http.Response, error) {
		return response(`{"ok":true,"result":[{"update_id":1,"message":{"text":"` + reply + `","chat":{"id":123,"type":"private"},"reply_to_message":{"message_id":` + id + `}}}]}`), nil
	})
	if e := c.answers(context.Background()); e != nil {
		t.Fatal(e)
	}
	c.agentQuestion(j, "executor", "Which threshold?")
	j.QuestionMessage = 11
	reply = "200"
	id = "11"
	if e := c.answers(context.Background()); e != nil {
		t.Fatal(e)
	}
	if len(j.Clarifications) != 2 || j.ExecutorSession != "original" {
		t.Fatal("clarification history or session lost")
	}
	prompt := c.prompt(*j, "executor")
	if !strings.Contains(prompt, "Use existing mode") || !strings.Contains(prompt, "200") {
		t.Fatal("earlier reply absent from next turn")
	}
	state, e := LoadState(c.C.StateDir)
	if e != nil || len(state.Jobs[j.ID].Clarifications) != 2 {
		t.Fatal("clarifications not persisted")
	}
}
func TestPromptKeepsHumanDecisionsWithoutReinjectingOldFindings(t *testing.T) {
	j := Job{Findings: []string{"Current material regression"}, Clarifications: []Clarification{
		{Origin: "system", Question: "The automatic fix limit has been reached. Findings: Obsolete fixed bug", Answer: "Continue; keep the existing API"},
		{Origin: "agent", Question: "May I add or modify a file in .github/workflows for this task?", Answer: "Yes, for this task only"},
	}}
	text := clarificationContext(j)
	if strings.Contains(text, "Obsolete fixed bug") || !strings.Contains(text, "keep the existing API") || !strings.Contains(text, ".github/workflows") || !strings.Contains(text, "Yes, for this task only") {
		t.Fatal("prompt lost a human decision or reinjected obsolete findings")
	}
	if !strings.Contains(j.Clarifications[0].Question, "Obsolete fixed bug") || j.Findings[0] != "Current material regression" {
		t.Fatal("prompt compaction modified saved history or current findings")
	}
}

func TestAnsweredQuestionIsNotImmediatelySentAgain(t *testing.T) {
	c := fixture(t)
	j := &Job{ID: "task", Role: "executor", Stage: "running", ExecutorSession: "same", Clarifications: []Clarification{{Role: "executor", Origin: "agent", Question: "Which mode?", Answer: "Existing mode"}}}
	c.State.Jobs[j.ID] = j
	for attempt := 0; attempt < 2; attempt++ {
		if e := c.finish(context.Background(), RunResult{ID: j.ID, Role: "executor", Verdict: Verdict{Status: "question", Question: "Which mode?"}}); e != nil {
			t.Fatal(e)
		}
		if attempt == 0 && (j.Stage != "queued" || j.Question != "") {
			t.Fatal("answered question immediately re-sent")
		}
	}
	if j.Stage != "waiting" || j.Question == "Which mode?" || !strings.Contains(j.Question, "previous answer was received") {
		t.Fatal("repeated question was not diagnosed")
	}
	if j.ExecutorSession != "same" {
		t.Fatal("retry replaced session")
	}
}
