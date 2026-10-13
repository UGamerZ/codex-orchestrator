package system

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Changing project ownership or card routing must never repoint retained jobs.
func environmentRoutingChanged(old, next Config) bool {
	return old.ProjectMode != next.ProjectMode || !strings.EqualFold(old.Repository, next.Repository) || old.BaseBranch != next.BaseBranch || !strings.EqualFold(old.GithubUser, next.GithubUser) || !strings.EqualFold(filepath.Clean(old.LocalRepository), filepath.Clean(next.LocalRepository)) || old.Board != next.Board || old.Todo != next.Todo || old.Progress != next.Progress || old.Done != next.Done
}

func (c *Controller) updateEnvironment(id string, input *EnvironmentInput, patch map[string]json.RawMessage) error {
	if !environmentID.MatchString(id) || input == nil || strings.TrimSpace(input.Name) == "" || len([]rune(strings.TrimSpace(input.Name))) > 80 {
		return fmt.Errorf("valid environment ID and name are required (maximum 80 characters)")
	}
	r, err := readRegistry(c.ConfigPath)
	if err != nil {
		return err
	}
	index := -1
	for i, p := range r.Profiles {
		if p.Deleted {
			continue
		}
		if p.ID == id {
			index = i
		} else if strings.EqualFold(p.Name, strings.TrimSpace(input.Name)) {
			return fmt.Errorf("environment name already exists")
		}
	}
	if index < 0 {
		return fmt.Errorf("environment not found")
	}
	active := id == r.Active
	if active && (!c.workersIdle() || len(c.State.PendingConfig) > 0 || (c.Login != nil && c.Login.Running())) {
		return fmt.Errorf("wait for active agents, publication, pending configuration and GitHub login before editing the active environment")
	}
	old := r.Profiles[index]
	if active {
		old.Config = c.C
	} else if _, e := os.Stat(filepath.Join(old.Config.StateDir, "controller.lock")); e == nil {
		return fmt.Errorf("environment is locked; inspect its controller before editing")
	}
	next := old.Config
	next.ProjectMode = input.Mode
	next.Repository = strings.TrimSpace(input.Repository)
	next.BaseBranch = strings.TrimSpace(input.Branch)
	next.GithubUser = strings.TrimSpace(input.Account)
	next.Board = strings.TrimSpace(input.Board)
	next.Todo = strings.TrimSpace(input.Todo)
	next.Progress = strings.TrimSpace(input.Progress)
	next.Done = strings.TrimSpace(input.Done)
	next.LocalRepository = ""
	if input.Mode == "local" {
		if strings.TrimSpace(input.LocalPath) == "" {
			return fmt.Errorf("local repository directory required")
		}
		next.LocalRepository, err = filepath.Abs(strings.TrimSpace(input.LocalPath))
		if err != nil {
			return err
		}
		next.Repository = ""
		next.GithubUser = ""
	}
	if strings.EqualFold(old.Config.Repository, next.Repository) {
		next.Repository = old.Config.Repository
	}
	if strings.EqualFold(old.Config.GithubUser, next.GithubUser) {
		next.GithubUser = old.Config.GithubUser
	}
	if strings.EqualFold(filepath.Clean(old.Config.LocalRepository), filepath.Clean(next.LocalRepository)) {
		next.LocalRepository = old.Config.LocalRepository
	}
	validator := &Controller{C: next, ConfigPath: c.ConfigPath}
	next, err = validator.validatedPanelConfig(patch)
	if err != nil {
		return err
	}
	changed := environmentRoutingChanged(old.Config, next)
	previous := r
	previous.Profiles = append([]EnvironmentProfile(nil), r.Profiles...)
	if changed {
		random := make([]byte, 8)
		if _, err = rand.Read(random); err != nil {
			return err
		}
		suffix := hex.EncodeToString(random)
		revision := "env-" + suffix
		next.StateDir = filepath.Join(filepath.Dir(c.ConfigPath), "environment-state", revision)
		if next.Backend == "native-windows" {
			next.NativeRoot = filepath.Join(r.NativeBase, "environments", revision)
		}
		// The old setup remains selectable with its original workspace and tasks.
		old.ID = "previous-" + suffix
		name := []rune(old.Name)
		if len(name) > 60 {
			name = name[:60]
		}
		old.Name = string(name) + " (previous " + suffix[:6] + ")"
		r.Profiles = append(r.Profiles, old)
		if next.ProjectMode == "local" {
			source := next.LocalRepository
			next.LocalRepository = filepath.Join(next.StateDir, "local.git")
			if err = os.MkdirAll(next.StateDir, 0700); err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			cmd := exec.CommandContext(ctx, "git", "-c", "core.hooksPath=", "-c", "protocol.ext.allow=never", "clone", "--bare", "--no-hardlinks", "--", source, next.LocalRepository)
			_, err = cmd.CombinedOutput()
			cancel()
			if err != nil {
				return fmt.Errorf("cannot import local Git history; original environment remains unchanged")
			}
		}
	}
	next, err = c.validateEnvironmentConfig(next)
	if err != nil {
		return err
	}
	r.Profiles[index].Name = strings.TrimSpace(input.Name)
	r.Profiles[index].Config = next
	if changed && next.Backend == "native-windows" {
		r.Profiles[index].IsolationVerified = false
	}
	if err = writePrivateJSON(registryPath(c.ConfigPath), r); err != nil {
		return err
	}
	if active {
		if changed {
			err = c.activateProfile(id, true)
		} else {
			err = writePrivateJSON(c.ConfigPath, next)
			if err == nil {
				c.C = next
				c.API.C = next
				if c.Runner != nil {
					c.Runner.C = next
				}
			}
		}
		if err != nil {
			if rollbackErr := writePrivateJSON(registryPath(c.ConfigPath), previous); rollbackErr != nil {
				return fmt.Errorf("edit failed: %v; registry rollback failed: %v", err, rollbackErr)
			}
			return err
		}
	}
	c.panelEvent("environment", "", "Updated environment: "+strings.TrimSpace(input.Name))
	return nil
}
