package system

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

type githubFeedback struct {
	ID        int64     `json:"id"`
	Body      string    `json:"body"`
	URL       string    `json:"html_url"`
	Path      string    `json:"path"`
	Line      int       `json:"line"`
	State     string    `json:"state"`
	Updated   time.Time `json:"updated_at"`
	Submitted time.Time `json:"submitted_at"`
	User      struct {
		Login string `json:"login"`
		Type  string `json:"type"`
	} `json:"user"`
	Kind string `json:"-"`
}

func (a *API) githubFeedback(ctx context.Context, pr int) ([]githubFeedback, error) {
	out := []githubFeedback{}
	for _, source := range []struct{ kind, path string }{
		{"comment", fmt.Sprintf("repos/%s/issues/%d/comments?per_page=100", a.C.Repository, pr)},
		{"inline", fmt.Sprintf("repos/%s/pulls/%d/comments?per_page=100", a.C.Repository, pr)},
		{"review", fmt.Sprintf("repos/%s/pulls/%d/reviews?per_page=100", a.C.Repository, pr)},
	} {
		b, err := a.gh(ctx, "api", "--paginate", "--slurp", source.path)
		if err != nil {
			return nil, err
		}
		var pages [][]githubFeedback
		if err = json.Unmarshal(b, &pages); err != nil {
			return nil, err
		}
		for _, page := range pages {
			for _, event := range page {
				event.Kind = source.kind
				if event.Updated.IsZero() {
					event.Updated = event.Submitted
				}
				out = append(out, event)
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Updated.Before(out[j].Updated) })
	return out, nil
}

func (c *Controller) pollGitHubFeedback(ctx context.Context) error {
	if c.C.ProjectMode == "local" || c.C.GithubUser == "" || time.Since(c.State.LastGitHubFeedbackPoll) < time.Minute {
		return nil
	}
	for _, j := range c.State.Jobs {
		if j.PR == 0 || j.ClosedPR || j.BoardCompleted || j.Stage == "closed" {
			continue
		}
		events, err := c.API.githubFeedback(ctx, j.PR)
		if err != nil {
			return err
		}
		initial := j.GitHubFeedbackSeen == nil
		if initial {
			j.GitHubFeedbackSeen = map[string]string{}
		}
		// On upgrade, don't replay feedback predating an already completed review.
		var legacyCutoff time.Time
		if initial && j.ReviewedHead != "" {
			for _, e := range events {
				if e.Kind == "comment" && strings.EqualFold(e.User.Login, c.C.GithubUser) && strings.HasPrefix(e.Body, "Automated review of commit "+j.ReviewedHead+".") && e.Updated.After(legacyCutoff) {
					legacyCutoff = e.Updated
				}
			}
		}
		var pending []githubFeedback
		for _, e := range events {
			key := fmt.Sprintf("%s:%d", e.Kind, e.ID)
			fingerprint := fmt.Sprintf("%x", sha256.Sum256([]byte(e.Body+"\n"+e.State)))
			if j.GitHubFeedbackSeen[key] == fingerprint {
				continue
			}
			j.GitHubFeedbackSeen[key] = fingerprint
			if !strings.EqualFold(e.User.Login, c.C.GithubUser) || strings.EqualFold(e.User.Type, "Bot") || strings.TrimSpace(e.Body) == "" || strings.HasPrefix(e.Body, "Automated review of commit ") || e.Updated.Before(legacyCutoff) {
				continue
			}
			if e.Kind == "review" && e.State != "COMMENTED" && e.State != "CHANGES_REQUESTED" {
				continue
			}
			pending = append(pending, e)
		}
		if len(pending) == 0 {
			continue
		}
		var text strings.Builder
		for _, e := range pending {
			fmt.Fprintf(&text, "GitHub %s by %s (%s)", e.Kind, e.User.Login, e.URL)
			if e.Path != "" {
				fmt.Fprintf(&text, " [%s:%d]", e.Path, e.Line)
			}
			fmt.Fprintf(&text, ":\n%s\n\n", e.Body)
		}
		_, ack, err := c.applyHumanFeedback(ctx, j, text.String())
		if err != nil {
			return err
		}
		c.panelEvent("github-feedback", j.ID, ack)
		// Persist deduplication and feedback before any worker can be scheduled.
		if err = c.save(); err != nil {
			return err
		}
	}
	c.State.LastGitHubFeedbackPoll = time.Now().UTC()
	return nil
}
