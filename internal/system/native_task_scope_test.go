package system

import (
	"codex-orchestrator/internal/broker"
	"os"
	"path/filepath"
	"testing"
)

func TestNativeScopeClosesPreviouslyProvisionedTasksAndOtherEnvironments(t *testing.T) {
	base := t.TempDir()
	active := filepath.Join(base, "environments", "active")
	workspace := filepath.Join(active, "jobs", "a", "workspace")
	home := filepath.Join(active, "jobs", "a", "private", "executor-runtime")
	other := filepath.Join(base, "environments", "other", "jobs", "b")
	legacy := filepath.Join(base, "jobs", "old")
	for _, root := range []string{filepath.Dir(workspace), other, legacy} {
		for _, rel := range []string{"workspace", filepath.Join("workspace", broker.ReviewDirectory), "private", filepath.Join("private", "executor-runtime", "scratch"), filepath.Join("private", "executor-runtime", "codex")} {
			if err := os.MkdirAll(filepath.Join(root, rel), 0700); err != nil {
				t.Fatal(err)
			}
		}
	}
	scope, err := nativeTaskScopePaths(Config{NativeRoot: active}, workspace, home)
	if err != nil {
		t.Fatal(err)
	}
	denied := map[string]bool{}
	for _, path := range scope.Deny {
		denied[path] = true
	}
	for _, path := range scope.Allow {
		if denied[path] {
			t.Fatalf("active path denied: %s", path)
		}
	}
	for _, root := range []string{other, legacy} {
		for _, rel := range []string{"workspace", filepath.Join("workspace", broker.ReviewDirectory), "private", filepath.Join("private", "executor-runtime"), filepath.Join("private", "executor-runtime", "scratch"), filepath.Join("private", "executor-runtime", "codex")} {
			if !denied[filepath.Join(root, rel)] {
				t.Fatalf("peer grant left open: %s", filepath.Join(root, rel))
			}
		}
	}
}

func TestNativeSchedulerWaitsForAnyActiveRole(t *testing.T) {
	for _, role := range []string{"executor", "reviewer", "manager", "publisher"} {
		t.Run(role, func(t *testing.T) {
			c := fixture(t)
			c.C.Backend = "native-windows"
			stage := "running"
			if role == "publisher" {
				stage = "publishing"
			}
			c.State.Jobs["active"] = &Job{ID: "active", Role: role, Stage: stage}
			c.State.Jobs["waiting"] = &Job{ID: "waiting", Stage: "review"}
			if err := c.schedule(); err != nil {
				t.Fatal(err)
			}
			if c.State.Jobs["waiting"].Stage != "review" {
				t.Fatal("native agent overlapped another active role")
			}
		})
	}
}
