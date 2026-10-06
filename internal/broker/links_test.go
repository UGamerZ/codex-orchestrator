package broker

import (
	"archive/tar"
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func linkArchive(entries []tar.Header) []byte {
	var b bytes.Buffer
	w := tar.NewWriter(&b)
	for _, h := range entries {
		h.Mode = 0755
		w.WriteHeader(&h)
		if h.Typeflag == tar.TypeReg {
			w.Write([]byte("x"))
		}
	}
	w.Close()
	return b.Bytes()
}
func TestInternalRepositoryLinks(t *testing.T) {
	archive := linkArchive([]tar.Header{{Name: "shared/data", Typeflag: tar.TypeReg, Size: 1}, {Name: "theme/lk", Typeflag: tar.TypeSymlink, Linkname: "../shared"}})
	src, dst := t.TempDir(), t.TempDir()
	if e := extract(archive, src); e != nil {
		t.Fatal(e)
	}
	b, e := os.ReadFile(filepath.Join(src, "theme", "lk", "data"))
	if e != nil || string(b) != "x" {
		t.Fatal("internal directory link is unusable")
	}
	links, e := archiveLinks(archive)
	if e != nil {
		t.Fatal(e)
	}
	if e = copyTree(src, dst, links); e != nil {
		t.Fatal(e)
	}
	if info, e := os.Lstat(filepath.Join(dst, "theme", "lk")); e != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatal("link was materialized instead of preserved")
	}
	os.Remove(filepath.Join(src, "theme", "lk"))
	os.Symlink("../other", filepath.Join(src, "theme", "lk"))
	if e = copyTree(src, t.TempDir(), links); e == nil {
		t.Fatal("agent changed a link target")
	}
}
func TestUnsafeLinksAndArchiveWritesAreRejected(t *testing.T) {
	cases := [][]tar.Header{
		{{Name: "link", Typeflag: tar.TypeSymlink, Linkname: "../outside"}},
		{{Name: "link", Typeflag: tar.TypeSymlink, Linkname: "/outside"}},
		{{Name: "link", Typeflag: tar.TypeSymlink, Linkname: "C:\\outside"}},
		{{Name: "link", Typeflag: tar.TypeSymlink, Linkname: ".git/config"}},
		{{Name: "a", Typeflag: tar.TypeSymlink, Linkname: "b"}, {Name: "b", Typeflag: tar.TypeSymlink, Linkname: "a"}},
		{{Name: "deep/more/alias", Typeflag: tar.TypeSymlink, Linkname: "../../safe"}, {Name: "b", Typeflag: tar.TypeSymlink, Linkname: "deep/more/alias/../../outside"}},
		{{Name: "a", Typeflag: tar.TypeSymlink, Linkname: "shared"}, {Name: "a/file", Typeflag: tar.TypeReg, Size: 1}},
		{{Name: ".git/config", Typeflag: tar.TypeReg, Size: 1}},
	}
	for _, entries := range cases {
		if e := extract(linkArchive(entries), t.TempDir()); e == nil {
			t.Fatalf("unsafe archive accepted: %+v", entries)
		}
	}
	src := t.TempDir()
	if e := os.Symlink("../outside", filepath.Join(src, "new-link")); e != nil {
		t.Fatal(e)
	}
	if e := copyTree(src, t.TempDir()); e == nil {
		t.Fatal("agent-created link accepted")
	}
}
