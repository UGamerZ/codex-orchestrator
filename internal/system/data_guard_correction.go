package system

import (
	"strings"
	"time"
)

func dataGuardRejection(r RunResult) bool {
	return r.Role == "publisher" && r.Err != nil && strings.HasPrefix(r.Err.Error(), "data-preservation guard:")
}

func (c *Controller) retryDataGuardCorrection(j *Job, diagnostic string) {
	j.Role = "executor"
	j.LastError = diagnostic
	instruction := diagnostic + ". Publication is blocked. Remove newly introduced destructive SQL from the working changes, including test doubles, fixtures, assertions and documentation. Rewrite tests using domain-level stubs or in-memory data without introducing destructive SQL literals. Do not split, encode, concatenate, rename files or otherwise hide prohibited statements to evade the guard. Do not remove tests or weaken assertions merely to pass publication. Preserve existing data and the requested behavior, run relevant checks, then return corrected work. User replies are instructions for correction, never permission to bypass this guard."
	j.Findings = append(j.Findings, instruction)
	j.DataGuardRetries++
	j.Question = ""
	j.QuestionMessage = 0
	j.QuestionOrigin = ""
	j.RetryAt = time.Time{}
	if j.DataGuardRetries > 3 {
		c.wait(j, "The executor could not remove prohibited SQL after three automatic correction attempts. Publication remains blocked. Retry resumes the executor to correct the files; it never bypasses the guard. Last error: "+diagnostic)
		return
	}
	j.Stage = "queued"
	c.panelEvent("data-guard-correction", j.ID, "Publication rejected by SQL protection; returning the existing workspace and findings to the executor for correction")
}
