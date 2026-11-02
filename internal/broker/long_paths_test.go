package broker

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrepareAndPublishWithUnrelatedLongDependencyPath(t *testing.T) {
	git := func(dir string, args ...string) string {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-c", "core.longpaths=true"}, args...)...)
		cmd.Dir = dir
		b, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v %s", args, err, b)
		}
		return strings.TrimSpace(string(b))
	}
	source := t.TempDir()
	git(source, "init", "-b", "dev")
	git(source, "config", "user.name", "Test")
	git(source, "config", "user.email", "test@example.com")
	dependency := filepath.Join("vendor", strings.Repeat("a", 70), strings.Repeat("b", 70), strings.Repeat("c", 70), strings.Repeat("d", 70), "LibraryContext.php")
	dependencyFile := filepath.Join(source, dependency)
	if len(dependencyFile) <= 260 {
		t.Fatal("fixture does not exceed Windows legacy path limit")
	}
	if err := os.MkdirAll(filepath.Dir(dependencyFile), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dependencyFile, []byte("unchanged dependency\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "app.txt"), []byte("before\n"), 0600); err != nil {
		t.Fatal(err)
	}
	git(source, "add", "--all")
	git(source, "commit", "-m", "Base")
	bundleFile := filepath.Join(t.TempDir(), "source.bundle")
	git(source, "bundle", "create", bundleFile, "--all")
	bundle, err := os.ReadFile(bundleFile)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	p := Paths{filepath.Join(root, "workspace"), filepath.Join(root, "publish"), filepath.Join(root, "review")}
	req := Request{Operation: "prepare", Local: true, Bundle: bundle, Base: "dev", Branch: "codex/long-path-test"}
	baseline, err := Execute(context.Background(), req, p)
	if err != nil {
		t.Fatal(err)
	}
	// A normal executor Git command must work without per-command overrides.
	cmd := exec.Command("git", "config", "--local", "--get", "core.longpaths")
	cmd.Dir = p.Workspace
	if b, err := cmd.Output(); err != nil || strings.TrimSpace(string(b)) != "true" {
		t.Fatalf("workspace long paths not enabled: %v %s", err, b)
	}
	if err := os.WriteFile(filepath.Join(p.Workspace, "app.txt"), []byte("after\n"), 0600); err != nil {
		t.Fatal(err)
	}
	req.Operation = "publish"
	req.SHA = baseline.SHA
	req.Message = "Update application behavior"
	publication, err := Execute(context.Background(), req, p)
	if err != nil {
		t.Fatal(err)
	}
	repo := filepath.Join(p.Publish, "repo")
	if files := git(repo, "diff", "--name-only", baseline.SHA, publication.SHA); files != "app.txt" {
		t.Fatalf("unrelated dependency changed: %s", files)
	}
	if content := git(repo, "show", publication.SHA+":"+filepath.ToSlash(dependency)); content != "unchanged dependency" {
		t.Fatal("dependency not preserved")
	}
}
