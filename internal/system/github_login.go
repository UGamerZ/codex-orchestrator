package system

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"
)

type GitHubLoginStatus struct {
	Status string `json:"status"`
	Code   string `json:"code,omitempty"`
	URL    string `json:"url,omitempty"`
	User   string `json:"user,omitempty"`
	Error  string `json:"error,omitempty"`
}
type GitHubLoginSession struct {
	mu     sync.Mutex
	status GitHubLoginStatus
	cancel context.CancelFunc
	buffer string
	path   string
}

var deviceCode = regexp.MustCompile(`\b[A-Z0-9]{4}-[A-Z0-9]{4}\b`)

func (s *GitHubLoginSession) Snapshot() GitHubLoginStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.status
}
func (s *GitHubLoginSession) Running() bool {
	status := s.Snapshot().Status
	return status == "starting" || status == "waiting"
}
func (s *GitHubLoginSession) Write(b []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.buffer += string(b)
	if len(s.buffer) > 8192 {
		s.buffer = s.buffer[len(s.buffer)-8192:]
	}
	if code := deviceCode.FindString(s.buffer); code != "" {
		s.status.Code = code
		s.status.URL = "https://github.com/login/device"
		s.status.Status = "waiting"
	}
	return len(b), nil
}
func (s *GitHubLoginSession) Start() error {
	s.mu.Lock()
	if s.status.Status == "starting" || s.status.Status == "waiting" {
		s.mu.Unlock()
		return fmt.Errorf("GitHub login already in progress")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	s.cancel = cancel
	s.buffer = ""
	s.status = GitHubLoginStatus{Status: "starting"}
	s.mu.Unlock()
	go func() {
		defer cancel()
		cmd := exec.CommandContext(ctx, s.path, "auth", "login", "--hostname", "github.com", "--git-protocol", "https", "--web", "--skip-ssh-key")
		cmd.Env = withoutEnv(os.Environ(), "GH_TOKEN", "GITHUB_TOKEN", "GH_HOST", "GH_BROWSER", "GH_PROMPT_DISABLED", "NO_COLOR")
		cmd.Env = append(cmd.Env, "GH_PROMPT_DISABLED=1", "NO_COLOR=1", "GH_BROWSER=orchestrator-no-browser")
		cmd.Stdin = strings.NewReader("\n")
		cmd.Stdout = s
		cmd.Stderr = s
		err := cmd.Run()
		user := ""
		if err == nil {
			check := exec.CommandContext(ctx, s.path, "api", "user", "--jq", ".login")
			check.Env = withoutEnv(os.Environ(), "GH_TOKEN", "GITHUB_TOKEN", "GH_HOST", "GH_BROWSER", "GH_PROMPT_DISABLED", "NO_COLOR")
			b, e := check.Output()
			if e != nil {
				err = e
			} else {
				user = strings.TrimSpace(string(b))
			}
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		s.status.Code = ""
		s.status.URL = ""
		if err != nil {
			s.status.Status = "failed"
			s.status.Error = "Login failed, expired, or was cancelled. Retry and complete the device authorization on GitHub."
		} else {
			s.status.Status = "completed"
			s.status.User = user
		}
		s.cancel = nil
		s.buffer = ""
	}()
	return nil
}
func (s *GitHubLoginSession) Cancel() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancel != nil {
		s.cancel()
	}
}

type GitHubAccount struct {
	Login  string `json:"login"`
	Active bool   `json:"active"`
	State  string `json:"state"`
}

func githubAccounts(path string) ([]GitHubAccount, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, "auth", "status", "--hostname", "github.com", "--json", "hosts")
	cmd.Env = withoutEnv(os.Environ(), "GH_TOKEN", "GITHUB_TOKEN", "GH_HOST", "GH_BROWSER", "GH_PROMPT_DISABLED", "NO_COLOR")
	b, err := cmd.Output()
	var data struct {
		Hosts map[string][]GitHubAccount `json:"hosts"`
	}
	if json.Unmarshal(b, &data) != nil {
		return nil, fmt.Errorf("GitHub account status unavailable")
	}
	accounts := data.Hosts["github.com"]
	if accounts == nil {
		accounts = []GitHubAccount{}
	}
	if err != nil && len(accounts) == 0 {
		return accounts, nil
	}
	return accounts, nil
}
