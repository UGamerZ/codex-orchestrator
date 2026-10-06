package broker

import (
	"fmt"
	"os"
	"path/filepath"
)

// Replace the directory entry instead of following a file/symlink left by a worker.
// Review inputs are generated after the executor exits and never published.
func writeReviewFile(dir, name string, data []byte) error {
	if name != "diff.patch" && name != "description.txt" {
		return fmt.Errorf("unexpected review file")
	}
	info, e := os.Lstat(dir)
	if e != nil {
		return e
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("review directory must be a regular directory")
	}
	f, e := os.CreateTemp(dir, ".trusted-review-*")
	if e != nil {
		return e
	}
	defer os.Remove(f.Name())
	if e = f.Chmod(0644); e != nil {
		f.Close()
		return e
	}
	_, e = f.Write(data)
	closeErr := f.Close()
	if e != nil {
		return e
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(f.Name(), filepath.Join(dir, name))
}
