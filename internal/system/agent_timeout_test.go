package system

import (
	"testing"
	"time"
)

func TestAgentRunTimeoutSeparatesReviewFromExecution(t *testing.T) {
	c := Config{TimeoutMinutes: 45}
	if agentRunTimeout(c, "reviewer") != 15*time.Minute {
		t.Fatal("default review deadline not bounded")
	}
	for _, role := range []string{"executor", "manager"} {
		if agentRunTimeout(c, role) != 45*time.Minute {
			t.Fatal("nonreview budget changed")
		}
	}
	c.ReviewTimeout = 7
	if agentRunTimeout(c, "reviewer") != 7*time.Minute {
		t.Fatal("custom review deadline ignored")
	}
	c.TimeoutMinutes = 5
	if agentRunTimeout(c, "reviewer") != 5*time.Minute {
		t.Fatal("review exceeded global run deadline")
	}
}
