package system

import (
	"context"
	"strings"
	"testing"
)

func TestExecutorCommitTitlePersistsPerRun(t *testing.T) {
	c := fixture(t)
	j := &Job{ID: "task", Title: "Logging is broken", Stage: "running"}
	c.State.Jobs[j.ID] = j
	for _, title := range []string{"Restore disabled debug logging", "Guard recursive SAPI logging fallback"} {
		v := Verdict{Status: "completed", Summary: "Detailed implementation and validation", CommitTitle: title}
		if e := validateVerdict("executor", v); e != nil {
			t.Fatal(e)
		}
		if e := c.finish(context.Background(), RunResult{ID: j.ID, Role: "executor", Verdict: v}); e != nil {
			t.Fatal(e)
		}
		if e := c.save(); e != nil {
			t.Fatal(e)
		}
		state, e := LoadState(c.C.StateDir)
		if e != nil {
			t.Fatal(e)
		}
		if got := publicationTitle(state.Jobs[j.ID]); got != title {
			t.Fatalf("commit title %q; want %q", got, title)
		}
	}
}

func TestInvalidExecutorCommitTitlesRejected(t *testing.T) {
	for _, title := range []string{"", " \t ", "Fix logs\nOther subject", "Fix\x00logs", "Fix error logging when debug disabled", strings.Repeat("x", 61), strings.Repeat("я", 101)} {
		if validateVerdict("executor", Verdict{Status: "completed", Summary: "Changed logs", CommitTitle: title}) == nil {
			t.Fatalf("accepted %q", title)
		}
	}
	if got := publicationTitle(&Job{Title: "Card title", Summary: "Restore stdout logging"}); got != "Restore stdout logging" {
		t.Fatalf("legacy publication title %q", got)
	}
}

func TestLegacyPublicationTitlesAreShort(t *testing.T) {
	title := publicationTitle(&Job{Summary: "Restore error logging when debug is disabled and include diagnostics"})
	if len(strings.Fields(title)) > 5 || len([]rune(title)) > 60 {
		t.Fatal(title)
	}
}
