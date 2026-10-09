package system

import (
	"strings"
	"testing"
)

func TestExternalReviewNeedsVisibleBlocker(t *testing.T) {
	c := fixture(t)
	j := &Job{ID: "task", Stage: "changes", External: true, Findings: []string{"Remove unsupported sqlite3 extension"}}
	c.State.Jobs[j.ID] = j
	c.recoverExternalReviewBlockers()
	if j.Stage != "waiting" || j.Role != "reviewer" || j.QuestionMessage != 0 || !strings.Contains(j.Question, j.Findings[0]) || !j.External {
		t.Fatal("silent blocker or external permission lost")
	}
	c.recoverExternalReviewBlockers()
	if j.Stage != "waiting" {
		t.Fatal("blocker repeatedly reset")
	}
}
