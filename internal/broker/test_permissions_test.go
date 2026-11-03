package broker

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReviewTestWritesStayWithinConfiguredRuntimeDirectories(t *testing.T) {
	for _, path := range []string{".", "../escape", "/absolute", "C:/host", ".git/hooks", "files,readonly=false"} {
		if _, err := ValidateTestWritePaths([]string{path}); err == nil {
			t.Fatalf("unsafe write scope accepted: %s", path)
		}
	}
	w := t.TempDir()
	if err := PrepareReviewTestWrites(w, []string{"files", "tests/_output"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(w, "files")); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(w, "external")); err != nil {
		t.Fatal(err)
	}
	if _, err := ReviewTestWriteRoots(w, []string{"external"}); err == nil {
		t.Fatal("external runtime symlink allowed")
	}
	if err := os.MkdirAll(filepath.Join(w, ".git"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(w, ".git"), filepath.Join(w, "metadata")); err != nil {
		t.Fatal(err)
	}
	if _, err := ReviewTestWriteRoots(w, []string{"metadata"}); err == nil {
		t.Fatal("metadata alias writable")
	}
}
