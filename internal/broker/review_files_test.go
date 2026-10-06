package broker

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReviewMetadataNeverPublished(t *testing.T) {
	src, dst := t.TempDir(), t.TempDir()
	review := filepath.Join(src, ReviewDirectory)
	os.Mkdir(review, 0755)
	if e := writeReviewFile(review, "diff.patch", []byte("trusted")); e != nil {
		t.Fatal(e)
	}
	os.WriteFile(filepath.Join(src, "code.txt"), []byte("code"), 0600)
	if e := copyTree(src, dst); e != nil {
		t.Fatal(e)
	}
	if _, e := os.Stat(filepath.Join(dst, ReviewDirectory)); !os.IsNotExist(e) {
		t.Fatal("review metadata copied into publication")
	}
	if _, e := os.Stat(filepath.Join(dst, "code.txt")); e != nil {
		t.Fatal(e)
	}
}
func TestTrustedReviewWriteDoesNotFollowAgentLink(t *testing.T) {
	dir := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside")
	os.WriteFile(outside, []byte("unchanged"), 0600)
	if e := os.Symlink(outside, filepath.Join(dir, "diff.patch")); e != nil {
		t.Fatal(e)
	}
	if e := writeReviewFile(dir, "diff.patch", []byte("trusted")); e != nil {
		t.Fatal(e)
	}
	b, e := os.ReadFile(outside)
	if e != nil || string(b) != "unchanged" {
		t.Fatal("review write followed agent link")
	}
	b, e = os.ReadFile(filepath.Join(dir, "diff.patch"))
	if e != nil || string(b) != "trusted" {
		t.Fatal("trusted input was not replaced")
	}
}
