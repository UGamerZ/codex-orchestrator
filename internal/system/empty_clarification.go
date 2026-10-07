package system

func (c *Controller) recoverEmptyClarifications() {
	for _, j := range c.State.Jobs {
		if j.Stage == "waiting" && j.LastError == "empty clarification" && j.InvalidResultRetries == 0 && !j.ClosedPR {
			j.InvalidResultRetries = 1
			j.Stage = "queued"
			if j.Role == "reviewer" {
				j.Stage = "review"
			}
			j.Question = ""
			j.QuestionMessage = 0
			j.LastError = ""
		}
	}
}
