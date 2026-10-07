package system

import "strings"

// Only an authenticated Telegram reply retained by the controller grants this
// task-scoped capability. Titles, descriptions and agent results cannot grant it.
func workflowApproved(j *Job) bool {
	approved := false
	for _, c := range j.Clarifications {
		if c.Role != "executor" || c.Origin != "agent" || c.At.IsZero() {
			continue
		}
		q := strings.ToLower(strings.TrimSpace(c.Question))
		if !strings.HasPrefix(q, "may i add or modify a file in .github/workflows") {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(c.Answer)) {
		case "yes", "yes.", "да", "да.":
			approved = true
		case "no", "no.", "нет", "нет.":
			approved = false
		}
	}
	return approved
}

func (c *Controller) recoverAuthorizedPublications() {
	for _, j := range c.State.Jobs {
		if j.Stage == "waiting" && j.Role == "publisher" && j.LastError == "workflow edit needs explicit human permission" && !j.External && !j.ClosedPR && workflowApproved(j) {
			j.Stage = "publish"
			j.Question = ""
			j.QuestionMessage = 0
			j.LastError = ""
		}
	}
}
