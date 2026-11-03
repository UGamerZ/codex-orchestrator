package system

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

func nativeReviewTests(home string) string {
	return filepath.Join(filepath.Dir(filepath.Dir(home)), "reviewer-tests")
}

func (r *Runner) cleanupDockerReviewTests(ctx context.Context, id string) error {
	_, err := r.docker(ctx, nil, "run", "--rm", "--label", "codex-orchestrator="+r.namespace(), "--network", "none", "--read-only", "--user", "1000:1000", "--cap-drop=ALL", "--security-opt=no-new-privileges:true", "--pids-limit=32", "--memory=128m", "--cpus=1", "--mount", "type=volume,src="+r.volume(id, "reviewer-home")+",dst=/home/codex", r.C.Image, "cleanup-review-tests")
	return err
}

// Tests often generate actors, caches and fixtures inside the project. A fresh,
// disposable copy lets reviewers run them without writing reviewed source files.
func prepareNativeReviewTests(c Config, id, workspace string) (string, error) {
	if !safeID.MatchString(id) {
		return "", fmt.Errorf("invalid review test task ID")
	}
	base := filepath.Join(c.NativeRoot, "jobs", id)
	home := filepath.Join(base, "private", "reviewer-runtime")
	destination := nativeReviewTests(home)
	if filepath.Clean(workspace) != filepath.Join(base, "workspace") {
		return "", fmt.Errorf("review test source escaped task workspace")
	}
	if err := plainToolPath(c.NativeRoot, workspace); err != nil {
		return "", err
	}
	if err := plainToolPath(base, destination); err != nil {
		return "", err
	}
	if err := os.RemoveAll(destination); err != nil {
		return "", err
	}
	skip := func(path string) bool {
		for _, excluded := range []string{".git", ".orchestrator-review", "tests/_output", "vendor/.orchestrator-tmp"} {
			if path == excluded || strings.HasPrefix(path, excluded+"/") {
				return true
			}
		}
		return false
	}
	links := map[string]string{}
	canonicalRoot, err := filepath.EvalSymlinks(workspace)
	if err != nil {
		return "", err
	}
	if err := filepath.WalkDir(workspace, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(workspace, path)
		if err != nil {
			return err
		}
		if skip(filepath.ToSlash(rel)) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Type()&(os.ModeSymlink|os.ModeIrregular) == 0 {
			return nil
		}
		target, err := os.Readlink(path)
		if err != nil {
			return err
		}
		if !filepath.IsAbs(target) {
			target = filepath.Join(filepath.Dir(path), target)
		}
		targetRoot := workspace
		if resolved, resolveErr := filepath.EvalSymlinks(target); resolveErr == nil {
			target = resolved
			targetRoot = canonicalRoot
		} else if !os.IsNotExist(resolveErr) {
			return resolveErr
		}
		targetRel, err := filepath.Rel(targetRoot, filepath.Clean(target))
		if err != nil || targetRel == ".." || strings.HasPrefix(targetRel, ".."+string(filepath.Separator)) {
			return fmt.Errorf("review test link escapes source workspace: %s", rel)
		}
		links[filepath.ToSlash(rel)] = filepath.Join(destination, targetRel)
		return nil
	}); err != nil {
		return "", err
	}
	if err := copyToolSnapshot(base, workspace, destination, func(path string) bool { _, link := links[path]; return link || skip(path) }); err != nil {
		os.RemoveAll(destination)
		return "", err
	}
	for rel, target := range links {
		path := filepath.Join(destination, filepath.FromSlash(rel))
		if err := os.Symlink(target, path); err != nil {
			os.RemoveAll(destination)
			return "", err
		}
	}
	return destination, nil
}
