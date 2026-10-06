package broker

import (
	"archive/tar"
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func archiveEntry(name string, kind byte, body string) []byte {
	var b bytes.Buffer
	w := tar.NewWriter(&b)
	h := &tar.Header{Name: name, Typeflag: kind, Mode: 0644}
	if kind == tar.TypeReg {
		h.Size = int64(len(body))
	}
	w.WriteHeader(h)
	if kind == tar.TypeReg {
		w.Write([]byte(body))
	}
	w.Close()
	return b.Bytes()
}
func TestArchiveBoundary(t *testing.T) {
	for _, name := range []string{"../escape", "/absolute", "x/../../escape"} {
		if extract(archiveEntry(name, tar.TypeReg, "x"), t.TempDir()) == nil {
			t.Fatalf("accepted %s", name)
		}
	}
	if extract(archiveEntry("link", tar.TypeSymlink, ""), t.TempDir()) == nil {
		t.Fatal("accepted symlink")
	}
}
func TestCopyIgnoresAgentGit(t *testing.T) {
	src, dst := t.TempDir(), t.TempDir()
	os.Mkdir(filepath.Join(src, ".git"), 0700)
	os.WriteFile(filepath.Join(src, ".git", "config"), []byte("untrusted"), 0600)
	os.WriteFile(filepath.Join(src, "app.php"), []byte("<?php echo 1;"), 0644)
	if e := copyTree(src, dst); e != nil {
		t.Fatal(e)
	}
	if _, e := os.Stat(filepath.Join(dst, ".git")); !os.IsNotExist(e) {
		t.Fatal("agent git copied")
	}
	if _, e := os.Stat(filepath.Join(dst, "app.php")); e != nil {
		t.Fatal(e)
	}
}
