package system

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"codex-orchestrator/internal/broker"
)

func localGit(ctx context.Context, c Config, args ...string) ([]byte, error) {
	hooks := filepath.Join(c.StateDir, "disabled-hooks")
	if e := os.MkdirAll(hooks, 0700); e != nil {
		return nil, e
	}
	cmd := exec.CommandContext(ctx, "git", append([]string{"--git-dir=" + c.LocalRepository, "-c", "core.hooksPath=" + hooks, "-c", "credential.helper=", "-c", "protocol.ext.allow=never", "-c", "protocol.file.allow=always"}, args...)...)
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "SystemRoot=" + os.Getenv("SystemRoot"), "TEMP=" + os.Getenv("TEMP"), "TMP=" + os.Getenv("TMP"), "GIT_TERMINAL_PROMPT=0", "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=" + os.DevNull}
	b, e := cmd.Output()
	if e != nil {
		return nil, fmt.Errorf("local Git operation failed: %s", args[0])
	}
	return b, nil
}
func (r *Runner) localRequest(ctx context.Context, req *broker.Request) error {
	if r.C.ProjectMode != "local" {
		return nil
	}
	if e := os.MkdirAll(r.C.StateDir, 0700); e != nil {
		return e
	}
	f, e := os.CreateTemp(r.C.StateDir, "source-*.bundle")
	if e != nil {
		return e
	}
	name := f.Name()
	f.Close()
	defer os.Remove(name)
	if _, e = localGit(ctx, r.C, "bundle", "create", name, "--branches"); e != nil {
		return e
	}
	info, e := os.Stat(name)
	if e != nil {
		return e
	}
	if info.Size() > 128<<20 {
		return fmt.Errorf("local bundle exceeds 128 MiB limit")
	}
	req.Bundle, e = os.ReadFile(name)
	req.Local = true
	return e
}
func (r *Runner) acceptLocal(ctx context.Context, j *Job, operation string, result *broker.Result) error {
	if r.C.ProjectMode != "local" || operation != "publish" || len(result.Bundle) == 0 {
		return nil
	}
	if !safeID.MatchString(j.ID) || j.Branch != "codex/"+j.ID {
		return fmt.Errorf("invalid local task branch")
	}
	f, e := os.CreateTemp(r.C.StateDir, "result-*.bundle")
	if e != nil {
		return e
	}
	name := f.Name()
	defer os.Remove(name)
	_, e = f.Write(result.Bundle)
	f.Close()
	if e != nil {
		return e
	}
	result.Bundle = nil
	oldBytes, _ := localGit(ctx, r.C, "rev-parse", "--verify", "refs/heads/"+j.Branch)
	old := strings.TrimSpace(string(oldBytes))
	expected := j.Head
	if old != "" && old != expected && old != result.SHA {
		return fmt.Errorf("local branch changed outside system")
	}
	importRef := "refs/orchestrator/import/" + j.ID
	if _, e = localGit(ctx, r.C, "fetch", "--no-tags", name, "refs/heads/"+j.Branch+":"+importRef); e != nil {
		return e
	}
	defer localGit(ctx, r.C, "update-ref", "-d", importRef)
	head, e := localGit(ctx, r.C, "rev-parse", importRef)
	if e != nil {
		return e
	}
	if strings.TrimSpace(string(head)) != result.SHA {
		return fmt.Errorf("local publication SHA mismatch")
	}
	// Import objects through a unique ref; FETCH_HEAD is not used for the update.
	if old == "" {
		old = strings.Repeat("0", 40)
	}
	_, e = localGit(ctx, r.C, "update-ref", "refs/heads/"+j.Branch, result.SHA, old)
	return e
}
func (a *API) reviewDir() string { return filepath.Join(a.C.StateDir, "reviews") }
func (a *API) createLocalReview(ctx context.Context, j *Job) (int, string, error) {
	if !safeID.MatchString(j.ID) {
		return 0, "", fmt.Errorf("invalid local review ID")
	}
	hash := sha256.Sum256([]byte(j.ID))
	id := int(binary.BigEndian.Uint32(hash[:4]) & 0x7fffffff)
	if id == 0 {
		id = 1
	}
	if e := os.MkdirAll(a.reviewDir(), 0700); e != nil {
		return 0, "", e
	}
	path := filepath.Join(a.reviewDir(), strconv.Itoa(id)+".json")
	var p Pull
	if b, e := os.ReadFile(path); e == nil {
		if json.Unmarshal(b, &p) != nil || p.Head.Ref != j.Branch {
			return 0, "", fmt.Errorf("local review identity collision")
		}
		return id, path, nil
	}
	p.Number = id
	p.URL = path
	p.Title = j.Title
	p.Body = j.Description
	p.Head.Ref = j.Branch
	p.Head.SHA = j.Head
	p.Head.Repo.FullName = a.C.Repository
	p.Base.Ref = a.C.BaseBranch
	p.Base.SHA = j.BaseSHA
	b, _ := json.MarshalIndent(p, "", "  ")
	e := os.WriteFile(path, b, 0600)
	return id, path, e
}
func (a *API) localReviews(ctx context.Context) ([]Pull, error) {
	files, e := filepath.Glob(filepath.Join(a.reviewDir(), "*.json"))
	if e != nil {
		return nil, e
	}
	out := []Pull{}
	for _, path := range files {
		b, e := os.ReadFile(path)
		if e != nil {
			return nil, e
		}
		var p Pull
		if e = json.Unmarshal(b, &p); e != nil {
			return nil, e
		}
		head, e := localGit(ctx, a.C, "rev-parse", "--verify", "refs/heads/"+p.Head.Ref)
		if e != nil {
			continue
		}
		p.Head.SHA = strings.TrimSpace(string(head))
		out = append(out, p)
	}
	return out, nil
}
func (a *API) localComment(id int, body string) error {
	path := filepath.Join(a.reviewDir(), strconv.Itoa(id)+".md")
	f, e := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0600)
	if e != nil {
		return e
	}
	defer f.Close()
	_, e = f.WriteString(body + "\n\n---\n\n")
	return e
}
