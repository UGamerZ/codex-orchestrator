package system

import (
	"encoding/json"
	"strings"
	"unicode"
)

func clarificationContext(j Job) string {
	if len(j.Clarifications) == 0 {
		if j.Answer != "" {
			return "Previously supplied answer (still valid): " + j.Answer + "\n"
		}
		return ""
	}
	b, _ := json.Marshal(j.Clarifications)
	return "Saved clarification history. Apply all relevant answers; do not ask these questions again:\n" + string(b) + "\n"
}
func questionKey(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return unicode.ToLower(r)
		}
		return -1
	}, s)
}
func (c *Controller) agentQuestion(j *Job, role, question string) {
	repeated := false
	for _, entry := range j.Clarifications {
		if entry.Role == role && entry.Origin != "system" && questionKey(entry.Question) == questionKey(question) {
			repeated = true
			break
		}
	}
	if repeated && j.ClarificationRetries == 0 {
		// Retry once with the saved answers, in the same role and session, without
		// sending the identical question to Telegram again.
		j.ClarificationRetries++
		j.Question = ""
		j.QuestionMessage = 0
		j.Stage = "queued"
		if role == "reviewer" {
			j.Stage = "review"
		}
		return
	}
	if repeated {
		question = "Your previous answer was received and included in the agent context. The agent still reports this blocker after retrying: " + question + " Please clarify only the remaining unresolved detail."
	}
	c.wait(j, question)
	j.QuestionOrigin = "agent"
}
