package system

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func environmentFixture(t *testing.T) *Controller {
	t.Helper()
	c := fixture(t)
	dir := t.TempDir()
	c.ConfigPath = filepath.Join(dir, "config.json")
	c.C.Backend = "docker"
	c.C.Network = "codex-agents"
	c.C.Proxy = "http://proxy:8080"
	c.C.ProjectMode = "github"
	c.C.GithubUser = "account-one"
	c.C.GHPath = "gh"
	c.C.PollSeconds = 20
	c.C.TimeoutMinutes = 45
	c.C.AuthFile = filepath.Join(dir, "private-auth.json")
	os.WriteFile(c.C.AuthFile, []byte("{}"), 0600)
	c.C.Board = strings.Repeat("1", 24)
	c.C.Todo = strings.Repeat("2", 24)
	c.C.Progress = strings.Repeat("3", 24)
	c.C.Done = strings.Repeat("4", 24)
	c.C.WorkerModel = "gpt-6.1-sol"
	c.C.FallbackModel = "gpt-6-sol"
	c.C.ManagerModel = "gpt-6-luna"
	c.C.ReviewerModel = "gpt-6-luna"
	c.C.WorkerEffort = "low"
	c.C.FallbackEffort = "medium"
	c.C.OtherEffort = "low"
	for _, key := range []string{"TRELLO_KEY", "TRELLO_TOKEN", "TELEGRAM_TOKEN"} {
		t.Setenv(key, "fixture-only")
	}
	c.API.C = c.C
	if err := writePrivateJSON(c.ConfigPath, c.C); err != nil {
		t.Fatal(err)
	}
	if err := c.initializeEnvironments(); err != nil {
		t.Fatal(err)
	}
	return c
}
func TestEnvironmentCreationSeparatesProjectAndStorage(t *testing.T) {
	c := environmentFixture(t)
	input := &EnvironmentInput{Name: "Second project", Mode: "github", Repository: "second/repo", Branch: "release", Account: "account-two", Board: strings.Repeat("5", 24), Todo: strings.Repeat("6", 24), Progress: strings.Repeat("7", 24), Done: strings.Repeat("8", 24)}
	if err := c.createEnvironment(input); err != nil {
		t.Fatal(err)
	}
	r, err := readRegistry(c.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Profiles) != 2 || r.Active != "default" {
		t.Fatal("creation changed active environment")
	}
	next := r.Profiles[1].Config
	if next.StateDir == c.C.StateDir || next.Repository != input.Repository || next.GithubUser != input.Account || next.BaseBranch != input.Branch || !strings.HasPrefix(next.StateDir, filepath.Dir(c.ConfigPath)) {
		t.Fatal("project or storage mixed")
	}
	if err := c.createEnvironment(input); err == nil {
		t.Fatal("duplicate name accepted")
	}
	input.Name = "Other"
	input.Board = c.C.Board
	input.Todo = c.C.Todo
	if err := c.createEnvironment(input); err == nil {
		t.Fatal("duplicate task queue accepted")
	}
	safe, err := publicEnvironmentProfiles(c.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(safe)
	if strings.Contains(string(b), c.C.AuthFile) || strings.Contains(string(b), "codex_auth_file") || strings.Contains(string(b), "native_root") {
		t.Fatal("private configuration exposed")
	}
}
func TestEnvironmentSwitchRefusesActiveWorkOrLogin(t *testing.T) {
	c := environmentFixture(t)
	for _, stage := range []string{"running", "publishing"} {
		c.State.Jobs["active"] = &Job{ID: "active", Stage: stage}
		if err := c.switchEnvironment("default"); err == nil {
			t.Fatal("active work bypassed")
		}
		if err := c.executePanelCommand(PanelCommand{Action: "github-login"}); err == nil {
			t.Fatal("login during work accepted")
		}
	}
	delete(c.State.Jobs, "active")
	c.Login = &GitHubLoginSession{status: GitHubLoginStatus{Status: "waiting"}}
	if err := c.switchEnvironment("default"); err == nil {
		t.Fatal("login lock bypassed")
	}
	c.Login = nil
	c.State.PendingConfig = map[string]json.RawMessage{"max_workers": json.RawMessage("1")}
	if err := c.switchEnvironment("default"); err == nil {
		t.Fatal("pending configuration bypassed")
	}
}
func TestEnvironmentActivationPreservesIndependentStateAndTelegramOffset(t *testing.T) {
	c := environmentFixture(t)
	c.State.TelegramOffset = 90
	c.State.Jobs["first"] = &Job{ID: "first", Stage: "waiting", ExecutorSession: "session-one"}
	c.LockPath = filepath.Join(c.C.StateDir, "controller.lock")
	os.WriteFile(c.LockPath, []byte(fmt.Sprint(os.Getpid())), 0600)
	oldDir := c.C.StateDir
	r, _ := readRegistry(c.ConfigPath)
	next := c.C
	next.StateDir = t.TempDir()
	next.Repository = "other/project"
	next.GithubUser = "account-two"
	r.Profiles = append(r.Profiles, EnvironmentProfile{ID: "other", Name: "Other", Config: next})
	second := &State{Jobs: map[string]*Job{"second": {ID: "second", Stage: "waiting", ExecutorSession: "session-two"}}, TelegramOffset: 20}
	os.WriteFile(filepath.Join(next.StateDir, "controller.lock"), []byte(fmt.Sprint(os.Getpid())), 0600)
	if err := c.activateEnvironment(r, 1, next, second); err != nil {
		t.Fatal(err)
	}
	if c.State.TelegramOffset != 90 || c.State.Jobs["first"] != nil || c.API.C.GithubUser != "account-two" || c.Runner.C.Repository != "other/project" {
		t.Fatal("active state or account mixed")
	}
	old, _ := LoadState(oldDir)
	if old.Jobs["first"].ExecutorSession != "session-one" {
		t.Fatal("old session lost")
	}
	if _, err := os.Stat(filepath.Join(oldDir, "controller.lock")); !os.IsNotExist(err) {
		t.Fatal("old controller lock retained")
	}
	// Simulate registry label written before the configuration in a crash.
	r.Active = "default"
	writePrivateJSON(registryPath(c.ConfigPath), r)
	if err := c.initializeEnvironments(); err != nil {
		t.Fatal(err)
	}
	recovered, _ := readRegistry(c.ConfigPath)
	if recovered.Active != "other" {
		t.Fatal("crash label not reconciled")
	}
	next.MaxWorkers = 1
	if err := c.saveProfileConfiguration(next); err != nil {
		t.Fatal(err)
	}
	recovered, _ = readRegistry(c.ConfigPath)
	if recovered.Profiles[1].Config.MaxWorkers != 1 {
		t.Fatal("runtime settings not retained per environment")
	}
	c.ReleaseControllerLock()
}
func TestPanelReadsSelectedEnvironmentState(t *testing.T) {
	c := environmentFixture(t)
	oldDir := c.C.StateDir
	(&State{Jobs: map[string]*Job{"old": {ID: "old"}}}).Save(oldDir)
	next := c.C
	next.StateDir = t.TempDir()
	(&State{Jobs: map[string]*Job{"new": {ID: "new"}}}).Save(next.StateDir)
	writePrivateJSON(c.ConfigPath, next)
	h := panelHandler(c.ConfigPath, oldDir, "token", make(chan PanelCommand, 1))
	req := httptest.NewRequest("GET", "http://localhost:8787/api/status", nil)
	req.Header.Set("Cookie", "panel_session=token")
	req.Header.Set("X-Panel-Token", "token")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"new"`) || strings.Contains(w.Body.String(), `"old"`) {
		t.Fatal("panel used stale environment", w.Body.String())
	}
}
func TestInactiveEnvironmentReplyReachesOriginalTask(t *testing.T) {
	c := environmentFixture(t)
	c.API.Client.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
		if !strings.HasSuffix(r.URL.Path, "/sendMessage") {
			t.Fatal("unexpected request")
		}
		return response(`{"ok":true,"result":{"message_id":100}}`), nil
	})
	r, _ := readRegistry(c.ConfigPath)
	next := c.C
	next.StateDir = t.TempDir()
	r.Profiles = append(r.Profiles, EnvironmentProfile{ID: "inactive", Name: "Inactive", Config: next})
	writePrivateJSON(registryPath(c.ConfigPath), r)
	inactive := &State{Jobs: map[string]*Job{"task": {ID: "task", Role: "executor", Stage: "waiting", Question: "Which option?", QuestionMessage: 42}}}
	inactive.Save(next.StateDir)
	c.State.Jobs["task"] = &Job{ID: "task", Stage: "waiting", QuestionMessage: 99}
	m := &TGMessage{Text: "Use existing option", Reply: &TGMessage{ID: 42}}
	m.Chat.ID = c.C.Chat
	if handled, err := c.routeInactiveAnswer(context.Background(), m); !handled || err != nil {
		t.Fatal(handled, err)
	}
	loaded, _ := LoadState(next.StateDir)
	if loaded.Jobs["task"].Stage != "queued" || loaded.Jobs["task"].Answer != m.Text || c.State.Jobs["task"].QuestionMessage != 99 {
		t.Fatal("reply routed to wrong environment")
	}
	loaded.Jobs["task"].Stage = "passed"
	loaded.Jobs["task"].Head = "head"
	loaded.Jobs["task"].ReviewedHead = "head"
	loaded.Jobs["task"].CompletionMessages = []int64{55}
	loaded.Save(next.StateDir)
	m.Reply.ID = 55
	m.Text = "Please correct the label"
	if handled, err := c.routeInactiveAnswer(context.Background(), m); !handled || err != nil {
		t.Fatal(handled, err)
	}
	loaded, _ = LoadState(next.StateDir)
	if loaded.Jobs["task"].Stage != "queued" || loaded.Jobs["task"].ReviewedHead != "" {
		t.Fatal("inactive completion feedback lost")
	}
}
func TestLocalEnvironmentImportsHistoryWithoutChangingSource(t *testing.T) {
	c := environmentFixture(t)
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git unavailable")
	}
	source := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = source
		if b, err := cmd.CombinedOutput(); err != nil {
			t.Fatal(err, string(b))
		}
	}
	run("init", "-b", "main")
	run("-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "--allow-empty", "-m", "Initial fixture")
	os.WriteFile(filepath.Join(source, "unsaved.txt"), []byte("keep this"), 0600)
	input := &EnvironmentInput{Name: "Local", Mode: "local", LocalPath: source, Branch: "main", Board: strings.Repeat("5", 24), Todo: strings.Repeat("6", 24), Progress: strings.Repeat("7", 24), Done: strings.Repeat("8", 24)}
	if err := c.createEnvironment(input); err != nil {
		t.Fatal(err)
	}
	r, _ := readRegistry(c.ConfigPath)
	next := r.Profiles[1].Config
	if next.LocalRepository == source || next.Repository != "" || next.GithubUser != "" {
		t.Fatal("source or GitHub identity reused")
	}
	cmd := exec.Command("git", "--git-dir", next.LocalRepository, "rev-parse", "--verify", "main")
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatal(err, string(b))
	}
	b, err := os.ReadFile(filepath.Join(source, "unsaved.txt"))
	if err != nil || string(b) != "keep this" {
		t.Fatal("source changed")
	}
}
func TestGitHubDeviceLoginExposesOnlyCodeAndURL(t *testing.T) {
	s := &GitHubLoginSession{status: GitHubLoginStatus{Status: "starting"}}
	s.Write([]byte("First copy your one-time code: ABCD-"))
	s.Write([]byte("1234\nprivate diagnostic token ghp_should_not_escape"))
	b, _ := json.Marshal(s.Snapshot())
	if !strings.Contains(string(b), "ABCD-1234") || !strings.Contains(string(b), "https://github.com/login/device") || strings.Contains(string(b), "ghp_") || strings.Contains(string(b), "diagnostic") {
		t.Fatal("invalid public login status")
	}
	if err := s.Start(); err == nil {
		t.Fatal("parallel login accepted")
	}
}

func TestInstallationLockPreventsControllersInDifferentProfiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	release, err := LockInstallation(path)
	if err != nil {
		t.Fatal(err)
	}
	if second, err := LockInstallation(path); err == nil {
		second()
		t.Fatal("second controller acquired registry")
	}
	release()
	next, err := LockInstallation(path)
	if err != nil {
		t.Fatal(err)
	}
	next()
}
