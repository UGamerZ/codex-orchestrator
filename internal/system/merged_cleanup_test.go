package system

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func mergedCleanupFiles(t *testing.T, root, id string) string {
	t.Helper()
	base := filepath.Join(root, "jobs", id)
	for _, rel := range []string{"workspace/app.txt", "private/publish/repo/.git/objects/pack/test.pack", "private/executor-runtime/codex/sessions/history.jsonl", "private/executor-runtime/codex/.sandbox-bin/codex.exe", "review-input/diff.patch"} {
		file := filepath.Join(base, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(file), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, []byte("keep until merged"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	return base
}

func TestCleanupOnlyConfirmedMergedPublishedCopies(t *testing.T) {
	c := fixture(t)
	c.C.Backend = "native-windows"
	c.C.NativeRoot = t.TempDir()
	c.API.C = c.C
	c.API.ghTransport = func(_ context.Context, args ...string) ([]byte, error) {
		if !strings.Contains(strings.Join(args, " "), "state=closed") {
			t.Fatal("wrong GitHub request", args)
		}
		return []byte(`[[{"number":7,"merged_at":"2026-11-01T00:00:00Z","head":{"sha":"abc"}},{"number":8,"merged_at":null,"head":{"sha":"abc"}},{"number":9,"merged_at":"2026-11-01T00:00:00Z","head":{"sha":"different"}},{"number":10,"merged_at":"2026-11-01T00:00:00Z","head":{"sha":"abc"}}]]`), nil
	}
	for id, j := range map[string]*Job{
		"merged":   {PR: 7, Head: "abc", Stage: "completed", BoardCompleted: true},
		"unmerged": {PR: 8, Head: "abc", Stage: "closed"},
		"changed":  {PR: 9, Head: "abc", Stage: "completed"},
		"active":   {PR: 10, Head: "abc", Stage: "running"},
		"waiting":  {PR: 10, Head: "abc", Stage: "waiting"},
	} {
		j.ID = id
		c.State.Jobs[id] = j
		mergedCleanupFiles(t, c.C.NativeRoot, id)
	}
	outside := t.TempDir()
	marker := filepath.Join(outside, "user-data.txt")
	if err := os.WriteFile(marker, []byte("outside"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(c.C.NativeRoot, "jobs", "merged", "workspace", "external-link")); err != nil {
		t.Fatal(err)
	}
	if err := c.cleanupMergedCopies(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{"workspace", "private/publish", "review-input", "private/executor-runtime/codex/.sandbox-bin"} {
		if _, err := os.Stat(filepath.Join(c.C.NativeRoot, "jobs", "merged", rel)); !os.IsNotExist(err) {
			t.Fatal("merged copy retained", rel, err)
		}
	}
	for _, id := range []string{"unmerged", "changed", "active", "waiting"} {
		if _, err := os.Stat(filepath.Join(c.C.NativeRoot, "jobs", id, "workspace", "app.txt")); err != nil {
			t.Fatalf("unmerged/active task deleted: %s %v", id, err)
		}
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatal("followed link outside task", err)
	}
	if _, err := os.Stat(filepath.Join(c.C.NativeRoot, "jobs", "merged", "private", "executor-runtime", "codex", "sessions", "history.jsonl")); err != nil {
		t.Fatal("agent logs lost", err)
	}
	if c.State.Jobs["merged"].MergedCopiesCleanedAt.IsZero() {
		t.Fatal("cleanup not recorded")
	}
	saved, err := LoadState(c.C.StateDir)
	if err != nil || saved.Jobs["merged"].MergedCopiesCleanedAt.IsZero() {
		t.Fatal("cleanup record not persistent", err)
	}
}

func TestMergedCleanupRejectsEscapingOrLinkedRoots(t *testing.T) {
	root := t.TempDir()
	r := &Runner{C: Config{Backend: "native-windows", NativeRoot: root}}
	if err := r.removeMergedCopies(context.Background(), &Job{ID: "../outside"}); err == nil {
		t.Fatal("traversal accepted")
	}
	base := filepath.Join(root, "jobs", "task")
	if err := os.MkdirAll(base, 0700); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	if err := os.MkdirAll(filepath.Join(outside, "publish"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(base, "private")); err != nil {
		t.Fatal(err)
	}
	if err := r.removeMergedCopies(context.Background(), &Job{ID: "task"}); err == nil {
		t.Fatal("linked ancestor accepted")
	}
	if _, err := os.Stat(filepath.Join(outside, "publish")); err != nil {
		t.Fatal("outside directory removed", err)
	}
}

func TestMergedCleanupNeverReopensFromStaleOpenPR(t *testing.T) {
	c := fixture(t)
	j := &Job{ID: "merged", PR: 7, Head: "abc", Stage: "closed", ClosedPR: true, MergedCopiesCleanedAt: time.Now()}
	c.State.Jobs[j.ID] = j
	fakePull(c.API, "abc")
	if err := c.discoverPRs(context.Background()); err != nil {
		t.Fatal(err)
	}
	if j.Stage != "closed" || !j.ClosedPR {
		t.Fatal("merged cleanup task reopened")
	}
}

func TestCompletedCardCleanupKeepsAllManagedLists(t *testing.T) {
	for _, list := range []string{"todo", "progress", "done", "merged", "archived"} {
		t.Run(list, func(t *testing.T) {
			c := fixture(t)
			c.C.Backend = "native-windows"
			c.C.NativeRoot = t.TempDir()
			c.C.Board = "board"
			c.C.Todo = "todo"
			c.C.Progress = "progress"
			c.C.Done = "done"
			c.API.C = c.C
			j := &Job{ID: "task", Card: "card", Stage: "waiting", Head: "abc", PR: 7, Question: "old blocker"}
			c.State.Jobs[j.ID] = j
			mergedCleanupFiles(t, c.C.NativeRoot, j.ID)
			c.API.Client.Transport = roundTrip(func(*http.Request) (*http.Response, error) {
				return response(`{"id":"card","idList":"` + list + `","closed":` + map[bool]string{false: "false", true: "true"}[list == "archived"] + `}`), nil
			})
			c.API.ghTransport = func(context.Context, ...string) ([]byte, error) {
				t.Fatal("Trello completion does not need GitHub merge")
				return nil, nil
			}
			if err := c.cleanupMergedCopies(context.Background()); err != nil {
				t.Fatal(err)
			}
			completed := list == "merged" || list == "archived"
			if completed != !j.CompletedCopiesCleanedAt.IsZero() {
				t.Fatal("wrong completion policy", list)
			}
			_, err := os.Stat(filepath.Join(c.C.NativeRoot, "jobs", j.ID, "workspace"))
			if completed && !os.IsNotExist(err) {
				t.Fatal("completed workspace remains")
			}
			if !completed && err != nil {
				t.Fatal("managed workspace deleted")
			}
			if completed && (j.Stage != "completed" || !j.BoardCompleted || j.ClosedPR) {
				t.Fatal("cleanup altered PR ownership/status")
			}
		})
	}
}

func TestFeedbackRestoresPreparationAfterCompletedCleanup(t *testing.T) {
	c, j := feedbackBoardFixture(t, "done", false, "open")
	j.BaseSHA = "old-base"
	j.CompletedCopiesCleanedAt = time.Now()
	if _, _, err := c.applyHumanFeedback(context.Background(), j, "Resume with corrections"); err != nil {
		t.Fatal(err)
	}
	if j.Stage != "queued" || j.BaseSHA != "" || j.CompletedCopiesCleanedAt.IsZero() {
		t.Fatal("cleaned task was not scheduled for fresh preparation")
	}
}
