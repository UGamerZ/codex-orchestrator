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
	"regexp"
	"strings"
	"time"
)

type EnvironmentProfile struct {
	Deleted           bool   `json:"deleted,omitempty"`
	ID                string `json:"id"`
	Name              string `json:"name"`
	Config            Config `json:"config"`
	IsolationVerified bool   `json:"isolation_verified"`
}
type EnvironmentRegistry struct {
	Active     string               `json:"active"`
	NativeBase string               `json:"native_base"`
	Profiles   []EnvironmentProfile `json:"profiles"`
}
type EnvironmentInput struct {
	Name       string `json:"name"`
	Mode       string `json:"project_mode"`
	Repository string `json:"repository"`
	LocalPath  string `json:"local_repository"`
	Branch     string `json:"base_branch"`
	Account    string `json:"github_user"`
	Board      string `json:"trello_board"`
	Todo       string `json:"trello_todo"`
	Progress   string `json:"trello_progress"`
	Done       string `json:"trello_done"`
}

var environmentID = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,39}$`)

func registryPath(configPath string) string {
	return filepath.Join(filepath.Dir(configPath), "environments.json")
}
func writePrivateJSON(path string, value any) error {
	b, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	if err = os.WriteFile(path+".tmp", b, 0600); err != nil {
		return err
	}
	return os.Rename(path+".tmp", path)
}
func readRegistry(path string) (EnvironmentRegistry, error) {
	var r EnvironmentRegistry
	b, err := os.ReadFile(registryPath(path))
	if err == nil {
		err = json.Unmarshal(b, &r)
	}
	return r, err
}
func (c *Controller) initializeEnvironments() error {
	r, err := readRegistry(c.ConfigPath)
	if os.IsNotExist(err) {
		r = EnvironmentRegistry{Active: "default", NativeBase: c.C.NativeRoot, Profiles: []EnvironmentProfile{{ID: "default", Name: "Current project", Config: c.C, IsolationVerified: true}}}
	} else if err != nil {
		return err
	}
	found := false
	for i := range r.Profiles {
		if !r.Profiles[i].Deleted && filepath.Clean(r.Profiles[i].Config.StateDir) == filepath.Clean(c.C.StateDir) {
			r.Active = r.Profiles[i].ID
			r.Profiles[i].Config = c.C
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("active configuration is not registered; restore its environment registry")
	}
	return writePrivateJSON(registryPath(c.ConfigPath), r)
}
func (c *Controller) workersIdle() bool {
	for _, j := range c.State.Jobs {
		if j.Stage == "running" || j.Stage == "publishing" {
			return false
		}
	}
	return true
}
func (c *Controller) validateEnvironmentConfig(config Config) (Config, error) {
	path := c.ConfigPath + ".environment-validate"
	if err := writePrivateJSON(path, config); err != nil {
		return config, err
	}
	defer os.Remove(path)
	return LoadConfig(path)
}
func (c *Controller) createEnvironment(input *EnvironmentInput) error {
	if input == nil || strings.TrimSpace(input.Name) == "" || len([]rune(input.Name)) > 80 {
		return fmt.Errorf("environment name is required (maximum 80 characters)")
	}
	r, err := readRegistry(c.ConfigPath)
	if err != nil {
		return err
	}
	for _, p := range r.Profiles {
		if !p.Deleted && strings.EqualFold(p.Name, strings.TrimSpace(input.Name)) {
			return fmt.Errorf("environment name already exists")
		}
	}
	random := make([]byte, 8)
	if _, err = rand.Read(random); err != nil {
		return err
	}
	id := "env-" + hex.EncodeToString(random)
	config := c.C
	config.ProjectMode = input.Mode
	config.Repository = strings.TrimSpace(input.Repository)
	config.BaseBranch = strings.TrimSpace(input.Branch)
	config.GithubUser = strings.TrimSpace(input.Account)
	config.Board = input.Board
	config.Todo = input.Todo
	config.Progress = input.Progress
	config.Done = input.Done
	config.StateDir = filepath.Join(filepath.Dir(c.ConfigPath), "environment-state", id)
	if config.Backend == "native-windows" {
		config.NativeRoot = filepath.Join(r.NativeBase, "environments", id)
	}
	config.LocalRepository = ""
	if input.Mode == "local" {
		if !c.workersIdle() {
			return fmt.Errorf("wait for workers to finish before importing a local repository")
		}
		if strings.TrimSpace(input.LocalPath) == "" {
			return fmt.Errorf("local repository directory required")
		}
		source, err := filepath.Abs(input.LocalPath)
		if err != nil {
			return err
		}
		info, err := os.Stat(source)
		if err != nil || !info.IsDir() {
			return fmt.Errorf("local repository directory unavailable")
		}
		config.LocalRepository = filepath.Join(config.StateDir, "local.git")
		config.Repository = ""
		config.GithubUser = ""
		if err = os.MkdirAll(config.StateDir, 0700); err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		cmd := exec.CommandContext(ctx, "git", "-c", "core.hooksPath=", "-c", "protocol.ext.allow=never", "clone", "--bare", "--no-hardlinks", "--", source, config.LocalRepository)
		if _, err = cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("cannot import local Git history; select an existing Git repository")
		}
	}
	config, err = c.validateEnvironmentConfig(config)
	if err != nil {
		return err
	}
	profile := EnvironmentProfile{ID: id, Name: strings.TrimSpace(input.Name), Config: config, IsolationVerified: config.Backend != "native-windows"}
	r.Profiles = append(r.Profiles, profile)
	if err = writePrivateJSON(registryPath(c.ConfigPath), r); err != nil {
		return err
	}
	c.panelEvent("environment", "", "Created environment: "+profile.Name)
	return nil
}
func (c *Controller) switchEnvironment(id string) error { return c.activateProfile(id, false) }
func (c *Controller) activateProfile(id string, force bool) error {
	if !environmentID.MatchString(id) {
		return fmt.Errorf("invalid environment")
	}
	if !c.workersIdle() {
		return fmt.Errorf("wait for active agents and publication to finish before switching")
	}
	if c.Login != nil && c.Login.Running() {
		return fmt.Errorf("finish or cancel GitHub login before switching")
	}
	if len(c.State.PendingConfig) != 0 {
		return fmt.Errorf("wait for pending configuration to apply before switching")
	}
	r, err := readRegistry(c.ConfigPath)
	if err != nil {
		return err
	}
	target := -1
	for i := range r.Profiles {
		if r.Profiles[i].ID == id && !r.Profiles[i].Deleted {
			target = i
		}
		if r.Profiles[i].ID == r.Active && !(force && r.Profiles[i].ID == id) {
			r.Profiles[i].Config = c.C
		}
	}
	if target < 0 {
		return fmt.Errorf("environment not found")
	}
	if id == r.Active && !force {
		return nil
	}
	next, err := c.validateEnvironmentConfig(r.Profiles[target].Config)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Join(next.StateDir, "logs"), 0700); err != nil {
		return err
	}
	lockPath := filepath.Join(next.StateDir, "controller.lock")
	lock, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return fmt.Errorf("target environment is already locked; inspect its controller before switching")
	}
	fmt.Fprint(lock, os.Getpid())
	lock.Close()
	committed := false
	defer func() {
		if !committed {
			os.Remove(lockPath)
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	api := NewAPI(next)
	runner := &Runner{C: next, API: api}
	if err = api.Validate(ctx); err != nil {
		return err
	}
	if environmentNeedsIsolationCheck(r.Profiles[target]) {
		if err = VerifyNativeIsolation(ctx, next); err != nil {
			return fmt.Errorf("environment isolation check failed: %w", err)
		}
		r.Profiles[target].IsolationVerified = true
	}
	if err = runner.Preflight(ctx); err != nil {
		return err
	}
	if err = runner.StopAbandoned(ctx); err != nil {
		return err
	}
	if err = bindProject(next); err != nil {
		return err
	}
	state, err := LoadState(next.StateDir)
	if err != nil {
		return err
	}
	if err = c.activateEnvironment(r, target, next, state); err != nil {
		return err
	}
	committed = true
	c.recoverDataGuardPublications()
	c.recoverExternalReviewBlockers()
	c.recoverInterruptedAgents()
	c.recoverDocumentationReviews(ctx)
	c.recoverAuthorizedPublications()
	c.recoverEmptyClarifications()
	c.panelEvent("environment", "", "Activated environment: "+r.Profiles[target].Name)
	return nil
}
func (c *Controller) ReleaseControllerLock() {
	if c.LockPath != "" {
		b, err := os.ReadFile(c.LockPath)
		if err == nil && strings.TrimSpace(string(b)) == fmt.Sprint(os.Getpid()) {
			os.Remove(c.LockPath)
		}
	}
}
func (c *Controller) saveProfileConfiguration(next Config) error {
	if c.ConfigPath == "" {
		return nil
	}
	r, err := readRegistry(c.ConfigPath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	for i := range r.Profiles {
		if r.Profiles[i].ID == r.Active {
			r.Profiles[i].Config = next
			return writePrivateJSON(registryPath(c.ConfigPath), r)
		}
	}
	return fmt.Errorf("active environment missing")
}

func publicEnvironmentProfiles(configPath string) (map[string]any, error) {
	r, err := readRegistry(configPath)
	if err != nil {
		return nil, err
	}
	profiles := []map[string]any{}
	for _, p := range r.Profiles {
		if p.Deleted {
			continue
		}
		profiles = append(profiles, map[string]any{"id": p.ID, "name": p.Name, "project_mode": p.Config.ProjectMode, "repository": p.Config.Repository, "base_branch": p.Config.BaseBranch, "github_user": p.Config.GithubUser, "trello_board": p.Config.Board, "trello_todo": p.Config.Todo, "trello_progress": p.Config.Progress, "trello_done": p.Config.Done, "isolation_verified": p.IsolationVerified, "local_repository": p.Config.LocalRepository, "backend": p.Config.Backend, "worker_model": p.Config.WorkerModel, "worker_effort": p.Config.WorkerEffort, "fallback_model": p.Config.FallbackModel, "fallback_effort": p.Config.FallbackEffort, "manager_model": p.Config.ManagerModel, "reviewer_model": p.Config.ReviewerModel, "other_effort": p.Config.OtherEffort, "auto_continue_task_branches": p.Config.AutoContinueTasks, "max_workers": p.Config.MaxWorkers, "max_fix_rounds": p.Config.MaxFixes, "run_timeout_minutes": p.Config.TimeoutMinutes, "review_timeout_minutes": int(agentRunTimeout(p.Config, "reviewer") / time.Minute), "manager_interval_seconds": p.Config.ManagerInterval})
	}
	return map[string]any{"active": r.Active, "profiles": profiles}, nil
}

// Inactive profiles have no live workers. Retain answers in the originating
// environment rather than routing them to the currently selected repository.
func (c *Controller) routeInactiveAnswer(ctx context.Context, m *TGMessage) (bool, error) {
	if m.Reply == nil || strings.TrimSpace(m.Text) == "" || c.ConfigPath == "" {
		return false, nil
	}
	r, err := readRegistry(c.ConfigPath)
	if err != nil {
		return false, nil
	}
	for _, p := range r.Profiles {
		if p.ID == r.Active || p.Config.Chat != m.Chat.ID {
			continue
		}
		path := filepath.Join(p.Config.StateDir, "state.json")
		b, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var state State
		if json.Unmarshal(b, &state) != nil {
			continue
		}
		if p.Deleted {
			for _, j := range state.Jobs {
				matched := j.QuestionMessage == m.Reply.ID
				for _, message := range j.CompletionMessages {
					matched = matched || message == m.Reply.ID
				}
				if matched {
					_, err = c.API.Send(ctx, "This task belongs to a deleted environment: "+p.Name+". Its files and history are retained, but replies cannot restart its agents.")
					return true, err
				}
			}
			continue
		}
		inactive := &Controller{C: p.Config, API: c.API, State: &state}
		if handled, feedbackErr := inactive.completionFeedback(ctx, m); handled {
			if feedbackErr != nil {
				return true, feedbackErr
			}
			if err = state.Save(p.Config.StateDir); err != nil {
				return true, err
			}
			_, err = c.API.Send(ctx, "Feedback retained for inactive environment: "+p.Name+". Switch to that environment to continue its task.")
			return true, err
		}
		for _, j := range state.Jobs {
			if j.Stage != "waiting" || j.BoardCompleted || j.ClosedPR || j.QuestionMessage == 0 || j.QuestionMessage != m.Reply.ID {
				continue
			}
			j.Answer = m.Text
			j.Clarifications = append(j.Clarifications, Clarification{Role: j.Role, Origin: j.QuestionOrigin, Question: j.Question, Answer: m.Text, At: time.Now().UTC()})
			j.Question = ""
			j.QuestionMessage = 0
			j.RetryAt = time.Time{}
			j.StallRetries = 0
			j.ClarificationRetries = 0
			j.ModelBusyRetries = 0
			j.InvalidResultRetries = 0
			j.Stage = "queued"
			if j.Role == "reviewer" {
				j.Stage = "review"
			}
			if j.Role == "publisher" {
				j.Stage = "publish"
			}
			if err = state.Save(p.Config.StateDir); err != nil {
				return true, err
			}
			_, err = c.API.Send(ctx, "Answer retained for inactive environment: "+p.Name+". Switch to that environment to continue its agent.")
			return true, err
		}
	}
	return false, nil
}

// Commit the prepared environment only after access and isolation checks succeeded.
func (c *Controller) activateEnvironment(r EnvironmentRegistry, target int, next Config, state *State) error {
	if state.TelegramOffset < c.State.TelegramOffset {
		state.TelegramOffset = c.State.TelegramOffset
	}
	if err := c.save(); err != nil {
		return err
	}
	if err := state.Save(next.StateDir); err != nil {
		return err
	}
	// Registry may be ahead after a crash; startup derives active profile from the
	// actual configuration/state directory instead of trusting the active label.
	previousRegistry := r
	r.Active = r.Profiles[target].ID
	r.Profiles[target].Config = next
	if err := writePrivateJSON(registryPath(c.ConfigPath), r); err != nil {
		return err
	}
	if err := writePrivateJSON(c.ConfigPath, next); err != nil {
		writePrivateJSON(registryPath(c.ConfigPath), previousRegistry)
		return err
	}
	oldLock := c.LockPath
	c.C = next
	c.API = NewAPI(next)
	c.Runner = &Runner{C: next, API: c.API}
	c.State = state
	c.LockPath = filepath.Join(next.StateDir, "controller.lock")

	if oldLock != "" && oldLock != c.LockPath {
		os.Remove(oldLock)
	}
	return nil
}

// One controller owns the environment registry, regardless of its selected state directory.
func LockInstallation(configPath string) (func(), error) {
	absolute, err := filepath.Abs(configPath)
	if err != nil {
		return nil, err
	}
	path := filepath.Join(filepath.Dir(absolute), "environment-controller.lock")
	lock, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return nil, fmt.Errorf("installation controller lock exists or unavailable; verify the previous controller has stopped before removing it")
	}
	fmt.Fprint(lock, os.Getpid())
	lock.Close()
	return func() {
		b, e := os.ReadFile(path)
		if e == nil && strings.TrimSpace(string(b)) == fmt.Sprint(os.Getpid()) {
			os.Remove(path)
		}
	}, nil
}
