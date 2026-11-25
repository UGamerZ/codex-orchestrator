package system

import (
	"context"
	"fmt"
	"time"
)

const reviewRefMismatchError = "PR head changed during preparation"

func reviewRefMismatch(r RunResult) bool {
	return r.Role == "reviewer" && r.Err != nil && r.Err.Error() == reviewRefMismatchError
}

// Never write refs, adopt an unverified head or weaken the broker's exact-SHA
// check. A lagging PR ref is retried without asking a human to change anything.
func (c *Controller) retryReviewRefWait(ctx context.Context, j *Job) bool {
	if j.ClosedPR || j.BoardCompleted || j.External || !systemCreatedTask(j) || j.PR == 0 || j.Head == "" {
		return false
	}
	p := Pull{Number: j.PR}
	p.Head.Ref, p.Head.Repo.FullName = j.Branch, c.C.Repository
	head, err := c.API.BranchHead(ctx, p)
	if err != nil || head != j.Head {
		return false // A real branch edit must still fail closed.
	}
	first := !j.ReviewRefWaiting
	j.ReviewRefWaiting = true
	j.Role, j.Stage = "reviewer", "review"
	j.RetryAt = time.Now().UTC().Add(time.Minute)
	j.LastError = "GitHub PR reference has not synchronized with the verified task branch. Automatic recheck in one minute; no reply needed. The expected commit is unchanged."
	j.Question, j.QuestionOrigin, j.QuestionMessage = "", "", 0
	if first {
		c.panelEvent("github-ref-wait", j.ID, j.LastError)
		if _, err := c.API.Send(ctx, fmt.Sprintf("GitHub synchronization delay [%s]: %s\n\n%s\nExpected commit: %s", j.ID, j.Title, j.LastError, j.Head)); err != nil {
			c.panelEvent("notification-error", j.ID, "Could not deliver GitHub synchronization delay notification")
		}
	}
	return true
}

func (c *Controller) recoverReviewRefWaits(ctx context.Context) {
	for _, j := range c.State.Jobs {
		if j.Stage == "waiting" && j.Role == "reviewer" && j.LastError == reviewRefMismatchError {
			c.retryReviewRefWait(ctx, j)
		}
	}
}
