package system

import (
	"fmt"
	"strings"
)

func approvedDraftPublication(j *Job) error {
	if j.External || j.DraftSHA == "" || j.DraftSHA != j.ApprovedDraftSHA {
		return fmt.Errorf("publication requires a successful internal review of the exact local checkpoint")
	}
	return nil
}

func (c *Controller) queueReviewCorrections(j *Job, findings []string) {
	j.Fixes++
	j.Role = "executor"
	if !j.FixLimitDisabled && j.Fixes > c.C.MaxFixes {
		c.wait(j, "The automatic fix limit has been reached. Findings: "+strings.Join(findings, "\n")+". Clarify the next step; your reply resets the fix counter and authorizes a new full cycle of automatic fixes.")
		return
	}
	j.Stage = "queued"
}
