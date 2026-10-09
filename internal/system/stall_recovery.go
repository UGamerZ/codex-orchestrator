package system

import "time"

const interruptedQuestion = "The controller restarted during processing. The interrupted task is paused. Reply to retry and continue."

// Called only after StopAbandoned confirms that prior workers have stopped.
// Publisher retries need separate reconciliation and are never inferred here.
func (c *Controller) recoverInterruptedAgents() {
	for _, j := range c.State.Jobs {
		if j.Stage == "waiting" && j.Question == interruptedQuestion && !j.ClosedPR && (j.Role == "executor" || j.Role == "reviewer" || j.Role == "manager") {
			c.retryStalledAgent(j, j.Role)
		}
	}
}

// Run deadlines terminate the native job / stop the Docker container before
// returning a result. Retry only after receiving that result, never merely from
// an idle timestamp: a quiet agent may still be doing useful work.
func (c *Controller) retryStalledAgent(j *Job, role string) {
	if role == "publisher" || (role == "executor" && j.External) {
		c.wait(j, "Automatic recovery cannot write this branch or repeat an uncertain publication. Inspect the branch state before retrying.")
		return
	}
	j.StallRetries++
	if j.StallRetries > 3 {
		c.wait(j, "The agent timed out or was interrupted repeatedly. Three automatic retries have been exhausted; the task, workspace, and known session are retained. Reply to retry after checking the blocker.")
		return
	}
	j.Role = role
	j.Stage = "queued"
	if role == "reviewer" {
		j.Stage = "review"
	}
	j.RetryAt = time.Now().UTC().Add(5 * time.Minute)
	j.Question = ""
	j.QuestionMessage = 0
	j.LastError = "Agent interrupted or exceeded its execution deadline; automatic retry scheduled"
}
