package broker

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestLocalCheckpointsPublishOnlyTheReviewedTree(t *testing.T) {
	git := func(dir string, args ...string) string {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-c", "core.autocrlf=false"}, args...)...)
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
	os.WriteFile(filepath.Join(source, "app.txt"), []byte("baseline"), 0600)
	git(source, "add", ".")
	git(source, "commit", "-m", "Baseline")
	base := git(source, "rev-parse", "HEAD")
	bundlePath := filepath.Join(t.TempDir(), "source.bundle")
	git(source, "bundle", "create", bundlePath, "--all")
	bundle, _ := os.ReadFile(bundlePath)
	root := t.TempDir()
	p := Paths{filepath.Join(root, "workspace"), filepath.Join(root, "publish"), filepath.Join(root, "review")}
	req := Request{Operation: "prepare", Local: true, Bundle: bundle, Base: "dev", Branch: "codex/task"}
	if _, err := Execute(context.Background(), req, p); err != nil {
		t.Fatal(err)
	}
	req.Operation, req.SHA, req.Message = "checkpoint", base, "Implement pending feature"
	var last Result
	for _, text := range []string{"first draft", "corrected draft"} {
		os.WriteFile(filepath.Join(p.Workspace, "app.txt"), []byte(text), 0600)
		var err error
		last, err = Execute(context.Background(), req, p)
		if err != nil {
			t.Fatal(err)
		}
		if !last.Changed || len(last.Bundle) != 0 {
			t.Fatal("unreviewed checkpoint was exported")
		}
		if _, err := os.Stat(filepath.Join(p.Publish, "last-head")); !os.IsNotExist(err) {
			t.Fatal("checkpoint changed the trusted publication head")
		}
		if parent := git(filepath.Join(p.Publish, "repo"), "rev-parse", last.SHA+"^"); parent != base {
			t.Fatal("fix checkpoint would publish earlier draft commits")
		}
	}
	review := req
	review.Operation, review.SHA, review.ReviewCheckpoint = "review", last.SHA, true
	if _, err := Execute(context.Background(), review, p); err != nil {
		t.Fatal("unpublished checkpoint cannot be reviewed", err)
	}
	req.Operation, req.ReviewedSHA = "publish", last.SHA
	os.WriteFile(filepath.Join(p.Workspace, "app.txt"), []byte("unreviewed edit"), 0600)
	if _, err := Execute(context.Background(), req, p); err == nil || !strings.Contains(err.Error(), "workspace changed after internal review") {
		t.Fatal("unreviewed tree reached publication", err)
	}
	os.WriteFile(filepath.Join(p.Workspace, "app.txt"), []byte("corrected draft"), 0600)
	result, err := Execute(context.Background(), req, p)
	if err != nil || result.SHA != last.SHA || len(result.Bundle) == 0 {
		t.Fatal("exact approved commit was not exported", err)
	}
	if count := git(filepath.Join(p.Publish, "repo"), "rev-list", "--count", base+".."+result.SHA); count != "1" {
		t.Fatal("intermediate fix commits reached published history", count)
	}
	repeated, err := Execute(context.Background(), req, p)
	if err != nil || repeated.SHA != result.SHA {
		t.Fatal("retry created another commit or rejected the exact reviewed publication", err)
	}
}
