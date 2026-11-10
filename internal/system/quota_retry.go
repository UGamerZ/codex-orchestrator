package system

import "time"

func (c *Controller) retryQuotaOnBoardCheck() int {
	resumed := 0
	for _, j := range c.State.Jobs {
		if j.ClosedPR || j.BoardCompleted || (j.Stage != "queued" && j.Stage != "review") {
			continue
		}
		// Older versions saved no retry reason for quota errors. Recover the
		// stranded manager only when its state matches that specific old path;
		// network, timeout and overload retries have separate counters/errors.
		legacyManager := j.ID == "manager" && j.Role == "manager" && len(c.State.Pending) > 0 &&
			!j.RetryAt.IsZero() && j.LastError == "" && j.Question == "" &&
			j.ModelBusyRetries == 0 && j.NetworkRetries == 0 && j.StallRetries == 0 && j.InvalidResultRetries == 0
		if !j.RateLimited && !legacyManager {
			continue
		}
		j.RetryAt = time.Time{}
		j.RateLimited = false
		resumed++
		c.panelEvent("quota-retry", j.ID, "Manual board check requested a retry after the Codex usage limit; task and session retained")
	}
	return resumed
}
