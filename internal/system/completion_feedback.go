package system

import (
	"context"
	"strings"
	"time"
)

// Older notifications did not persist their IDs. Telegram includes the original
// notification in reply_to_message, allowing an exact legacy match.
func completionReply(j *Job, reply *TGMessage) bool {
	for _, id := range j.CompletionMessages {
		if id != 0 && id == reply.ID {
			return true
		}
	}
	prefix := "Review complete: no issues found.\n" + j.Title + "\n" + j.PRURL + "\n\n"
	return j.Head != "" && strings.HasPrefix(reply.Text, prefix)
}

func (c *Controller) completionFeedback(ctx context.Context, m *TGMessage) (bool, error) {
	for _, j := range c.State.Jobs {
		if !completionReply(j, m.Reply) {
			continue
		}
		if j.ClosedPR || j.Stage == "closed" {
			_, err := c.API.Send(ctx, "This task's PR is closed. Create a new task for further changes; the closed branch will not be reopened automatically.")
			return true, err
		}
		j.Clarifications = append(j.Clarifications, Clarification{Role: "executor", Origin: "human-review", Question: "Human feedback on the completed task", Answer: m.Text, At: time.Now().UTC()})
		j.Answer = m.Text
		j.FeedbackPending = true
		// Persist feedback and update offset together before acknowledging it.
		c.resumeFeedback(j)
		return true, nil
	}
	return false, nil
}

// An active worker owns a snapshot of the job. Wait for it to finish before
// starting another turn, but invalidate Done immediately via FeedbackPending.
func (c *Controller) resumeFeedback(j *Job) {
	if !j.FeedbackPending || j.Stage == "running" || j.Stage == "publishing" {
		return
	}
	if j.ClosedPR || j.Stage == "closed" {
		j.FeedbackPending = false
		return
	}
	j.FeedbackPending = false
	j.ReviewedHead = ""
	j.NotifiedHead = ""
	j.Question = ""
	j.QuestionMessage = 0
	j.RetryAt = time.Time{}
	j.ModelBusyRetries = 0
	j.InvalidResultRetries = 0
	j.ClarificationRetries = 0
	j.Fixes = 0
	if j.External {
		j.Role = "reviewer"
		j.Stage = "review"
		return
	}
	j.Role = "executor"
	j.Stage = "queued"
	j.Findings = nil
}
