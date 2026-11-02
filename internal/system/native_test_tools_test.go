package system

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNativeApprovedTestToolsAreTaskLocalAndRequireMatchingLock(t *testing.T) {
	c := nativeFixture(t)
	control := filepath.Join(c.NativeRoot, "control")
	manifest := []byte(`{"require-dev":{}}`)
	lock := []byte(`{"packages":[]}`)
	for path, body := range map[string]string{"php/php.exe": "runtime", "php/php.ini": "extension_dir=@PHP_RUNTIME@/ext", "vendor/autoload.php": "autoload", "vendor/package/data.txt": "approved dependency"} {
		path = filepath.Join(control, filepath.FromSlash(path))
		os.MkdirAll(filepath.Dir(path), 0700)
		if err := os.WriteFile(path, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	profile := nativePHPProfile{Version: 1, PHP: "php", Vendor: "vendor", ManifestHash: fmt.Sprintf("%x", sha256.Sum256(manifest)), LockHash: fmt.Sprintf("%x", sha256.Sum256(lock))}
	b, _ := json.Marshal(profile)
	os.WriteFile(filepath.Join(control, "native-php-tests.json"), b, 0600)
	for _, id := range []string{"task-a", "task-b"} {
		workspace := filepath.Join(c.NativeRoot, "jobs", id, "workspace")
		os.MkdirAll(workspace, 0700)
		os.WriteFile(filepath.Join(workspace, "composer.json"), manifest, 0600)
		os.WriteFile(filepath.Join(workspace, "composer.lock"), lock, 0600)
		if err := prepareNativeTestTools(c, Job{ID: id}, workspace); err != nil {
			t.Fatal(err)
		}
		ini, err := os.ReadFile(filepath.Join(c.NativeRoot, "jobs", id, "private", "tools", "php", "php.ini"))
		if err != nil || strings.Contains(string(ini), "@PHP_RUNTIME@") {
			t.Fatal("PHP ini not localized", err)
		}
		dep := filepath.Join(workspace, "vendor", "package", "data.txt")
		if b, err := os.ReadFile(dep); err != nil || string(b) != "approved dependency" {
			t.Fatal("dependency isolation failed", err)
		}
		os.WriteFile(dep, []byte("task changed dependency"), 0600)
	}
	w := filepath.Join(c.NativeRoot, "jobs", "task-b", "workspace")
	os.WriteFile(filepath.Join(w, "composer.lock"), []byte("modified lock"), 0600)
	if err := prepareNativeTestTools(c, Job{ID: "task-b"}, w); err != nil {
		t.Fatal("unmatched snapshot blocked coding work", err)
	}
	data, _ := os.ReadFile(filepath.Join(w, "vendor", "package", "data.txt"))
	if string(data) != "task changed dependency" {
		t.Fatal("unmatched dependency snapshot was copied")
	}
}

func TestTestToolCopyRejectsLinkedDestination(t *testing.T) {
	base := t.TempDir()
	source := t.TempDir()
	outside := t.TempDir()
	os.WriteFile(filepath.Join(source, "data"), []byte("source"), 0600)
	if err := os.Symlink(outside, filepath.Join(base, "vendor")); err != nil {
		t.Fatal(err)
	}
	if err := copyToolSnapshot(base, source, filepath.Join(base, "vendor")); err == nil {
		t.Fatal("escaped destination accepted")
	}
	if _, err := os.Stat(filepath.Join(outside, "data")); !os.IsNotExist(err) {
		t.Fatal("wrote outside task")
	}
}
