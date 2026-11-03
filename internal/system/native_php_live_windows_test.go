package system

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// Explicit operator-only probe. Project commands run exclusively inside the
// same offline permission profile and Windows job used by the executor.
func TestLiveNativePHPUnitSandbox(t *testing.T) {
	id := os.Getenv("ORCHESTRATOR_PHP_PROBE_TASK")
	if id == "" {
		t.Skip("operator-only live sandbox probe")
	}
	b, err := os.ReadFile(filepath.Join("..", "..", "config.native.json"))
	if err != nil {
		t.Fatal(err)
	}
	var c Config
	if err = json.Unmarshal(b, &c); err != nil {
		t.Fatal(err)
	}
	r := &Runner{C: c}
	p, err := r.nativePaths(id)
	if err != nil {
		t.Fatal(err)
	}
	role := os.Getenv("ORCHESTRATOR_PHP_PROBE_ROLE")
	if role == "" {
		role = "executor"
	}
	if role != "executor" && role != "reviewer" {
		t.Fatal("invalid probe role")
	}
	home := filepath.Join(filepath.Dir(p.Publish), role+"-runtime")
	if err = prepareNativeTestTools(c, Job{ID: id}, p.Workspace); err != nil {
		t.Fatal(err)
	}
	if err = prepareNativeTaskRuntime(c, home, p.Workspace, ""); err != nil {
		t.Fatal(err)
	}
	testRoot := p.Workspace
	prefix := ""
	if role == "reviewer" {
		testRoot, err = prepareNativeReviewTests(c, id, p.Workspace)
		if err != nil {
			t.Fatal(err)
		}
		defer os.RemoveAll(testRoot)
		sentinel := filepath.Join(p.Workspace, "reviewer-readonly-probe.txt")
		if err = os.WriteFile(sentinel, []byte("original"), 0600); err != nil {
			t.Fatal(err)
		}
		defer os.Remove(sentinel)
		prefix = "$ErrorActionPreference='Stop'; $blocked=$false; try { Set-Content -LiteralPath " + psQuote(sentinel) + " -Value 'changed' } catch { $blocked=$true }; if (!$blocked) { throw 'Reviewer can modify original source' }; 'ORIGINAL_SOURCE_READONLY'; "
	}
	php := filepath.Join(nativeTaskTools(home), "php", "php.exe")
	codecept := filepath.Join(nativeTaskTools(home), "codecept.php")
	config := filepath.Join(testRoot, "codeception.yml")
	output := filepath.Join(home, "scratch", "unit-suite-probe.log")
	selection := ""
	if test := os.Getenv("ORCHESTRATOR_PHP_PROBE_TEST"); test != "" {
		selection = " " + psQuote(test)
	}
	script := prefix + "$ErrorActionPreference='Stop'; & " + psQuote(php) + " -v; if ($LASTEXITCODE -ne 0) {throw 'PHP unavailable'}; & " + psQuote(php) + " " + psQuote(codecept) + " " + psQuote(testRoot) + " build -c " + psQuote(config) + "; if ($LASTEXITCODE -ne 0) {throw 'Unit actor build failed'}; $ErrorActionPreference='Continue'; & " + psQuote(php) + " " + psQuote(codecept) + " " + psQuote(testRoot) + " run unit" + selection + " -o 'coverage: {enabled: false}' --no-colors -c " + psQuote(config) + " 2>&1 | Tee-Object -FilePath " + psQuote(output) + "; 'UNIT_EXIT='+$LASTEXITCODE"
	text := runLiveNativeSandbox(t, c, r, p.Workspace, home, script)
	if !strings.Contains(text, "Tests:") && !strings.Contains(text, "OK (") {
		t.Fatal("unit suite did not reach test results")
	}
}

func runLiveNativeSandbox(t *testing.T, c Config, r *Runner, workspace, home, script string) string {
	t.Helper()
	role := strings.TrimSuffix(filepath.Base(home), "-runtime")
	args := append([]string{"sandbox", "--permission-profile", "orchestrator"}, nativePolicyArgs(c, workspace, home, role)...)
	args = append(args, "-C", workspace, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, c.CodexPath, args...)
	cmd.Dir = workspace
	cmd.Env = r.nativeEnv(home)
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x4 | 0x08000000}
	job, err := newWindowsJob()
	if err != nil {
		t.Fatal(err)
	}
	defer job.Close()
	cmd.Cancel = job.Terminate
	cmd.WaitDelay = 5 * time.Second
	var log boundedOutput
	cmd.Stdout = &log
	cmd.Stderr = &log
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	if err = job.Assign(cmd.Process.Pid); err != nil {
		cmd.Process.Kill()
		t.Fatal(err)
	}
	if err = resumeNativeProcess(cmd.Process.Pid); err != nil {
		job.Terminate()
		t.Fatal(err)
	}
	err = cmd.Wait()
	text := log.String()
	fmt.Println(text)
	if err != nil {
		t.Fatal("sandbox test launch failed", err)
	}
	return text
}

func TestLiveNativePHPParallelDirectoryAccess(t *testing.T) {
	id := os.Getenv("ORCHESTRATOR_PHP_PROBE_TASK")
	if id == "" {
		t.Skip("operator-only live sandbox probe")
	}
	b, err := os.ReadFile(filepath.Join("..", "..", "config.native.json"))
	if err != nil {
		t.Fatal(err)
	}
	var c Config
	if err = json.Unmarshal(b, &c); err != nil {
		t.Fatal(err)
	}
	r := &Runner{C: c}
	p, err := r.nativePaths(id)
	if err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(filepath.Dir(p.Publish), "executor-runtime")
	if err = prepareNativeTestTools(c, Job{ID: id}, p.Workspace); err != nil {
		t.Fatal(err)
	}
	if err = prepareNativeTaskRuntime(c, home, p.Workspace, ""); err != nil {
		t.Fatal(err)
	}
	probe := filepath.Join(nativeTaskTools(home), "directory-access-probe.php")
	marker := filepath.Join(nativePHPTemp(home), "directory-access-probe.ready")
	os.Remove(marker)
	defer os.Remove(marker)
	defer os.Remove(probe)
	if err = os.WriteFile(probe, []byte(`<?php
$workspace = $argv[1];
for ($i = 0; $i < 60; $i++) {
    clearstatcache(true);
    if (!is_dir($workspace) || realpath($workspace) === false || !chdir($workspace)) {
        for ($path = $workspace; strlen($path) > 3; $path = dirname($path)) {
            fwrite(STDERR, $path . ' is_dir=' . (is_dir($path) ? 'yes' : 'no') . ' realpath=' . var_export(realpath($path), true) . "\n");
        }
        fwrite(STDERR, "PHP directory access failed during parallel sandbox setup\n"); exit(2);
    }
    if ($i === 0) { file_put_contents($argv[2], 'ready'); }
    usleep(500000);
}
echo "PHP_PATH_OK\n";
`), 0600); err != nil {
		t.Fatal(err)
	}
	peerID := fmt.Sprintf("php-acl-probe-%d", time.Now().UnixNano())
	peer, err := r.nativePaths(peerID)
	if err != nil {
		t.Fatal(err)
	}
	peerHome := filepath.Join(filepath.Dir(peer.Publish), "executor-runtime")
	if err = os.MkdirAll(peer.Workspace, 0700); err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(filepath.Dir(peer.Workspace)) // Own unique probe root; never follow runtime junctions.
	if err = prepareNativeTaskRuntime(c, peerHome, peer.Workspace, ""); err != nil {
		t.Fatal(err)
	}
	done := make(chan string, 1)
	go func() {
		defer close(done)
		script := "& " + psQuote(filepath.Join(nativeTaskTools(home), "php", "php.exe")) + " " + psQuote(probe) + " " + psQuote(p.Workspace) + " " + psQuote(marker) + "; if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }"
		done <- runLiveNativeSandbox(t, c, r, p.Workspace, home, script)
	}()
	deadline := time.Now().Add(60 * time.Second)
	for {
		if _, err = os.Stat(marker); err == nil {
			break
		}
		select {
		case output := <-done:
			t.Fatalf("PHP probe failed before peer launch: %s", output)
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("PHP probe did not start")
		}
		time.Sleep(250 * time.Millisecond)
	}
	peerOutput := runLiveNativeSandbox(t, c, r, peer.Workspace, peerHome, "Start-Sleep -Seconds 3; 'peer-policy-applied'")
	if !strings.Contains(peerOutput, "peer-policy-applied") {
		t.Fatal("peer sandbox did not run")
	}
	if output := <-done; !strings.Contains(output, "PHP_PATH_OK") {
		t.Fatal("PHP lost directory access", output)
	}
	launcher := "& " + psQuote(filepath.Join(nativeTaskTools(home), "php", "php.exe")) + " " + psQuote(filepath.Join(nativeTaskTools(home), "codecept.php")) + " " + psQuote(p.Workspace) + " --version; if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }"
	if output := runLiveNativeSandbox(t, c, r, p.Workspace, home, launcher); !strings.Contains(output, "Codeception") {
		t.Fatal("Codeception launcher failed", output)
	}
}
