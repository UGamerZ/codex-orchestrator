package system

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var pullCardURL = regexp.MustCompile(`https://trello\.com/c/([A-Za-z0-9]{8}|[a-f0-9]{24})(?:[/\s?#]|$)`)
var pullCardBranch = regexp.MustCompile(`^codex/([a-f0-9]{24})$`)

func pullCardReference(body, branch string) string {
	// A system task branch is stable even if the PR description was edited.
	if match := pullCardBranch.FindStringSubmatch(branch); match != nil {
		return match[1]
	}
	if match := pullCardURL.FindStringSubmatch(body); match != nil {
		return match[1]
	}
	return ""
}

func (c *Controller) inactivePullOwner(p Pull) (bool, string, error) {
	if c.ConfigPath == "" {
		return false, "", nil
	}
	registry, err := readRegistry(c.ConfigPath)
	if err != nil {
		return false, "", err
	}
	for _, profile := range registry.Profiles {
		if profile.ID == registry.Active || !strings.EqualFold(profile.Config.Repository, c.C.Repository) || profile.Config.ProjectMode == "local" {
			continue
		}
		b, err := os.ReadFile(filepath.Join(profile.Config.StateDir, "state.json"))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return false, "", err
		}
		var state State
		if err = json.Unmarshal(b, &state); err != nil {
			return false, "", fmt.Errorf("inactive environment task state unavailable")
		}
		for _, job := range state.Jobs {
			if job.External || job.Card == "" {
				continue
			}
			if job.PR == p.Number || (job.PR == 0 && p.Head.Repo.FullName == c.C.Repository && job.Branch == p.Head.Ref) {
				return true, job.Card, nil
			}
		}
	}
	return false, "", nil
}

// Resolve completion before creating/reopening an external review. Never use
// title matching, which can bind an unrelated task to a completed card.
func (c *Controller) reconcileDiscoveredPull(ctx context.Context, p Pull, j *Job) (*Job, bool, error) {
	if j != nil && j.BoardCompleted {
		return j, true, nil
	}
	foreign := false
	reference := ""
	if j == nil || j.External {
		var err error
		foreign, reference, err = c.inactivePullOwner(p)
		if err != nil {
			return j, false, err
		}
	}
	if j != nil && j.Card != "" {
		reference = j.Card
	}
	if reference == "" {
		branch := ""
		if strings.EqualFold(p.Head.Repo.FullName, c.C.Repository) {
			branch = p.Head.Ref
		}
		reference = pullCardReference(p.Body, branch)
	}
	var card Card
	if reference != "" && c.C.Board != "" {
		var err error
		card, err = c.resolvePullCard(ctx, reference)
		if err != nil {
			return j, false, err
		}
		if card.ID == "" || (!card.Closed && card.List == "") {
			return j, false, fmt.Errorf("linked Trello card status unavailable")
		}
	}
	completed := card.ID != "" && cardOutsideLists(c.C, card)
	if j == nil && (completed || !foreign) {
		id := fmt.Sprintf("pr-%d", p.Number)
		j = &Job{ID: id, Title: p.Title, Description: p.Body, Branch: "codex/" + id, Head: p.Head.SHA, PR: p.Number, PRURL: p.URL, External: true, Role: "reviewer", Stage: "review"}
		c.State.Jobs[id] = j
	}
	if j != nil && card.ID != "" {
		j.Card = card.ID
	}
	if completed {
		c.completeBoardTask(j)
		return j, true, nil
	}
	if foreign {
		if j != nil {
			j.BoardCompleted = true
			c.completeBoardTask(j)
			c.panelEvent("completed", j.ID, "PR belongs to an inactive environment; duplicate review stopped here")
		}
		return j, true, nil
	}
	return j, false, nil
}

// Recheck a card before reporting a late worker error, so moving a card out of
// managed lists during execution cannot produce a blocker or restart review.
func (c *Controller) checkResultCard(ctx context.Context, j *Job) error {
	if j == nil || j.BoardCompleted || c.C.Board == "" {
		return nil
	}
	reference := j.Card
	if reference == "" && j.External {
		reference = pullCardReference(j.Description, "")
	}
	if reference == "" {
		return nil
	}
	card, err := c.resolvePullCard(ctx, reference)
	if err != nil {
		return err
	}
	if card.ID == "" || (!card.Closed && card.List == "") {
		return fmt.Errorf("linked Trello card status unavailable")
	}
	j.Card = card.ID
	if cardOutsideLists(c.C, card) {
		c.completeBoardTask(j)
	}
	return nil
}

func (c *Controller) resolvePullCard(ctx context.Context, reference string) (Card, error) {
	if !regexp.MustCompile(`^[A-Za-z0-9]{8}$`).MatchString(reference) {
		return c.API.CardLocation(ctx, reference)
	}
	var resolved struct {
		Card
		ShortLink string `json:"shortLink"`
	}
	err := c.API.trello(ctx, "GET", "cards/"+reference, url.Values{"fields": {"id,idList,closed,shortLink"}}, &resolved)
	if err != nil {
		return resolved.Card, err
	}
	if resolved.ShortLink != reference || !regexp.MustCompile(`^[a-f0-9]{24}$`).MatchString(resolved.ID) || (!resolved.Closed && resolved.List == "") {
		return resolved.Card, fmt.Errorf("invalid linked card location response")
	}
	return resolved.Card, nil
}
