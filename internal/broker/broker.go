// Package broker runs only in the trusted controller or Git service container.
// It never executes project code or a command supplied by an agent.
package broker

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
)

type Request struct {
	Operation  string
	Repository string
	Base       string
	SHA        string
	Branch     string
	Token      string
	Message    string
	PR         int
}
type Result struct {
	SHA     string `json:"sha"`
	Changed bool   `json:"changed"`
}
type Paths struct{ Workspace, Publish, Review string }

func Execute(ctx context.Context, r Request, p Paths) (Result, error) {
	var out Result
	var b bytes.Buffer
	e := execute(ctx, r, p, &b)
	if e != nil {
		return out, e
	}
	e = json.Unmarshal(b.Bytes(), &out)
	return out, e
}

func Run() error {
	var r Request
	if e := json.NewDecoder(io.LimitReader(os.Stdin, 2<<20)).Decode(&r); e != nil {
		return e
	}
	return execute(context.Background(), r, Paths{"/workspace", "/publish", "/review-input"}, os.Stdout)
}
func execute(parentContext context.Context, r Request, p Paths, output io.Writer) error {
	for _, dir := range []string{p.Workspace, p.Publish, p.Review, filepath.Join(p.Publish, "tmp"), filepath.Join(p.Publish, "disabled-hooks")} {
		if !filepath.IsAbs(dir) || filepath.Clean(dir) == filepath.VolumeName(dir)+string(filepath.Separator) {
			return fmt.Errorf("broker needs dedicated absolute directories")
		}
		if info, e := os.Lstat(dir); e == nil && info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("broker root is a reparse point")
		}
		if e := os.MkdirAll(dir, 0700); e != nil {
			return e
		}
	}
	if r.Operation != "prepare" && r.Operation != "publish" && r.Operation != "review" {
		return fmt.Errorf("operation forbidden")
	}
	if r.Branch == r.Base || r.PR < 0 {
		return fmt.Errorf("base branch publication forbidden")
	}
	if !regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`).MatchString(r.Repository) {
		return fmt.Errorf("invalid repository")
	}
	if !regexp.MustCompile(`^[a-f0-9]{40}$`).MatchString(r.SHA) && r.SHA != "" {
		return fmt.Errorf("invalid SHA")
	}
	if !regexp.MustCompile(`^codex/[a-z0-9-]+$`).MatchString(r.Branch) {
		return fmt.Errorf("only new codex/ branches allowed")
	}
	ctx, cancel := context.WithTimeout(parentContext, 10*time.Minute)
	defer cancel()
	// Credential and hook settings override all repository configuration.
	git := func(dir string, args ...string) ([]byte, error) {
		cmd := exec.CommandContext(ctx, "git", append([]string{"-c", "core.hooksPath=" + filepath.Join(p.Publish, "disabled-hooks"), "-c", "core.autocrlf=false", "-c", "credential.helper=", "-c", "protocol.file.allow=never", "-c", "protocol.ext.allow=never"}, args...)...)
		cmd.Dir = dir
		cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "SystemRoot=" + os.Getenv("SystemRoot"), "HOME=" + p.Publish, "TEMP=" + filepath.Join(p.Publish, "tmp"), "TMP=" + filepath.Join(p.Publish, "tmp"), "GIT_TERMINAL_PROMPT=0", "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=" + os.DevNull, "GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=http.https://github.com/.extraHeader", "GIT_CONFIG_VALUE_0=Authorization: Basic " + base64.StdEncoding.EncodeToString([]byte("x-access-token:"+r.Token))}
		b, e := cmd.Output()
		if e != nil {
			return nil, fmt.Errorf("trusted Git operation failed: %s", args[0])
		}
		return b, nil
	}
	if e := os.MkdirAll(p.Publish, 0700); e != nil {
		return e
	}
	if _, e := os.Stat(filepath.Join(p.Publish, "repo", ".git")); os.IsNotExist(e) {
		if _, e = git(p.Publish, "clone", "--no-checkout", "--no-tags", "--single-branch", "--branch", r.Base, "https://github.com/"+r.Repository+".git", "repo"); e != nil {
			return e
		}
	}
	dir := filepath.Join(p.Publish, "repo")
	fetchHead := func() error {
		ref := r.SHA
		if r.PR > 0 {
			ref = fmt.Sprintf("refs/pull/%d/head", r.PR)
		}
		if _, e := git(dir, "fetch", "--no-tags", "origin", ref); e != nil {
			return e
		}
		b, e := git(dir, "rev-parse", "FETCH_HEAD")
		if e != nil {
			return e
		}
		if strings.TrimSpace(string(b)) != r.SHA {
			return fmt.Errorf("PR head changed during preparation")
		}
		return nil
	}
	if _, e := git(dir, "fetch", "--no-tags", "origin", r.Base); e != nil {
		return e
	}
	if r.Operation == "review" {
		if r.SHA == "" {
			return fmt.Errorf("review SHA required")
		}
		if e := fetchHead(); e != nil {
			return e
		}
		b, e := git(dir, "diff", "--no-ext-diff", "--no-textconv", "origin/"+r.Base+"..."+r.SHA, "--")
		if e != nil {
			return e
		}
		if e = os.MkdirAll(p.Review, 0755); e != nil {
			return e
		}
		if e = os.WriteFile(filepath.Join(p.Review, "diff.patch"), b, 0644); e != nil {
			return e
		}
		if e = os.WriteFile(filepath.Join(p.Review, "description.txt"), []byte(r.Message), 0644); e != nil {
			return e
		}
		return json.NewEncoder(output).Encode(Result{SHA: r.SHA})
	}
	if r.Operation == "prepare" {
		ref := r.SHA
		if ref == "" {
			ref = "origin/" + r.Base
		}
		if r.SHA != "" {
			if e := fetchHead(); e != nil {
				return e
			}
		}
		if _, e := git(dir, "checkout", "--detach", ref); e != nil {
			return e
		}
		b, e := git(dir, "rev-parse", "HEAD")
		if e != nil {
			return e
		}
		sha := strings.TrimSpace(string(b))
		// git archive exposes tracked content, never Git credentials or controller files.
		b, e = git(dir, "archive", "HEAD")
		if e != nil {
			return e
		}
		entries, e := os.ReadDir(p.Workspace)
		if e != nil {
			return e
		}
		for _, ent := range entries {
			if e = os.RemoveAll(filepath.Join(p.Workspace, ent.Name())); e != nil {
				return e
			}
		}
		if e = extract(b, p.Workspace); e != nil {
			return e
		}
		if _, e = git(p.Workspace, "init"); e != nil {
			return e
		}
		if _, e = git(p.Workspace, "checkout", "-b", r.Branch); e != nil {
			return e
		}
		if _, e = git(p.Workspace, "add", "--all"); e != nil {
			return e
		}
		if _, e = git(p.Workspace, "-c", "user.name=Codex Baseline", "-c", "user.email=codex-baseline@users.noreply.github.com", "commit", "--allow-empty", "-m", "Local task baseline"); e != nil {
			return e
		}
		if runtime.GOOS != "windows" {
			if e = os.Chown(p.Workspace, 1000, 1000); e != nil {
				return e
			}
			if e = filepath.WalkDir(p.Workspace, func(p string, d os.DirEntry, e error) error {
				if e != nil {
					return e
				}
				return os.Lchown(p, 1000, 1000)
			}); e != nil {
				return e
			}
		}
		return json.NewEncoder(output).Encode(Result{SHA: sha})
	}
	if r.Operation != "publish" {
		return fmt.Errorf("operation forbidden")
	}
	if r.SHA == "" {
		return fmt.Errorf("baseline SHA required")
	}
	if _, e := git(dir, "fetch", "--no-tags", "origin", r.SHA); e != nil {
		return e
	}
	prior, _ := os.ReadFile(filepath.Join(p.Publish, "last-head"))
	parent := r.SHA
	if len(prior) > 0 {
		parent = strings.TrimSpace(string(prior))
		if !regexp.MustCompile(`^[a-f0-9]{40}$`).MatchString(parent) {
			return fmt.Errorf("invalid trusted publication head")
		}
	}
	if _, e := git(dir, "checkout", "-B", r.Branch, parent); e != nil {
		return e
	}
	// The trusted publisher copies the tree into its own repository. Agent .git is ignored.
	entries, e := os.ReadDir(dir)
	if e != nil {
		return e
	}
	for _, ent := range entries {
		if ent.Name() != ".git" {
			if e = os.RemoveAll(filepath.Join(dir, ent.Name())); e != nil {
				return e
			}
		}
	}
	if e = copyTree(p.Workspace, dir); e != nil {
		return e
	}
	if _, e = git(dir, "add", "--all"); e != nil {
		return e
	}
	b, e := git(dir, "diff", "--cached", "--name-only")
	if e != nil {
		return e
	}
	if len(b) == 0 {
		return json.NewEncoder(output).Encode(Result{SHA: parent, Changed: false})
	}
	// Workflow edits need a separately scoped permission; do not publish them silently.
	for _, p := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(p, ".github/workflows/") {
			return fmt.Errorf("workflow edit needs explicit human permission")
		}
	}
	if _, e = git(dir, "-c", "user.name=Codex Executor", "-c", "user.email=codex-executor@users.noreply.github.com", "commit", "-m", r.Message); e != nil {
		return e
	}
	// Only this exact task branch can be written. A fast-forward push preserves human updates.
	remote, e := git(dir, "ls-remote", "origin", "refs/heads/"+r.Branch)
	if e != nil {
		return e
	}
	old := ""
	if fields := strings.Fields(string(remote)); len(fields) > 0 {
		old = fields[0]
	}
	// Expected publication head is stored only in the trusted publisher volume.
	if old != "" && old != strings.TrimSpace(string(prior)) {
		return fmt.Errorf("branch changed outside system; human confirmation required")
	}
	if _, e = git(dir, "push", "origin", "HEAD:refs/heads/"+r.Branch); e != nil {
		return e
	}
	b, e = git(dir, "rev-parse", "HEAD")
	if e != nil {
		return e
	}
	sha := strings.TrimSpace(string(b))
	if e = os.WriteFile(filepath.Join(p.Publish, "last-head"), []byte(sha), 0600); e != nil {
		return e
	}
	return json.NewEncoder(output).Encode(Result{SHA: sha, Changed: true})
}
func extract(b []byte, root string) error {
	tr := tar.NewReader(strings.NewReader(string(b)))
	for {
		h, e := tr.Next()
		if e == io.EOF {
			return nil
		}
		if e != nil {
			return e
		}
		p := path.Clean(h.Name)
		if path.IsAbs(p) || p == ".." || strings.HasPrefix(p, "../") || strings.ContainsAny(h.Name, "\\:") {
			return fmt.Errorf("unsafe archive path")
		}
		dest := filepath.Join(root, filepath.FromSlash(p))
		switch h.Typeflag {
		case tar.TypeDir:
			if e = os.MkdirAll(dest, 0755); e != nil {
				return e
			}
		case tar.TypeReg:
			if e = os.MkdirAll(filepath.Dir(dest), 0755); e != nil {
				return e
			}
			f, e := os.OpenFile(dest, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, os.FileMode(h.Mode)&0777)
			if e != nil {
				return e
			}
			_, e = io.Copy(f, tr)
			ce := f.Close()
			if e != nil {
				return e
			}
			if ce != nil {
				return ce
			}
		default:
			return fmt.Errorf("unsupported archive entry %s (symlinks require explicit preparation)", h.Name)
		}
	}
}
func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(p string, d os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		rel, e := filepath.Rel(src, p)
		if e != nil {
			return e
		}
		if rel == "." {
			return nil
		}
		if rel == ".git" || strings.HasPrefix(rel, ".git/") {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlink publication requires human preparation: %s", rel)
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0755)
		}
		info, e := d.Info()
		if e != nil {
			return e
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("nonregular file forbidden")
		}
		in, e := os.Open(p)
		if e != nil {
			return e
		}
		defer in.Close()
		out, e := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, info.Mode().Perm())
		if e != nil {
			return e
		}
		_, e = io.Copy(out, in)
		ce := out.Close()
		if e != nil {
			return e
		}
		return ce
	})
}
