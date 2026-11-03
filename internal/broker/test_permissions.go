package broker

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
)

// These paths come only from operator configuration, never task/agent results.
func ValidateTestWritePaths(paths []string) ([]string, error) {
	var result []string
	seen := map[string]bool{}
	for _, value := range paths {
		value = strings.ReplaceAll(strings.TrimSpace(value), "\\", "/")
		if value == "" || strings.HasPrefix(value, "/") || strings.ContainsAny(value, ",:\x00\r\n") {
			return nil, fmt.Errorf("test_write_paths must contain project-relative directories")
		}
		for _, part := range strings.Split(value, "/") {
			if part == ".." || strings.EqualFold(part, ".git") || strings.EqualFold(part, ReviewDirectory) {
				return nil, fmt.Errorf("reserved test write path")
			}
		}
		value = path.Clean(value)
		if value == "." {
			return nil, fmt.Errorf("reviewers cannot receive write access to the whole workspace")
		}
		if !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	return result, nil
}

func ReviewTestWriteRoots(workspace string, paths []string) ([]string, error) {
	paths, err := ValidateTestWritePaths(paths)
	if err != nil {
		return nil, err
	}
	base, err := filepath.EvalSymlinks(workspace)
	if err != nil {
		return nil, err
	}
	var roots []string
	for _, rel := range paths {
		lexical := filepath.Join(workspace, filepath.FromSlash(rel))
		resolved, err := filepath.EvalSymlinks(lexical)
		if err != nil {
			return nil, err
		}
		inside, err := filepath.Rel(base, resolved)
		if err != nil {
			return nil, err
		}
		if _, err = ValidateTestWritePaths([]string{filepath.ToSlash(inside)}); err != nil {
			return nil, fmt.Errorf("test write path escapes workspace or targets metadata: %s", rel)
		}
		roots = append(roots, lexical, resolved)
	}
	return roots, nil
}

func PrepareReviewTestWrites(workspace string, paths []string) error {
	paths, err := ValidateTestWritePaths(paths)
	if err != nil {
		return err
	}
	root, err := os.OpenRoot(workspace)
	if err != nil {
		return err
	}
	defer root.Close()
	for _, rel := range paths {
		if err = root.MkdirAll(filepath.FromSlash(rel), 0755); err != nil {
			return err
		}
	}
	roots, err := ReviewTestWriteRoots(workspace, paths)
	if err != nil {
		return err
	}
	if runtime.GOOS != "windows" {
		for _, target := range roots {
			if err = os.Chown(target, 1000, 1000); err != nil {
				return err
			}
		}
	}
	return nil
}
