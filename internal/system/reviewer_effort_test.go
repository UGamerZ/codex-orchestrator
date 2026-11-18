package system

import (
	"strings"
	"testing"
)

func TestReviewerHighDoesNotChangeManagerEffort(t *testing.T) {
	c := nativeFixture(t)
	c.WorkerModel, c.ReviewerModel = "gpt-6.2-sol", "gpt-6.2-sol"
	c.WorkerEffort, c.ReviewerEffort = "high", "high"
	for _, role := range []string{"executor", "reviewer", "manager"} {
		args := strings.Join(nativeExecArgs(c, Job{}, role, c.NativeRoot, c.StateDir, "result", "schema"), " ")
		want := `model_reasoning_effort="high"`
		if role == "manager" {
			want = `model_reasoning_effort="low"`
		}
		if !strings.Contains(args, want) || (role != "manager" && !strings.Contains(args, "--model gpt-6.2-sol")) {
			t.Fatalf("wrong model/effort for %s", role)
		}
	}
	c.ReviewerEffort = ""
	if reviewerEffort(c) != c.OtherEffort {
		t.Fatal("legacy reviewer effort changed")
	}
}
