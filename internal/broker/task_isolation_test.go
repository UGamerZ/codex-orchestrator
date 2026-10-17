package broker

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestNewTaskStartsFromCleanBaseNotAnotherWorkspace(t *testing.T) {
	git := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if b, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, b)
		}
	}
	source := t.TempDir()
	git(source, "init", "-b", "dev")
	git(source, "config", "user.name", "Test")
	git(source, "config", "user.email", "test@example.com")
	os.WriteFile(filepath.Join(source, "app.txt"), []byte("clean base"), 0600)
	git(source, "add", ".")
	git(source, "commit", "-m", "Base")
	bundle := filepath.Join(t.TempDir(), "source.bundle")
	git(source, "bundle", "create", bundle, "--all")
	b, err := os.ReadFile(bundle)
	if err != nil {
		t.Fatal(err)
	}
	prepare := func(id string) Paths {
		t.Helper()
		root := t.TempDir()
		p := Paths{filepath.Join(root, "workspace"), filepath.Join(root, "publish"), filepath.Join(root, "review")}
		req := Request{Operation: "prepare", Local: true, Bundle: b, Base: "dev", Branch: "codex/" + id}
		var output bytes.Buffer
		if err := execute(context.Background(), req, p, &output); err != nil {
			t.Fatal(err)
		}
		return p
	}
	a := prepare("task-a")
	os.WriteFile(filepath.Join(a.Workspace, "app.txt"), []byte("unfinished task A"), 0600)
	os.WriteFile(filepath.Join(a.Workspace, "task-a-cache.json"), []byte("private task A"), 0600)
	next := prepare("task-b")
	content, err := os.ReadFile(filepath.Join(next.Workspace, "app.txt"))
	if err != nil || string(content) != "clean base" {
		t.Fatal("task B inherited unfinished task A", err)
	}
	if _, err = os.Stat(filepath.Join(next.Workspace, "task-a-cache.json")); !os.IsNotExist(err) {
		t.Fatal("task A cache leaked")
	}
	content, err = os.ReadFile(filepath.Join(a.Workspace, "app.txt"))
	if err != nil || string(content) != "unfinished task A" {
		t.Fatal("creating task B changed task A", err)
	}
}
