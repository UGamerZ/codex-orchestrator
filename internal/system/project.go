package system

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// ValidateProject checks the configured identity, repository write access and
// target branch before any model runs or project data is fetched.
func (a *API) ValidateProject(ctx context.Context) error {
	if a.C.ProjectMode == "local" {
		bare, e := localGit(ctx, a.C, "rev-parse", "--is-bare-repository")
		if e != nil || strings.TrimSpace(string(bare)) != "true" {
			return fmt.Errorf("local_repository must be a bare Git store")
		}
		b, e := localGit(ctx, a.C, "rev-parse", "--verify", "refs/heads/"+a.C.BaseBranch)
		if e != nil || len(b) == 0 {
			return fmt.Errorf("local base branch unavailable")
		}
		return nil
	}
	b, err := a.gh(ctx, "api", "user")
	if err != nil {
		return err
	}
	var user struct {
		Login string `json:"login"`
	}
	if err = json.Unmarshal(b, &user); err != nil {
		return err
	}
	if !strings.EqualFold(user.Login, a.C.GithubUser) {
		return fmt.Errorf("authenticated GitHub identity differs from configured account")
	}
	b, err = a.gh(ctx, "api", "repos/"+a.C.Repository)
	if err != nil {
		return err
	}
	var repo struct {
		Archived    bool `json:"archived"`
		Permissions struct {
			Push bool `json:"push"`
		} `json:"permissions"`
	}
	if err = json.Unmarshal(b, &repo); err != nil {
		return err
	}
	if repo.Archived || !repo.Permissions.Push {
		return fmt.Errorf("configured account needs write access to an active repository")
	}
	_, err = a.gh(ctx, "api", "repos/"+a.C.Repository+"/branches/"+url.PathEscape(a.C.BaseBranch))
	return err
}

// Never let a new configuration reuse another project's sessions and jobs.
func bindProject(c Config) error {
	if err := os.MkdirAll(c.StateDir, 0700); err != nil {
		return err
	}
	identity := c.Repository + "\n" + c.BaseBranch + "\n" + c.GithubUser + "\n" + c.ProjectMode + "\n" + c.LocalRepository
	path := filepath.Join(c.StateDir, "project.identity")
	b, err := os.ReadFile(path)
	if err == nil {
		if string(b) != identity {
			return fmt.Errorf("state directory belongs to a different project, branch or account; choose a separate state_dir")
		}
		return nil
	}
	if !os.IsNotExist(err) {
		return err
	}
	if _, err := os.Stat(filepath.Join(c.StateDir, "state.json")); err == nil {
		return fmt.Errorf("existing state has no project identity; use a new state_dir")
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(identity)
	return err
}
