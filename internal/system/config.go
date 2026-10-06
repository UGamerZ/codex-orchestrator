package system

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

type Config struct {
	ProjectMode     string `json:"project_mode"`
	LocalRepository string `json:"local_repository,omitempty"`
	Backend         string `json:"backend"`
	NativeRoot      string `json:"native_root"`
	CodexPath       string `json:"codex_path"`
	NativeVerified  bool   `json:"native_isolation_verified"`
	DockerDistro    string `json:"docker_distro"`
	Repository      string `json:"repository"`
	BaseBranch      string `json:"base_branch"`
	GithubUser      string `json:"github_user"`
	GHPath          string `json:"gh_path"`
	StateDir        string `json:"state_dir"`
	AuthFile        string `json:"codex_auth_file"`
	Board           string `json:"trello_board"`
	Todo            string `json:"trello_todo"`
	Progress        string `json:"trello_progress"`
	Done            string `json:"trello_done"`
	Chat            int64  `json:"telegram_chat"`
	WorkerModel     string `json:"worker_model"`
	WorkerEffort    string `json:"worker_effort"`
	FallbackModel   string `json:"fallback_model"`
	FallbackEffort  string `json:"fallback_effort"`
	ManagerModel    string `json:"manager_model"`
	ReviewerModel   string `json:"reviewer_model"`
	OtherEffort     string `json:"other_effort"`
	Image           string `json:"worker_image"`
	Network         string `json:"network"`
	Proxy           string `json:"proxy_url"`
	MaxWorkers      int    `json:"max_workers"`
	MaxFixes        int    `json:"max_fix_rounds"`
	TimeoutMinutes  int    `json:"run_timeout_minutes"`
	ManagerInterval int    `json:"manager_interval_seconds"`
	PollSeconds     int    `json:"poll_seconds"`
}

func LoadConfig(path string) (Config, error) {
	var c Config
	f, e := os.Open(path)
	if e != nil {
		return c, e
	}
	defer f.Close()
	d := json.NewDecoder(f)
	d.DisallowUnknownFields()
	if e = d.Decode(&c); e != nil {
		return c, e
	}
	if c.Backend == "" {
		c.Backend = "docker"
	}
	if c.Backend != "docker" && c.Backend != "native-windows" {
		return c, fmt.Errorf("unknown execution backend")
	}
	if c.Backend == "native-windows" {
		if c.NativeRoot == "" || c.CodexPath == "" {
			return c, fmt.Errorf("native_root and codex_path required")
		}
		c.NativeRoot, e = filepath.Abs(c.NativeRoot)
		if e != nil {
			return c, e
		}
		if !c.NativeVerified {
			return c, fmt.Errorf("native isolation must pass Check-NativeIsolation.ps1 before enabling service")
		}
	}
	if c.ProjectMode == "" {
		c.ProjectMode = "github"
	}
	if c.ProjectMode != "github" && c.ProjectMode != "local" {
		return c, fmt.Errorf("project_mode must be github or local")
	}
	if c.ProjectMode == "local" {
		if c.LocalRepository == "" {
			return c, fmt.Errorf("local_repository required")
		}
		c.LocalRepository, e = filepath.Abs(c.LocalRepository)
		if e != nil {
			return c, e
		}
	} else if !regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`).MatchString(c.Repository) {
		return c, fmt.Errorf("invalid repository")
	}
	if c.BaseBranch == "" || (c.ProjectMode == "github" && (c.GithubUser == "" || c.GHPath == "")) {
		return c, fmt.Errorf("base_branch, github_user and gh_path required")
	}
	if strings.HasPrefix(c.BaseBranch, "-") || strings.ContainsAny(c.BaseBranch, " ~^:?*[\\\x00\r\n") || strings.Contains(c.BaseBranch, "..") || strings.Contains(c.BaseBranch, "@{") {
		return c, fmt.Errorf("invalid base branch")
	}
	if c.MaxWorkers < 1 || c.MaxWorkers > 2 {
		return c, fmt.Errorf("max_workers must be 1 or 2")
	}
	if c.MaxFixes < 1 || c.TimeoutMinutes < 1 || c.ManagerInterval < 3600 || c.PollSeconds < 5 {
		return c, fmt.Errorf("invalid limits; board interval must be >=3600 seconds")
	}
	for _, model := range []string{c.WorkerModel, c.FallbackModel, c.ManagerModel, c.ReviewerModel} {
		if !regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`).MatchString(model) {
			return c, fmt.Errorf("invalid model name")
		}
	}
	for _, effort := range []string{c.WorkerEffort, c.FallbackEffort, c.OtherEffort} {
		if effort != "low" && effort != "medium" && effort != "high" && effort != "xhigh" {
			return c, fmt.Errorf("invalid reasoning effort")
		}
	}
	for _, v := range []string{c.Board, c.Todo, c.Progress, c.Done} {
		if !regexp.MustCompile(`^[a-f0-9]{24}$`).MatchString(v) {
			return c, fmt.Errorf("Trello board/list IDs must be actual 24-character IDs")
		}
	}
	if c.Todo == c.Progress || c.Todo == c.Done || c.Progress == c.Done || c.Chat == 0 {
		return c, fmt.Errorf("distinct lists and telegram_chat required")
	}
	for _, k := range []string{"TRELLO_KEY", "TRELLO_TOKEN", "TELEGRAM_TOKEN"} {
		if os.Getenv(k) == "" {
			return c, fmt.Errorf("missing %s", k)
		}
	}
	c.StateDir, e = filepath.Abs(c.StateDir)
	if e != nil {
		return c, e
	}
	c.AuthFile, e = filepath.Abs(c.AuthFile)
	if e != nil {
		return c, e
	}
	if c.Backend == "docker" && (c.Network != "codex-agents" || c.Proxy != "http://proxy:8080") {
		return c, fmt.Errorf("use isolated codex-agents network and its proxy")
	}
	if _, e = os.Stat(c.AuthFile); e != nil {
		return c, fmt.Errorf("Codex ChatGPT auth file unavailable: %w", e)
	}
	return c, nil
}
