package system

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func nativeCodexHome(home string) string { return filepath.Join(home, "codex") }

// The machine sandbox provisioning is reusable. Codex conversations, databases,
// memories, caches, capabilities and configuration never cross task/role homes.
func prepareNativeTaskRuntime(c Config, home, workspace, session string) error {
	root := nativeCodexHome(home)
	for _, dir := range []string{home, root} {
		if info, err := os.Lstat(dir); err == nil && info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("task runtime is a reparse point")
		}
		if err := os.MkdirAll(dir, 0700); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(filepath.Join(home, "scratch"), 0700); err != nil {
		return err
	}
	ownerPath := filepath.Join(home, "task-runtime.json")
	var owner struct {
		Workspace string `json:"workspace"`
	}
	if b, err := os.ReadFile(ownerPath); err == nil {
		if json.Unmarshal(b, &owner) != nil || !strings.EqualFold(filepath.Clean(owner.Workspace), filepath.Clean(workspace)) {
			return fmt.Errorf("task runtime belongs to another workspace")
		}
	} else if !os.IsNotExist(err) {
		return err
	} else {
		entries, err := os.ReadDir(root)
		if err != nil {
			return err
		}
		if len(entries) != 0 {
			return fmt.Errorf("unregistered nonempty task runtime; refusing reuse")
		}
		owner.Workspace = workspace
		if err = writePrivateJSON(ownerPath, owner); err != nil {
			return err
		}
	}
	seed := filepath.Join(c.NativeRoot, "codex-runtime")
	for _, relative := range []string{filepath.Join(".sandbox", "setup_marker.json"), filepath.Join(".sandbox-secrets", "sandbox_users.json")} {
		if err := copyNativeProvisioningFile(filepath.Join(seed, relative), filepath.Join(root, relative)); err != nil {
			return err
		}
	}
	if session != "" {
		if !safeID.MatchString(session) {
			return fmt.Errorf("invalid native session ID")
		}
		if err := migrateNativeSession(seed, root, workspace, session); err != nil {
			return err
		}
		if !nativeOwnedSessionExists(root, workspace, session) {
			return fmt.Errorf("saved session is unavailable for this task; refusing cross-task resume")
		}
		return nil
	}
	return nil
}

func copyNativeProvisioningFile(source, target string) error {
	if info, err := os.Lstat(target); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return fmt.Errorf("invalid task sandbox provisioning file")
		}
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	info, err := os.Lstat(source)
	if err != nil {
		return fmt.Errorf("native sandbox provisioning missing; run native isolation setup")
	}
	if !info.Mode().IsRegular() || info.Size() > 64<<10 {
		return fmt.Errorf("invalid native sandbox provisioning source")
	}
	for _, dir := range []string{filepath.Dir(source), filepath.Dir(target)} {
		if info, err := os.Lstat(dir); err == nil && info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("sandbox provisioning directory is a reparse point")
		}
	}
	b, err := os.ReadFile(source)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(target), 0700); err != nil {
		return err
	}
	return os.WriteFile(target, b, 0600)
}

func nativeSessionMatches(path, workspace, session string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	line, err := bufio.NewReader(io.LimitReader(f, 1<<20)).ReadBytes('\n')
	if err != nil {
		return false
	}
	var meta struct {
		Payload struct {
			ID  string `json:"id"`
			CWD string `json:"cwd"`
		} `json:"payload"`
	}
	return json.Unmarshal(line, &meta) == nil && meta.Payload.ID == session &&
		strings.EqualFold(filepath.Clean(meta.Payload.CWD), filepath.Clean(workspace))
}

func migrateNativeSession(seed, root, workspace, session string) error {
	// Only the exact saved session belonging to this workspace is transferable.
	// In particular, never copy the shared state database or session directory.
	sourceRoot := filepath.Join(seed, "sessions")
	return filepath.WalkDir(sourceRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if d.IsDir() || d.Type()&os.ModeSymlink != 0 || !strings.HasSuffix(d.Name(), ".jsonl") || !strings.Contains(d.Name(), session) {
			return nil
		}
		if !nativeSessionMatches(path, workspace, session) {
			return nil
		}
		relative, err := filepath.Rel(sourceRoot, path)
		if err != nil {
			return err
		}
		target := filepath.Join(root, "sessions", relative)
		if _, err = os.Lstat(target); err == nil {
			if !nativeSessionMatches(target, workspace, session) {
				return fmt.Errorf("task session ownership mismatch")
			}
			return nil
		} else if !os.IsNotExist(err) {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if info.Size() > 64<<20 {
			return fmt.Errorf("saved session exceeds migration limit")
		}
		if err = os.MkdirAll(filepath.Dir(target), 0700); err != nil {
			return err
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer in.Close()
		out, err := os.CreateTemp(filepath.Dir(target), ".rollout-*.tmp")
		if err != nil {
			return err
		}
		temporary := out.Name()
		defer os.Remove(temporary)
		_, copyErr := io.Copy(out, io.LimitReader(in, 64<<20))
		closeErr := out.Close()
		if copyErr != nil || closeErr != nil {
			os.Remove(temporary)
			if copyErr != nil {
				return copyErr
			}
			return closeErr
		}
		return os.Rename(temporary, target)
	})
}

func nativeSessionRoots(c Config, id, role string) []string {
	return []string{
		filepath.Join(c.NativeRoot, "jobs", id, "private", role+"-runtime", "codex", "sessions"),
		filepath.Join(c.NativeRoot, "codex-runtime", "sessions"), // Read-only legacy log fallback.
	}
}

func nativeOwnedSessionExists(root, workspace, session string) bool {
	found := false
	filepath.WalkDir(filepath.Join(root, "sessions"), func(path string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && d.Type()&os.ModeSymlink == 0 && strings.HasSuffix(d.Name(), ".jsonl") && strings.Contains(d.Name(), session) && nativeSessionMatches(path, workspace, session) {
			found = true
		}
		return nil
	})
	return found
}
