package system

import (
	"context"
	"fmt"
	"time"

	"codex-orchestrator/internal/netretry"
)

func retryableNetworkResult(r RunResult) bool {
	return r.Err != nil && r.Role != "publisher" && netretry.ReadFailure(r.Err.Error())
}

func (c *Controller) retryNetworkFailure(ctx context.Context, j *Job, role, diagnostic string) bool {
	if j.ClosedPR || j.BoardCompleted || role == "publisher" || !netretry.ReadFailure(diagnostic) {
		return false
	}
	if j.NetworkRetries >= 6 {
		j.LastError = diagnostic
		c.wait(j, "Git connectivity is still failing after six automatic task retries (each with up to three Git attempts). The task, workspace, and sessions are retained. Check the controller's network connection to GitHub, then use Retry in the dashboard or reply to this message. No task clarification is needed. Last error: "+diagnostic)
		return true
	}
	j.NetworkRetries++
	delay := time.Minute * time.Duration(1<<(j.NetworkRetries-1))
	if delay > 30*time.Minute {
		delay = 30 * time.Minute
	}
	j.Stage = "queued"
	if role == "reviewer" {
		j.Stage = "review"
	}
	j.Role = role
	j.RetryAt = time.Now().UTC().Add(delay)
	j.LastError = diagnostic
	j.Question = ""
	j.QuestionMessage = 0
	j.QuestionOrigin = ""
	message := fmt.Sprintf("Temporary Git connection failure [%s]: %s\nAutomatic retry %d/6 scheduled in %s. No reply is needed. Task files and sessions are retained.\n\n%s", j.ID, j.Title, j.NetworkRetries, delay, diagnostic)
	c.panelEvent("network-retry", j.ID, message)
	if j.NetworkRetries == 1 {
		if _, err := c.API.Send(ctx, message); err != nil {
			c.panelEvent("notification-error", j.ID, "Could not send automatic retry notification")
		}
	}
	return true
}

// Recover old system blockers after upgrade without touching human questions,
// completed cards, or uncertain publications.
func (c *Controller) recoverNetworkFailures(ctx context.Context) {
	for _, j := range c.State.Jobs {
		if j.Stage == "waiting" && j.QuestionOrigin == "system" && j.NetworkRetries < 6 {
			c.retryNetworkFailure(ctx, j, j.Role, j.LastError)
		}
	}
}
