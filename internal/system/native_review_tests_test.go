package system

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestReviewerTestCopyIsFreshAndSeparate(t *testing.T) {
	c := nativeFixture(t)
	base := filepath.Join(c.NativeRoot, "jobs", "review-task")
	w := filepath.Join(base, "workspace")
	for name, text := range map[string]string{"src/code.php": "original", "vendor/autoload.php": "deps", ".git/config": "metadata", ".orchestrator-review/context.txt": "trusted", "tests/_output/old.log": "old"} {
		path := filepath.Join(w, filepath.FromSlash(name))
		os.MkdirAll(filepath.Dir(path), 0700)
		os.WriteFile(path, []byte(text), 0600)
	}
	if err := os.Symlink("code.php", filepath.Join(w, "src", "link.php")); err != nil {
		t.Fatal(err)
	}
	copy, err := prepareNativeReviewTests(c, "review-task", w)
	if err != nil {
		t.Fatal(err)
	}
	for _, excluded := range []string{".git", ".orchestrator-review", "tests/_output"} {
		if _, err = os.Stat(filepath.Join(copy, excluded)); !os.IsNotExist(err) {
			t.Fatal("stale or trusted metadata copied", excluded, err)
		}
	}
	os.WriteFile(filepath.Join(copy, "src", "code.php"), []byte("test changed copy"), 0600)
	if b, err := os.ReadFile(filepath.Join(copy, "src", "link.php")); err != nil || string(b) != "test changed copy" {
		t.Fatal("internal link was not redirected into disposable copy", err)
	}
	if b, _ := os.ReadFile(filepath.Join(w, "src", "code.php")); string(b) != "original" {
		t.Fatal("reviewed source changed")
	}
	os.WriteFile(filepath.Join(copy, "stale.tmp"), []byte("stale"), 0600)
	if _, err = prepareNativeReviewTests(c, "review-task", w); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(filepath.Join(copy, "stale.tmp")); !os.IsNotExist(err) {
		t.Fatal("review reused old test artifacts")
	}
	home := filepath.Join(base, "private", "reviewer-runtime")
	policy := strings.Join(nativePolicyArgs(c, w, home, "reviewer"), "\n")
	if !strings.Contains(policy, strconv.Quote(filepath.ToSlash(w))+`="read"`) {
		t.Fatal("reviewer source must be read-only")
	}
	if !strings.Contains(policy, strconv.Quote(filepath.ToSlash(copy))+`="write"`) {
		t.Fatal("reviewer tests need a writable copy")
	}
}

func TestReviewerTestCopyRejectsLinkedRoot(t *testing.T) {
	c := nativeFixture(t)
	base := filepath.Join(c.NativeRoot, "jobs", "review-task")
	w := filepath.Join(base, "workspace")
	os.MkdirAll(w, 0700)
	outside := t.TempDir()
	os.WriteFile(filepath.Join(outside, "keep"), []byte("safe"), 0600)
	if err := os.Symlink(outside, filepath.Join(base, "reviewer-tests")); err != nil {
		t.Fatal(err)
	}
	if _, err := prepareNativeReviewTests(c, "review-task", w); err == nil {
		t.Fatal("linked review root accepted")
	}
	if b, _ := os.ReadFile(filepath.Join(outside, "keep")); string(b) != "safe" {
		t.Fatal("outside data changed")
	}
}

func TestReviewerTestCopyRejectsExternalSourceLink(t *testing.T) {
	c := nativeFixture(t)
	w := filepath.Join(c.NativeRoot, "jobs", "review-task", "workspace")
	os.MkdirAll(w, 0700)
	outside := t.TempDir()
	os.WriteFile(filepath.Join(outside, "keep"), []byte("outside"), 0600)
	if err := os.Symlink(outside, filepath.Join(w, "external")); err != nil {
		t.Fatal(err)
	}
	if _, err := prepareNativeReviewTests(c, "review-task", w); err == nil {
		t.Fatal("external source link copied into review")
	}
	if b, _ := os.ReadFile(filepath.Join(outside, "keep")); string(b) != "outside" {
		t.Fatal("outside file changed")
	}
}
