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
	// Keep the full audit history in state, but do not repeatedly feed obsolete
	// reviewer findings back as if they were outstanding requirements.
	history := append([]Clarification(nil), j.Clarifications...)
	for i := range history {
		if history[i].Origin == "system" && strings.HasPrefix(history[i].Question, "The automatic fix limit has been reached.") {
			history[i].Question = "Earlier correction cycle resumed. Historical findings are not pending requirements; use the CURRENT reviewer findings."
		}
	}
	b, _ := json.Marshal(history)
	return "Saved human answers remain valid. Use the latest applicable human decision; do not repeat answered questions or reopen historical findings:\n" + string(b) + "\n"
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
