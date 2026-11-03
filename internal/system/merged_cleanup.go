package system

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func cleanupCandidate(j *Job) bool {
	return j.ID != "manager" && (j.Card != "" || j.PR > 0) && j.CompletedCopiesCleanedAt.IsZero() && j.Stage != "running" && j.Stage != "publishing"
}

func (a *API) mergedPullHeads(ctx context.Context) (map[int]string, error) {
	b, err := a.gh(ctx, "api", "--paginate", "--slurp", "repos/"+a.C.Repository+"/pulls?state=closed&per_page=100")
	if err != nil {
		return nil, err
	}
	var pages [][]struct {
		Number   int        `json:"number"`
		MergedAt *time.Time `json:"merged_at"`
		Head     struct {
			SHA string `json:"sha"`
		} `json:"head"`
	}
	if err = json.Unmarshal(b, &pages); err != nil {
		return nil, err
	}
	heads := map[int]string{}
	for _, page := range pages {
		for _, p := range page {
			if p.MergedAt != nil && p.Head.SHA != "" {
				heads[p.Number] = p.Head.SHA
			}
		}
	}
	return heads, nil
}

// Remove only task-specific, reproducible project copies. Keep controller task
// history, Codex sessions/logs, account credentials, and shared sandbox setup.
func (r *Runner) removeMergedCopies(ctx context.Context, j *Job) error {
	if !safeID.MatchString(j.ID) {
		return fmt.Errorf("invalid cleanup task ID")
	}
	if r.C.Backend == "native-windows" {
		if !filepath.IsAbs(r.C.NativeRoot) || filepath.Clean(r.C.NativeRoot) == filepath.VolumeName(r.C.NativeRoot)+string(filepath.Separator) {
			return fmt.Errorf("cleanup requires dedicated absolute native root")
		}
		p, err := r.nativePaths(j.ID)
		if err != nil {
			return err
		}
		base := filepath.Join(r.C.NativeRoot, "jobs", j.ID)
		for _, ancestor := range []string{r.C.NativeRoot, filepath.Join(r.C.NativeRoot, "jobs"), base, filepath.Join(base, "private")} {
			if info, err := os.Lstat(ancestor); err == nil {
				if !info.IsDir() || info.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0 {
					return fmt.Errorf("cleanup ancestor is not a plain directory")
				}
			} else if !os.IsNotExist(err) {
				return err
			}
		}
		targets := []string{p.Workspace, p.Publish, filepath.Join(base, "review-input"), filepath.Join(base, "reviewer-tests"), filepath.Join(base, "private", "tools")}
		for _, role := range []string{"executor", "reviewer", "manager"} {
			home := filepath.Join(base, "private", role+"-runtime")
			targets = append(targets, filepath.Join(home, "codex", ".sandbox-bin"), filepath.Join(home, "codex", "tmp"), filepath.Join(home, "scratch"))
		}
		for _, target := range targets {
			rel, err := filepath.Rel(base, target)
			if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				return fmt.Errorf("cleanup target escaped task directory")
			}
			for parent := filepath.Dir(target); parent != base; parent = filepath.Dir(parent) {
				if parent == filepath.Dir(parent) {
					return fmt.Errorf("cleanup ancestor escaped task root")
				}
				if info, err := os.Lstat(parent); err == nil {
					if !info.IsDir() || info.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0 {
						return fmt.Errorf("cleanup ancestor is not a plain directory")
					}
				} else if !os.IsNotExist(err) {
					return err
				}
			}
			if info, err := os.Lstat(target); err == nil && (!info.IsDir() || info.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0) {
				return fmt.Errorf("cleanup root is not a plain directory")
			}
			// RemoveAll does not follow symlinks/reparse points within the tree.
			if err = os.RemoveAll(target); err != nil {
				return err
			}
		}
		return nil
	}
	b, err := r.docker(ctx, nil, "volume", "ls", "--filter", "label=codex-orchestrator="+r.namespace(), "--format", "{{.Name}}")
	if err != nil {
		return err
	}
	existing := map[string]bool{}
	for _, name := range strings.Fields(string(b)) {
		existing[name] = true
	}
	if existing[r.volume(j.ID, "reviewer-home")] {
		if err = r.cleanupDockerReviewTests(ctx, j.ID); err != nil {
			return err
		}
	}
	for _, kind := range []string{"workspace", "publish", "review"} {
		name := r.volume(j.ID, kind)
		if existing[name] {
			if _, err = r.docker(ctx, nil, "volume", "rm", name); err != nil {
				return err
			}
		}
	}
	return nil
}

func (c *Controller) cleanupMergedCopies(ctx context.Context) error {
	if time.Now().Before(c.NextMergedCleanup) {
		return nil
	}
	c.NextMergedCleanup = time.Now().Add(5 * time.Minute)
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	profiles := []EnvironmentProfile{{Config: c.C}}
	if c.ConfigPath != "" {
		r, err := readRegistry(c.ConfigPath)
		if err != nil {
			return err
		}
		profiles = r.Profiles
	}
	cache := map[string]map[int]string{}
	var cleanupErrors []error
	for _, profile := range profiles {
		cfg := profile.Config
		active := filepath.Clean(cfg.StateDir) == filepath.Clean(c.C.StateDir)
		state := c.State
		if active {
			cfg = c.C
		} else {
			b, err := os.ReadFile(filepath.Join(cfg.StateDir, "state.json"))
			if os.IsNotExist(err) {
				continue
			}
			if err != nil {
				cleanupErrors = append(cleanupErrors, err)
				continue
			}
			state = &State{}
			if err = json.Unmarshal(b, state); err != nil {
				cleanupErrors = append(cleanupErrors, err)
				continue
			}
		}
		var candidates []*Job
		for _, j := range state.Jobs {
			if cleanupCandidate(j) {
				candidates = append(candidates, j)
			}
		}
		if len(candidates) == 0 {
			continue
		}
		api := *c.API
		api.C = cfg
		key := cfg.GithubUser + "\n" + cfg.Repository
		changed := false
		for _, j := range candidates {
			boardCompleted := false
			merged := false
			if j.Card != "" && cfg.Board != "" {
				card, err := api.CardLocation(ctx, j.Card)
				if err != nil {
					cleanupErrors = append(cleanupErrors, fmt.Errorf("task %s Trello status: %w", j.ID, err))
					continue
				}
				if !cardOutsideLists(cfg, card) {
					continue
				}
				boardCompleted = true
			} else {
				if cfg.ProjectMode == "local" || j.PR == 0 || j.Head == "" || (j.Stage != "completed" && j.Stage != "closed" && j.Stage != "passed") {
					continue
				}
				heads, ok := cache[key]
				if !ok {
					var err error
					heads, err = api.mergedPullHeads(ctx)
					if err != nil {
						cleanupErrors = append(cleanupErrors, err)
						continue
					}
					cache[key] = heads
				}
				if heads[j.PR] != j.Head {
					continue
				}
				merged = true
			}
			runner := &Runner{C: cfg, API: &api}
			if err := runner.removeMergedCopies(ctx, j); err != nil {
				cleanupErrors = append(cleanupErrors, fmt.Errorf("task %s: %w", j.ID, err))
				continue
			}
			j.CompletedCopiesCleanedAt = time.Now().UTC()
			if merged {
				j.MergedCopiesCleanedAt = j.CompletedCopiesCleanedAt
				j.ClosedPR = true
				j.Stage = "closed"
			}
			if boardCompleted {
				j.BoardCompleted = true
				j.Stage = "completed"
				j.Question = ""
				j.QuestionMessage = 0
				j.FeedbackPending = false
			}
			changed = true
			c.panelEvent("cleanup", j.ID, fmt.Sprintf("Removed local project copies for completed task %s (PR #%d); task history and agent logs retained", j.ID, j.PR))
		}
		if changed {
			if err := state.Save(cfg.StateDir); err != nil {
				cleanupErrors = append(cleanupErrors, err)
			}
		}
	}
	return errors.Join(cleanupErrors...)
}
