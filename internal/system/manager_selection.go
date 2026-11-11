package system

import "encoding/json"

const invalidManagerIDs = "The manager returned unknown or duplicate task IDs."
const tooManyManagerTasks = "The manager selected more than two tasks. Clarify their priority."

func managerCards(pending []*Job) string {
	// Only the card has a selectable ID. Checklist IDs and internal lifecycle
	// fields must not compete with it in the manager's input.
	type card struct {
		ID          string `json:"id"`
		Title       string `json:"title"`
		Description string `json:"description"`
	}
	cards := []card{}
	for _, j := range pending {
		if j.BoardCompleted || j.ClosedPR {
			continue
		}
		cards = append(cards, card{j.ID, j.Title, taskDescription(j)})
	}
	b, _ := json.Marshal(cards)
	return string(b)
}

func (c *Controller) retryManagerSelection(j *Job) {
	if j.InvalidResultRetries >= 1 {
		c.wait(j, "The manager returned invalid card IDs twice. This is an internal selection error, not missing task information. The pending cards are retained; use Retry to try again.")
		return
	}
	j.InvalidResultRetries++
	j.Stage = "queued"
	j.Session = ""
	j.Question = ""
	j.QuestionMessage = 0
	j.QuestionOrigin = ""
	c.State.BoardCheckRequested = true
	c.panelEvent("manager-retry", j.ID, "Invalid manager selection rejected; retrying with current cards and a fresh session")
}

func (c *Controller) recoverManagerSelectionBlockers() {
	j := c.State.Jobs["manager"]
	if j != nil && j.Role == "manager" && j.Stage == "waiting" && j.QuestionOrigin == "system" && j.InvalidResultRetries == 0 && (j.Question == invalidManagerIDs || j.Question == tooManyManagerTasks) {
		c.retryManagerSelection(j)
	}
}
