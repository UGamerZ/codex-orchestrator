package system

import (
	"context"
	"fmt"
	"time"
)

var errCardCompleted = fmt.Errorf("card is completed outside managed lists; automatic work stopped")

func cardOutsideLists(c Config, card Card) bool {
	return card.Closed || (card.List != "" && card.List != c.Todo && card.List != c.Progress && card.List != c.Done)
}
func (c *Controller) completeBoardTask(j *Job) {
	if !j.BoardCompleted {
		c.panelEvent("completed", j.ID, "Card left the managed Trello lists; automatic work permanently stopped")
	}
	j.BoardCompleted = true
	j.Question = ""
	j.QuestionMessage = 0
	j.LastError = ""
	j.RetryAt = time.Time{}
	j.FeedbackPending = false
	if j.Stage != "running" && j.Stage != "publishing" {
		j.Stage = "completed"
	}
}
func (c *Controller) syncBoardCompletion(ctx context.Context) error {
	// Headless unit controllers have no Trello board; production config requires it.
	if c.C.Board == "" {
		return nil
	}
	needsCheck := false
	for _, j := range c.State.Jobs {
		if j.Card != "" && !j.BoardCompleted {
			needsCheck = true
			break
		}
	}
	for _, j := range c.State.Pending {
		if j.Card != "" && !j.BoardCompleted {
			needsCheck = true
			break
		}
	}
	if !needsCheck {
		return nil
	}
	cards, err := c.API.Cards(ctx)
	if err != nil {
		return err
	}
	byID := map[string]Card{}
	for _, card := range cards {
		byID[card.ID] = card
	}
	jobs := make([]*Job, 0, len(c.State.Jobs)+len(c.State.Pending))
	for _, j := range c.State.Jobs {
		jobs = append(jobs, j)
	}
	jobs = append(jobs, c.State.Pending...)
	for _, j := range jobs {
		if j.Card == "" || j.BoardCompleted {
			continue
		}
		card, ok := byID[j.Card]
		if !ok {
			card, err = c.API.CardLocation(ctx, j.Card)
			if err != nil {
				return err
			}
		}
		if !card.Closed && card.List == "" {
			return fmt.Errorf("card list unavailable")
		}
		if cardOutsideLists(c.C, card) {
			c.completeBoardTask(j)
		}
	}
	return nil
}
func (r *Runner) verifyPublicationCard(ctx context.Context, j *Job) error {
	if j.BoardCompleted {
		return errCardCompleted
	}
	if j.Card == "" || r.C.Board == "" {
		return nil
	}
	card, err := r.API.CardLocation(ctx, j.Card)
	if err != nil {
		return err
	}
	if cardOutsideLists(r.C, card) {
		return errCardCompleted
	}
	return nil
}
