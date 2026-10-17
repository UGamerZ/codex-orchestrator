package system

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"codex-orchestrator/internal/broker"
)

type nativeScopePaths struct {
	Allow []string `json:"allow"`
	Deny  []string `json:"deny"`
}

// Registered native profiles live under one installation base. Enumerate only
// service-owned task directories, never project files or arbitrary host paths.
func nativeTaskScopePaths(c Config, workspace, home string) (nativeScopePaths, error) {
	out := nativeScopePaths{Allow: []string{workspace, filepath.Join(workspace, broker.ReviewDirectory), home, filepath.Join(home, "scratch"), nativeCodexHome(home)}, Deny: []string{}}
	base := c.NativeRoot
	if filepath.Base(filepath.Dir(base)) == "environments" {
		base = filepath.Dir(filepath.Dir(base))
	}
	roots := []string{base}
	entries, err := os.ReadDir(filepath.Join(base, "environments"))
	if err != nil && !os.IsNotExist(err) {
		return out, err
	}
	for _, d := range entries {
		if d.IsDir() && d.Type()&os.ModeSymlink == 0 {
			roots = append(roots, filepath.Join(base, "environments", d.Name()))
		}
	}
	seen := map[string]bool{}
	add := func(path string) error {
		info, err := os.Lstat(path)
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("task scope root is a reparse point")
		}
		if !info.IsDir() {
			return fmt.Errorf("task scope root is not a directory")
		}
		for _, allowed := range out.Allow {
			if strings.EqualFold(filepath.Clean(path), filepath.Clean(allowed)) {
				return nil
			}
		}
		if !seen[path] {
			out.Deny = append(out.Deny, path)
			seen[path] = true
		}
		return nil
	}
	for _, root := range roots {
		jobs, err := os.ReadDir(filepath.Join(root, "jobs"))
		if err != nil && !os.IsNotExist(err) {
			return out, err
		}
		for _, job := range jobs {
			if !job.IsDir() || !safeID.MatchString(job.Name()) {
				continue
			}
			jobRoot := filepath.Join(root, "jobs", job.Name())
			if err = add(filepath.Join(jobRoot, "workspace")); err != nil {
				return out, err
			}
			if err = add(filepath.Join(jobRoot, "workspace", broker.ReviewDirectory)); err != nil {
				return out, err
			}
			if err = add(filepath.Join(jobRoot, "private")); err != nil {
				return out, err
			}
			for _, role := range []string{"executor", "reviewer", "manager", "probe"} {
				roleHome := filepath.Join(jobRoot, "private", role+"-runtime")
				for _, path := range []string{roleHome, filepath.Join(roleHome, "scratch"), nativeCodexHome(roleHome)} {
					if err = add(path); err != nil {
						return out, err
					}
				}
			}
		}
	}
	return out, nil
}
