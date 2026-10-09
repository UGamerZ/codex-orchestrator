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
		switch workflowDecision(c.Answer) {
		case "approve":
			approved = true
		case "deny":
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

// Recognize explicit standalone approvals, not arbitrary sentences containing
// yes (which may include conditions, refusal, or quoted approvals).
func workflowDecision(answer string) string {
	normalized := strings.ToLower(strings.TrimSpace(answer))
	normalized = strings.Join(strings.Fields(strings.Map(func(r rune) rune {
		switch r {
		case ',', '.', '!', '?', ':', ';':
			return ' '
		}
		return r
	}, normalized)), " ")
	switch normalized {
	case "yes", "yes u can", "yes you can", "yes please", "yes go ahead", "go ahead", "approved", "i approve", "да", "да можно", "да разрешаю", "разрешаю":
		return "approve"
	case "no", "no you cannot", "no u cannot", "denied", "нет", "не разрешаю":
		return "deny"
	}
	return ""
}
