package system

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

type nativePHPProfile struct {
	Version      int    `json:"version"`
	PHP          string `json:"php"`
	Vendor       string `json:"vendor"`
	ManifestHash string `json:"composer_manifest_sha256"`
	LockHash     string `json:"composer_lock_sha256"`
}

func nativeTaskTools(home string) string { return filepath.Join(filepath.Dir(home), "tools") }

func nativePHPTemp(home string) string {
	return filepath.Join(filepath.Dir(filepath.Dir(home)), "workspace", "vendor", ".orchestrator-tmp", strings.TrimSuffix(filepath.Base(home), "-runtime"))
}

func grantNativeTestRead(path, rights string) error {
	if runtime.GOOS != "windows" {
		return nil
	}
	// Avoid repeatedly propagating an unchanged grant through old task trees.
	if acl, err := exec.Command("icacls.exe", path).CombinedOutput(); err == nil {
		staleDeny := rights == "(RX)" && strings.Contains(string(acl), "\\CodexSandboxUsers:(DENY)")
		if staleDeny {
			// Older parallel profiles denied sibling task roots. Remove that
			// obsolete explicit deny only on these validated traversal directories.
			if output, err := exec.Command("icacls.exe", path, "/remove:d", "CodexSandboxUsers").CombinedOutput(); err != nil {
				return fmt.Errorf("repair stale task traversal deny: %w: %s", err, output)
			}
		} else if strings.Contains(string(acl), "\\CodexSandboxUsers:"+rights) {
			return nil
		}
	}
	if output, err := exec.Command("icacls.exe", path, "/grant", "CodexSandboxUsers:"+rights).CombinedOutput(); err != nil {
		return fmt.Errorf("grant task test tools read/execute: %w: %s", err, output)
	}
	return nil
}

func plainToolPath(base, target string) error {
	rel, err := filepath.Rel(base, target)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("test tool path escaped approved root")
	}
	for current := target; ; current = filepath.Dir(current) {
		if info, err := os.Lstat(current); err == nil {
			if info.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0 {
				return fmt.Errorf("test tool path contains a reparse point")
			}
		} else if !os.IsNotExist(err) {
			return err
		}
		if current == base {
			break
		}
		if current == filepath.Dir(current) {
			return fmt.Errorf("test tool path escaped approved root")
		}
	}
	return nil
}

func copyToolSnapshot(base, source, destination string) error {
	if err := plainToolPath(base, destination); err != nil {
		return err
	}
	if err := os.MkdirAll(destination, 0700); err != nil {
		return err
	}
	root, err := os.OpenRoot(destination)
	if err != nil {
		return err
	}
	defer root.Close()
	return filepath.WalkDir(source, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		if entry.Type()&(os.ModeSymlink|os.ModeIrregular) != 0 {
			return fmt.Errorf("approved test snapshot contains a link")
		}
		if entry.IsDir() {
			return root.MkdirAll(rel, 0700)
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("approved test snapshot contains a special file")
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer in.Close()
		out, err := root.OpenFile(rel, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(out, in)
		closeErr := out.Close()
		if copyErr != nil {
			return copyErr
		}
		return closeErr
	})
}

// Only operator-prepared snapshots are used. Agents cannot trigger downloads,
// Composer scripts/plugins, or installation of a modified dependency manifest.
func prepareNativeTestTools(c Config, j Job, workspace string) error {
	control := filepath.Join(c.NativeRoot, "control")
	b, err := os.ReadFile(filepath.Join(control, "native-php-tests.json"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var profile nativePHPProfile
	if err = json.Unmarshal(b, &profile); err != nil || profile.Version != 1 {
		return fmt.Errorf("invalid approved PHP test profile")
	}
	tools := filepath.Join(c.NativeRoot, "jobs", j.ID, "private", "tools")
	base := filepath.Join(c.NativeRoot, "jobs", j.ID)
	if !safeID.MatchString(j.ID) {
		return fmt.Errorf("invalid test tool task ID")
	}
	if err = plainToolPath(c.NativeRoot, base); err != nil {
		return err
	}
	phpSource := filepath.Join(control, filepath.FromSlash(profile.PHP))
	vendorSource := filepath.Join(control, filepath.FromSlash(profile.Vendor))
	for _, source := range []string{phpSource, vendorSource} {
		if err = plainToolPath(control, source); err != nil {
			return err
		}
	}
	stamp := fmt.Sprintf("%x", sha256.Sum256(append([]byte("native-php-tools-v2\n"), b...)))
	php := filepath.Join(tools, "php")
	ready := filepath.Join(tools, "profile.ready")
	previous, _ := os.ReadFile(ready)
	if string(previous) != stamp {
		if err = copyToolSnapshot(base, phpSource, php); err != nil {
			return err
		}
		ini := filepath.Join(php, "php.ini")
		template, err := os.ReadFile(ini)
		if err != nil {
			return err
		}
		if err = os.WriteFile(ini, []byte(strings.ReplaceAll(string(template), "@PHP_RUNTIME@", filepath.ToSlash(php))+"\nsys_temp_dir=\"${ORCHESTRATOR_TEST_TMP}\"\nupload_tmp_dir=\"${ORCHESTRATOR_TEST_TMP}\"\nsession.save_path=\"${ORCHESTRATOR_TEST_TMP}\"\n"), 0600); err != nil {
			return err
		}
		if err = os.WriteFile(ready, []byte(stamp), 0600); err != nil {
			return err
		}
	}
	if runtime.GOOS == "windows" {
		for _, dir := range []string{c.NativeRoot, filepath.Join(c.NativeRoot, "jobs"), base} {
			if grantErr := grantNativeTestRead(dir, "(RX)"); grantErr != nil {
				return grantErr
			}
		}
	}
	launcher := `<?php
// The Windows sandbox shell may start at the drive root. Resolve project-relative
// Codeception configuration from this task, never from the shell's directory.
$workspace = $argv[1];
if (!chdir($workspace)) { fwrite(STDERR, "Cannot enter test workspace: " . $workspace . "\n"); exit(2); }
array_splice($argv, 1, 1);
$argc = count($argv);
$_SERVER['argv'] = $argv;
$_SERVER['argc'] = $argc;
require $workspace . '/vendor/codeception/codeception/app.php';
`
	if err = os.WriteFile(filepath.Join(tools, "codecept.php"), []byte(launcher), 0600); err != nil {
		return err
	}
	if runtime.GOOS == "windows" {
		// Codex's read rule alone does not override the inherited native-root deny.
		// An explicit read/execute grant reopens only these approved task tools.
		if grantErr := grantNativeTestRead(tools, "(OI)(CI)(RX)"); grantErr != nil {
			return grantErr
		}
	}
	for _, role := range []string{"executor", "reviewer"} {
		tmp := nativePHPTemp(filepath.Join(base, "private", role+"-runtime"))
		if err = plainToolPath(base, tmp); err != nil {
			return err
		}
		if err = os.MkdirAll(tmp, 0700); err != nil {
			return err
		}
	}
	manifest, err := os.ReadFile(filepath.Join(workspace, "composer.json"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	lock, err := os.ReadFile(filepath.Join(workspace, "composer.lock"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if fmt.Sprintf("%x", sha256.Sum256(manifest)) != profile.ManifestHash || fmt.Sprintf("%x", sha256.Sum256(lock)) != profile.LockHash {
		// Other projects and dependency changes must not receive the wrong vendor
		// snapshot. PHP remains usable; missing dependencies follow the normal
		// resource-request flow rather than blocking unrelated coding work.
		return nil
	}
	vendorReady := filepath.Join(tools, "vendor.ready")
	previous, _ = os.ReadFile(vendorReady)
	if string(previous) != stamp {
		if err = copyToolSnapshot(base, vendorSource, filepath.Join(workspace, "vendor")); err != nil {
			return err
		}
		if err = os.WriteFile(vendorReady, []byte(stamp), 0600); err != nil {
			return err
		}
	}

	return nil
}
