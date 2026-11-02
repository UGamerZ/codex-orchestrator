package system

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestNativePolicyKeepsSiblingTaskRootsTraversable(t *testing.T) {
	c := nativeFixture(t)
	peer := filepath.Join(c.NativeRoot, "jobs", "peer-task")
	workspace := filepath.Join(c.NativeRoot, "jobs", "current-task", "workspace")
	home := filepath.Join(c.NativeRoot, "jobs", "current-task", "private", "executor-runtime")
	for _, dir := range []string{peer, workspace, home} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	policy := strings.Join(nativePolicyArgs(c, workspace, home, "executor"), "\n")
	deny := func(path string) string { return strconv.Quote(filepath.ToSlash(path)) + `="deny"` }
	if strings.Contains(policy, deny(peer)) {
		t.Fatal("shared deny ACL would break a parallel task's directory access")
	}
	if !strings.Contains(policy, deny(filepath.Join(peer, "private"))) {
		t.Fatal("sibling private state must remain protected")
	}
	if !strings.Contains(policy, deny(c.AuthFile)) {
		t.Fatal("authentication must remain protected")
	}
}
