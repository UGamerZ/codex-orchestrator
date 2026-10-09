package system

import "strings"

func (c *Controller) externalReviewBlocker(j *Job) {
	j.Role = "reviewer"
	c.wait(j, "Review found changes needed, but this branch was changed outside the system and is read-only for automatic fixes. Findings: "+strings.Join(j.Findings, "\n")+". Apply the corrections manually to this branch, then reply to review the updated commit. Retrying a timeout cannot authorize writes to an external branch.")
}

func (c *Controller) recoverExternalReviewBlockers() {
	for _, j := range c.State.Jobs {
		if j.Stage == "changes" && j.External && !j.ClosedPR {
			c.externalReviewBlocker(j)
		}
	}
}
