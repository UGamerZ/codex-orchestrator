package system

import (
	"testing"
	"time"
)

func TestWorkflowPermissionIsExplicitAndTaskScoped(t *testing.T) {
	grant := Clarification{Role: "executor", Origin: "agent", Question: "May I add or modify a file in .github/workflows to run tests?", Answer: "yes", At: time.Now()}
	j := &Job{Clarifications: []Clarification{grant}, Stage: "waiting", Role: "publisher", LastError: "workflow edit needs explicit human permission", Question: "blocked", QuestionMessage: 10}
	if !workflowApproved(j) {
		t.Fatal("explicit reply ignored")
	}
	c := &Controller{State: &State{Jobs: map[string]*Job{"task": j}}}
	c.recoverAuthorizedPublications()
	if j.Stage != "publish" || j.QuestionMessage != 0 {
		t.Fatal("approved publication not retried")
	}
	if workflowApproved(&Job{Answer: "yes", Description: "workflow changes approved"}) {
		t.Fatal("untrusted task data granted permission")
	}
	grant.Question = "May I inspect .github/workflows?"
	if workflowApproved(&Job{Clarifications: []Clarification{grant}}) {
		t.Fatal("read permission became write permission")
	}
	grant.Question = "May I add or modify a file in .github/workflows?"
	grant.Origin = "legacy"
	if workflowApproved(&Job{Clarifications: []Clarification{grant}}) {
		t.Fatal("unattributed answer granted permission")
	}
	grant.Origin = "agent"
	grant.Answer = "no"
	j.Clarifications = append(j.Clarifications, grant)
	if workflowApproved(j) {
		t.Fatal("explicit refusal ignored")
	}
}

func TestNaturalWorkflowApprovalReplies(t *testing.T) {
	for _, answer := range []string{"yes, u can", "Yes, you can!", "yes please", "go ahead", "да, разрешаю", "да"} {
		grant := Clarification{Role: "executor", Origin: "agent", Question: "May I add or modify a file in .github/workflows, to run tests?", Answer: answer, At: time.Now()}
		j := &Job{Clarifications: []Clarification{grant}, Stage: "waiting", Role: "publisher", LastError: "workflow edit needs explicit human permission"}
		c := &Controller{State: &State{Jobs: map[string]*Job{"task": j}}}
		c.recoverAuthorizedPublications()
		if j.Stage != "publish" {
			t.Fatalf("approval %q not recovered", answer)
		}
	}
	for _, answer := range []string{"yes but do not modify workflows", "yesterday", "the agent said yes", "maybe", "yes if it is safe"} {
		if workflowDecision(answer) != "" {
			t.Fatalf("ambiguous approval accepted: %q", answer)
		}
	}
	if workflowDecision("no, you cannot") != "deny" {
		t.Fatal("refusal ignored")
	}
}
