package system

import "testing"

func TestTaskContinuationPreservesProvenanceAndHead(t *testing.T) {
	external := &Job{ID: "pr-7", Branch: "codex/pr-7", External: true}
	if systemCreatedTask(external) {
		t.Fatal("external PR granted write policy")
	}
	j := &Job{ID: "abc", Card: "abc", Branch: "codex/abc", ExecutorSession: "old", External: true, Head: "old", BaseSHA: "base", Clarifications: []Clarification{{Answer: "yes"}}}
	if !systemCreatedTask(j) {
		t.Fatal("legacy provenance lost")
	}
	adoptTaskHead(j, "new")
	if j.External || !j.SystemCreated || j.Head != "new" || j.BaseSHA != "" || j.Stage != "review" || j.ExecutorSession != "" || len(j.Clarifications) != 1 {
		t.Fatal("unsafe adoption")
	}
}
