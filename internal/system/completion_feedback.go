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

func (c *Controller) completionFeedback(ctx context.Context, m *TGMessage) (bool, string, error) {
	if m.Reply == nil {
		return false, "", nil
	}
	for _, j := range c.State.Jobs {
		if !completionReply(j, m.Reply) {
			continue
		}
		// Even blocked feedback must be retained. Acknowledgment is sent by the caller
		// only after this state and the Telegram offset have been persisted.
		j.Clarifications = append(j.Clarifications, Clarification{Role: "executor", Origin: "human-review", Question: "Human feedback on the completed task", Answer: m.Text, At: time.Now().UTC()})
		j.Answer = m.Text
		if j.ClosedPR || j.Stage == "closed" {
			return true, "Feedback retained. This PR is closed; no work was restarted. Create a new task for further changes.", nil
		}
		if j.Stage == "running" || j.Stage == "publishing" {
			if j.BoardCompleted {
				return true, "Feedback retained. This task was completed outside the managed lists. Let the active run stop, then return the card to a managed list and reply again; no new work was started.", nil
			}
			j.FeedbackPending = true
			return true, "Feedback retained. The active run will finish before your feedback is processed for changes and another review.", nil
		}
		blocked := func(cause string) (bool, string, error) {
			j.FeedbackPending = false
			j.ReviewedHead = ""
			c.wait(j, "Human review was retained, but "+cause+". Resolve the integration issue and retry.")
			j.QuestionOrigin = "system"
			return true, "Feedback retained, but " + cause + ". Work remains blocked; it has not restarted.", nil
		}
		doneMoved := j.DoneMoved
		if j.Card != "" && c.C.Board != "" {
			card, err := c.API.CardLocation(ctx, j.Card)
			if err != nil {
				return blocked("the current Trello status could not be verified")
			}
			if !card.Closed && card.List == "" {
				return blocked("the current Trello list could not be verified")
			}
			if cardOutsideLists(c.C, card) {
				c.completeBoardTask(j)
				return true, "Feedback retained. The card is archived or outside To Do / In Progress / Done; no work was restarted. Return it to a managed list and reply to the review notification again to request changes.", nil
			}
			doneMoved = card.List == c.C.Done
		} else if j.BoardCompleted {
			return true, "Feedback retained. This task is completed and its return to a managed Trello list could not be verified; no work was restarted.", nil
		}
		if j.PR > 0 && c.C.ProjectMode != "local" {
			pull, err := c.API.Pull(ctx, j.PR)
			if err != nil {
				return blocked("the current PR status could not be verified")
			}
			if pull.State != "open" || pull.Merged {
				j.ClosedPR = true
				j.FeedbackPending = false
				if j.Stage != "running" && j.Stage != "publishing" {
					j.Stage = "closed"
				}
				return true, "Feedback retained. This PR is closed or merged; no work was restarted. Create a new task for further changes.", nil
			}
			if pull.Head.SHA != "" && pull.Head.SHA != j.Head && j.Stage != "running" && j.Stage != "publishing" {
				if c.C.AutoContinueTasks && systemCreatedTask(j) && pull.Head.Repo.FullName == c.C.Repository && pull.Head.Ref == j.Branch {
					adoptTaskHead(j, pull.Head.SHA)
				} else {
					// Receiving feedback is not permission to overwrite someone else's changes.
					j.External = true
					j.Head = pull.Head.SHA
					j.BaseSHA = ""
					j.ReviewerSession = ""
					j.Session = ""
				}
			}
		}
		if j.BoardCompleted {
			c.panelEvent("feedback", j.ID, "Card returned to a managed list; explicit human feedback resumed the open task")
		}
		j.BoardCompleted = false
		j.DoneMoved = doneMoved
		j.FeedbackPending = true
		c.resumeFeedback(j)
		if j.External {
			return true, "Feedback retained. This branch is review-only; another review is queued. Apply code changes manually.", nil
		}
		if j.FeedbackPending {
			return true, "Feedback retained. The active run will finish before your feedback is processed for changes and another review.", nil
		}
		return true, "Feedback retained. The executor is queued for changes and another review; the task will return to In Progress.", nil
	}
	return false, "", nil
}

// An active worker owns a snapshot of the job. Wait for it to finish before
// starting another turn, but invalidate Done immediately via FeedbackPending.
func (c *Controller) resumeFeedback(j *Job) {
	if !j.FeedbackPending || j.Stage == "running" || j.Stage == "publishing" {
		return
	}
	if j.BoardCompleted || j.ClosedPR || j.Stage == "closed" {
		j.FeedbackPending = false
		return
	}
	j.FeedbackPending = false
	j.ReviewedHead = ""
	j.NotifiedHead = ""
	j.LastError = ""
	j.Question = ""
	j.QuestionMessage = 0
	j.RetryAt = time.Time{}
	j.StallRetries = 0
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
