package system

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"codex-orchestrator/internal/broker"
)

type Verdict struct {
	CommitTitle string   `json:"commit_title"`
	Status      string   `json:"status"`
	Question    string   `json:"question"`
	Summary     string   `json:"summary"`
	Findings    []string `json:"findings"`
	Selected    []string `json:"selected"`
}
type RunResult struct {
	TimedOut           bool
	EmptyClarification bool
	ModelBusy          bool
	ID                 string
	Role               string
	Session            string
	Verdict            Verdict
	Err                error
	RateLimited        bool
	ModelUnavailable   bool
	BaseSHA            string
	Publication        broker.Result
}
type Runner struct {
	C   Config
	API *API
}

// Keep only the start (thread ID) and tail (final verdict), rather than accepting
// unbounded tool output from an untrusted agent into the controller's memory.
type boundedOutput struct {
	First []byte
	Tail  []byte
}

func (b *boundedOutput) Write(p []byte) (int, error) {
	n := len(p)
	if len(b.First) < 128<<10 {
		remaining := (128 << 10) - len(b.First)
		count := len(p)
		if count > remaining {
			count = remaining
		}
		b.First = append(b.First, p[:count]...)
	}
	const limit = 8 << 20
	if len(p) >= limit {
		b.Tail = append(b.Tail[:0], p[len(p)-limit:]...)
	} else {
		if excess := len(b.Tail) + len(p) - limit; excess > 0 {
			copy(b.Tail, b.Tail[excess:])
			b.Tail = b.Tail[:len(b.Tail)-excess]
		}
		b.Tail = append(b.Tail, p...)
	}
	return n, nil
}
func (b *boundedOutput) String() string { return string(b.Tail) }

var safeID = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)

func (r *Runner) namespace() string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(r.C.StateDir+"\n"+r.C.Repository+"\n"+r.C.LocalRepository)))[:12]
}
func (r *Runner) volume(id, kind string) string {
	return "codex-" + r.namespace() + "-" + id + "-" + kind
}
func (r *Runner) container(id, role string) string {
	return "codex-run-" + r.namespace() + "-" + id + "-" + role
}
func (r *Runner) dockerCommand(ctx context.Context, args ...string) *exec.Cmd {
	if r.C.DockerVM == "colima" {
		return exec.CommandContext(ctx, "docker", append([]string{"--context", "colima-codex-orchestrator"}, args...)...)
	}
	if r.C.DockerDistro != "" {
		return exec.CommandContext(ctx, "wsl.exe", append([]string{"--distribution", r.C.DockerDistro, "--user", "root", "--exec", "docker"}, args...)...)
	}
	return exec.CommandContext(ctx, "docker", args...)
}
func (r *Runner) hostCommand(ctx context.Context, args ...string) *exec.Cmd {
	if r.C.DockerVM == "colima" {
		return exec.CommandContext(ctx, "colima", append([]string{"ssh", "codex-orchestrator", "--", "sudo", "-n", "--"}, args...)...)
	}
	if r.C.DockerDistro != "" {
		return exec.CommandContext(ctx, "wsl.exe", append([]string{"--distribution", r.C.DockerDistro, "--user", "root", "--exec"}, args...)...)
	}
	return exec.CommandContext(ctx, args[0], args[1:]...)
}
func (r *Runner) networkPolicy(ctx context.Context) error {
	checks := [][]string{
		{"iptables", "-C", "INPUT", "-s", "172.30.250.0/24", "-j", "DROP"},
		{"iptables", "-C", "DOCKER-USER", "-j", "CODEX-AGENTS"},
		{"iptables", "-C", "CODEX-AGENTS", "-s", "172.30.250.0/24", "-d", "172.30.250.2", "-p", "tcp", "--dport", "8080", "-j", "ACCEPT"},
		{"iptables", "-C", "CODEX-AGENTS", "-s", "172.30.250.2", "-d", "172.30.250.0/24", "-m", "conntrack", "--ctstate", "ESTABLISHED,RELATED", "-j", "ACCEPT"},
		{"iptables", "-C", "CODEX-AGENTS", "-s", "172.30.250.0/24", "-j", "DROP"},
	}
	for _, args := range checks {
		if e := r.hostCommand(ctx, args...).Run(); e != nil {
			return fmt.Errorf("agent firewall policy missing; run scripts/network-policy.sh")
		}
	}
	b, e := r.hostCommand(ctx, "sysctl", "-n", "net.bridge.bridge-nf-call-iptables").Output()
	if e != nil || strings.TrimSpace(string(b)) != "1" {
		return fmt.Errorf("bridge firewall must be enabled")
	}
	return nil
}
func (r *Runner) docker(ctx context.Context, input []byte, args ...string) ([]byte, error) {
	cmd := r.dockerCommand(ctx, args...)
	cmd.Stdin = bytes.NewReader(input)
	// Never propagate GitHub, Telegram, Trello or local Codex env into Docker CLI.
	cmd.Env = withoutEnv(os.Environ(), "GH_TOKEN", "GITHUB_TOKEN", "TRELLO_KEY", "TRELLO_TOKEN", "TELEGRAM_TOKEN", "OPENAI_API_KEY", "CODEX_API_KEY")
	b, e := cmd.Output()
	if e != nil {
		return nil, fmt.Errorf("Docker operation failed: %s", args[0])
	}
	return b, nil
}
func (r *Runner) Broker(ctx context.Context, j *Job, operation string) (broker.Result, error) {
	if r.C.Backend == "native-windows" {
		return r.nativeBroker(ctx, j, operation)
	}
	var out broker.Result
	if !safeID.MatchString(j.ID) {
		return out, fmt.Errorf("invalid job ID")
	}
	defer func() {
		stopCtx, stop := context.WithTimeout(context.Background(), 15*time.Second)
		defer stop()
		r.docker(stopCtx, nil, "rm", "-f", r.container(j.ID, "broker"))
	}()
	var token []byte
	var e error
	if r.C.ProjectMode != "local" {
		token, e = accountToken(ctx, r.C)
	}
	if e != nil {
		return out, fmt.Errorf("GitHub credential unavailable")
	}
	if operation == "publish" {
		if err := r.verifyPublicationCard(ctx, j); err != nil {
			return broker.Result{}, err
		}
	}
	req := broker.Request{Operation: operation, Repository: r.C.Repository, Base: r.C.BaseBranch, SHA: j.BaseSHA, Branch: j.Branch, Token: strings.TrimSpace(string(token)), Message: publicationTitle(j)}
	req.ExpectedHead = j.Head
	req.AdoptTaskHead = operation == "prepare" && r.C.AutoContinueTasks && systemCreatedTask(j) && !j.External && j.Head != ""
	if req.AdoptTaskHead {
		req.SHA = j.Head
		req.PR = j.PR
	}
	req.AllowWorkflowEdits = workflowApproved(j)
	if operation == "context" {
		req.Assets, e = r.API.TaskAssets(ctx, j)
		if e != nil {
			return out, e
		}
	}
	if operation == "review" || (operation == "prepare" && j.External) {
		req.PR = j.PR
	}
	if operation == "prepare" && j.External {
		req.SHA = j.Head
	}
	if operation == "review" {
		req.SHA = j.Head
		req.Message = j.Title + "\n" + j.Description
	}
	if e = r.localRequest(ctx, &req); e != nil {
		return out, e
	}
	input, e := json.Marshal(req)
	if e != nil {
		return out, e
	}
	mount := "type=volume,src=" + r.volume(j.ID, "workspace") + ",dst=/workspace"
	if operation != "prepare" {
		mount += ",readonly"
	}
	args := []string{"run", "--rm", "-i", "--name", r.container(j.ID, "broker"), "--label", "codex-orchestrator=" + r.namespace(), "--network", "codex-control", "--read-only", "--cap-drop=ALL", "--cap-add=CHOWN", "--cap-add=DAC_OVERRIDE", "--security-opt=no-new-privileges:true", "--pids-limit=128", "--memory=1g", "--cpus=1", "--tmpfs", "/tmp:rw,noexec,nosuid,size=128m", "--mount", mount, "--mount", "type=volume,src=" + r.volume(j.ID, "publish") + ",dst=/publish", "--mount", "type=volume,src=" + r.volume(j.ID, "review") + ",dst=/review-input", "codex-broker:0.1", "broker"}
	b, e := r.docker(ctx, input, args...)
	if e != nil {
		return out, e
	}
	e = json.Unmarshal(b, &out)
	if e == nil {
		e = r.acceptLocal(ctx, j, operation, &out)
	}
	return out, e
}
func (r *Runner) initialize(ctx context.Context, id, role string) error {
	b, e := os.ReadFile(r.C.AuthFile)
	if e != nil {
		return fmt.Errorf("Codex authentication unavailable")
	}
	// Only the selected ChatGPT credential is copied. No global config, MCPs, hooks or skills.
	var auth map[string]json.RawMessage
	if e = json.Unmarshal(b, &auth); e != nil {
		return e
	}
	var tokens map[string]string
	if json.Unmarshal(auth["tokens"], &tokens) != nil || len(tokens["access_token"]) == 0 {
		return fmt.Errorf("ChatGPT login required; API key authentication is disabled")
	}
	clean := map[string]json.RawMessage{}
	for _, k := range []string{"auth_mode", "tokens", "last_refresh"} {
		if v := auth[k]; len(v) > 0 {
			clean[k] = v
		}
	}
	b, e = json.Marshal(clean)
	if e != nil {
		return e
	}
	_, e = r.docker(ctx, b, "run", "--rm", "-i", "--label", "codex-orchestrator="+r.namespace(), "--network", "none", "--read-only", "--cap-drop=ALL", "--cap-add=CHOWN", "--cap-add=DAC_OVERRIDE", "--security-opt=no-new-privileges:true", "--mount", "type=volume,src="+r.volume(id, role+"-home")+",dst=/home/codex", "--tmpfs", "/tmp:rw,noexec,nosuid,size=16m", r.C.Image, "init-auth")
	return e
}
func (r *Runner) Run(j Job, role, prompt string) RunResult {
	if r.C.Backend == "native-windows" {
		return r.nativeRun(j, role, prompt)
	}
	result := RunResult{ID: j.ID, Role: role}
	ctx, cancel := context.WithTimeout(context.Background(), agentRunTimeout(r.C, role))
	defer cancel()
	if !safeID.MatchString(j.ID) {
		result.Err = fmt.Errorf("invalid job ID")
		return result
	}
	// Named volumes are created by Docker. All project files stay inside the Docker VM.
	for _, kind := range []string{"workspace", role + "-home"} {
		if _, e := r.docker(ctx, nil, "volume", "create", "--label", "codex-orchestrator="+r.namespace(), r.volume(j.ID, kind)); e != nil {
			result.Err = e
			return result
		}
	}
	if role != "manager" && j.BaseSHA == "" {
		p, e := r.Broker(ctx, &j, "prepare")
		if e != nil {
			result.Err = e
			return result
		}
		result.BaseSHA = p.SHA
	}
	if role == "reviewer" {
		review, e := r.Broker(ctx, &j, "review")
		if e != nil {
			result.Err = e
			return result
		}
		prompt += documentationReviewInstructions(review.Files)
	}
	if role != "manager" {
		if _, e := r.Broker(ctx, &j, "context"); e != nil {
			result.Err = e
			return result
		}
	}
	if e := r.initialize(ctx, j.ID, role); e != nil {
		result.Err = e
		return result
	}
	model, effort := r.C.WorkerModel, r.C.WorkerEffort
	if role == "reviewer" {
		model, effort = r.C.ReviewerModel, r.C.OtherEffort
	}
	if role == "manager" {
		model, effort = r.C.ManagerModel, r.C.OtherEffort
	}
	if role == "executor" && j.Fallback {
		model, effort = r.C.FallbackModel, r.C.FallbackEffort
	}
	name := r.container(j.ID, role)
	if e := r.networkPolicy(ctx); e != nil {
		result.Err = e
		return result
	}
	defer func() {
		stopCtx, stop := context.WithTimeout(context.Background(), 15*time.Second)
		defer stop()
		r.docker(stopCtx, nil, "rm", "-f", name)
	}()
	mount := "type=volume,src=" + r.volume(j.ID, "workspace") + ",dst=/workspace"
	if role != "executor" {
		mount += ",readonly"
	}
	args := []string{"run", "--rm", "-i", "--name", name, "--label", "codex-orchestrator=" + r.namespace(), "--network", r.C.Network, "--dns", "127.0.0.1", "--add-host", "proxy:172.30.250.2", "--read-only", "--user", "1000:1000", "--cap-drop=ALL", "--security-opt=no-new-privileges:true", "--pids-limit=256", "--memory=2g", "--cpus=2", "--tmpfs", "/tmp:rw,noexec,nosuid,size=256m", "--mount", mount, "--mount", "type=volume,src=" + r.volume(j.ID, role+"-home") + ",dst=/home/codex", "-e", "HTTPS_PROXY=" + r.C.Proxy, "-e", "HTTP_PROXY=" + r.C.Proxy, "-e", "ALL_PROXY=" + r.C.Proxy, "-e", "NO_PROXY=", "-e", "CODEX_HOME=/home/codex/.codex", r.C.Image, "run", model, effort, j.Session}
	if role != "manager" {
		idx := len(args) - 5
		extra := []string{"--mount", "type=volume,src=" + r.volume(j.ID, "review") + ",dst=/review-input,readonly"}
		args = append(args[:idx], append(extra, args[idx:]...)...)
	}
	// The image writes its final response to stdout behind a fixed marker; JSONL preserves
	// the exact thread ID. Questions pause this thread, not a shared/global conversation.
	cmd := r.dockerCommand(ctx, args...)
	cmd.Stdin = strings.NewReader(prompt)
	cmd.Env = withoutEnv(os.Environ(), "GH_TOKEN", "GITHUB_TOKEN", "TRELLO_KEY", "TRELLO_TOKEN", "TELEGRAM_TOKEN", "OPENAI_API_KEY", "CODEX_API_KEY")
	var stdout, stderr boundedOutput
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	e := cmd.Run()
	lines := strings.Split(string(stdout.First), "\n")
	for _, line := range lines {
		var event struct {
			Type   string `json:"type"`
			Thread string `json:"thread_id"`
		}
		if json.Unmarshal([]byte(line), &event) == nil && event.Type == "thread.started" {
			result.Session = event.Thread
		}
	}
	if result.Session == "" {
		result.Session = j.Session
	}
	if e != nil {
		diagnostic := strings.ToLower(stdout.String() + stderr.String())
		result.RateLimited = strings.Contains(diagnostic, "usage limit") || strings.Contains(diagnostic, "rate limit") || strings.Contains(diagnostic, "quota")
		result.ModelUnavailable = strings.Contains(diagnostic, "model_not_found") || strings.Contains(diagnostic, "model is not supported") || strings.Contains(diagnostic, "model does not exist")
		result.ModelBusy = modelBusy(diagnostic)
		result.Err = fmt.Errorf("agent execution failed (logs withheld to protect task data)")
		if ctx.Err() != nil {
			result.TimedOut = true
			result.Err = fmt.Errorf("agent execution timed out")
		}
		return result
	}
	marker := "ORCHESTRATOR_RESULT="
	at := strings.LastIndex(stdout.String(), marker)
	if at < 0 {
		result.Err = fmt.Errorf("agent returned no structured result")
		return result
	}
	if e = json.Unmarshal([]byte(strings.TrimSpace(stdout.String()[at+len(marker):])), &result.Verdict); e != nil {
		result.Err = fmt.Errorf("invalid agent result")
		return result
	}
	if e = validateVerdict(role, result.Verdict); e != nil {
		result.EmptyClarification = e.Error() == "empty clarification"
		result.Err = e
	}
	return result
}
func validateVerdict(role string, v Verdict) error {
	if v.Status == "question" || v.Status == "blocked" {
		if strings.TrimSpace(v.Question) == "" {
			return fmt.Errorf("empty clarification")
		}
		return nil
	}
	if role == "executor" && v.Status == "completed" && strings.TrimSpace(v.Summary) != "" {
		if strings.TrimSpace(v.CommitTitle) == "" || strings.ContainsAny(v.CommitTitle, "\r\n\x00") || len([]rune(v.CommitTitle)) > 60 || len(strings.Fields(v.CommitTitle)) > 5 {
			return fmt.Errorf("executor must provide a single-line commit_title of at most 5 words and 60 characters")
		}
		return nil
	}
	if role == "reviewer" && v.Status == "passed" && len(v.Findings) == 0 {
		return nil
	}
	if role == "reviewer" && v.Status == "changes" && len(v.Findings) > 0 {
		return nil
	}
	if role == "manager" && v.Status == "completed" {
		return nil
	}
	return fmt.Errorf("result violates role policy")
}
func (r *Runner) Preflight(ctx context.Context) error {
	if r.C.Backend == "native-windows" {
		return r.nativePreflight(ctx)
	}
	if e := r.networkPolicy(ctx); e != nil {
		return e
	}
	b, e := r.docker(ctx, nil, "network", "inspect", r.C.Network)
	if e != nil {
		return e
	}
	var n []struct {
		Internal bool `json:"Internal"`
	}
	if e = json.Unmarshal(b, &n); e != nil || len(n) != 1 || !n[0].Internal {
		return fmt.Errorf("agent network must be Docker internal")
	}
	for _, image := range []string{r.C.Image, "codex-broker:0.1"} {
		if _, e = r.docker(ctx, nil, "image", "inspect", image); e != nil {
			return e
		}
	}
	return nil
}
func (r *Runner) StopAbandoned(ctx context.Context) error {
	if r.C.Backend == "native-windows" {
		return nil
	} // Windows job objects kill the complete process tree on controller exit.
	b, e := r.docker(ctx, nil, "ps", "-aq", "--filter", "label=codex-orchestrator="+r.namespace())
	if e != nil {
		return e
	}
	for _, id := range strings.Fields(string(b)) {
		if _, e = r.docker(ctx, nil, "rm", "-f", id); e != nil {
			return e
		}
	}
	return nil
}
