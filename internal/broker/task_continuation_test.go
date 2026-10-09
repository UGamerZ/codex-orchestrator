package broker

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestAdoptThenPublishPreservesManualCommit(t *testing.T) {
	git := func(dir string, args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
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
	os.WriteFile(filepath.Join(source, "app.txt"), []byte("base"), 0600)
	git(source, "add", ".")
	git(source, "commit", "-m", "Base")
	git(source, "checkout", "-b", "codex/test")
	old := git(source, "rev-parse", "HEAD")
	os.WriteFile(filepath.Join(source, "manual.txt"), []byte("preserve manual change"), 0600)
	git(source, "add", ".")
	git(source, "commit", "-m", "Manual change")
	head := git(source, "rev-parse", "HEAD")
	bundle := filepath.Join(t.TempDir(), "source.bundle")
	git(source, "bundle", "create", bundle, "--all")
	b, _ := os.ReadFile(bundle)
	root := t.TempDir()
	p := Paths{filepath.Join(root, "workspace"), filepath.Join(root, "publish"), filepath.Join(root, "review")}
	os.MkdirAll(p.Publish, 0700)
	os.WriteFile(filepath.Join(p.Publish, "last-head"), []byte(old), 0600)
	req := Request{Operation: "prepare", Local: true, Bundle: b, Base: "dev", Branch: "codex/test", SHA: head, ExpectedHead: head, AdoptTaskHead: true}
	var output bytes.Buffer
	if err := execute(context.Background(), req, p, &output); err != nil {
		t.Fatal(err)
	}
	marker, _ := os.ReadFile(filepath.Join(p.Publish, "last-head"))
	if string(marker) != head {
		t.Fatal("old publisher parent retained")
	}
	manual, _ := os.ReadFile(filepath.Join(p.Workspace, "manual.txt"))
	if string(manual) != "preserve manual change" {
		t.Fatal("manual change lost")
	}
	os.WriteFile(filepath.Join(p.Workspace, "app.txt"), []byte("task correction"), 0600)
	req.Operation = "publish"
	req.AdoptTaskHead = false
	req.Message = "Correct task behavior"
	output.Reset()
	if err := execute(context.Background(), req, p, &output); err != nil {
		t.Fatal(err)
	}
	var result Result
	if err := json.Unmarshal(output.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	parent := git(filepath.Join(p.Publish, "repo"), "rev-parse", result.SHA+"^")
	if parent != head {
		t.Fatal("published on stale parent", parent, head)
	}
	if got := git(filepath.Join(p.Publish, "repo"), "show", result.SHA+":manual.txt"); got != "preserve manual change" {
		t.Fatal("manual content overwritten")
	}
}
