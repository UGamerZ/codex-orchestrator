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
