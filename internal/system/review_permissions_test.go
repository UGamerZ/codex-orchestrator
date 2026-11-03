package system

import (
	"codex-orchestrator/internal/broker"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestReviewerRunsInOriginalWorkspaceWithScopedTestWrites(t *testing.T) {
	c := nativeFixture(t)
	c.TestWritePaths = []string{"files", "tests/_output", "tests/_support/_generated"}
	base := filepath.Join(c.NativeRoot, "jobs", "review-task")
	w := filepath.Join(base, "workspace")
	os.MkdirAll(w, 0700)
	if err := broker.PrepareReviewTestWrites(w, c.TestWritePaths); err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(base, "private", "reviewer-runtime")
	policy := strings.Join(nativePolicyArgs(c, w, home, "reviewer"), "\n")
	if !strings.Contains(policy, strconv.Quote(filepath.ToSlash(w))+`="read"`) {
		t.Fatal("source no longer read-only")
	}
	for _, rel := range c.TestWritePaths {
		if !strings.Contains(policy, strconv.Quote(filepath.ToSlash(filepath.Join(w, filepath.FromSlash(rel))))+`="write"`) {
			t.Fatal("test write directory missing", rel)
		}
	}
	if _, err := os.Stat(filepath.Join(base, "reviewer-tests")); !os.IsNotExist(err) {
		t.Fatal("second project copy created")
	}
}
